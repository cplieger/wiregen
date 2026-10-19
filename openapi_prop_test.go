package wiregen_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/cplieger/wiregen/v4/testdata/openapi"
)

// FuzzOpenAPI_encodingsValidate holds the emitted schemas to encoding/json:
// whatever it writes for a valid Detail or Create, nil pointers, nil and
// empty collections and every enum constant included, validates against
// that type's component.
func FuzzOpenAPI_encodingsValidate(f *testing.F) {
	f.Add(false, uint8(255), "", int64(0), false, uint16(0), uint8(0))
	f.Add(true, uint8(0), "é", int64(-1), true, uint16(499), uint8(3))
	f.Add(true, uint8(3), strings.Repeat("é", 255), int64(1<<62), false, uint16(7), uint8(1))
	f.Add(false, uint8(1), "a\"b\u2028", int64(42), true, uint16(1000), uint8(255))
	doc := mustGen(f, fullReg().openapiFile)
	detailSchema := compiled(f, doc, "#/components/schemas/Detail")
	createSchema := compiled(f, doc, "#/components/schemas/CreateInput")
	f.Fuzz(func(t *testing.T, hasItem bool, items uint8, name string, id int64, high bool, limit uint16, files uint8) {
		if !utf8.ValidString(name) || len(name) > 510 || strings.Contains(name, "/") {
			return // outside Name's bounds, which the binder refuses
		}
		level := openapi.LevelLow
		if high {
			level = openapi.LevelHigh
		}
		item := openapi.Item{Name: openapi.Name(name), ID: id}
		d := openapi.Detail{Level: level, At: time.Unix(id%1e10, 0).UTC(), Count: uint32(limit), Enabled: high}
		if hasItem {
			d.Item = &item
			d.Note = &name
			d.Labels = map[string]string{name: name}
			d.Blob = []byte(name)
			d.Raw = json.RawMessage(`[1]`)
			d.Shared.Hint = &name
		}
		if items != 255 {
			d.Items = make([]openapi.Item, items%4)
		}
		if err := detailSchema.Validate(instance(t, mustJSON(t, d))); err != nil {
			t.Errorf("Detail %s: %v", mustJSON(t, d), err)
		}
		c := openapi.Create{Level: level, Limit: openapi.Limit(1 + int(limit)%500)}
		for range 1 + int(files)%3 {
			c.Files = append(c.Files, openapi.Name(name))
		}
		if hasItem {
			c.Item = &item
			c.Tags = []string{name}
			c.Shared.Hint = &name
		}
		if files == 255 {
			c.Labels = map[string]string{}
		}
		if err := createSchema.Validate(instance(t, mustJSON(t, c))); err != nil {
			t.Errorf("Create %s: %v", mustJSON(t, c), err)
		}
	})
}
