package edges

import (
	"encoding/json"
	"time"
)

// TriplePtr has a ***string (multi-level pointer unwrap).
type TriplePtr struct {
	Val ***string `json:"val,omitempty"`
}

// PtrToSlice has *[]string (pointer to slice).
type PtrToSlice struct {
	Items *[]string `json:"items,omitempty"`
}

// PtrToMap has *map[string]int (pointer to map).
type PtrToMap struct {
	Data *map[string]int `json:"data,omitempty"`
}

// SliceOfPtrs has []*string.
type SliceOfPtrs struct {
	Names []*string `json:"names"`
}

// MapOfPtrs has map[string]*int.
type MapOfPtrs struct {
	Scores map[string]*int `json:"scores"`
}

// TimeSlice has []time.Time.
type TimeSlice struct {
	Dates []time.Time `json:"dates"`
}

// OptionalByteSlice has *[]byte.
type OptionalByteSlice struct {
	Data *[]byte `json:"data,omitempty"`
}

// MapOfBytes has map[string][]byte.
type MapOfBytes struct {
	Blobs map[string][]byte `json:"blobs"`
}

// DeeplyNestedMap has map[string]map[string]map[string]string.
type DeeplyNestedMap struct {
	Deep map[string]map[string]map[string]string `json:"deep"`
}

// SliceOfSliceOfSlice has [][][]int.
type SliceOfSliceOfSlice struct {
	Cube [][][]int `json:"cube"`
}

// RawSlice has []json.RawMessage.
type RawSlice struct {
	Items []json.RawMessage `json:"items"`
}

// InterfaceSlice has []interface{}.
type InterfaceSlice struct {
	Items []interface{} `json:"items"`
}

// EmbedWithTag has an embedded struct whose fields clash with direct fields.
type EmbedBase2 struct {
	X string `json:"x"`
	Y string `json:"y"`
}

type EmbedOverride struct {
	EmbedBase2
	X int `json:"x"` // Direct field wins over embedded
}

// TwoLevelEmbed: A -> B -> C, with field override at different depths.
type Level3 struct {
	Name string `json:"name"`
	Age  int    `json:"age"`
}

type Level2 struct {
	Level3
	Name string `json:"name"` // overrides Level3.Name at depth 1
}

type Level1 struct {
	Level2
	// Does NOT override Name, so Level2.Name wins (depth 1 < depth 2)
	Email string `json:"email"`
}

// TagEmpty: json tag is just a comma (field name = Go name, options after comma).
type TagEmpty struct {
	Value string `json:",omitempty"` // wire name = "Value", optional
}

// TagOnlyOptions: json tag that starts with comma.
type TagOnlyOptions struct {
	Count int `json:",string"` // wire name = "Count", string-encoded
}

// AllPointerFields: every field is a pointer.
type AllPointerFields struct {
	A *string  `json:"a"`
	B *int     `json:"b"`
	C *bool    `json:"c"`
	D *float64 `json:"d"`
}

// StructWithRawAndTime: combination of json.RawMessage + time.Time.
type StructWithRawAndTime struct {
	Payload json.RawMessage `json:"payload"`
	When    time.Time       `json:"when"`
	Label   string          `json:"label"`
}

// PrivateEmbedded: embeds unexported type (should be skipped).
type privateBase struct {
	Secret string `json:"secret"`
}

type HasPrivateEmbed struct {
	Name string `json:"name"`
	privateBase
}

// HasJSONNumber has an encoding/json.Number field. encoding/json marshals a
// json.Number as an unquoted JSON number, so it maps to TS number.
type HasJSONNumber struct {
	Amount json.Number `json:"amount"`
	Name   string      `json:"name"`
}

// PlainName promotes an untagged "Name" field (wire name = Go field name).
type PlainName struct {
	Name string
}

// TaggedName promotes a tagged field whose wire name is also "Name".
type TaggedName struct {
	Renamed string `json:"Name"`
}

// TaggedDominatesA embeds the tagged struct first; both promote wire "Name"
// at depth 1, and the tagged field must win (not an ambiguous drop).
type TaggedDominatesA struct {
	TaggedName
	PlainName
}

// TaggedDominatesB embeds the untagged struct first; the tagged field must
// still win regardless of declaration order.
type TaggedDominatesB struct {
	PlainName
	TaggedName
}

// TaggedEmbed carries an explicit json name on an embedded struct: per
// encoding/json a tagged embed becomes a NAMED nested field, not flattened.
type TaggedEmbed struct {
	Inner `json:"meta"`
	Extra string `json:"extra"`
}

// DashEmbed excludes an embedded struct via json:"-": the whole embed and its
// promoted fields are dropped, not flattened.
type DashEmbed struct {
	Inner `json:"-"`
	Kept  string `json:"kept"`
}
