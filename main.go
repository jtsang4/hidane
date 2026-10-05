// Command hidane (火种) is a persistent personal agent runtime. Without
// arguments it opens the desktop app; subcommands run it headless or inspect it.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/jtsang4/hidane/internal/agents"
	"github.com/jtsang4/hidane/internal/app"
	"github.com/jtsang4/hidane/internal/clip"
	"github.com/jtsang4/hidane/internal/config"
	"github.com/jtsang4/hidane/internal/guard"
	"github.com/jtsang4/hidane/internal/kernel"
	"github.com/jtsang4/hidane/internal/projections"
	"github.com/jtsang4/hidane/internal/settings"
)

const usage = `hidane (火种) — persistent personal agent runtime

Usage:
  hidane                      open the desktop app
  hidane serve [--addr A]     run headless: agent loop, connectors, web UI and API over HTTP
  hidane chat <message...>    send a message and follow the answers [--item ID] [--timeout SEC]
                              [--agent claude|codex|pi [--provider ID] [--model M] [--effort E]]
  hidane items [--all]        list work items
  hidane events [--tail N] [--thread ID] [--item ID]
  hidane log [DAY] [--write]  render the daily worklog projection
  hidane archive [DAY]        archive a day: worklog + session traces
  hidane distill [--min N]    run one memory distillation pass
  hidane model [--ping] [--role R]  show which CLI/provider/model each role uses; --ping calls it once
  hidane agents               detect the local claude / codex / pi CLIs
  hidane memories [--ids]     print the memory files (global, then each work item's)
  hidane forget <id>          remove a memory entry (the expiry channel)
  hidane version
`

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		if err := runGUI(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	cmd, rest := args[0], args[1:]
	var err error
	switch cmd {
	case "guard":
		os.Exit(guardCmd(rest))
	case "serve":
		err = serveCmd(rest)
	case "chat":
		err = chatCmd(rest)
	case "items":
		err = itemsCmd(rest)
	case "events":
		err = eventsCmd(rest)
	case "log":
		err = logCmd(rest)
	case "archive":
		err = archiveCmd(rest)
	case "distill":
		err = distillCmd(rest)
	case "model":
		err = modelCmd(rest)
	case "agents":
		err = agentsCmd()
	case "memories":
		err = memoriesCmd(rest)
	case "forget":
		err = forgetCmd(rest)
	case "version", "--version", "-v":
		fmt.Println("hidane", app.Version)
	case "help", "--help", "-h":
		fmt.Print(usage)
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// guardCmd is the PreToolUse hook every agent CLI calls before a tool runs.
func guardCmd(args []string) int {
	fs := flag.NewFlagSet("guard", flag.ContinueOnError)
	format := fs.String("format", "claude", "hook protocol: claude | codex | pi")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	return guard.RunHook(*format, os.Stdin, os.Stdout, os.Stderr, guard.EnvFromOS())
}

func open(loginShell bool) (*app.App, error) {
	return app.Open(config.Load(), app.Options{LoginShell: loginShell})
}

func randomToken() string {
	b := make([]byte, 20)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func serveCmd(args []string) error {
	cfg := config.Load()
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	addr := fs.String("addr", cfg.Addr, "listen address")
	_ = fs.Parse(args)
	a, err := app.Open(cfg, app.Options{})
	if err != nil {
		return err
	}
	defer a.Close()
	lost, err := a.Start()
	if err != nil {
		return err
	}
	defer a.Stop()
	// /api/* is never open over the network: without a configured token one
	// is generated for this run and printed with the address.
	token := cfg.APIToken
	generated := false
	if token == "" {
		token, generated = randomToken(), true
	}
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: a.Handler(app.HandlerOptions{Token: token}), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("http: %v", err)
		}
	}()
	url := "http://" + ln.Addr().String()
	fmt.Printf("hidane serving on %s (home %s, workers %d, turns %d)\n", url, cfg.Home, cfg.MaxWorkers, cfg.MaxConcurrentTurns)
	if generated {
		fmt.Printf("open %s/?token=%s\n", url, token)
	}
	if lost > 0 {
		fmt.Printf("%d execution(s) lost to the last restart were reported to their owners\n", lost)
	}
	for _, role := range settings.Roles {
		fmt.Printf("  %-9s %s\n", role, a.Settings.Get().Resolve(role).Describe())
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return srv.Shutdown(ctx)
}

// chatCmd posts a message and prints the answers until its chain goes quiet.
// Without a running app it runs the loop itself; with one, it only posts.
func chatCmd(args []string) error {
	fs := flag.NewFlagSet("chat", flag.ExitOnError)
	item := fs.String("item", "", "address the message to a work item (comma-separated: to several at once)")
	timeout := fs.Int("timeout", 900, "stop following after this many seconds")
	agent := fs.String("agent", "", "run the task on this CLI (claude | codex | pi) instead of the role settings")
	provider := fs.String("provider", "", "with --agent: a provider id from settings (default: the CLI's own login)")
	model := fs.String("model", "", "with --agent: the model (default: the CLI's default)")
	effort := fs.String("effort", "", "with --agent: the reasoning effort")
	_ = fs.Parse(args)
	text := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if text == "" {
		return errors.New("message required")
	}
	a, err := open(false)
	if err != nil {
		return err
	}
	defer a.Close()
	var runAs *kernel.RunAs
	if *agent != "" {
		runAs = &kernel.RunAs{Agent: *agent, Provider: *provider, Model: *model, Effort: *effort}
		if err := a.Settings.Get().ValidateRun(settings.RoleConfig{Agent: *agent, Provider: *provider, Model: *model, Effort: *effort}); err != nil {
			return err
		}
	} else if *provider != "" || *model != "" || *effort != "" {
		return errors.New("--provider, --model and --effort need --agent")
	}
	if _, err := a.Start(); err == nil {
		defer a.Stop()
	} else if !errors.Is(err, app.ErrLocked) {
		return err
	}
	ctx := context.Background()
	var targets []string
	for _, id := range strings.Split(*item, ",") {
		if id = strings.TrimSpace(id); id != "" {
			targets = append(targets, id)
		}
	}
	msg, err := a.Sys.SubmitMessage(ctx, agents.InboundMessage{Text: text, Source: "connector:cli", Targets: targets, RunAs: runAs})
	if err != nil {
		return err
	}
	return follow(ctx, a, msg.ID, time.Duration(*timeout)*time.Second)
}

func follow(ctx context.Context, a *app.App, messageID string, timeout time.Duration) error {
	seen := map[string]bool{}
	started := time.Now()
	// Quiet means nothing could still produce an answer: no execution, no
	// unread mail, no turn in flight. Each is briefly empty in the hand-offs
	// between them (an execution ends before its outcome is posted), so the
	// whole system must stay quiet for a while, not just the root's events.
	quietSince := time.Now()
	wake, cancel := a.K.Hub.Subscribe()
	defer cancel()
	for time.Since(started) < timeout {
		events, err := a.K.ListEvents(ctx, kernel.ListFilter{Conversation: true, Tail: 200})
		if err != nil {
			return err
		}
		answered := false
		for _, e := range events {
			if e.Payload.Str("root") != messageID && e.Payload.Str("of") != messageID {
				continue
			}
			// A steer note says where the words went, not what came of them.
			if e.Kind != "execution.steered" && slices.Contains(kernel.AnswerKinds, e.Kind) {
				answered = true
			}
			if seen[e.ID] {
				continue
			}
			seen[e.ID] = true
			quietSince = time.Now()
			label := e.Kind
			if e.Kind == "message.attributed" {
				label = "→ " + e.Payload.Str("workItemId")
			} else if e.WorkItemID != "" {
				label += " " + e.WorkItemID
			}
			fmt.Printf("\n[%s]\n", label)
			for _, key := range []string{"text", "question", "error"} {
				if v := e.Payload.Str(key); v != "" {
					fmt.Println(v)
					break
				}
			}
		}
		active, _ := a.K.ActiveExecutions(ctx)
		pending, _ := a.K.MailboxesWithPending(ctx)
		// Without a runtime here (the app or `serve` holds it), the other
		// process's turns still show as pending mail until they commit.
		turning := a.Runtime != nil && len(a.Runtime.ActiveTurns()) > 0
		if len(active) > 0 || len(pending) > 0 || turning {
			quietSince = time.Now()
		}
		if answered && time.Since(quietSince) > 2*time.Second {
			return nil
		}
		select {
		case <-wake:
		case <-time.After(500 * time.Millisecond):
		}
	}
	fmt.Println("\n(still working — follow it in the app or with `hidane events`)")
	return nil
}

func itemsCmd(args []string) error {
	fs := flag.NewFlagSet("items", flag.ExitOnError)
	all := fs.Bool("all", false, "include non-open items")
	_ = fs.Parse(args)
	a, err := open(false)
	if err != nil {
		return err
	}
	defer a.Close()
	status := kernel.StatusOpen
	if *all {
		status = ""
	}
	items, err := a.K.ListWorkItems(context.Background(), status)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		fmt.Println("(no work items)")
	}
	for _, i := range items {
		fmt.Printf("%s  [%s]  %s\n  thread: %s  workspace: %s\n", i.ID, i.Status, i.Title, i.ThreadID, i.Workspace)
	}
	return nil
}

func eventsCmd(args []string) error {
	fs := flag.NewFlagSet("events", flag.ExitOnError)
	tail := fs.Int("tail", 20, "last n events")
	thread := fs.String("thread", "", "filter by thread")
	item := fs.String("item", "", "filter by work item")
	_ = fs.Parse(args)
	a, err := open(false)
	if err != nil {
		return err
	}
	defer a.Close()
	events, err := a.K.ListEvents(context.Background(), kernel.ListFilter{Tail: *tail, ThreadID: *thread, WorkItemID: *item})
	if err != nil {
		return err
	}
	for _, e := range events {
		b, _ := json.Marshal(e.Payload)
		brief := clip.Runes(string(b), 120)
		tag := ""
		if e.WorkItemID != "" {
			tag = " [" + e.WorkItemID + "]"
		}
		fmt.Printf("#%d %s %s (%s)%s %s\n", e.Seq, e.TS, e.Kind, e.Source, tag, brief)
	}
	return nil
}

func dayArg(a *app.App, fs *flag.FlagSet) string {
	if fs.NArg() > 0 {
		return fs.Arg(0)
	}
	return a.K.Today()
}

func logCmd(args []string) error {
	fs := flag.NewFlagSet("log", flag.ExitOnError)
	write := fs.Bool("write", false, "write to the worklogs directory")
	_ = fs.Parse(reorder(args))
	a, err := open(false)
	if err != nil {
		return err
	}
	defer a.Close()
	day := dayArg(a, fs)
	if *write {
		path, err := projections.WriteDay(context.Background(), a.K, day)
		if err != nil {
			return err
		}
		fmt.Println(path)
		return nil
	}
	md, _, err := projections.RenderDay(context.Background(), a.K, day)
	if err != nil {
		return err
	}
	fmt.Print(md)
	return nil
}

// reorder lets flags follow positional arguments (`log 2026-10-01 --write`).
func reorder(args []string) []string {
	var flags, pos []string
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			flags = append(flags, a)
		} else {
			pos = append(pos, a)
		}
	}
	return append(flags, pos...)
}

func archiveCmd(args []string) error {
	fs := flag.NewFlagSet("archive", flag.ExitOnError)
	_ = fs.Parse(args)
	a, err := open(false)
	if err != nil {
		return err
	}
	defer a.Close()
	dir, n, err := projections.ArchiveDay(context.Background(), a.K, dayArg(a, fs))
	if err != nil {
		return err
	}
	fmt.Printf("%s (%d session files)\n", dir, n)
	return nil
}

func distillCmd(args []string) error {
	fs := flag.NewFlagSet("distill", flag.ExitOnError)
	min := fs.Int("min", 1, "minimum meaningful events required")
	_ = fs.Parse(args)
	a, err := open(true)
	if err != nil {
		return err
	}
	defer a.Close()
	res, err := a.Sys.RunDistillation(context.Background(), *min)
	if err != nil {
		return err
	}
	b, _ := json.Marshal(res)
	fmt.Println(string(b))
	return nil
}

func modelCmd(args []string) error {
	fs := flag.NewFlagSet("model", flag.ExitOnError)
	ping := fs.Bool("ping", false, "make one real request")
	role := fs.String("role", "", "only this role")
	_ = fs.Parse(args)
	a, err := open(true)
	if err != nil {
		return err
	}
	defer a.Close()
	roles := settings.Roles
	if *role != "" {
		roles = []string{*role}
	}
	failed := false
	for _, r := range roles {
		fmt.Printf("%-9s %s\n", r, a.Settings.Get().Resolve(r).Describe())
		if *ping {
			res, _ := a.Sys.Ping(context.Background(), r)
			if !res.OK || strings.TrimSpace(res.Text) == "" {
				failed = true
				fmt.Printf("          ping failed after %dms: %s\n", res.DurationMs, res.Error)
			} else {
				fmt.Printf("          ping ok in %dms: %s\n", res.DurationMs, strings.TrimSpace(res.Text))
			}
		}
	}
	if failed {
		return errors.New("ping failed")
	}
	return nil
}

func agentsCmd() error {
	a, err := open(true)
	if err != nil {
		return err
	}
	defer a.Close()
	for _, d := range a.Detect(context.Background()) {
		if d.Available {
			fmt.Printf("%-7s ok   %s (%s)\n", d.Kind, d.Version, d.Path)
		} else {
			fmt.Printf("%-7s --   %s\n", d.Kind, d.Error)
		}
	}
	return nil
}

func memoriesCmd(args []string) error {
	fs := flag.NewFlagSet("memories", flag.ExitOnError)
	ids := fs.Bool("ids", false, "list entries with their ids")
	_ = fs.Parse(args)
	cfg := config.Load()
	k, err := kernel.Open(cfg)
	if err != nil {
		return err
	}
	defer k.Close()
	text := kernel.ReadTextFile(k.GlobalMemoryPath())
	items, err := k.WorkItemMemories(context.Background())
	if err != nil {
		return err
	}
	if strings.TrimSpace(text) == "" && len(items) == 0 {
		fmt.Println("(empty)", k.GlobalMemoryPath())
		return nil
	}
	layers := append([]kernel.MemoryLayer{{Scope: "global", Path: k.GlobalMemoryPath(), Entries: kernel.ParseMemories(text)}}, items...)
	printed := 0
	for _, l := range layers {
		if l.Scope == "global" && strings.TrimSpace(text) == "" {
			continue
		}
		if printed > 0 {
			fmt.Println()
		}
		printed++
		if !*ids {
			if l.Scope != "global" {
				fmt.Printf("<!-- %s: %s -->\n", l.WorkItemID, l.Path)
			}
			fmt.Print(kernel.ReadTextFile(l.Path))
			continue
		}
		label := "global"
		if l.Scope != "global" {
			label = l.WorkItemID + " " + l.Title
		}
		fmt.Printf("# %s (%s)\n", label, l.Path)
		for _, m := range l.Entries {
			fmt.Printf("%s  [%s] (%s) %s\n", m.ID, m.Kind, m.Date, m.Content)
		}
	}
	return nil
}

func forgetCmd(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: hidane forget <id>")
	}
	k, err := kernel.Open(config.Load())
	if err != nil {
		return err
	}
	defer k.Close()
	ok, err := k.Forget(context.Background(), args[0], "cli")
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("not found: %s", args[0])
	}
	fmt.Println("forgot", args[0])
	return nil
}
