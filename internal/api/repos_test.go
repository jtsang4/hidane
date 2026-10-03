package api_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jtsang4/hidane/internal/api"
	"github.com/jtsang4/hidane/internal/kernel"
)

func gitRepo(t *testing.T, name string) string {
	t.Helper()
	home := t.TempDir()
	for k, v := range map[string]string{
		"HOME": home, "GIT_CONFIG_GLOBAL": filepath.Join(home, "gitconfig"), "GIT_CONFIG_NOSYSTEM": "1",
		"GIT_AUTHOR_NAME": "test", "GIT_AUTHOR_EMAIL": "test@example.com",
		"GIT_COMMITTER_NAME": "test", "GIT_COMMITTER_EMAIL": "test@example.com",
	} {
		t.Setenv(k, v)
	}
	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"commit", "-q", "--allow-empty", "-m", "init"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %s", err, out)
		}
	}
	resolved, _ := filepath.EvalSymlinks(dir)
	return resolved
}

// Repositories and worktrees are managed by hand through the API: listed with
// their state, archived only when nothing would be lost unasked.
func TestReposAndCheckoutsAPI(t *testing.T) {
	e := newEnv(t, api.Options{Token: "tok"})
	dir := gitRepo(t, "blog")
	if code, _ := e.do("POST", "/api/repos", "tok", map[string]any{"path": t.TempDir()}); code != 400 {
		t.Fatalf("a directory that is not a repo is refused: %d", code)
	}
	code, body := e.do("POST", "/api/repos", "tok", map[string]any{"path": dir})
	if code != 201 {
		t.Fatalf("register: %d %v", code, body)
	}
	repoID := body["repo"].(map[string]any)["id"].(string)

	code, body = e.do("POST", "/api/work-items", "tok", map[string]any{"title": "rss", "repos": []any{map[string]any{"repo": "blog"}}})
	if code != 201 {
		t.Fatalf("create in a repo: %d %v", code, body)
	}
	itemID := body["item"].(map[string]any)["id"].(string)
	if code, body = e.do("POST", "/api/work-items", "tok", map[string]any{"title": "x", "repo": "nope"}); code != 400 || !strings.Contains(body["error"].(string), "nope") {
		t.Fatalf("an unknown repo is a question, not a task: %d %v", code, body)
	}

	_, body = e.do("GET", "/api/checkouts", "tok", nil)
	list := body["checkouts"].([]any)
	if len(list) != 1 {
		t.Fatalf("checkouts: %v", body)
	}
	co := list[0].(map[string]any)
	if co["workItemId"] != itemID || co["repoName"] != "blog" || co["health"] != "ok" || co["title"] != "rss" || co["branch"] != "hidane/"+itemID {
		t.Fatalf("checkout view: %v", co)
	}
	path := co["path"].(string)
	if err := os.WriteFile(filepath.Join(path, "draft.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	coID := co["id"].(string)
	code, body = e.do("POST", "/api/checkouts/"+coID+"/archive", "tok", map[string]any{})
	if code != 409 || body["dirty"].(float64) != 1 {
		t.Fatalf("uncommitted work needs force: %d %v", code, body)
	}
	if code, _ := e.do("DELETE", "/api/repos/"+repoID, "tok", nil); code != 409 {
		t.Fatalf("a repo in use is not forgotten: %d", code)
	}
	if code, body = e.do("POST", "/api/checkouts/"+coID+"/archive", "tok", map[string]any{"force": true}); code != 200 {
		t.Fatalf("archive: %d %v", code, body)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("archived worktree removed")
	}
	_, body = e.do("GET", "/api/checkouts", "tok", nil)
	if len(body["checkouts"].([]any)) != 0 {
		t.Fatalf("the default list holds active ones: %v", body)
	}
	_, body = e.do("GET", "/api/checkouts?all", "tok", nil)
	if all := body["checkouts"].([]any); len(all) != 1 || all[0].(map[string]any)["status"] != kernel.CheckoutArchived {
		t.Fatalf("?all includes archived: %v", body)
	}
	_, body = e.do("GET", "/api/work-items/"+itemID, "tok", nil)
	if cs, _ := body["checkouts"].([]any); len(cs) != 1 {
		t.Fatalf("the work item shows its checkouts: %v", body["checkouts"])
	}
	_, body = e.do("GET", "/api/board", "tok", nil)
	cards, _ := body["cards"].([]any)
	if len(cards) == 0 || len(cards[0].(map[string]any)["checkouts"].([]any)) != 1 {
		t.Fatalf("the card names the repo and branch: %v", body)
	}

	moved := filepath.Join(filepath.Dir(dir), "blog-moved")
	if err := os.Rename(dir, moved); err != nil {
		t.Fatal(err)
	}
	_, body = e.do("GET", "/api/repos", "tok", nil)
	if r := body["repos"].([]any)[0].(map[string]any); r["status"] != kernel.RepoMissing {
		t.Fatalf("listing notices a missing repo: %v", r)
	}
	if code, body = e.do("PATCH", "/api/repos/"+repoID, "tok", map[string]any{"path": moved}); code != 200 || body["repo"].(map[string]any)["status"] != kernel.RepoPresent {
		t.Fatalf("relocate: %d %v", code, body)
	}
	if code, _ := e.do("DELETE", "/api/repos/"+repoID, "tok", nil); code != 200 {
		t.Fatalf("forget: %d", code)
	}
}
