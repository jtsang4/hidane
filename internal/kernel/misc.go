package kernel

import (
	"context"
	"database/sql"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Read access to a work item's workspace. Worker output exists only on disk;
// without this the only way to read a produced file was to ask the agent to
// paste it back into chat.

var skipDirs = map[string]bool{".git": true, "node_modules": true, ".hidane": true, "__pycache__": true, ".venv": true}

const maxEntries = 500

// MaxInlineBytes: text beyond this is a download, not inline content.
const MaxInlineBytes = 512 * 1024

type ArtifactEntry struct {
	Path       string `json:"path"`
	Size       int64  `json:"size"`
	ModifiedAt string `json:"modifiedAt"`
}

// ResolveInside resolves a workspace-relative path, or "" when it would escape.
// This is the security boundary of the feature: the path comes from a URL.
// Comparing fully resolved paths (symlinks included) is what makes `..`,
// absolute, encoded and symlinked variants all fail.
func ResolveInside(workspace, requested string) string {
	root, err := filepath.Abs(workspace)
	if err != nil {
		return ""
	}
	if r, err := filepath.EvalSymlinks(root); err == nil {
		root = r
	}
	if filepath.IsAbs(requested) {
		return ""
	}
	target := resolveExisting(filepath.Join(root, requested))
	if target == root {
		return target
	}
	if strings.HasPrefix(target, root+string(filepath.Separator)) {
		return target
	}
	return ""
}

// resolveExisting resolves symlinks in the deepest existing ancestor of path,
// so a link inside the workspace cannot point a not-yet-existing child outside it.
func resolveExisting(path string) string {
	rest := ""
	cur := filepath.Clean(path)
	for {
		if r, err := filepath.EvalSymlinks(cur); err == nil {
			if rest == "" {
				return r
			}
			return filepath.Join(r, rest)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return path
		}
		rest = filepath.Join(filepath.Base(cur), rest)
		cur = parent
	}
}

// ListArtifacts is a flat listing of the workspace's files, newest first.
func ListArtifacts(workspace string) []ArtifactEntry {
	root, _ := filepath.Abs(workspace)
	out := []ArtifactEntry{}
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if d != nil && d.IsDir() && path != root {
				return fs.SkipDir
			}
			return nil
		}
		if len(out) >= maxEntries {
			return fs.SkipAll
		}
		if path == root {
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			if strings.HasPrefix(name, ".") || skipDirs[name] {
				return fs.SkipDir
			}
			return nil
		}
		if skipDirs[name] || !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		out = append(out, ArtifactEntry{Path: filepath.ToSlash(rel), Size: info.Size(), ModifiedAt: FormatTime(info.ModTime())})
		return nil
	})
	sort.SliceStable(out, func(i, j int) bool { return out[i].ModifiedAt > out[j].ModifiedAt })
	return out
}

var textExtensions = map[string]bool{}

func init() {
	for _, e := range strings.Fields("md txt json yaml yml toml csv log html xml js ts tsx jsx py sh rs go java c h cpp sql css env ini conf gitignore svelte") {
		textExtensions[e] = true
	}
}

// LooksTextual: extensionless files in a workspace are usually scripts or notes.
func LooksTextual(path string) bool {
	name := filepath.Base(path)
	ext := ""
	if i := strings.LastIndex(name, "."); i >= 0 {
		ext = strings.ToLower(name[i+1:])
	}
	return ext == "" || textExtensions[ext]
}

type ArtifactContent struct {
	Path   string  `json:"path"`
	Size   int64   `json:"size"`
	Text   *string `json:"text,omitempty"`
	Reason string  `json:"reason,omitempty"`
}

// ReadArtifact returns a file's content, or nil when it is not a readable file inside the workspace.
func ReadArtifact(workspace, requested string) *ArtifactContent {
	target := ResolveInside(workspace, requested)
	if target == "" {
		return nil
	}
	info, err := os.Stat(target)
	if err != nil || !info.Mode().IsRegular() {
		return nil
	}
	path := filepath.ToSlash(requested)
	if !LooksTextual(path) {
		return &ArtifactContent{Path: path, Size: info.Size(), Reason: "binary"}
	}
	if info.Size() > MaxInlineBytes {
		return &ArtifactContent{Path: path, Size: info.Size(), Reason: "too-large"}
	}
	b, err := os.ReadFile(target)
	if err != nil {
		return nil
	}
	text := string(b)
	return &ArtifactContent{Path: path, Size: info.Size(), Text: &text}
}

// ChannelBinding maps runtime threads onto channel-native structures. Feishu:
// main thread ↔ p2p chat top level; work item thread ↔ the reply thread under
// a bot-posted root message.
type ChannelBinding struct {
	ID         string
	Channel    string
	Kind       string // main | work_item
	WorkItemID string
	ChatID     string
	RootID     string
}

const bindingCols = `id, channel, kind, work_item_id, chat_id, root_id`

func scanBinding(s scanner) (ChannelBinding, error) {
	var b ChannelBinding
	var item, root sql.NullString
	err := s.Scan(&b.ID, &b.Channel, &b.Kind, &item, &b.ChatID, &root)
	b.WorkItemID, b.RootID = str(item), str(root)
	return b, err
}

func (k *Kernel) CreateBinding(ctx context.Context, b ChannelBinding) (ChannelBinding, error) {
	b.ID = GenID("cb", 6)
	_, err := k.DB.ExecContext(ctx, `INSERT INTO channel_bindings (id, channel, kind, work_item_id, chat_id, root_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, b.ID, b.Channel, b.Kind, nullable(b.WorkItemID), b.ChatID, nullable(b.RootID), k.stamp())
	return b, err
}

func (k *Kernel) findBinding(ctx context.Context, q string, args ...any) (ChannelBinding, bool, error) {
	b, err := scanBinding(k.DB.QueryRowContext(ctx, `SELECT `+bindingCols+` FROM channel_bindings `+q, args...))
	if err == sql.ErrNoRows {
		return b, false, nil
	}
	return b, err == nil, err
}

// FindByChannelRef answers inbound routing: which thread does a channel message belong to?
func (k *Kernel) FindByChannelRef(ctx context.Context, channel, chatID, rootID string) (ChannelBinding, bool, error) {
	if rootID != "" {
		return k.findBinding(ctx, `WHERE channel = ? AND chat_id = ? AND root_id = ?`, channel, chatID, rootID)
	}
	return k.findBinding(ctx, `WHERE channel = ? AND chat_id = ? AND root_id IS NULL`, channel, chatID)
}

func (k *Kernel) FindMainBinding(ctx context.Context, channel string) (ChannelBinding, bool, error) {
	return k.findBinding(ctx, `WHERE channel = ? AND kind = 'main' ORDER BY created_at DESC, rowid DESC LIMIT 1`, channel)
}

// FindByWorkItem answers outbound routing: where do a work item's replies go?
func (k *Kernel) FindByWorkItem(ctx context.Context, channel, workItemID string) (ChannelBinding, bool, error) {
	return k.findBinding(ctx, `WHERE channel = ? AND work_item_id = ? ORDER BY created_at DESC, rowid DESC LIMIT 1`, channel, workItemID)
}

// Triage: deterministic rules run before any model is woken; most connector
// events end here as "record".
type TriageRule struct {
	Name   string
	Match  func(Event) bool
	Action string // record | wake_primary
}

var DefaultTriageRules = []TriageRule{
	{Name: "heartbeat-record-only", Match: func(e Event) bool { return e.Kind == "connector.heartbeat" }, Action: "record"},
	{Name: "webhook-wakes-primary", Match: func(e Event) bool { return e.Kind == "connector.webhook" }, Action: "wake_primary"},
	// The wake decision for a scheduled poll is declared on its definition;
	// triage reads the declared hint. Connectors still never judge.
	{Name: "scheduled-http-wake-flag", Match: func(e Event) bool { return e.Kind == "connector.http" && e.Payload.Bool("wake") }, Action: "wake_primary"},
	{Name: "scheduled-http-record-only", Match: func(e Event) bool { return e.Kind == "connector.http" }, Action: "record"},
}

func TriageEvent(e Event, rules []TriageRule) (string, string) {
	for _, r := range rules {
		if r.Match(e) {
			return r.Name, r.Action
		}
	}
	return "default-record", "record"
}

// NeedsTriage: only connector events are triaged; agent and kernel events never
// re-enter triage (loop protection).
func NeedsTriage(e Event) bool {
	return strings.HasPrefix(e.Source, "connector:") && strings.HasPrefix(e.Kind, "connector.")
}

// Bubbling kinds travel up the work tree one level at a time. Like DOM events
// this is declared per kind: progress and tool traffic stay where they happen,
// because waking a parent's model for each would cost a model call per tool call.
var bubblingKinds = map[string]bool{"escalation.raised": true, "message.reroute_requested": true}

func Bubbles(kind string) bool { return bubblingKinds[kind] }
