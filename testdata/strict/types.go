// Package strict provides fixtures for the fields Generate refuses and the
// opaque or string-encoded types it accepts.
package strict

import (
	"encoding/json"
	"encoding/json/jsontext"
	"fmt"
	"time"
)

// HasDuration carries a time.Duration, which encoding/json writes as integer
// nanoseconds.
type HasDuration struct {
	Timeout time.Duration `json:"timeout"`
}

// Unregistered is never registered.
type Unregistered struct {
	X int `json:"x"`
}

// Unknowns holds one field of each shape with no checked TS type.
type Unknowns struct {
	Any        any             `json:"any"`
	Iface      fmt.Stringer    `json:"iface"`
	Inline     struct{ X int } `json:"inline"`
	Unresolved Unregistered    `json:"unresolved"`
}

// Level encodes through a value-receiver MarshalText.
type Level int

// MarshalText implements encoding.TextMarshaler.
func (l Level) MarshalText() ([]byte, error) { return fmt.Appendf(nil, "L%d", int(l)), nil }

// Opaque holds the accepted opaque and text-encoded fields.
type Opaque struct {
	Raw   json.RawMessage `json:"raw"`
	Value jsontext.Value  `json:"value"`
	Level Level           `json:"level"`
	ByLvl map[Level]int   `json:"by_level"`
}

// Custom writes its own JSON shape.
type Custom struct{ v int }

// MarshalJSON implements json.Marshaler.
func (c *Custom) MarshalJSON() ([]byte, error) { return json.Marshal(c.v) }

// PtrText has a pointer-receiver MarshalText, which encoding/json skips for a
// non-addressable value.
type PtrText int

// MarshalText implements encoding.TextMarshaler on *PtrText.
func (p *PtrText) MarshalText() ([]byte, error) { return []byte("p"), nil }

// Base is embedded so a refused promoted field's path runs through it.
type Base struct {
	Timeout time.Duration `json:"timeout"`
}

// Exotic holds the remaining refused shapes.
type Exotic struct {
	Base
	Items   []any              `json:"items"`
	ByFloat map[float64]string `json:"by_float"`
	Ch      chan int           `json:"ch"`
	Fn      func()             `json:"fn"`
	Cplx    complex128         `json:"cplx"`
	Fixed   [4]int             `json:"fixed"`
	Custom  Custom             `json:"custom"`
	Text    PtrText            `json:"text"`
	Nested  map[string][]any   `json:"nested"`
	Skipped any                `json:"-"`
}

// T has an untagged exported field.
type T struct{ Value string }

// Tagged has only tagged fields.
type Tagged struct {
	Value string `json:"value"`
}

// JSONObj is registered but encodes through MarshalJSON.
type JSONObj struct {
	A string `json:"a"`
}

// MarshalJSON implements json.Marshaler.
func (JSONObj) MarshalJSON() ([]byte, error) { return []byte(`"x"`), nil }

// TextObj is registered but encodes through a value-receiver MarshalText.
type TextObj struct {
	B int `json:"b"`
}

// MarshalText implements encoding.TextMarshaler.
func (TextObj) MarshalText() ([]byte, error) { return []byte("t"), nil }

// Stamped embeds time.Time, whose promoted MarshalJSON encodes the whole
// struct as a time.
type Stamped struct {
	time.Time
	Note string `json:"note"`
}

// HoldsMarshalers names the registered marshaler types.
type HoldsMarshalers struct {
	O JSONObj `json:"o"`
	T TextObj `json:"t"`
}

// Misleading has a MarshalText of the wrong signature, which is no
// encoding.TextMarshaler, so encoding/json writes its fields.
type Misleading struct {
	Value string `json:"value"`
}

// MarshalText is not encoding.TextMarshaler's method.
func (Misleading) MarshalText() string { return "m" }

// MisleadingName is a string with a MarshalText of the wrong signature, so
// encoding/json writes the string itself.
type MisleadingName string

// MarshalText is not encoding.TextMarshaler's method.
func (MisleadingName) MarshalText() string { return "m" }

// Hex16 is a fixed-size array that encodes through a value MarshalText.
type Hex16 [16]byte

// MarshalText implements encoding.TextMarshaler.
func (h Hex16) MarshalText() ([]byte, error) { return fmt.Appendf(nil, "%x", h[:]), nil }

// Raw16 is a bare fixed-size array.
type Raw16 [16]byte

// MisleadingKey has a wrong-signature MarshalText, so it is no map key.
type MisleadingKey struct{ K int }

// MarshalText is not encoding.TextMarshaler's method.
func (MisleadingKey) MarshalText() string { return "k" }

// Codecs holds the marshaler shapes encoding/json reads by interface.
type Codecs struct {
	Item  Misleading             `json:"item"`
	Name  MisleadingName         `json:"name"`
	ID    Hex16                  `json:"id"`
	Raw   Raw16                  `json:"raw"`
	ByKey map[MisleadingKey]bool `json:"by_key"`
}

// Millis is an integer a mapping gives its own wire form.
type Millis int64

// Digit is a bounded integer.
type Digit int

// Range bounds a Digit.
func (Digit) Range() (lo, hi int64) { return 0, 9 }

// Quoted holds the ,string option on every shape.
type Quoted struct {
	N      int     `json:"n,string"`
	P      *int    `json:"p,string"`
	Values []int   `json:"values,string"`
	PP     **int   `json:"pp,string"`
	Lvl    Level   `json:"lvl,string"`
	F      float64 `json:"f,string"`
	M      Millis  `json:"m,string"`
	D      Digit   `json:"d,string"`
}

// Named holds tag names every encoding/json reads alike.
type Named struct {
	Punct string `json:"a-b.c:d"`
	Space string `json:"a b"`
	Plain string `json:"plain"`
}

// BadNames holds tag names encoding/json versions read differently.
type BadNames struct {
	Escaped string `json:"a\\b"`
	Quote   string `json:"q\"b"`
	Euro    string `json:"€"`
}

// TextArray holds the accepted counterparts of Codecs' refused fields.
type TextArray struct {
	ID   Hex16          `json:"id"`
	Name MisleadingName `json:"name"`
}

// Loud is a string enum whose MarshalText writes something other than its
// value.
type Loud string

// LoudOne is Loud's member.
const LoudOne Loud = "one"

// MarshalText implements encoding.TextMarshaler.
func (l Loud) MarshalText() ([]byte, error) { return []byte("ONE"), nil }

// Coded is an integer enum that also implements json.Marshaler.
type Coded uint8

// CodedOne is Coded's member.
const CodedOne Coded = 1

// MarshalText implements encoding.TextMarshaler.
func (Coded) MarshalText() ([]byte, error) { return []byte("one"), nil }

// MarshalJSON implements json.Marshaler.
func (Coded) MarshalJSON() ([]byte, error) { return []byte(`1`), nil }
