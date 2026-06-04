package cmd

import (
	"github.com/spf13/cobra"
)

var configPath string

var rootCmd = &cobra.Command{
	Use:   "ghost",
	Short: "Synthesize identity, rules, and topics from Claude Code transcripts",
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().StringVar(&configPath, "config", "", "path to config.toml (default ~/.ghost/config.toml)")
}
