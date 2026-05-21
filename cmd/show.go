package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/spf13/cobra"

	"github.com/SarahFrankle/ghost/internal/extract"
	"github.com/SarahFrankle/ghost/internal/ledger"
	"github.com/SarahFrankle/ghost/internal/paths"
)

var showRecent int

var showCmd = &cobra.Command{
	Use:   "show",
	Short: "Show ghost outputs",
}

var showObservationsCmd = &cobra.Command{
	Use:   "observations",
	Short: "Print recently extracted observations",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		outDir, _ := paths.Expand(cfg.Paths.OutputDir)
		l, err := ledger.Load(filepath.Join(outDir, ".state", "ledger.json"))
		if err != nil {
			return err
		}

		type row struct {
			path  string
			entry ledger.Entry
		}
		rows := make([]row, 0, len(l.Conversations))
		for p, e := range l.Conversations {
			rows = append(rows, row{p, e})
		}
		sort.Slice(rows, func(i, j int) bool {
			return rows[i].entry.ProcessedAt.After(rows[j].entry.ProcessedAt)
		})
		if showRecent > 0 && len(rows) > showRecent {
			rows = rows[:showRecent]
		}

		for _, r := range rows {
			full := filepath.Join(outDir, r.entry.ObservationsFile)
			body, err := os.ReadFile(full)
			if err != nil {
				fmt.Fprintf(os.Stderr, "skip %s: %v\n", full, err)
				continue
			}
			var f extract.ObservationsFile
			if err := json.Unmarshal(body, &f); err != nil {
				fmt.Fprintf(os.Stderr, "parse %s: %v\n", full, err)
				continue
			}
			fmt.Printf("\n=== %s (%s) — %d obs, processed %s\n",
				filepath.Base(r.path), f.Project, len(f.Observations),
				r.entry.ProcessedAt.Format(time.RFC3339))
			for _, o := range f.Observations {
				sub := o.Kind
				if o.Topic != "" {
					sub = o.Kind + ":" + o.Topic
				} else if o.Context != "" {
					sub = o.Kind + ":" + o.Context
				}
				fmt.Printf("  [%s] %s\n      ← %s\n", sub, o.Text, o.Evidence)
			}
		}
		return nil
	},
}

func init() {
	showObservationsCmd.Flags().IntVar(&showRecent, "recent", 5, "show observations from N most recent transcripts (0 = all)")
	showCmd.AddCommand(showObservationsCmd)
	rootCmd.AddCommand(showCmd)
}
