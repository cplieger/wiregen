// Package presence provides fixtures for what encoding/json writes for nil
// pointers, collections and embeds, and for keys a JS object inherits.
package presence

// Base is embedded through a pointer, whose fields encoding/json skips when
// it is nil.
type Base struct {
	ID int `json:"id"`
}

// Inner is a collection element and an omitempty struct.
type Inner struct {
	A int `json:"a"`
}

// Wire holds every pointer, collection and embed shape whose null or
// absence the generated code must follow.
type Wire struct {
	*Base
	Items   *[]string          `json:"items,omitempty"`
	Map     *map[string]int    `json:"map,omitempty"`
	Bytes   *[]byte            `json:"bytes,omitempty"`
	Deep    **int              `json:"deep,omitempty"`
	DeepReq **int              `json:"deep_req"`
	Elems   []*int             `json:"elems"`
	Vals    map[string]*string `json:"vals"`
	Nested  []*Inner           `json:"nested"`
	Struct  Inner              `json:"struct,omitempty"`
	Quoted  *int               `json:"quoted,string"`
	Some    *Some              `json:"some"`
	Octets  []Octet            `json:"octets"`
	Name    string             `json:"name"`
}

// Some holds one to three items.
type Some []string

// Count bounds Some.
func (Some) Count() (lo, hi int) { return 1, 3 }

// Octet is a byte kind, so a slice of it is base64 text.
type Octet uint8

// Inherited names fields after the properties every JS object inherits.
type Inherited struct {
	Meta  map[string]string `json:"meta"`
	Ctor  *string           `json:"constructor,omitempty"`
	ToStr *string           `json:"toString,omitempty"`
	Proto *string           `json:"__proto__,omitempty"`
}

// ProtoReq has a required field named __proto__.
type ProtoReq struct {
	P string `json:"__proto__"`
}
