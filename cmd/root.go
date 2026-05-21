package cmd

import (
	"github.com/spf13/cobra"
)

var (
	verbose    bool
	configPath string
)

var rootCmd = &cobra.Command{
	Use:   "ghost",
	Short: "Synthesize identity, rules, topics, and voice from Claude Code transcripts",
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "verbose output")
	rootCmd.PersistentFlags().StringVar(&configPath, "config", "", "path to config.toml (default ~/.ghost/config.toml)")
}
