package config_test

import (
	"testing"

	"github.com/jtsang4/hidane/internal/config"
)

// The causal budgets are a person's knobs: the runtime reads them from the environment.
func TestBudgetsComeFromTheEnvironment(t *testing.T) {
	t.Setenv("HIDANE_MAX_HOPS", "3")
	t.Setenv("HIDANE_MAX_EXECUTIONS_PER_ITEM", "2")
	if c := config.Load(); c.MaxHops != 3 || c.MaxExecutionsPerItem != 2 {
		t.Fatalf("hops %d, executions per item %d", c.MaxHops, c.MaxExecutionsPerItem)
	}
}
