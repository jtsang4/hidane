package agents

import (
	"context"
	"maps"
	"sync"
	"time"

	"github.com/jtsang4/hidane/internal/agentcli"
	"github.com/jtsang4/hidane/internal/guard"
	"github.com/jtsang4/hidane/internal/kernel"
)

// toolTrail records a run's tool calls as they happen: the intent before a
// tool acts, its result after, and each refusal by the guard as
// policy.blocked. The guard runs in a process of its own and leaves its
// refusals in a file; every tool event, a ticker and the run's end turn what
// is new there into events, so a refusal lands before the result of the call
// it stopped and shows while the run is still going.
type toolTrail struct {
	k      *kernel.Kernel
	base   kernel.EventInput
	root   kernel.Payload
	blocks string

	mu   sync.Mutex
	seen []guard.Block
}

func newToolTrail(k *kernel.Kernel, base kernel.EventInput, root kernel.Payload, blocks string) *toolTrail {
	return &toolTrail{k: k, base: base, root: root, blocks: blocks}
}

// drain records refusals not yet recorded; the caller holds mu.
func (t *toolTrail) drain() {
	all := guard.ReadBlocks(t.blocks)
	for _, b := range all[min(len(t.seen), len(all)):] {
		in := t.base
		in.Kind, in.Payload = "policy.blocked", blockPayload(b)
		maps.Copy(in.Payload, t.root)
		_, _ = t.k.Append(context.Background(), in)
	}
	if len(all) > len(t.seen) {
		t.seen = all
	}
}

// tool is a run's OnTool.
func (t *toolTrail) tool(e agentcli.ToolEvent) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.drain()
	in := t.base
	if e.Phase == "start" {
		in.Kind, in.Payload = "side_effect.intent", kernel.Payload{"tool": e.Tool, "input": e.Detail}
	} else {
		in.Kind, in.Payload = "side_effect.result", kernel.Payload{"tool": e.Tool, "isError": e.IsError}
	}
	_, _ = t.k.Append(context.Background(), in)
}

// watch drains on a ticker until stopped: codex shows no tool item for a call
// its hook refused, so no tool event may come to drain it.
func (t *toolTrail) watch() (stop func()) {
	done := make(chan struct{})
	go func() {
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		for {
			select {
			case <-done:
				return
			case <-tick.C:
				t.mu.Lock()
				t.drain()
				t.mu.Unlock()
			}
		}
	}()
	return func() { close(done) }
}

// finish records what is left and returns every refusal of the run.
func (t *toolTrail) finish() []guard.Block {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.drain()
	return t.seen
}
