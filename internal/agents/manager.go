package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jtsang4/hidane/internal/kernel"
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
		return fmt.Sprintf("[%s] (person%s) %s", m.ID, tags, p.Str("text"))
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
			lines = append(lines, "error: "+clipRunes(e, 1000))
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
		lines = append(lines, "summary:\n"+clipRunes(p.Str("summary"), 6000))
		return strings.Join(lines, "\n")
	case "escalation.raised":
		title := ""
		for _, c := range children {
			if c.ID == m.WorkItemID {
				title = fmt.Sprintf(" %q", c.Title)
			}
		}
		return fmt.Sprintf("[%s] (question escalated by child %s%s) %s", m.ID, m.WorkItemID, title, p.Str("question"))
	case "children.settled":
		var lines []string
		if list, ok := p["children"].([]any); ok {
			for _, c := range list {
				if cm, ok := c.(map[string]any); ok {
					lines = append(lines, fmt.Sprintf("- %v %q [%v]: %v", cm["workItemId"], fmt.Sprint(cm["title"]), cm["status"], cm["result"]))
				}
			}
		}
		return fmt.Sprintf("[%s] (all child work items finished)\n%s", m.ID, strings.Join(lines, "\n"))
	case "message.reroute_requested":
		return fmt.Sprintf("[%s] (child %s says this message is not theirs) %s", m.ID, m.WorkItemID, p.Str("text"))
	}
	b, _ := json.Marshal(p)
	return fmt.Sprintf("[%s] (%s) %s", m.ID, m.Kind, clipRunes(string(b), 500))
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
}

func (t *managerTurn) reply(ctx context.Context, text string, of *kernel.Event) error {
	anchor := t.cause
	if of != nil {
		anchor = *of
	}
	payload := kernel.Payload{"text": clipRunes(text, 8000), "of": anchor.ID, "root": kernel.RootOf(anchor)}
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
		n, err := k.CountExecutions(ctx, item.ID)
		if err != nil {
			return err
		}
		if n >= k.Cfg.MaxExecutionsPerItem {
			_, err := k.Append(ctx, kernel.EventInput{Source: "agent:manager", Kind: "escalation", ThreadID: "main", WorkItemID: item.ID,
				CausedBy: t.cause.ID, Payload: kernel.Payload{"reason": "budget", "root": kernel.RootOf(t.cause),
					"question": fmt.Sprintf("「%s」已经执行了 %d 次，已暂停。需要继续的话，直接回复这个任务。", item.Title, k.Cfg.MaxExecutionsPerItem)}})
			return err
		}
		_, err = s.Pool.Dispatch(ctx, item, kernel.ManagerAddress(item.ID), instructions, Str(e["expect"]), t.cause)
		if errors.Is(err, ErrBudget) {
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
		return t.bubble(ctx, "escalation.raised", kernel.Payload{"question": question, "path": path})
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
	case "create_children":
		list, _ := e["children"].([]any)
		if len(list) > 8 {
			list = list[:8]
		}
		for _, raw := range list {
			c, _ := raw.(map[string]any)
			title, brief := Str(c["title"]), Str(c["brief"])
			if title == "" || brief == "" {
				continue
			}
			child, err := k.CreateWorkItem(ctx, title, "agent:manager", kernel.CreateWorkItemOpts{ParentID: item.ID})
			if err != nil {
				return err
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

type managerSession struct {
	Agent     string `json:"agent"`
	SessionID string `json:"sessionId"`
}

func managerSessionPath(item kernel.WorkItem) string {
	return filepath.Join(item.Workspace, ".hidane", "sessions", "manager", "session.json")
}

// ManagerTurn: rules first. While its worker runs, the person's words are
// steered straight into that worker (no model call); a cancelled run is
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
			var steered bool
			if active.Status == kernel.ExecRunning {
				steered = s.Pool.Steer(item.ID, text)
			} else {
				steered = s.Pool.AmendQueued(item.ID, text)
			}
			in := kernel.EventInput{Source: "agent:manager", ThreadID: item.ThreadID, WorkItemID: item.ID, ExecutionID: active.ID, CausedBy: m.ID}
			if steered {
				in.Kind = "execution.steered"
				in.Payload = kernel.Payload{"text": text, "of": m.ID, "root": kernel.RootOf(m), "queued": active.Status == kernel.ExecQueued}
			} else {
				in.Kind = "agent.error"
				in.Payload = kernel.Payload{"error": "could not deliver the message to the running execution", "of": m.ID, "root": kernel.RootOf(m)}
			}
			if _, err := k.Append(ctx, in); err != nil {
				return err
			}
		}
		return nil
	}
	cause := latest(messages)
	t := &managerTurn{s: s, item: item, batch: messages, cause: cause}
	onlyCancelled := len(personal) == 0 && len(others) > 0
	for _, m := range others {
		if m.Kind != "execution.finished" || !m.Payload.Bool("cancelled") {
			onlyCancelled = false
		}
	}
	if onlyCancelled {
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
			hist = append(hist, fmt.Sprintf("[%s] %s", e.Kind, clipRunes(e.Payload.Str("text"), 600)))
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
		described = append(described, describeManager(m, children))
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
		activeLine, answeredLine, historyBlock,
		"Messages this turn:\n" + strings.Join(described, "\n\n"),
	}, "\n\n")

	sessionPath := managerSessionPath(item)
	agent := s.Settings.Get().Resolve("manager").Agent
	var saved managerSession
	if b, err := os.ReadFile(sessionPath); err == nil {
		_ = json.Unmarshal(b, &saved)
	}
	opts := thinkOpts{Role: "manager", Charter: ManagerCharter, Cwd: item.Workspace,
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
	if thought.SessionID != "" {
		_ = os.MkdirAll(filepath.Dir(sessionPath), 0o755)
		b, _ := json.Marshal(managerSession{Agent: agent, SessionID: thought.SessionID})
		_ = os.WriteFile(sessionPath, b, 0o644)
	}
	var ofIDs []any
	for _, m := range messages {
		ofIDs = append(ofIDs, m.ID)
	}
	recorded := []any{}
	for _, e := range thought.Effects {
		recorded = append(recorded, map[string]any(e))
	}
	if _, err := k.Append(ctx, kernel.EventInput{Source: "agent:manager", Kind: "manager.decision", ThreadID: item.ThreadID,
		WorkItemID: item.ID, CausedBy: cause.ID,
		Payload: kernel.Payload{"ok": thought.OK, "durationMs": thought.DurationMs, "of": ofIDs, "effects": recorded}}); err != nil {
		return err
	}
	if !thought.OK {
		return t.reply(ctx, "manager planning failed: "+thought.Error, nil)
	}
	if thought.Effects == nil {
		// Not the effect JSON: the text itself is the Manager's answer.
		if strings.TrimSpace(thought.Raw) != "" {
			return t.reply(ctx, thought.Raw, nil)
		}
		return nil
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
	// A turn that only restates its understanding leaves the person waiting
	// on a task that silently went idle. Ask once for the missing decision.
	if !hasAction(effects) {
		retryOpts := opts
		retryOpts.ResumeID = thought.SessionID
		retryOpts.Images = nil
		nudge := "Your answer contained no action, so the person would get no response. Respond again with the full effect list, including at least one of: spawn, reply, escalate, create_children, reroute, answer, done."
		if thought.SessionID == "" {
			nudge = prompt + "\n\n" + nudge
		}
		retry := s.think(ctx, nudge, retryOpts)
		if retry.OK && hasAction(retry.Effects) {
			for _, e := range retry.Effects {
				if Str(e["type"]) != "understanding" {
					effects = append(effects, e)
				}
			}
		}
	}
	spawned := false
	for _, e := range effects {
		if err := t.apply(ctx, e, &spawned); err != nil {
			return err
		}
	}
	return nil
}
