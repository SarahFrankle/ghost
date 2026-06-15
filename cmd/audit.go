package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/SarahFrankle/ghost/internal/effectiveness"
	"github.com/SarahFrankle/ghost/internal/paths"
	"github.com/SarahFrankle/ghost/internal/source"
	"github.com/SarahFrankle/ghost/internal/transcript"
)

var auditCmd = &cobra.Command{
	Use:   "audit",
	Short: "Scan transcripts for ghost topic reads and judge their purpose-fit",
	RunE:  runAudit,
}

func runAudit(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	outDir, err := paths.Expand(cfg.Paths.OutputDir)
	if err != nil {
		return err
	}
	topicsDir := filepath.Join(outDir, "topics")

	indexBytes, err := os.ReadFile(filepath.Join(outDir, "index.md"))
	if err != nil {
		return fmt.Errorf("read index.md (run `ghost compose` first): %w", err)
	}
	triggers := effectiveness.ParseIndexTriggers(string(indexBytes))

	metricsDir := filepath.Join(outDir, "metrics")
	jsonlPath := filepath.Join(metricsDir, "topic-reads.jsonl")
	ledgerPath := filepath.Join(outDir, ".state", "audit-ledger.json")
	led, err := effectiveness.LoadAuditLedger(ledgerPath)
	if err != nil {
		return err
	}

	glob, err := paths.Expand(cfg.Paths.TranscriptsGlob)
	if err != nil {
		return err
	}
	src := source.ClaudeCode(glob)
	ctx := context.Background()
	convs, err := src.Discover(ctx, 5*time.Minute, time.Now())
	if err != nil {
		return err
	}

	total := 0
	counter := stderrCounter("scanning")
	for i, c := range convs {
		if counter != nil {
			counter(i+1, len(convs))
		}
		evs, err := transcript.ParseEvents(c.ID)
		if err != nil {
			continue
		}
		reads, lines := effectiveness.DetectTopicReadsWithLines(c.ID, evs, topicsDir, triggers)
		kept, maxLine := effectiveness.NewEventsSince(reads, lines, led.ScannedLines(c.ID))
		if len(kept) > 0 {
			if err := effectiveness.AppendEvents(jsonlPath, kept); err != nil {
				return err
			}
			total += len(kept)
		}
		led.SetScannedLines(c.ID, maxLine)
		if err := led.Save(ledgerPath); err != nil {
			return err
		}
	}
	fmt.Printf("appended %d new topic-read events to %s\n", total, jsonlPath)
	return nil
}

func init() {
	rootCmd.AddCommand(auditCmd)
}
