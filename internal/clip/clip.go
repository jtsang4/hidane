// Package clip shortens text to a number of characters (runes): a byte cut
// can split a character in two.
package clip

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Runes keeps the first n runes of s.
func Runes(s string, n int) string {
	for i := range s {
		if n == 0 {
			return s[:i]
		}
		n--
	}
	return s
}

// Ellipsis is Runes followed by "…" when anything was cut.
func Ellipsis(s string, n int) string {
	if kept := Runes(s, n); len(kept) < len(s) {
		return kept + "…"
	}
	return s
}

// Line flattens every run of whitespace to one space, then clips with Ellipsis.
func Line(s string, n int) string {
	return Ellipsis(strings.Join(strings.Fields(s), " "), n)
}

// Noted is Runes that says how much it left out: a reader must know the text
// is not whole.
func Noted(s string, n int) string {
	kept := Runes(s, n)
	if len(kept) == len(s) {
		return s
	}
	return kept + fmt.Sprintf("\n\n[… %d more characters not shown]", utf8.RuneCountInString(s[len(kept):]))
}
