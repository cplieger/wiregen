// Package a declares an Outcome enum, as package b does.
package a

// Outcome is a string enum.
type Outcome string

// OutcomeDone is an Outcome.
const OutcomeDone Outcome = "done-a"

// Thing shares its bare name with b.Thing.
type Thing struct {
	X int `json:"x"`
}
