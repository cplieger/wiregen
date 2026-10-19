package wiregen_test

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// FuzzMaxBytes_validatorMatchesUTF8Length holds the generated checkBytes to
// Go's own count: a string passes MaxBytes exactly when its UTF-8 encoding is
// no longer than the bound.
func FuzzMaxBytes_validatorMatchesUTF8Length(f *testing.F) {
	for _, seed := range []struct {
		s   string
		max uint16
	}{
		{"", 1},
		{strings.Repeat("é", 255), 510},
		{strings.Repeat("é", 256), 510},
		{strings.Repeat("a", 510), 510},
		{"日本語", 8},
		{"日本語", 9},
		{"😀", 3},
		{"😀", 4},
		{"\u2028\"\\", 7},
	} {
		f.Add(seed.s, seed.max)
	}
	repl := startREPL(f, boundsReg(), `import { checkBytes } from "./validators.gen.ts";`,
		`try { checkBytes(req.s, req.max); return "pass"; } catch { return "refuse"; }`)
	f.Fuzz(func(t *testing.T, s string, maxBytes uint16) {
		if !utf8.ValidString(s) || maxBytes == 0 {
			return // JSON cannot carry invalid UTF-8; a bound is at least 1
		}
		want := "refuse"
		if len(s) <= int(maxBytes) {
			want = "pass"
		}
		if got := repl.ask(t, map[string]any{"s": s, "max": maxBytes}); got != want {
			t.Errorf("checkBytes(%q [%d bytes], %d) = %s, want %s", s, len(s), maxBytes, got, want)
		}
	})
}
