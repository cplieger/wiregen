// Package crossref provides fixtures exercising slice/map element
// cross-reference resolution, recurring field-name doc scoping, and
// unresolved-type fallback — the regressions fixed alongside these tests.
package crossref

import "github.com/cplieger/wiregen/testdata/crossref/dep"

// _ forces dep into the loaded package graph as a transitive dependency, so
// the enum-discovery scoping test has a same-named (dep.Color) type to ignore.
var _ = dep.DepRed

// Item is a registered struct used as a slice and map element.
type Item struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Status is a registered enum used as a slice element.
type Status string

// Container exercises slice-of-struct, map-of-struct, and slice-of-enum fields,
// all of whose elements are registered types.
type Container struct {
	Items    []Item          `json:"items"`
	ByKey    map[string]Item `json:"by_key"`
	Statuses []Status        `json:"statuses"`
}

// Alpha has a path field with Alpha-specific documentation.
type Alpha struct {
	// AlphaPathDoc marks alpha.
	Path string `json:"path"`
}

// Beta has a path field with Beta-specific documentation.
type Beta struct {
	// BetaPathDoc marks beta.
	Path string `json:"path"`
}

// Unregistered is intentionally NOT registered; fields of this type resolve to unknown.
type Unregistered struct {
	X int `json:"x"`
}

// Outer has a field of an unregistered struct type.
type Outer struct {
	Known  string       `json:"known"`
	Nested Unregistered `json:"nested"`
}

// Color is a string enum with a const block — used to test enum-value
// auto-discovery from source (declared red, green, blue in that order).
type Color string

const (
	ColorRed   Color = "red"
	ColorGreen Color = "green"
	ColorBlue  Color = "blue"
)

// Palette references Color so the engine loads + resolves the enum.
type Palette struct {
	Primary Color `json:"primary"`
}
