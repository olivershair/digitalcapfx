package redact

import "testing"

func TestPhone(t *testing.T) {
	if got := Phone("+237612345678"); got != "**********678" {
		t.Errorf("got %q", got)
	}
	if got := Phone("12"); got != "***" {
		t.Errorf("got %q", got)
	}
}
