package agents_test

import (
	"testing"

	"github.com/jtsang4/hidane/internal/kernel"
	"github.com/jtsang4/hidane/internal/settings"
)

// A distillation leaves both what it considered and what it kept in the log.
func TestDistillationRecordsCandidatesAndPromotions(t *testing.T) {
	t.Parallel()
	w := newWorld(t, settings.Claude)
	m(w.k.Append(ctx, kernel.EventInput{Source: "connector:web", Kind: "user.message", ThreadID: "main", Payload: kernel.Payload{"text": "请记住我喜欢简洁"}}))
	m(w.s.RunDistillation(ctx, 1))
	candidates, promoted := w.events("memory.candidate"), w.events("memory.promoted")
	if len(candidates) != 1 || candidates[0].Payload.Str("content") != "偏好简洁的回答" {
		t.Fatalf("candidates: %+v", candidates)
	}
	entries := kernel.ParseMemories(kernel.ReadTextFile(w.k.GlobalMemoryPath()))
	if len(promoted) != 1 || promoted[0].Source != "agent:distiller" || len(entries) != 1 || promoted[0].Payload.Str("memoryId") != entries[0].ID {
		t.Fatalf("promoted: %+v, file: %+v", promoted, entries)
	}
}
