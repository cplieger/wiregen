package wiregen_test

import (
	"context"
	"testing"
)

// mustGen runs one of the three generators that load the registered packages
// from source (passed as a bound method, e.g. r.GenerateTypes) under the test's
// own context, and fails the test on a config error. The v2 API returns errors
// instead of panicking, so nearly every test call site funnels through here.
func mustGen(t *testing.T, fn func(context.Context) (string, error)) string {
	t.Helper()
	out, err := fn(t.Context())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	return out
}

// mustGenNoLoad is the same for the generators that render from the registry
// alone and therefore take no context.
func mustGenNoLoad(t *testing.T, fn func() (string, error)) string {
	t.Helper()
	out, err := fn()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	return out
}
