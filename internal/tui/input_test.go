package tui

import "testing"

func TestSanitizeInputStripsControlAndNulRunes(t *testing.T) {
	cases := map[string]string{
		"https://example.com/repo.git": "https://example.com/repo.git",
		"a\x00b":                       "ab",
		"path\twith\r\ncontrol":        "pathwithcontrol",
		"C:\\Users\\me\\vault":         "C:\\Users\\me\\vault",
		"has space":                    "has space",
		"\x00\x01\x02":                 "",
	}
	for in, want := range cases {
		if got := sanitizeInput(in); got != want {
			t.Errorf("sanitizeInput(%q) = %q, want %q", in, got, want)
		}
	}
}
