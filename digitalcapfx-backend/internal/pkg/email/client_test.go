package email

import "testing"

func TestNewTrimsSenderAddress(t *testing.T) {
	client := New("smtp.example.com", 587, "DigitalFX", "  digitalcapfx@gmail.com  ", "user", "password")

	if client.from != "digitalcapfx@gmail.com" {
		t.Fatalf("sender address = %q, want trimmed address", client.from)
	}
}
