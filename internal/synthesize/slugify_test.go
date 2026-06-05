package synthesize

import (
	"strings"
	"testing"
)

func TestSlugHappyPaths(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"Testing", "testing"},
		{"Error Handling", "error-handling"},
		{"  Documentation  ", "documentation"},
		{"Pull Requests & Reviews", "pull-requests-reviews"},
		{"API Design (REST)", "api-design-rest"},
		{"CI/CD", "ci-cd"},
		{"git: rebase before merge", "git-rebase-before-merge"},
	}
	for _, c := range cases {
		got, err := Slug(c.in)
		if err != nil {
			t.Fatalf("Slug(%q) returned error: %v", c.in, err)
		}
		if got != c.want {
			t.Fatalf("Slug(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSlugRejects(t *testing.T) {
	cases := []string{
		"",
		"   ",
		"12345",
		"!!!",
	}
	for _, in := range cases {
		if _, err := Slug(in); err == nil {
			t.Fatalf("Slug(%q) should have rejected", in)
		}
	}
}

func TestSlugTruncatesLongTitlesAtWordBoundary(t *testing.T) {
	// An over-length title no longer fails; it is truncated to the last
	// whole word that fits within maxSlugLen, with no trailing dash.
	in := "This Title Is Far Too Long To Be A Reasonable Slug For A Topic File"
	got, err := Slug(in)
	if err != nil {
		t.Fatalf("Slug(%q) returned error: %v", in, err)
	}
	const want = "this-title-is-far-too-long-to-be-a"
	if got != want {
		t.Fatalf("Slug(%q) = %q, want %q", in, got, want)
	}
	if len(got) > maxSlugLen {
		t.Fatalf("Slug(%q) = %q exceeds maxSlugLen %d", in, got, maxSlugLen)
	}
	if strings.HasSuffix(got, "-") || strings.HasPrefix(got, "-") {
		t.Fatalf("Slug(%q) = %q has a leading/trailing dash", in, got)
	}
}

func TestSlugTruncatesSingleGiantToken(t *testing.T) {
	// A single token longer than maxSlugLen has no word boundary to cut on,
	// so it is hard-cut to maxSlugLen rather than rejected.
	in := strings.Repeat("a", 50)
	got, err := Slug(in)
	if err != nil {
		t.Fatalf("Slug(%q) returned error: %v", in, err)
	}
	if want := strings.Repeat("a", maxSlugLen); got != want {
		t.Fatalf("Slug(50xa) = %q, want %q", got, want)
	}
}

func TestParseH1ExtractsTitle(t *testing.T) {
	body := "# Error Handling\n\nSome content here.\n"
	title, err := ParseH1(body)
	if err != nil {
		t.Fatalf("ParseH1 returned error: %v", err)
	}
	if title != "Error Handling" {
		t.Fatalf("got %q, want %q", title, "Error Handling")
	}
}

func TestParseH1RejectsMissingHeading(t *testing.T) {
	body := "No heading here.\n"
	if _, err := ParseH1(body); err == nil {
		t.Fatal("ParseH1 should have rejected body without H1 first line")
	}
}

func TestParseH1RejectsNonFirstLineHeading(t *testing.T) {
	body := "Some preamble.\n\n# Title\n"
	if _, err := ParseH1(body); err == nil {
		t.Fatal("ParseH1 should reject H1 not on the first non-empty line")
	}
}

func TestParseH1AllowsLeadingBlankLine(t *testing.T) {
	body := "\n# Title\n\nbody.\n"
	title, err := ParseH1(body)
	if err != nil {
		t.Fatalf("ParseH1 returned error: %v", err)
	}
	if title != "Title" {
		t.Fatalf("got %q, want %q", title, "Title")
	}
}
