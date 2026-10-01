// Package guard is the capture phase of the work tree: before a worker's tool
// call runs it passes the built-in deny list, every policy file from the
// global one down through each ancestor workspace to its own, and the
// pending-input check — any of them can refuse it. Rules are data, never
// prompts: a model cannot talk its way past them.
//
// The same evaluation backs all three agent CLIs: Claude Code and Codex call
// `hidane guard` as a PreToolUse hook, and pi calls it from a tiny extension.
package guard

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Rule is one capture-phase rule.
type Rule struct {
	ID string `json:"id"`
	// Pattern is a case-insensitive regular expression over the command, or the target path.
	Pattern string `json:"pattern"`
	Reason  string `json:"reason"`
	// Tools the rule applies to; all mutating tools when empty.
	Tools []string `json:"tools,omitempty"`
}

type File struct {
	Rules []Rule `json:"rules"`
}

// ReadFile returns the rules in a policy file; a missing or broken file has none.
func ReadFile(path string) File {
	f, _ := Load(path)
	return f
}

// Load reads a policy file. A missing file is no rules and no error; a file
// that exists but cannot be read or parsed is an error, so the guard can fail
// closed on it instead of silently dropping every rule it holds.
func Load(path string) (File, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return File{Rules: []Rule{}}, nil
	}
	if err != nil {
		return File{Rules: []Rule{}}, err
	}
	var raw struct {
		Rules []json.RawMessage `json:"rules"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return File{Rules: []Rule{}}, err
	}
	out := File{Rules: []Rule{}}
	for _, r := range raw.Rules {
		var rule Rule
		if json.Unmarshal(r, &rule) == nil && rule.ID != "" && rule.Pattern != "" && rule.Reason != "" {
			out.Rules = append(out.Rules, rule)
		}
	}
	return out, nil
}

func WriteFile(path string, f File) error {
	if f.Rules == nil {
		f.Rules = []Rule{}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

func compile(pattern string) (*regexp.Regexp, error) { return regexp.Compile("(?i)" + pattern) }

// ValidatePattern says why a pattern cannot be used, or "" when it compiles.
func ValidatePattern(pattern string) string {
	if strings.TrimSpace(pattern) == "" {
		return "pattern required"
	}
	if _, err := compile(pattern); err != nil {
		return err.Error()
	}
	return ""
}

var deny = []*regexp.Regexp{
	regexp.MustCompile(`(?i)rm\s+(-[a-z]*[rf][a-z]*\s+)+/(\s|$)`),
	regexp.MustCompile(`\bsudo\b`),
	regexp.MustCompile(`\bmkfs\b`),
	regexp.MustCompile(`\bdd\s+if=`),
	regexp.MustCompile(`:\(\)\s*\{\s*:\|:&\s*\};:`),
	regexp.MustCompile(`\bshutdown\b|\breboot\b`),
	regexp.MustCompile(`git\s+push\s+.*--force`),
}

var mutating = map[string]bool{"bash": true, "write": true, "edit": true}

// Shell commands that only look. Anything else counts as a change, which errs
// toward pausing: a read that waits one turn costs little, a write made against
// superseded instructions may not be undoable.
var readOnlyHead = regexp.MustCompile(`^\s*(ls|cat|head|tail|grep|rg|find|pwd|wc|file|stat|tree|du|which|echo|git\s+(status|log|diff|show|branch))\b[^;&|>]*$`)

// Read-only commands that can still change things through a flag.
var (
	findActs   = regexp.MustCompile(`^\s*find\b.*\s-(delete|exec|execdir|ok|okdir|fprint|fprint0|fprintf|fls)\b`)
	branchActs = regexp.MustCompile(`^\s*git\s+branch\b.*\s(-[dDmMcCfu]\b|--(delete|move|copy|force|set-upstream-to|unset-upstream|edit-description)\b)`)
	treeOut    = regexp.MustCompile(`^\s*tree\b.*\s-o\b`)
)

// IsReadOnly reports whether a shell command only looks. A second line, a
// command substitution or an acting flag makes any command a change.
func IsReadOnly(cmd string) bool {
	if strings.ContainsAny(cmd, "\n\r`") || strings.Contains(cmd, "$(") || strings.Contains(cmd, "<(") || strings.Contains(cmd, ">(") {
		return false
	}
	return readOnlyHead.MatchString(cmd) && !findActs.MatchString(cmd) && !branchActs.MatchString(cmd) && !treeOut.MatchString(cmd)
}

// Call is a tool call in canonical form: tool is bash | write | edit | <other>.
type Call struct {
	Tool    string
	Subject string
}

type Decision struct {
	Block  bool   `json:"block"`
	Reason string `json:"reason,omitempty"`
	// Policy marks a refusal by a rule (built-in or policy file), as opposed to
	// a pause for pending input; only those are reported as policy.blocked.
	Policy bool `json:"-"`
}

// Env is what the guard reads, passed to the agent CLI as environment.
type Env struct {
	PolicyFiles      []string
	PendingInputFile string
	BlocksFile       string
	ExtraDeny        []string
	// Workspace is the only place a worker may write.
	Workspace string
	// Protected is hidane's own data directory (policies, settings, the log):
	// no worker may change it, whatever its instructions say.
	Protected string
}

const (
	EnvPolicyFiles  = "HIDANE_POLICY_FILES"
	EnvPendingInput = "HIDANE_PENDING_INPUT_FILE"
	EnvBlocksFile   = "HIDANE_POLICY_BLOCKS_FILE"
	EnvExtraDeny    = "HIDANE_GUARD_DENY"
	EnvWorkspace    = "HIDANE_WORKSPACE"
	EnvProtected    = "HIDANE_PROTECTED_DIR"
)

func lines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

func EnvFromOS() Env {
	return Env{
		PolicyFiles:      lines(os.Getenv(EnvPolicyFiles)),
		PendingInputFile: os.Getenv(EnvPendingInput),
		BlocksFile:       os.Getenv(EnvBlocksFile),
		ExtraDeny:        lines(os.Getenv(EnvExtraDeny)),
		Workspace:        os.Getenv(EnvWorkspace),
		Protected:        os.Getenv(EnvProtected),
	}
}

// Vars renders the environment for a subprocess.
func (e Env) Vars() []string {
	return []string{
		EnvPolicyFiles + "=" + strings.Join(e.PolicyFiles, "\n"),
		EnvPendingInput + "=" + e.PendingInputFile,
		EnvBlocksFile + "=" + e.BlocksFile,
		EnvWorkspace + "=" + e.Workspace,
		EnvProtected + "=" + e.Protected,
	}
}

func resolved(path string) string {
	if r, err := filepath.EvalSymlinks(path); err == nil {
		return r
	}
	// A path that does not exist yet: resolve its deepest existing parent.
	dir, base := filepath.Split(filepath.Clean(path))
	if dir == "" || dir == path {
		return path
	}
	return filepath.Join(resolved(filepath.Clean(dir)), base)
}

func inside(root, path string) bool {
	return path == root || strings.HasPrefix(path, root+string(filepath.Separator))
}

// homeVariants are the spellings of a path a shell command may use.
func homeVariants(path string) []string {
	out := []string{path}
	if r := resolved(path); r != path {
		out = append(out, r)
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		for _, p := range append([]string{}, out...) {
			if rest, ok := strings.CutPrefix(p, home); ok && (rest == "" || strings.HasPrefix(rest, "/")) {
				out = append(out, "~"+rest, "$HOME"+rest, "${HOME}"+rest)
			}
		}
	}
	return out
}

// fileTarget resolves a file tool's path the way the CLIs do: pi expands a
// leading `~` and strips a leading `@`; a relative path is relative to the
// workspace. Returns "" for a path that cannot be resolved (`~otheruser`).
func fileTarget(target, workspace string) string {
	t := strings.TrimSpace(strings.ReplaceAll(target, "\u00a0", " "))
	t = strings.TrimPrefix(t, "@")
	if t == "~" || strings.HasPrefix(t, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		t = filepath.Join(home, strings.TrimPrefix(t, "~"))
	} else if strings.HasPrefix(t, "~") {
		return ""
	}
	if !filepath.IsAbs(t) {
		t = filepath.Join(workspace, t)
	}
	return t
}

var controlDir = regexp.MustCompile(`(^|[\s'"=:(])(\./)?\.hidane(/|[\s'"]|$)`)

// confinement keeps a worker's changes inside its workspace, out of the
// workspace's own control directory (.hidane: its policy, its traces), and
// away from hidane's data directory. File tools are checked exactly; shell
// commands can only be checked for naming those places.
func confinement(call Call, env Env) string {
	if env.Workspace == "" {
		return ""
	}
	ws := resolved(env.Workspace)
	control := filepath.Join(ws, ".hidane")
	switch call.Tool {
	case "write", "edit":
		for _, raw := range strings.Split(call.Subject, "\n") {
			if strings.TrimSpace(raw) == "" {
				continue
			}
			target := fileTarget(raw, env.Workspace)
			if target == "" {
				return fmt.Sprintf("blocked by hidane guard: cannot resolve %s; use a path inside this work item's workspace (%s)", strings.TrimSpace(raw), env.Workspace)
			}
			r := resolved(target)
			if !inside(ws, r) {
				return fmt.Sprintf("blocked by hidane guard: %s is outside this work item's workspace (%s); keep every file inside it", target, env.Workspace)
			}
			if inside(control, r) {
				return "blocked by hidane guard: .hidane holds this workspace's policy and traces; it is not for work products"
			}
		}
	case "bash":
		if IsReadOnly(call.Subject) {
			return ""
		}
		subject := call.Subject
		for _, v := range homeVariants(filepath.Join(env.Workspace, ".hidane")) {
			if strings.Contains(subject, v) {
				return "blocked by hidane guard: commands may not change .hidane, which holds this workspace's policy and traces"
			}
		}
		for _, v := range homeVariants(env.Workspace) {
			subject = strings.ReplaceAll(subject, v, "")
		}
		if controlDir.MatchString(subject) {
			return "blocked by hidane guard: commands may not change .hidane, which holds this workspace's policy and traces"
		}
		if env.Protected != "" {
			for _, v := range homeVariants(env.Protected) {
				if strings.Contains(subject, v) {
					return fmt.Sprintf("blocked by hidane guard: commands may not change hidane's data directory (%s); work inside this work item's workspace (%s)", env.Protected, env.Workspace)
				}
			}
		}
	}
	return ""
}

// Evaluate decides one call.
func Evaluate(call Call, env Env) Decision {
	if call.Tool == "bash" {
		for _, re := range deny {
			if re.MatchString(call.Subject) {
				return Decision{Block: true, Policy: true, Reason: fmt.Sprintf("blocked by hidane guard: %s", re.String())}
			}
		}
		for _, lit := range env.ExtraDeny {
			if strings.Contains(call.Subject, lit) {
				return Decision{Block: true, Policy: true, Reason: "blocked by hidane guard: " + lit}
			}
		}
	}
	if reason := confinement(call, env); reason != "" {
		return Decision{Block: true, Policy: true, Reason: reason}
	}
	isMutating := mutating[call.Tool] && !(call.Tool == "bash" && IsReadOnly(call.Subject))
	for _, path := range env.PolicyFiles {
		file, err := Load(path)
		if err != nil && mutating[call.Tool] {
			return Decision{Block: true, Policy: true, Reason: fmt.Sprintf("blocked by hidane guard: policy file %s is unreadable (%v); fix it before changing anything", path, err)}
		}
		for _, rule := range file.Rules {
			tools := rule.Tools
			applies := false
			if len(tools) == 0 {
				applies = mutating[call.Tool]
			} else {
				for _, t := range tools {
					if strings.EqualFold(t, call.Tool) {
						applies = true
					}
				}
			}
			if !applies {
				continue
			}
			re, err := compile(rule.Pattern)
			if err != nil || !re.MatchString(call.Subject) {
				continue
			}
			return Decision{Block: true, Policy: true, Reason: fmt.Sprintf("blocked by hidane policy %s: %s", rule.ID, rule.Reason)}
		}
	}
	if isMutating && env.PendingInputFile != "" {
		if _, err := os.Stat(env.PendingInputFile); err == nil {
			return Decision{Block: true, Reason: "hidane: the user has sent new input that you have not read yet. Do not change anything now; it arrives before your next step — re-plan with it first."}
		}
	}
	return Decision{}
}

// Normalize maps a CLI's tool name and input to a canonical call.
func Normalize(toolName string, input map[string]any) Call {
	s := func(keys ...string) string {
		for _, k := range keys {
			if v, ok := input[k].(string); ok && v != "" {
				return v
			}
		}
		return ""
	}
	name := strings.ToLower(toolName)
	switch name {
	case "bash", "shell", "exec_command", "local_shell", "powershell":
		cmd := s("command", "cmd")
		if cmd == "" {
			if arr, ok := input["command"].([]any); ok {
				parts := make([]string, 0, len(arr))
				for _, p := range arr {
					parts = append(parts, fmt.Sprint(p))
				}
				cmd = strings.Join(parts, " ")
			}
		}
		return Call{Tool: "bash", Subject: cmd}
	case "write", "create":
		return Call{Tool: "write", Subject: s("file_path", "path")}
	case "edit", "multiedit", "notebookedit", "str_replace":
		return Call{Tool: "edit", Subject: s("file_path", "path", "notebook_path")}
	case "apply_patch":
		subject := s("patch", "input", "command")
		if p := patchPaths(subject); p != "" {
			subject = p
		}
		return Call{Tool: "edit", Subject: subject}
	}
	return Call{Tool: name, Subject: s("file_path", "path", "command", "pattern", "url")}
}

var patchFile = regexp.MustCompile(`(?m)^\*\*\* (?:(?:Add|Update|Delete) File|Move to): (.+)$`)

func patchPaths(patch string) string {
	var out []string
	for _, m := range patchFile.FindAllStringSubmatch(patch, -1) {
		out = append(out, strings.TrimSpace(m[1]))
	}
	return strings.Join(out, "\n")
}

// RecordBlock appends a refusal to the blocks file so the execution can report it.
func RecordBlock(env Env, call Call, d Decision) {
	if env.BlocksFile == "" || !d.Policy {
		return
	}
	f, err := os.OpenFile(env.BlocksFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	b, _ := json.Marshal(map[string]string{"tool": call.Tool, "reason": d.Reason})
	_, _ = f.Write(append(b, '\n'))
}

// Block is one refusal recorded during an execution.
type Block struct {
	Tool   string `json:"tool"`
	Reason string `json:"reason"`
}

// ReadBlocks returns the refusals recorded in a blocks file.
func ReadBlocks(path string) []Block {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var out []Block
	for _, l := range strings.Split(string(b), "\n") {
		var blk Block
		if json.Unmarshal([]byte(l), &blk) == nil && blk.Reason != "" {
			out = append(out, blk)
		}
	}
	return out
}

// RunHook implements `hidane guard --format <claude|codex|pi>`: read the CLI's
// hook input on stdin, decide, answer in that CLI's protocol. Returns the exit code.
//
// A guard that cannot read its input fails closed: refusing one tool call is
// recoverable, letting an unchecked one through may not be.
func RunHook(format string, stdin io.Reader, stdout, stderr io.Writer, env Env) int {
	raw, err := io.ReadAll(stdin)
	var in struct {
		ToolName  string         `json:"tool_name"`
		ToolInput map[string]any `json:"tool_input"`
	}
	if err == nil {
		err = json.Unmarshal(raw, &in)
	}
	if err != nil {
		fmt.Fprintf(stderr, "hidane guard: unreadable hook input: %v\n", err)
		if format == "pi" {
			_ = json.NewEncoder(stdout).Encode(Decision{Block: true, Reason: "hidane guard: unreadable hook input"})
			return 0
		}
		return 2
	}
	if in.ToolInput == nil {
		in.ToolInput = map[string]any{}
	}
	call := Normalize(in.ToolName, in.ToolInput)
	d := Evaluate(call, env)
	if d.Block {
		RecordBlock(env, call, d)
		if d.Policy {
			// Said to the model only: a refused compound command was once
			// reported as half done, when none of it had run.
			d.Reason += " — nothing in this tool call ran; do the parts that are allowed as separate calls."
		}
	}
	switch format {
	case "pi":
		_ = json.NewEncoder(stdout).Encode(d)
		return 0
	case "claude":
		if !d.Block {
			return 0
		}
		_ = json.NewEncoder(stdout).Encode(map[string]any{
			"hookSpecificOutput": map[string]any{
				"hookEventName":            "PreToolUse",
				"permissionDecision":       "deny",
				"permissionDecisionReason": d.Reason,
			},
		})
		return 0
	default:
		// Exit code 2 with the reason on stderr is the hook protocol's
		// universal "block" signal.
		if !d.Block {
			return 0
		}
		fmt.Fprintln(stderr, d.Reason)
		return 2
	}
}
