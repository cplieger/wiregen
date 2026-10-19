//go:build integration

package wiregen_test

import (
	"strings"
	"testing"

	"github.com/cplieger/wiregen/v4"
	"github.com/cplieger/wiregen/v4/testdata/enums"
)

// A required TextEnum field decodes each named member and refuses the zero
// value however a client spells it.
func TestRequiredEnumWithUnnamedZero_decoder(t *testing.T) {
	r := enumsReg([]wiregen.EnumRef{wiregen.TextEnum[enums.Level]()}, wiregen.TypeRef[enums.Job]())
	out := runTS(t, r, map[string]string{"main.ts": `import { decodeJob } from "./decoders.gen.js";
for (const body of ['{"level":"low"}', '{"level":"high"}', '{"level":0}', '{"level":""}', '{}']) {
  try {
    decodeJob(JSON.parse(body));
    console.log(body + " ok");
  } catch {
    console.log(body + " refused");
  }
}
`})
	want := strings.Join([]string{
		`{"level":"low"} ok`, `{"level":"high"} ok`,
		`{"level":0} refused`, `{"level":""} refused`, `{} refused`, "",
	}, "\n")
	if out != want {
		t.Errorf("node output:\n%s\nwant:\n%s", out, want)
	}
}
