// Package b declares an Outcome enum, as package a does.
package b

// Outcome is a string enum.
type Outcome string

// OutcomeDone is an Outcome.
const OutcomeDone Outcome = "done-b"

// Thing shares its bare name with a.Thing.
type Thing struct {
	Y int `json:"y"`
}

// Report uses this package's Outcome and Thing.
type Report struct {
	Outcome Outcome `json:"outcome"`
	Thing   Thing   `json:"thing"`
}
