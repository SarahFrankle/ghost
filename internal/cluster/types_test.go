package cluster

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestClusterMember_ConversationIDJSONTag(t *testing.T) {
	m := ClusterMember{ConversationID: "conv-123"}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"source":"conv-123"`) {
		t.Fatalf("ConversationID must serialize under json tag \"source\" for clusters.json compatibility; got %s", b)
	}
}
