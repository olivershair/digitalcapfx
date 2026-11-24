package email

import (
	"bytes"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

const (
	dialTimeout    = 15 * time.Second
	sessionTimeout = 45 * time.Second
)

// Client sends transactional email via Brevo (smtp-relay.brevo.com:587, STARTTLS).
type Client struct {
	host     string
	port     int
	fromName string
	from     string
	user     string // Brevo SMTP login (your Brevo account email)
	password string // Brevo SMTP key (not the master API key)
}

func New(host string, port int, fromName, from, user, password string) *Client {
	return &Client{
		host:     host,
		port:     port,
		fromName: fromName,
		from:     strings.TrimSpace(from),
		user:     user,
		password: password,
	}
}

// Send delivers an HTML email. Runs synchronously; call via goroutine for fire-and-forget.
func (c *Client) Send(to, subject, htmlBody string) error {
	addr := net.JoinHostPort(c.host, strconv.Itoa(c.port))

	// Validate the recipient before opening a connection.
	rcpt, err := mail.ParseAddress(oneLine(to))
	if err != nil {
		return fmt.Errorf("email: invalid recipient %q: %w", to, err)
	}

	// Timeouts: a stuck relay must never hang the sending goroutine forever.
	conn, err := net.DialTimeout("tcp", addr, dialTimeout)
	if err != nil {
		return fmt.Errorf("email: dial %s: %w", addr, err)
	}
	_ = conn.SetDeadline(time.Now().Add(sessionTimeout))

	cl, err := smtp.NewClient(conn, c.host)
	if err != nil {
		return fmt.Errorf("email: smtp client: %w", err)
	}
	defer cl.Close()

	if err := cl.StartTLS(&tls.Config{ServerName: c.host}); err != nil {
		return fmt.Errorf("email: starttls: %w", err)
	}

	if err := cl.Auth(smtp.PlainAuth("", c.user, c.password, c.host)); err != nil {
		return fmt.Errorf("email: auth: %w", err)
	}

	if err := cl.Mail(c.from); err != nil {
		return fmt.Errorf("email: MAIL FROM: %w", err)
	}
	if err := cl.Rcpt(rcpt.Address); err != nil {
		return fmt.Errorf("email: RCPT TO: %w", err)
	}

	w, err := cl.Data()
	if err != nil {
		return fmt.Errorf("email: DATA: %w", err)
	}
	msg, err := buildRaw(c.fromName, c.from, rcpt.Address, subject, htmlBody, time.Now())
	if err != nil {
		_ = w.Close()
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return fmt.Errorf("email: write body: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("email: close data: %w", err)
	}

	return cl.Quit()
}

// oneLine removes CR/LF so a value can never inject extra mail headers
// (subjects and names can contain customer-supplied data).
func oneLine(v string) string {
	v = strings.ReplaceAll(v, "\r", " ")
	v = strings.ReplaceAll(v, "\n", " ")
	return strings.TrimSpace(v)
}

// buildRaw assembles an RFC 5322 message:
//   - headers are single-line and RFC 2047-encoded when non-ASCII (accented
//     names, "✓"…), so they display correctly everywhere;
//   - Date and Message-ID are set (their absence is a classic spam signal);
//   - the HTML body is quoted-printable, which keeps every line under the SMTP
//     998-character limit (long template lines otherwise get cut or rejected).
func buildRaw(fromName, from, to, subject, body string, now time.Time) ([]byte, error) {
	fromAddr := mail.Address{Name: oneLine(fromName), Address: oneLine(from)}
	domain := "localhost"
	if at := strings.LastIndex(fromAddr.Address, "@"); at >= 0 && at < len(fromAddr.Address)-1 {
		domain = fromAddr.Address[at+1:]
	}
	idBytes := make([]byte, 12)
	_, _ = rand.Read(idBytes)

	var b bytes.Buffer
	b.WriteString("From: " + fromAddr.String() + "\r\n")
	b.WriteString("To: " + (&mail.Address{Address: oneLine(to)}).String() + "\r\n")
	b.WriteString("Subject: " + mime.QEncoding.Encode("utf-8", oneLine(subject)) + "\r\n")
	b.WriteString("Date: " + now.Format(time.RFC1123Z) + "\r\n")
	b.WriteString("Message-ID: <" + hex.EncodeToString(idBytes) + "." + strconv.FormatInt(now.UnixNano(), 36) + "@" + domain + ">\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
	b.WriteString("Content-Transfer-Encoding: quoted-printable\r\n")
	b.WriteString("\r\n")
	qp := quotedprintable.NewWriter(&b)
	if _, err := qp.Write([]byte(body)); err != nil {
		return nil, fmt.Errorf("email: encode body: %w", err)
	}
	if err := qp.Close(); err != nil {
		return nil, fmt.Errorf("email: encode body: %w", err)
	}
	return b.Bytes(), nil
}
