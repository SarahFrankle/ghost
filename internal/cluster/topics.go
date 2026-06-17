package cluster

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"sort"
	"strings"
	"sync"

	"github.com/SarahFrankle/ghost/internal/anthropic"
	"github.com/SarahFrankle/ghost/prompts"
)

// groupByLabel groups members by their themed label, keeping only groups with
// at least minClusterSize members. It returns one Cluster per surviving label
// (Canonical = the label) with Kind carried from the input members (all members
// in one label-partition share a kind) rather than hard-coded. The number of
// members dropped as below-threshold noise is also returned.
//
// themedLabelOf[i] is the themed label for members[i]; an empty label is an
// error — every member must resolve to a label (the grouping half of the
// ID-validation contract). Output is deterministic: labels sorted ascending,
// members within a group in ObservationHash order.
func groupByLabel(members []ClusterMember, themedLabelOf []string, minClusterSize int) (clusters []Cluster, dropped int, err error) {
	if len(themedLabelOf) != len(members) {
		return nil, 0, fmt.Errorf("group: %d labels for %d members", len(themedLabelOf), len(members))
	}
	byLabel := map[string][]int{}
	for i := range members {
		lbl := themedLabelOf[i]
		if lbl == "" {
			return nil, 0, fmt.Errorf("group: member %q has no themed label", members[i].ObservationHash)
		}
		byLabel[lbl] = append(byLabel[lbl], i)
	}

	labels := make([]string, 0, len(byLabel))
	for l := range byLabel {
		labels = append(labels, l)
	}
	sort.Strings(labels)

	accounted := 0
	for _, lbl := range labels {
		idxs := byLabel[lbl]
		accounted += len(idxs)
		if len(idxs) < minClusterSize {
			dropped += len(idxs)
			continue
		}
		sort.Slice(idxs, func(a, b int) bool {
			return members[idxs[a]].ObservationHash < members[idxs[b]].ObservationHash
		})
		mems := make([]ClusterMember, 0, len(idxs))
		projects := map[string]struct{}{}
		convs := map[string]struct{}{}
		for _, i := range idxs {
			mems = append(mems, members[i])
			if members[i].Project != "" {
				projects[members[i].Project] = struct{}{}
			}
			if members[i].ConversationID != "" {
				convs[members[i].ConversationID] = struct{}{}
			}
		}
		clusters = append(clusters, Cluster{
			Kind:              members[idxs[0]].Kind,
			Canonical:         lbl,
			Members:           mems,
			EvidenceCount:     len(mems),
			ProjectCount:      len(projects),
			ConversationCount: len(convs),
		})
	}
	if accounted != len(members) {
		return nil, 0, fmt.Errorf("group: accounted for %d of %d members", accounted, len(members))
	}
	return clusters, dropped, nil
}

// labelSaveInterval is how often labelAll flushes the label cache to disk
// mid-run, so an interrupted labeling pass keeps its progress.
const labelSaveInterval = 25

// LabelFunc returns one short thematic label for a topic observation's text.
type LabelFunc func(ctx context.Context, text string) (string, error)

// ThemeIdentifyFunc distills the full label vocabulary into a small set of
// canonical theme names (theme pass 1).
type ThemeIdentifyFunc func(ctx context.Context, labels []string) ([]string, error)

// ThemeMapFunc assigns a batch of labels onto a fixed theme set, returning a
// label→theme mapping for that batch (theme pass 2). Keys are resolved back to
// the exact input labels so grouping can match them verbatim.
type ThemeMapFunc func(ctx context.Context, themes, labels []string) (map[string]string, error)

// themeBatchSize bounds how many labels go to one map call. A single call that
// must emit a mapping for the whole vocabulary drops labels at scale (~166
// labels dropped one in practice); batching keeps each call's output small.
const themeBatchSize = 100

// themeMaxRounds caps the map/retry loop. The no-progress self-map fallback
// normally ends it sooner; this is a defensive bound.
const themeMaxRounds = 8

// TopicGrouper runs label → theme → group for topic observations. Model calls
// are injected (Label, ThemeIdentify, ThemeMap) so the path is testable
// without a live model.
type TopicGrouper struct {
	Label                   LabelFunc
	ThemeIdentify           ThemeIdentifyFunc
	ThemeMap                ThemeMapFunc
	Cache                   *LabelCache
	CacheSavePath           string
	ThemesPath              string
	ThemeModel              string
	ThemeIdentifyPromptHash string
	ThemeMapPromptHash      string
	MinClusterSize          int
	Workers                 int
	Log                     func(format string, args ...any)
	// SeedNames are the user-curated leaf topic names.
	// Injected as Pass-1 guidance and unioned into the Pass-2 candidate set so a seeded topic with
	// evidence gets a verbatim-stable slug.
	// Empty = no anchoring.
	SeedNames []string
	// SeedHash is the content hash of the seed, mixed into ThemesFingerprint so
	// editing the seed re-runs the theme step.
	SeedHash string
	// Progress, if non-nil, is called once per newly-labeled observation with
	// the running count and the new-observation total, mirroring synthesize's
	// topic progress. Cached observations are not counted.
	Progress func(done, total int)
}

func (g *TopicGrouper) logf(format string, args ...any) {
	if g.Log != nil {
		g.Log(format, args...)
	}
}

// Run labels every member (cached, parallel), consolidates the vocabulary into
// themes (cached by fingerprint), applies the mapping, and groups by exact
// themed label. Returns the surviving topic clusters.
func (g *TopicGrouper) Run(ctx context.Context, members []ClusterMember) ([]Cluster, error) {
	if len(members) == 0 {
		return nil, nil
	}
	labels, err := g.labelAll(ctx, members)
	if err != nil {
		return nil, fmt.Errorf("label: %w", err)
	}

	uniq := uniqueSorted(labels)
	mapping, err := g.themeMapping(ctx, uniq)
	if err != nil {
		return nil, fmt.Errorf("theme: %w", err)
	}
	if err := validateMapping(uniq, mapping); err != nil {
		return nil, err
	}

	themed := make([]string, len(members))
	for i, l := range labels {
		themed[i] = mapping[l]
	}

	clusters, dropped, err := groupByLabel(members, themed, g.MinClusterSize)
	if err != nil {
		return nil, err
	}
	g.logf("cluster: topics: %d label(s) -> %d theme(s) -> %d topic(s); %d observation(s) dropped as noise (< minClusterSize=%d)",
		len(uniq), len(uniqueSorted(themed)), len(clusters), dropped, g.MinClusterSize)
	return clusters, nil
}

func (g *TopicGrouper) labelAll(ctx context.Context, members []ClusterMember) ([]string, error) {
	out := make([]string, len(members))
	var todo []int
	for i, m := range members {
		if v, ok := g.Cache.Get(m.ObservationHash); ok {
			out[i] = v
			continue
		}
		todo = append(todo, i)
	}
	g.logf("cluster: topics: labeling %d new observation(s), %d cached", len(todo), len(members)-len(todo))

	workers := max(g.Workers, 1)
	sem := make(chan struct{}, workers)
	errs := make([]error, len(todo))
	total := len(todo)
	var mu sync.Mutex
	done := 0
	var wg sync.WaitGroup
	for j, idx := range todo {
		sem <- struct{}{}
		wg.Go(func() {
			defer func() { <-sem }()
			text := members[idx].Text
			if members[idx].Context != "" {
				text = text + "\n\ncontext: " + members[idx].Context
			}
			lbl, err := g.Label(ctx, text)
			if err != nil {
				errs[j] = fmt.Errorf("observation %q: %w", members[idx].ObservationHash, err)
				return
			}
			lbl = strings.TrimSpace(lbl)
			if lbl == "" {
				errs[j] = fmt.Errorf("observation %q: empty label", members[idx].ObservationHash)
				return
			}
			out[idx] = lbl
			g.Cache.Put(members[idx].ObservationHash, lbl)
			mu.Lock()
			done++
			n := done
			// Persist periodically so an interrupted run keeps its progress
			// (each entry is independently valid; the write is atomic). The
			// post-loop Save below still captures the final remainder.
			if g.CacheSavePath != "" && done%labelSaveInterval == 0 {
				if err := g.Cache.Save(g.CacheSavePath); err != nil {
					g.logf("cluster: topics: label cache save: %v", err)
				}
			}
			mu.Unlock()
			// The caller renders this (in-place counter on a TTY, nothing
			// otherwise); the up-front "labeling N new" log marks the start.
			if g.Progress != nil {
				g.Progress(n, total)
			}
		})
	}
	wg.Wait()
	for _, e := range errs {
		if e != nil {
			return nil, e
		}
	}
	if g.CacheSavePath != "" {
		if err := g.Cache.Save(g.CacheSavePath); err != nil {
			g.logf("cluster: topics: label cache save: %v", err)
		}
	}
	return out, nil
}

// NewLabelFunc adapts the anthropic client into a LabelFunc using the label
// system prompt. The label is the model's whole (trimmed) reply.
func NewLabelFunc(client anthropic.Client, model string) LabelFunc {
	return func(ctx context.Context, text string) (string, error) {
		// Frame the payload as a labelled observation. An unframed long,
		// imperative observation ("For tool evaluation, assess on three
		// signals: ...") is indistinguishable from instructions to the model,
		// so it replies "send me an observation" instead of labeling — a ~24%
		// conversational-reply rate on the real corpus. The "Observation:"
		// prefix anchors the role (muse's technique) and fixed it outright.
		raw, err := client.Complete(ctx, model, prompts.ClusterLabelSystem(), "Observation: "+text)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(raw), nil
	}
}

// NewThemeIdentifyFunc adapts the client into a ThemeIdentifyFunc (theme pass
// 1). It sends the full label list and parses the "THEME: <name>" lines.
// seedNames, if non-empty, are injected as Pass-1 guidance so the model keeps those
// distinctions separate and names them as given.
func NewThemeIdentifyFunc(client anthropic.Client, model string, seedNames []string) ThemeIdentifyFunc {
	return func(ctx context.Context, labels []string) ([]string, error) {
		var b strings.Builder
		if len(seedNames) > 0 {
			b.WriteString("The user wants these distinctions kept separate and named exactly as given: ")
			b.WriteString(strings.Join(seedNames, ", "))
			b.WriteString(".\nHonor them when the data supports such a theme; never merge across them; never force unrelated labels into them; discover all other themes naturally.\n\n")
		}
		b.WriteString("LABELS:\n")
		for _, l := range labels {
			b.WriteString("- ")
			b.WriteString(l)
			b.WriteByte('\n')
		}
		raw, err := client.Complete(ctx, model, prompts.ClusterThemeIdentifySystem(), b.String())
		if err != nil {
			return nil, err
		}
		return parseThemeNames(raw), nil
	}
}

// NewThemeMapFunc adapts the client into a ThemeMapFunc (theme pass 2). It maps
// one batch of labels onto the fixed theme set. A parse failure yields an empty
// mapping rather than an error so the caller's retry/self-map loop can recover;
// only a transport error propagates.
func NewThemeMapFunc(client anthropic.Client, model string) ThemeMapFunc {
	return func(ctx context.Context, themes, labels []string) (map[string]string, error) {
		var b strings.Builder
		b.WriteString("THEMES:\n")
		for _, t := range themes {
			b.WriteString("- ")
			b.WriteString(t)
			b.WriteByte('\n')
		}
		b.WriteString("\nLABELS:\n")
		for _, l := range labels {
			b.WriteString("- ")
			b.WriteString(l)
			b.WriteByte('\n')
		}
		raw, err := client.Complete(ctx, model, prompts.ClusterThemeMapSystem(), b.String())
		if err != nil {
			return nil, err
		}
		m, perr := parseThemeMapping(raw)
		if perr != nil {
			m = map[string]string{}
		}
		return resolveMapping(m, labels), nil
	}
}

// parseThemeNames extracts canonical theme names from a pass-1 reply. It
// tolerates a "THEME: " prefix and a leading "- " bullet, and ignores blanks.
func parseThemeNames(raw string) []string {
	var out []string
	seen := map[string]struct{}{}
	for line := range strings.SplitSeq(raw, "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimPrefix(line, "- ")
		if rest, ok := cutThemePrefix(line); ok {
			line = strings.TrimSpace(rest)
		}
		if line == "" {
			continue
		}
		if _, dup := seen[line]; dup {
			continue
		}
		seen[line] = struct{}{}
		out = append(out, line)
	}
	return out
}

// cutThemePrefix strips a case-insensitive "THEME:" prefix if present.
func cutThemePrefix(line string) (string, bool) {
	if len(line) >= 6 && strings.EqualFold(line[:6], "THEME:") {
		return line[6:], true
	}
	return line, false
}

// resolveMapping keeps only entries whose key matches one of inputLabels
// (case-insensitively) and rekeys them to the exact input label, so the themed
// value can be matched against the original label verbatim downstream. Empty
// theme values are dropped.
func resolveMapping(m map[string]string, inputLabels []string) map[string]string {
	lookup := make(map[string]string, len(inputLabels))
	for _, l := range inputLabels {
		lookup[strings.ToLower(l)] = l
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if orig, ok := lookup[strings.ToLower(strings.TrimSpace(k))]; ok {
			out[orig] = v
		}
	}
	return out
}

// parseThemeMapping extracts the {"mapping": {...}} object from the model
// reply, tolerating leading/trailing prose or a code fence.
func parseThemeMapping(raw string) (map[string]string, error) {
	s := strings.TrimSpace(raw)
	start := strings.IndexByte(s, '{')
	end := strings.LastIndexByte(s, '}')
	if start < 0 || end <= start {
		return nil, fmt.Errorf("theme: no JSON object in reply")
	}
	var payload struct {
		Mapping map[string]string `json:"mapping"`
	}
	if err := json.Unmarshal([]byte(s[start:end+1]), &payload); err != nil {
		return nil, fmt.Errorf("theme: parse mapping: %w", err)
	}
	if len(payload.Mapping) == 0 {
		return nil, fmt.Errorf("theme: empty mapping")
	}
	return payload.Mapping, nil
}

// themeMapping consolidates the label vocabulary into a label→theme mapping in
// two passes: identify the canonical theme set, then map labels onto it in
// batches, retrying the unmapped remainder each round. A round that makes no
// progress (or the round cap) self-maps the stragglers so every label is
// always covered. Cached by fingerprint in themes.json.
func (g *TopicGrouper) themeMapping(ctx context.Context, uniqueLabels []string) (map[string]string, error) {
	fp := ThemesFingerprint(uniqueLabels, g.ThemeModel, g.ThemeIdentifyPromptHash, g.ThemeMapPromptHash, g.SeedHash)
	if g.ThemesPath != "" {
		if tf, err := LoadThemes(g.ThemesPath); err == nil && tf.Fingerprint == fp && tf.Mapping != nil {
			return tf.Mapping, nil
		}
	}

	themes, err := g.ThemeIdentify(ctx, uniqueLabels)
	if err != nil {
		return nil, fmt.Errorf("identify: %w", err)
	}
	if len(themes) == 0 {
		return nil, fmt.Errorf("identify: no themes returned")
	}
	g.logf("cluster: topics: identified %d theme(s) from %d label(s)", len(themes), len(uniqueLabels))

	mapping := make(map[string]string, len(uniqueLabels))
	unmapped := uniqueLabels
	for round := 1; len(unmapped) > 0; round++ {
		got, err := g.mapBatches(ctx, themes, unmapped)
		if err != nil {
			return nil, fmt.Errorf("map round %d: %w", round, err)
		}
		maps.Copy(mapping, got)
		var remaining []string
		for _, l := range unmapped {
			if _, ok := mapping[l]; !ok {
				remaining = append(remaining, l)
			}
		}
		if len(remaining) == len(unmapped) || round >= themeMaxRounds {
			// No progress (or round cap): self-map the stragglers as their own
			// theme. They form singleton groups that fall below MinClusterSize
			// and drop out as noise — the right outcome for a label that fits
			// no theme — rather than failing the whole run.
			for _, l := range remaining {
				mapping[l] = l
			}
			if len(remaining) > 0 {
				g.logf("cluster: topics: %d label(s) self-mapped (no theme fit) after round %d", len(remaining), round)
			}
			break
		}
		g.logf("cluster: topics: theme map round %d: %d/%d mapped, %d remaining",
			round, len(unmapped)-len(remaining), len(unmapped), len(remaining))
		unmapped = remaining
	}

	if g.ThemesPath != "" {
		if err := SaveThemes(g.ThemesPath, ThemesFile{Fingerprint: fp, Mapping: mapping}); err != nil {
			g.logf("cluster: topics: themes save: %v", err)
		}
	}
	return mapping, nil
}

// mapBatches maps labels onto themes in parallel batches of themeBatchSize. A
// transport error from any batch fails the whole call; parse failures are
// absorbed by the adapter (empty batch result) and surface as unmapped labels
// for the caller's retry loop.
func (g *TopicGrouper) mapBatches(ctx context.Context, themes, labels []string) (map[string]string, error) {
	workers := max(g.Workers, 1)
	sem := make(chan struct{}, workers)
	out := make(map[string]string, len(labels))
	var mu sync.Mutex
	var firstErr error
	var wg sync.WaitGroup
	for i := 0; i < len(labels); i += themeBatchSize {
		batch := labels[i:min(i+themeBatchSize, len(labels))]
		sem <- struct{}{}
		wg.Go(func() {
			defer func() { <-sem }()
			m, err := g.ThemeMap(ctx, themes, batch)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				return
			}
			maps.Copy(out, m)
		})
	}
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	return out, nil
}
