package edges

import "time"

// TypeAlias tests that type aliases are resolved properly.
type MyString = string
type MyInt = int

// HasAliases uses type aliases as field types.
type HasAliases struct {
	Label MyString `json:"label"`
	Count MyInt    `json:"count"`
}

// TimeAlias is a type alias for time.Time.
type TimeAlias = time.Time

// HasTimeAlias has a type alias for time.Time.
type HasTimeAlias struct {
	At TimeAlias `json:"at"`
}

// DoublePtr has a **string field (should still emit string).
type DoublePtr struct {
	Val **string `json:"val,omitempty"`
}

// MapOfMap has a map[string]map[string]int field.
type MapOfMap struct {
	Nested map[string]map[string]int `json:"nested"`
}

// TagOmitemptyRequired: omitempty but non-pointer → should still be optional.
type TagVariants struct {
	Required  string `json:"required"`
	Omitempty string `json:"omitempty_field,omitempty"`
	Renamed   string `json:"wire_name"`
	NoTag     string
}
