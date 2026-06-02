package wiregen_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/cplieger/wiregen"
)

// --- Recursive/self-referential types ---

type TreeNode struct {
	*TreeNode
	Value string `json:"value"`
}

func TestRecursiveEmbeddedStruct(t *testing.T) {
	r := &wiregen.Registry{
		WireTypes:        []reflect.Type{reflect.TypeFor[TreeNode]()},
		ValidatorsImport: "./v.js",
		BusImport:        "./b.js",
	}
	// Must not infinite-loop/stack-overflow.
	out := r.GenerateTypes()
	if !strings.Contains(out, "value: string;") {
		t.Errorf("expected value field, got:\n%s", out)
	}
}

// --- Slice-of-enum: enum validation must be enforced for array elements ---

type Priority string

type TaskWithPriorities struct {
	Name       string     `json:"name"`
	Priorities []Priority `json:"priorities"`
}

func TestSliceOfEnumValidation(t *testing.T) {
	r := &wiregen.Registry{
		WireTypes: []reflect.Type{reflect.TypeFor[TaskWithPriorities]()},
		Enums: map[string]wiregen.EnumDef{
			"Priority": {Values: []string{"low", "medium", "high"}},
		},
		ValidatorsImport: "./v.js",
		BusImport:        "./b.js",
	}
	dec := r.GenerateDecoders()
	if !strings.Contains(dec, "PRIORITYS") {
		t.Errorf("should validate enum values in slice element decoder, got:\n%s", dec)
	}
}

// --- Output determinism ---

func TestOutputDeterminism(t *testing.T) {
	makeRegistry := func() *wiregen.Registry {
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
			ValidatorsImport: "./test-validators.js",
			BusImport:        "./test-bus.js",
		}
	}

	r := makeRegistry()
	typesRef := r.GenerateTypes()
	decodersRef := r.GenerateDecoders()
	registryRef := r.GenerateRegistry()

	for i := range 50 {
		r2 := makeRegistry()
		if got := r2.GenerateTypes(); got != typesRef {
			t.Fatalf("iteration %d: GenerateTypes output differs", i)
		}
		if got := r2.GenerateDecoders(); got != decodersRef {
			t.Fatalf("iteration %d: GenerateDecoders output differs", i)
		}
		if got := r2.GenerateRegistry(); got != registryRef {
			t.Fatalf("iteration %d: GenerateRegistry output differs", i)
		}
	}
}
