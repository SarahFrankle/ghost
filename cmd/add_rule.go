package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/SarahFrankle/ghost/internal/paths"
)

var addRuleCmd = &cobra.Command{
	Use:   "add-rule <text>",
	Short: "Append a user-authored rule to ~/.ghost/rules.user.md",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		outDir, err := paths.Expand(cfg.Paths.OutputDir)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(outDir, 0o755); err != nil {
			return err
		}
		if err := appendUserRule(outDir, args[0]); err != nil {
			return err
		}
		fmt.Printf("appended to %s\n", filepath.Join(outDir, "rules.user.md"))
		return nil
	},
}

func appendUserRule(ghostDir, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return errors.New("rule text required")
	}
	path := filepath.Join(ghostDir, "rules.user.md")
	body, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if len(body) == 0 {
		body = []byte("# Rules (user-authored)\n\nThese rules override anything in rules.md and survive recompose.\n\n")
	} else if !strings.HasSuffix(string(body), "\n") {
		body = append(body, '\n')
	}
	body = append(body, []byte("- "+text+"\n")...)
	return os.WriteFile(path, body, 0o644)
}

func init() { rootCmd.AddCommand(addRuleCmd) }
