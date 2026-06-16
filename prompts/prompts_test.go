package prompts

import "testing"

func TestSynthesizeGeneralitySystem_NonEmpty(t *testing.T) {
	if SynthesizeGeneralitySystem() == "" {
		t.Fatal("generality prompt must be embedded")
	}
	if SynthesizeGeneralitySystemHash() == "" {
		t.Fatal("generality prompt hash must be non-empty")
	}
}

func TestClusterPromptsNonEmpty(t *testing.T) {
	if ClusterLabelSystem() == "" || ClusterLabelSystemHash() == "" {
		t.Fatal("label prompt or hash empty")
	}
	if ClusterThemeIdentifySystem() == "" || ClusterThemeIdentifySystemHash() == "" {
		t.Fatal("theme identify prompt or hash empty")
	}
	if ClusterThemeMapSystem() == "" || ClusterThemeMapSystemHash() == "" {
		t.Fatal("theme map prompt or hash empty")
	}
	if EffectivenessJudgeSystem() == "" || EffectivenessJudgeSystemHash() == "" {
		t.Fatal("effectiveness judge prompt or hash empty")
	}
}
