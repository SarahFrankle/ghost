package seed

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func write(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "seed-topics.yml")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadTopicsOnly(t *testing.T) {
	s, warns, err := Load(write(t, "topics:\n  - testing-discipline\n  - commit-message-style\n"))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(warns) != 0 {
		t.Fatalf("warnings: %v", warns)
	}
	want := []string{"commit-message-style", "testing-discipline"}
	if got := s.Names(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Names()=%v want %v", got, want)
	}
	flat := s.Flatten()
	if len(flat) != 2 || flat[0].Parent != "" {
		t.Fatalf("Flatten()=%+v", flat)
	}
}

func TestLoadCategoriesWithPinnedParents(t *testing.T) {
	s, _, err := Load(write(t, "categories:\n  pr:\n    - pr-creation\n    - pr-reviewing\n"))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	want := []string{"pr-creation", "pr-reviewing"}
	if got := s.Names(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Names()=%v want %v", got, want)
	}
	for _, tp := range s.Flatten() {
		if tp.Parent != "pr" {
			t.Fatalf("topic %q parent=%q want pr", tp.Name, tp.Parent)
		}
	}
}

func TestLoadBothSections(t *testing.T) {
	s, _, err := Load(write(t, "topics:\n  - testing-discipline\ncategories:\n  pr:\n    - pr-creation\n"))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := s.Names(); !reflect.DeepEqual(got, []string{"pr-creation", "testing-discipline"}) {
		t.Fatalf("Names()=%v", got)
	}
}

func TestLoadAbsentFile(t *testing.T) {
	s, warns, err := Load(filepath.Join(t.TempDir(), "does-not-exist.yml"))
	if err != nil {
		t.Fatalf("absent file should not error, got %v", err)
	}
	if warns != nil || len(s.Names()) != 0 {
		t.Fatalf("absent file should yield empty seed, got %+v warns=%v", s, warns)
	}
}

func TestLoadMalformedYAMLErrors(t *testing.T) {
	_, _, err := Load(write(t, "topics:\n  - one\n   bad-indent: x\n"))
	if err == nil {
		t.Fatal("malformed YAML should return an error")
	}
}

func TestLoadDuplicateAndEmptyDropped(t *testing.T) {
	s, warns, err := Load(write(t, "topics:\n  - testing-discipline\n  - testing-discipline\n  - \"\"\n"))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := s.Names(); !reflect.DeepEqual(got, []string{"testing-discipline"}) {
		t.Fatalf("Names()=%v want one entry", got)
	}
	if len(warns) != 2 {
		t.Fatalf("want 2 warnings (dup + empty), got %v", warns)
	}
}
