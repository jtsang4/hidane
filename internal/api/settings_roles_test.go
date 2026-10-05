package api_test

import (
	"testing"

	"github.com/jtsang4/hidane/internal/api"
	"github.com/jtsang4/hidane/internal/kernel"
)

// The Chat picker saves only the Primary role (`{roles: {primary}}`, as the
// composer sends it); the change is a fact in the log, the other roles stay.
func TestSwitchingWhoAnswersTheChatIsRecorded(t *testing.T) {
	e := newEnv(t, api.Options{})
	before := e.st.Get().Roles
	primary := map[string]any{"agent": "codex", "provider": "", "model": "gpt-fake-1", "effort": "high"}
	if code, body := e.do("PUT", "/api/settings/roles", "", map[string]any{"roles": map[string]any{"primary": primary}}); code != 200 {
		t.Fatalf("save: %d %v", code, body)
	}
	after := e.st.Get().Roles
	if after["primary"].Agent != "codex" || after["primary"].Model != "gpt-fake-1" || after["primary"].Effort != "high" {
		t.Fatalf("the Primary runs on the choice: %+v", after["primary"])
	}
	for _, role := range []string{"manager", "worker", "distiller"} {
		if after[role] != before[role] {
			t.Fatalf("%s changed with the Primary: %+v → %+v", role, before[role], after[role])
		}
	}
	updates := m(e.k.ListEvents(t.Context(), kernel.ListFilter{Kind: "settings.updated"}))
	if len(updates) != 1 || updates[0].Payload.Str("what") != "roles" {
		t.Fatalf("one settings.updated for the roles: %+v", updates)
	}
	roles, _ := updates[0].Payload["roles"].(map[string]any)
	recorded, _ := roles["primary"].(map[string]any)
	for key, want := range primary {
		if recorded[key] != want {
			t.Fatalf("the event records the new Primary: %v", roles)
		}
	}
}
