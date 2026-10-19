//go:build integration

package wiregen_test

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/cplieger/wiregen/v4"
	"github.com/cplieger/wiregen/v4/testdata/basic"
)

// fastCheckStub types the fast-check import loosely enough to compile the
// arbitraries module, which is all this test needs of it.
const fastCheckStub = `interface Arb<T = any> {
  filter(keep: (v: T) => boolean): Arb<T>;
  map<U>(f: (v: T) => U): Arb<U>;
}
declare namespace fc {
  type Arbitrary<T> = Arb<T>;
}
declare const fc: {
  letrec<T>(build: (tie: (key: string) => Arb) => { [K in keyof T]: Arb<T[K]> }): { [K in keyof T]: Arb<T[K]> };
  [name: string]: <T = any>(...args: any[]) => Arb<T>;
};
export default fc;
`

// libDecl matches a top-level declaration in a TypeScript lib file.
var libDecl = regexp.MustCompile(`(?m)^(?:declare )?(?:var|let|const|function|interface|type|class|abstract class|namespace|enum) ([A-Za-z_$][\w$]*)`)

// conflict matches tsc's report that a shadow collides with a module's own
// top-level name, which a bare reference then means.
var conflict = regexp.MustCompile(`(?m)^([\w.-]+\.ts)\(\d+,\d+\): error TS(?:2300|2451|2440|2395|2397): .*?'([\w$]+)'`)

// Generated code names every global through globalThis, so no registered or
// renamed type can shadow one. Every name the TypeScript libs declare is
// shadowed at the top of each generated module, and the modules must still
// compile.
func TestGenerated_namesGlobalsThroughGlobalThis(t *testing.T) {
	r := fullReg()
	r.Types = append(r.Types, wiregen.TypeRef[basic.HasMap](), wiregen.TypeRef[basic.HasRaw]())
	r.opts = append(r.opts, wiregen.WithArbitrariesFile("arbitraries.gen.ts"))
	dir := tsProject(t, r, map[string]string{"api-client.ts": transportTS(t)})
	stub := filepath.Join(dir, "node_modules", "fast-check")
	if err := os.MkdirAll(stub, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"package.json": `{"name": "fast-check", "type": "module", "types": "index.d.ts"}`,
		"index.d.ts":   fastCheckStub,
	} {
		if err := os.WriteFile(filepath.Join(stub, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if out, err := tool(t, dir, "tsc", "-p", "tsconfig.json", "--noEmit"); err != nil {
		t.Fatalf("tsc refuses the generated modules before any shadow: %v\n%s", err, out)
	}
	globals := libGlobals(t, dir)
	generated, err := filepath.Glob(filepath.Join(dir, "*.gen.ts"))
	if err != nil || len(generated) < 6 {
		t.Fatalf("generated modules %v: %v", generated, err)
	}
	bodies := map[string]string{}
	own := map[string]map[string]bool{}
	for _, path := range generated {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		bodies[filepath.Base(path)] = string(b)
		own[filepath.Base(path)] = map[string]bool{}
	}
	for range 8 {
		for name, body := range bodies {
			var prelude strings.Builder
			prelude.WriteString("declare const wiregenShadow: unique symbol;\n")
			for _, g := range globals {
				if !own[name][g] {
					prelude.WriteString("declare const " + g + ": typeof wiregenShadow; type " + g + " = typeof wiregenShadow;\n")
				}
			}
			if err := os.WriteFile(filepath.Join(dir, name), []byte(prelude.String()+body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		out, err := tool(t, dir, "tsc", "-p", "tsconfig.json", "--noEmit")
		if err == nil {
			return
		}
		found := false
		for _, m := range conflict.FindAllStringSubmatch(out, -1) {
			if own[m[1]] != nil && !own[m[1]][m[2]] && slices.Contains(globals, m[2]) && declaresTopLevel(bodies[m[1]], m[2]) {
				own[m[1]][m[2]], found = true, true
			}
		}
		if !found {
			t.Fatalf("a generated module names a global without globalThis:\n%s", out)
		}
	}
	t.Fatal("the shadows kept colliding with the modules' own names")
}

// declaresTopLevel reports whether body binds name at its top level, by an
// import or a declaration, so a bare use means its own binding.
func declaresTopLevel(body, name string) bool {
	re := regexp.MustCompile(`(?m)^(?:export )?(?:declare )?(?:const|let|var|function|interface|type|class) ` + regexp.QuoteMeta(name) + `\b|^import [^\n]*\b` + regexp.QuoteMeta(name) + `\b`)
	return re.MatchString(body)
}

// libGlobals lists the names the TypeScript lib files of dir's project
// declare at the top level, except those no module may redeclare.
func libGlobals(t *testing.T, dir string) []string {
	t.Helper()
	out, err := tool(t, dir, "tsc", "-p", "tsconfig.json", "--listFilesOnly")
	if err != nil {
		t.Fatalf("tsc --listFilesOnly: %v\n%s", err, out)
	}
	seen := map[string]bool{"eval": true, "arguments": true, "undefined": true}
	var names []string
	for line := range strings.Lines(out) {
		path := strings.TrimSpace(line)
		if !strings.HasPrefix(filepath.Base(path), "lib.") {
			continue
		}
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range libDecl.FindAllStringSubmatch(string(b), -1) {
			if !seen[m[1]] {
				seen[m[1]] = true
				names = append(names, m[1])
			}
		}
	}
	if len(names) < 500 {
		t.Fatalf("read only %d lib globals from:\n%s", len(names), out)
	}
	slices.Sort(names)
	return names
}

// A registered type may take the name of a global the generated code uses:
// the client compiles and runs with its types named AbortSignal, Promise,
// Map and TypeError.
func TestGenerated_typeNamedAfterAGlobal(t *testing.T) {
	r := clientRegistry()
	r.TSNameOverride = map[string]string{"Item": "AbortSignal", "Job": "Promise", "Position": "Map", "Tick": "TypeError"}
	out := runTS(t, r, map[string]string{"api-client.ts": transportTS(t), "main.ts": clientMain})
	if !strings.Contains(out, `getItem {"ok":true,"data":{"id":"a/b","name":"n"}}`) {
		t.Errorf("node output:\n%s", out)
	}
}
