package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/SarahFrankle/ghost/internal/cluster"
	"github.com/SarahFrankle/ghost/internal/ledger"
	"github.com/SarahFrankle/ghost/internal/paths"
	"github.com/SarahFrankle/ghost/internal/source"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show ledger summary: total / processed / pending / dirty",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		outDir, _ := paths.Expand(cfg.Paths.OutputDir)
		ledgerPath := filepath.Join(outDir, ".state", "ledger.json")
		l, err := ledger.Load(ledgerPath)
		if err != nil {
			return err
		}

		glob, _ := paths.Expand(cfg.Paths.TranscriptsGlob)
		src := source.ClaudeCode(glob)
		ctx := context.Background()
		convs, err := src.Discover(ctx, 5*time.Minute, time.Now())
		if err != nil {
			return err
		}

		var processed, pending, dirty int
		for _, c := range convs {
			h, err := src.ContentHash(ctx, c)
			if err != nil {
				continue
			}
			entry, ok := l.Conversations[c.ID]
			switch {
			case !ok:
				pending++
			case entry.ContentHash != h:
				dirty++
			default:
				processed++
			}
		}

		fmt.Printf("transcripts: total=%d  processed=%d  pending=%d  dirty=%d\n",
			len(convs), processed, pending, dirty)
		if l.LastCompose != nil {
			fmt.Printf("last compose: %s (stages: %v)\n", l.LastCompose.At.Format(time.RFC3339), l.LastCompose.StagesRun)
		} else {
			fmt.Println("last compose: never")
		}
		stateDir := filepath.Join(outDir, ".state")
		clustersPath := filepath.Join(stateDir, "clusters.json")
		if info, err := os.Stat(clustersPath); err == nil {
			cf, err := cluster.LoadClusters(clustersPath)
			if err == nil {
				fmt.Printf("clusters: %d (built %s, embedding=%s)\n", len(cf.Clusters), info.ModTime().Format(time.RFC3339), cf.EmbeddingModelID)
			} else {
				fmt.Printf("clusters: present but unreadable: %v\n", err)
			}
		} else {
			fmt.Println("clusters: none (run: ghost cluster)")
		}

		for _, name := range []string{"identity.md", "rules.md"} {
			p := filepath.Join(outDir, name)
			if info, err := os.Stat(p); err == nil {
				fmt.Printf("%s: present (%d bytes, %s)\n", name, info.Size(), info.ModTime().Format(time.RFC3339))
			} else {
				fmt.Printf("%s: missing (run: ghost synthesize)\n", name)
			}
		}

		return nil
	},
}

func init() { rootCmd.AddCommand(statusCmd) }
