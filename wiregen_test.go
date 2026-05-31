package wiregen_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/cplieger/wiregen"
)

// Sample types for testing — these simulate what a consumer would register.

type Status string // enum

type Address struct {
	Street string `json:"street"`
	City   string `json:"city"`
	Zip    string `json:"zip,omitempty"`
}

type User struct {
	ID      int      `json:"id"`
	Name    string   `json:"name"`
	Email   string   `json:"email,omitempty"`
	Age     *int     `json:"age,omitempty"`
	Status  Status   `json:"status"`
	Address Address  `json:"address"`
	Tags    []string `json:"tags,omitempty"`
}

type Notification struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

func newRegistry() *wiregen.Registry {
	return &wiregen.Registry{
		WireTypes: []reflect.Type{
			reflect.TypeFor[Address](),
			reflect.TypeFor[User](),
			reflect.TypeFor[Notification](),
		},
		Enums: map[string]wiregen.EnumDef{
			"Status": {Values: []string{"active", "inactive", "banned"}},
		},
		SSEEvents: []wiregen.SSERegEntry{
			{EventType: "notification", TypeName: "Notification"},
		},
		ValidatorsImport: "../validators.js",
		BusImport:        "../bus.js",
	}
}

func TestGenerateTypes(t *testing.T) {
	r := newRegistry()
	out := r.GenerateTypes()

	// Check enum
	if !strings.Contains(out, `export type Status = "active" | "inactive" | "banned";`) {
		t.Errorf("missing Status enum type, got:\n%s", out)
	}
	// Check interface
	if !strings.Contains(out, "export interface User {") {
		t.Errorf("missing User interface, got:\n%s", out)
	}
	if !strings.Contains(out, "  id: number;") {
		t.Errorf("missing required id field, got:\n%s", out)
	}
	if !strings.Contains(out, "  email?: string;") {
		t.Errorf("missing optional email field, got:\n%s", out)
	}
	if !strings.Contains(out, "  address: Address;") {
		t.Errorf("missing nested struct field, got:\n%s", out)
	}
	if !strings.Contains(out, "  tags?: string[];") {
		t.Errorf("missing optional tags field, got:\n%s", out)
	}
	if !strings.Contains(out, "export interface Address {") {
		t.Errorf("missing Address interface, got:\n%s", out)
	}
}

func TestGenerateDecoders(t *testing.T) {
	r := newRegistry()
	out := r.GenerateDecoders()

	if !strings.Contains(out, "export const decodeUser: Decoder<User>") {
		t.Errorf("missing decodeUser, got:\n%s", out)
	}
	if !strings.Contains(out, "export const decodeAddress: Decoder<Address>") {
		t.Errorf("missing decodeAddress, got:\n%s", out)
	}
	if !strings.Contains(out, "reqOneOf(o, \"status\", STATUSS") {
		t.Errorf("missing enum validation for status, got:\n%s", out)
	}
	if !strings.Contains(out, "decodeAddress(o[\"address\"])") {
		t.Errorf("missing nested struct decoder call, got:\n%s", out)
	}
	// Check validators import
	if !strings.Contains(out, `from "../validators.js"`) {
		t.Errorf("missing validators import, got:\n%s", out)
	}
}

func TestGenerateRegistry(t *testing.T) {
	r := newRegistry()
	out := r.GenerateRegistry()

	if !strings.Contains(out, `registerSSEDecoder("notification", decodeNotification)`) {
		t.Errorf("missing SSE registration, got:\n%s", out)
	}
	if !strings.Contains(out, `from "../bus.js"`) {
		t.Errorf("missing bus import, got:\n%s", out)
	}
}

func TestGenerateToDir(t *testing.T) {
	r := newRegistry()
	dir := t.TempDir()
	if err := r.Generate(dir); err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
}
