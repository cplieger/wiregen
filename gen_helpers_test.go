package wiregen_test

import "testing"

// mustGen runs a per-file string generator (passed as a bound method, e.g.
// r.GenerateTypes) and fails the test on a config error. The v2 API returns
// errors instead of panicking, so nearly every test call site funnels
// through here.
func mustGen(t *testing.T, fn func() (string, error)) string {
	t.Helper()
	out, err := fn()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	return out
}
