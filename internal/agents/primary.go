package agents

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jtsang4/hidane/internal/kernel"
	"github.com/jtsang4/hidane/internal/projections"
)

// Message kinds the Primary's model reads; everything else is handled by rule.
var routable = map[string]bool{
	"user.message": true, "triage.decision": true, "schedule.prompt": true,
	"message.reroute_requested": true, "conversation.recalled": true,
}

// describePrimary is one line per message, tagged with its id so effects can
// say what they answer.
func describePrimary(m kernel.Event) string {
	p := m.Payload
	switch m.Kind {
	case "triage.decision":
		ofKind := p.Str("ofKind")
		if ofKind == "" {
			ofKind = "event"
		}
		return fmt.Sprintf("[%s] (external: %s) %s", m.ID, ofKind, p.Str("summary"))
	case "schedule.prompt":
		return fmt.Sprintf("[%s] (scheduled %q) %s", m.ID, p.Str("name"), p.Str("prompt"))
	case "conversation.recalled":
		return fmt.Sprintf("[%s] (recall: you searched earlier conversation for %q to answer %s; answer that message now with of=%q. Do not recall again.)\nThe message: %s\nFound:\n%s",
			m.ID, p.Str("query"), p.Str("of"), m.ID, p.Str("original"), p.Str("text"))
	case "message.reroute_requested":
		var ex []string
		if list, ok := p["exclude"].([]any); ok {
			for _, x := range list {
				ex = append(ex, fmt.Sprint(x))
			}
		}
		return fmt.Sprintf("[%s] (reroute: work item %s says this is not theirs — do not route it back there) %s", m.ID, strings.Join(ex, ", "), p.Str("text"))
	}
	images := ""
	if n, ok := p.Num("imageCount"); ok && n > 0 {
		images = fmt.Sprintf(", %d image(s) attached", int(n))
	}
	return fmt.Sprintf("[%s] (user%s) %s", m.ID, images, p.Str("text"))
}

// rootMeta says where an answer to this message belongs, and how to title it.
func rootMeta(m kernel.Event) kernel.Payload {
	meta := kernel.Payload{"of": m.ID, "root": kernel.RootOf(m)}
	switch m.Kind {
	case "conversation.recalled":
		for _, key := range []string{"rootKind", "rootText"} {
			if v, ok := m.Payload[key]; ok {
				meta[key] = v
			}
		}
	case "triage.decision":
		meta["rootKind"] = "external"
		meta["rootText"] = clipRunes(m.Payload.Str("summary"), 200)
	case "schedule.prompt":
		meta["rootKind"] = "scheduled"
		meta["rootText"] = m.Payload.Str("name")
	}
	return meta
}

// surfaceEscalation: a bubbled question that reached the top goes to the
// person as-is, with the path it took — the Primary has no better knowledge
// than the Manager that asked.
func (s *System) surfaceEscalation(ctx context.Context, m kernel.Event) error {
	path := m.Payload["path"]
	if path == nil {
		path = []any{}
	}
	_, err := s.K.Append(ctx, kernel.EventInput{
		Source: "agent:primary", Kind: "escalation", ThreadID: "main", WorkItemID: m.WorkItemID,
		CausedBy: m.ID, Hop: m.Hop + 1,
		Payload: kernel.Payload{"question": m.Payload["question"], "path": path, "of": m.ID, "root": kernel.RootOf(m), "reason": "question"},
	})
	return err
}

type primaryTurn struct {
	s     *System
	batch []kernel.Event
	all   []kernel.WorkItem
	busy  []string
	// working: items holding an active checkout, which a follow-up can still
	// reach however long ago they ended.
	working map[string]bool
	covered map[string]bool
	// confirmed: what status changes and cancels actually did, per message.
	// They answer together, once: the model's own reply would say it a
	// second time, and a stop-and-close request got two confirmations.
	confirmed map[string][]string
}

func (t *primaryTurn) message(of any) (kernel.Event, bool) {
	id, _ := of.(string)
	for _, m := range t.batch {
		if m.ID == id {
			return m, true
		}
	}
	return kernel.Event{}, false
}

func (t *primaryTurn) cover(m kernel.Event, also any) {
	t.covered[m.ID] = true
	if list, ok := also.([]any); ok {
		for _, id := range list {
			if s, ok := id.(string); ok {
				t.covered[s] = true
			}
		}
	}
}

func (t *primaryTurn) open() []kernel.WorkItem {
	var out []kernel.WorkItem
	for _, i := range t.all {
		if i.Status == kernel.StatusOpen {
			out = append(out, i)
		}
	}
	return out
}

// routable are the items a message may go to: open ones, and finished ones
// whose worktree still holds their work.
func (t *primaryTurn) routable() []kernel.WorkItem {
	var out []kernel.WorkItem
	for _, i := range t.all {
		if i.Status == kernel.StatusOpen || t.working[i.ID] {
			out = append(out, i)
		}
	}
	return out
}

func (t *primaryTurn) find(list []kernel.WorkItem, id string) (kernel.WorkItem, bool) {
	for _, i := range list {
		if i.ID == id {
			return i, true
		}
	}
	return kernel.WorkItem{}, false
}

// confirm reports what a status change or cancel actually did — the model's
// reply was written before it happened.
func (t *primaryTurn) confirm(_ context.Context, m kernel.Event, text string) error {
	if t.confirmed == nil {
		t.confirmed = map[string][]string{}
	}
	t.confirmed[m.ID] = append(t.confirmed[m.ID], text)
	return nil
}

// flushConfirmations posts each message's confirmations as one reply.
func (t *primaryTurn) flushConfirmations(ctx context.Context) error {
	for _, m := range t.batch {
		if texts := t.confirmed[m.ID]; len(texts) > 0 {
			if err := t.reply(ctx, m, strings.Join(texts, "\n")); err != nil {
				return err
			}
		}
	}
	return nil
}

func (t *primaryTurn) reply(ctx context.Context, m kernel.Event, text string) error {
	payload := rootMeta(m)
	payload["text"] = text
	_, err := t.s.K.Append(ctx, kernel.EventInput{Source: "agent:primary", Kind: "agent.reply", ThreadID: "main",
		CausedBy: m.ID, Hop: m.Hop + 1, Payload: payload})
	return err
}

// attributionSubject: a reroute decides for the original message.
func (t *primaryTurn) attributionSubject(ctx context.Context, m kernel.Event) kernel.Event {
	if m.Kind != "message.reroute_requested" {
		return m
	}
	if orig, ok, _ := t.s.K.GetEvent(ctx, m.Payload.Str("of")); ok {
		return orig
	}
	return m
}

func parseIDs(raw any) (present bool, ids []string, malformed bool) {
	if raw == nil {
		return false, nil, false
	}
	list, ok := raw.([]any)
	if !ok {
		return true, nil, true
	}
	seen := map[string]bool{}
	for _, v := range list {
		s, ok := v.(string)
		if !ok || strings.TrimSpace(s) == "" {
			malformed = true
			continue
		}
		s = strings.TrimSpace(s)
		if !seen[s] {
			seen[s] = true
			ids = append(ids, s)
		}
	}
	return true, ids, malformed
}

func (t *primaryTurn) apply(ctx context.Context, e Effect) error {
	s, k := t.s, t.s.K
	m, ok := t.message(e["of"])
	if !ok {
		return nil
	}
	typ := Str(e["type"])
	switch typ {
	case "recall":
		// Looking further back than the recent conversation. The turn does not
		// wait: findings come back as the next message in this mailbox.
		query := Str(e["query"])
		if query == "" || m.Kind == "conversation.recalled" {
			return nil
		}
		t.cover(m, e["also_of"])
		found, err := projections.SearchConversation(ctx, k, query, nil, 8)
		if err != nil {
			return err
		}
		var hits []kernel.Event
		var hitIDs []any
		for _, h := range found.Events {
			if _, inBatch := t.message(h.ID); !inBatch {
				hits = append(hits, h)
				hitIDs = append(hitIDs, h.ID)
			}
		}
		meta := rootMeta(m)
		payload := kernel.Payload{"of": m.ID, "root": meta["root"], "query": query, "original": describePrimary(m),
			"hits": hitIDs, "text": projections.DescribeRecall(hits)}
		if v, ok := meta["rootKind"]; ok {
			payload["rootKind"], payload["rootText"] = v, meta["rootText"]
		}
		_, _, err = k.Post(ctx, kernel.PostInput{EventInput: kernel.EventInput{Source: "agent:primary", Kind: "conversation.recalled",
			Mailbox: kernel.Primary, Lane: kernel.LaneInterrupt, ThreadID: "main", Payload: payload}, CausedBy: &m})
		return err

	case "reply":
		t.cover(m, e["also_of"])
		if len(t.confirmed[m.ID]) > 0 {
			return nil
		}
		return t.reply(ctx, m, Str(e["reply"]))

	case "ambiguous", "route":
		var excluded []string
		if list, ok := m.Payload["exclude"].([]any); ok {
			for _, x := range list {
				excluded = append(excluded, fmt.Sprint(x))
			}
		}
		isExcluded := func(id string) bool {
			for _, x := range excluded {
				if x == id {
					return true
				}
			}
			return false
		}
		target := Str(e["work_item_id"])
		var confidence *float64
		if c, ok := e["confidence"].(float64); ok {
			confidence = &c
		}
		// Never back to the Manager that refused it.
		if typ == "route" && target != "" && isExcluded(target) {
			return nil
		}
		item, found := t.find(t.routable(), target)
		conf := 1.0
		if confidence != nil {
			conf = *confidence
		}
		if typ == "route" && found && conf >= k.Cfg.AttributionThreshold {
			t.cover(m, e["also_of"])
			_, err := s.DeliverToWorkItem(ctx, t.attributionSubject(ctx, m), item, "model",
				DeliverOpts{Text: Str(e["message"]), Confidence: confidence, Source: "agent:primary"})
			return err
		}
		// Asked, or not sure enough: put the choice in front of the person
		// rather than guessing — a wrong guess sends work into the wrong workspace.
		var raw []any
		if typ == "route" {
			raw = []any{target, "new"}
		} else if list, ok := e["candidates"].([]any); ok {
			raw = list
		}
		var candidates []any
		for _, c := range raw {
			id, _ := c.(string)
			if id == "new" {
				candidates = append(candidates, map[string]any{"workItemId": "new", "title": ""})
				continue
			}
			if it, ok := t.find(t.routable(), id); ok && !isExcluded(it.ID) {
				candidates = append(candidates, map[string]any{"workItemId": it.ID, "title": it.Title})
			}
		}
		if len(candidates) == 0 {
			return nil
		}
		t.cover(m, e["also_of"])
		subject := t.attributionSubject(ctx, m)
		question := Str(e["reply"])
		if question == "" {
			question = "这条消息属于哪个任务？"
			if found {
				question = fmt.Sprintf("这条消息是关于「%s」的吗？", item.Title)
			}
		}
		_, err := k.Append(ctx, kernel.EventInput{Source: "agent:primary", Kind: "attribution.ambiguous", ThreadID: "main",
			CausedBy: m.ID, Hop: m.Hop + 1,
			Payload: kernel.Payload{"of": subject.ID, "root": kernel.RootOf(subject), "question": question, "candidates": candidates}})
		return err

	case "create_work_item":
		t.cover(m, e["also_of"])
		if d, present := e["dispatch"]; present {
			if _, isBool := d.(bool); !isBool {
				return t.reply(ctx, m, "无法创建工作项：dispatch 必须是布尔值。")
			}
		}
		subject := t.attributionSubject(ctx, m)
		title := Str(e["title"])
		if title == "" {
			title = clipRunes(subject.Payload.Str("text"), 60)
		}
		// Which repositories is settled before anything exists: a question
		// goes to the person instead of work starting on a guess.
		said := []string{subject.Payload.Str("text")}
		if list, ok := e["also_of"].([]any); ok {
			for _, id := range list {
				if extra, ok := t.message(id); ok {
					said = append(said, extra.Payload.Str("text"))
				}
			}
		}
		wanted := heldToWords(parseRepoRequests(e), strings.Join(said, "\n"))
		item, problem, err := s.StartWorkItem(ctx, title, "agent:primary", kernel.CreateWorkItemOpts{Of: subject.ID}, wanted)
		if err != nil {
			return err
		}
		if problem != "" {
			return t.reply(ctx, m, problem)
		}
		if r := runAsOf(subject); r != nil {
			if item, err = k.SetWorkItemRunAs(ctx, item.ID, r, "agent:primary"); err != nil {
				return err
			}
		}
		brief := Str(e["brief"])
		if brief == "" {
			brief = subject.Payload.Str("text")
		}
		if d, _ := e["dispatch"].(bool); e["dispatch"] != nil && !d {
			if _, err := k.Append(ctx, kernel.EventInput{Source: "agent:primary", Kind: "message.attributed", ThreadID: "main",
				WorkItemID: item.ID, CausedBy: m.ID,
				Payload: kernel.Payload{"of": subject.ID, "workItemId": item.ID, "title": item.Title, "by": "model", "created": true}}); err != nil {
				return err
			}
			if _, err := k.Append(ctx, kernel.EventInput{Source: "agent:primary", Kind: "user.message", ThreadID: item.ThreadID,
				WorkItemID: item.ID,
				Payload:    kernel.Payload{"text": brief, "of": subject.ID, "root": kernel.RootOf(subject), "forwardedFrom": "main", "deferred": true}}); err != nil {
				return err
			}
			return t.reply(ctx, m, fmt.Sprintf("已创建工作项 %s，状态为 open，暂未启动 Manager/Worker。", item.ID))
		}
		if _, err := s.DeliverToWorkItem(ctx, subject, item, "model", DeliverOpts{Text: brief, Created: true, Source: "agent:primary"}); err != nil {
			return err
		}
		// Later messages folded into this one reach the Manager too, so none
		// of the person's refinements is dropped.
		if list, ok := e["also_of"].([]any); ok {
			for _, id := range list {
				extra, ok := t.message(id)
				if !ok || extra.ID == m.ID {
					continue
				}
				if _, err := s.DeliverToWorkItem(ctx, extra, item, "model", DeliverOpts{Source: "agent:primary"}); err != nil {
					return err
				}
			}
		}
		return nil

	case "set_status":
		t.cover(m, nil)
		present, ids, malformed := parseIDs(e["work_item_ids"])
		allOpen, _ := e["all_open"].(bool)
		allItems, _ := e["all_items"].(bool)
		status := Str(e["status"])
		_, aoBad := e["all_open"].(bool)
		_, aiBad := e["all_items"].(bool)
		selectors := 0
		for _, b := range []bool{present, allOpen, allItems} {
			if b {
				selectors++
			}
		}
		if !kernel.ValidStatus(status) || malformed || (e["all_open"] != nil && !aoBad) || (e["all_items"] != nil && !aiBad) || selectors != 1 {
			return t.confirm(ctx, m, "无法执行工作项状态变更：需要从工作项清单中选择 ID，或使用 all_open/all_items，并指定 open、done 或 closed。")
		}
		targets := ids
		if allOpen {
			targets = nil
			for _, i := range t.open() {
				targets = append(targets, i.ID)
			}
		} else if allItems {
			targets = nil
			for _, i := range t.all {
				targets = append(targets, i.ID)
			}
		}
		var unknown []string
		for _, id := range targets {
			if _, ok := t.find(t.all, id); !ok {
				unknown = append(unknown, id)
			}
		}
		if len(unknown) > 0 {
			return t.confirm(ctx, m, fmt.Sprintf("无法变更这些工作项：%s。只能操作当前工作项清单中的 ID。", strings.Join(unknown, "、")))
		}
		// Re-read: another channel may have changed an item while the model thought.
		var current []kernel.WorkItem
		for _, id := range targets {
			it, err := k.GetWorkItem(ctx, id)
			if err != nil {
				return err
			}
			current = append(current, it)
		}
		if allOpen {
			var gone []string
			for _, it := range current {
				if it.Status != kernel.StatusOpen {
					gone = append(gone, it.ID)
				}
			}
			if len(gone) > 0 {
				return t.confirm(ctx, m, fmt.Sprintf("无法变更这些工作项：%s 已不再是开放状态。", strings.Join(gone, "、")))
			}
		}
		var changed []string
		for _, it := range current {
			if it.Status == status {
				continue
			}
			if _, err := s.ChangeStatus(ctx, it.ID, status, "agent:primary", &m); err != nil {
				return err
			}
			changed = append(changed, it.ID)
		}
		if len(changed) > 0 {
			return t.confirm(ctx, m, fmt.Sprintf("已将 %d 个工作项的状态设为 %s：%s", len(changed), status, strings.Join(changed, "、")))
		}
		return t.confirm(ctx, m, fmt.Sprintf("没有工作项需要变更（目标状态：%s）。", status))

	case "update_repo":
		t.cover(m, nil)
		r, err := s.Repos.Relocate(ctx, Str(e["repo"]), Str(e["path"]), "agent:primary")
		if err != nil {
			return t.confirm(ctx, m, RepoProblem(Str(e["path"]), err))
		}
		return t.confirm(ctx, m, fmt.Sprintf("已记下：仓库「%s」现在在 %s。", r.Name, r.Path))

	case "forget_repo":
		t.cover(m, nil)
		r, err := k.GetRepo(ctx, Str(e["repo"]))
		if err != nil {
			return t.confirm(ctx, m, "无法移除仓库：只能使用仓库清单里的 ID。")
		}
		if err := s.Repos.Forget(ctx, r.ID, "agent:primary"); err != nil {
			if errors.Is(err, kernel.ErrRepoInUse) {
				return t.confirm(ctx, m, fmt.Sprintf("仓库「%s」还有任务在用的工作树，先在任务页的「工作树」里归档它们。", r.Name))
			}
			return err
		}
		return t.confirm(ctx, m, fmt.Sprintf("已从仓库列表里移除「%s」。", r.Name))

	case "cancel":
		t.cover(m, nil)
		present, ids, malformed := parseIDs(e["work_item_ids"])
		allRunning, _ := e["all_running"].(bool)
		_, arIsBool := e["all_running"].(bool)
		n := 0
		if present {
			n++
		}
		if allRunning {
			n++
		}
		if malformed || (e["all_running"] != nil && !arIsBool) || n != 1 {
			return t.confirm(ctx, m, "无法中止执行：需要从正在运行的执行清单中选择 ID，或使用 all_running:true。")
		}
		targets := ids
		if allRunning {
			targets = t.busy
		}
		var unknown []string
		for _, id := range targets {
			if _, ok := t.find(t.all, id); !ok {
				unknown = append(unknown, id)
			}
		}
		if len(unknown) > 0 {
			return t.confirm(ctx, m, fmt.Sprintf("无法中止这些工作项：%s 不在工作项清单中。", strings.Join(unknown, "、")))
		}
		var cancelled []string
		for _, id := range targets {
			got, err := s.Pool.CancelTree(ctx, id, "cancelled from the primary agent", "agent:primary")
			if err != nil {
				return err
			}
			cancelled = append(cancelled, got...)
		}
		if len(cancelled) > 0 {
			return t.confirm(ctx, m, fmt.Sprintf("已请求中止 %d 个正在运行的执行：%s", len(cancelled), strings.Join(cancelled, "、")))
		}
		return t.confirm(ctx, m, "没有可中止的正在运行的执行。")
	}
	return nil
}

// PrimaryTurn handles every message that reached the Primary since its last
// turn, decided together. Rules first — a bubbled question goes straight to
// the person — and only what needs judgment reaches the model.
func (s *System) PrimaryTurn(ctx context.Context, _ string, messages []kernel.Event) error {
	k := s.K
	var batch []kernel.Event
	for _, m := range messages {
		if m.Kind == "escalation.raised" {
			if err := s.surfaceEscalation(ctx, m); err != nil {
				return err
			}
		} else if routable[m.Kind] {
			batch = append(batch, m)
		}
	}
	if len(batch) == 0 {
		return nil
	}
	all, err := k.ListWorkItems(ctx, "")
	if err != nil {
		return err
	}
	busy, err := k.BusyWorkItemIDs(ctx)
	if err != nil {
		return err
	}
	exclude := map[string]bool{}
	for _, m := range batch {
		exclude[kernel.RootOf(m)] = true
	}
	recentConv, err := projections.Recent(ctx, k, exclude, 0, 0)
	if err != nil {
		return err
	}
	held, err := s.checkoutsByItem(ctx)
	if err != nil {
		return err
	}
	working := map[string]bool{}
	for id, ic := range held {
		for _, c := range ic.list {
			if c.Status == kernel.CheckoutActive {
				working[id] = true
			}
		}
	}
	t := &primaryTurn{s: s, batch: batch, all: all, busy: busy, working: working, covered: map[string]bool{}}
	var openIDs []string
	for _, i := range t.routable() {
		openIDs = append(openIDs, i.ID)
	}
	understanding, err := s.latestUnderstanding(ctx, openIDs)
	if err != nil {
		return err
	}
	isBusy := map[string]bool{}
	for _, id := range busy {
		isBusy[id] = true
	}
	line := func(i kernel.WorkItem) string {
		l := fmt.Sprintf("- %s [%s]", i.ID, i.Status)
		if isBusy[i.ID] {
			l += " [running]"
		}
		if i.Parent() != "" {
			l += " (child of " + i.Parent() + ")"
		}
		l += ": " + i.Title
		if u := understanding[i.ID]; u != "" {
			l += " — " + u
		}
		if r := held[i.ID].short(); r != "" {
			l += " — " + r
		}
		return l
	}
	list := func(items []kernel.WorkItem) string {
		if len(items) == 0 {
			return "(none)"
		}
		var lines []string
		for _, i := range items {
			lines = append(lines, line(i))
		}
		return strings.Join(lines, "\n")
	}
	running := "(none)"
	if len(busy) > 0 {
		var lines []string
		for _, i := range all {
			if isBusy[i.ID] {
				lines = append(lines, fmt.Sprintf("- %s: %s", i.ID, i.Title))
			}
		}
		running = strings.Join(lines, "\n")
	}
	var described []string
	for _, m := range batch {
		described = append(described, describePrimary(m))
	}
	prompt := joinNonEmpty([]string{
		kernel.NowLine(k.Now()),
		s.RecallForPrimary(),
		"Work items you can route to (open, or finished but still holding a worktree):\n" + list(t.routable()),
		"Repositories (the person's local git repos):\n" + s.repoInventory(ctx),
		"All work items (status management):\n" + list(all),
		"Running executions (cancellation targets):\n" + running,
		recentConv.Text,
		"Messages this turn:\n" + strings.Join(described, "\n"),
	}, "\n\n")
	opts := thinkOpts{
		Role: "primary", Charter: PrimaryCharter, Cwd: roleDir(k, "primary"), SessionDir: k.Cfg.SessionsDir(),
		Images: imagesOf(batch), LiveThreadID: "main",
	}
	thought := s.think(ctx, prompt, opts)
	if thought.Aborted {
		return ctx.Err()
	}
	var ofIDs []any
	for _, m := range batch {
		ofIDs = append(ofIDs, m.ID)
	}
	record := func(th Thought, nudged bool) error {
		recorded := []any{}
		for _, e := range th.Effects {
			recorded = append(recorded, map[string]any(e))
		}
		if th.Effects == nil {
			recorded = []any{map[string]any{"type": "reply", "raw": clipRunes(th.Raw, 500)}}
		}
		payload := kernel.Payload{"ok": th.OK, "durationMs": th.DurationMs, "of": ofIDs, "effects": recorded}
		if nudged {
			payload["nudged"] = true
		}
		_, err := k.Append(ctx, kernel.EventInput{Source: "agent:primary", Kind: "route.decision", ThreadID: "main", Payload: payload})
		return err
	}
	if err := record(thought, false); err != nil {
		return err
	}
	if thought.OK && thought.Effects == nil {
		// Not the effect list — sometimes stray markup ("<reasoning_effort>5…"),
		// which once went to the person as the reply. Ask once more, as the
		// Manager does, before the text is taken as the answer.
		retry := s.think(ctx, prompt+"\n\nYour previous answer was not the JSON effect list. Answer again with only the effect list.", opts)
		if retry.Aborted {
			return ctx.Err()
		}
		if err := record(retry, true); err != nil {
			return err
		}
		if retry.OK && retry.Effects != nil {
			thought = retry
		}
	}
	if thought.Effects == nil {
		text := thought.Raw
		if !thought.OK {
			text = "primary routing failed: " + thought.Error
		}
		for _, m := range batch {
			if err := t.reply(ctx, m, text); err != nil {
				return err
			}
		}
		return nil
	}
	// Replies last, so a confirmation of what really happened can stand in for them.
	for _, last := range []bool{false, true} {
		if last {
			if err := t.flushConfirmations(ctx); err != nil {
				return err
			}
		}
		for _, e := range thought.Effects {
			if (Str(e["type"]) == "reply") != last {
				continue
			}
			if err := t.apply(ctx, e); err != nil {
				return err
			}
		}
	}
	// A message the model skipped would otherwise sit "routing…" forever.
	for _, m := range batch {
		if t.covered[m.ID] || m.Kind == "triage.decision" {
			continue
		}
		if err := t.reply(ctx, m, "这条消息没有被处理，请换个说法再试一次。"); err != nil {
			return err
		}
	}
	return nil
}

func (s *System) latestUnderstanding(ctx context.Context, ids []string) (map[string]string, error) {
	out := map[string]string{}
	if len(ids) == 0 {
		return out, nil
	}
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	events, err := s.K.ListEvents(ctx, kernel.ListFilter{Kind: "work_item.understanding", Tail: 200})
	if err != nil {
		return nil, err
	}
	for _, e := range events {
		if want[e.WorkItemID] {
			out[e.WorkItemID] = clipRunes(e.Payload.Str("text"), 200)
		}
	}
	return out, nil
}
