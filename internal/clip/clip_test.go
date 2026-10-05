package clip

import (
	"testing"
	"unicode/utf8"
)

func TestClip(t *testing.T) {
	for _, tc := range []struct {
		name string
		fn   func(string, int) string
		in   string
		n    int
		want string
	}{
		{"Runes", Runes, "日本語のテキスト", 2, "日本"}, // a byte cut at 2 lands inside 日
		{"Runes", Runes, "héllo", 2, "hé"},
		{"Runes", Runes, "abc", 3, "abc"},
		{"Runes", Runes, "abc", 5, "abc"},
		{"Runes", Runes, "abc", 0, ""},
		{"Runes", Runes, "", 3, ""},
		{"Runes", Runes, " a  b ", 4, " a  "},

		{"Ellipsis", Ellipsis, "日本語のテキスト", 3, "日本語…"},
		{"Ellipsis", Ellipsis, "abc", 3, "abc"},
		{"Ellipsis", Ellipsis, "abcd", 3, "abc…"},
		{"Ellipsis", Ellipsis, "abc", 0, "…"},
		{"Ellipsis", Ellipsis, "", 0, ""},

		{"Line", Line, "  a\n\tb  c ", 10, "a b c"},
		{"Line", Line, "日本\n語の テキスト", 4, "日本 語…"},
		{"Line", Line, "a\nb", 3, "a b"},
		{"Line", Line, " \n ", 3, ""},

		{"Noted", Noted, "日本語のテキスト", 3, "日本語\n\n[… 5 more characters not shown]"},
		{"Noted", Noted, "abcd", 1, "a\n\n[… 3 more characters not shown]"},
		{"Noted", Noted, "abc", 3, "abc"},
		{"Noted", Noted, "abc", 0, "\n\n[… 3 more characters not shown]"},
	} {
		got := tc.fn(tc.in, tc.n)
		if got != tc.want || !utf8.ValidString(got) {
			t.Errorf("%s(%q, %d) = %q, want %q", tc.name, tc.in, tc.n, got, tc.want)
		}
	}
}
