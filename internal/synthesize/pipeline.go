package synthesize

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/SarahFrankle/ghost/internal/anthropic"
	"github.com/SarahFrankle/ghost/internal/cluster"
)

// Pipeline orchestrates stage 3 for the always-loaded core
// (identity.md, rules.md). Topics/voice/index are out of scope in
// Phase 2.
//
// Write strategy:
//  1. Create ~/.ghost/.tmp-synthesize-<ts>/.
//  2. Call each generator into the tmpdir.
//  3. If ANY generator returned an error, leave the tmpdir in place
//     (for inspection) and return a partial-failure error. The prior
//     generation's files in ~/.ghost/ remain authoritative.
//  4. If all generators succeeded, rename each file from the tmpdir
//     into ~/.ghost/ and remove the tmpdir.
//
// POSIX has no atomic multi-file dir-merge, so step 4 renames file-by-
// file. The order is identity.md first then rules.md so a crash mid-
// step leaves identity stale-but-consistent rather than rules stale-
// but-consistent. (rules.md is what changes behavior — better to have
// the old version one cycle longer than to have rules referencing an
// identity the model didn't see.)
type Pipeline struct {
	Client          anthropic.Client
	SmartModel      string
	GhostDir        string
	MinRuleEvidence int
	MinRuleProjects int
}

func (p *Pipeline) Run(ctx context.Context, cf cluster.ClustersFile) error {
	if p.GhostDir == "" {
		return errors.New("synthesize: GhostDir required")
	}
	if err := os.MkdirAll(p.GhostDir, 0o755); err != nil {
		return err
	}
	tmpDir, err := os.MkdirTemp(p.GhostDir, ".tmp-synthesize-"+time.Now().UTC().Format("20060102T150405")+"-")
	if err != nil {
		return err
	}

	identityClusters := pickKind(cf.Clusters, "identity")
	ruleClusters := FilterRules(cf.Clusters, p.MinRuleEvidence, p.MinRuleProjects)

	userRules := readUserRules(p.GhostDir)

	results := []FileResult{
		BuildIdentity(ctx, p.Client, p.SmartModel, identityClusters),
		BuildRules(ctx, p.Client, p.SmartModel, ruleClusters, userRules),
	}

	var failed []string
	for _, r := range results {
		if r.Err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", r.Name, r.Err))
			continue
		}
		if err := os.WriteFile(filepath.Join(tmpDir, r.Name), []byte(r.Content), 0o644); err != nil {
			failed = append(failed, fmt.Sprintf("%s: write: %v", r.Name, err))
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("synthesize partial failure (tmpdir preserved at %s): %s", tmpDir, strings.Join(failed, "; "))
	}

	for _, r := range results {
		src := filepath.Join(tmpDir, r.Name)
		dst := filepath.Join(p.GhostDir, r.Name)
		if err := os.Rename(src, dst); err != nil {
			return fmt.Errorf("rename %s: %w (tmpdir preserved at %s)", r.Name, err, tmpDir)
		}
	}
	_ = os.Remove(tmpDir)
	return nil
}

func pickKind(cs []cluster.Cluster, kind string) []cluster.Cluster {
	out := make([]cluster.Cluster, 0, len(cs))
	for _, c := range cs {
		if c.Kind == kind {
			out = append(out, c)
		}
	}
	return out
}

func readUserRules(ghostDir string) string {
	b, err := os.ReadFile(filepath.Join(ghostDir, "rules.user.md"))
	if err != nil {
		return "(rules.user.md does not exist; no user-authored rules)"
	}
	return string(b)
}
