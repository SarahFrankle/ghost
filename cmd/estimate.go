package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/SarahFrankle/ghost/internal/cluster"
	"github.com/SarahFrankle/ghost/internal/config"
	"github.com/SarahFrankle/ghost/internal/ledger"
	"github.com/SarahFrankle/ghost/internal/paths"
	"github.com/SarahFrankle/ghost/internal/pricing"
	"github.com/SarahFrankle/ghost/internal/transcript"
)

// runEstimate prints a per-stage token + cost estimate for the
// selected stages without calling any LLM. Output costs are
// approximated at 1/5 of input tokens; the flag is for sanity
// checking, not billing.
func runEstimate(ctx context.Context, cfg config.Config, stages []string) error {
	outDir, _ := paths.Expand(cfg.Paths.OutputDir)
	stateDir := filepath.Join(outDir, ".state")

	for _, s := range stages {
		switch s {
		case "extract":
			if err := estimateExtract(cfg, outDir, stateDir); err != nil {
				return err
			}
		case "cluster":
			if err := estimateCluster(cfg, stateDir); err != nil {
				return err
			}
		case "synthesize":
			if err := estimateSynthesize(cfg, stateDir); err != nil {
				return err
			}
		}
	}
	return nil
}

func estimateExtract(cfg config.Config, outDir, stateDir string) error {
	glob, _ := paths.Expand(cfg.Paths.TranscriptsGlob)
	ts, err := transcript.Discover(glob, 0, nowFn())
	if err != nil {
		return err
	}
	l, _ := ledger.Load(filepath.Join(stateDir, "ledger.json"))

	var bytes int64
	pending := 0
	for _, t := range ts {
		h, err := transcript.ContentHash(t.Path)
		if err != nil {
			continue
		}
		if !l.NeedsProcessing(t.Path, h) {
			continue
		}
		info, err := os.Stat(t.Path)
		if err != nil {
			continue
		}
		bytes += info.Size()
		pending++
	}
	report("extract", cfg.Models.Cheap, int(bytes), pending)
	return nil
}

func estimateCluster(cfg config.Config, stateDir string) error {
	obsDir := filepath.Join(stateDir, "observations")
	entries, err := os.ReadDir(obsDir)
	if err != nil {
		return nil
	}
	var bytes int64
	files := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		bytes += info.Size()
		files++
	}
	report("cluster (canonical phrasing)", cfg.Models.Cheap, int(bytes), files)
	return nil
}

func estimateSynthesize(cfg config.Config, stateDir string) error {
	clustersPath := filepath.Join(stateDir, "clusters.json")
	body, err := os.ReadFile(clustersPath)
	if err != nil {
		fmt.Println("synthesize: clusters.json missing. Run cluster first.")
		return nil
	}
	var cf cluster.ClustersFile
	if err := json.Unmarshal(body, &cf); err != nil {
		return err
	}
	report("synthesize", cfg.Models.Smart, len(body), len(cf.Clusters))
	return nil
}

func report(stage, model string, inputBytes, units int) {
	tokens := pricing.EstimateTokens(inputBytes)
	p, ok := pricing.Lookup(model)
	if !ok {
		fmt.Printf("%s: model=%s units=%d input~%d tokens (no pricing entry)\n", stage, model, units, tokens)
		return
	}
	inUSD := float64(tokens) / 1_000_000.0 * p.InputPerMTok
	outTokens := tokens / 5
	outUSD := float64(outTokens) / 1_000_000.0 * p.OutputPerMTok
	fmt.Printf("%s: model=%s units=%d input~%d tok ($%.4f) output~%d tok ($%.4f) total~$%.4f\n",
		stage, model, units, tokens, inUSD, outTokens, outUSD, inUSD+outUSD)
}

// nowFn is overridable in tests.
var nowFn = func() time.Time { return time.Now() }
