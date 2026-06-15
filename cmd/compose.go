package cmd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/SarahFrankle/ghost/internal/anthropic"
	"github.com/SarahFrankle/ghost/internal/atomicfs"
	"github.com/SarahFrankle/ghost/internal/cluster"
	"github.com/SarahFrankle/ghost/internal/config"
	"github.com/SarahFrankle/ghost/internal/embedding"
	"github.com/SarahFrankle/ghost/internal/extract"
	"github.com/SarahFrankle/ghost/internal/fingerprint"
	"github.com/SarahFrankle/ghost/internal/ledger"
	"github.com/SarahFrankle/ghost/internal/paths"
	"github.com/SarahFrankle/ghost/internal/source"
	"github.com/SarahFrankle/ghost/internal/synthesize"
	"github.com/SarahFrankle/ghost/prompts"
)

var (
	composeLimit     int
	composeDry       bool
	composeEstimate  bool
	composeReobserve bool
	composeRecluster bool
	composeResynth   bool
)

// allStages is the full pipeline order used by `ghost compose`.
var allStages = []string{"extract", "cluster", "synthesize"}

var composeCmd = &cobra.Command{
	Use:   "compose",
	Short: "Run the full ghost pipeline: extract, cluster, synthesize",
	RunE: func(cmd *cobra.Command, args []string) error {
		if composeEstimate {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			return runEstimate(cmd.Context(), cfg, allStages)
		}
		if err := runExtract(cmd.Context()); err != nil {
			return err
		}
		if err := runCluster(cmd.Context()); err != nil {
			return err
		}
		if err := runSynthesize(cmd.Context()); err != nil {
			return err
		}
		// Each sub-stage records only its own stage in the ledger; the last
		// writer (synthesize) would otherwise leave `stages: [synthesize]`.
		// Overwrite with the full pipeline so `ghost status` reflects that a
		// complete compose ran.
		return recordCompose(allStages)
	},
}

var extractCmd = &cobra.Command{
	Use:   "extract",
	Short: "Extract observations from new transcripts",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runExtract(cmd.Context())
	},
}

var clusterCmd = &cobra.Command{
	Use:   "cluster",
	Short: "Cluster observations by similarity",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runCluster(cmd.Context())
	},
}

var synthesizeCmd = &cobra.Command{
	Use:   "synthesize",
	Short: "Synthesize identity, rules, topics, and index from clusters",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runSynthesize(cmd.Context())
	},
}

func init() {
	extractCmd.Flags().IntVar(&composeLimit, "limit", 0, "process at most N unprocessed transcripts (0 = all)")
	extractCmd.Flags().BoolVar(&composeDry, "dry-run", false, "list what would be processed and exit")
	extractCmd.Flags().BoolVar(&composeReobserve, "reobserve", false, "force re-extract of all transcripts, skipping fingerprint cache")

	clusterCmd.Flags().BoolVar(&composeRecluster, "recluster", false, "force rebuild of clusters.json, skipping fingerprint cache")

	synthesizeCmd.Flags().BoolVar(&composeResynth, "resynth", false, "force re-synthesis of identity/rules/topics/index, skipping fingerprint cache")

	composeCmd.Flags().IntVar(&composeLimit, "limit", 0, "process at most N unprocessed transcripts in the extract stage (0 = all)")
	composeCmd.Flags().BoolVar(&composeEstimate, "estimate", false, "print per-stage token + cost estimate for the full pipeline and exit")

	rootCmd.AddCommand(composeCmd)
	rootCmd.AddCommand(extractCmd)
	rootCmd.AddCommand(clusterCmd)
	rootCmd.AddCommand(synthesizeCmd)
}

// recordCompose stamps the ledger's last_compose record with the given stage
// list. `ghost compose` calls it once after all stages succeed so the record
// reflects the full pipeline rather than only the final sub-stage's write.
func recordCompose(stages []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	outDir, err := paths.Expand(cfg.Paths.OutputDir)
	if err != nil {
		return err
	}
	ledgerPath := filepath.Join(outDir, ".state", "ledger.json")
	l, err := ledger.Load(ledgerPath)
	if err != nil {
		return err
	}
	l.SetLastCompose(stages, "")
	return l.Save(ledgerPath)
}

func runExtract(ctx context.Context) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	outDir, err := paths.Expand(cfg.Paths.OutputDir)
	if err != nil {
		return err
	}
	stateDir := filepath.Join(outDir, ".state")
	obsDir := filepath.Join(stateDir, "observations")
	if err := paths.EnsureDir(obsDir); err != nil {
		return err
	}

	ledgerPath := filepath.Join(stateDir, "ledger.json")
	l, err := ledger.Load(ledgerPath)
	if err != nil {
		return err
	}

	glob, err := paths.Expand(cfg.Paths.TranscriptsGlob)
	if err != nil {
		return err
	}
	src := source.ClaudeCode(glob)
	convs, err := src.Discover(ctx, 5*time.Minute, time.Now())
	if err != nil {
		return err
	}

	type job struct {
		c    source.Conversation
		hash string
	}
	pending := make([]job, 0, len(convs))
	for _, c := range convs {
		h, err := src.ContentHash(ctx, c)
		if err != nil {
			log.Printf("hash %s: %v", c.ID, err)
			continue
		}
		if composeReobserve {
			pending = append(pending, job{c: c, hash: h})
			continue
		}
		if l.NeedsProcessing(c.ID, h) {
			pending = append(pending, job{c: c, hash: h})
			continue
		}
		if observationsStale(outDir, l, c.ID, c.Source, c.Project, h, cfg.Models.Cheap) {
			pending = append(pending, job{c: c, hash: h})
		}
	}
	sort.Slice(pending, func(i, j int) bool {
		return pending[i].c.ModTime.Before(pending[j].c.ModTime)
	})
	if composeLimit > 0 && len(pending) > composeLimit {
		pending = pending[:composeLimit]
	}

	if composeDry {
		fmt.Printf("would process %d transcript(s):\n", len(pending))
		for _, p := range pending {
			fmt.Printf("  %s\n", p.c.ID)
		}
		return nil
	}
	if len(pending) == 0 {
		fmt.Println("nothing to do")
		return nil
	}

	client, err := anthropic.New()
	if err != nil {
		return err
	}
	runner := &extract.Runner{
		Client: client,
		Model:  cfg.Models.Cheap,
		Log:    log.Default(),
	}

	const ledgerSaveInterval = 25
	workers := cfg.Batching.ExtractWorkers
	workers = max(workers, 1)
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	var processed, failed, totalObs int
	var failures []string
	total := len(pending)
	var mu sync.Mutex
	counter := stderrCounter("extract:")

	for _, j := range pending {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()

			result, runErr := runner.Run(ctx, src, j.c, j.hash)
			mu.Lock()
			defer mu.Unlock()
			defer func() {
				if counter != nil {
					counter(processed+failed, total)
				}
			}()

			fail := func(format string, args ...any) {
				failed++
				failures = append(failures, fmt.Sprintf(format, args...))
			}
			if runErr != nil {
				fail("%s: %v", filepath.Base(j.c.ID), runErr)
				return
			}
			obsFileName := observationsFileName(j.hash) + ".json"
			obsRelPath := filepath.Join(".state/observations", obsFileName)
			obsAbsPath := filepath.Join(stateDir, "observations", obsFileName)

			body, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				fail("%s: marshal: %v", filepath.Base(j.c.ID), err)
				return
			}
			if err := atomicfs.WriteFile(obsAbsPath, body, 0o644); err != nil {
				fail("%s: write: %v", filepath.Base(j.c.ID), err)
				return
			}
			l.Mark(j.c.ID, ledger.Entry{
				ContentHash:      j.hash,
				ObservationsFile: obsRelPath,
				MessageCount:     len(result.Observations),
			})
			processed++
			totalObs += len(result.Observations)
			// Checkpoint periodically for crash-resumability instead of
			// rewriting the whole ledger after every transcript — the latter
			// is O(N^2) write volume serialized under mu, partly defeating the
			// worker pool. The post-loop Save persists the final state.
			if processed%ledgerSaveInterval == 0 {
				if err := l.Save(ledgerPath); err != nil {
					log.Printf("ledger checkpoint after %s: %v", j.c.ID, err)
				}
			}
		}()
	}
	wg.Wait()

	l.SetLastCompose([]string{"extract"}, "")
	if err := l.Save(ledgerPath); err != nil {
		return err
	}
	fmt.Printf("done: processed=%d failed=%d observations=%d\n", processed, failed, totalObs)
	for _, f := range failures {
		fmt.Printf("  failed: %s\n", f)
	}
	return nil
}

// observationsStale reports whether the cached observations file for convID
// is missing or carries a fingerprint that no longer matches the current
// source / prompt / model. A stale (or missing) file means the transcript
// should be re-extracted even though its content hash hasn't changed.
func observationsStale(outDir string, l *ledger.Ledger, convID, sourceName, project, contentHash, model string) bool {
	entry, ok := l.Get(convID)
	if !ok || entry.ObservationsFile == "" {
		return true
	}
	absPath := filepath.Join(outDir, entry.ObservationsFile)
	b, err := os.ReadFile(absPath)
	if err != nil {
		return true
	}
	var f extract.ObservationsFile
	if err := json.Unmarshal(b, &f); err != nil {
		return true
	}
	want := extract.ObservationsFingerprint(sourceName, project, contentHash, model)
	return f.Fingerprint != want
}

func observationsFileName(contentHash string) string {
	trimmed := strings.TrimPrefix(contentHash, "sha256:")
	if len(trimmed) > 16 {
		trimmed = trimmed[:16]
	}
	sum := sha256.Sum256([]byte(contentHash))
	return trimmed + "-" + hex.EncodeToString(sum[:4])
}

func runCluster(ctx context.Context) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	outDir, err := paths.Expand(cfg.Paths.OutputDir)
	if err != nil {
		return err
	}
	stateDir := filepath.Join(outDir, ".state")
	obsDir := filepath.Join(stateDir, "observations")
	clustersPath := filepath.Join(stateDir, "clusters.json")

	obsFingerprints, err := cluster.ObservationsFingerprints(obsDir)
	if err != nil {
		return fmt.Errorf("scan observations fingerprints: %w", err)
	}
	embModelForFP := embeddingModelName(cfg)
	// Only identity/rule/voice cosine-cluster now; topics route through the
	// grouper and never reach thresholdFor.
	thresholdFor := func(string) float32 {
		return float32(cfg.Thresholds.ClusterCosineIdentityRule)
	}
	labelModel := cfg.Models.Label
	if labelModel == "" {
		labelModel = cfg.Models.Cheap
	}
	themeModel := cfg.Models.Theme
	if themeModel == "" {
		themeModel = cfg.Models.Smart
	}
	expectedFP := cluster.ClustersFingerprint(
		obsFingerprints,
		embModelForFP,
		float32(cfg.Thresholds.ClusterCosineIdentityRule),
		labelModel,
		prompts.ClusterLabelSystemHash(),
		themeModel,
		prompts.ClusterThemeIdentifySystemHash(),
		prompts.ClusterThemeMapSystemHash(),
		cfg.Thresholds.MinClusterSize,
	)
	if !composeRecluster {
		if existing, err := cluster.LoadClusters(clustersPath); err == nil && existing.Fingerprint == expectedFP {
			fmt.Println("cluster: up to date (fingerprint match)")
			return nil
		}
	}

	emb, embModel := selectEmbedder(cfg)
	log.Printf("cluster: using embedder %T model=%s", emb, embModel)
	cache, err := embedding.LoadCache(filepath.Join(stateDir, "embeddings.json"), embModel)
	if err != nil {
		return err
	}

	client, err := anthropic.New()
	if err != nil {
		return err
	}
	labelCache, err := cluster.LoadLabelCache(filepath.Join(stateDir, "labels.json"), labelModel, prompts.ClusterLabelSystemHash())
	if err != nil {
		return err
	}
	grouper := &cluster.TopicGrouper{
		Label:                   cluster.NewLabelFunc(client, labelModel),
		ThemeIdentify:           cluster.NewThemeIdentifyFunc(client, themeModel),
		ThemeMap:                cluster.NewThemeMapFunc(client, themeModel),
		Cache:                   labelCache,
		CacheSavePath:           filepath.Join(stateDir, "labels.json"),
		ThemesPath:              filepath.Join(stateDir, "themes.json"),
		ThemeModel:              themeModel,
		ThemeIdentifyPromptHash: prompts.ClusterThemeIdentifySystemHash(),
		ThemeMapPromptHash:      prompts.ClusterThemeMapSystemHash(),
		MinClusterSize:          1, // stop dropping at grouping; noise floors apply post-routing in synthesize
		Workers:                 cfg.Batching.ExtractWorkers,
		Log:                     log.Printf,
		Progress:                stderrCounter("cluster: topics: completed"),
	}

	p := &cluster.Pipeline{
		Embedder:       emb,
		EmbeddingModel: embModel,
		Cache:          cache,
		CacheSavePath:  filepath.Join(stateDir, "embeddings.json"),
		ClustersPath:   clustersPath,
		ThresholdFor:   thresholdFor,
		Workers:        cfg.Batching.ExtractWorkers,
		Log:            log.Printf,
		Topics:         grouper,
		Fingerprint:    expectedFP,
	}
	if err := p.Run(ctx, obsDir); err != nil {
		return err
	}

	l, err := ledger.Load(filepath.Join(stateDir, "ledger.json"))
	if err != nil {
		return err
	}
	l.SetLastCompose([]string{"cluster"}, "")
	if err := l.Save(filepath.Join(stateDir, "ledger.json")); err != nil {
		return err
	}
	fmt.Println("cluster: done")
	return nil
}

// embeddingModelName returns the embedding model id that selectEmbedder
// will use, without constructing the embedder. Fingerprinting and cost
// estimation call this so they never diverge from the real backend:
// Voyage uses the configured cfg.Models.Embedding; Ollama falls back to
// nomic-embed-text unless OLLAMA_EMBEDDING_MODEL overrides it.
func embeddingModelName(cfg config.Config) string {
	if os.Getenv("VOYAGE_API_KEY") != "" {
		return cfg.Models.Embedding
	}
	if m := os.Getenv("OLLAMA_EMBEDDING_MODEL"); m != "" {
		return m
	}
	return "nomic-embed-text"
}

// selectEmbedder picks an embedding backend based on environment.
// Voyage if VOYAGE_API_KEY is set, otherwise local Ollama. The model
// name comes from embeddingModelName so it matches what the fingerprint
// and estimate use.
func selectEmbedder(cfg config.Config) (embedding.Embedder, string) {
	model := embeddingModelName(cfg)
	if os.Getenv("VOYAGE_API_KEY") != "" {
		v, _ := embedding.NewVoyageFromEnv()
		return v, model
	}
	return embedding.NewOllamaFromEnv(), model
}

func runSynthesize(ctx context.Context) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	outDir, err := paths.Expand(cfg.Paths.OutputDir)
	if err != nil {
		return err
	}
	stateDir := filepath.Join(outDir, ".state")
	clustersPath := filepath.Join(stateDir, "clusters.json")

	cf, err := cluster.LoadClusters(clustersPath)
	if err != nil {
		return fmt.Errorf("load clusters.json (run `ghost cluster` first): %w", err)
	}

	expectedFP := synthesizeFingerprint(cf.Fingerprint, cfg.Models.Smart, cfg.Models.Topic, cfg.Thresholds.RecurrenceForConfidence, cfg.Index.MaxTopicEntries)
	sidecarPath := filepath.Join(stateDir, "synthesize.fingerprint")
	if !composeResynth && synthesizeOutputsFresh(outDir, sidecarPath, expectedFP) {
		fmt.Println("synthesize: up to date (fingerprint match)")
		return nil
	}

	client, err := anthropic.New()
	if err != nil {
		return err
	}
	p := &synthesize.Pipeline{
		Client:                  client,
		SmartModel:              cfg.Models.Smart,
		TopicModel:              cfg.Models.Topic,
		GhostDir:                outDir,
		MaxTopicEntries:         cfg.Index.MaxTopicEntries,
		GeneralityModel:         cfg.Models.Smart,
		VerdictsPath:            filepath.Join(stateDir, "verdicts.json"),
		RecurrenceForConfidence: cfg.Thresholds.RecurrenceForConfidence,
		Workers:                 cfg.Batching.SynthWorkers,
		Log:                     log.Printf,
		Progress:                stderrCounter("synthesize: topics"),
	}
	if err := p.Run(ctx, cf); err != nil {
		return err
	}
	if err := os.WriteFile(sidecarPath, []byte(expectedFP), 0o644); err != nil {
		log.Printf("synthesize: write fingerprint sidecar: %v", err)
	}

	l, err := ledger.Load(filepath.Join(stateDir, "ledger.json"))
	if err != nil {
		return err
	}
	l.SetLastCompose([]string{"synthesize"}, "")
	if err := l.Save(filepath.Join(stateDir, "ledger.json")); err != nil {
		return err
	}
	fmt.Println("synthesize: wrote identity.md, rules.md, topics/*.md, index.md")
	return nil
}

// stderrCounter returns an in-place progress counter that rewrites a single
// stderr line "<label> done/total" (e.g. "synthesize: topics 37/141"). When
// stderr is not a terminal (piped, redirected, CI) it returns nil so progress
// stays quiet rather than spraying carriage returns into a log file; the
// per-stage log lines already mark coarse progress there.
func stderrCounter(label string) func(done, total int) {
	fi, err := os.Stderr.Stat()
	if err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		return nil
	}
	return func(done, total int) {
		fmt.Fprintf(os.Stderr, "\r\033[K%s %d/%d", label, done, total)
		if done == total {
			fmt.Fprintln(os.Stderr)
		}
	}
}

// synthesizeFingerprint composes the cache key for synthesize outputs.
// Inputs: the clusters.json fingerprint (which already captures observation
// state, embedding model, and the per-kind cosine thresholds), the smart and
// topic models, the five synth prompts (including generality routing), and the
// structural thresholds that change which clusters survive to be rendered. The
// "synthesize/v3" namespace guarantees stale v2 caches miss on the first run
// after the generality-routing redesign.
func synthesizeFingerprint(clustersFP, smartModel, topicModel string, recurrenceForConfidence, maxTopicEntries int) string {
	return fingerprint.Compute(
		"synthesize/v3",
		clustersFP,
		smartModel,
		topicModel,
		prompts.SynthesizeIdentitySystemHash(),
		prompts.SynthesizeRulesSystemHash(),
		prompts.SynthesizeTopicsSystemHash(),
		prompts.SynthesizeIndexSystemHash(),
		prompts.SynthesizeGeneralitySystemHash(),
		fmt.Sprintf("recurrence_for_confidence=%d", recurrenceForConfidence),
		fmt.Sprintf("max_topics=%d", maxTopicEntries),
	)
}

// synthesizeOutputsFresh reports whether the sidecar fingerprint at
// sidecarPath matches expectedFP AND the load-bearing synthesized files
// exist on disk. The file existence check guards against the case where
// outputs were manually deleted but the sidecar remains.
func synthesizeOutputsFresh(outDir, sidecarPath, expectedFP string) bool {
	b, err := os.ReadFile(sidecarPath)
	if err != nil {
		return false
	}
	if strings.TrimSpace(string(b)) != expectedFP {
		return false
	}
	for _, name := range []string{"identity.md", "rules.md", "index.md"} {
		if _, err := os.Stat(filepath.Join(outDir, name)); err != nil {
			return false
		}
	}
	return true
}

func loadConfig() (config.Config, error) {
	path := configPath
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return config.Config{}, err
		}
		path = filepath.Join(home, ".ghost", "config.toml")
	}
	return config.Load(path)
}
