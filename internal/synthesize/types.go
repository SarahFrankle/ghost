package synthesize

// FileResult is the per-output-file outcome of a synthesis run.
type FileResult struct {
	Name    string // e.g. "identity.md"
	Content string
	Err     error
}
