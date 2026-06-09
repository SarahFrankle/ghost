package cluster

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
)

func TestLabelAllReportsProgressForNewObservationsOnly(t *testing.T) {
	// Progress reflects the work actually done: one tick per newly-labeled
	// observation, counted against the new-observation total (cached entries
	// are not re-labeled and do not move the counter).
	dir := t.TempDir()
	members := []ClusterMember{
		{ObservationHash: "h1", Text: "a"},
		{ObservationHash: "h2", Text: "b"},
		{ObservationHash: "h3", Text: "c"},
	}
	cache := mustLabelCache(t, dir)
	cache.Put("h1", "cached") // already labeled: not part of the new total

	var mu sync.Mutex
	var calls, maxDone int
	g := &TopicGrouper{
		Label:   func(context.Context, string) (string, error) { return "fresh", nil },
		Cache:   cache,
		Workers: 2,
		Progress: func(done, total int) {
			mu.Lock()
			defer mu.Unlock()
			calls++
			if total != 2 {
				t.Errorf("progress total = %d, want 2 (new observations)", total)
			}
			if done > maxDone {
				maxDone = done
			}
		},
	}
	out, err := g.labelAll(context.Background(), members)
	if err != nil {
		t.Fatalf("labelAll error: %v", err)
	}
	if out[0] != "cached" || out[1] != "fresh" || out[2] != "fresh" {
		t.Fatalf("labels = %v, want [cached fresh fresh]", out)
	}
	if calls != 2 {
		t.Fatalf("progress fired %d times, want 2", calls)
	}
	if maxDone != 2 {
		t.Fatalf("max done = %d, want 2", maxDone)
	}
}

func member(hash, project, text string) ClusterMember {
	return ClusterMember{ObservationHash: hash, Kind: "topic", Project: project, Text: text}
}

func TestGroupByLabelKeepsAboveThreshold(t *testing.T) {
	members := []ClusterMember{
		member("a", "p1", "t1"), member("b", "p2", "t2"), member("c", "p1", "t3"),
		member("d", "p1", "t4"), member("e", "p1", "t5"),
	}
	themed := []string{"git", "git", "git", "docs", "docs"} // git=3, docs=2

	clusters, dropped, err := groupByLabel(members, themed, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(clusters) != 1 || clusters[0].Canonical != "git" {
		t.Fatalf("want 1 cluster 'git', got %+v", clusters)
	}
	if clusters[0].EvidenceCount != 3 || clusters[0].ProjectCount != 2 {
		t.Fatalf("git counts = ev %d proj %d; want 3,2", clusters[0].EvidenceCount, clusters[0].ProjectCount)
	}
	if dropped != 2 {
		t.Fatalf("dropped = %d; want 2", dropped)
	}
}

func TestGroupByLabelFailsOnEmptyLabel(t *testing.T) {
	members := []ClusterMember{member("a", "p", "t")}
	if _, _, err := groupByLabel(members, []string{""}, 1); err == nil {
		t.Fatal("expected error for empty themed label")
	}
}

func TestGroupByLabelFailsOnLengthMismatch(t *testing.T) {
	members := []ClusterMember{member("a", "p", "t")}
	if _, _, err := groupByLabel(members, []string{"x", "y"}, 1); err == nil {
		t.Fatal("expected error for label/member length mismatch")
	}
}

func TestTopicGrouperEndToEnd(t *testing.T) {
	members := []ClusterMember{
		{ObservationHash: "a", Kind: "topic", Project: "p1", Text: "squash merge"},
		{ObservationHash: "b", Kind: "topic", Project: "p2", Text: "conventional commits"},
		{ObservationHash: "c", Kind: "topic", Project: "p1", Text: "branch naming"},
		{ObservationHash: "d", Kind: "topic", Project: "p1", Text: "lonely note"},
	}
	label := func(_ context.Context, text string) (string, error) {
		if text == "lonely note" {
			return "misc", nil
		}
		return "git " + text[:3], nil // distinct raw labels, themed together below
	}
	identify := func(_ context.Context, _ []string) ([]string, error) {
		return []string{"Git Workflow", "Miscellaneous"}, nil
	}
	mapper := func(_ context.Context, _ []string, labels []string) (map[string]string, error) {
		m := map[string]string{}
		for _, l := range labels {
			if l == "misc" {
				m[l] = "Miscellaneous"
			} else {
				m[l] = "Git Workflow"
			}
		}
		return m, nil
	}
	dir := t.TempDir()
	g := &TopicGrouper{
		Label:                   label,
		ThemeIdentify:           identify,
		ThemeMap:                mapper,
		Cache:                   mustLabelCache(t, dir),
		CacheSavePath:           filepath.Join(dir, "labels.json"),
		ThemesPath:              filepath.Join(dir, "themes.json"),
		ThemeModel:              "sonnet",
		ThemeIdentifyPromptHash: "ih",
		ThemeMapPromptHash:      "mh",
		MinClusterSize:          3,
		Workers:                 2,
	}
	clusters, err := g.Run(context.Background(), members)
	if err != nil {
		t.Fatal(err)
	}
	if len(clusters) != 1 || clusters[0].Canonical != "Git Workflow" || clusters[0].EvidenceCount != 3 {
		t.Fatalf("want 1 Git Workflow cluster of 3, got %+v", clusters)
	}
}

func mustLabelCache(t *testing.T, dir string) *LabelCache {
	t.Helper()
	c, err := LoadLabelCache(filepath.Join(dir, "labels.json"), "haiku", "ph")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestParseThemeMapping(t *testing.T) {
	raw := "here you go:\n{\"mapping\": {\"git\": \"Git Workflow\", \"docs\": \"Documentation\"}}\nthanks"
	m, err := parseThemeMapping(raw)
	if err != nil {
		t.Fatal(err)
	}
	if m["git"] != "Git Workflow" || m["docs"] != "Documentation" {
		t.Fatalf("bad mapping: %+v", m)
	}
}

func TestParseThemeMappingRejectsJunk(t *testing.T) {
	if _, err := parseThemeMapping("no json here"); err == nil {
		t.Fatal("expected error")
	}
	if _, err := parseThemeMapping(`{"mapping": {}}`); err == nil {
		t.Fatal("expected error for empty mapping")
	}
}

// fakeClient implements anthropic.Client by returning a canned reply.
type fakeClient struct{ reply string }

func (f fakeClient) Complete(_ context.Context, _, _, _ string) (string, error) {
	return f.reply, nil
}

func TestNewThemeIdentifyFuncParsesThemeLines(t *testing.T) {
	f := NewThemeIdentifyFunc(fakeClient{reply: "THEME: Git Workflow\n- THEME: Documentation\nnoise line\n"}, "sonnet")
	themes, err := f(context.Background(), []string{"a"})
	if err != nil {
		t.Fatal(err)
	}
	if len(themes) != 3 || themes[0] != "Git Workflow" || themes[1] != "Documentation" || themes[2] != "noise line" {
		t.Fatalf("got %+v", themes)
	}
}

func TestNewThemeMapFuncResolvesKeysToInputLabels(t *testing.T) {
	// Model re-capitalizes the key; resolveMapping must rekey it to the exact
	// input label so grouping matches verbatim.
	f := NewThemeMapFunc(fakeClient{reply: `{"mapping":{"Git Squash":"Git Workflow"}}`}, "sonnet")
	m, err := f(context.Background(), []string{"Git Workflow"}, []string{"git squash"})
	if err != nil {
		t.Fatal(err)
	}
	if m["git squash"] != "Git Workflow" || len(m) != 1 {
		t.Fatalf("got %+v", m)
	}
}

func TestNewThemeMapFuncEmptyOnUnparseable(t *testing.T) {
	// A junk reply must not error — the caller's retry/self-map loop recovers.
	f := NewThemeMapFunc(fakeClient{reply: "no json here"}, "sonnet")
	m, err := f(context.Background(), []string{"T"}, []string{"a"})
	if err != nil || len(m) != 0 {
		t.Fatalf("want empty map, no error; got %+v err %v", m, err)
	}
}

func TestThemeMappingRetriesUnmappedThenCovers(t *testing.T) {
	identify := func(_ context.Context, _ []string) ([]string, error) { return []string{"T"}, nil }
	call := 0
	mapper := func(_ context.Context, _ []string, batch []string) (map[string]string, error) {
		call++
		m := map[string]string{}
		if call == 1 {
			m["a"] = "T" // map only one on the first round; b,c must be retried
		} else {
			for _, l := range batch {
				m[l] = "T"
			}
		}
		return m, nil
	}
	g := &TopicGrouper{ThemeIdentify: identify, ThemeMap: mapper, Workers: 2}
	got, err := g.themeMapping(context.Background(), []string{"a", "b", "c"})
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range []string{"a", "b", "c"} {
		if got[l] != "T" {
			t.Fatalf("label %q not mapped to T: %+v", l, got)
		}
	}
}

func TestThemeMappingSelfMapsStragglers(t *testing.T) {
	identify := func(_ context.Context, _ []string) ([]string, error) { return []string{"T"}, nil }
	mapper := func(_ context.Context, _ []string, batch []string) (map[string]string, error) {
		m := map[string]string{}
		for _, l := range batch {
			if l == "a" {
				m[l] = "T" // "b" never maps → must be self-mapped, not crash
			}
		}
		return m, nil
	}
	g := &TopicGrouper{ThemeIdentify: identify, ThemeMap: mapper, Workers: 1}
	got, err := g.themeMapping(context.Background(), []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if got["a"] != "T" || got["b"] != "b" {
		t.Fatalf("want a->T and b self-mapped to b; got %+v", got)
	}
	if err := validateMapping([]string{"a", "b"}, got); err != nil {
		t.Fatalf("self-map must satisfy the coverage contract: %v", err)
	}
}

func TestThemeMappingErrorsOnNoThemes(t *testing.T) {
	identify := func(_ context.Context, _ []string) ([]string, error) { return nil, nil }
	mapper := func(_ context.Context, _ []string, _ []string) (map[string]string, error) { return nil, nil }
	g := &TopicGrouper{ThemeIdentify: identify, ThemeMap: mapper, Workers: 1}
	if _, err := g.themeMapping(context.Background(), []string{"a"}); err == nil {
		t.Fatal("expected error when identify returns no themes")
	}
}
