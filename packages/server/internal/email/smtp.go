package email

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/smtp"
	"net/textproto"
	"strings"
	"time"
)

// SMTPSender sends emails via SMTP with STARTTLS.
// Universal fallback — works with any SMTP server (Postfix, SES SMTP, Gmail relay, etc.).
// Sender address is always set by the caller via Message.From (controlled by EMAIL_FROM env).
type SMTPSender struct {
	host     string // "smtp.example.com:587"
	username string
	password string
}

// NewSMTPSender creates an SMTP email sender.
// host must include port, e.g., "smtp.example.com:587".
func NewSMTPSender(host, username, password string) *SMTPSender {
	return &SMTPSender{
		host:     host,
		username: username,
		password: password,
	}
}

func (s *SMTPSender) Send(ctx context.Context, msg Message) (SendResult, error) {
	from := msg.From
	// Extract bare email from "Name <email>" format for SMTP envelope
	envelopeFrom := extractEmail(from)

	// Build MIME message
	body, err := buildMIMEMessage(from, msg)
	if err != nil {
		return SendResult{}, fmt.Errorf("smtp: build message: %w", err)
	}

	// Connect with timeout derived from context (or 15s default)
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(15 * time.Second)
	}

	hostname, _, _ := net.SplitHostPort(s.host)

	dialer := &net.Dialer{Deadline: deadline}
	conn, err := dialer.DialContext(ctx, "tcp", s.host)
	if err != nil {
		return SendResult{}, fmt.Errorf("smtp: dial %s: %w", s.host, err)
	}

	client, err := smtp.NewClient(conn, hostname)
	if err != nil {
		conn.Close()
		return SendResult{}, fmt.Errorf("smtp: new client: %w", err)
	}
	defer client.Close()

	// STARTTLS
	if ok, _ := client.Extension("STARTTLS"); ok {
		tlsConfig := &tls.Config{ServerName: hostname}
		if err := client.StartTLS(tlsConfig); err != nil {
			return SendResult{}, fmt.Errorf("smtp: starttls: %w", err)
		}
	}

	// Auth
	if s.username != "" {
		auth := smtp.PlainAuth("", s.username, s.password, hostname)
		if err := client.Auth(auth); err != nil {
			return SendResult{}, fmt.Errorf("smtp: auth: %w", err)
		}
	}

	// Envelope
	if err := client.Mail(envelopeFrom); err != nil {
		return SendResult{}, fmt.Errorf("smtp: mail from: %w", err)
	}
	for _, to := range msg.To {
		if err := client.Rcpt(extractEmail(to)); err != nil {
			return SendResult{}, fmt.Errorf("smtp: rcpt to %s: %w", to, err)
		}
	}

	// Data
	w, err := client.Data()
	if err != nil {
		return SendResult{}, fmt.Errorf("smtp: data: %w", err)
	}
	if _, err := w.Write(body); err != nil {
		return SendResult{}, fmt.Errorf("smtp: write: %w", err)
	}
	if err := w.Close(); err != nil {
		return SendResult{}, fmt.Errorf("smtp: close data: %w", err)
	}

	if err := client.Quit(); err != nil {
		// Non-fatal — message was already accepted at this point
	}

	// SMTP doesn't return a message ID synchronously
	return SendResult{MessageID: ""}, nil
}

// buildMIMEMessage constructs a multipart/alternative email with text + HTML parts.
func buildMIMEMessage(from string, msg Message) ([]byte, error) {
	var buf bytes.Buffer

	// Headers
	buf.WriteString("From: " + from + "\r\n")
	buf.WriteString("To: " + strings.Join(msg.To, ", ") + "\r\n")
	buf.WriteString("Subject: " + mime.QEncoding.Encode("utf-8", msg.Subject) + "\r\n")
	if msg.ReplyTo != "" {
		buf.WriteString("Reply-To: " + msg.ReplyTo + "\r\n")
	}
	buf.WriteString("MIME-Version: 1.0\r\n")
	buf.WriteString("Date: " + time.Now().UTC().Format("Mon, 02 Jan 2006 15:04:05 -0700") + "\r\n")

	if msg.HTML != "" && msg.Text != "" {
		// multipart/alternative
		writer := multipart.NewWriter(&buf)
		buf.WriteString("Content-Type: multipart/alternative; boundary=" + writer.Boundary() + "\r\n\r\n")

		// Text part — properly quoted-printable encoded
		textHeader := make(textproto.MIMEHeader)
		textHeader.Set("Content-Type", "text/plain; charset=utf-8")
		textHeader.Set("Content-Transfer-Encoding", "quoted-printable")
		textPart, _ := writer.CreatePart(textHeader)
		qpText := quotedprintable.NewWriter(textPart)
		qpText.Write([]byte(msg.Text))
		qpText.Close()

		// HTML part — properly quoted-printable encoded
		htmlHeader := make(textproto.MIMEHeader)
		htmlHeader.Set("Content-Type", "text/html; charset=utf-8")
		htmlHeader.Set("Content-Transfer-Encoding", "quoted-printable")
		htmlPart, _ := writer.CreatePart(htmlHeader)
		qpHTML := quotedprintable.NewWriter(htmlPart)
		qpHTML.Write([]byte(msg.HTML))
		qpHTML.Close()

		writer.Close()
	} else if msg.HTML != "" {
		buf.WriteString("Content-Type: text/html; charset=utf-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n")
		qp := quotedprintable.NewWriter(&buf)
		qp.Write([]byte(msg.HTML))
		qp.Close()
	} else {
		buf.WriteString("Content-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n")
		qp := quotedprintable.NewWriter(&buf)
		qp.Write([]byte(msg.Text))
		qp.Close()
	}

	return buf.Bytes(), nil
}

// extractEmail pulls "user@example.com" from "Name <user@example.com>" or returns as-is.
func extractEmail(addr string) string {
	if i := strings.Index(addr, "<"); i >= 0 {
		if j := strings.Index(addr[i:], ">"); j >= 0 {
			return addr[i+1 : i+j]
		}
	}
	return strings.TrimSpace(addr)
}
