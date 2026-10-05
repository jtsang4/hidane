package main

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/jtsang4/hidane/internal/config"
	"github.com/jtsang4/hidane/internal/kernel"
)

// What a task runs on is chosen as a whole: --model or --provider without
// --agent is refused before anything is said, not quietly dropped.
func TestCLIChatRefusesAModelWithoutAnAgent(t *testing.T) {
	t.Parallel()
	bin := buildCLI(t)
	home := fakeHome(t)
	for _, flags := range [][]string{{"--model", "gpt-fake-1"}, {"--provider", "deepseek"}} {
		cmd := exec.Command(bin, append(append([]string{"chat", "--timeout", "30"}, flags...), "你好，换个模型")...)
		cmd.Env = append(os.Environ(), "HIDANE_HOME="+home, "HIDANE_LOGIN_SHELL=0")
		out, err := cmd.CombinedOutput()
		if err == nil || !strings.Contains(string(out), "need --agent") {
			t.Fatalf("chat %v must be refused: %v\n%s", flags, err, out)
		}
	}
	k, err := kernel.Open(config.ForTest(home))
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	if said, _ := k.ListEvents(context.Background(), kernel.ListFilter{Kind: "user.message"}); len(said) != 0 {
		t.Fatalf("a refused chat says nothing: %+v", said)
	}
}
