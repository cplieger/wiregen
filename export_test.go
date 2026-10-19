package wiregen

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"golang.org/x/tools/go/packages"
)

// The suite generates from the same fixture packages hundreds of times, and
// a load runs the go command: one load per root set keeps the race run
// inside go test's default timeout. A cancelled context still loads, so the
// cancellation tests see the real error.
func init() {
	load := loadPackages
	var mu sync.Mutex
	cache := map[string]func() ([]*packages.Package, error){}
	loadPackages = func(ctx context.Context, paths []string) ([]*packages.Package, error) {
		if ctx.Err() != nil {
			return load(ctx, paths)
		}
		key := strings.Join(paths, "\x00")
		mu.Lock()
		once, ok := cache[key]
		if !ok {
			once = sync.OnceValues(func() ([]*packages.Package, error) { return load(context.Background(), paths) })
			cache[key] = once
		}
		mu.Unlock()
		return once()
	}
}

// RenderFile renders one generated file in memory, exactly as Generate would
// write it: role is types, decoders, validators, events, constants, client,
// arbitraries or openapi. The TS directory defaults to ".", the arbitraries
// file is always rendered and the openapi role writes "openapi.json"; opts
// override all three.
func RenderFile(ctx context.Context, r *Registry, role string, opts ...Option) (string, error) {
	defaults := []Option{WithTSDir("."), WithArbitrariesFile("arbitraries.gen.ts")}
	if role == "openapi" {
		defaults = append(defaults, WithOpenAPIFiles("openapi.json"))
	}
	_, files, err := r.render(ctx, append(defaults, opts...))
	if err != nil {
		return "", err
	}
	for _, f := range files {
		if f.role == role {
			return f.content, nil
		}
	}
	return "", fmt.Errorf("no %s file rendered", role)
}
