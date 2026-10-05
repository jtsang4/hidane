package guard_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jtsang4/hidane/internal/guard"
)

func TestAddAndRemoveRule(t *testing.T) {
	path := filepath.Join(t.TempDir(), "POLICY.json")
	rule := guard.Rule{ID: "pol_a", Pattern: `rm\s`, Reason: "no rm"}
	if err := guard.AddRule(path, rule); err != nil {
		t.Fatal(err)
	}
	if f, err := guard.Load(path); err != nil || len(f.Rules) != 1 || f.Rules[0].ID != "pol_a" {
		t.Fatalf("added: %+v %v", f, err)
	}
	if ok, err := guard.RemoveRule(path, "pol_a"); !ok || err != nil {
		t.Fatalf("remove: %v %v", ok, err)
	}
	if ok, err := guard.RemoveRule(path, "pol_a"); ok || err != nil {
		t.Fatalf("removing twice finds nothing: %v %v", ok, err)
	}
}

func TestRuleEditsRefuseABrokenPolicyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "POLICY.json")
	broken := []byte(`{"rules":[{"id":"mine","pattern":"\.env"}]}`)
	if err := os.WriteFile(path, broken, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := guard.AddRule(path, guard.Rule{ID: "pol_b", Pattern: "rm", Reason: "no"}); err == nil {
		t.Fatal("adding a rule to a broken policy file must fail")
	}
	if _, err := guard.RemoveRule(path, "mine"); err == nil {
		t.Fatal("removing a rule from a broken policy file must fail")
	}
	if b, _ := os.ReadFile(path); string(b) != string(broken) {
		t.Fatalf("the hand-written file was overwritten: %s", b)
	}
}
