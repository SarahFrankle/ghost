package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/SarahFrankle/ghost/internal/atomicfs"
	"github.com/SarahFrankle/ghost/internal/extract"
	"github.com/SarahFrankle/ghost/internal/paths"
	"github.com/SarahFrankle/ghost/internal/seed"
)

const provisionalMarker = "<!-- ghost: pending until next compose -->"

var rememberKind string

var rememberCmd = &cobra.Command{
	Use:   "remember --kind <identity|preference> <text>",
	Short: "Record a user-authored fact that applies now and survives recompose",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		kind, err := parseKind(rememberKind)
		if err != nil {
			return err
		}
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
		if err := rememberFact(outDir, kind, args[0]); err != nil {
			return err
		}
		fmt.Printf("remembered (%s): %q\n", kind, strings.TrimSpace(args[0]))
		fmt.Println("applied now; will be re-placed on next `ghost compose`.")
		return nil
	},
}

func parseKind(s string) (extract.Kind, error) {
	switch extract.Kind(s) {
	case extract.KindIdentity:
		return extract.KindIdentity, nil
	case extract.KindPreference:
		return extract.KindPreference, nil
	default:
		return "", fmt.Errorf("invalid --kind %q (want identity or preference)", s)
	}
}

// rememberFact records the fact as a high-confidence seed observation and
// appends it to the matching live doc immediately (provisional, regenerated
// away on the next compose).
func rememberFact(outDir string, kind extract.Kind, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return fmt.Errorf("fact text required")
	}
	o := extract.Observation{
		Kind:       kind,
		Text:       text,
		Evidence:   "stated directly by user (ghost remember, " + time.Now().Format("2006-01-02") + ")",
		Confidence: extract.ConfidenceHigh,
	}
	if err := seed.AppendSeedObservation(seed.SeedObservationsPath(outDir), o); err != nil {
		return err
	}
	return appendProvisional(outDir, kind, text)
}

// appendProvisional appends a provisional bullet to the live doc matching kind:
// identity.md for identity, rules.md for preference. Thrown away on next compose.
func appendProvisional(outDir string, kind extract.Kind, text string) error {
	name := "rules.md"
	if kind == extract.KindIdentity {
		name = "identity.md"
	}
	path := filepath.Join(outDir, name)
	body, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if len(body) > 0 && !strings.HasSuffix(string(body), "\n") {
		body = append(body, '\n')
	}
	addition := "\n" + provisionalMarker + "\n- " + text + "\n"
	body = append(body, []byte(addition)...)
	return atomicfs.WriteFile(path, body, 0o644)
}

func init() {
	rememberCmd.Flags().StringVar(&rememberKind, "kind", "", "fact kind: identity or preference (required)")
	rootCmd.AddCommand(rememberCmd)
}
