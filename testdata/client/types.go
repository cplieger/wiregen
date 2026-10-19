// Package client provides the endpoint fixtures the client emitter and its
// integration test generate from.
package client

// Kind is a string enum carried in a query.
type Kind string

const (
	KindMovie  Kind = "movie"
	KindSeries Kind = "series"
)

// ItemParams names GET /api/items/{id}'s placeholder.
type ItemParams struct {
	ID string `json:"id"`
}

// OtherParams names its one placeholder other.
type OtherParams struct {
	Other string `json:"other"`
}

// ListQuery is a query of an optional cursor, limit and kind.
type ListQuery struct {
	Cursor string `json:"cursor,omitempty"`
	Kind   Kind   `json:"kind,omitempty"`
	Limit  int    `json:"limit,omitempty"`
	Fresh  bool   `json:"fresh,omitempty"`
}

// RequiredQuery is a query whose one field is required.
type RequiredQuery struct {
	Key int `json:"key"`
}

// Item is a response row.
type Item struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// StartRequest is a job start's body.
type StartRequest struct {
	Item string `json:"item"`
}

// Job is a job start's answer.
type Job struct {
	ID string `json:"id"`
}

// ErrorBody is every non-2xx JSON body.
type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Position is an event frame's data.
type Position struct {
	Rev int `json:"rev"`
}

// Tick is a second event frame's data.
type Tick struct {
	At int `json:"at"`
}

// SliceParams has a field no URL argument can carry.
type SliceParams struct {
	IDs []string `json:"ids"`
}

// NestedQuery has a struct field no URL argument can carry.
type NestedQuery struct {
	Item Item `json:"item"`
}

// OptionalParams has an optional placeholder field.
type OptionalParams struct {
	ID string `json:"id,omitempty"`
}

// BodyQuery has a field named body, which a request body's argument takes.
type BodyQuery struct {
	Body string `json:"body"`
}
