package email

import (
	"bytes"
	"io"
	"mime"
	"mime/quotedprintable"
	"net/mail"
	"strings"
	"testing"
	"time"
)

func TestNewTrimsSenderAddress(t *testing.T) {
	client := New("smtp.example.com", 587, "DigitalFX", "  digitalcapfx@gmail.com  ", "user", "password")

	if client.from != "digitalcapfx@gmail.com" {
		t.Fatalf("sender address = %q, want trimmed address", client.from)
	}
}

func TestBuildRaw_HeadersAndBody(t *testing.T) {
	long := strings.Repeat("<td>x</td>", 300) // one 3000-char line
	raw, err := buildRaw("DigitalFX", "noreply@digitalcapfx.com", "client@example.com",
		"Dossier KYC : Éloïse ✓\r\nBcc: attacker@evil.example", "<p>Bonjour Éloïse</p>"+long, time.Unix(0, 0))
	if err != nil {
		t.Fatal(err)
	}
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("not a valid RFC 5322 message: %v", err)
	}
	if msg.Header.Get("Bcc") != "" {
		t.Fatal("header injection: a Bcc header was created from the subject")
	}
	subj, err := new(mime.WordDecoder).DecodeHeader(msg.Header.Get("Subject"))
	if err != nil || !strings.Contains(subj, "Éloïse ✓") {
		t.Errorf("subject not decoded correctly: %q (%v)", subj, err)
	}
	for _, h := range []string{"Date", "Message-ID", "From", "To"} {
		if msg.Header.Get(h) == "" {
			t.Errorf("missing %s header", h)
		}
	}
	if msg.Header.Get("Content-Transfer-Encoding") != "quoted-printable" {
		t.Error("body must be quoted-printable")
	}
	for _, line := range strings.Split(string(raw), "\r\n") {
		if len(line) > 998 {
			t.Fatalf("line of %d chars exceeds the SMTP limit", len(line))
		}
	}
	body, _ := io.ReadAll(quotedprintable.NewReader(msg.Body))
	if !strings.Contains(string(body), "Bonjour Éloïse") {
		t.Error("body not decoded correctly")
	}
}

func TestSend_RejectsInvalidRecipient(t *testing.T) {
	c := New("127.0.0.1", 1, "DigitalFX", "noreply@digitalcapfx.com", "u", "p")
	if err := c.Send("not-an-email\r\nRCPT TO:<x@y>", "s", "b"); err == nil || !strings.Contains(err.Error(), "invalid recipient") {
		t.Fatalf("got %v", err)
	}
}
