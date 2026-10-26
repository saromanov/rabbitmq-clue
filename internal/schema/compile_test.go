package schema

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

func loadTestCompiled(t *testing.T) *Compiled {
	t.Helper()
	src, err := os.ReadFile("testdata/test.proto")
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}
	c, err := Compile(map[string]string{"test.proto": string(src)})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	return c
}

func TestCompileListsAllMessages(t *testing.T) {
	c := loadTestCompiled(t)
	want := []string{
		"clue.test.v1.Address",
		"clue.test.v1.AnyEnvelope",
		"clue.test.v1.Item",
		"clue.test.v1.Nested",
	}
	got := c.Types()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("Types() = %v, want %v", got, want)
	}
	for _, name := range want {
		if !c.HasType(name) {
			t.Errorf("HasType(%q) = false", name)
		}
	}
	if c.HasType("clue.test.v1.Missing") {
		t.Error("HasType for missing message = true")
	}
}

func TestCompileErrors(t *testing.T) {
	t.Run("no files", func(t *testing.T) {
		if _, err := Compile(map[string]string{}); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("too many files", func(t *testing.T) {
		files := map[string]string{}
		for i := 0; i <= MaxFiles; i++ {
			files[fmt.Sprintf("f%d.proto", i)] = "syntax = \"proto3\";"
		}
		if _, err := Compile(files); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("file too large", func(t *testing.T) {
		big := strings.Repeat("//", MaxFileSize)
		if _, err := Compile(map[string]string{"a.proto": big}); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("bad name", func(t *testing.T) {
		if _, err := Compile(map[string]string{"../evil.proto": "syntax = \"proto3\";"}); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("syntax error", func(t *testing.T) {
		_, err := Compile(map[string]string{"bad.proto": "this is not proto"})
		if err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("unknown import", func(t *testing.T) {
		src := "syntax = \"proto3\";\npackage p;\nimport \"nope.proto\";\n"
		if _, err := Compile(map[string]string{"a.proto": src}); err == nil {
			t.Fatal("expected error")
		}
	})
}
