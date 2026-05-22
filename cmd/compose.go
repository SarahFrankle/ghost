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
	"github.com/SarahFrankle/ghost/internal/canonicalize"
	"github.com/SarahFrankle/ghost/internal/cluster"
	"github.com/SarahFrankle/ghost/internal/config"
	"github.com/SarahFrankle/ghost/internal/embedding"
	"github.com/SarahFrankle/ghost/internal/extract"
	"github.com/SarahFrankle/ghost/internal/ledger"
	"github.com/SarahFrankle/ghost/internal/paths"
	"github.com/SarahFrankle/ghost/internal/source"
	"github.com/SarahFrankle/ghost/internal/synthesize"
)

var (
	composeLimit    int
	composeStages   string
	composeDry      bool
	composeEstimate bool
)

var composeCmd = &cobra.Command{
	Use:   "compose",
	Short: "Run the ghost compose pipeline",
	RunE: func(cmd *cobra.Command, args []string) error {
		stages, err := parseStages(composeStages)
		if err != nil {
			return err
		}
		if composeEstimate {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			return runEstimate(cmd.Context(), cfg, stages)
		}
		for _, s := range stages {
			switch s {
			case "extract":
				if err := runExtract(cmd.Context()); err != nil {
					return err
				}
			case "canonicalize":
				if err := runCanonicalize(cmd.Context()); err != nil {
					return err
				}
			case "cluster":
				if err := runCluster(cmd.Context()); err != nil {
					return err
				}
			case "synthesize":
				if err := runSynthesize(cmd.Context()); err != nil {
					return err
				}
			default:
				return fmt.Errorf("unknown stage %q", s)
			}
		}
		return nil
	},
}

func init() {
	composeCmd.Flags().IntVar(&composeLimit, "limit", 0, "process at most N unprocessed transcripts (0 = all)")
	composeCmd.Flags().StringVar(&composeStages, "stages", "extract", "comma-separated stages: extract,canonicalize,cluster,synthesize, or all")
	composeCmd.Flags().BoolVar(&composeDry, "dry-run", false, "list what would be processed and exit")
	composeCmd.Flags().BoolVar(&composeEstimate, "estimate", false, "print per-stage token + cost estimate and exit")
	rootCmd.AddCommand(composeCmd)
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
		if !l.NeedsProcessing(c.ID, h) {
			continue
		}
		pending = append(pending, job{c: c, hash: h})
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
		Client:      client,
		Model:       cfg.Models.Cheap,
		Log:         log.Default(),
		KnownTopics: listKnownTopics(outDir),
	}

	workers := cfg.Batching.ExtractWorkers
	if workers < 1 {
		workers = 1
	}
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	var processed, failed int
	var mu sync.Mutex

	for _, j := range pending {
		j := j
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()

			result, err := runner.Run(ctx, src, j.c, j.hash)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				log.Printf("extract %s: %v", j.c.ID, err)
				failed++
				return
			}
			obsFileName := observationsFileName(j.hash) + ".json"
			obsRelPath := filepath.Join(".state/observations", obsFileName)
			obsAbsPath := filepath.Join(stateDir, "observations", obsFileName)

			body, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				log.Printf("marshal %s: %v", j.c.ID, err)
				failed++
				return
			}
			if err := atomicfs.WriteFile(obsAbsPath, body, 0o644); err != nil {
				log.Printf("write %s: %v", obsAbsPath, err)
				failed++
				return
			}
			l.Mark(j.c.ID, ledger.Entry{
				ContentHash:      j.hash,
				ObservationsFile: obsRelPath,
				MessageCount:     len(result.Observations),
			})
			if err := l.Save(ledgerPath); err != nil {
				log.Printf("ledger save after %s: %v", j.c.ID, err)
			}
			processed++
			fmt.Printf("extracted %d observation(s) from %s\n", len(result.Observations), filepath.Base(j.c.ID))
		}()
	}
	wg.Wait()

	l.SetLastCompose([]string{"extract"}, "")
	if err := l.Save(ledgerPath); err != nil {
		return err
	}
	fmt.Printf("done: processed=%d failed=%d\n", processed, failed)
	return nil
}

// listKnownTopics returns the slugs of existing topic files (basename without
// extension) under outDir/topics, sorted. Used to bias the extract prompt
// toward reusing existing slugs instead of minting near-synonym variants.
// A missing or unreadable topics dir is treated as "no known topics".
func listKnownTopics(outDir string) []string {
	entries, err := os.ReadDir(filepath.Join(outDir, "topics"))
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".md") {
			continue
		}
		out = append(out, strings.TrimSuffix(name, ".md"))
	}
	sort.Strings(out)
	return out
}

func observationsFileName(contentHash string) string {
	trimmed := strings.TrimPrefix(contentHash, "sha256:")
	if len(trimmed) > 16 {
		trimmed = trimmed[:16]
	}
	sum := sha256.Sum256([]byte(contentHash))
	return trimmed + "-" + hex.EncodeToString(sum[:4])
}

// parseStages accepts comma-separated stage names or the literal "all".
// Order is enforced: extract → cluster → synthesize.
func parseStages(raw string) ([]string, error) {
	if raw == "all" {
		return []string{"extract", "canonicalize", "cluster", "synthesize"}, nil
	}
	known := map[string]int{"extract": 0, "canonicalize": 1, "cluster": 2, "synthesize": 3}
	parts := strings.Split(raw, ",")
	for _, p := range parts {
		if _, ok := known[p]; !ok {
			return nil, fmt.Errorf("unknown stage %q (want one of: extract, canonicalize, cluster, synthesize, all)", p)
		}
	}
	sort.SliceStable(parts, func(i, j int) bool { return known[parts[i]] < known[parts[j]] })
	return parts, nil
}

func runCanonicalize(ctx context.Context) error {
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
	aliasesPath := filepath.Join(stateDir, "slug_aliases.json")

	slugSamples, err := observedTopicSlugSamples(obsDir)
	if err != nil {
		return err
	}
	if len(slugSamples) < 2 {
		fmt.Printf("canonicalize: %d distinct topic slug(s); nothing to do\n", len(slugSamples))
		return nil
	}

	existing, err := canonicalize.Load(aliasesPath)
	if err != nil {
		return fmt.Errorf("load aliases: %w", err)
	}

	client, err := anthropic.New()
	if err != nil {
		return err
	}
	emb, embModel := selectEmbedder(cfg.Models.Embedding)
	p := &canonicalize.Pipeline{
		Client:              client,
		Model:               cfg.Models.Cheap,
		Log:                 log.Printf,
		Embedder:            emb,
		EmbeddingModel:      embModel,
		SimilarityThreshold: float32(cfg.Thresholds.CanonicalizeSimilarityThreshold),
	}
	merged, err := p.Run(ctx, slugSamples, existing)
	if err != nil {
		return err
	}

	if err := canonicalize.Save(aliasesPath, cfg.Models.Cheap, merged); err != nil {
		return fmt.Errorf("save aliases: %w", err)
	}
	fmt.Printf("canonicalize: %d alias(es) total; wrote %s\n", len(merged), aliasesPath)
	return nil
}

// observedTopicSlugSamples reads every observations JSON under obsDir
// and returns slug → sample observation texts for topic-kind
// observations. The samples drive the canonicalizer's embedding
// fingerprints; the slug set is the same as before but now carries
// signal about what each slug actually contains.
func observedTopicSlugSamples(obsDir string) (map[string][]string, error) {
	entries, err := os.ReadDir(obsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := map[string][]string{}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(obsDir, e.Name()))
		if err != nil {
			return nil, err
		}
		var f extract.ObservationsFile
		if err := json.Unmarshal(b, &f); err != nil {
			return nil, fmt.Errorf("decode %s: %w", e.Name(), err)
		}
		for _, o := range f.Observations {
			if o.Kind == "topic" && o.Topic != "" {
				out[o.Topic] = append(out[o.Topic], o.Text)
			}
		}
	}
	return out, nil
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

	emb, embModel := selectEmbedder(cfg.Models.Embedding)
	log.Printf("cluster: using embedder %T model=%s", emb, embModel)
	cache, err := embedding.LoadCache(filepath.Join(stateDir, "embeddings.json"), embModel)
	if err != nil {
		return err
	}

	client, err := anthropic.New()
	if err != nil {
		return err
	}
	canonCache, err := cluster.LoadCanonicalCache(filepath.Join(stateDir, "canonical_cache.json"), cfg.Models.Cheap)
	if err != nil {
		return err
	}
	canon := &cluster.Canonicalizer{
		Client:  client,
		Model:   cfg.Models.Cheap,
		Cache:   canonCache,
		Workers: cfg.Batching.ExtractWorkers,
		Log:     log.Printf,
	}

	aliases, err := canonicalize.Load(filepath.Join(stateDir, "slug_aliases.json"))
	if err != nil {
		return fmt.Errorf("load slug aliases: %w", err)
	}

	p := &cluster.Pipeline{
		Embedder:        emb,
		EmbeddingModel:  embModel,
		Cache:           cache,
		CacheSavePath:   filepath.Join(stateDir, "embeddings.json"),
		ClustersPath:    filepath.Join(stateDir, "clusters.json"),
		CosineThreshold: float32(cfg.Thresholds.ClusterCosineThreshold),
		Canonicalizer:   canon,
		Workers:         cfg.Batching.ExtractWorkers,
		Log:             log.Printf,
		TopicAliases:    aliases,
	}
	if err := p.Run(ctx, obsDir); err != nil {
		return err
	}
	if err := canonCache.Save(filepath.Join(stateDir, "canonical_cache.json")); err != nil {
		log.Printf("canonical cache save: %v", err)
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

// selectEmbedder picks an embedding backend based on environment.
// Voyage if VOYAGE_API_KEY is set, otherwise local Ollama. Returns the
// model name to use, since each provider has its own default model:
// the configured cfg.Models.Embedding is used for Voyage; Ollama falls
// back to nomic-embed-text unless OLLAMA_EMBEDDING_MODEL overrides it.
func selectEmbedder(configuredModel string) (embedding.Embedder, string) {
	if os.Getenv("VOYAGE_API_KEY") != "" {
		v, _ := embedding.NewVoyageFromEnv()
		return v, configuredModel
	}
	model := os.Getenv("OLLAMA_EMBEDDING_MODEL")
	if model == "" {
		model = "nomic-embed-text"
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
		return fmt.Errorf("load clusters.json (run `ghost compose --stages cluster` first): %w", err)
	}

	client, err := anthropic.New()
	if err != nil {
		return err
	}
	p := &synthesize.Pipeline{
		Client:          client,
		SmartModel:      cfg.Models.Smart,
		GhostDir:        outDir,
		MinRuleEvidence: cfg.Thresholds.RuleMinEvidenceCount,
		MinRuleProjects: cfg.Thresholds.RuleMinProjectCount,
		MaxTopicEntries: cfg.Index.MaxTopicEntries,
	}
	if err := p.Run(ctx, cf); err != nil {
		return err
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
