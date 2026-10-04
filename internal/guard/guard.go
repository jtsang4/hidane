// Package guard is the capture phase of the work tree: before an agent's tool
// call runs it passes the built-in deny list (the few commands too dangerous
// for any agent), every policy file from the global one down through each
// ancestor workspace to its own, and the pending-input check — any of them can
// refuse it. Rules are data, never prompts: a model cannot talk its way past
// them. Everything else is the agent CLI's own business: hidane runs them in
// their bypass mode and fences no directory.
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
// Not here, though they mostly look: sort (-o), uniq (an output file argument)
// and sed (its w command) can write.
// cd changes no file: read as a change, `cd <workspace> && cat a; cat b` was
// refused by a rule meant for writes.
var readOnlyHead = regexp.MustCompile(`^\s*(ls|cat|head|tail|grep|rg|find|pwd|wc|file|stat|tree|du|which|echo|printf|true|false|test|\[|basename|dirname|realpath|readlink|diff|cmp|cut|tr|jq|shasum|sha256sum|md5|md5sum|cd|pushd|popd|read|git\s+(status|log|diff|show|branch))(\s|$)[^;&|>]*$`)

// Read-only commands that can still change things through a flag.
var (
	findActs   = regexp.MustCompile(`^\s*find\b.*\s-(delete|exec|execdir|ok|okdir|fprint|fprint0|fprintf|fls)\b`)
	branchActs = regexp.MustCompile(`^\s*git\s+branch\b.*\s(-[dDmMcCfu]\b|--(delete|move|copy|force|set-upstream-to|unset-upstream|edit-description)\b)`)
	treeOut    = regexp.MustCompile(`^\s*tree\b.*\s-o\b`)
)

// IsReadOnly reports whether a shell command only looks. A command
// substitution or an acting flag makes any command a change. Commands joined
// with ;, &&, ||, | or a newline, and the parts of an if, for or while, are
// read-only when every part is: a multi-line look was refused under a rule,
// and the worker concluded it could not even name the file.
func IsReadOnly(cmd string) bool {
	if strings.ContainsAny(cmd, "`") || strings.Contains(cmd, "$(") || strings.Contains(cmd, "<(") || strings.Contains(cmd, ">(") {
		return false
	}
	masked := maskQuoted(cmd)
	if masked == "" {
		return false
	}
	cmd, masked = dropHarmlessRedirections(cmd, masked)
	for _, span := range splitSpans(masked, readOnlySep) {
		seg, mseg := cmd[span[0]:span[1]], masked[span[0]:span[1]]
		if loc := shellKeyword.FindStringIndex(mseg); loc != nil {
			seg, mseg = seg[loc[1]:], mseg[loc[1]:]
		}
		if strings.TrimSpace(mseg) == "" || loopHeader.MatchString(mseg) || inputOnly.MatchString(mseg) {
			continue
		}
		if !readOnlyHead.MatchString(mseg) || findActs.MatchString(seg) || branchActs.MatchString(seg) || treeOut.MatchString(seg) {
			return false
		}
	}
	return true
}

var (
	readOnlySep = regexp.MustCompile(`&&|\|\||;|\||&|\r?\n`)
	// The words of a compound command; what follows them is judged as any command.
	shellKeyword = regexp.MustCompile(`^\s*(if|then|elif|else|fi|do|done|while|until)(\s+|$)`)
	loopHeader   = regexp.MustCompile(`^\s*for\s+\w+(\s+in\b[^;&|>]*)?$`)
	// `done < notes.md`: what is left of a loop's end is where it reads from.
	inputOnly = regexp.MustCompile(`^\s*<\s*[^<>;&|]+$`)
)

// A redirection into a device or onto another descriptor (2>&1, 2>/dev/null)
// writes no file. Agents append them to plain looks; read as writes, they got
// `ls .hidane 2>&1` refused as a change to .hidane.
var harmlessRedirection = regexp.MustCompile(`(?:[0-9]?>>?|&>>?)\s*(?:&[0-9]+-?|&-|/dev/(?:null|stdout|stderr|tty))`)

// dropHarmlessRedirections blanks those redirections in both the command and
// its masked form, keeping every offset.
func dropHarmlessRedirections(cmd, masked string) (string, string) {
	c, m := []byte(cmd), []byte(masked)
	for _, loc := range harmlessRedirection.FindAllStringIndex(masked, -1) {
		if end := loc[1]; end < len(m) && !strings.ContainsRune(" \t;&|)", rune(m[end])) {
			continue // `2>/dev/nullx` names a file
		}
		for i := loc[0]; i < loc[1]; i++ {
			c[i], m[i] = ' ', ' '
		}
	}
	return string(c), string(m)
}

// maskQuoted blanks what is inside quotes, keeping every offset, so a `>` or
// `;` in quoted text is not read as shell syntax. "" means the quotes do not
// close: nothing can then be read off the command reliably.
func maskQuoted(cmd string) string {
	b := []byte(cmd)
	var quote byte
	for i := 0; i < len(b); i++ {
		c := b[i]
		switch {
		case quote == 0 && (c == '\'' || c == '"'):
			quote = c
		case quote != 0 && c == quote:
			quote = 0
		case quote == '"' && c == '\\' && i+1 < len(b):
			b[i], b[i+1] = '_', '_'
			i++
		case quote != 0:
			b[i] = '_'
		}
	}
	if quote != 0 {
		return ""
	}
	return string(b)
}

// splitSpans cuts at the separators found in the masked command; the spans
// index the original command too.
func splitSpans(masked string, sep *regexp.Regexp) [][2]int {
	var out [][2]int
	start := 0
	for _, m := range sep.FindAllStringIndex(masked, -1) {
		out = append(out, [2]int{start, m[0]})
		start = m[1]
	}
	return append(out, [2]int{start, len(masked)})
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
}

const (
	EnvPolicyFiles  = "HIDANE_POLICY_FILES"
	EnvPendingInput = "HIDANE_PENDING_INPUT_FILE"
	EnvBlocksFile   = "HIDANE_POLICY_BLOCKS_FILE"
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
	}
}

// Vars renders the environment for a subprocess.
func (e Env) Vars() []string {
	return []string{
		EnvPolicyFiles + "=" + strings.Join(e.PolicyFiles, "\n"),
		EnvPendingInput + "=" + e.PendingInputFile,
		EnvBlocksFile + "=" + e.BlocksFile,
	}
}

// Evaluate decides one call.
func Evaluate(call Call, env Env) Decision {
	if call.Tool == "bash" {
		for _, re := range deny {
			if re.MatchString(call.Subject) {
				return Decision{Block: true, Policy: true, Reason: fmt.Sprintf("blocked by hidane guard: %s", re.String())}
			}
		}
	}
	isMutating := mutating[call.Tool] && !(call.Tool == "bash" && IsReadOnly(call.Subject))
	for _, path := range env.PolicyFiles {
		file, err := Load(path)
		if err != nil && isMutating {
			return Decision{Block: true, Policy: true, Reason: fmt.Sprintf("blocked by hidane guard: policy file %s is unreadable (%v); fix it before changing anything", path, err)}
		}
		for _, rule := range file.Rules {
			tools := rule.Tools
			applies := false
			if len(tools) == 0 {
				// Changes only: a shell command that just looks (ls, cat, grep)
				// is a read, like the read tool. A rule meant for reads names
				// its tools.
				applies = isMutating
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
		// Recorded under the CLI's own name, matching its side_effect events.
		RecordBlock(env, Call{Tool: in.ToolName, Subject: call.Subject}, d)
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
