// Package redact masks personal data before it is written to logs.
package redact

import "strings"

// Phone keeps only the last 3 characters of a phone number
// ("+237612345678" → "**********678"). Phone numbers are personal data and
// must not appear in clear in logs.
func Phone(p string) string {
	if len(p) <= 3 {
		return "***"
	}
	return strings.Repeat("*", len(p)-3) + p[len(p)-3:]
}
