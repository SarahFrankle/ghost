package canonicalize

import (
	"reflect"
	"sort"
	"testing"
)

func TestProposeGroupsLemmas(t *testing.T) {
	got := Propose([]string{
		"runbook-refactor",
		"runbook-refactoring",
		"testing",
		"tests",
		"git",
		"pull-requests",
	})
	for _, g := range got {
		sort.Strings(g)
	}
	want := [][]string{
		{"runbook-refactor", "runbook-refactoring"},
		{"testing", "tests"},
	}
	if !equalGroups(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestProposeGroupsByPathTail(t *testing.T) {
	got := Propose([]string{
		"api-design",
		"engineering/api-design",
		"implementation",
	})
	if len(got) != 1 || !reflect.DeepEqual(got[0], []string{"api-design", "engineering/api-design"}) {
		t.Fatalf("expected path-tail grouping, got %v", got)
	}
}

func TestProposeGroupsBySharedFirstToken(t *testing.T) {
	got := Propose([]string{
		"git",
		"git-workflow",
		"git-config",
		"decision-analysis",
		"decision-docs",
		"unrelated",
	})
	want := [][]string{
		{"decision-analysis", "decision-docs"},
		{"git", "git-config", "git-workflow"},
	}
	if !equalGroups(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestProposeIgnoresSingletons(t *testing.T) {
	got := Propose([]string{"alpha", "beta", "gamma"})
	if len(got) != 0 {
		t.Fatalf("expected no groups, got %v", got)
	}
}

func TestLemmaStripsCommonSuffixes(t *testing.T) {
	cases := map[string]string{
		"refactoring":          "refactor",
		"runbook-refactoring":  "runbook-refactor",
		"tests":                "test",
		"testing":              "test",
		"alerting":             "alert",
		// lemma operates on the path tail so nested slugs collide with
		// their flat equivalents — that's the whole point.
		"engineering/api-design": "api-design",
	}
	for in, want := range cases {
		if got := lemma(in); got != want {
			t.Errorf("lemma(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAliasesResolveAndCollapse(t *testing.T) {
	a := collapse(Aliases{
		"runbook-refactoring": "runbook-refactor",
		"tests":               "testing",
		// chain: a -> b -> c collapses to a -> c
		"a": "b",
		"b": "c",
	})
	if got := a.Resolve("runbook-refactoring"); got != "runbook-refactor" {
		t.Errorf("Resolve variant: got %q", got)
	}
	if got := a.Resolve("runbook-refactor"); got != "runbook-refactor" {
		t.Errorf("Resolve canonical (no entry): got %q", got)
	}
	if got := a.Resolve("a"); got != "c" {
		t.Errorf("Resolve chain: got %q", got)
	}
}

func equalGroups(a, b [][]string) bool {
	if len(a) != len(b) {
		return false
	}
	sort.Slice(a, func(i, j int) bool { return a[i][0] < a[j][0] })
	sort.Slice(b, func(i, j int) bool { return b[i][0] < b[j][0] })
	for i := range a {
		if !reflect.DeepEqual(a[i], b[i]) {
			return false
		}
	}
	return true
}
