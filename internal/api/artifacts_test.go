package api_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jtsang4/hidane/internal/api"
	"github.com/jtsang4/hidane/internal/kernel"
)

// A workspace's products are listed, read and downloaded; a path that leaves
// the workspace is refused however it is written, since it comes from a URL.
func TestArtifactsStayInsideTheWorkspace(t *testing.T) {
	e := newEnv(t, api.Options{Token: "t"})
	item := m(e.k.CreateWorkItem(context.Background(), "files", "test", kernel.CreateWorkItemOpts{}))
	png := []byte{0x89, 'P', 'N', 'G', 0}
	for rel, data := range map[string][]byte{
		"notes/detail.md": []byte("# detail"), "chart.png": png, "big.log": bytes.Repeat([]byte("x"), kernel.MaxInlineBytes+1),
		"node_modules/lib/index.js": []byte("x"), ".git/HEAD": []byte("x"),
	} {
		path := filepath.Join(item.Workspace, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil || os.WriteFile(path, data, 0o644) != nil {
			t.Fatal(rel)
		}
	}
	base := "/api/work-items/" + item.ID
	_, body := e.do("GET", base+"/files", "t", nil)
	listed := map[string]bool{}
	for _, f := range body["files"].([]any) {
		p := f.(map[string]any)["path"].(string)
		listed[p] = true
		if strings.HasPrefix(p, "node_modules/") || strings.HasPrefix(p, ".git/") {
			t.Fatalf("%s is not a product", p)
		}
	}
	if !listed["notes/detail.md"] || !listed["chart.png"] || !listed["big.log"] {
		t.Fatalf("listed: %v", listed)
	}
	if code, body := e.do("GET", base+"/file?path=notes/detail.md", "t", nil); code != 200 || body["text"] != "# detail" {
		t.Fatalf("text: %d %v", code, body)
	}
	for path, reason := range map[string]string{"chart.png": "binary", "big.log": "too-large"} {
		if code, body := e.do("GET", base+"/file?path="+path, "t", nil); code != 200 || body["reason"] != reason || body["text"] != nil {
			t.Fatalf("%s is not inlined: %d %v, text %t", path, code, body["reason"], body["text"] != nil)
		}
	}
	req, _ := http.NewRequest("GET", e.srv.URL+base+"/file?path=chart.png&download", nil)
	req.Header.Set("Authorization", "Bearer t")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || !strings.HasPrefix(res.Header.Get("Content-Disposition"), "attachment") || !bytes.Equal(got, png) {
		t.Fatalf("download: %d %q %q", res.StatusCode, res.Header.Get("Content-Disposition"), got)
	}
	for _, p := range []string{"../../../etc/passwd", "..%2F..%2F..%2Fetc%2Fpasswd", "/etc/passwd", "%2Fetc%2Fpasswd", "notes/../../../../etc/passwd"} {
		for _, suffix := range []string{"", "&download"} {
			if code, _ := e.do("GET", base+"/file?path="+p+suffix, "t", nil); code != 403 {
				t.Fatalf("%s%s escapes the workspace: %d", p, suffix, code)
			}
		}
	}
	if code, _ := e.do("GET", base+"/file?path=notes/detail.md", "", nil); code != 401 {
		t.Fatalf("no token: %d", code)
	}
}
