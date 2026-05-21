package embedding

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestObservationHashIsStable(t *testing.T) {
	a := ObservationHash("rule", "", "prefer integration tests")
	b := ObservationHash("rule", "", "prefer integration tests")
	if a != b {
		t.Fatalf("hash unstable: %s vs %s", a, b)
	}
	if len(a) != 64 {
		t.Fatalf("hash length = %d, want 64 hex chars", len(a))
	}
	if ObservationHash("rule", "", "x") == ObservationHash("identity", "", "x") {
		t.Fatalf("kind not part of hash")
	}
	if ObservationHash("voice", "cli-chat", "x") == ObservationHash("voice", "slack", "x") {
		t.Fatalf("sub_key not part of hash")
	}
}

func TestCosine(t *testing.T) {
	got := Cosine([]float32{1, 0}, []float32{1, 0})
	if math.Abs(float64(got)-1.0) > 1e-6 {
		t.Fatalf("identical vectors cosine = %v, want 1", got)
	}
	got = Cosine([]float32{1, 0}, []float32{0, 1})
	if math.Abs(float64(got)) > 1e-6 {
		t.Fatalf("orthogonal cosine = %v, want 0", got)
	}
	if Cosine([]float32{1, 0}, []float32{0, 0}) != 0 {
		t.Fatalf("zero-vector cosine must be 0, not NaN")
	}
}

func TestVoyageEmbedHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("missing/wrong auth header: %q", r.Header.Get("Authorization"))
		}
		var body struct {
			Model string   `json:"model"`
			Input []string `json:"input"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Model != "voyage-3-lite" {
			t.Errorf("model = %q", body.Model)
		}
		if len(body.Input) != 2 {
			t.Errorf("expected 2 inputs, got %d", len(body.Input))
		}
		_, _ = w.Write([]byte(`{"data":[{"embedding":[0.1,0.2],"index":0},{"embedding":[0.3,0.4],"index":1}]}`))
	}))
	defer srv.Close()

	v := &Voyage{APIKey: "test-key", BaseURL: srv.URL, HTTPClient: srv.Client()}
	vecs, err := v.Embed(context.Background(), "voyage-3-lite", []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if len(vecs) != 2 || vecs[0][0] != 0.1 || vecs[1][1] != 0.4 {
		t.Fatalf("unexpected vectors: %v", vecs)
	}
}

func TestVoyageReturnsErrorOn4xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	defer srv.Close()
	v := &Voyage{APIKey: "x", BaseURL: srv.URL, HTTPClient: srv.Client()}
	_, err := v.Embed(context.Background(), "voyage-3-lite", []string{"a"})
	if err == nil {
		t.Fatal("expected error on 401")
	}
}

func TestOllamaEmbedHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embed" {
			t.Errorf("path = %q, want /api/embed", r.URL.Path)
		}
		var body struct {
			Model string   `json:"model"`
			Input []string `json:"input"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Model != "nomic-embed-text" {
			t.Errorf("model = %q", body.Model)
		}
		if len(body.Input) != 2 {
			t.Errorf("expected 2 inputs, got %d", len(body.Input))
		}
		_, _ = w.Write([]byte(`{"embeddings":[[0.1,0.2],[0.3,0.4]]}`))
	}))
	defer srv.Close()

	o := &Ollama{BaseURL: srv.URL, HTTPClient: srv.Client()}
	vecs, err := o.Embed(context.Background(), "nomic-embed-text", []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if len(vecs) != 2 || vecs[0][0] != 0.1 || vecs[1][1] != 0.4 {
		t.Fatalf("unexpected vectors: %v", vecs)
	}
}

func TestOllamaErrorsOnVectorCountMismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"embeddings":[[0.1]]}`))
	}))
	defer srv.Close()
	o := &Ollama{BaseURL: srv.URL, HTTPClient: srv.Client()}
	_, err := o.Embed(context.Background(), "nomic-embed-text", []string{"a", "b"})
	if err == nil {
		t.Fatal("expected error when vector count != input count")
	}
}
