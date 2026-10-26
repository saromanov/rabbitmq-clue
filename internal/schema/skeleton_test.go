package schema

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSkeletonDefaults(t *testing.T) {
	c := loadTestCompiled(t)
	skel, _, err := c.Skeleton("clue.test.v1.Item")
	if err != nil {
		t.Fatalf("Skeleton: %v", err)
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(skel), &obj); err != nil {
		t.Fatalf("skeleton is not JSON: %v", err)
	}

	checks := map[string]any{
		"int32Field":             float64(0),
		"int64Field":             float64(0),
		"uint64Field":            float64(0),
		"floatField":             float64(0),
		"doubleField":            float64(0),
		"boolField":              false,
		"stringField":            "",
		"bytesField":             "",
		"status":                 "STATUS_UNKNOWN",
		"tags":                   []any{},
		"addresses":              []any{},
		"counters":               map[string]any{},
		"addressesById":          map[string]any{},
		"createdAt":              "0001-01-01T00:00:00Z",
		"ttl":                    "0s",
		"payload":                map[string]any{"@type": ""},
		"address.city":           "",
		"note":                   "",
		"score":                  float64(0),
		"nested.item.int32Field": float64(0),
	}
	for path, want := range checks {
		got, err := lookupPath(obj, path)
		if err != nil {
			t.Errorf("path %q: %v", path, err)
			continue
		}
		gotJSON, _ := json.Marshal(got)
		wantJSON, _ := json.Marshal(want)
		if string(gotJSON) != string(wantJSON) {
			t.Errorf("path %q = %s, want %s", path, gotJSON, wantJSON)
		}
	}

	// oneof fields keep their own skeleton values
	if obj["note"] != "" || obj["score"] != float64(0) {
		t.Errorf("oneof skeleton broken: %v %v", obj["note"], obj["score"])
	}
}

func TestSkeletonFieldInfo(t *testing.T) {
	c := loadTestCompiled(t)
	_, fields, err := c.Skeleton("clue.test.v1.Item")
	if err != nil {
		t.Fatalf("Skeleton: %v", err)
	}
	byPath := map[string]FieldInfo{}
	for _, f := range fields {
		byPath[f.Path] = f
	}

	f := byPath["status"]
	if f.Kind != "enum" || f.TypeName != "clue.test.v1.Status" {
		t.Errorf("status field = %+v", f)
	}
	if len(f.EnumValues) != 3 || f.EnumValues[0] != "STATUS_UNKNOWN" {
		t.Errorf("status enum values = %v", f.EnumValues)
	}
	f = byPath["tags"]
	if f.Kind != "repeated" || !f.Repeated || f.TypeName != "string" {
		t.Errorf("tags field = %+v", f)
	}
	f = byPath["counters"]
	if f.Kind != "map" || !f.Map {
		t.Errorf("counters field = %+v", f)
	}
	f = byPath["note"]
	if f.Oneof != "extra" {
		t.Errorf("note oneof = %q", f.Oneof)
	}
	f = byPath["address.city"]
	if f.Kind != "scalar" || f.ProtoName != "city" || f.TypeName != "string" {
		t.Errorf("address.city field = %+v", f)
	}
	f = byPath["payload"]
	if f.TypeName != "google.protobuf.Any" {
		t.Errorf("payload typeName = %q", f.TypeName)
	}
}

func TestSkeletonUnknownType(t *testing.T) {
	c := loadTestCompiled(t)
	if _, _, err := c.Skeleton("no.such.Msg"); err == nil {
		t.Fatal("expected error")
	}
}

func lookupPath(obj map[string]any, path string) (any, error) {
	cur := any(obj)
	for _, part := range strings.Split(path, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, errNotFound{part}
		}
		cur, ok = m[part]
		if !ok {
			return nil, errNotFound{part}
		}
	}
	return cur, nil
}

type errNotFound struct{ part string }

func (e errNotFound) Error() string { return "path part not found: " + e.part }
