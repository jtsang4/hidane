package kernel

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

// TurnHandler runs one turn of one agent loop: every message that piled up in
// its mailbox since the last turn, handled together. It decides and returns; it
// never waits on long work — that is dispatched, and its outcome comes back
// later as another message.
type TurnHandler func(ctx context.Context, address string, messages []Event) error

type idleTask struct {
	name    string
	run     func(context.Context) error
	min     time.Duration
	max     time.Duration
	lastRun time.Time
	running bool
}

type timerTask struct {
	name    string
	run     func(context.Context) error
	every   time.Duration
	lastRun time.Time
	running bool
}

type handlerEntry struct {
	prefix  string
	handler TurnHandler
}

// Runtime is the event loop. Mailboxes are the task queues, turns run to
// completion, and priority is: interrupt-lane mailboxes, then normal, then idle
// tasks only once nothing is pending or running.
type Runtime struct {
	k        *Kernel
	maxTurns int
	pollEvery time.Duration

	mu        sync.Mutex
	handlers  []handlerEntry
	active    map[string]chan struct{}
	idle      []*idleTask
	timers    []*timerTask
	unhandled map[string]bool
	ticking   bool
	again     bool
	stopped   bool
	ctx       context.Context
	cancel    context.CancelFunc
	bg        sync.WaitGroup
	stopLoop  chan struct{}
}

func NewRuntime(k *Kernel, maxTurns int) *Runtime {
	if maxTurns <= 0 {
		maxTurns = 4
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Runtime{
		k: k, maxTurns: maxTurns, pollEvery: 5 * time.Second,
		active: map[string]chan struct{}{}, unhandled: map[string]bool{},
		stopped: true, ctx: ctx, cancel: cancel,
	}
}

// Register handles every mailbox whose address equals prefix or starts with it.
func (r *Runtime) Register(prefix string, h TurnHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handlers = append(r.handlers, handlerEntry{prefix, h})
}

// RegisterIdle adds work that runs when nothing is pending (or past max, regardless).
func (r *Runtime) RegisterIdle(name string, run func(context.Context) error, min, max time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.idle = append(r.idle, &idleTask{name: name, run: run, min: min, max: max, lastRun: time.Now()})
}

// RegisterTimer adds housekeeping that runs on a clock regardless of load.
func (r *Runtime) RegisterTimer(name string, run func(context.Context) error, every time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.timers = append(r.timers, &timerTask{name: name, run: run, every: every})
}

// Start begins scheduling on appends and on a fallback poll.
func (r *Runtime) Start() {
	r.mu.Lock()
	r.stopped = false
	r.stopLoop = make(chan struct{})
	stop := r.stopLoop
	r.mu.Unlock()
	wake, unsub := r.k.Hub.Subscribe()
	r.bg.Add(1)
	go func() {
		defer r.bg.Done()
		defer unsub()
		ticker := time.NewTicker(r.pollEvery)
		defer ticker.Stop()
		r.Kick()
		for {
			select {
			case <-stop:
				return
			case <-wake:
				r.Kick()
			case <-ticker.C:
				r.Kick()
			}
		}
	}()
}

// Stop ends scheduling and waits for in-flight turns and tasks.
func (r *Runtime) Stop() {
	r.mu.Lock()
	if r.stopped {
		r.mu.Unlock()
		return
	}
	r.stopped = true
	if r.stopLoop != nil {
		close(r.stopLoop)
	}
	r.mu.Unlock()
	r.cancel()
	r.bg.Wait()
}

// ActiveTurns lists mailboxes with a turn in flight.
func (r *Runtime) ActiveTurns() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.active))
	for a := range r.active {
		out = append(out, a)
	}
	return out
}

// Kick requests a scheduling pass; bursts coalesce into one.
func (r *Runtime) Kick() {
	r.mu.Lock()
	if r.stopped {
		r.mu.Unlock()
		return
	}
	if r.ticking {
		r.again = true
		r.mu.Unlock()
		return
	}
	r.ticking = true
	r.mu.Unlock()
	r.bg.Add(1)
	go func() {
		defer r.bg.Done()
		for {
			r.mu.Lock()
			r.again = false
			r.mu.Unlock()
			if err := r.tick(); err != nil {
				log.Printf("runtime tick failed: %v", err)
			}
			r.mu.Lock()
			if !r.again || r.stopped {
				r.ticking = false
				r.mu.Unlock()
				return
			}
			r.mu.Unlock()
		}
	}()
}

func (r *Runtime) handlerFor(address string) TurnHandler {
	for _, h := range r.handlers {
		if address == h.prefix || strings.HasPrefix(address, h.prefix) {
			return h.handler
		}
	}
	return nil
}

func (r *Runtime) waitActive() {
	r.mu.Lock()
	chans := make([]chan struct{}, 0, len(r.active))
	for _, c := range r.active {
		chans = append(chans, c)
	}
	r.mu.Unlock()
	for _, c := range chans {
		<-c
	}
}

// Drain runs until every handled mailbox is empty and no turn is in flight.
// For tests and one-shot processes; the desktop app uses Start.
func (r *Runtime) Drain(maxRounds int) error {
	for round := 0; round < maxRounds; round++ {
		if err := r.tick(); err != nil {
			return err
		}
		r.mu.Lock()
		n := len(r.active)
		r.mu.Unlock()
		if n == 0 {
			pending, err := r.k.MailboxesWithPending(r.ctx)
			if err != nil {
				return err
			}
			busy := false
			r.mu.Lock()
			for _, p := range pending {
				if r.handlerFor(p.Address) != nil {
					busy = true
				}
			}
			r.mu.Unlock()
			if !busy {
				return nil
			}
			continue
		}
		r.waitActive()
	}
	return fmt.Errorf("runtime did not drain")
}

// Step runs one scheduling pass and the turns it started.
func (r *Runtime) Step() error {
	if err := r.tick(); err != nil {
		return err
	}
	r.waitActive()
	return nil
}

func (r *Runtime) tick() error {
	now := time.Now()
	r.mu.Lock()
	for _, t := range r.timers {
		if t.running || now.Sub(t.lastRun) < t.every {
			continue
		}
		t.running = true
		t.lastRun = now
		task := t
		r.bg.Add(1)
		go func() {
			defer r.bg.Done()
			if err := task.run(r.ctx); err != nil {
				log.Printf("timer %s failed: %v", task.name, err)
			}
			r.mu.Lock()
			task.running = false
			r.mu.Unlock()
		}()
	}
	r.mu.Unlock()

	pending, err := r.k.MailboxesWithPending(r.ctx)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, mb := range pending {
		if len(r.active) >= r.maxTurns {
			break
		}
		if _, busy := r.active[mb.Address]; busy {
			continue
		}
		h := r.handlerFor(mb.Address)
		if h == nil {
			if !r.unhandled[mb.Address] {
				r.unhandled[mb.Address] = true
				log.Printf("no turn handler for mailbox %s", mb.Address)
			}
			continue
		}
		done := make(chan struct{})
		r.active[mb.Address] = done
		address := mb.Address
		r.bg.Add(1)
		go func() {
			defer r.bg.Done()
			r.runTurn(address, h)
			r.mu.Lock()
			delete(r.active, address)
			r.mu.Unlock()
			close(done)
			r.Kick()
		}()
	}

	idle := len(r.active) == 0
	for _, p := range pending {
		if r.handlerFor(p.Address) != nil {
			idle = false
		}
	}
	for _, t := range r.idle {
		if t.running {
			continue
		}
		since := now.Sub(t.lastRun)
		if !((idle && since >= t.min) || since >= t.max) {
			continue
		}
		t.running = true
		t.lastRun = now
		task := t
		r.bg.Add(1)
		go func() {
			defer r.bg.Done()
			if err := task.run(r.ctx); err != nil {
				log.Printf("idle task %s failed: %v", task.name, err)
			}
			r.mu.Lock()
			task.running = false
			r.mu.Unlock()
		}()
	}
	return nil
}

func (r *Runtime) runTurn(address string, h TurnHandler) {
	ctx := r.ctx
	messages, err := r.k.PendingMessages(ctx, address, 100)
	if err != nil {
		log.Printf("turn %s: %v", address, err)
		return
	}
	if len(messages) == 0 {
		return
	}
	last := messages[len(messages)-1]
	err = func() (err error) {
		defer func() {
			if p := recover(); p != nil {
				err = fmt.Errorf("panic: %v", p)
			}
		}()
		return h(ctx, address, messages)
	}()
	if err != nil {
		// At-least-once delivery, not at-least-forever: a turn that fails is
		// recorded and its messages are consumed, or one poison message would
		// wedge the mailbox and re-spend on every retry.
		_, _ = r.k.Append(context.Background(), EventInput{
			Source: "kernel:runtime", Kind: "agent.error", ThreadID: "main",
			Payload: Payload{
				"error":   fmt.Sprintf("turn failed for %s: %v", address, err),
				"mailbox": address,
				"of":      last.ID,
			},
		})
	}
	if err := r.k.CommitCursor(context.Background(), MailboxCursor(address), last.Seq); err != nil {
		log.Printf("commit cursor %s: %v", address, err)
	}
}
