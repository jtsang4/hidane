package kernel

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/jtsang4/hidane/internal/guard"
)

// GlobalPolicyPath is the outermost capture-phase policy file.
func (k *Kernel) GlobalPolicyPath() string { return filepath.Join(k.Cfg.Home, "POLICY.json") }

func WorkspacePolicyPath(workspace string) string {
	return filepath.Join(workspace, ".hidane", "POLICY.json")
}

// PolicyFilesFor lists the files a worker's tool calls pass, outermost first.
func (k *Kernel) PolicyFilesFor(ctx context.Context, item WorkItem) []string {
	chain, _ := k.Ancestors(ctx, item)
	files := []string{k.GlobalPolicyPath()}
	for i := len(chain) - 1; i >= 0; i-- {
		files = append(files, WorkspacePolicyPath(chain[i].Workspace))
	}
	return append(files, WorkspacePolicyPath(item.Workspace))
}

func (k *Kernel) AddGlobalRule(pattern, reason string, tools []string) (guard.Rule, error) {
	path := k.GlobalPolicyPath()
	f := guard.ReadFile(path)
	rule := guard.Rule{ID: GenID("pol", 6), Pattern: pattern, Reason: reason}
	if len(tools) > 0 {
		rule.Tools = tools
	}
	f.Rules = append(f.Rules, rule)
	return rule, guard.WriteFile(path, f)
}

func (k *Kernel) RemoveGlobalRule(id string) (bool, error) {
	path := k.GlobalPolicyPath()
	f := guard.ReadFile(path)
	kept := make([]guard.Rule, 0, len(f.Rules))
	for _, r := range f.Rules {
		if r.ID != id {
			kept = append(kept, r)
		}
	}
	if len(kept) == len(f.Rules) {
		return false, nil
	}
	return true, guard.WriteFile(path, guard.File{Rules: kept})
}

// Memory lives in layered markdown FILES — the canonical current state,
// human-readable and agent-native. The log records every change
// (memory.candidate / memory.promoted / memory.forgotten); the files are the
// working set. No database table: that would be a second source of truth.
//
//	global    → <home>/memory/MEMORY.md
//	work item → <workspace>/MEMORY.md (workers see it in their cwd)
type MemoryEntry struct {
	Kind    string `json:"kind"`
	Content string `json:"content"`
	Date    string `json:"date"`
	ID      string `json:"id"`
}

var MemoryKinds = []string{"fact", "preference", "decision", "lesson"}

func ValidMemoryKind(kind string) bool {
	for _, k := range MemoryKinds {
		if k == kind {
			return true
		}
	}
	return false
}

func (k *Kernel) GlobalMemoryPath() string { return filepath.Join(k.Cfg.Home, "memory", "MEMORY.md") }

func WorkItemMemoryPath(workspace string) string { return filepath.Join(workspace, "MEMORY.md") }

func ReadTextFile(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(b)
}

func memoryHeader(scope string) string {
	return fmt.Sprintf("# hidane memory (%s)\n\nDistilled long-term memory. Edit freely — this file is the source of truth; history lives in the event log.\n", scope)
}

var nextHeading = regexp.MustCompile(`\n## `)

// AppendMemory adds one entry under its kind section, creating file and section as needed.
func (k *Kernel) AppendMemory(path, scope, kind, content string) (MemoryEntry, error) {
	id := GenID("mem", 6)
	date := k.Now().Format("2006-01-02")
	text := ReadTextFile(path)
	if strings.TrimSpace(text) == "" {
		text = memoryHeader(scope)
	}
	section := "## " + kind
	line := fmt.Sprintf("- (%s) %s <!-- %s -->", date, content, id)
	if strings.Contains(text, section+"\n") || strings.HasSuffix(text, section) {
		start := strings.Index(text, section)
		rest := text[start+len(section):]
		insertAt := start + len(section) + len(rest)
		if loc := nextHeading.FindStringIndex(rest); loc != nil {
			insertAt = start + len(section) + loc[0]
		}
		before := strings.TrimRight(text[:insertAt], "\n") + "\n"
		after := strings.TrimLeft(text[insertAt:], "\n")
		if after != "" {
			after = "\n" + after
		}
		text = before + line + "\n" + after
	} else {
		text = strings.TrimRight(text, "\n") + "\n\n" + section + "\n\n" + line + "\n"
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return MemoryEntry{}, err
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		return MemoryEntry{}, err
	}
	return MemoryEntry{Kind: kind, Content: content, Date: date, ID: id}, nil
}

var (
	memHeading = regexp.MustCompile(`^## (\w+)`)
	memBullet  = regexp.MustCompile(`^- \((\d{4}-\d{2}-\d{2})\) (.*?)(?: <!-- (\S+) -->)?$`)
)

// ParseMemories reads entries back out of a memory file.
func ParseMemories(text string) []MemoryEntry {
	out := []MemoryEntry{}
	kind := "fact"
	for _, line := range strings.Split(text, "\n") {
		if m := memHeading.FindStringSubmatch(line); m != nil && ValidMemoryKind(m[1]) {
			kind = m[1]
			continue
		}
		if m := memBullet.FindStringSubmatch(line); m != nil {
			out = append(out, MemoryEntry{Kind: kind, Date: m[1], Content: strings.TrimSpace(m[2]), ID: m[3]})
		}
	}
	return out
}

// ForgetMemory removes a memory line. Memory must be able to expire: a lesson
// distilled from a since-fixed bug becomes an actively wrong instruction, so
// removal is as first-class as promotion, and equally recorded.
func (k *Kernel) ForgetMemory(ctx context.Context, path, memoryID, source string) (bool, error) {
	text := ReadTextFile(path)
	if text == "" {
		return false, nil
	}
	marker := "<!-- " + memoryID + " -->"
	lines := strings.Split(text, "\n")
	kept := lines[:0:0]
	for _, l := range lines {
		if !strings.Contains(l, marker) {
			kept = append(kept, l)
		}
	}
	if len(kept) == len(lines) {
		return false, nil
	}
	if err := os.WriteFile(path, []byte(strings.Join(kept, "\n")), 0o644); err != nil {
		return false, err
	}
	_, err := k.Append(ctx, EventInput{Source: source, Kind: "memory.forgotten", Payload: Payload{"memoryId": memoryID, "path": path}})
	return true, err
}

// PromoteToFile writes a memory into its layer file and records the fact.
func (k *Kernel) PromoteToFile(ctx context.Context, kind, content, scope, workspace, workItemID, source string) (MemoryEntry, error) {
	path := k.GlobalMemoryPath()
	scopeLabel := "global"
	if scope == "work_item" && workspace != "" {
		path = WorkItemMemoryPath(workspace)
		scopeLabel = "work item " + workItemID
	} else {
		scope = "global"
		workItemID = ""
	}
	saved, err := k.AppendMemory(path, scopeLabel, kind, content)
	if err != nil {
		return saved, err
	}
	_, err = k.Append(ctx, EventInput{
		Source: source, Kind: "memory.promoted", WorkItemID: workItemID,
		Payload: Payload{"memoryId": saved.ID, "kind": kind, "scope": scope, "content": content, "path": path},
	})
	return saved, err
}

// Today is the local date.
func (k *Kernel) Today() string { return k.Now().Format("2006-01-02") }

// NowLine gives a role the current local time.
func NowLine(now time.Time) string {
	zone, _ := now.Zone()
	name := now.Location().String()
	if name == "Local" {
		name = zone
	}
	return fmt.Sprintf("Now: %s (%s)", now.Format("Monday, January 2, 2006 at 15:04"), name)
}
