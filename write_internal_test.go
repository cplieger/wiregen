package wiregen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A target that cannot be replaced fails the pass naming the file, leaves
// the earlier staged files unpublished and no temp behind.
func TestWriteFiles_unreplaceableTargetCleansTemps(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "types.gen.ts"), 0o755); err != nil {
		t.Fatal(err)
	}
	err := writeFiles(t.Context(), dir, []genFile{
		{role: "decoders", name: "decoders.gen.ts", content: "x", ts: true},
		{role: "types", name: "types.gen.ts", content: "x", ts: true},
	})
	if err == nil || !strings.Contains(err.Error(), "types.gen.ts") {
		t.Fatalf("writeFiles = %v, want an error naming types.gen.ts", err)
	}
	entries, rerr := os.ReadDir(dir)
	if rerr != nil {
		t.Fatal(rerr)
	}
	for _, e := range entries {
		if e.Name() != "types.gen.ts" {
			t.Errorf("%s left behind by a failed pass", e.Name())
		}
	}
}
