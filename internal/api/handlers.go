package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/jtsang4/hidane/internal/agents"
	"github.com/jtsang4/hidane/internal/guard"
	"github.com/jtsang4/hidane/internal/kernel"
	"github.com/jtsang4/hidane/internal/projections"
	"github.com/jtsang4/hidane/internal/repos"
	"github.com/jtsang4/hidane/internal/settings"
)

func (s *server) filters(r *http.Request) kernel.ListFilter {
	q := r.URL.Query()
	f := kernel.ListFilter{
		Conversation: has(r, "conversation"),
		ThreadID:     q.Get("thread"),
		WorkItemID:   q.Get("item"),
	}
	f.PersonOnly = f.Conversation && q.Get("origin") == "person"
	// `kind` accepts a comma-separated list.
	for _, k := range strings.Split(q.Get("kind"), ",") {
		if k = strings.TrimSpace(k); k != "" {
			f.Kinds = append(f.Kinds, k)
		}
	}
	return f
}

func (s *server) titled(ctx context.Context, conversation bool, events []kernel.Event) map[string]string {
	if !conversation {
		return map[string]string{}
	}
	t, err := projections.TitlesFor(ctx, s.K, events)
	if err != nil {
		return map[string]string{}
	}
	return t
}

func (s *server) events(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	f := s.filters(r)
	if has(r, "before") || has(r, "page") {
		limit := queryInt(r, "limit", 50)
		if limit > 200 {
			limit = 200
		}
		if limit < 1 {
			limit = 1
		}
		// A window around one event: how a search hit, a link or a day is
		// opened without paging through everything newer first.
		if around := q.Get("around"); around != "" {
			target, ok, err := s.K.GetEvent(ctx, around)
			if err != nil || !ok {
				writeJSON(w, http.StatusNotFound, errBody("not found"))
				return
			}
			half := limit / 2
			of := f
			of.BeforeSeq, of.Tail = &target.Seq, half+1
			older, err1 := s.K.ListEvents(ctx, of)
			nf := f
			from := target.Seq - 1
			nf.AfterSeq, nf.Limit = &from, limit-half+1
			newer, err2 := s.K.ListEvents(ctx, nf)
			if err := errors.Join(err1, err2); err != nil {
				writeJSON(w, http.StatusInternalServerError, errBody(err.Error()))
				return
			}
			hasMore := len(older) > half
			hasNewer := len(newer) > limit-half
			if hasMore {
				older = older[1:]
			}
			if hasNewer {
				newer = newer[:limit-half]
			}
			events := append(older, newer...)
			oldest, newest := ends(events)
			writeJSON(w, http.StatusOK, eventsPage{Events: nonNil(events), HasMore: hasMore, HasNewer: hasNewer, OldestSeq: oldest, NewestSeq: newest, Titles: s.titled(ctx, f.Conversation, events)})
			return
		}
		if a := q.Get("after"); a != "" {
			var after int64
			if _, err := fmt.Sscan(a, &after); err != nil || after < 0 {
				writeJSON(w, http.StatusBadRequest, errBody("after must be a seq"))
				return
			}
			af := f
			af.AfterSeq, af.Limit = &after, limit+1
			page, err := s.K.ListEvents(ctx, af)
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, errBody(err.Error()))
				return
			}
			hasNewer := len(page) > limit
			if hasNewer {
				page = page[:limit]
			}
			oldest, newest := ends(page)
			writeJSON(w, http.StatusOK, eventsPage{Events: nonNil(page), HasMore: true, HasNewer: hasNewer, OldestSeq: oldest, NewestSeq: newest, Titles: s.titled(ctx, f.Conversation, page)})
			return
		}
		bf := f
		bf.Tail = limit + 1
		if b := q.Get("before"); b != "" {
			var before int64
			if _, err := fmt.Sscan(b, &before); err == nil {
				bf.BeforeSeq = &before
			}
		}
		page, err := s.K.ListEvents(ctx, bf)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errBody(err.Error()))
			return
		}
		hasMore := len(page) > limit
		if hasMore {
			page = page[len(page)-limit:]
		}
		oldest, newest := ends(page)
		writeJSON(w, http.StatusOK, eventsPage{Events: nonNil(page), HasMore: hasMore, OldestSeq: oldest, NewestSeq: newest, Titles: s.titled(ctx, f.Conversation, page)})
		return
	}
	if a := q.Get("after"); a != "" {
		var after int64
		fmt.Sscan(a, &after)
		f.AfterSeq = &after
	}
	f.Tail = queryInt(r, "tail", 0)
	f.Limit = queryInt(r, "limit", 0)
	events, err := s.K.ListEvents(ctx, f)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": nonNil(events)})
}

func (s *server) workItems(w http.ResponseWriter, r *http.Request) {
	status := kernel.StatusOpen
	if has(r, "all") {
		status = ""
	}
	items, err := s.K.ListWorkItems(r.Context(), status)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody(err.Error()))
		return
	}
	// Busy comes from the executions table: durable, not inferred from a
	// window of recent events that a long run pushes out.
	busy, _ := s.K.BusyWorkItemIDs(r.Context())
	isBusy := map[string]bool{}
	for _, id := range busy {
		isBusy[id] = true
	}
	running := []string{}
	for _, it := range items {
		if isBusy[it.ID] {
			running = append(running, it.ID)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "running": running})
}

func (s *server) workItem(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	item, err := s.K.GetWorkItem(ctx, r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, errBody("not found"))
		return
	}
	limit := queryInt(r, "limit", 200)
	if limit > 500 {
		limit = 500
	}
	page, err := s.K.ListEvents(ctx, kernel.ListFilter{WorkItemID: item.ID, Tail: limit + 1})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody(err.Error()))
		return
	}
	hasMore := len(page) > limit
	if hasMore {
		page = page[len(page)-limit:]
	}
	ex, running, _ := s.K.ActiveExecutionFor(ctx, item.ID)
	children, _ := s.K.ListChildren(ctx, item.ID)
	var execution any
	if running {
		execution = ex
	}
	held, _ := s.K.ListCheckouts(ctx, kernel.CheckoutFilter{WorkItemID: item.ID})
	checkouts, err := s.describeCheckouts(ctx, held)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"item": item, "events": nonNil(page), "hasMore": hasMore,
		"running": running, "execution": execution, "children": children, "checkouts": checkouts})
}

// createWorkItem: not every task starts as a conversation. The brief enters
// through the same door as any message, addressed explicitly.
func (s *server) createWorkItem(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Title string `json:"title"`
		Brief string `json:"brief"`
		// Repo is one repository by name or path; Repos says more about each.
		Repo  string `json:"repo"`
		Repos []struct {
			Repo    string `json:"repo"`
			Base    string `json:"base"`
			From    string `json:"from"`
			InPlace bool   `json:"inPlace"`
		} `json:"repos"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, errBody("invalid json"))
		return
	}
	title := strings.TrimSpace(body.Title)
	if title == "" {
		writeJSON(w, http.StatusBadRequest, errBody("title required"))
		return
	}
	ctx := r.Context()
	var wanted []agents.RepoRequest
	if strings.TrimSpace(body.Repo) != "" {
		wanted = append(wanted, agents.RepoRequest{Ref: body.Repo})
	}
	for _, rq := range body.Repos {
		if strings.TrimSpace(rq.Repo) != "" {
			wanted = append(wanted, agents.RepoRequest{Ref: rq.Repo, Spec: repos.AttachSpec{Base: rq.Base, From: rq.From, InPlace: rq.InPlace}})
		}
	}
	item, problem, err := s.Sys.StartWorkItem(ctx, title, "connector:web", kernel.CreateWorkItemOpts{}, wanted)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody(err.Error()))
		return
	}
	if problem != "" {
		writeJSON(w, http.StatusBadRequest, errBody(problem))
		return
	}
	brief := strings.TrimSpace(body.Brief)
	if brief != "" {
		if _, err := s.Sys.SubmitMessage(ctx, agents.InboundMessage{Text: brief, Source: "connector:web", Target: item.ID}); err != nil {
			writeJSON(w, http.StatusInternalServerError, errBody(err.Error()))
			return
		}
	}
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "item": item, "dispatched": brief != ""})
}

func (s *server) files(w http.ResponseWriter, r *http.Request) {
	item, err := s.K.GetWorkItem(r.Context(), r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, errBody("not found"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"workspace": item.Workspace, "files": kernel.ListArtifacts(item.Workspace)})
}

func (s *server) file(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query().Get("path")
	if p == "" {
		writeJSON(w, http.StatusBadRequest, errBody("path required"))
		return
	}
	item, err := s.K.GetWorkItem(r.Context(), r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, errBody("not found"))
		return
	}
	// The path comes from a URL; escaping the workspace must be impossible.
	target := kernel.ResolveInside(item.Workspace, p)
	if target == "" {
		writeJSON(w, http.StatusForbidden, errBody("path outside workspace"))
		return
	}
	if has(r, "download") {
		info, err := os.Stat(target)
		if err != nil || !info.Mode().IsRegular() {
			writeJSON(w, http.StatusNotFound, errBody("not found"))
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename=%q`, filepath.Base(target)))
		http.ServeFile(w, r, target)
		return
	}
	content := kernel.ReadArtifact(item.Workspace, p)
	if content == nil {
		writeJSON(w, http.StatusNotFound, errBody("not found"))
		return
	}
	writeJSON(w, http.StatusOK, content)
}

func (s *server) patchWorkItem(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Status *string         `json:"status"`
		RunAs  json.RawMessage `json:"runAs"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, errBody("invalid json"))
		return
	}
	if body.Status == nil && body.RunAs == nil {
		writeJSON(w, http.StatusBadRequest, errBody("status or runAs required"))
		return
	}
	var runAs *kernel.RunAs
	if body.RunAs != nil {
		var err error
		if runAs, err = s.runAsFrom(body.RunAs); err != nil {
			writeJSON(w, http.StatusBadRequest, errBody(err.Error()))
			return
		}
	}
	if body.Status != nil && !kernel.ValidStatus(*body.Status) {
		writeJSON(w, http.StatusBadRequest, errBody("status must be open | done | closed"))
		return
	}
	ctx := r.Context()
	id := r.PathValue("id")
	item, err := s.K.GetWorkItem(ctx, id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, errBody("not found"))
		return
	}
	if body.Status != nil {
		if item, err = s.Sys.ChangeStatus(ctx, id, *body.Status, "connector:web", nil); err != nil {
			writeJSON(w, http.StatusInternalServerError, errBody(err.Error()))
			return
		}
	}
	if body.RunAs != nil {
		if item, err = s.K.SetWorkItemRunAs(ctx, id, runAs, "connector:web"); err != nil {
			writeJSON(w, http.StatusInternalServerError, errBody(err.Error()))
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "item": item})
}

// cancelWorkItem stops the item and everything under it, instead of waiting out a timeout.
func (s *server) cancelWorkItem(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")
	if _, err := s.K.GetWorkItem(ctx, id); err != nil {
		writeJSON(w, http.StatusNotFound, errBody("not found"))
		return
	}
	cancelled, err := s.Sys.Pool.CancelTree(ctx, id, "cancelled from the web ui", "connector:web")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody(err.Error()))
		return
	}
	if len(cancelled) == 0 {
		writeJSON(w, http.StatusConflict, errBody("no running execution"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "cancelled": cancelled})
}

// Bounded: base64 rides in the JSON body.
const maxImageBytes = 6 << 20

func (s *server) chat(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Text    string                `json:"text"`
		Images  []agents.InboundImage `json:"images"`
		Target  string                `json:"target"`
		Targets []string              `json:"targets"`
		ReplyTo string                `json:"replyTo"`
		Focus   bool                  `json:"focus"`
		RunAs   json.RawMessage       `json:"runAs"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, errBody("invalid json"))
		return
	}
	text := strings.TrimSpace(body.Text)
	var images []agents.InboundImage
	for _, im := range body.Images {
		if len(images) == agents.MaxImages {
			break
		}
		if !strings.HasPrefix(im.MimeType, "image/") || im.Data == "" || len(im.Data)*3/4 > maxImageBytes {
			continue
		}
		images = append(images, im)
	}
	if text == "" && len(images) == 0 {
		writeJSON(w, http.StatusBadRequest, errBody("text or images required"))
		return
	}
	runAs, err := s.runAsFrom(body.RunAs)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errBody(err.Error()))
		return
	}
	ctx := r.Context()
	for _, id := range append([]string{body.Target}, body.Targets...) {
		if id == "" {
			continue
		}
		if _, err := s.K.GetWorkItem(ctx, id); err != nil {
			writeJSON(w, http.StatusNotFound, errBody("target not found: "+id))
			return
		}
	}
	if text == "" {
		text = agents.ImageOnlyText
	}
	msg, err := s.Sys.SubmitMessage(ctx, agents.InboundMessage{Text: text, Images: images, Source: "connector:web",
		Target: body.Target, Targets: body.Targets, Focus: body.Focus, ReplyTo: body.ReplyTo, RunAs: runAs})
	switch {
	case errors.Is(err, agents.ErrTooManyTargets) || errors.Is(err, agents.ErrRunAsNeedsOneTarget):
		writeJSON(w, http.StatusBadRequest, errBody(err.Error()))
		return
	case err != nil:
		writeJSON(w, http.StatusInternalServerError, errBody(err.Error()))
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "accepted": true, "messageId": msg.ID})
}

// routeMessage: the person corrects where a message went, or answers "which one?".
func (s *server) routeMessage(w http.ResponseWriter, r *http.Request) {
	var body struct {
		WorkItemID string `json:"workItemId"`
	}
	_ = readJSON(r, &body)
	if body.WorkItemID == "" {
		writeJSON(w, http.StatusBadRequest, errBody("workItemId required"))
		return
	}
	item, err := s.Sys.Reattribute(r.Context(), r.PathValue("id"), body.WorkItemID, "connector:web")
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "workItemId": item.ID})
	case body.WorkItemID != "new":
		writeJSON(w, http.StatusNotFound, errBody(err.Error()))
	case notFound(err):
		writeJSON(w, http.StatusNotFound, errBody("message not found"))
	default:
		writeJSON(w, http.StatusInternalServerError, errBody(err.Error()))
	}
}

// promote: the person makes a work item of something the Primary answered.
func (s *server) promote(w http.ResponseWriter, r *http.Request) {
	item, err := s.Sys.Promote(r.Context(), r.PathValue("id"), "connector:web")
	switch {
	case notFound(err):
		writeJSON(w, http.StatusNotFound, errBody("message not found"))
	case errors.Is(err, agents.ErrAlreadyAttributed):
		writeJSON(w, http.StatusConflict, errBody(err.Error()))
	case err != nil:
		writeJSON(w, http.StatusInternalServerError, errBody(err.Error()))
	default:
		writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "item": item})
	}
}

func (s *server) redact(w http.ResponseWriter, r *http.Request) {
	ev, err := s.Sys.RedactMessage(r.Context(), r.PathValue("id"), "connector:web")
	if err != nil {
		writeJSON(w, http.StatusNotFound, errBody(err.Error()))
		return
	}
	var id any
	if ev != nil {
		id = ev.ID
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "eventId": id})
}

func (s *server) search(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		writeJSON(w, http.StatusBadRequest, errBody("q required"))
		return
	}
	var before *int64
	if b := r.URL.Query().Get("before"); b != "" {
		var n int64
		if _, err := fmt.Sscan(b, &n); err == nil {
			before = &n
		}
	}
	page, err := projections.SearchConversation(ctx, s.K, query, before, queryInt(r, "limit", 20))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody(err.Error()))
		return
	}
	items := []kernel.WorkItem{}
	// Items only with the first page: they are a short list, not a feed.
	if before == nil {
		items, _ = projections.SearchWorkItems(ctx, s.K, query, 8)
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": page.Events, "hasMore": page.HasMore, "items": items,
		"titles": s.titled(ctx, true, page.Events)})
}

func (s *server) days(w http.ResponseWriter, r *http.Request) {
	days, err := projections.ConversationDays(r.Context(), s.K, r.URL.Query().Get("tz"))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"days": days})
}

// conversationContext: where the Primary's view of the conversation begins,
// so nobody assumes the assistant remembers everything above it.
func (s *server) conversationContext(w http.ResponseWriter, r *http.Request) {
	rc, err := projections.Recent(r.Context(), s.K, nil)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, rc)
}

func (s *server) board(w http.ResponseWriter, r *http.Request) {
	cards, err := projections.BuildBoard(r.Context(), s.K, s.Sys.ActiveTurns())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"cards": cards})
}

func (s *server) policies(w http.ResponseWriter, r *http.Request) {
	path := s.K.GlobalPolicyPath()
	file, err := guard.Load(path)
	body := map[string]any{"path": path, "rules": file.Rules}
	// An unreadable file is not "no rules": the guard refuses every change
	// until it is fixed, and the person has to be able to see why.
	if err != nil {
		body["error"] = err.Error()
	}
	writeJSON(w, http.StatusOK, body)
}

func (s *server) addPolicy(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Pattern string   `json:"pattern"`
		Reason  string   `json:"reason"`
		Tools   []string `json:"tools"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, errBody("invalid json"))
		return
	}
	pattern, reason := strings.TrimSpace(body.Pattern), strings.TrimSpace(body.Reason)
	if msg := guard.ValidatePattern(pattern); msg != "" {
		writeJSON(w, http.StatusBadRequest, errBody(msg))
		return
	}
	if reason == "" {
		writeJSON(w, http.StatusBadRequest, errBody("reason required"))
		return
	}
	var tools []string
	for _, t := range body.Tools {
		if strings.TrimSpace(t) != "" {
			tools = append(tools, strings.TrimSpace(t))
		}
	}
	rule := guard.Rule{ID: kernel.GenID("pol", 6), Pattern: pattern, Reason: reason, Tools: tools}
	if err := guard.AddRule(s.K.GlobalPolicyPath(), rule); err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody(err.Error()))
		return
	}
	_, _ = s.K.Append(r.Context(), kernel.EventInput{Source: "connector:web", Kind: "policy.added",
		Payload: kernel.Payload{"ruleId": rule.ID, "pattern": rule.Pattern, "reason": rule.Reason}})
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "rule": rule})
}

func (s *server) deletePolicy(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ok, err := guard.RemoveRule(s.K.GlobalPolicyPath(), id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody(err.Error()))
		return
	}
	if !ok {
		writeJSON(w, http.StatusNotFound, errBody("not found"))
		return
	}
	_, _ = s.K.Append(r.Context(), kernel.EventInput{Source: "connector:web", Kind: "policy.removed", Payload: kernel.Payload{"ruleId": id}})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *server) memories(w http.ResponseWriter, r *http.Request) {
	path := s.K.GlobalMemoryPath()
	text := kernel.ReadTextFile(path)
	items, err := s.K.WorkItemMemories(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": path, "entries": kernel.ParseMemories(text), "markdown": text, "workItems": items})
}

// addMemory: a person who already knows a preference should not have to hint
// at it and hope the distiller notices.
func (s *server) addMemory(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Kind    string `json:"kind"`
		Content string `json:"content"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, errBody("invalid json"))
		return
	}
	content := strings.TrimSpace(body.Content)
	if content == "" {
		writeJSON(w, http.StatusBadRequest, errBody("content required"))
		return
	}
	kind := body.Kind
	if !kernel.ValidMemoryKind(kind) {
		kind = "fact"
	}
	entry, err := s.K.AppendMemory(s.K.GlobalMemoryPath(), "global", kind, content)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody(err.Error()))
		return
	}
	// Provenance matters: this one was a person's decision, not a distillation.
	_, _ = s.K.Append(r.Context(), kernel.EventInput{Source: "connector:web", Kind: "memory.promoted",
		Payload: kernel.Payload{"memoryId": entry.ID, "kind": entry.Kind, "content": entry.Content, "scope": "global", "manual": true}})
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "entry": entry})
}

func (s *server) forgetMemory(w http.ResponseWriter, r *http.Request) {
	ok, err := s.K.Forget(r.Context(), r.PathValue("id"), "connector:web")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody(err.Error()))
		return
	}
	if !ok {
		writeJSON(w, http.StatusNotFound, errBody("not found"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *server) schedules(w http.ResponseWriter, r *http.Request) {
	list, err := s.K.ListSchedules(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"schedules": list})
}

func (s *server) createSchedule(w http.ResponseWriter, r *http.Request) {
	var in kernel.ScheduleInput
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, errBody("invalid json"))
		return
	}
	sc, err := s.K.CreateSchedule(r.Context(), in, "connector:web")
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "schedule": sc})
}

func (s *server) updateSchedule(w http.ResponseWriter, r *http.Request) {
	var in kernel.ScheduleInput
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, errBody("invalid json"))
		return
	}
	sc, err := s.K.UpdateSchedule(r.Context(), r.PathValue("id"), in, "connector:web")
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "schedule": sc})
}

func (s *server) deleteSchedule(w http.ResponseWriter, r *http.Request) {
	if err := s.K.DeleteSchedule(r.Context(), r.PathValue("id"), "connector:web"); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// scheduleRuns reads a schedule's firings by the id inside the payload: a
// daily schedule's previous run is thousands of heartbeats back.
func (s *server) scheduleRuns(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")
	if _, err := s.K.GetSchedule(ctx, id); err != nil {
		writeJSON(w, http.StatusNotFound, errBody("not found"))
		return
	}
	limit := queryInt(r, "limit", 20)
	if limit > 100 {
		limit = 100
	}
	runs, err := s.K.ListEvents(ctx, kernel.ListFilter{PayloadKey: "scheduleId", PayloadValue: id, Tail: limit * 2})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody(err.Error()))
		return
	}
	for i, j := 0, len(runs)-1; i < j; i, j = i+1, j-1 {
		runs[i], runs[j] = runs[j], runs[i]
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": nonNil(runs)})
}

// runSchedule: without run-now every definition mistake takes a full period to see.
func (s *server) runSchedule(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sc, err := s.K.GetSchedule(ctx, r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, errBody("not found"))
		return
	}
	status, err := s.FireSchedule(ctx, sc)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody(err.Error()))
		return
	}
	sc, _ = s.K.GetSchedule(ctx, sc.ID)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": status, "schedule": sc})
}

var dayPattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

func (s *server) worklog(w http.ResponseWriter, r *http.Request) {
	day := r.PathValue("day")
	if day == "today" {
		day = s.K.Today()
	}
	if !dayPattern.MatchString(day) {
		writeJSON(w, http.StatusBadRequest, errBody("day must be YYYY-MM-DD"))
		return
	}
	md, count, err := projections.RenderDay(r.Context(), s.K, day)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody(err.Error()))
		return
	}
	// The count is separate: an empty day still renders a heading.
	writeJSON(w, http.StatusOK, map[string]any{"day": day, "markdown": md, "eventCount": count})
}

func (s *server) status(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	latest, _ := s.K.LatestSeq(ctx)
	cursor, _ := s.K.GetCursor(ctx, "triage")
	hb, _ := s.K.ListEvents(ctx, kernel.ListFilter{Kind: "connector.heartbeat", Tail: 1})
	open, _ := s.K.ListWorkItems(ctx, kernel.StatusOpen)
	mailboxes, _ := s.K.MailboxesWithPending(ctx)
	var lastHB any
	if len(hb) > 0 {
		lastHB = hb[0].TS
	}
	pendingMessages := 0
	for _, mb := range mailboxes {
		pendingMessages += mb.Count
	}
	st := s.Settings.Get()
	roles := map[string]any{}
	for _, role := range settings.Roles {
		res := st.Resolve(role)
		provider := ""
		if res.Provider != nil {
			provider = res.Provider.Label
		}
		roles[role] = map[string]any{"agent": res.Agent, "provider": provider, "model": res.Model}
	}
	lag := latest - cursor
	if lag < 0 {
		lag = 0
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"latestSeq": latest, "triageLag": lag, "lastHeartbeatAt": lastHB,
		"openWorkItems": len(open), "roles": roles,
		"agents": s.Detect(ctx),
		"runtime": map[string]any{"up": s.Sys.Runtime != nil, "activeTurns": s.Sys.ActiveTurns(),
			"pendingMessages": pendingMessages, "workers": s.Sys.Pool.Status()},
	})
}

func (s *server) getSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.Settings.View())
}

// writeError maps a schedule or settings error to its status.
func writeError(w http.ResponseWriter, err error) {
	var verr kernel.ValidationError
	var serr settings.Error
	var inUse settings.InUseError
	switch {
	case errors.As(err, &verr):
		writeJSON(w, http.StatusBadRequest, errBody(verr.Msg))
	case errors.As(err, &serr):
		writeJSON(w, http.StatusBadRequest, errBody(serr.Msg))
	case errors.As(err, &inUse):
		writeJSON(w, http.StatusConflict, errBody(inUse.Error()))
	case notFound(err):
		writeJSON(w, http.StatusNotFound, errBody("not found"))
	default:
		writeJSON(w, http.StatusInternalServerError, errBody(err.Error()))
	}
}

// recordSettings writes the fact of a change — never a key.
func (s *server) recordSettings(ctx context.Context, what string, extra kernel.Payload) {
	p := kernel.Payload{"what": what}
	for k, v := range extra {
		p[k] = v
	}
	_, _ = s.K.Append(ctx, kernel.EventInput{Source: "connector:web", Kind: "settings.updated", Payload: p})
}

func (s *server) putRoles(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Roles map[string]settings.RoleConfig `json:"roles"`
	}
	if err := readJSON(r, &body); err != nil || body.Roles == nil {
		writeJSON(w, http.StatusBadRequest, errBody("roles required"))
		return
	}
	if _, err := s.Settings.SetRoles(body.Roles); err != nil {
		writeError(w, err)
		return
	}
	roles := map[string]any{}
	for name, rc := range s.Settings.Get().Roles {
		roles[name] = map[string]any{"agent": rc.Agent, "provider": rc.Provider, "model": rc.Model, "effort": rc.Effort}
	}
	s.recordSettings(r.Context(), "roles", kernel.Payload{"roles": roles})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "settings": s.Settings.View()})
}

func (s *server) putBinaries(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Binaries map[string]string `json:"binaries"`
	}
	if err := readJSON(r, &body); err != nil || body.Binaries == nil {
		writeJSON(w, http.StatusBadRequest, errBody("binaries required"))
		return
	}
	if _, err := s.Settings.SetBinaries(body.Binaries); err != nil {
		writeError(w, err)
		return
	}
	if s.OnSettingsChanged != nil {
		s.OnSettingsChanged()
	}
	s.recordSettings(r.Context(), "binaries", nil)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "settings": s.Settings.View()})
}

func (s *server) addProvider(w http.ResponseWriter, r *http.Request) {
	var in settings.ProviderInput
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, errBody("invalid json"))
		return
	}
	p, err := s.Settings.AddProvider(in)
	if err != nil {
		writeError(w, err)
		return
	}
	s.recordSettings(r.Context(), "providers", kernel.Payload{"added": p.ID})
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "provider": settings.ViewOf(p)})
}

func (s *server) patchProvider(w http.ResponseWriter, r *http.Request) {
	var in settings.ProviderInput
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, errBody("invalid json"))
		return
	}
	p, err := s.Settings.UpdateProvider(r.PathValue("id"), in)
	if err != nil {
		writeError(w, err)
		return
	}
	s.recordSettings(r.Context(), "providers", kernel.Payload{"updated": p.ID})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "provider": settings.ViewOf(p)})
}

func (s *server) deleteProvider(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.Settings.DeleteProvider(id); err != nil {
		writeError(w, err)
		return
	}
	s.recordSettings(r.Context(), "providers", kernel.Payload{"deleted": id})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *server) presets(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"presets": settings.Presets()})
}

func (s *server) agentsList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"agents": s.Detect(r.Context())})
}

func (s *server) agentModels(w http.ResponseWriter, r *http.Request) {
	agent := r.PathValue("agent")
	known := false
	for _, a := range settings.Agents {
		known = known || a == agent
	}
	if !known || s.Catalog == nil {
		writeJSON(w, http.StatusNotFound, errBody("unknown agent"))
		return
	}
	writeJSON(w, http.StatusOK, s.Catalog(r.Context(), agent))
}

// runAsFrom reads a choice of agent from a request body: absent or null is
// nil (follow the settings); anything else must be a valid way to run.
func (s *server) runAsFrom(raw json.RawMessage) (*kernel.RunAs, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var r kernel.RunAs
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("runAs must be an object")
	}
	if err := s.Settings.Get().ValidateRun(settings.RoleConfig{Agent: r.Agent, Provider: r.Provider, Model: strings.TrimSpace(r.Model), Effort: r.Effort}); err != nil {
		return nil, fmt.Errorf("runAs: %v", err)
	}
	r.Model = strings.TrimSpace(r.Model)
	return &r, nil
}

// testAgent makes one real round trip for a role.
func (s *server) testAgent(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Role string `json:"role"`
	}
	_ = readJSON(r, &body)
	valid := false
	for _, role := range settings.Roles {
		if role == body.Role {
			valid = true
		}
	}
	if !valid {
		writeJSON(w, http.StatusBadRequest, errBody("role must be one of "+strings.Join(settings.Roles, ", ")))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 170*time.Second)
	defer cancel()
	res, resolved := s.Sys.Ping(ctx, body.Role)
	ok := res.OK && strings.TrimSpace(res.Text) != ""
	errText := res.Error
	if res.OK && !ok {
		errText = "empty reply"
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": ok, "text": strings.TrimSpace(res.Text), "error": errText,
		"durationMs": res.DurationMs, "agent": resolved.Agent, "model": resolved.Model})
}

func (s *server) desktopOnly(w http.ResponseWriter) bool {
	if !s.Desktop || s.Host == nil {
		// The page names this to the person; a bare "not found" says nothing.
		writeJSON(w, http.StatusNotFound, errBody(DesktopOnly))
		return false
	}
	return true
}

// DesktopOnly is the error for host actions outside the desktop app.
const DesktopOnly = "desktop app only"

func hostDone(w http.ResponseWriter, err error) {
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// openURL sends an external link to the system browser: navigating the app's
// own window to it would leave no way back to the app.
func (s *server) openURL(w http.ResponseWriter, r *http.Request) {
	if !s.desktopOnly(w) {
		return
	}
	var body struct {
		URL string `json:"url"`
	}
	_ = readJSON(r, &body)
	u, err := url.Parse(strings.TrimSpace(body.URL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "mailto") || (u.Scheme != "mailto" && u.Host == "") {
		writeJSON(w, http.StatusBadRequest, errBody("only http(s) and mailto links open externally"))
		return
	}
	hostDone(w, s.Host.Open(u.String(), false))
}

// reveal shows a workspace file in the file manager; a webview cannot save
// downloads the way a browser does.
func (s *server) reveal(w http.ResponseWriter, r *http.Request) {
	if !s.desktopOnly(w) {
		return
	}
	var body struct {
		Path string `json:"path"`
	}
	_ = readJSON(r, &body)
	item, err := s.K.GetWorkItem(r.Context(), r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, errBody("not found"))
		return
	}
	target := item.Workspace
	if body.Path != "" {
		target = kernel.ResolveInside(item.Workspace, body.Path)
		if target == "" {
			writeJSON(w, http.StatusForbidden, errBody("path outside workspace"))
			return
		}
	}
	if _, err := os.Stat(target); err != nil {
		writeJSON(w, http.StatusNotFound, errBody("not found"))
		return
	}
	hostDone(w, s.Host.Open(target, true))
}

func (s *server) copyText(w http.ResponseWriter, r *http.Request) {
	if !s.desktopOnly(w) {
		return
	}
	var body struct {
		Text string `json:"text"`
	}
	_ = readJSON(r, &body)
	hostDone(w, s.Host.CopyText(body.Text))
}

func (s *server) notify(w http.ResponseWriter, r *http.Request) {
	if !s.desktopOnly(w) {
		return
	}
	var body struct {
		Title string `json:"title"`
		Body  string `json:"body"`
	}
	_ = readJSON(r, &body)
	if strings.TrimSpace(body.Title) == "" {
		writeJSON(w, http.StatusBadRequest, errBody("title required"))
		return
	}
	hostDone(w, s.Host.Notify(clipText(body.Title, 120), clipText(body.Body, 400)))
}

func (s *server) badge(w http.ResponseWriter, r *http.Request) {
	if !s.desktopOnly(w) {
		return
	}
	var body struct {
		Count int `json:"count"`
	}
	_ = readJSON(r, &body)
	if body.Count < 0 {
		body.Count = 0
	}
	hostDone(w, s.Host.SetBadge(body.Count))
}

func clipText(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// openDataDir shows hidane's data directory (settings, memory, worklogs) in
// the file manager.
func (s *server) openDataDir(w http.ResponseWriter, r *http.Request) {
	if !s.desktopOnly(w) {
		return
	}
	hostDone(w, s.Host.Open(s.K.Cfg.Home, false))
}
