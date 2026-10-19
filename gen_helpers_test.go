package wiregen_test

import (
	"context"
	"testing"

	"github.com/cplieger/wiregen/v4"
)

// testRegistry pairs a registry with the options every render of it takes, so
// a test builds both once and reads any one generated file.
type testRegistry struct {
	wiregen.Registry
	opts []wiregen.Option
}

func newTestRegistry(opts ...wiregen.Option) *testRegistry {
	return &testRegistry{opts: opts}
}

func (r *testRegistry) file(ctx context.Context, role string) (string, error) {
	return wiregen.RenderFile(ctx, &r.Registry, role, r.opts...)
}

func (r *testRegistry) typesFile(ctx context.Context) (string, error) {
	return r.file(ctx, "types")
}

func (r *testRegistry) decodersFile(ctx context.Context) (string, error) {
	return r.file(ctx, "decoders")
}

func (r *testRegistry) validatorsFile(ctx context.Context) (string, error) {
	return r.file(ctx, "validators")
}

func (r *testRegistry) eventsFile(ctx context.Context) (string, error) {
	return r.file(ctx, "events")
}

// event names a frame whose data is the registered type typeName.
func (r *testRegistry) event(name, typeName string) wiregen.Event {
	for _, wt := range r.Types {
		if wt.Name == typeName {
			return wiregen.Event{Name: name, Data: wt}
		}
	}
	panic("event: " + typeName + " is not in Types")
}

func (r *testRegistry) constantsFile(ctx context.Context) (string, error) {
	return r.file(ctx, "constants")
}

func (r *testRegistry) clientFile(ctx context.Context) (string, error) {
	return r.file(ctx, "client")
}

func (r *testRegistry) arbitrariesFile(ctx context.Context) (string, error) {
	return r.file(ctx, "arbitraries")
}

func (r *testRegistry) openapiFile(ctx context.Context) (string, error) {
	return r.file(ctx, "openapi")
}

// generate writes the registry's files under dir.
func (r *testRegistry) generate(ctx context.Context, dir string) error {
	return r.Generate(ctx, append(r.opts, wiregen.WithTSDir(dir))...)
}

// mustGen renders one file through a bound role method (r.typesFile) under
// the test's context and fails the test on an error.
func mustGen(t testing.TB, fn func(context.Context) (string, error)) string {
	t.Helper()
	out, err := fn(t.Context())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	return out
}
