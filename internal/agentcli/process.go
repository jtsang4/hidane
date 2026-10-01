package agentcli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// proc is one CLI subprocess in its own process group, with stdout read as
// LF-delimited JSON records and the tail of stderr kept for diagnostics.
type proc struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  *bufio.Reader
	stdoutR *os.File
	stderr  *tailBuffer

	mu          sync.Mutex
	stdinClosed bool
	done        chan struct{}
	exitErr     error
}

type tailBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf.Write(p)
	if t.buf.Len() > 8192 {
		b := t.buf.Bytes()
		keep := append([]byte(nil), b[len(b)-4096:]...)
		t.buf.Reset()
		t.buf.Write(keep)
	}
	return len(p), nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	s := t.buf.String()
	if len(s) > 1000 {
		s = s[len(s)-1000:]
	}
	return s
}

func startProc(bin string, args []string, cwd string, env []string, pipeStdin bool) (*proc, error) {
	cmd := exec.Command(bin, args...)
	cmd.Dir = cwd
	cmd.Env = env
	cmd.SysProcAttr = sysProcAttr()
	p := &proc{cmd: cmd, stderr: &tailBuffer{}, done: make(chan struct{})}
	cmd.Stderr = p.stderr
	if pipeStdin {
		w, err := cmd.StdinPipe()
		if err != nil {
			return nil, err
		}
		p.stdin = w
	}
	// Our own pipe rather than StdoutPipe, so Wait can run while stdout is
	// still being read: a grandchild that left the process group can hold the
	// write end open, and the run must still end when the CLI does.
	r, w, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	cmd.Stdout = w
	// Bounds the stderr copy the same way.
	cmd.WaitDelay = 2 * time.Second
	if err := cmd.Start(); err != nil {
		r.Close()
		w.Close()
		return nil, err
	}
	w.Close()
	p.stdoutR = r
	p.stdout = bufio.NewReaderSize(r, 1<<20)
	return p, nil
}

// readLines delivers each stdout record; done closes once the CLI has exited
// and its output is drained (or abandoned, if something else still holds it).
func (p *proc) readLines(onLine func([]byte)) {
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		for {
			line, err := p.stdout.ReadBytes('\n')
			line = bytes.TrimRight(line, "\r\n")
			if len(bytes.TrimSpace(line)) > 0 {
				onLine(line)
			}
			if err != nil {
				return
			}
		}
	}()
	go func() {
		p.exitErr = p.cmd.Wait()
		select {
		case <-drained:
		case <-time.After(3 * time.Second):
			p.stdoutR.Close()
			<-drained
		}
		p.stdoutR.Close()
		close(p.done)
	}()
}

func (p *proc) writeJSON(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stdinClosed || p.stdin == nil {
		return errors.New("stdin closed")
	}
	_, err = p.stdin.Write(append(b, '\n'))
	return err
}

func (p *proc) writeRaw(s string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stdinClosed || p.stdin == nil {
		return errors.New("stdin closed")
	}
	_, err := io.WriteString(p.stdin, s)
	return err
}

func (p *proc) closeStdin() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.stdinClosed && p.stdin != nil {
		p.stdinClosed = true
		_ = p.stdin.Close()
	}
}

// kill stops the whole process group: TERM, then KILL if it lingers.
func (p *proc) kill() {
	select {
	case <-p.done:
		return
	default:
	}
	killGroup(p.cmd, syscall.SIGTERM)
	select {
	case <-p.done:
	case <-time.After(3 * time.Second):
		killGroup(p.cmd, syscall.SIGKILL)
	}
}

// supervise ends the process on cancellation or timeout.
func (p *proc) supervise(ctx context.Context, timeout time.Duration, onTimeout func()) {
	go func() {
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		select {
		case <-p.done:
		case <-ctx.Done():
			p.kill()
		case <-timer.C:
			if onTimeout != nil {
				onTimeout()
			}
			p.kill()
		}
	}()
}

func (p *proc) exitError(prefix string) string {
	msg := prefix
	if p.exitErr != nil {
		msg = fmt.Sprintf("%s (%v)", prefix, p.exitErr)
	}
	if tail := p.stderr.String(); tail != "" {
		msg += "\nstderr: " + tail
	}
	return msg
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
