package schema

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testProto = "syntax = \"proto3\";\npackage clue.store.v1;\nmessage Ping { string text = 1; }\n"

func testFiles() map[string]string {
	return map[string]string{"test.proto": testProto}
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	return NewStore(filepath.Join(t.TempDir(), "schemas"))
}

func TestCreateAndGet(t *testing.T) {
	st := newTestStore(t)
	meta, err := st.Create(SchemaMeta{Name: "Violations Proto", MessageType: "clue.store.v1.Ping", Files: []string{"test.proto"}}, testFiles())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if meta.ID != "violations-proto" {
		t.Errorf("ID = %q", meta.ID)
	}
	if len(meta.Files) != 1 || meta.Files[0] != "test.proto" {
		t.Errorf("Files = %v", meta.Files)
	}

	got, err := st.Get(meta.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "Violations Proto" || got.MessageType != "clue.store.v1.Ping" {
		t.Errorf("meta = %+v", got)
	}

	files, err := st.Files(meta.ID)
	if err != nil {
		t.Fatalf("Files: %v", err)
	}
	if files["test.proto"] != testProto {
		t.Errorf("stored file = %q", files["test.proto"])
	}
}

func TestCreateSlugCollision(t *testing.T) {
	st := newTestStore(t)
	first, err := st.Create(SchemaMeta{Name: "violations", MessageType: "a.P"}, testFiles())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	second, err := st.Create(SchemaMeta{Name: "violations", MessageType: "b.P"}, testFiles())
	if err != nil {
		t.Fatalf("Create second: %v", err)
	}
	if first.ID != "violations" || second.ID != "violations-2" {
		t.Errorf("ids = %q %q", first.ID, second.ID)
	}
}

func TestList(t *testing.T) {
	st := newTestStore(t)
	if _, err := st.Create(SchemaMeta{Name: "b", MessageType: "a.P"}, testFiles()); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Create(SchemaMeta{Name: "a", MessageType: "a.P"}, testFiles()); err != nil {
		t.Fatal(err)
	}
	metas, err := st.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(metas) != 2 || metas[0].ID != "a" || metas[1].ID != "b" {
		t.Fatalf("metas = %+v", metas)
	}
}

func TestListEmptyDir(t *testing.T) {
	st := newTestStore(t)
	metas, err := st.List()
	if err != nil || metas != nil {
		t.Fatalf("List on missing dir: %v %v", metas, err)
	}
}

func TestUpdateKeepsFiles(t *testing.T) {
	st := newTestStore(t)
	meta, err := st.Create(SchemaMeta{Name: "x", MessageType: "a.P", Bindings: []Binding{{Kind: BindingExchange, Name: "orders"}}}, testFiles())
	if err != nil {
		t.Fatal(err)
	}
	updated, err := st.Update(meta.ID, SchemaMeta{Bindings: []Binding{{Kind: BindingQueue, Name: "work"}}}, nil)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Name != "x" || updated.MessageType != "a.P" {
		t.Errorf("updated meta = %+v", updated)
	}
	if len(updated.Bindings) != 1 || updated.Bindings[0] != (Binding{Kind: BindingQueue, Name: "work"}) {
		t.Errorf("bindings = %v", updated.Bindings)
	}
	files, err := st.Files(meta.ID)
	if err != nil || files["test.proto"] != testProto {
		t.Fatalf("files after update: %v %v", files, err)
	}
}

func TestUpdateReplacesFiles(t *testing.T) {
	st := newTestStore(t)
	meta, err := st.Create(SchemaMeta{Name: "x", MessageType: "a.P"}, testFiles())
	if err != nil {
		t.Fatal(err)
	}
	updated, err := st.Update(meta.ID, SchemaMeta{Name: "x2", MessageType: "b.P"}, map[string]string{"other.proto": "syntax = \"proto3\";\npackage b;\nmessage P2{}"})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Name != "x2" {
		t.Errorf("name = %q", updated.Name)
	}
	files, err := st.Files(meta.ID)
	if err != nil {
		t.Fatalf("Files: %v", err)
	}
	if _, ok := files["test.proto"]; ok {
		t.Error("old file still present")
	}
	if _, ok := files["other.proto"]; !ok {
		t.Error("new file missing")
	}
	if updated.ID != meta.ID {
		t.Errorf("ID changed: %q -> %q", meta.ID, updated.ID)
	}
}

func TestDelete(t *testing.T) {
	st := newTestStore(t)
	meta, err := st.Create(SchemaMeta{Name: "x", MessageType: "a.P"}, testFiles())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Delete(meta.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := st.Get(meta.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if err := st.Delete(meta.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("double delete: %v", err)
	}
}

func TestGetUnknown(t *testing.T) {
	st := newTestStore(t)
	if _, err := st.Get("nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestCreateValidation(t *testing.T) {
	cases := []struct {
		name  string
		meta  SchemaMeta
		files map[string]string
	}{
		{"no name", SchemaMeta{MessageType: "a.P"}, testFiles()},
		{"no message type", SchemaMeta{Name: "x"}, testFiles()},
		{"no files", SchemaMeta{Name: "x", MessageType: "a.P"}, nil},
		{"bad binding kind", SchemaMeta{Name: "x", MessageType: "a.P", Bindings: []Binding{{Kind: "topic", Name: "e"}}}, testFiles()},
	}
	st := newTestStore(t)
	for _, tc := range cases {
		if _, err := st.Create(tc.meta, tc.files); err == nil {
			t.Errorf("%s: expected error", tc.name)
		}
	}
}

func TestCreateBadFileName(t *testing.T) {
	st := newTestStore(t)
	if _, err := st.Create(SchemaMeta{Name: "x", MessageType: "a.P"}, map[string]string{"../evil.proto": testProto}); err == nil {
		t.Fatal("expected error for traversal name")
	}
	if _, err := st.Create(SchemaMeta{Name: "y", MessageType: "a.P"}, map[string]string{"/abs.proto": testProto}); err == nil {
		t.Fatal("expected error for absolute name")
	}
}

func TestCreateTooManyFiles(t *testing.T) {
	st := newTestStore(t)
	files := map[string]string{}
	for i := 0; i <= MaxFiles; i++ {
		files[fmt.Sprintf("f%d.proto", i)] = testProto
	}
	if _, err := st.Create(SchemaMeta{Name: "x", MessageType: "a.P"}, files); err == nil {
		t.Fatal("expected error")
	}
}

func TestSubdirFileNames(t *testing.T) {
	st := newTestStore(t)
	meta, err := st.Create(SchemaMeta{Name: "sub", MessageType: "a.P"}, map[string]string{"proto/test.proto": testProto})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	files, err := st.Files(meta.ID)
	if err != nil {
		t.Fatalf("Files: %v", err)
	}
	if _, ok := files["proto/test.proto"]; !ok {
		t.Errorf("files = %v", files)
	}
}

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Violations Proto": "violations-proto",
		"  x  ":            "x",
		"///":              "schema",
		"many   spaces!!":  "many-spaces",
		"Udfa_1-ok":        "udfa_1-ok",
	}
	for in, want := range cases {
		if got := slugify(in); got != want {
			t.Errorf("slugify(%q) = %q, want %q", in, got, want)
		}
	}
	long := strings.Repeat("a", 100)
	if got := slugify(long); len(got) != 64 {
		t.Errorf("slugify long len = %d", len(got))
	}
}

func TestSafeID(t *testing.T) {
	if safeID("ok") != true || safeID("../x") != false || safeID("") != false {
		t.Error("safeID broken")
	}
	if safeID("a/b") || safeID("a\\b") {
		t.Error("safeID allows separators")
	}
}

func TestTmpDirsNotListed(t *testing.T) {
	st := newTestStore(t)
	if _, err := st.Create(SchemaMeta{Name: "x", MessageType: "a.P"}, testFiles()); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(st.Dir(), ".tmp-orphan"), 0o755); err != nil {
		t.Fatal(err)
	}
	metas, err := st.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(metas) != 1 {
		t.Fatalf("metas = %+v", metas)
	}
}
