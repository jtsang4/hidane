package repos

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/jtsang4/hidane/internal/kernel"
)

// Changes is what a task changed in one checkout, read from git when the
// person reviews it. Nothing is stored: the worktree is the record.
type Changes struct {
	CheckoutID string `json:"checkoutId"`
	Repo       string `json:"repo"`
	Mode       string `json:"mode"`
	Path       string `json:"path"`
	Branch     string `json:"branch"`
	// Against names what the changes are measured from: the trunk the branch
	// left (from where it left it), or HEAD for work done in place.
	Against string       `json:"against"`
	Commits []Commit     `json:"commits"`
	Files   []FileChange `json:"files"`
	// Patch is the unified diff of tracked files, untracked ones appended as
	// new files; cut at maxPatchBytes.
	Patch     string `json:"patch"`
	Truncated bool   `json:"truncated"`
	// Problem: why nothing could be read — "missing" (the directory is gone)
	// or "repo_missing".
	Problem string `json:"problem,omitempty"`
}

type Commit struct {
	Hash    string `json:"hash"`
	Subject string `json:"subject"`
}

type FileChange struct {
	Path string `json:"path"`
	// Status: added, modified, deleted, renamed or untracked.
	Status string `json:"status"`
	From   string `json:"from,omitempty"`
	// Additions and Deletions are -1 for a binary file.
	Additions int `json:"additions"`
	Deletions int `json:"deletions"`
}

const (
	maxPatchBytes    = 400 << 10
	maxUntrackedRead = 64 << 10
	maxCommits       = 50
)

// Changes reads a checkout's changes: for a worktree everything since it left
// the trunk, committed or not; in place, only what is not committed yet.
func (s *Service) Changes(ctx context.Context, c kernel.Checkout, r kernel.Repo) Changes {
	ch := Changes{CheckoutID: c.ID, Repo: r.Name, Mode: c.Mode, Path: c.Path, Branch: c.Branch, Against: "HEAD",
		Commits: []Commit{}, Files: []FileChange{}}
	switch {
	case r.Status == kernel.RepoMissing || !Present(r.Path):
		ch.Problem = "repo_missing"
		return ch
	case !Present(c.Path):
		ch.Problem = "missing"
		return ch
	}
	from := "HEAD"
	if c.Mode == kernel.CheckoutWorktree {
		trunk := r.DefaultBranch
		if trunk == "" {
			trunk = c.Base
		}
		if trunk != "" && trunk != c.Branch {
			if base, err := s.git(ctx, c.Path, "merge-base", trunk, "HEAD"); err == nil && base != "" {
				from, ch.Against = base, trunk
			}
		}
	}
	if from != "HEAD" {
		if out, err := s.git(ctx, c.Path, "log", "--format=%h%x09%s", "-n", strconv.Itoa(maxCommits), from+"..HEAD"); err == nil {
			for _, line := range strings.Split(out, "\n") {
				if hash, subject, ok := strings.Cut(line, "\t"); ok {
					ch.Commits = append(ch.Commits, Commit{Hash: hash, Subject: subject})
				}
			}
		}
	}
	counts := map[string][2]int{}
	if out, err := s.git(ctx, c.Path, "diff", "--numstat", "-M", "-z", from); err == nil {
		counts = parseNumstat(out)
	}
	if out, err := s.git(ctx, c.Path, "diff", "--name-status", "-M", "-z", from); err == nil {
		for _, f := range parseNameStatus(out) {
			n, ok := counts[f.Path]
			if !ok {
				n = [2]int{-1, -1}
			}
			f.Additions, f.Deletions = n[0], n[1]
			ch.Files = append(ch.Files, f)
		}
	}
	var patch strings.Builder
	if out, err := s.git(ctx, c.Path, "diff", "--no-color", "--no-ext-diff", "-M", from); err == nil && out != "" {
		patch.WriteString(out)
		patch.WriteString("\n")
	}
	if out, err := s.git(ctx, c.Path, "ls-files", "--others", "--exclude-standard", "-z"); err == nil {
		for _, rel := range strings.Split(out, "\x00") {
			if rel == "" {
				continue
			}
			f := FileChange{Path: rel, Status: "untracked", Additions: -1, Deletions: -1}
			if text, ok := readText(filepath.Join(c.Path, rel)); ok {
				lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
				if text == "" {
					lines = nil
				}
				f.Additions, f.Deletions = len(lines), 0
				fmt.Fprintf(&patch, "diff --git a/%s b/%s\nnew file (untracked)\n--- /dev/null\n+++ b/%s\n", rel, rel, rel)
				if len(lines) > 0 {
					fmt.Fprintf(&patch, "@@ -0,0 +1,%d @@\n", len(lines))
					for _, l := range lines {
						patch.WriteString("+" + l + "\n")
					}
				}
			}
			ch.Files = append(ch.Files, f)
		}
	}
	ch.Patch = patch.String()
	if len(ch.Patch) > maxPatchBytes {
		cut := maxPatchBytes
		for cut > 0 && !utf8.RuneStart(ch.Patch[cut]) {
			cut--
		}
		ch.Patch, ch.Truncated = ch.Patch[:cut], true
	}
	return ch
}

// readText reads a small text file; binary or large files are not shown.
func readText(path string) (string, bool) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxUntrackedRead {
		return "", false
	}
	b, err := os.ReadFile(path)
	if err != nil || !utf8.Valid(b) || strings.ContainsRune(string(b), 0) {
		return "", false
	}
	return string(b), true
}

// parseNumstat reads `git diff --numstat -z`: counts per (new) path, -1 for binary.
func parseNumstat(out string) map[string][2]int {
	counts := map[string][2]int{}
	fields := strings.Split(out, "\x00")
	for i := 0; i < len(fields); i++ {
		parts := strings.SplitN(fields[i], "\t", 3)
		if len(parts) != 3 {
			continue
		}
		add, errA := strconv.Atoi(parts[0])
		del, errD := strconv.Atoi(parts[1])
		if errA != nil || errD != nil {
			add, del = -1, -1
		}
		path := parts[2]
		// A rename leaves the path empty and names old and new in the next two fields.
		if path == "" && i+2 < len(fields) {
			path = fields[i+2]
			i += 2
		}
		counts[path] = [2]int{add, del}
	}
	return counts
}

// parseNameStatus reads `git diff --name-status -M -z`.
func parseNameStatus(out string) []FileChange {
	var files []FileChange
	fields := strings.Split(out, "\x00")
	for i := 0; i < len(fields); i++ {
		code := fields[i]
		if code == "" || i+1 >= len(fields) {
			continue
		}
		switch code[0] {
		case 'R', 'C':
			if i+2 >= len(fields) {
				return files
			}
			files = append(files, FileChange{Path: fields[i+2], From: fields[i+1], Status: "renamed"})
			i += 2
		case 'A':
			files = append(files, FileChange{Path: fields[i+1], Status: "added"})
			i++
		case 'D':
			files = append(files, FileChange{Path: fields[i+1], Status: "deleted"})
			i++
		default:
			files = append(files, FileChange{Path: fields[i+1], Status: "modified"})
			i++
		}
	}
	return files
}
