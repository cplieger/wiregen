package wiregen

// These derive every null and absence rule from what encoding/json writes and
// reads for a field; the TS types, decoders, arbitraries and OpenAPI schemas
// all read them.

// optional reports whether the key may be absent: encoding/json can omit it,
// or a request may leave a pointer nil. The TS member is `key?:`, and the
// decoder reads an absent key or null as absent.
func (f *fieldInfo) optional() bool { return f.Ptrs > 0 || f.absentOut() }

// absentOut reports whether encoding/json can leave the key out.
func (f *fieldInfo) absentOut() bool { return f.Omittable || f.ViaEmbedPtr }

// nullOut reports whether encoding/json can write null for the key: a nil
// pointer or collection no omit option drops, or anything nil under a
// non-nil outer pointer, which the option does not look through.
func (f *fieldInfo) nullOut() bool {
	switch {
	case f.Kind == kindRaw:
		return false
	case f.Ptrs > 1:
		return true
	case f.Ptrs == 1:
		return !f.Omittable || f.nilValue()
	}
	return f.nilValue() && !f.Omittable
}

// nullIn reports whether a request may send null, which encoding/json decodes
// to a nil pointer or collection.
func (f *fieldInfo) nullIn() bool { return f.Kind != kindRaw && (f.Ptrs > 0 || f.nilValue()) }

// nilValue reports whether the value under the pointers can be nil.
func (f *fieldInfo) nilValue() bool {
	return f.Kind == kindSlice || f.Kind == kindMap || f.Kind == kindBytes
}

// nullElem reports whether a collection element decodes null as null: it has
// a pointer level. A nil collection element instead reads as empty.
func (f *fieldInfo) nullElem() bool { return f.Ptrs > 0 && f.Kind != kindRaw }

// elemTS is f's TS type as a collection element.
func (f *fieldInfo) elemTS() string {
	if f.nullElem() {
		return f.TSType + " | null"
	}
	return f.TSType
}

// refusesNull reports whether f's Count bound refuses null: null would decode
// to an empty collection below the bound, which a pointer level instead
// takes as nil.
func (f *fieldInfo) refusesNull() bool { return f.Ptrs == 0 && f.Bounds.refusesNull() }
