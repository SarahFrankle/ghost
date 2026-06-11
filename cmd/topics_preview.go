package cmd

import (
	"fmt"
	"log"
	"path/filepath"
	"sort"

	"github.com/spf13/cobra"

	"github.com/SarahFrankle/ghost/internal/anthropic"
	"github.com/SarahFrankle/ghost/internal/cluster"
	"github.com/SarahFrankle/ghost/internal/paths"
	"github.com/SarahFrankle/ghost/prompts"
)

// topicsPreviewCmd runs label -> theme -> group on the current observation set
// and prints the resulting themes for eyeball review. It does NOT write
// clusters.json. It DOES populate labels.json / themes.json so a later
// `ghost cluster` reuses the cached labels. This is the validation gate for
// the topic-clustering redesign.
var topicsPreviewCmd = &cobra.Command{
	Use:   "topics-preview",
	Short: "Preview label->theme->group topic clusters without writing clusters.json",
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
		var topics []cluster.ClusterMember
		for _, m := range all {
			if m.Kind == "preference" {
				topics = append(topics, m)
			}
		}
		if len(topics) == 0 {
			fmt.Println("no preference observations found")
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
			ThemeIdentify:           cluster.NewThemeIdentifyFunc(client, themeModel),
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
			Progress:                stderrCounter("cluster: topics: completed"),
		}

		clusters, err := g.Run(cmd.Context(), topics)
		if err != nil {
			return err
		}

		sort.Slice(clusters, func(i, j int) bool {
			if clusters[i].EvidenceCount != clusters[j].EvidenceCount {
				return clusters[i].EvidenceCount > clusters[j].EvidenceCount
			}
			return clusters[i].Canonical < clusters[j].Canonical
		})
		fmt.Printf("\n%d topic observation(s) -> %d theme(s):\n\n", len(topics), len(clusters))
		for _, c := range clusters {
			fmt.Printf("## %s  (%d obs, %d project(s))\n", c.Canonical, c.EvidenceCount, c.ProjectCount)
			for i, m := range c.Members {
				if i >= 3 {
					fmt.Printf("    … and %d more\n", len(c.Members)-3)
					break
				}
				fmt.Printf("    - %s\n", truncate(m.Text, 90))
			}
			fmt.Println()
		}
		return nil
	},
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func init() {
	rootCmd.AddCommand(topicsPreviewCmd)
}
