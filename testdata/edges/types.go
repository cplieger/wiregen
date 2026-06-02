// Package edges provides edge-case types for wiregen tests.
package edges

import "encoding/json"

// TreeNode is recursive via pointer embedding.
type TreeNode struct {
	*TreeNode
	Value string `json:"value"`
}

// CycleA and CycleB form a mutual recursion cycle.
type CycleA struct {
	Name string  `json:"name"`
	B    *CycleB `json:"b,omitempty"`
}

type CycleB struct {
	Value string  `json:"value"`
	A     *CycleA `json:"a,omitempty"`
}

// SelfSlice references itself via a slice.
type SelfSlice struct {
	Name     string      `json:"name"`
	Children []SelfSlice `json:"children,omitempty"`
}

// SelfMap references itself via a map.
type SelfMap struct {
	Name     string             `json:"name"`
	Children map[string]SelfMap `json:"children,omitempty"`
}

// DashComma tests json:"-," (field named "-").
type DashComma struct {
	Name   string `json:"name"`
	Hidden string `json:"-"`
	Dash   string `json:"-,"`
}

// EmbeddedIface embeds an interface (should skip).
type Embedder interface {
	Embed()
}

type HasEmbeddedIface struct {
	Name string `json:"name"`
}

// FieldNamedType has a field named the same as a type.
type FieldNamedType struct {
	FieldNamedType string `json:"field_named_type"`
}

// ReservedFields has TS-reserved words as JSON field names.
type ReservedFields struct {
	Delete  string `json:"delete"`
	Private string `json:"private"`
	Class   string `json:"class"`
	Return  string `json:"return"`
}

// DeepA -> DeepB -> DeepC (3 levels of embedding).
type DeepC struct {
	ID int `json:"id"`
}

type DeepB struct {
	DeepC
	Name string `json:"name"`
}

type DeepA struct {
	DeepB
	Email string `json:"email"`
}

// PtrSlicePtr has a pointer to a slice of pointers.
type Inner struct {
	Val string `json:"val"`
}

type PtrSlicePtr struct {
	Items *[]*Inner `json:"items,omitempty"`
}

// AllKinds covers every basic Go kind.
type AllKinds struct {
	Bool    bool              `json:"bool"`
	Int     int               `json:"int"`
	Int8    int8              `json:"int8"`
	Int16   int16             `json:"int16"`
	Int32   int32             `json:"int32"`
	Int64   int64             `json:"int64"`
	Uint    uint              `json:"uint"`
	Uint8   uint8             `json:"uint8"`
	Uint16  uint16            `json:"uint16"`
	Uint32  uint32            `json:"uint32"`
	Uint64  uint64            `json:"uint64"`
	Float32 float32           `json:"float32"`
	Float64 float64           `json:"float64"`
	Str     string            `json:"string"`
	Slice   []string          `json:"slice"`
	Map     map[string]string `json:"map"`
	Bytes   []byte            `json:"bytes"`
	Raw     json.RawMessage   `json:"raw"`
	Iface   interface{}       `json:"iface"`
}

// SliceOfSlice is a [][]string field.
type SliceOfSlice struct {
	Matrix [][]string `json:"matrix"`
}

// MapOfSlice is a map[string][]int field.
type MapOfSlice struct {
	Data map[string][]int `json:"data"`
}

// SliceOfMap is a []map[string]string field.
type SliceOfMap struct {
	Items []map[string]string `json:"items"`
}

// ThreeWayCycle: X -> Y -> Z -> X.
type CycleX struct {
	Name string  `json:"name"`
	Y    *CycleY `json:"y,omitempty"`
}

type CycleY struct {
	Name string  `json:"name"`
	Z    *CycleZ `json:"z,omitempty"`
}

type CycleZ struct {
	Name string  `json:"name"`
	X    *CycleX `json:"x,omitempty"`
}

// EmptyStruct has no fields.
type EmptyStruct struct{}

// AllOptional has only optional fields.
type AllOptional struct {
	A *string `json:"a,omitempty"`
	B *int    `json:"b,omitempty"`
}

// Ambiguous has embedding ambiguity at the same depth.
type AmbigLeft struct {
	Name string `json:"name"`
}

type AmbigRight struct {
	Name string `json:"name"`
}

type Ambiguous struct {
	AmbigLeft
	AmbigRight
	ID int `json:"id"`
}

// DirectWins has a direct field that overrides an embedded one.
type EmbBase struct {
	Name string `json:"name"`
}

type DirectWins struct {
	EmbBase
	Name string `json:"name"`
}

// MapOfStructs has a map value that is a registered struct.
type MapVal struct {
	X int `json:"x"`
}

type MapOfStructs struct {
	Data map[string]MapVal `json:"data"`
}

// NestedOptPtr has an optional pointer to a struct.
type NestedOptPtr struct {
	Inner *Inner `json:"inner,omitempty"`
}

// OptionalEnum tests optional enum fields.
type MyEnum string

type HasOptEnum struct {
	Status *MyEnum `json:"status,omitempty"`
	Name   string  `json:"name"`
}
