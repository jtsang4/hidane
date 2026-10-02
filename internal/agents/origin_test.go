package agents

import (
	"context"
	"testing"

	"github.com/jtsang4/hidane/internal/kernel"
	"github.com/jtsang4/hidane/internal/kernel/kerneltest"
	"github.com/jtsang4/hidane/internal/settings"
)

func TestAnswersInheritTheirConversationsOrigin(t *testing.T) {
	ctx := context.Background()
	k := kerneltest.New(t)
	st, _ := settings.Load(k.Cfg.SettingsPath())
	s := New(k, st, nil)
	hook, _ := k.Append(ctx, kernel.EventInput{Source: "kernel:triage", Kind: "triage.decision", Payload: kernel.Payload{"summary": "deploy failed"}})
	person, _ := k.Append(ctx, kernel.EventInput{Source: "connector:web", Kind: "user.message", ThreadID: "main"})
	p := kernel.Payload{}
	s.originOf(ctx, hook.ID, p)
	if p.Str("rootKind") != "external" || p.Str("rootText") != "deploy failed" {
		t.Fatalf("a webhook's work item answers as external: %+v", p)
	}
	q := kernel.Payload{}
	s.originOf(ctx, person.ID, q)
	if _, ok := q["rootKind"]; ok {
		t.Fatal("a person's conversation is not marked")
	}
}
