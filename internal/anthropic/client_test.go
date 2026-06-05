package anthropic

import (
	"io"
	"os"
	"testing"
)

func TestWriteTempStdin(t *testing.T) {
	const payload = "CLUSTER:\nsome multi-line\npayload\n"
	f, err := writeTempStdin(payload)
	if err != nil {
		t.Fatalf("writeTempStdin: %v", err)
	}
	defer func() {
		_ = f.Close()
		_ = os.Remove(f.Name())
	}()

	// The file must be rewound to the start so the child reads from byte 0.
	got, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != payload {
		t.Fatalf("payload = %q, want %q", got, payload)
	}
}

func TestWriteTempStdinEmpty(t *testing.T) {
	// An empty payload still yields a readable, rewound file (EOF at byte 0),
	// not a nil handle — the caller hands a valid fd to the child either way.
	f, err := writeTempStdin("")
	if err != nil {
		t.Fatalf("writeTempStdin: %v", err)
	}
	defer func() {
		_ = f.Close()
		_ = os.Remove(f.Name())
	}()
	got, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("payload = %q, want empty", got)
	}
}
