package edges

import "time"

// --- Generic alias (Go 1.22+ types.Alias regression) ---

// AliasOfAlias is a chain: MyString -> string, then alias of alias.
type AliasOfAlias = MyString

// HasAliasOfAlias uses a doubly-aliased type.
type HasAliasOfAlias struct {
	Val AliasOfAlias `json:"val"`
}

// --- Pointer to alias ---

// HasPtrAlias has *MyString (pointer to type alias).
type HasPtrAlias struct {
	Name *MyString `json:"name,omitempty"`
}

// --- time.Time through alias chain ---
type MyTime = time.Time
type MyTimeAlias = MyTime

type HasDeepTimeAlias struct {
	At MyTimeAlias `json:"at"`
}

// --- Embedded pointer to struct with field conflicts ---

type EmbedPtrBase struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type HasEmbedPtr struct {
	*EmbedPtrBase
	Name string `json:"name"` // direct override of embedded ptr field
}

// --- Map with struct value that is NOT registered (should be unknown) ---

type MapOfUnregistered struct {
	Data map[string]EmbedPtrBase `json:"data"`
}

// --- Slice of interface ---

type SliceOfIface struct {
	Items []interface{} `json:"items"`
}

// --- Field with json tag that has many options ---

type ManyOptions struct {
	A string `json:"a,omitempty,string"`
}

// --- Struct with only unexported fields ---

type OnlyUnexported struct {
	hidden string //nolint:unused
	secret int    //nolint:unused
}

// --- Nested embedded with json:"-" at various levels ---

type EmbedWithDash struct {
	Visible string `json:"visible"`
	Hidden  string `json:"-"`
}

type HasEmbedWithDash struct {
	EmbedWithDash
	Extra string `json:"extra"`
}
