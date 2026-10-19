package wiregen

import (
	"go/ast"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"
)

var validTSIdent = regexp.MustCompile(`^[a-zA-Z_$][a-zA-Z0-9_$]*$`)

func FuzzCommentToJSDoc(f *testing.F) {
	f.Add("// Hello world")
	f.Add("/* block comment */")
	f.Add("// line with */")
	f.Add("// nolint:foo")
	f.Add("// go:generate something")
	f.Add("// */ injection */")
	f.Add("/* nested */ and more */")
	f.Add("")

	f.Fuzz(func(t *testing.T, input string) {
		cg := &ast.CommentGroup{
			List: []*ast.Comment{{Text: input}},
		}
		result := commentToJSDoc(cg)
		// CRITICAL: output must NEVER contain */ which would close the JSDoc block
		if result != "" && strings.Contains(result, "*/") {
			// Allow the closing */ of the JSDoc itself but not embedded ones
			// A valid JSDoc has exactly one */ at the very end
			trimmed := strings.TrimSpace(result)
			// Remove the final closing */ and check for others
			if strings.HasSuffix(trimmed, "*/") {
				inner := trimmed[:len(trimmed)-2]
				if strings.Contains(inner, "*/") {
					t.Errorf("commentToJSDoc(%q) contains embedded '*/': %q", input, result)
				}
			}
		}
	})
}

func FuzzEnumValueEscape(f *testing.F) {
	f.Add("active")
	f.Add(`with"quote`)
	f.Add("with\\backslash")
	f.Add("with\nnewline")
	f.Add("with`backtick")
	f.Add(`"already"quoted"`)
	f.Add("multi\n\"line\"\ttab")

	f.Fuzz(func(t *testing.T, input string) {
		escaped := tsStringLiteral(input)
		literal := "\"" + escaped + "\""

		// No raw newlines in a TS string literal
		if strings.ContainsAny(escaped, "\n\r") {
			t.Errorf("tsStringLiteral(%q) contains raw newline: %q", input, escaped)
		}
		// Count unescaped quotes — should be exactly 2 (opening + closing)
		unescaped := 0
		for i := 0; i < len(literal); i++ {
			if literal[i] == '\\' {
				i++
				continue
			}
			if literal[i] == '"' {
				unescaped++
			}
		}
		if unescaped != 2 {
			t.Errorf("tsStringLiteral(%q) produces unbalanced quotes (%d unescaped): %q", input, unescaped, literal)
		}
	})
}

// FuzzSSERegEventType checks that an event name the registry accepts is
// emitted only inside balanced string literals, and that only a name an SSE
// event field cannot carry is refused.
func FuzzSSERegEventType(f *testing.F) {
	f.Add("user.created")
	f.Add(`"; import("evil"); "`)
	f.Add("a]b")
	f.Add("new\nline")
	f.Add(`quote"break`)
	f.Add("back\\slash")

	f.Fuzz(func(t *testing.T, input string) {
		reg := &Registry{Events: []Event{{Name: input, Data: WireType{PkgPath: "example.com/p", Name: "Dummy"}}}}
		r, err := newGenerator(reg, []Option{WithTSDir(".")})
		carriable := input != "" && !strings.ContainsAny(input, "\r\n") && utf8.ValidString(input)
		if err != nil {
			if carriable {
				t.Errorf("newGenerator refused event name %q: %v", input, err)
			}
			return
		}
		if !carriable {
			t.Fatalf("newGenerator accepted event name %q", input)
		}
		var w strings.Builder
		r.generateEvents(&w)
		for line := range strings.SplitSeq(w.String(), "\n") {
			if unescapedQuotes(line)%2 != 0 {
				t.Errorf("unbalanced quotes for event name %q: line=%q", input, line)
			}
		}
	})
}

// unescapedQuotes counts the double quotes in line not escaped by a
// backslash.
func unescapedQuotes(line string) int {
	n := 0
	for i := 0; i < len(line); i++ {
		if line[i] == '\\' {
			i++
			continue
		}
		if line[i] == '"' {
			n++
		}
	}
	return n
}

func FuzzWireConstTSName(f *testing.F) {
	f.Add("MaxRetries")
	f.Add("export const evil = 0;\n//")
	f.Add("123bad")
	f.Add("")
	f.Add("with spaces")
	f.Add("foo\x00bar")

	f.Fuzz(func(t *testing.T, input string) {
		// validateConstants rejects exactly the TSNames that sanitize to an
		// empty identifier; everything it accepts must emit a valid one below.
		r, err := newGenerator(&Registry{Constants: []WireConst{{TSName: input, Value: 42}}}, []Option{WithTSDir(".")})
		if err != nil {
			if sanitizeTSIdent(input) != "" {
				t.Errorf("newGenerator rejected sanitizable TSName %q: %v", input, err)
			}
			return
		}
		if sanitizeTSIdent(input) == "" {
			t.Errorf("newGenerator accepted TSName %q that sanitizes to empty", input)
			return
		}
		var w strings.Builder
		r.generateConstants(&w)
		out := w.String()

		// Every "export const X = ..." line must have valid identifier X
		for line := range strings.SplitSeq(out, "\n") {
			if !strings.HasPrefix(line, "export const ") {
				continue
			}
			rest := strings.TrimPrefix(line, "export const ")
			parts := strings.SplitN(rest, " = ", 2)
			ident := parts[0]
			if !validTSIdent.MatchString(ident) {
				t.Errorf("invalid identifier in output for TSName=%q: got %q", input, ident)
			}
		}
	})
}

func FuzzImportPathInjection(f *testing.F) {
	f.Add("./validators.js")
	f.Add(`"; import("evil"); "`)
	f.Add("path\nwith\nnewlines")
	f.Add(`back\slash`)
	f.Add("quote\"break")

	f.Fuzz(func(t *testing.T, input string) {
		if input == "" {
			t.Skip("empty import paths panic by design")
		}
		r := mustGenerator(t, &Registry{Events: []Event{{Name: "test", Data: WireType{PkgPath: "example.com/p", Name: "Dummy"}}}},
			WithValidatorsImport(input))
		var w strings.Builder
		r.generateEvents(&w)
		for line := range strings.SplitSeq(w.String(), "\n") {
			if strings.Contains(line, "from \"") && unescapedQuotes(line)%2 != 0 {
				t.Errorf("unbalanced quotes for import path=%q: line=%q", input, line)
			}
		}
	})
}

func FuzzPathNameOverride(f *testing.F) {
	f.Add("custom_path")
	f.Add(`"; import("evil"); "`)
	f.Add("path\ninjection")
	f.Add(`back\slash`)
	f.Add("quote\"break")

	f.Fuzz(func(t *testing.T, input string) {
		r := mustGenerator(t, &Registry{PathNameOverride: map[string]string{"Test": input}})
		result := r.pathName("Test")
		// Must not contain raw newlines or unescaped quotes
		if strings.ContainsAny(result, "\n\r") {
			t.Errorf("pathName override contains raw newline for %q: %q", input, result)
		}
		// When embedded in "$.X", quotes must be balanced
		literal := "\"$." + result + "\""
		unescaped := 0
		for i := 0; i < len(literal); i++ {
			if literal[i] == '\\' {
				i++
				continue
			}
			if literal[i] == '"' {
				unescaped++
			}
		}
		if unescaped != 2 {
			t.Errorf("pathName(%q) produces unbalanced quotes (%d): %q", input, unescaped, literal)
		}
	})
}

func FuzzEnumConstName(f *testing.F) {
	f.Add("Status")
	f.Add("123invalid")
	f.Add("")
	f.Add("with spaces")
	f.Add("export type Evil = never;\n//")
	f.Add("foo\x00bar")

	f.Fuzz(func(t *testing.T, input string) {
		r := mustGenerator(t, &Registry{
			Enums:      []EnumRef{{Type: WireType{PkgPath: "example.com/p", Name: "TestEnum"}}},
			EnumTSName: map[string]string{"TestEnum": input},
		})
		result := r.tsEnumName("TestEnum")
		if !validTSIdent.MatchString(result) {
			t.Errorf("tsEnumName with override %q = %q, not a valid TS identifier", input, result)
		}
	})
}
