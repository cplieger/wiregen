package wiregen

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
)

func (g *generator) tsName(goName string) string {
	if override, ok := g.TSNameOverride[goName]; ok {
		if s := sanitizeTypeIdent(override); s != "" {
			return s
		}
	}
	return goName
}

func (g *generator) tsEnumName(goName string) string {
	if override, ok := g.EnumTSName[goName]; ok {
		if s := sanitizeTypeIdent(override); s != "" {
			return s
		}
	}
	return goName
}

// sanitizeTypeIdent sanitizes a consumer-supplied TS type-name override —
// the ONE sanitizer for both struct (TSNameOverride) and enum (EnumTSName)
// overrides, so the same raw override string always yields the same
// identifier: case-preserving character stripping plus the reserved-word
// suffix guard.
func sanitizeTypeIdent(s string) string {
	t := sanitizeTSIdent(s)
	if t == "" {
		return ""
	}
	if tsReservedWords[t] {
		return t + "Val"
	}
	return t
}

func (g *generator) decoderName(typeName string) string {
	return "decode" + g.tsName(typeName)
}

func (g *generator) pathName(typeName string) string {
	if override, ok := g.PathNameOverride[typeName]; ok {
		return tsStringLiteral(override)
	}
	var b strings.Builder
	runes := []rune(typeName)
	for i, ru := range runes {
		if ru < 'A' || ru > 'Z' {
			b.WriteRune(ru)
			continue
		}
		if needsWordBreak(runes, i) {
			b.WriteByte('_')
		}
		b.WriteRune(ru + 32)
	}
	return b.String()
}

// needsWordBreak reports whether an underscore should precede runes[i] (which
// the caller has determined is an uppercase letter): after a lowercase letter
// (camelCase boundary), or at the tail of an acronym immediately followed by a
// lowercase letter (e.g. the "S" in "HTTPServer").
func needsWordBreak(runes []rune, i int) bool {
	if i == 0 {
		return false
	}
	prev := runes[i-1]
	if prev >= 'a' && prev <= 'z' {
		return true
	}
	return prev >= 'A' && prev <= 'Z' && i+1 < len(runes) && runes[i+1] >= 'a' && runes[i+1] <= 'z'
}

func (g *generator) enumConstName(goTypeName string) string {
	name := g.tsEnumName(goTypeName)
	var b strings.Builder
	runes := []rune(name)
	for i, ru := range runes {
		switch {
		case ru >= 'a' && ru <= 'z':
			b.WriteRune(ru - 32)
		case ru >= 'A' && ru <= 'Z':
			if needsWordBreak(runes, i) {
				b.WriteByte('_')
			}
			b.WriteRune(ru)
		default:
			// Digits, '_', '$' pass through unchanged (the old unconditional
			// ru-32 corrupted them into control characters / other symbols).
			b.WriteRune(ru)
		}
	}
	b.WriteString("S")
	return b.String()
}

// keepIdentRune reports whether r is valid in a TS identifier: letters, '_'
// and '$' are always allowed; a digit is allowed only when a prefix has
// already been emitted (hasPrefix), so an identifier never starts with a digit.
func keepIdentRune(r rune, hasPrefix bool) bool {
	if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_' || r == '$' {
		return true
	}
	return hasPrefix && r >= '0' && r <= '9'
}

// isValidTSIdent reports whether s is usable as a bare TS identifier (an
// interface/object property name or a member access needs no quoting). It
// mirrors keepIdentRune's char-class and rejects the empty string and any
// leading digit.
func isValidTSIdent(s string) bool {
	if s == "" || (s[0] >= '0' && s[0] <= '9') {
		return false
	}
	for i, r := range s {
		if !keepIdentRune(r, i > 0) {
			return false
		}
	}
	return true
}

// tsPropName renders wireName as a TS property name: bare when it is a valid
// identifier (preserving existing golden output), otherwise a double-quoted
// string-literal key so a non-identifier JSON key (e.g. "content-type") stays
// valid TypeScript.
func tsPropName(wireName string) string {
	if isValidTSIdent(wireName) {
		return wireName
	}
	return "\"" + tsStringLiteral(wireName) + "\""
}

// tsMemberRef renders a member access into the decoded out object: ".name" for
// a valid identifier (preserving existing golden output), otherwise ["name"]
// bracket access so a non-identifier JSON key stays valid TypeScript.
func tsMemberRef(wireName string) string {
	if isValidTSIdent(wireName) {
		return "." + wireName
	}
	return "[\"" + tsStringLiteral(wireName) + "\"]"
}

// tsLiteralKey renders wireName as an object-literal key. A literal
// "__proto__" key sets the prototype; the computed form makes an own
// property (ECMA-262 PropertyDefinitionEvaluation, __proto__ property names).
func tsLiteralKey(wireName string) string {
	if wireName == protoKey {
		return "[\"" + protoKey + "\"]"
	}
	return tsPropName(wireName)
}

// setOwn renders the statement giving out the own property wireName; an
// assignment to "__proto__" would call the prototype setter instead.
func setOwn(wireName, expr string) string {
	if wireName == protoKey {
		return "globalThis.Object.defineProperty(out, \"" + protoKey + "\", { value: " + expr + ", enumerable: true, writable: true, configurable: true })"
	}
	return "out" + tsMemberRef(wireName) + " = " + expr
}

const protoKey = "__proto__"

// sanitizeTSIdent strips characters that are not valid in a TS identifier,
// preserving case and underscores.
func sanitizeTSIdent(s string) string {
	var b strings.Builder
	for _, r := range s {
		if keepIdentRune(r, b.Len() > 0) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// tsReservedWords are the identifiers a generated binding (a renamed type,
// an endpoint function) must never take: the ES keywords, the strict-mode
// reserved words (generated files are ES modules), module-level `await`, the
// restricted names `eval` and `arguments`, the predefined type names no
// interface or type alias may take (TS2427, TS2457), the decoder's locals
// (`o`, `out`, `v`), and the two globals the generated code reads bare,
// `undefined` and `globalThis`, through which it names every other.
var tsReservedWords = map[string]bool{
	// ES keywords
	"break": true, "case": true, "catch": true, "class": true, "const": true,
	"continue": true, "debugger": true, "default": true, "delete": true,
	"do": true, "else": true, "enum": true, "export": true, "extends": true,
	"false": true, "finally": true, "for": true, "function": true, "if": true,
	"import": true, "in": true, "instanceof": true, "new": true, "null": true,
	"return": true, "super": true, "switch": true, "this": true, "throw": true,
	"true": true, "try": true, "typeof": true, "var": true, "void": true,
	"while": true, "with": true,
	// strict-mode reserved words, restricted binding names, module-level await
	"implements": true, "interface": true, "let": true, "package": true,
	"private": true, "protected": true, "public": true, "static": true,
	"yield": true, "await": true, "eval": true, "arguments": true,
	// predefined type names
	"any": true, "bigint": true, "boolean": true, "never": true, "number": true,
	"object": true, "string": true, "symbol": true, "unknown": true,
	// decoder emit-context locals
	"o": true, "out": true, "v": true,
	// read bare by the generated code, and no module may declare them
	"undefined": true, "globalThis": true,
}

// tsStringLiteral escapes a string for safe embedding in a TS double-quoted string literal.
func tsStringLiteral(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// jsString renders s as a JS string literal, with every quote and line
// terminator escaped.
func jsString(s string) string {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s) // a string always encodes
	return strings.TrimSuffix(b.String(), "\n")
}

// isIdentReferenced reports whether ident occurs in body with identifier
// boundaries on both sides. Import/const selection is owned by the
// usedIdents collector (identifiers recorded at their emission sites), so
// this scan runs ONLY over the consumer-supplied Mappings
// expressions (usedIdents.opaque) — never over the generated body, whose
// string literals (wire names, paths) could false-positively match.
func isIdentReferenced(body, ident string) bool {
	if ident == "" {
		return false
	}
	for i := 0; i < len(body); {
		j := strings.Index(body[i:], ident)
		if j < 0 {
			return false
		}
		j += i
		if j > 0 {
			c := body[j-1]
			if isIdentChar(c) {
				i = j + len(ident)
				continue
			}
		}
		end := j + len(ident)
		if end < len(body) {
			c := body[end]
			if isIdentChar(c) {
				i = end
				continue
			}
		}
		return true
	}
	return false
}

func isIdentChar(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '$'
}

func enumNamesSlice(m map[string]enumDef) []string {
	return slices.Sorted(maps.Keys(m))
}

// validatorsExports are the names the validators module exports, which the
// other modules import, in the order an import line lists them; Decoder,
// the one type, stays first.
var validatorsExports = []string{
	"Decoder", "own", "asObject", "asArray", helperReqStr, helperReqNum, helperReqBool,
	"optStr", "optNum", "optBool", "reqOneOf", helperDecodeArray, helperDecodeRecord,
	"refuseKeys", "checkRange", "checkBytes", "checkPattern", "checkCount",
}

// arbitrariesIdents are the names the arbitraries module binds itself.
var arbitrariesIdents = []string{"fc", "arbModel", "ARBITRARY_BY_TYPE"}

// claimIdents claims every top-level identifier the generated modules bind
// or import, in one namespace, refusing a name two of them would take: the
// modules import each other's exports, so a repeat is a TS compile error or a
// silently shadowed binding. It returns the claims for the endpoints to join.
func (g *generator) claimIdents() (map[string]string, error) {
	taken := map[string]string{}
	var errs []error
	claim := func(who string, ids ...string) {
		for _, id := range ids {
			if prev, dup := taken[id]; dup {
				errs = append(errs, fmt.Errorf("wiregen: %s and %s both emit the TS identifier %s; rename one through TSNameOverride or EnumTSName", prev, who, id))
				continue
			}
			taken[id] = who
		}
	}
	claim("the validators module", validatorsExports...)
	if len(g.Events) > 0 {
		claim("the events module", eventsIdents...)
	}
	if len(g.Endpoints) > 0 {
		claim("the client module", clientReserved...)
	}
	arbs := g.o.arbitrariesFilename != ""
	if arbs {
		claim("the arbitraries module", arbitrariesIdents...)
	}
	for _, name := range slices.Sorted(maps.Keys(g.typeNames)) {
		claim("type "+name, g.tsName(name), g.decoderName(name))
		if arbs {
			claim("the arbitrary of type "+name, arbName(g.tsName(name)))
		}
	}
	for _, name := range enumNamesSlice(g.enums) {
		claim("enum "+name, g.tsEnumName(name), g.enumConstName(name))
		if arbs {
			claim("the arbitrary of enum "+name, arbName(g.tsEnumName(name)))
		}
	}
	return taken, errors.Join(errs...)
}

func (g *generator) typesSpec(from string) string {
	return tsStringLiteral(cmp.Or(g.o.typesImportPath, moduleSpecifier(from, g.o.typesFilename)))
}

func (g *generator) validatorsSpec(from string) string {
	return tsStringLiteral(cmp.Or(g.o.validatorsImport, moduleSpecifier(from, g.o.validatorsFilename)))
}

func (g *generator) decodersSpec(from string) string {
	return tsStringLiteral(moduleSpecifier(from, g.o.decodersFilename))
}
