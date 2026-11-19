package middleware

import (
	"strings"
	"testing"
)

func TestSanitizeQueryRedactsSecrets(t *testing.T) {
	got := sanitizeQuery("token=abc&txSecret=def&invite=ghi&limit=20&password=pw")
	for _, secret := range []string{"abc", "def", "ghi", "pw"} {
		if strings.Contains(got, secret) {
			t.Fatalf("sanitized query still contains %q: %s", secret, got)
		}
	}
	if !strings.Contains(got, "limit=20") {
		t.Fatalf("non-sensitive query was removed: %s", got)
	}
	if !strings.Contains(got, "redacted") {
		t.Fatalf("expected redaction marker: %s", got)
	}
}

func TestSanitizeQueryInvalidInput(t *testing.T) {
	if got := sanitizeQuery("%zz"); got != "redacted" {
		t.Fatalf("got = %q", got)
	}
}
