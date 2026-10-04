package kernel

import (
	"context"
	"crypto/rand"
	"log"
	"sync"
	"time"
)

const idAlphabet = "0123456789abcdefghjkmnpqrstvwxyz"

// GenID returns a short url-safe id like `wi_9f3kq2`.
func GenID(prefix string, n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	out := make([]byte, n)
	for i, v := range b {
		out[i] = idAlphabet[int(v)%len(idAlphabet)]
	}
	return prefix + "_" + string(out)
}

// Hub wakes in-process listeners when an event is appended.
//
// Delivery is a hint, never the data: a subscriber still reads the log from its
// own cursor, so a dropped wake-up (a full channel, another process appending)
// only costs latency until the subscriber's fallback poll.
type Hub struct {
	mu   sync.Mutex
	subs map[chan int64]struct{}
}

func NewHub() *Hub { return &Hub{subs: map[chan int64]struct{}{}} }

// Subscribe returns a channel of appended seqs and a cancel function.
func (h *Hub) Subscribe() (<-chan int64, func()) {
	ch := make(chan int64, 64)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		if _, ok := h.subs[ch]; ok {
			delete(h.subs, ch)
			close(ch)
		}
		h.mu.Unlock()
	}
}

func (h *Hub) Publish(seq int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		select {
		case ch <- seq:
		default:
		}
	}
}

// Consume runs a consumer's pass now, on every append and on a fallback poll,
// until ctx ends. A pass that handled a full batch runs again at once.
func (k *Kernel) Consume(ctx context.Context, name string, poll time.Duration, batch int, pass func(context.Context) (int, error)) {
	wake, cancel := k.Hub.Subscribe()
	defer cancel()
	t := time.NewTicker(poll)
	defer t.Stop()
	for {
		for {
			n, err := pass(ctx)
			if err != nil {
				if ctx.Err() == nil {
					log.Printf("%s: %v", name, err)
				}
				break
			}
			if n < batch {
				break
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-wake:
		case <-t.C:
		}
	}
}
