package wiregen

import (
	"context"
	"testing"
)

// mustGen is the internal-package twin of the external test helper: renders
// one file through a bound role method (r.typesFile) and fails the test on an
// error.
func mustGen(t *testing.T, fn func(context.Context) (string, error)) string {
	t.Helper()
	out, err := fn(t.Context())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	return out
}

func (r *Registry) typesFile(ctx context.Context) (string, error) {
	return RenderFile(ctx, r, "types")
}

func (r *Registry) decodersFile(ctx context.Context) (string, error) {
	return RenderFile(ctx, r, "decoders")
}

// mustGenerator builds one Generate call's state without loading packages,
// for tests of the emitters that render from the registry alone.
func mustGenerator(t *testing.T, r *Registry, opts ...Option) *generator {
	t.Helper()
	g, err := newGenerator(r, append([]Option{WithTSDir(".")}, opts...))
	if err != nil {
		t.Fatalf("newGenerator: %v", err)
	}
	return g
}
