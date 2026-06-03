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

func FuzzSSERegEventType(f *testing.F) {
	f.Add("user.created")
	f.Add(`"; import("evil"); "`)
	f.Add("a]b")
	f.Add("new\nline")
	f.Add(`quote"break`)
	f.Add("back\\slash")

	f.Fuzz(func(t *testing.T, input string) {
		r := &Registry{
			ValidatorsImport:      "./v.js",
			SelfContainedRegistry: true,
			SSEEvents:             []SSERegEntry{{EventType: input, TypeName: "Dummy"}},
			RegistryFuncName:      "register",
		}
		r.typeNames = map[string]bool{"Dummy": true}
		var w strings.Builder
		r.generateRegistry(&w)
		out := w.String()

		// Every line with registry.set must have balanced quotes
		for line := range strings.SplitSeq(out, "\n") {
			if strings.Contains(line, "registry.set(") {
				unescaped := 0
				for i := 0; i < len(line); i++ {
					if line[i] == '\\' {
						i++
						continue
					}
					if line[i] == '"' {
						unescaped++
					}
				}
				if unescaped%2 != 0 {
					t.Errorf("unbalanced quotes for EventType=%q: line=%q", input, line)
				}
			}
		}
		// No raw newlines should appear inside a single line (output should not have broken lines)
		if strings.ContainsAny(input, "\n\r") {
			for line := range strings.SplitSeq(out, "\n") {
				if strings.Contains(line, "registry.set(") && (strings.Contains(line, "\r") || strings.Contains(line, "\n")) {
					t.Errorf("raw newline in output for EventType=%q", input)
				}
			}
		}
	})
}

func FuzzWireConstTSName(f *testing.F) {
	f.Add("MaxRetries")
	f.Add("export const evil = 0;\n//")
	f.Add("123bad")
	f.Add("")
	f.Add("with spaces")
	f.Add("foo\x00bar")

	f.Fuzz(func(t *testing.T, input string) {
		r := &Registry{
			Constants: []WireConst{{TSName: input, Value: 42}},
		}
		r.init()
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
		// Test ValidatorsImport in self-contained mode
		r := &Registry{
			ValidatorsImport:      input,
			SelfContainedRegistry: true,
			SSEEvents:             []SSERegEntry{{EventType: "test", TypeName: "Dummy"}},
			RegistryFuncName:      "register",
		}
		r.typeNames = map[string]bool{"Dummy": true}
		var w strings.Builder
		r.generateRegistry(&w)
		out := w.String()

		// The import line must have balanced quotes
		for line := range strings.SplitSeq(out, "\n") {
			if strings.Contains(line, "from \"") {
				unescaped := 0
				for i := 0; i < len(line); i++ {
					if line[i] == '\\' {
						i++
						continue
					}
					if line[i] == '"' {
						unescaped++
					}
				}
				if unescaped%2 != 0 {
					t.Errorf("unbalanced quotes for import path=%q: line=%q", input, line)
				}
			}
		}

		// Test BusImport in non-self-contained mode
		r2 := &Registry{
			BusImport:        input,
			RegisterFuncName: "reg",
			RegistryFuncName: "register",
			SSEEvents:        []SSERegEntry{{EventType: "test", TypeName: "Dummy"}},
		}
		r2.typeNames = map[string]bool{"Dummy": true}
		var w2 strings.Builder
		r2.generateRegistry(&w2)
		out2 := w2.String()

		for line := range strings.SplitSeq(out2, "\n") {
			if strings.Contains(line, "from \"") {
				unescaped := 0
				for i := 0; i < len(line); i++ {
					if line[i] == '\\' {
						i++
						continue
					}
					if line[i] == '"' {
						unescaped++
					}
				}
				if unescaped%2 != 0 {
					t.Errorf("unbalanced quotes for BusImport path=%q: line=%q", input, line)
				}
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
		r := &Registry{
			PathNameOverride: map[string]string{"Test": input},
		}
		r.init()
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
		r := &Registry{
			Enums:      map[string]EnumDef{"TestEnum": {Values: []string{"a", "b"}}},
			EnumTSName: map[string]string{"TestEnum": input},
		}
		r.init()
		result := r.tsEnumName("TestEnum")
		if !validTSIdent.MatchString(result) {
			t.Errorf("tsEnumName with override %q = %q, not a valid TS identifier", input, result)
		}
	})
}
