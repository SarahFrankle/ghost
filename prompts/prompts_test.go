package prompts

import "testing"

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
}
