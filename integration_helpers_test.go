//go:build integration

package wiregen_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const tsconfig = `{
  "compilerOptions": {
    "target": "es2022",
    "lib": ["es2022", "dom"],
    "module": "nodenext",
    "moduleResolution": "nodenext",
    "strict": true,
    "noEmitOnError": true,
    "skipLibCheck": true,
    "outDir": "out"
  },
  "include": ["*.ts"]
}
`

// tsProject generates r into a fresh directory and writes files beside the
// generated ones, with a tsconfig and an ES-module package.json.
func tsProject(t *testing.T, r *testRegistry, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	if err := r.generate(t.Context(), dir); err != nil {
		t.Fatalf("generate: %v", err)
	}
	files["tsconfig.json"] = tsconfig
	files["package.json"] = `{"type": "module"}`
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// tool runs name from PATH in dir, failing the test when it is not on PATH.
func tool(t *testing.T, dir, name string, args ...string) (string, error) {
	t.Helper()
	bin, err := exec.LookPath(name)
	if err != nil {
		t.Fatalf("%s is not on PATH: %v", name, err)
	}
	cmd := exec.CommandContext(t.Context(), bin, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// runTS generates r beside files (main.ts among them), compiles them with tsc
// and runs main.js under node, returning node's combined output. It fails the
// test when either tool is missing or exits non-zero.
func runTS(t *testing.T, r *testRegistry, files map[string]string) string {
	t.Helper()
	dir := tsProject(t, r, files)
	if out, err := tool(t, dir, "tsc", "-p", "tsconfig.json"); err != nil {
		t.Fatalf("tsc: %v\n%s", err, out)
	}
	out, err := tool(t, dir, "node", filepath.Join("out", "main.js"))
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	return out
}

// tscFails generates r beside files and requires tsc to refuse them, with
// output naming want.
func tscFails(t *testing.T, r *testRegistry, files map[string]string, want string) {
	t.Helper()
	dir := tsProject(t, r, files)
	out, err := tool(t, dir, "tsc", "-p", "tsconfig.json", "--noEmit")
	if err == nil {
		t.Fatalf("tsc accepted the fixture, want a refusal naming %q", want)
	}
	if !strings.Contains(out, want) {
		t.Errorf("tsc refused the fixture without naming %q:\n%s", want, out)
	}
}
