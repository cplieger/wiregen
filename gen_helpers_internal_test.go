package wiregen

import (
	"context"
	"testing"
)

// mustGen is the internal-package twin of the external test helper: runs one of
// the generators that load the registered packages from source and fails the
// test on a config error.
func mustGen(t *testing.T, fn func(context.Context) (string, error)) string {
	t.Helper()
	out, err := fn(t.Context())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	return out
}
