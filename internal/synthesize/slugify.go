package synthesize

import (
	"bufio"
	"errors"
	"fmt"
	"strings"
	"unicode"
)

// maxSlugLen is the upper bound on slug length. A longer result is
// truncated to the last whole word that fits (see Slug) rather than
// rejected: one verbose title must never abort the whole topics rebuild.
const maxSlugLen = 40

// Slug turns a title string into a kebab-case filename slug. Deterministic.
//
// Rules:
//   - lowercase
//   - any run of non-[a-z0-9] characters collapses to a single '-'
//   - leading/trailing '-' trimmed
//   - if longer than maxSlugLen, truncate to the last whole word that
//     fits (no word boundary in range -> hard cut at maxSlugLen)
//   - reject if result is empty or contains no letter
func Slug(title string) (string, error) {
	var b strings.Builder
	prevDash := true // treat start of string as if it had a trailing dash, so leading garbage doesn't emit one
	for _, r := range strings.TrimSpace(title) {
		switch {
		case r >= 'A' && r <= 'Z':
			b.WriteRune(unicode.ToLower(r))
			prevDash = false
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	s := strings.TrimRight(b.String(), "-")

	if s == "" {
		return "", fmt.Errorf("slugify: empty result from %q", title)
	}
	if len(s) > maxSlugLen {
		cut := s[:maxSlugLen]
		if i := strings.LastIndexByte(cut, '-'); i > 0 {
			cut = cut[:i] // drop the partial trailing word
		}
		s = strings.TrimRight(cut, "-")
	}
	hasLetter := false
	for _, r := range s {
		if r >= 'a' && r <= 'z' {
			hasLetter = true
			break
		}
	}
	if !hasLetter {
		return "", fmt.Errorf("slugify: result %q has no letter", s)
	}
	return s, nil
}

// ParseH1 extracts the title from the first non-empty line of body, which
// must be of the form `# <Title>`. Whitespace before/after the heading
// text is trimmed. Returns an error if no such heading is the first
// non-empty line.
func ParseH1(body string) (string, error) {
	scanner := bufio.NewScanner(strings.NewReader(body))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "# ") {
			return "", errors.New("ParseH1: first non-empty line is not a level-1 heading")
		}
		title := strings.TrimSpace(strings.TrimPrefix(line, "# "))
		if title == "" {
			return "", errors.New("ParseH1: heading has no title text")
		}
		return title, nil
	}
	return "", errors.New("ParseH1: body is empty")
}
