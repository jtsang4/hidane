package api_test

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jtsang4/hidane/internal/api"
	"github.com/jtsang4/hidane/internal/kernel"
)

// A message addressed to a work item that does not exist is refused before
// anything is kept: no event, and none of its images on disk.
func TestChatRefusesAnUnknownTargetBeforeStoringImages(t *testing.T) {
	e := newEnv(t, api.Options{})
	ctx := context.Background()
	a := m(e.k.CreateWorkItem(ctx, "alpha", "test", kernel.CreateWorkItemOpts{}))
	png := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="
	images := []any{map[string]any{"data": png, "mimeType": "image/png"}}
	stored := func() int {
		n := 0
		_ = filepath.WalkDir(filepath.Join(e.k.Cfg.Home, "inbox"), func(_ string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				n++
			}
			return nil
		})
		return n
	}
	for name, body := range map[string]map[string]any{
		"target":  {"text": "x", "images": images, "target": "wi_nope"},
		"targets": {"text": "x", "images": images, "targets": []any{a.ID, "wi_nope"}},
	} {
		code, res := e.do("POST", "/api/chat", "", body)
		if code != 404 || !strings.Contains(fmt.Sprint(res["error"]), "target not found: wi_nope") {
			t.Fatalf("%s: %d %v", name, code, res)
		}
	}
	if n := stored(); n != 0 {
		t.Fatalf("a refused message left %d image file(s)", n)
	}
	if got := m(e.k.ListEvents(ctx, kernel.ListFilter{Kind: "user.message"})); len(got) != 0 {
		t.Fatalf("a refused message was recorded: %+v", got)
	}
	if code, res := e.do("POST", "/api/chat", "", map[string]any{"text": "x", "images": images, "target": a.ID}); code != 202 {
		t.Fatalf("known target: %d %v", code, res)
	}
	if n := stored(); n != 1 {
		t.Fatalf("an accepted message keeps its image: %d", n)
	}
}

// An unknown target is the first refusal, whatever else is wrong with the
// message, and ids are taken as given.
func TestChatNamesAnUnknownTargetFirst(t *testing.T) {
	e := newEnv(t, api.Options{})
	ctx := context.Background()
	a := m(e.k.CreateWorkItem(ctx, "alpha", "test", kernel.CreateWorkItemOpts{}))
	nine := []any{"wi_nope"}
	for i := 0; i < 8; i++ {
		nine = append(nine, a.ID)
	}
	for name, body := range map[string]map[string]any{
		"too many":    {"text": "x", "targets": nine},
		"run-as":      {"text": "x", "targets": []any{a.ID, "wi_nope"}, "runAs": map[string]any{"agent": "claude"}},
		"padded":      {"text": "x", "target": " " + a.ID + " "},
		"padded list": {"text": "x", "targets": []any{" " + a.ID}},
	} {
		if code, res := e.do("POST", "/api/chat", "", body); code != 404 || !strings.HasPrefix(fmt.Sprint(res["error"]), "target not found: ") {
			t.Errorf("%s: %d %v", name, code, res)
		}
	}
}
