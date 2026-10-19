// Package enums provides fixtures for enums registered by type.
package enums

import (
	"errors"

	"github.com/cplieger/wiregen/v4/testdata/enums/owner"
)

// Level is a closed uint8 enum whose zero value has no wire name.
type Level uint8

const (
	// levelNone is the zero value, which MarshalText refuses, so it is no member.
	levelNone Level = iota
	LevelLow
	LevelHigh
	// LevelTop shares LevelHigh's value, so it adds no member.
	LevelTop = LevelHigh
)

// MarshalText implements encoding.TextMarshaler, failing on the zero value.
func (l Level) MarshalText() ([]byte, error) {
	switch l {
	case LevelLow:
		return []byte("low"), nil
	case LevelHigh:
		return []byte("high"), nil
	}
	return nil, errors.New("invalid level")
}

// Job carries a required Level.
type Job struct {
	Level Level `json:"level"`
}

// Member carries an enum declared in a package no registered type lives in.
type Member struct {
	Role owner.Role `json:"role"`
}

// Empty is a string enum with no constants.
type Empty string

// Clash has two constants that encode alike.
type Clash uint8

const (
	ClashOne Clash = iota + 1
	ClashTwo
)

// MarshalText implements encoding.TextMarshaler, naming both values "same".
func (Clash) MarshalText() ([]byte, error) { return []byte("same"), nil }
