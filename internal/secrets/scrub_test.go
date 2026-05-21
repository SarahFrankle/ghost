package secrets

import "testing"

func TestDetectsCommonPatterns(t *testing.T) {
	samples := []string{
		"sk-ant-api03-abc123def456ghi789jkl012",
		"ghp_aBcDeFgHiJkLmNoPqRsTuVwXyZ0123456789",
		"AKIAIOSFODNN7EXAMPLE",
		"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NSJ9.abc",
		"Authorization: Bearer xyz123abc",
		"-----BEGIN RSA PRIVATE KEY-----",
	}
	for _, s := range samples {
		hit, pat := Detect(s)
		if !hit {
			t.Errorf("expected hit for %q (got pat=%q)", s, pat)
		}
	}
}

func TestDoesNotFlagNormalText(t *testing.T) {
	clean := []string{
		"the user prefers integration tests",
		"break comments at end of thought",
		"works at Miro on Content Security team",
	}
	for _, s := range clean {
		if hit, pat := Detect(s); hit {
			t.Errorf("false positive on %q (pat=%q)", s, pat)
		}
	}
}
