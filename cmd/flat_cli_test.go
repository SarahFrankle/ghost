package cmd

import "testing"

func TestStageVerbsRegistered(t *testing.T) {
	want := map[string]bool{"extract": false, "cluster": false, "synthesize": false}
	for _, c := range rootCmd.Commands() {
		if _, ok := want[c.Name()]; ok {
			want[c.Name()] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("command %q not registered on rootCmd", name)
		}
	}
}

func TestComposeHasNoStagesFlag(t *testing.T) {
	if composeCmd.Flags().Lookup("stages") != nil {
		t.Error("compose still has --stages flag; expected it removed")
	}
}
