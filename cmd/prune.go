package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/spf13/cobra"

	"github.com/SarahFrankle/ghost/internal/extract"
	"github.com/SarahFrankle/ghost/internal/ledger"
	"github.com/SarahFrankle/ghost/internal/paths"
	"github.com/SarahFrankle/ghost/internal/transcript"
)

var pruneDry bool

var pruneCmd = &cobra.Command{
	Use:   "prune",
	Short: "Drop ledger entries and observation files for vanished or sidecar transcripts",
	Long: `prune removes state written for sources that should no longer be tracked:
transcripts that have been deleted, and per-session sidecar files (e.g.
ai-title) that earlier versions of discovery ingested by mistake. It is
idempotent — running it on a clean state is a no-op.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		outDir, err := paths.Expand(cfg.Paths.OutputDir)
		if err != nil {
			return err
		}
		entries, files, err := pruneState(outDir, pruneDry)
		if err != nil {
			return err
		}
		if pruneDry {
			fmt.Printf("would drop %d ledger entr(ies) and %d observation file(s)\n", entries, files)
			return nil
		}
		fmt.Printf("pruned %d ledger entr(ies) and %d observation file(s)\n", entries, files)
		if entries > 0 || files > 0 {
			fmt.Println("note: synthesis is now stale; rerun `ghost compose`.")
		}
		return nil
	},
}

func init() {
	pruneCmd.Flags().BoolVar(&pruneDry, "dry-run", false, "report what would be pruned and exit")
	rootCmd.AddCommand(pruneCmd)
}

// shouldPruneSource reports whether state recorded for a source path should be
// dropped: the source no longer exists, or it is a non-conversation sidecar
// file that should never have been discovered.
func shouldPruneSource(path string) bool {
	if _, err := os.Stat(path); err != nil {
		return true // source gone
	}
	return transcript.IsSidecar(path)
}

// pruneState removes ledger entries and observation files whose source should
// be pruned (see shouldPruneSource). It sweeps both the ledger (entries and
// their referenced observation files) and the observations directory (to catch
// orphan files with no ledger entry). With dryRun set, nothing is written and
// the returned counts reflect what would be removed.
func pruneState(outDir string, dryRun bool) (entries, files int, err error) {
	stateDir := filepath.Join(outDir, ".state")
	ledgerPath := filepath.Join(stateDir, "ledger.json")
	obsDir := filepath.Join(stateDir, "observations")

	l, err := ledger.Load(ledgerPath)
	if err != nil {
		return 0, 0, err
	}

	removedObs := map[string]bool{}

	// Sweep ledger entries in a stable order so dry-run and real runs agree.
	convPaths := make([]string, 0, len(l.Conversations))
	for p := range l.Conversations {
		convPaths = append(convPaths, p)
	}
	sort.Strings(convPaths)
	for _, p := range convPaths {
		if !shouldPruneSource(p) {
			continue
		}
		entry := l.Conversations[p]
		if entry.ObservationsFile != "" {
			obsPath := filepath.Join(outDir, entry.ObservationsFile)
			if removed, rerr := removeIfPresent(obsPath, dryRun); rerr != nil {
				return entries, files, rerr
			} else if removed {
				files++
				removedObs[obsPath] = true
			}
		}
		entries++
		if !dryRun {
			l.Forget(p)
		}
	}

	// Sweep observation files for orphans (no ledger entry) whose recorded
	// source should be pruned.
	matches, _ := filepath.Glob(filepath.Join(obsDir, "*.json"))
	sort.Strings(matches)
	for _, obsPath := range matches {
		if removedObs[obsPath] {
			continue
		}
		src, ok := observationSource(obsPath)
		if !ok || !shouldPruneSource(src) {
			continue
		}
		if removed, rerr := removeIfPresent(obsPath, dryRun); rerr != nil {
			return entries, files, rerr
		} else if removed {
			files++
		}
	}

	if !dryRun && entries > 0 {
		if err := l.Save(ledgerPath); err != nil {
			return entries, files, err
		}
	}
	return entries, files, nil
}

// observationSource reads the recorded source path from an observations file.
func observationSource(obsPath string) (string, bool) {
	b, err := os.ReadFile(obsPath)
	if err != nil {
		return "", false
	}
	var f extract.ObservationsFile
	if err := json.Unmarshal(b, &f); err != nil {
		return "", false
	}
	return f.Source, f.Source != ""
}

// removeIfPresent deletes path unless dryRun is set. It reports whether a file
// was (or would be) removed; a missing file is not an error and not counted.
func removeIfPresent(path string, dryRun bool) (bool, error) {
	if _, err := os.Stat(path); err != nil {
		return false, nil
	}
	if dryRun {
		return true, nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return false, err
	}
	return true, nil
}
