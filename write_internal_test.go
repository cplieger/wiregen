package wiregen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// White-box tests for writeFilesAtomically's error and cleanup paths in
// wiregen.go (the happy path is covered via Generate and FuzzGenerate).

// TestWriteFilesAtomically_renameFailureCleansTemps pins the atomicity
// contract: when a rename into place fails mid-sequence, writeFilesAtomically
// returns the wrapped error AND removes the staged temp so no half-written
// wire/ directory and no leaked *.tmp sibling is left behind. A target name
// that is an existing directory makes os.Rename fail deterministically.
func TestWriteFilesAtomically_renameFailureCleansTemps(t *testing.T) {
	outDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(outDir, "types.gen.ts"), 0o755); err != nil {
		t.Fatal(err)
	}
	err := writeFilesAtomically(outDir, []genFile{{name: "types.gen.ts", content: "x"}})
	if err == nil {
		t.Fatal("expected rename error when the target name is an existing directory")
	}
	if !strings.Contains(err.Error(), "write types.gen.ts") {
		t.Errorf("error not wrapped with target name: %v", err)
	}
	entries, rerr := os.ReadDir(outDir)
	if rerr != nil {
		t.Fatal(rerr)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("staged temp not cleaned up on error path: %s", e.Name())
		}
	}
}
