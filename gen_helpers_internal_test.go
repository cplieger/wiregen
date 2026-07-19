package wiregen

import "testing"

// mustGen is the internal-package twin of the external test helper: runs a
// per-file string generator and fails the test on a config error.
func mustGen(t *testing.T, fn func() (string, error)) string {
	t.Helper()
	out, err := fn()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	return out
}
