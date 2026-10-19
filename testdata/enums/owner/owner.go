// Package owner declares an enum and no registered type.
package owner

// Role is a string enum.
type Role string

const (
	RoleAdmin Role = "admin"
	RoleUser  Role = "user"
)
