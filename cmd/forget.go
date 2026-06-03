package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/SarahFrankle/ghost/internal/ledger"
	"github.com/SarahFrankle/ghost/internal/paths"
)

var forgetCmd = &cobra.Command{
	Use:   "forget <transcript-path>",
	Short: "Drop a conversation's observations and ledger entry",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		target := args[0]
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

		entry, ok := l.Conversations[target]
		if !ok {
			return fmt.Errorf("not in ledger: %s", target)
		}
		obsPath := filepath.Join(outDir, entry.ObservationsFile)
		if err := os.Remove(obsPath); err != nil && !os.IsNotExist(err) {
			return err
		}
		l.Forget(target)
		if err := l.Save(ledgerPath); err != nil {
			return err
		}
		fmt.Printf("forgot %s\n", target)
		fmt.Println("note: synthesis is now stale; rerun `ghost compose`.")
		return nil
	},
}

func init() { rootCmd.AddCommand(forgetCmd) }
