package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/SarahFrankle/ghost/internal/anthropic"
	"github.com/SarahFrankle/ghost/internal/effectiveness"
	"github.com/SarahFrankle/ghost/internal/paths"
	"github.com/SarahFrankle/ghost/internal/source"
	"github.com/SarahFrankle/ghost/internal/transcript"
	"github.com/SarahFrankle/ghost/prompts"
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

	judgeModel := cfg.Models.Judge
	if judgeModel == "" {
		judgeModel = cfg.Models.Cheap
	}
	client, err := anthropic.New()
	if err != nil {
		return err
	}
	judge := effectiveness.NewJudge(client, judgeModel,
		prompts.EffectivenessJudgeSystem(), prompts.EffectivenessJudgeSystemHash(),
		filepath.Join(metricsDir, "judge-cache.json"))

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
		for idx := range kept {
			body := topicBody(topicsDir, kept[idx].TopicSlug)
			fit, reason, _ := judge.Judge(ctx, kept[idx], body)
			kept[idx].Fit = fit
			kept[idx].FitReason = reason
		}
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
	if err := judge.SaveCache(); err != nil {
		return err
	}
	fmt.Printf("appended %d new topic-read events to %s\n", total, jsonlPath)
	return nil
}

// topicBody reads a topic file's content, or "" if unreadable (e.g. the topic
// was renamed/removed since the read). An empty body still gets judged on slug.
func topicBody(topicsDir, slug string) string {
	b, err := os.ReadFile(filepath.Join(topicsDir, slug+".md"))
	if err != nil {
		return ""
	}
	return string(b)
}

var auditReportCmd = &cobra.Command{
	Use:   "report",
	Short: "Summarize topic-read purpose-fit from the metrics log",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		outDir, _ := paths.Expand(cfg.Paths.OutputDir)
		jsonlPath := filepath.Join(outDir, "metrics", "topic-reads.jsonl")
		evs, err := effectiveness.ReadEvents(jsonlPath)
		if err != nil {
			return err
		}
		if len(evs) == 0 {
			fmt.Println("no topic-read events yet — run `ghost audit`")
			return nil
		}
		fmt.Println("Note: a topic is Read once per session and stays in context;")
		fmt.Println("'right purpose' is judged against the load-time task only, so later")
		fmt.Println("same-session uses are undercounted.")
		fmt.Printf("\n%-28s %6s %8s %5s %5s %5s %5s\n", "TOPIC", "READS", "TRIG", "YES", "PART", "NO", "UNK")
		for _, s := range effectiveness.Summarize(evs) {
			fmt.Printf("%-28s %6d %8d %5d %5d %5d %5d\n",
				s.Slug, s.Reads, s.TriggerMatched, s.FitYes, s.FitPartial, s.FitNo, s.FitUnknown)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(auditCmd)
	auditCmd.AddCommand(auditReportCmd)
}
