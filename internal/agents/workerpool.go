package agents

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/jtsang4/hidane/internal/agentcli"
	"github.com/jtsang4/hidane/internal/guard"
	"github.com/jtsang4/hidane/internal/kernel"
)

// WorkerPool runs executions — the runtime's I/O. They are dispatched from a
// turn that then ends, run as isolated CLI subprocesses, and reported by
// posting execution.finished to the owner's mailbox. Two limits shape the
// queue: at most MaxWorkers at once, and one per work item (one workspace, one
// writer).
type WorkerPool struct {
	s *System
	// ctx ends every CLI the pool started when the runtime stops.
	ctx    context.Context
	cancel context.CancelFunc

	mu      sync.Mutex
	jobs    map[string]*job
	order   []string
	running map[string]*job
	wg      sync.WaitGroup
}

type job struct {
	executionID  string
	workItemID   string
	owner        string
	instructions string
	started      kernel.Event
	// amendments are the person's words that arrived while the job was queued.
	amendments []string

	mu        sync.Mutex
	run       agentcli.Run
	buffer    []string
	cancelled bool
	pending   string
}

func newWorkerPool(s *System) *WorkerPool {
	ctx, cancel := context.WithCancel(context.Background())
	return &WorkerPool{s: s, ctx: ctx, cancel: cancel, jobs: map[string]*job{}, running: map[string]*job{}}
}

// Shutdown stops every running CLI and waits (bounded) for their outcomes to
// be reported as lost — a worker must not outlive the runtime that owns it,
// or the next start would dispatch a second writer into the same workspace.
func (p *WorkerPool) Shutdown(timeout time.Duration) {
	p.cancel()
	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(timeout):
	}
}

// ErrBudget means the causal hop budget refused the dispatch; a person was asked instead.
var ErrBudget = errors.New("hop budget spent")

// Dispatch records an execution and queues it; the caller's turn returns.
func (p *WorkerPool) Dispatch(ctx context.Context, item kernel.WorkItem, owner, instructions, expect string, causedBy kernel.Event) (kernel.Event, error) {
	k := p.s.K
	// Checked before anything is spent: the outcome of a dispatched execution
	// is always delivered, so this is the last point a chain can stop cheaply.
	if k.OverBudget(&causedBy) {
		if err := k.BudgetEscalation(ctx, &causedBy, "execution.started", owner, item.ID); err != nil {
			return kernel.Event{}, err
		}
		return kernel.Event{}, ErrBudget
	}
	id := kernel.GenID("ex", 6)
	if err := k.CreateExecution(ctx, id, item.ID, owner); err != nil {
		return kernel.Event{}, err
	}
	var exp any
	if expect != "" {
		exp = expect
	}
	started, err := k.Append(ctx, kernel.EventInput{Source: "agent:manager", Kind: "execution.started", ThreadID: item.ThreadID,
		WorkItemID: item.ID, ExecutionID: id, CausedBy: causedBy.ID, Hop: causedBy.Hop + 1,
		Payload: kernel.Payload{"instructions": instructions, "expect": exp, "root": kernel.RootOf(causedBy)}})
	if err != nil {
		return started, err
	}
	p.mu.Lock()
	p.jobs[id] = &job{executionID: id, workItemID: item.ID, owner: owner, instructions: instructions, started: started}
	p.order = append(p.order, id)
	p.mu.Unlock()
	p.Pump()
	return started, nil
}

// AmendQueued adds the person's words to a job that has not started.
func (p *WorkerPool) AmendQueued(workItemID, text string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, id := range p.order {
		j := p.jobs[id]
		if j != nil && j.workItemID == workItemID && p.running[id] == nil {
			j.amendments = append(j.amendments, text)
			return true
		}
	}
	return false
}

func (p *WorkerPool) activeJobFor(workItemID string) *job {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, j := range p.running {
		if j.workItemID == workItemID {
			return j
		}
	}
	return nil
}

// Steer hands the person's words to the running execution for a work item.
// The pending-input flag goes up first, so no write can slip between the two.
func (p *WorkerPool) Steer(workItemID, text string) bool {
	j := p.activeJobFor(workItemID)
	if j == nil {
		return false
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.pending != "" {
		_ = os.WriteFile(j.pending, []byte(clipRunes(text, 2000)), 0o644)
	}
	if j.run == nil {
		j.buffer = append(j.buffer, text)
		return true
	}
	return j.run.Steer(text)
}

// Pump starts whatever the limits allow.
func (p *WorkerPool) Pump() {
	p.mu.Lock()
	defer p.mu.Unlock()
	busy := map[string]bool{}
	for _, j := range p.running {
		busy[j.workItemID] = true
	}
	for _, id := range p.order {
		if len(p.running) >= p.s.K.Cfg.MaxWorkers {
			break
		}
		j := p.jobs[id]
		if j == nil || p.running[id] != nil || busy[j.workItemID] {
			continue
		}
		busy[j.workItemID] = true
		p.running[id] = j
		p.wg.Add(1)
		go p.runJob(j)
	}
}

func (p *WorkerPool) remove(id string) {
	p.mu.Lock()
	delete(p.jobs, id)
	delete(p.running, id)
	for i, x := range p.order {
		if x == id {
			p.order = append(p.order[:i], p.order[i+1:]...)
			break
		}
	}
	p.mu.Unlock()
}

// Wait blocks until running jobs finish (tests, shutdown).
func (p *WorkerPool) Wait() { p.wg.Wait() }

type runOutcome struct {
	agentcli.Result
	Lost         bool
	PolicyBlocks []guard.Block
}

func (p *WorkerPool) runJob(j *job) {
	defer p.wg.Done()
	ctx := context.Background()
	k := p.s.K
	var outcome runOutcome
	var item *kernel.WorkItem
	skip := false
	// Tool events are appended in order and all land before the outcome.
	var trailMu sync.Mutex
	defer func() {
		p.remove(j.executionID)
		p.Pump()
	}()
	func() {
		it, err := k.GetWorkItem(ctx, j.workItemID)
		if err != nil {
			outcome.Error = err.Error()
			return
		}
		item = &it
		cur, ok, err := k.GetExecution(ctx, j.executionID)
		if err != nil || !ok || cur.Status != kernel.ExecQueued {
			// Cancelled between dispatch and start; CancelTree already reported it.
			skip = true
			return
		}
		if err := k.SetExecutionStatus(ctx, j.executionID, kernel.ExecRunning); err != nil {
			outcome.Error = err.Error()
			return
		}
		_, _ = k.Append(ctx, kernel.EventInput{Source: "agent:worker", Kind: "execution.running", ThreadID: it.ThreadID,
			WorkItemID: it.ID, ExecutionID: j.executionID, Payload: kernel.Payload{}})
		instructions := j.instructions
		p.mu.Lock()
		amend := append([]string{}, j.amendments...)
		p.mu.Unlock()
		if len(amend) > 0 {
			instructions += "\n\nThe person added, after this was planned:\n- " + strings.Join(amend, "\n- ")
		}
		hidaneDir := filepath.Join(it.Workspace, ".hidane")
		_ = os.MkdirAll(hidaneDir, 0o755)
		// The guard's own signals live outside the workspace, where the worker
		// it governs cannot delete or forge them.
		control := filepath.Join(k.Cfg.RuntimeDir(), "executions", j.executionID)
		_ = os.MkdirAll(control, 0o755)
		defer os.RemoveAll(control)
		pending := filepath.Join(control, "pending-input")
		blocks := filepath.Join(control, "policy-blocks.jsonl")
		genv := guard.Env{PolicyFiles: k.PolicyFilesFor(ctx, it), PendingInputFile: pending, BlocksFile: blocks,
			Workspace: it.Workspace, Protected: k.Cfg.Home}
		r := p.s.Settings.Get().Resolve("worker")
		req := agentcli.Request{
			Prompt: instructions, SystemPrompt: WorkerCharter, Cwd: it.Workspace, Tools: true,
			Model: r.Model, Effort: r.Effort, Provider: r.Provider,
			SessionDir: filepath.Join(hidaneDir, "sessions"), Env: genv.Vars(), Timeout: k.Cfg.WorkerTimeout,
			// Two-phase side-effect trail: intent before the tool acts, result after.
			OnTool: func(e agentcli.ToolEvent) {
				trailMu.Lock()
				defer trailMu.Unlock()
				in := kernel.EventInput{Source: "agent:worker", ThreadID: it.ThreadID, WorkItemID: it.ID, ExecutionID: j.executionID}
				if e.Phase == "start" {
					in.Kind, in.Payload = "side_effect.intent", kernel.Payload{"tool": e.Tool, "input": e.Detail}
				} else {
					in.Kind, in.Payload = "side_effect.result", kernel.Payload{"tool": e.Tool, "isError": e.IsError}
				}
				_, _ = k.Append(ctx, in)
			},
			OnSteerConsumed: func() { _ = os.Remove(pending) },
		}
		j.mu.Lock()
		j.pending = pending
		cancelled := j.cancelled
		j.mu.Unlock()
		if cancelled {
			outcome.Cancelled, outcome.Error = true, "cancelled"
			return
		}
		run, err := p.s.Agents.Start(p.ctx, r.Agent, req)
		if err != nil {
			outcome.Error = err.Error()
			return
		}
		j.mu.Lock()
		j.run = run
		buffered := j.buffer
		j.buffer = nil
		cancelled = j.cancelled
		j.mu.Unlock()
		if cancelled {
			run.Cancel()
		}
		for _, b := range buffered {
			run.Steer(b)
		}
		outcome.Result = run.Wait()
		outcome.PolicyBlocks = guard.ReadBlocks(blocks)
		j.mu.Lock()
		if p.ctx.Err() != nil && !j.cancelled {
			outcome.OK, outcome.Lost = false, true
			outcome.Error = "lost: the runtime stopped while this execution was active"
		}
		if j.cancelled {
			// The outcome follows the person's intent, not which signal won.
			outcome.OK, outcome.Cancelled, outcome.Error = false, true, "cancelled"
		}
		j.mu.Unlock()
	}()
	if skip {
		return
	}
	trailMu.Lock()
	trailMu.Unlock()
	if err := p.reportOutcome(ctx, j.executionID, j.workItemID, j.owner, &j.started, item, outcome); err != nil {
		_, _ = k.Append(ctx, kernel.EventInput{Source: "kernel:runtime", Kind: "agent.error", ThreadID: "main",
			Payload: kernel.Payload{"error": "reporting execution outcome failed: " + err.Error(), "executionId": j.executionID}})
	}
}

var blockedLine = regexp.MustCompile(`(?m)^\s*BLOCKED:\s*(.+)$`)

// BlockedQuestion: a worker that cannot proceed ends with `BLOCKED: <question>`.
func BlockedQuestion(text string) string {
	if m := blockedLine.FindStringSubmatch(text); m != nil {
		return strings.TrimSpace(m[1])
	}
	return ""
}

// reportOutcome records the outcome and delivers it to the owner — the one way
// an execution ends.
func (p *WorkerPool) reportOutcome(ctx context.Context, executionID, workItemID, owner string, started *kernel.Event, item *kernel.WorkItem, run runOutcome) error {
	k := p.s.K
	status := kernel.ExecFailed
	switch {
	case run.Lost:
		status = kernel.ExecLost
	case run.Cancelled:
		status = kernel.ExecCancelled
	case run.OK:
		status = kernel.ExecDone
	}
	if err := k.SetExecutionStatus(ctx, executionID, status); err != nil {
		return err
	}
	threadID := ""
	if item != nil {
		threadID = item.ThreadID
	}
	rootP := kernel.Payload{}
	if started != nil && started.ID != "" {
		rootP["root"] = kernel.RootOf(*started)
	}
	var blocks []any
	for _, b := range run.PolicyBlocks {
		blocks = append(blocks, map[string]any{"tool": b.Tool, "reason": b.Reason})
		payload := kernel.Payload{"tool": b.Tool, "rule": nil, "reason": b.Reason}
		if strings.HasPrefix(b.Reason, "blocked by hidane policy ") {
			rest := strings.TrimPrefix(b.Reason, "blocked by hidane policy ")
			if i := strings.Index(rest, ": "); i > 0 {
				payload["rule"], payload["reason"] = rest[:i], rest[i+2:]
			}
		}
		for key, v := range rootP {
			payload[key] = v
		}
		if _, err := k.Append(ctx, kernel.EventInput{Source: "agent:worker", Kind: "policy.blocked", ThreadID: threadID,
			WorkItemID: workItemID, ExecutionID: executionID, Payload: payload}); err != nil {
			return err
		}
	}
	if blocks == nil {
		blocks = []any{}
	}
	var blocked any
	if run.OK {
		if q := BlockedQuestion(run.Text); q != "" {
			blocked = q
		}
	}
	var errVal any
	if run.Error != "" {
		errVal = run.Error
	}
	source := "agent:worker"
	payload := kernel.Payload{"ok": run.OK, "durationMs": run.DurationMs, "toolCalls": run.ToolCalls,
		"summary": clipRunes(run.Text, 8000), "error": errVal, "cancelled": run.Cancelled, "blocked": blocked, "policyBlocks": blocks}
	if run.Lost {
		source = "kernel:runtime"
		payload["lost"] = true
	}
	for key, v := range rootP {
		payload[key] = v
	}
	_, _, err := k.Post(ctx, kernel.PostInput{EventInput: kernel.EventInput{Source: source, Kind: "execution.finished",
		Mailbox: owner, Lane: kernel.LaneNormal, ThreadID: threadID, WorkItemID: workItemID, ExecutionID: executionID, Payload: payload},
		CausedBy: started, AlwaysDeliver: true})
	return err
}

func (p *WorkerPool) startedEventOf(ctx context.Context, ex kernel.Execution) *kernel.Event {
	events, err := p.s.K.ListEvents(ctx, kernel.ListFilter{Kind: "execution.started", ExecutionID: ex.ID, Limit: 1})
	if err != nil || len(events) == 0 {
		return nil
	}
	return &events[0]
}

// Recover reports executions recorded as active that this process does not
// hold: they were lost to a restart, and their owners are told like any other
// outcome so nothing waits on them forever.
func (p *WorkerPool) Recover(ctx context.Context) (int, error) {
	active, err := p.s.K.ActiveExecutions(ctx)
	if err != nil {
		return 0, err
	}
	lost := 0
	for _, ex := range active {
		p.mu.Lock()
		_, held := p.jobs[ex.ID]
		p.mu.Unlock()
		if held {
			continue
		}
		lost++
		var item *kernel.WorkItem
		if it, err := p.s.K.GetWorkItem(ctx, ex.WorkItemID); err == nil {
			item = &it
		}
		if err := p.reportOutcome(ctx, ex.ID, ex.WorkItemID, ex.Owner, p.startedEventOf(ctx, ex), item, runOutcome{
			Result: agentcli.Result{Error: "lost: the runtime restarted while this execution was active"}, Lost: true,
		}); err != nil {
			return lost, err
		}
	}
	return lost, nil
}

// CancelTree stops a work item and everything under it — the cancel flows
// down the tree. The reason names the source: a person, a deadline, a budget.
func (p *WorkerPool) CancelTree(ctx context.Context, workItemID, reason, source string) ([]string, error) {
	k := p.s.K
	nodes, err := k.Subtree(ctx, workItemID)
	if err != nil {
		return nil, err
	}
	cancelled := []string{}
	for _, node := range nodes {
		ex, ok, err := k.ActiveExecutionFor(ctx, node.ID)
		if err != nil {
			return cancelled, err
		}
		if !ok {
			continue
		}
		payload := kernel.Payload{"reason": reason}
		if node.ID != workItemID {
			payload["via"] = workItemID
		}
		// Intent before the effect: otherwise the stop lands after the execution it stopped.
		if _, err := k.Append(ctx, kernel.EventInput{Source: source, Kind: "execution.cancelled", ThreadID: node.ThreadID,
			WorkItemID: node.ID, ExecutionID: ex.ID, Payload: payload}); err != nil {
			return cancelled, err
		}
		p.mu.Lock()
		j := p.jobs[ex.ID]
		isRunning := p.running[ex.ID] != nil
		if !isRunning && j != nil {
			delete(p.jobs, ex.ID)
			for i, x := range p.order {
				if x == ex.ID {
					p.order = append(p.order[:i], p.order[i+1:]...)
					break
				}
			}
		}
		p.mu.Unlock()
		if isRunning && j != nil {
			// The worker stops and its own completion reports the cancellation.
			j.mu.Lock()
			j.cancelled = true
			run := j.run
			j.mu.Unlock()
			if run != nil {
				run.Cancel()
			}
		} else {
			var started *kernel.Event
			if j != nil {
				started = &j.started
			} else {
				started = p.startedEventOf(ctx, ex)
			}
			n := node
			if err := p.reportOutcome(ctx, ex.ID, node.ID, ex.Owner, started, &n,
				runOutcome{Result: agentcli.Result{Error: "cancelled", Cancelled: true}}); err != nil {
				return cancelled, err
			}
		}
		cancelled = append(cancelled, node.ID)
	}
	return cancelled, nil
}

// Status counts running and queued jobs.
func (p *WorkerPool) Status() map[string]int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return map[string]int{"running": len(p.running), "queued": len(p.jobs) - len(p.running)}
}
