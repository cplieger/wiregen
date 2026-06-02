// Package basic provides test types for wiregen golden-file tests.
package basic

import (
	"encoding/json"
	"time"
)

// Status is a typed string for user status.
type Status string

// Address represents a physical mailing address.
type Address struct {
	Street string `json:"street"`
	City   string `json:"city"`
	Zip    string `json:"zip,omitempty"`
}

// User represents a registered user in the system.
type User struct {
	Age     *int     `json:"age,omitempty"`
	Address Address  `json:"address"`
	Name    string   `json:"name"`
	Email   string   `json:"email,omitempty"`
	Status  Status   `json:"status"`
	Tags    []string `json:"tags,omitempty"`
	ID      int      `json:"id"`
}

// Notification is the payload for toast notifications.
type Notification struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

// HasUnexported has unexported fields that must be skipped.
type HasUnexported struct {
	Name     string `json:"name"`
	internal int    //nolint:unused // intentionally unexported for test
	hidden   string //nolint:unused // intentionally unexported for test
}

// HasBytes has a []byte field that should map to string (base64).
type HasBytes struct {
	Data []byte `json:"data"`
	Name string `json:"name"`
}

// HasOmitzero uses the Go 1.24 omitzero tag option.
type HasOmitzero struct {
	Value string `json:"value,omitzero"`
	Name  string `json:"name"`
}

// HasJSONString uses json:",string" to wrap a number as string on the wire.
type HasJSONString struct {
	BigID int64  `json:"big_id,string"`
	Name  string `json:"name"`
}

// CustomID is a custom type for testing TypeMappings.
type CustomID struct{ Value string }

// HasCustomMapped uses a custom-mapped type.
type HasCustomMapped struct {
	ID   CustomID `json:"id"`
	Name string   `json:"name"`
}

// HasTime has a time.Time field.
type HasTime struct {
	Created time.Time  `json:"created"`
	Updated *time.Time `json:"updated,omitempty"`
}

// HasRaw has a json.RawMessage field.
type HasRaw struct {
	Data json.RawMessage `json:"data"`
	Name string          `json:"name"`
}

// HasMap has a map field.
type HasMap struct {
	Meta map[string]string `json:"meta"`
	Name string            `json:"name"`
}

// HasInterface has an interface field.
type HasInterface struct {
	Payload interface{} `json:"payload"`
	Name    string      `json:"name"`
}

// Base is embedded by other types for testing.
type Base struct {
	ID        int       `json:"id"`
	CreatedAt time.Time `json:"created_at"`
}

// WithEmbedding embeds Base.
type WithEmbedding struct {
	Base
	Name string `json:"name"`
}
