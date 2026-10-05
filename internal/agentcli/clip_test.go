package agentcli

import (
	"testing"
	"unicode/utf8"
)

func TestClipNeverSplitsACharacter(t *testing.T) {
	for _, tc := range []struct {
		in   string
		n    int
		want string
	}{
		{"日本語のテキスト", 2, "日本"}, // a byte cut at 2 lands inside 日
		{"héllo", 2, "hé"},
		{"abc", 3, "abc"},
		{"abc", 5, "abc"},
		{"abc", 0, ""},
	} {
		got := clip(tc.in, tc.n)
		if got != tc.want || !utf8.ValidString(got) {
			t.Errorf("clip(%q, %d) = %q, want %q", tc.in, tc.n, got, tc.want)
		}
	}
}
