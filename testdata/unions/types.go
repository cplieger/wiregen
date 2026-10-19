// Package unions provides tagged-union fixtures for wiregen's Union tests.
package unions

import "errors"

// Kind names a Timing's shape.
type Kind uint8

// The Timing kinds; the zero is no kind.
const (
	KindIdentity Kind = iota + 1
	KindShift
	KindLinear
)

// MarshalText names a kind on the wire.
func (k Kind) MarshalText() ([]byte, error) {
	switch k {
	case KindIdentity:
		return []byte("identity"), nil
	case KindShift:
		return []byte("shift"), nil
	case KindLinear:
		return []byte("linear"), nil
	}
	return nil, errors.New("unknown kind")
}

// Timing is a union tagged by an enum.
type Timing struct {
	Kind     Kind     `json:"kind"`
	Ms       *int64   `json:"ms,omitempty"`
	Scale    *float64 `json:"scale,omitempty"`
	OffsetMs *int64   `json:"offset_ms,omitempty"`
}

// Shape is a union tagged by a string.
type Shape struct {
	Type   string   `json:"type"`
	Radius float64  `json:"radius,omitempty"`
	Side   float64  `json:"side,omitempty"`
	Tags   []string `json:"tags"`
}

// Holder holds unions as fields.
type Holder struct {
	Timing Timing  `json:"timing"`
	Shapes []Shape `json:"shapes"`
}

// Tree is a union that reaches itself.
type Tree struct {
	Type     string `json:"type"`
	Value    int    `json:"value,omitempty"`
	Children []Tree `json:"children,omitempty"`
}

// Untagged has no field a union could be tagged by.
type Untagged struct {
	Kind *string `json:"kind,omitempty"`
	Name string  `json:"name"`
}
