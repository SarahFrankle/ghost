package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/SarahFrankle/ghost/internal/atomicfs"
	"github.com/SarahFrankle/ghost/internal/paths"
	"github.com/SarahFrankle/ghost/skill"
)

var installSkillCmd = &cobra.Command{
	Use:   "install-skill",
	Short: "Write SKILL.md to ~/.claude/skills/ghost/",
	RunE: func(cmd *cobra.Command, args []string) error {
		dir, err := paths.Expand(skill.DefaultInstallDir)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		target := filepath.Join(dir, "SKILL.md")
		if err := atomicfs.WriteFile(target, []byte(skill.Content()), 0o644); err != nil {
			return err
		}
		fmt.Printf("installed SKILL.md -> %s\n", target)
		return nil
	},
}

func init() { rootCmd.AddCommand(installSkillCmd) }
