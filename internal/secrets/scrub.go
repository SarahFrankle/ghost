package secrets

import "regexp"

type pattern struct {
	name string
	re   *regexp.Regexp
}

var patterns = []pattern{
	{"anthropic_key", regexp.MustCompile(`sk-ant-[A-Za-z0-9_\-]{20,}`)},
	{"openai_key", regexp.MustCompile(`sk-[A-Za-z0-9]{20,}`)},
	{"github_token", regexp.MustCompile(`gh[pous]_[A-Za-z0-9]{30,}`)},
	{"aws_access_key", regexp.MustCompile(`\b(?:AKIA|ASIA)[A-Z0-9]{16}\b`)},
	{"jwt", regexp.MustCompile(`eyJ[A-Za-z0-9_\-]{10,}\.[A-Za-z0-9_\-]{10,}\.[A-Za-z0-9_\-]{3,}`)},
	{"bearer_header", regexp.MustCompile(`(?i)Authorization:\s*Bearer\s+[A-Za-z0-9._\-]{8,}`)},
	{"pem_block", regexp.MustCompile(`-----BEGIN [A-Z ]+ KEY-----`)},
	{"long_hex", regexp.MustCompile(`\b[0-9a-f]{48,}\b`)},
	{"long_base64", regexp.MustCompile(`\b[A-Za-z0-9+/]{48,}={0,2}\b`)},
}

// Detect returns (true, patternName) if any pattern matches.
func Detect(s string) (bool, string) {
	for _, p := range patterns {
		if p.re.MatchString(s) {
			return true, p.name
		}
	}
	return false, ""
}
