package wiregen_test

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/cplieger/wiregen/v4"
)

// nodeArgs run a TS module by stripping its types, which node does natively
// from 22.6 (https://nodejs.org/api/typescript.html#type-stripping).
var nodeArgs = []string{"--experimental-strip-types", "--no-warnings"}

// nodeBin returns node from PATH, failing the test when it is absent.
func nodeBin(tb testing.TB) string {
	tb.Helper()
	bin, err := exec.LookPath("node")
	if err != nil {
		tb.Fatalf("node is not on PATH: %v", err)
	}
	return bin
}

// nodeModules generates r into a fresh directory as modules node can import
// by their .ts names, beside files, and returns the directory.
func nodeModules(tb testing.TB, ctx context.Context, r *testRegistry, files map[string]string) string {
	tb.Helper()
	dir := tb.TempDir()
	opts := append(append([]wiregen.Option{}, r.opts...),
		wiregen.WithTSDir(dir),
		wiregen.WithValidatorsImport("./validators.gen.ts"),
		wiregen.WithTypesImportPath("./types.gen.ts"))
	if err := r.Generate(ctx, opts...); err != nil {
		tb.Fatalf("generate: %v", err)
	}
	files["package.json"] = `{"type": "module"}`
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			tb.Fatal(err)
		}
	}
	return dir
}

// decodeVerdicts runs r's decoder named decoder over each JSON body under
// node and returns "ok" or the decoder's error message for each.
func decodeVerdicts(t *testing.T, r *testRegistry, decoder string, bodies []string) []string {
	t.Helper()
	return runDecoder(t, r, decoder, bodies, `"ok"`)
}

// decodeResults is decodeVerdicts answering each decoded value as its
// JSON.stringify, own properties only, so a key lost to the prototype shows.
func decodeResults(t *testing.T, r *testRegistry, decoder string, bodies []string) []string {
	t.Helper()
	return runDecoder(t, r, decoder, bodies, "JSON.stringify(d)")
}

// runDecoder runs decoder over each body and prints answer, an expression
// over the decoded value d, or the decoder's error message.
func runDecoder(t *testing.T, r *testRegistry, decoder string, bodies []string, answer string) []string {
	t.Helper()
	list, err := json.Marshal(bodies)
	if err != nil {
		t.Fatal(err)
	}
	main := "import { " + decoder + " } from \"./decoders.gen.ts\";\n" +
		"const bodies: string[] = " + string(list) + ";\n" +
		"for (const b of bodies) {\n" +
		"  try {\n    const d = " + decoder + "(JSON.parse(b));\n    console.log(" + answer + ");\n" +
		"  } catch (e) {\n    console.log(e instanceof Error ? e.message : String(e));\n  }\n}\n"
	dir := nodeModules(t, t.Context(), r, map[string]string{"main.ts": main})
	cmd := exec.CommandContext(t.Context(), nodeBin(t), append(nodeArgs, "main.ts")...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	lines := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	if len(lines) != len(bodies) {
		t.Fatalf("node printed %d verdicts for %d bodies:\n%s", len(lines), len(bodies), out)
	}
	return lines
}

// nodeREPL is a node process answering one JSON request per line through a
// handler module, for a fuzz target that cannot afford a process per input.
type nodeREPL struct {
	in  io.WriteCloser
	out *bufio.Scanner
	mu  sync.Mutex
}

// startREPL runs handler, the body of `(req: any) => string`, in a module
// beside r's generated files, with imports prepended.
func startREPL(tb testing.TB, r *testRegistry, imports, handler string) *nodeREPL {
	tb.Helper()
	main := imports + "\nimport { createInterface } from \"node:readline\";\n" +
		"const handle = (req: any): string => {\n" + handler + "\n};\n" +
		"for await (const line of createInterface({ input: process.stdin })) {\n" +
		"  process.stdout.write(handle(JSON.parse(line)) + \"\\n\");\n}\n"
	// The process outlives the fuzz target's setup call, so no test context
	// bounds it; Cleanup ends it.
	dir := nodeModules(tb, context.Background(), r, map[string]string{"main.ts": main})
	cmd := exec.Command(nodeBin(tb), append(nodeArgs, "main.ts")...)
	cmd.Dir = dir
	cmd.Stderr = os.Stderr
	in, err := cmd.StdinPipe()
	if err != nil {
		tb.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		tb.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() {
		_ = in.Close()
		_ = cmd.Wait() // the REPL exits at end of input
	})
	return &nodeREPL{in: in, out: bufio.NewScanner(out)}
}

// ask sends req and returns the handler's answer.
func (n *nodeREPL) ask(tb testing.TB, req any) string {
	tb.Helper()
	b, err := json.Marshal(req)
	if err != nil {
		tb.Fatal(err)
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if _, err := n.in.Write(append(b, '\n')); err != nil {
		tb.Fatalf("write to node: %v", err)
	}
	if !n.out.Scan() {
		tb.Fatalf("node exited: %v", n.out.Err())
	}
	return n.out.Text()
}
