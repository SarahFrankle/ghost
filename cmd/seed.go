package cmd

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/SarahFrankle/ghost/internal/anthropic"
	"github.com/SarahFrankle/ghost/internal/atomicfs"
	"github.com/SarahFrankle/ghost/internal/cluster"
	"github.com/SarahFrankle/ghost/internal/extract"
	"github.com/SarahFrankle/ghost/internal/paths"
	"github.com/SarahFrankle/ghost/prompts"
)

// seedCmd runs fine-grained Pass-1 discovery over the current preference
// observations and writes a candidate seed list for the user to curate.
// It never overwrites the committed seed-topics.yml.
var seedCmd = &cobra.Command{
	Use:   "seed",
	Short: "Generate a candidate seed-topics list from the current corpus",
	RunE: func(cmd *cobra.Command, args []string) error {
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

		all, err := cluster.LoadObservations(obsDir)
		if err != nil {
			return fmt.Errorf("load observations: %w", err)
		}
		var members []cluster.ClusterMember
		for _, m := range all {
			if m.Kind == extract.KindPreference {
				members = append(members, m)
			}
		}
		if len(members) == 0 {
			fmt.Println("no preference observations found; run `ghost extract` first")
			return nil
		}

		client, err := anthropic.New()
		if err != nil {
			return err
		}
		labelModel := cfg.Models.Label
		if labelModel == "" {
			labelModel = cfg.Models.Cheap
		}
		themeModel := cfg.Models.Theme
		if themeModel == "" {
			themeModel = cfg.Models.Smart
		}
		labelCache, err := cluster.LoadLabelCache(filepath.Join(stateDir, "labels.json"), labelModel, prompts.ClusterLabelSystemHash())
		if err != nil {
			return err
		}
		g := &cluster.TopicGrouper{
			Label:                   cluster.NewLabelFunc(client, labelModel),
			ThemeIdentify:           cluster.NewThemeIdentifyFunc(client, themeModel, nil),
			ThemeMap:                cluster.NewThemeMapFunc(client, themeModel),
			Cache:                   labelCache,
			CacheSavePath:           filepath.Join(stateDir, "labels.json"),
			ThemesPath:              filepath.Join(stateDir, "themes.json"),
			ThemeModel:              themeModel,
			ThemeIdentifyPromptHash: prompts.ClusterThemeIdentifySystemHash(),
			ThemeMapPromptHash:      prompts.ClusterThemeMapSystemHash(),
			MinClusterSize:          cfg.Thresholds.MinClusterSize,
			Workers:                 cfg.Batching.ExtractWorkers,
			Log:                     log.Printf,
			Progress:                stderrCounter("seed: topics: completed"),
		}
		clusters, err := g.Run(cmd.Context(), members)
		if err != nil {
			return err
		}

		names := make([]string, 0, len(clusters))
		for _, c := range clusters {
			names = append(names, c.Canonical)
		}
		body, err := yaml.Marshal(map[string][]string{"topics": names})
		if err != nil {
			return err
		}
		dst := filepath.Join(outDir, "seed-topics.candidate.yml")
		if _, err := os.Stat(filepath.Join(outDir, "seed-topics.yml")); err == nil {
			fmt.Println("note: seed-topics.yml exists and is NOT touched; curate the candidate by hand")
		}
		if err := atomicfs.WriteFile(dst, body, 0o644); err != nil {
			return err
		}
		fmt.Printf("seed: wrote %d candidate topic(s) to %s\n", len(names), dst)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(seedCmd)
}
