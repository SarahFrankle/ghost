package cmd

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/SarahFrankle/ghost/internal/ledger"
	"github.com/SarahFrankle/ghost/internal/paths"
	"github.com/SarahFrankle/ghost/internal/transcript"
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
		transcripts, err := transcript.Discover(glob, 5*time.Minute, time.Now())
		if err != nil {
			return err
		}

		var processed, pending, dirty int
		for _, t := range transcripts {
			h, err := transcript.ContentHash(t.Path)
			if err != nil {
				continue
			}
			entry, ok := l.Conversations[t.Path]
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
			len(transcripts), processed, pending, dirty)
		if l.LastCompose != nil {
			fmt.Printf("last compose: %s (stages: %v)\n", l.LastCompose.At.Format(time.RFC3339), l.LastCompose.StagesRun)
		} else {
			fmt.Println("last compose: never")
		}
		return nil
	},
}

func init() { rootCmd.AddCommand(statusCmd) }
