// Package oracle provides the fixtures the encoding/json differential
// property test draws values over: every embed, ,string, omit option,
// marshaler and mapping shape whose wire form the generated code must follow.
package oracle

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// Color is a string enum.
type Color string

// The colors.
const (
	Red  Color = "red"
	Blue Color = "blue"
)

// Level is an integer enum named by its MarshalText, whose zero is no level.
type Level uint8

// The levels.
const (
	levelNone Level = iota
	Low
	High
)

var _ = levelNone

// MarshalText names a level, refusing the zero.
func (l Level) MarshalText() ([]byte, error) {
	switch l {
	case Low:
		return []byte("low"), nil
	case High:
		return []byte("high"), nil
	}
	return nil, errors.New("no level")
}

// UnmarshalText reads a level's name.
func (l *Level) UnmarshalText(b []byte) error {
	switch string(b) {
	case "low":
		*l = Low
	case "high":
		*l = High
	default:
		return errors.New("no level")
	}
	return nil
}

// Text marshals itself as text, so it crosses as a string.
type Text struct{ s string }

// NewText returns the Text holding s.
func NewText(s string) Text { return Text{s: s} }

// MarshalText writes the text with a prefix.
func (t Text) MarshalText() ([]byte, error) { return []byte("t:" + t.s), nil }

// UnmarshalText reads what MarshalText writes.
func (t *Text) UnmarshalText(b []byte) error {
	s, ok := strings.CutPrefix(string(b), "t:")
	if !ok {
		return errors.New("no t: prefix")
	}
	t.s = s
	return nil
}

// Stamp marshals itself as an integer, so only a mapping gives it a shape.
type Stamp struct{ ms int64 }

// NewStamp returns the Stamp of ms.
func NewStamp(ms int64) Stamp { return Stamp{ms: ms} }

// MarshalJSON writes the stamp as an integer.
func (s Stamp) MarshalJSON() ([]byte, error) { return json.Marshal(s.ms) }

// UnmarshalJSON reads an integer.
func (s *Stamp) UnmarshalJSON(b []byte) error { return json.Unmarshal(b, &s.ms) }

// Millis is an integer a mapping gives its own TS type.
type Millis int64

// Inner is a nested struct.
type Inner struct {
	B *string `json:"b,omitempty"`
	A int     `json:"a"`
}

// Base is embedded by value.
type Base struct {
	Tag string `json:"tag,omitempty"`
	ID  int    `json:"id"`
}

// Extra is embedded through a pointer.
type Extra struct {
	E bool `json:"e"`
}

type base struct {
	N      *int   `json:"n,omitempty"`
	Hidden string `json:"hidden"`
}

type hidden struct {
	H string `json:"h"`
}

// Fields holds every shape that crosses in both directions.
type Fields struct {
	OZTime   time.Time         `json:"oz_time,omitzero"`
	At       time.Time         `json:"at"`
	OEPtr    *string           `json:"oe_ptr,omitempty"`
	PP       **string          `json:"pp"`
	PSlice   *[]int            `json:"p_slice,omitempty"`
	QPtr     *int64            `json:"q_ptr,string"`
	TextPtr  *Text             `json:"text_ptr"`
	OEMap    map[string]int    `json:"oe_map,omitempty"`
	MapPtr   map[string]*Inner `json:"map_ptr"`
	Levels   map[string]Level  `json:"levels"`
	IntKeys  map[int]string    `json:"int_keys"`
	LevelKey map[Level]int     `json:"level_key"`
	*Extra
	QStr     string          `json:"q_str,string"`
	OEStr    string          `json:"oe_str,omitempty"`
	Text     Text            `json:"text"`
	Stamp    Stamp           `json:"stamp"`
	Color    Color           `json:"color"`
	QEnum    Color           `json:"q_enum,string"`
	OESlice  []string        `json:"oe_slice,omitempty"`
	Colors   []Color         `json:"colors"`
	Bytes    []byte          `json:"bytes"`
	Raw      json.RawMessage `json:"raw"`
	Elems    []*int          `json:"elems"`
	Nested   [][]string      `json:"nested"`
	OZStruct Inner           `json:"oz_struct,omitzero"`
	OEStruct Inner           `json:"oe_struct,omitempty"`
	Base
	base
	QInt   int     `json:"q_int,string"`
	QFloat float64 `json:"q_float,string"`
	OEInt  int     `json:"oe_int,omitempty"`
	F64    float64 `json:"f64"`
	I64    int64   `json:"i64"`
	Millis Millis  `json:"millis"`
	U8     uint8   `json:"u8"`
	Level  Level   `json:"level"`
	QBool  bool    `json:"q_bool,string"`
	Flag   bool    `json:"flag"`
}

// Resp is a response body; it promotes fields through an embedded pointer to
// an unexported struct, which encoding/json writes but cannot decode.
type Resp struct {
	*hidden
	Fields
}

// Req is a request body.
type Req struct {
	Fields
}

// SeedReqEmbedPtr is a request promoting a field through an embedded pointer
// to an unexported struct.
type SeedReqEmbedPtr struct {
	*hidden
	ID int `json:"id"`
}

// SeedQuotedMapped is a mapped type under ,string.
type SeedQuotedMapped struct {
	M Millis `json:"m,string"`
}

// OutLevel has MarshalText but no UnmarshalText.
type OutLevel uint8

// MarshalText names the level.
func (l OutLevel) MarshalText() ([]byte, error) { return []byte("l" + string(rune('0'+l%10))), nil }

// SeedTextNoUnmarshal is a request carrying a type encoding/json writes as
// text but cannot read back.
type SeedTextNoUnmarshal struct {
	L OutLevel `json:"l"`
}
