// Package dep defines a Color enum that collides by name with crossref.Color.
// crossref imports it, so it becomes a transitive dependency in the loaded
// package graph — used to verify enum-value discovery scopes to the root
// packages and does not pull values from same-named types in dependencies.
package dep

// Color collides by name with crossref.Color but has a different value set.
type Color string

// DepRed must never appear in crossref.Color's discovered values.
const DepRed Color = "DEPRED"
