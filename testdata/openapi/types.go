// Package openapi provides fixtures for wiregen's OpenAPI document tests.
package openapi

import (
	"encoding/json"
	"errors"
	"time"
)

// Level is a uint8 enum whose zero has no wire name.
type Level uint8

// The levels; the zero is no level.
const (
	levelNone Level = iota
	LevelLow
	LevelHigh
)

// MarshalText names a level on the wire, refusing the zero.
func (l Level) MarshalText() ([]byte, error) {
	switch l {
	case LevelLow:
		return []byte("low"), nil
	case LevelHigh:
		return []byte("high"), nil
	}
	return nil, errors.New("invalid level")
}

// UnmarshalText reads a level's name.
func (l *Level) UnmarshalText(b []byte) error {
	switch string(b) {
	case "low":
		*l = LevelLow
	case "high":
		*l = LevelHigh
	default:
		return errors.New("invalid level")
	}
	return nil
}

var _ = levelNone

// Name is a bounded string.
type Name string

// MaxBytes bounds a Name in UTF-8 bytes.
func (Name) MaxBytes() int { return 510 }

// Pattern refuses a slash.
func (Name) Pattern() string { return `^[^/]*$` }

// Limit is a bounded integer.
type Limit int

// Range bounds a Limit.
func (Limit) Range() (lo, hi int64) { return 1, 500 }

// Files is a bounded list of names.
type Files []Name

// Count bounds Files.
func (Files) Count() (lo, hi int) { return 1, 3 }

// Item is one row, alike in both directions.
type Item struct {
	Name Name  `json:"name"`
	ID   int64 `json:"id"`
}

// Shared crosses both ways, and a nil pointer is required in a response but
// optional in a request, so its two schemas differ.
type Shared struct {
	Hint *string `json:"hint"`
}

// Detail is a response body.
type Detail struct {
	Item    *Item             `json:"item"`
	Note    *string           `json:"note,omitempty"`
	Labels  map[string]string `json:"labels"`
	Raw     json.RawMessage   `json:"raw"`
	At      time.Time         `json:"at"`
	Items   []Item            `json:"items"`
	Blob    []byte            `json:"blob"`
	Shared  Shared            `json:"shared"`
	Count   uint32            `json:"count"`
	Level   Level             `json:"level"`
	Enabled bool              `json:"enabled"`
}

// Create is a request body.
type Create struct {
	Item   *Item             `json:"item"`
	Labels map[string]string `json:"labels"`
	Files  Files             `json:"files"`
	Tags   []string          `json:"tags,omitempty"`
	Shared Shared            `json:"shared"`
	Limit  Limit             `json:"limit"`
	Level  Level             `json:"level"`
}

// ItemParams are a path's placeholders.
type ItemParams struct {
	ID int64 `json:"id"`
}

// ListQuery is a query string.
type ListQuery struct {
	Limit  *Limit `json:"limit,omitempty"`
	Cursor string `json:"cursor"`
}

// Problem is the error envelope.
type Problem struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Tick is an event's data.
type Tick struct {
	Seq uint64 `json:"seq"`
}

// Kind tags a Shift.
type Kind string

// The shift kinds.
const (
	KindNone  Kind = "none"
	KindShift Kind = "shift"
)

// Shift is a union tagged by a string enum.
type Shift struct {
	Kind Kind   `json:"kind"`
	Ms   *int64 `json:"ms,omitempty"`
}

// Retime is a request carrying a union.
type Retime struct {
	Shift Shift `json:"shift"`
}

// Stamp marshals itself, so only a mapping gives it a shape.
type Stamp struct{ ms int64 }

// MarshalJSON writes the stamp as an integer.
func (s Stamp) MarshalJSON() ([]byte, error) { return json.Marshal(s.ms) }

// Stamped carries a mapped type.
type Stamped struct {
	At Stamp `json:"at"`
}

// BadRange declares Range on a string.
type BadRange string

// Range bounds nothing a string has.
func (BadRange) Range() (lo, hi int64) { return 1, 2 }

// PtrBound has a pointer-receiver bound method.
type PtrBound string

// MaxBytes has a pointer receiver.
func (*PtrBound) MaxBytes() int { return 3 }

// Computed returns a value computed at run time.
type Computed string

// MaxBytes is not one return of constants.
func (Computed) MaxBytes() int { n := 3; return n }

// EmptyRange declares an empty range.
type EmptyRange int

// Range is empty.
func (EmptyRange) Range() (lo, hi int64) { return 5, 1 }

// RE2Only's pattern is not one Go's regexp compiles.
type RE2Only string

// Pattern has an unclosed group.
func (RE2Only) Pattern() string { return "(a" }

// Misbound gathers the refused bounds.
type Misbound struct {
	A BadRange   `json:"a"`
	B PtrBound   `json:"b"`
	C Computed   `json:"c"`
	D EmptyRange `json:"d"`
	E RE2Only    `json:"e"`
}

// Inner's request and response schemas differ: P is optional only in a
// request.
type Inner struct {
	P *int `json:"p"`
}

// Wrapper is reached from no endpoint and names a request-only type and a
// parameters-only type.
type Wrapper struct {
	I Inner      `json:"i"`
	Q ItemParams `json:"q"`
}
