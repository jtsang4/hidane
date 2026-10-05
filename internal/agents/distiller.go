package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/jtsang4/hidane/internal/clip"
	"github.com/jtsang4/hidane/internal/kernel"
	"github.com/jtsang4/hidane/internal/projections"
)

const (
	distillerConsumer = "distiller"
	promoteThreshold  = 0.8
	recallCap         = 4000
)

var meaningfulKinds = map[string]bool{
	"user.message": true, "agent.reply": true, "escalation": true, "execution.finished": true,
	"work_item.created": true, "work_item.status_changed": true,
}

// MeaningfulEvents are kinds that can carry durable material. A message the
// person hid must not come back as a memory.
func MeaningfulEvents(events []kernel.Event) []kernel.Event {
	var out []kernel.Event
	for _, e := range events {
		if meaningfulKinds[e.Kind] && !e.Payload.Bool("redacted") {
			out = append(out, e)
		}
	}
	return out
}

func eventLine(e kernel.Event) string {
	text := e.Payload.Str("text")
	if text == "" {
		text = e.Payload.Str("summary")
	}
	if text == "" {
		text = e.Payload.Str("note")
	}
	body := clip.Runes(text, 500)
	if text == "" {
		b, _ := json.Marshal(e.Payload)
		body = clip.Runes(string(b), 200)
	}
	tag := e.Kind
	if e.WorkItemID != "" {
		tag += " " + e.WorkItemID
	}
	return fmt.Sprintf("[%s] %s", tag, body)
}

type DistillResult struct {
	Scanned    int  `json:"scanned"`
	Meaningful int  `json:"meaningful"`
	Extracted  int  `json:"extracted"`
	Promoted   int  `json:"promoted"`
	Skipped    bool `json:"skipped"`
}

// reconcileMemoryLog records entries edited into MEMORY.md by hand: a memory
// whose provenance the log cannot account for is exactly the kind of thing
// that later steers work with nobody able to say why.
func (s *System) reconcileMemoryLog(ctx context.Context, entries []kernel.MemoryEntry) error {
	if len(entries) == 0 {
		return nil
	}
	promoted, err := s.K.ListEvents(ctx, kernel.ListFilter{Kind: "memory.promoted"})
	if err != nil {
		return err
	}
	recorded := map[string]bool{}
	for _, e := range promoted {
		recorded[e.Payload.Str("memoryId")] = true
	}
	for _, entry := range entries {
		if entry.ID == "" || recorded[entry.ID] {
			continue
		}
		if _, err := s.K.Append(ctx, kernel.EventInput{Source: "agent:distiller", Kind: "memory.promoted",
			Payload: kernel.Payload{"memoryId": entry.ID, "kind": entry.Kind, "content": entry.Content, "scope": "global", "observedInFile": true}}); err != nil {
			return err
		}
	}
	return nil
}

// RunDistillation consumes the log with its own cursor: batch → candidate
// memories → high-confidence ones land in the layered memory files. The cursor
// advances only when the batch was processed (or held nothing meaningful), so
// sparse material accumulates instead of getting lost.
func (s *System) RunDistillation(ctx context.Context, minEvents int) (DistillResult, error) {
	k := s.K
	// Read forward until there is enough material, the head of the log, or the
	// cap: re-reading one fixed window that holds a few messages among
	// heartbeats would never advance, and distillation would stop for good.
	const page, scanCap = 200, 2000
	cursor, err := k.GetCursor(ctx, distillerConsumer)
	if err != nil {
		return DistillResult{Skipped: true}, err
	}
	var batch, meaningful []kernel.Event
	atHead := false
	for after := cursor; len(batch) < scanCap; {
		next, err := k.ListEvents(ctx, kernel.ListFilter{AfterSeq: &after, Limit: page})
		if err != nil {
			return DistillResult{Skipped: true}, err
		}
		batch = append(batch, next...)
		meaningful = append(meaningful, MeaningfulEvents(next)...)
		if len(next) < page {
			atHead = true
			break
		}
		after = next[len(next)-1].Seq
		if len(meaningful) >= minEvents {
			break
		}
	}
	if len(batch) == 0 {
		return DistillResult{Skipped: true}, nil
	}
	last := batch[len(batch)-1].Seq
	res := DistillResult{Scanned: len(batch), Meaningful: len(meaningful), Skipped: true}
	if len(meaningful) == 0 {
		return res, k.CommitCursor(ctx, distillerConsumer, last)
	}
	// Sparse material waits for more — unless the cap was hit, which means it
	// is all there will be for a while.
	if len(meaningful) < minEvents && atHead {
		return res, nil
	}
	existing := kernel.ParseMemories(kernel.ReadTextFile(k.GlobalMemoryPath()))
	if err := s.reconcileMemoryLog(ctx, existing); err != nil {
		return res, err
	}
	var known []string
	for _, m := range existing {
		known = append(known, "- "+m.Content)
	}
	// A work item's own memories too: shown only the global layer, a
	// replayed distillation re-promoted a work item's memories reworded.
	seen := map[string]bool{}
	for _, e := range meaningful {
		if e.WorkItemID == "" || seen[e.WorkItemID] {
			continue
		}
		seen[e.WorkItemID] = true
		it, err := k.GetWorkItem(ctx, e.WorkItemID)
		if err != nil || it.Workspace == "" {
			continue
		}
		for _, m := range kernel.ParseMemories(kernel.ReadTextFile(kernel.WorkItemMemoryPath(it.Workspace))) {
			known = append(known, fmt.Sprintf("- (work item %s) %s", it.ID, m.Content))
		}
	}
	existingText := ""
	if len(known) > 0 {
		existingText = "Existing memories (do not duplicate, not even reworded):\n" + strings.Join(known, "\n")
	}
	var lines []string
	for _, e := range meaningful {
		lines = append(lines, eventLine(e))
	}
	thought := s.think(ctx, joinNonEmpty([]string{existingText, "Recent events:\n" + strings.Join(lines, "\n")}),
		thinkOpts{Role: "distiller", Charter: DistillerCharter, Cwd: roleDir(k, "distiller"), SessionDir: k.Cfg.SessionsDir()})
	if !thought.OK {
		_, err := k.Append(ctx, kernel.EventInput{Source: "agent:distiller", Kind: "distill.run",
			Payload: kernel.Payload{"ok": false, "error": thought.Error, "scanned": len(batch)}})
		// The cursor stays: the same material is retried next round.
		return res, err
	}
	var parsed struct {
		Memories []struct {
			Kind       string  `json:"kind"`
			Scope      string  `json:"scope"`
			WorkItemID *string `json:"work_item_id"`
			Content    string  `json:"content"`
			Confidence float64 `json:"confidence"`
		} `json:"memories"`
	}
	ExtractJSON(thought.Raw, &parsed)
	for _, m := range parsed.Memories {
		content := strings.TrimSpace(m.Content)
		if content == "" {
			continue
		}
		kind := m.Kind
		if !kernel.ValidMemoryKind(kind) {
			kind = "fact"
		}
		scope := "global"
		if m.Scope == "work_item" {
			scope = "work_item"
		}
		workItemID := ""
		if m.WorkItemID != nil {
			workItemID = *m.WorkItemID
		}
		confidence := math.Max(0, math.Min(1, m.Confidence))
		cand := kernel.EventInput{Source: "agent:distiller", Kind: "memory.candidate",
			Payload: kernel.Payload{"kind": kind, "scope": scope, "content": content, "confidence": confidence}}
		if scope == "work_item" {
			cand.WorkItemID = workItemID
		}
		if _, err := k.Append(ctx, cand); err != nil {
			return res, err
		}
		if confidence < promoteThreshold {
			continue
		}
		workspace := ""
		if scope == "work_item" && workItemID != "" {
			if it, err := k.GetWorkItem(ctx, workItemID); err == nil {
				workspace = it.Workspace
			}
		}
		target := "global"
		if workspace != "" {
			target = "work_item"
		}
		_, added, err := k.PromoteToFile(ctx, kind, content, target, workspace, workItemID, "agent:distiller")
		if err != nil {
			return res, err
		}
		if added {
			res.Promoted++
		}
	}
	res.Extracted = len(parsed.Memories)
	res.Skipped = false
	if err := k.CommitCursor(ctx, distillerConsumer, last); err != nil {
		return res, err
	}
	_, err = k.Append(ctx, kernel.EventInput{Source: "agent:distiller", Kind: "distill.run", Payload: kernel.Payload{
		"ok": true, "scanned": len(batch), "meaningful": len(meaningful), "extracted": res.Extracted, "promoted": res.Promoted,
		"durationMs": thought.DurationMs}})
	return res, err
}

// RecallForPrimary is the global memory file: cheap cross-day recall.
func (s *System) RecallForPrimary() string {
	return clip.Runes(strings.TrimSpace(kernel.ReadTextFile(s.K.GlobalMemoryPath())), recallCap)
}

// RecallForManager is the work item's memory plus the global memory.
func (s *System) RecallForManager(item kernel.WorkItem) string {
	scoped := strings.TrimSpace(kernel.ReadTextFile(kernel.WorkItemMemoryPath(item.Workspace)))
	global := strings.TrimSpace(kernel.ReadTextFile(s.K.GlobalMemoryPath()))
	return clip.Runes(joinNonEmpty([]string{scoped, global}), recallCap)
}

// NewRuntime is the event loop with every role and housekeeping task wired in.
func (s *System) NewRuntime() *kernel.Runtime {
	k := s.K
	rt := kernel.NewRuntime(k, k.Cfg.MaxConcurrentTurns)
	rt.Register(kernel.Primary, s.PrimaryTurn)
	rt.Register(kernel.ManagerPrefix, s.ManagerTurn)
	rt.RegisterTimer("worker-pump", func(context.Context) error { s.Pool.Pump(); return nil }, 2*time.Second)
	rt.RegisterIdle("distill", func(ctx context.Context) error {
		_, err := s.RunDistillation(ctx, 10)
		return err
	}, k.Cfg.DistillInterval, 3*k.Cfg.DistillInterval)
	rt.RegisterIdle("archive", func(ctx context.Context) error {
		_, _, err := projections.ArchiveDay(ctx, k, k.Today())
		return err
	}, time.Hour, 2*time.Hour)
	s.Runtime = rt
	return rt
}

// ActiveTurns is empty when no runtime runs in this process.
func (s *System) ActiveTurns() []string {
	if s.Runtime == nil {
		return []string{}
	}
	return s.Runtime.ActiveTurns()
}
