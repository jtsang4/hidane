package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jtsang4/hidane/internal/clip"
	"github.com/jtsang4/hidane/internal/kernel"
	"github.com/jtsang4/hidane/internal/repos"
)

func describeManager(m kernel.Event, children []kernel.WorkItem) string {
	p := m.Payload
	switch m.Kind {
	case "user.message":
		tags := ""
		if p.Str("answers") != "" {
			tags += ", answering your escalated question"
		}
		if p.Bool("deferred") {
			tags += ", recorded earlier"
		}
		if others := alsoToOf(p); len(others) > 0 {
			tags += ", also sent to " + strings.Join(others, ", ") + " — act only on what is meant for this work item"
		}
		line := fmt.Sprintf("[%s] (person%s) %s", m.ID, tags, p.Str("text"))
		if c := p.Str("context"); c != "" {
			line += "\n(context, not said by the person) " + c
		}
		return line
	case "execution.finished":
		status := "failed"
		switch {
		case p.Bool("cancelled"):
			status = "cancelled"
		case p.Bool("lost"):
			status = "lost (runtime restarted)"
		case p.Bool("ok") && p.Str("blocked") != "":
			status = "blocked"
		case p.Bool("ok"):
			status = "ok"
		}
		lines := []string{fmt.Sprintf("[%s] (worker result %s: %s)", m.ID, m.ExecutionID, status)}
		if b := p.Str("blocked"); b != "" {
			lines = append(lines, "blocked on: "+b)
		}
		if e := p.Str("error"); e != "" {
			lines = append(lines, "error: "+clip.Runes(e, 1000))
		}
		if blocks, ok := p["policyBlocks"].([]any); ok && len(blocks) > 0 {
			var reasons []string
			for _, b := range blocks {
				if mb, ok := b.(map[string]any); ok {
					reasons = append(reasons, fmt.Sprint(mb["reason"]))
				}
			}
			lines = append(lines, "refused by policy: "+strings.Join(reasons, "; "))
		}
		lines = append(lines, "summary:\n"+clip.Noted(p.Str("summary"), 20000))
		return strings.Join(lines, "\n")
	case "escalation.raised":
		title := ""
		for _, c := range children {
			if c.ID == m.WorkItemID {
				title = fmt.Sprintf(" %q", c.Title)
			}
		}
		line := fmt.Sprintf("[%s] (question escalated by child %s%s) %s", m.ID, m.WorkItemID, title, p.Str("question"))
		if opts, ok := p["options"].([]any); ok && len(opts) > 0 {
			var names []string
			for _, o := range opts {
				names = append(names, fmt.Sprint(o))
			}
			line += "\noptions offered: " + strings.Join(names, " | ")
		}
		return line
	case "children.settled":
		var lines []string
		if list, ok := p["children"].([]any); ok {
			for _, c := range list {
				if cm, ok := c.(map[string]any); ok {
					lines = append(lines, fmt.Sprintf("- %v %q [%v]: %v", cm["workItemId"], fmt.Sprint(cm["title"]), cm["status"], cm["result"]))
					if branches, ok := cm["branches"].([]any); ok && len(branches) > 0 {
						var bs []string
						for _, b := range branches {
							bs = append(bs, fmt.Sprint(b))
						}
						lines = append(lines, "  its branches: "+strings.Join(bs, ", "))
					}
				}
			}
		}
		return fmt.Sprintf("[%s] (all child work items finished)\n%s", m.ID, strings.Join(lines, "\n"))
	case "message.reroute_requested":
		return fmt.Sprintf("[%s] (child %s says this message is not theirs) %s", m.ID, m.WorkItemID, p.Str("text"))
	}
	b, _ := json.Marshal(p)
	return fmt.Sprintf("[%s] (%s) %s", m.ID, m.Kind, clip.Runes(string(b), 500))
}

// alsoToOf names the other work items a message was sent to at the same time.
func alsoToOf(p kernel.Payload) []string {
	var out []string
	if list, ok := p["alsoTo"].([]any); ok {
		for _, raw := range list {
			if o, ok := raw.(map[string]any); ok {
				out = append(out, fmt.Sprintf("%v %q", o["workItemId"], fmt.Sprint(o["title"])))
			}
		}
	}
	return out
}

// options are the answers a question offers to pick from; the person may still
// answer in their own words.
func options(raw any) []any {
	list, _ := raw.([]any)
	var out []any
	seen := map[string]bool{}
	for _, v := range list {
		s := clip.Runes(strings.Join(strings.Fields(Str(v)), " "), 80)
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
		if len(out) == 5 {
			break
		}
	}
	if len(out) < 2 {
		return nil
	}
	return out
}

// latest decides the chain position of everything a turn emits.
func latest(messages []kernel.Event) kernel.Event {
	out := messages[0]
	for _, m := range messages[1:] {
		if m.Hop > out.Hop {
			out = m
		}
	}
	return out
}

// Effects that move a work item forward or answer someone.
var managerActions = map[string]bool{"spawn": true, "reply": true, "escalate": true, "create_children": true, "reroute": true, "answer": true, "done": true}

type managerTurn struct {
	s     *System
	item  kernel.WorkItem
	batch []kernel.Event
	cause kernel.Event
	// refused: a budget stopped this turn's dispatch.
	refused bool
	// worked: the Manager changed things with its own tools this turn.
	worked bool
}

func (t *managerTurn) reply(ctx context.Context, text string, of *kernel.Event) error {
	anchor := t.cause
	if of != nil {
		anchor = *of
	}
	payload := kernel.Payload{"text": clip.Noted(text, maxAnswerRunes), "of": anchor.ID, "root": kernel.RootOf(anchor)}
	t.s.originOf(ctx, kernel.RootOf(anchor), payload)
	// The work reporting back — a worker's outcome, children settling, or
	// changes this Manager made itself — rather than an answer to what the
	// person just asked: the conversation shows it as one line and the task
	// waits for the person to review it. Stopping a run they asked to stop is
	// only an acknowledgement.
	if t.worked || (anchor.Kind != "user.message" && !(anchor.Kind == "execution.finished" && anchor.Payload.Bool("cancelled"))) {
		payload["report"] = true
	}
	// A child's answer is for its parent; the person reads the parent's summary.
	if t.item.Parent() != "" {
		payload["child"] = true
	}
	_, err := t.s.K.Append(ctx, kernel.EventInput{Source: "agent:manager", Kind: "agent.reply", ThreadID: t.item.ThreadID,
		WorkItemID: t.item.ID, CausedBy: anchor.ID, Hop: anchor.Hop + 1, Payload: payload})
	return err
}

// bubble sends a fact up one level of the work tree.
func (t *managerTurn) bubble(ctx context.Context, kind string, payload kernel.Payload) error {
	if !kernel.Bubbles(kind) {
		return fmt.Errorf("%s does not bubble", kind)
	}
	payload["root"] = kernel.RootOf(t.cause)
	_, _, err := t.s.K.Post(ctx, kernel.PostInput{EventInput: kernel.EventInput{Source: "agent:manager", Kind: kind,
		Mailbox: ParentMailbox(t.item), Lane: kernel.LaneNormal, ThreadID: t.item.ThreadID, WorkItemID: t.item.ID, Payload: payload},
		CausedBy: &t.cause})
	return err
}

func (t *managerTurn) find(of any) *kernel.Event {
	id, _ := of.(string)
	for i := range t.batch {
		if t.batch[i].ID == id {
			return &t.batch[i]
		}
	}
	return nil
}

func (t *managerTurn) apply(ctx context.Context, e Effect, spawned *bool) error {
	s, k, item := t.s, t.s.K, t.item
	switch Str(e["type"]) {
	case "understanding":
		text := Str(e["text"])
		if text == "" {
			return nil
		}
		// The file is what agents read (workers see it in their cwd); the event
		// is what the card shows. Neither is derived from the other.
		_ = os.MkdirAll(item.Workspace, 0o755)
		if err := os.WriteFile(filepath.Join(item.Workspace, "TASK.md"), []byte(fmt.Sprintf("# %s\n\n%s\n", item.Title, text)), 0o644); err != nil {
			return err
		}
		_, err := k.Append(ctx, kernel.EventInput{Source: "agent:manager", Kind: "work_item.understanding", ThreadID: item.ThreadID,
			WorkItemID: item.ID, CausedBy: t.cause.ID, Payload: kernel.Payload{"text": text, "root": kernel.RootOf(t.cause)}})
		return err
	case "reply":
		return t.reply(ctx, Str(e["reply"]), t.find(e["of"]))
	case "spawn":
		instructions := Str(e["instructions"])
		if instructions == "" || *spawned {
			return nil
		}
		*spawned = true
		// The budget is per stretch of autonomous work: whoever answers this
		// item (the person, or a parent) starts a new one — which is what the
		// escalation below promises.
		n, err := k.CountExecutionsSinceInput(ctx, item.ID)
		if err != nil {
			return err
		}
		if n >= k.Cfg.MaxExecutionsPerItem {
			t.refused = true
			_, err := k.Append(ctx, kernel.EventInput{Source: "agent:manager", Kind: "escalation", ThreadID: "main", WorkItemID: item.ID,
				CausedBy: t.cause.ID, Payload: kernel.Payload{"reason": "budget", "limit": "executions", "count": n, "root": kernel.RootOf(t.cause),
					"question": fmt.Sprintf("「%s」自上次回复以来已经执行了 %d 次，已暂停。需要继续的话，直接回复这个任务。", item.Title, n)}})
			return err
		}
		err = s.Pool.Dispatch(ctx, item, kernel.ManagerAddress(item.ID), instructions, Str(e["expect"]), t.cause)
		if errors.Is(err, ErrBudget) {
			t.refused = true
			return nil
		}
		return err
	case "escalate":
		question := Str(e["question"])
		if question == "" {
			return nil
		}
		var path []any
		for _, m := range t.batch {
			if m.Kind == "escalation.raised" {
				if p, ok := m.Payload["path"].([]any); ok {
					path = append(path, p...)
				}
				break
			}
		}
		path = append(path, map[string]any{"workItemId": item.ID, "title": item.Title, "tried": Str(e["tried"])})
		payload := kernel.Payload{"question": question, "path": path}
		if opts := options(e["options"]); opts != nil {
			payload["options"] = opts
		}
		return t.bubble(ctx, "escalation.raised", payload)
	case "answer":
		childID, text := Str(e["work_item_id"]), Str(e["text"])
		if childID == "" || text == "" {
			return nil
		}
		children, err := k.ListChildren(ctx, item.ID)
		if err != nil {
			return err
		}
		for _, c := range children {
			if c.ID == childID {
				_, _, err := k.Post(ctx, kernel.PostInput{EventInput: kernel.EventInput{Source: "agent:manager", Kind: "user.message",
					Mailbox: kernel.ManagerAddress(c.ID), Lane: kernel.LaneInterrupt, ThreadID: c.ThreadID, WorkItemID: c.ID,
					Payload: kernel.Payload{"text": text, "root": kernel.RootOf(t.cause), "fromParent": item.ID}}, CausedBy: &t.cause})
				return err
			}
		}
		return nil
	case "attach_repo":
		ref := Str(e["repo"])
		if strings.TrimSpace(ref) == "" {
			return nil
		}
		inPlace, _ := e["in_place"].(bool)
		var said []string
		for _, m := range t.batch {
			if m.Kind == "user.message" {
				said = append(said, m.Payload.Str("text"))
			}
		}
		asked := heldToWords([]RepoRequest{{Ref: ref, Asked: Str(e["asked"]), Spec: repos.AttachSpec{Base: Str(e["base"]), InPlace: inPlace}}}, strings.Join(said, "\n"))
		wanted, problem := s.resolveRepos(ctx, asked, "agent:manager")
		if problem == "" {
			if err := s.attachAll(ctx, item, wanted, "agent:manager"); err != nil {
				problem = fmt.Sprintf("没能为这个任务准备仓库：%v", err)
			}
		}
		if problem != "" {
			// What the person must answer, not a silent failure the next spawn trips over.
			return t.reply(ctx, problem, nil)
		}
		return nil
	case "create_children":
		list, _ := e["children"].([]any)
		if len(list) > 8 {
			list = list[:8]
		}
		for i, raw := range list {
			c, _ := raw.(map[string]any)
			title, brief := Str(c["title"]), Str(c["brief"])
			if strings.TrimSpace(title) == "" || strings.TrimSpace(brief) == "" {
				// A part that is dropped must not vanish without a trace — and
				// the note must say which one, for the person and for this
				// Manager's next turn (it reads these back in its history).
				missing := "title"
				if strings.TrimSpace(title) != "" {
					missing = "brief"
				}
				name := clip.Runes(title, 60)
				if name == "" {
					name = clip.Runes(brief, 60)
				}
				if _, err := k.Append(ctx, kernel.EventInput{Source: "agent:manager", Kind: "agent.error", ThreadID: item.ThreadID,
					WorkItemID: item.ID, CausedBy: t.cause.ID, Payload: kernel.Payload{
						"error": fmt.Sprintf("child #%d of %d (%q) was not created: it has no %s", i+1, len(list), name, missing),
						"index": i + 1, "title": title, "brief": clip.Runes(brief, 200), "root": kernel.RootOf(t.cause)}}); err != nil {
					return err
				}
				continue
			}
			child, err := k.CreateWorkItem(ctx, title, "agent:manager", kernel.CreateWorkItemOpts{ParentID: item.ID})
			if err != nil {
				return err
			}
			// The parts of a task run on what the task runs on, in the repos it works in.
			if item.RunAs != nil {
				if child, err = k.SetWorkItemRunAs(ctx, child.ID, item.RunAs, "agent:manager"); err != nil {
					return err
				}
			}
			var only []string
			if names, present := c["repos"]; present {
				only = []string{}
				if list, ok := names.([]any); ok {
					for _, n := range list {
						only = append(only, Str(n))
					}
				}
			}
			if _, errs := s.Repos.Inherit(ctx, item, child, only, "agent:manager"); len(errs) > 0 {
				var msgs []string
				for _, e := range errs {
					msgs = append(msgs, e.Error())
				}
				if _, err := k.Append(ctx, kernel.EventInput{Source: "agent:manager", Kind: "agent.error", ThreadID: item.ThreadID,
					WorkItemID: item.ID, CausedBy: t.cause.ID, Payload: kernel.Payload{
						"error": fmt.Sprintf("child %s (%q) did not get every repository: %s", child.ID, title, strings.Join(msgs, "; ")),
						"root":  kernel.RootOf(t.cause)}}); err != nil {
					return err
				}
			}
			if _, _, err := k.Post(ctx, kernel.PostInput{EventInput: kernel.EventInput{Source: "agent:manager", Kind: "user.message",
				Mailbox: kernel.ManagerAddress(child.ID), Lane: kernel.LaneNormal, ThreadID: child.ThreadID, WorkItemID: child.ID,
				Payload: kernel.Payload{"text": brief, "root": kernel.RootOf(t.cause), "fromParent": item.ID}}, CausedBy: &t.cause}); err != nil {
				return err
			}
		}
		return nil
	case "reroute":
		msg := t.find(e["of"])
		if msg == nil {
			for i := range t.batch {
				if t.batch[i].Kind == "user.message" {
					msg = &t.batch[i]
					break
				}
			}
		}
		if msg == nil {
			return nil
		}
		// A message the person addressed to several work items at once is
		// theirs to split, not this Manager's to send elsewhere.
		if len(alsoToOf(msg.Payload)) > 0 {
			return nil
		}
		of := msg.Payload.Str("of")
		if of == "" {
			of = msg.ID
		}
		return t.bubble(ctx, "message.reroute_requested", kernel.Payload{"of": of, "text": msg.Payload["text"], "exclude": []any{item.ID}})
	case "done":
		if item.Parent() == "" {
			return nil
		}
		_, err := s.ChangeStatus(ctx, item.ID, kernel.StatusDone, "agent:manager", &t.cause)
		return err
	}
	return nil
}

// steeredWords are the person's words handed to each finished execution while
// it ran (given), and those that came too late to be given to it (late): the
// latter ride with its outcome instead. Keyed by execution.
func (s *System) steeredWords(ctx context.Context, batch []kernel.Event) (given, late map[string][]string, err error) {
	given, late = map[string][]string{}, map[string][]string{}
	for _, m := range batch {
		if m.Kind != "execution.finished" || m.ExecutionID == "" {
			continue
		}
		steered, err := s.K.ListEvents(ctx, kernel.ListFilter{Kind: "execution.steered", ExecutionID: m.ExecutionID, Limit: 50})
		if err != nil {
			return nil, nil, err
		}
		for _, e := range steered {
			text := e.Payload.Str("text")
			switch {
			case e.Payload.Bool("late"):
				late[m.ExecutionID] = append(late[m.ExecutionID], text)
			case text != "":
				given[m.ExecutionID] = append(given[m.ExecutionID], text)
			}
		}
	}
	return given, late, nil
}

type managerSession struct {
	Agent     string `json:"agent"`
	SessionID string `json:"sessionId"`
}

func managerSessionPath(item kernel.WorkItem) string {
	return filepath.Join(item.Workspace, ".hidane", "sessions", "manager", "session.json")
}

// ManagerTurn: rules first. While its worker runs, the person's words are
// steered straight into that worker (no model call) or, if it is already
// ending, kept for the turn that reads its outcome; a cancelled run is
// acknowledged without one. Everything else is one model call that ends by
// dispatching, never by waiting.
func (s *System) ManagerTurn(ctx context.Context, address string, messages []kernel.Event) error {
	k := s.K
	item, err := k.GetWorkItem(ctx, kernel.WorkItemIDOf(address))
	if err != nil {
		return err
	}
	active, hasActive, err := k.ActiveExecutionFor(ctx, item.ID)
	if err != nil {
		return err
	}
	var personal, others []kernel.Event
	for _, m := range messages {
		if m.Kind == "user.message" {
			personal = append(personal, m)
		} else {
			others = append(others, m)
		}
	}
	if hasActive && len(others) == 0 && len(personal) > 0 {
		for _, m := range personal {
			text := m.Payload.Str("text")
			d := s.Pool.Deliver(item.ID, text)
			// `of` names the person's own message, so hiding it hides this copy too.
			of := m.Payload.Str("of")
			if of == "" {
				of = m.ID
			}
			// Missed: the run is ending. Its outcome is on the way to this
			// mailbox, and the turn that reads it is given these words too.
			payload := kernel.Payload{"text": text, "of": of, "root": kernel.RootOf(m), "queued": d == Amended, "late": d == Missed}
			if _, err := k.Append(ctx, kernel.EventInput{Source: "agent:manager", Kind: "execution.steered", ThreadID: item.ThreadID,
				WorkItemID: item.ID, ExecutionID: active.ID, CausedBy: m.ID, Payload: payload}); err != nil {
				return err
			}
		}
		return nil
	}
	given, late, err := s.steeredWords(ctx, others)
	if err != nil {
		return err
	}
	cause := latest(messages)
	t := &managerTurn{s: s, item: item, batch: messages, cause: cause}
	onlyCancelled := len(personal) == 0 && len(others) > 0
	for _, m := range others {
		if m.Kind != "execution.finished" || !m.Payload.Bool("cancelled") {
			onlyCancelled = false
		}
	}
	if onlyCancelled && len(late) == 0 {
		return t.reply(ctx, "执行已取消。", nil)
	}
	children, err := k.ListChildren(ctx, item.ID)
	if err != nil {
		return err
	}
	history, err := k.ListEvents(ctx, kernel.ListFilter{WorkItemID: item.ID, Tail: 40})
	if err != nil {
		return err
	}
	inBatch := map[string]bool{}
	for _, m := range messages {
		inBatch[m.ID] = true
	}
	var hist []string
	for _, e := range history {
		if inBatch[e.ID] {
			continue
		}
		if (e.Kind == "user.message" && e.ThreadID != "main") || e.Kind == "agent.reply" || e.Kind == "work_item.understanding" {
			hist = append(hist, fmt.Sprintf("[%s] %s", e.Kind, clip.Runes(e.Payload.Str("text"), 600)))
		}
		if e.Kind == "agent.error" && e.Source == "agent:manager" {
			hist = append(hist, fmt.Sprintf("[agent.error] %s", clip.Runes(e.Payload.Str("error"), 600)))
		}
	}
	if len(hist) > 14 {
		hist = hist[len(hist)-14:]
	}
	parentLine := ""
	if p := item.Parent(); p != "" {
		if parent, err := k.GetWorkItem(ctx, p); err == nil {
			parentLine = fmt.Sprintf("Parent work item: %s — %s. You are a CHILD work item.", parent.ID, parent.Title)
		}
	}
	answeredLine := ""
	for _, m := range messages {
		if m.Kind == "user.message" && m.Payload.Str("answers") != "" {
			if q, ok, _ := k.GetEvent(ctx, m.Payload.Str("answers")); ok {
				answeredLine = "Question you escalated earlier: " + q.Payload.Str("question")
			}
			break
		}
	}
	held, err := s.checkoutsOf(ctx, item.ID, "")
	if err != nil {
		return err
	}
	repoLines := ""
	if l := held.long(); l != "" {
		repoLines = "Repositories this work item works in (each worker is told these too):\n" + l
	}
	childLines := ""
	if len(children) > 0 {
		var cl []string
		for _, c := range children {
			cl = append(cl, fmt.Sprintf("- %s [%s] %s", c.ID, c.Status, c.Title))
		}
		childLines = "Child work items:\n" + strings.Join(cl, "\n")
	}
	activeLine := ""
	if hasActive {
		activeLine = fmt.Sprintf("An execution (%s) is %s; do not spawn another until it reports.", active.ID, active.Status)
	}
	var described []string
	for _, m := range messages {
		d := describeManager(m, children)
		if words := given[m.ExecutionID]; m.Kind == "execution.finished" && len(words) > 0 {
			// Unsaid, the words read as a request still open: a result that had
			// already carried them out was once handed to a second worker.
			d += "\nThe person said this while the run was going and the worker received it; the result above should account for it:\n- " + strings.Join(words, "\n- ")
		}
		if words := late[m.ExecutionID]; m.Kind == "execution.finished" && len(words) > 0 {
			d += "\nThe person said this while the run was ending; the worker never saw it:\n- " + strings.Join(words, "\n- ")
		}
		described = append(described, d)
	}
	historyBlock := ""
	if len(hist) > 0 {
		historyBlock = "Earlier in this work item:\n" + strings.Join(hist, "\n")
	}
	prompt := joinNonEmpty([]string{
		kernel.NowLine(k.Now()),
		s.RecallForManager(item),
		fmt.Sprintf("Work item: %s — %s (status: %s)", item.ID, item.Title, item.Status),
		parentLine, childLines,
		"Workspace: " + item.Workspace,
		repoLines,
		"The person's registered repositories (for attach_repo):\n" + s.repoInventory(ctx),
		activeLine, answeredLine, historyBlock,
		"Messages this turn:\n" + strings.Join(described, "\n\n"),
	})

	sessionPath := managerSessionPath(item)
	agent := s.Settings.Get().ResolveWith("manager", ownRun(item)).Agent
	var saved managerSession
	if b, err := os.ReadFile(sessionPath); err == nil {
		_ = json.Unmarshal(b, &saved)
	}
	opts := thinkOpts{Role: "manager", Own: ownRun(item), Charter: ManagerCharter, Cwd: item.Workspace,
		Tools: true, PolicyFiles: k.PolicyFilesFor(ctx, item),
		Trail:      kernel.EventInput{Source: "agent:manager", ThreadID: item.ThreadID, WorkItemID: item.ID, CausedBy: cause.ID},
		SessionDir: filepath.Dir(sessionPath), Images: imagesOf(messages), LiveThreadID: item.ThreadID}
	if saved.Agent == agent {
		opts.ResumeID = saved.SessionID
	}
	thought := s.think(ctx, prompt, opts)
	if !thought.OK && !thought.Aborted && opts.ResumeID != "" {
		// A session the CLI no longer has must not wedge the work item.
		opts.ResumeID = ""
		thought = s.think(ctx, prompt, opts)
	}
	if thought.Aborted {
		return ctx.Err()
	}
	t.worked = thought.Changed
	if thought.SessionID != "" {
		_ = os.MkdirAll(filepath.Dir(sessionPath), 0o755)
		b, _ := json.Marshal(managerSession{Agent: agent, SessionID: thought.SessionID})
		_ = os.WriteFile(sessionPath, b, 0o644)
	}
	record := func(th Thought, nudged bool) error {
		payload := th.decision(messages)
		if nudged {
			payload["nudged"] = true
		}
		_, err := k.Append(ctx, kernel.EventInput{Source: "agent:manager", Kind: "manager.decision", ThreadID: item.ThreadID,
			WorkItemID: item.ID, CausedBy: cause.ID, Payload: payload})
		return err
	}
	if err := record(thought, false); err != nil {
		return err
	}
	if !thought.OK {
		return t.reply(ctx, "manager planning failed: "+thought.Error, nil)
	}
	effects := thought.Effects
	hasAction := func(list []Effect) bool {
		for _, e := range list {
			if managerActions[Str(e["type"])] {
				return true
			}
		}
		return false
	}
	// A turn that only restates its understanding — or is not the effect list
	// at all — leaves the person waiting on a task that silently went idle.
	// Ask once for the missing decision. (Output that was not the effect list
	// was once posted as the answer: "response." reached the person.)
	if !hasAction(effects) {
		retryOpts := opts
		retryOpts.ResumeID = thought.SessionID
		retryOpts.Images = nil
		nudge := "Your answer contained no action, so the person would get no response. Respond again with the full effect list, including at least one of: spawn, reply, escalate, create_children, reroute, answer, done."
		if thought.SessionID == "" {
			nudge = prompt + "\n\n" + nudge
		}
		retry := s.think(ctx, nudge, retryOpts)
		if retry.Aborted {
			return ctx.Err()
		}
		t.worked = t.worked || retry.Changed
		// The follow-up is its own decision: what was decided must be on record.
		if err := record(retry, true); err != nil {
			return err
		}
		if retry.OK && hasAction(retry.Effects) {
			for _, e := range retry.Effects {
				if Str(e["type"]) != "understanding" {
					effects = append(effects, e)
				}
			}
		}
	}
	if !hasAction(effects) && thought.Effects == nil && strings.TrimSpace(thought.Raw) != "" {
		// Twice not the effect list: the text is the only answer there is.
		return t.reply(ctx, thought.Raw, nil)
	}
	// Replies come after everything else: one written as if its dispatch
	// happened must not reach the person when a budget refused that dispatch —
	// they get the escalation instead (a reply once announced a worker that
	// never ran). `done` comes after the replies: closing a child tells its
	// parent, with the child's last reply as its result, so that reply must
	// already be written.
	spawned := false
	for _, phase := range []func(string) bool{
		// A repo attached in this turn is where this turn's worker runs.
		func(kind string) bool { return kind == "attach_repo" },
		func(kind string) bool { return kind != "reply" && kind != "done" && kind != "attach_repo" },
		func(kind string) bool { return kind == "reply" },
		func(kind string) bool { return kind == "done" },
	} {
		for _, e := range effects {
			if !phase(Str(e["type"])) {
				continue
			}
			if err := t.apply(ctx, e, &spawned); err != nil {
				return err
			}
		}
		if t.refused {
			return nil
		}
	}
	return nil
}
