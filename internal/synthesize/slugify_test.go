package synthesize

import "testing"

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
		"this-title-is-far-too-long-to-be-a-reasonable-slug-for-a-topic-file",
	}
	for _, in := range cases {
		if _, err := Slug(in); err == nil {
			t.Fatalf("Slug(%q) should have rejected", in)
		}
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
