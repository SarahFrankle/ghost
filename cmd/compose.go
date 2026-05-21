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
	"github.com/SarahFrankle/ghost/internal/ledger"
	"github.com/SarahFrankle/ghost/internal/paths"
	"github.com/SarahFrankle/ghost/internal/transcript"
)

var (
	composeLimit  int
	composeStages string
	composeDry    bool
)

var composeCmd = &cobra.Command{
	Use:   "compose",
	Short: "Run the ghost compose pipeline",
	RunE: func(cmd *cobra.Command, args []string) error {
		stages, err := parseStages(composeStages)
		if err != nil {
			return err
		}
		for _, s := range stages {
			switch s {
			case "extract":
				if err := runExtract(cmd.Context()); err != nil {
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
	composeCmd.Flags().StringVar(&composeStages, "stages", "extract", "comma-separated stages: extract,cluster,synthesize, or all")
	composeCmd.Flags().BoolVar(&composeDry, "dry-run", false, "list what would be processed and exit")
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
	transcripts, err := transcript.Discover(glob, 5*time.Minute, time.Now())
	if err != nil {
		return err
	}

	type job struct {
		t    transcript.Transcript
		hash string
	}
	pending := make([]job, 0, len(transcripts))
	for _, t := range transcripts {
		h, err := transcript.ContentHash(t.Path)
		if err != nil {
			log.Printf("hash %s: %v", t.Path, err)
			continue
		}
		if !l.NeedsProcessing(t.Path, h) {
			continue
		}
		pending = append(pending, job{t: t, hash: h})
	}
	sort.Slice(pending, func(i, j int) bool {
		return pending[i].t.ModTime.Before(pending[j].t.ModTime)
	})
	if composeLimit > 0 && len(pending) > composeLimit {
		pending = pending[:composeLimit]
	}

	if composeDry {
		fmt.Printf("would process %d transcript(s):\n", len(pending))
		for _, p := range pending {
			fmt.Printf("  %s\n", p.t.Path)
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

			result, err := runner.Run(ctx, j.t, j.hash)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				log.Printf("extract %s: %v", j.t.Path, err)
				failed++
				return
			}
			obsFileName := observationsFileName(j.hash) + ".json"
			obsRelPath := filepath.Join(".state/observations", obsFileName)
			obsAbsPath := filepath.Join(stateDir, "observations", obsFileName)

			body, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				log.Printf("marshal %s: %v", j.t.Path, err)
				failed++
				return
			}
			if err := atomicfs.WriteFile(obsAbsPath, body, 0o644); err != nil {
				log.Printf("write %s: %v", obsAbsPath, err)
				failed++
				return
			}
			l.Mark(j.t.Path, ledger.Entry{
				ContentHash:      j.hash,
				ObservationsFile: obsRelPath,
				MessageCount:     len(result.Observations),
			})
			if err := l.Save(ledgerPath); err != nil {
				log.Printf("ledger save after %s: %v", j.t.Path, err)
			}
			processed++
			fmt.Printf("extracted %d observation(s) from %s\n", len(result.Observations), filepath.Base(j.t.Path))
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
		return []string{"extract", "cluster", "synthesize"}, nil
	}
	known := map[string]int{"extract": 0, "cluster": 1, "synthesize": 2}
	parts := strings.Split(raw, ",")
	for _, p := range parts {
		if _, ok := known[p]; !ok {
			return nil, fmt.Errorf("unknown stage %q (want one of: extract, cluster, synthesize, all)", p)
		}
	}
	sort.SliceStable(parts, func(i, j int) bool { return known[parts[i]] < known[parts[j]] })
	return parts, nil
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

	emb, err := embedding.NewVoyageFromEnv()
	if err != nil {
		return err
	}
	cache, err := embedding.LoadCache(filepath.Join(stateDir, "embeddings.json"), cfg.Models.Embedding)
	if err != nil {
		return err
	}

	client, err := anthropic.New()
	if err != nil {
		return err
	}
	canon := &cluster.Canonicalizer{
		Client: client,
		Model:  cfg.Models.Cheap,
		Log:    log.Printf,
	}

	p := &cluster.Pipeline{
		Embedder:        emb,
		EmbeddingModel:  cfg.Models.Embedding,
		Cache:           cache,
		CacheSavePath:   filepath.Join(stateDir, "embeddings.json"),
		ClustersPath:    filepath.Join(stateDir, "clusters.json"),
		CosineThreshold: float32(cfg.Thresholds.ClusterCosineThreshold),
		Canonicalizer:   canon,
		Workers:         cfg.Batching.ExtractWorkers,
		Log:             log.Printf,
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

// runSynthesize is implemented in Task 11. Stub for dispatch.
func runSynthesize(ctx context.Context) error {
	return fmt.Errorf("synthesize: not implemented yet")
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
