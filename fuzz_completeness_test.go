package wiregen

import (
	"go/ast"
	"regexp"
	"strings"
	"testing"
)

var validTSIdent = regexp.MustCompile(`^[a-zA-Z_$][a-zA-Z0-9_$]*$`)

func FuzzSanitizeVarName(f *testing.F) {
	f.Add("hello_world")
	f.Add("")
	f.Add("__proto__")
	f.Add("return")
	f.Add("a_b_c")
	f.Add("123bad")
	f.Add("with spaces")
	f.Add("foo\x00bar")

	f.Fuzz(func(t *testing.T, input string) {
		result := sanitizeVarName(input)
		// Must be a valid TS identifier or empty
		if result != "" && !validTSIdent.MatchString(result) {
			t.Errorf("sanitizeVarName(%q) = %q, not a valid TS identifier", input, result)
		}
		// Idempotent
		if result != "" {
			again := sanitizeVarName(result)
			if again != result {
				t.Errorf("sanitizeVarName not idempotent: sanitizeVarName(%q)=%q, sanitizeVarName(%q)=%q", input, result, result, again)
			}
		}
	})
}

func FuzzParseUnionDirective(f *testing.F) {
	f.Add("// wiregen:union discriminator=type variants=A,B,C")
	f.Add("// wiregen:union discriminator= variants=")
	f.Add("// wiregen:union")
	f.Add("// just a comment")
	f.Add("")
	f.Add("// wiregen:union discriminator=x variants=A,,B")
	f.Add("// wiregen:union variants=X discriminator=y")
	f.Add("// wiregen:union discriminator=a\x00b variants=C\nD")

	f.Fuzz(func(t *testing.T, input string) {
		cg := &ast.CommentGroup{
			List: []*ast.Comment{{Text: input}},
		}
		ud := parseUnionDirective(cg)
		if ud == nil {
			return // ok=false, no union
		}
		// If returns a union, discriminator and variants must be non-empty
		if ud.Discriminator == "" {
			t.Errorf("parseUnionDirective(%q): ok but discriminator is empty", input)
		}
		if len(ud.Variants) == 0 {
			t.Errorf("parseUnionDirective(%q): ok but variants is empty", input)
		}
		for _, v := range ud.Variants {
			if strings.ContainsAny(v, ", ") {
				t.Errorf("parseUnionDirective(%q): variant %q contains comma or space", input, v)
			}
		}
	})
}

func FuzzCommentToJSDoc(f *testing.F) {
	f.Add("// Hello world")
	f.Add("/* block comment */")
	f.Add("// line with */")
	f.Add("// nolint:foo")
	f.Add("// go:generate something")
	f.Add("// wiregen:union discriminator=x variants=A")
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
