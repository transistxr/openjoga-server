package notify

import (
	"context"
	"fmt"
	"log/slog"
	"net/smtp"
	"strings"
)

type SMTPMailer struct {
	Host string
	Port int
	User string
	Pass string
	From string
}

func (m *SMTPMailer) Send(_ context.Context, to, subject, body string) error {
	msg := buildMessage(m.From, to, subject, body)
	addr := fmt.Sprintf("%s:%d", m.Host, m.Port)

	var auth smtp.Auth
	if m.User != "" {
		auth = smtp.PlainAuth("", m.User, m.Pass, m.Host)
	}
	return smtp.SendMail(addr, auth, m.From, []string{to}, msg)
}

type LogMailer struct{ Log *slog.Logger }

func (m *LogMailer) Send(_ context.Context, to, subject, body string) error {
	m.Log.Info("email (not sent: no SMTP configured)",
		"to", to, "subject", subject, "body", "\n"+body)
	return nil
}

func buildMessage(from, to, subject, body string) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", from)
	fmt.Fprintf(&b, "To: %s\r\n", to)
	fmt.Fprintf(&b, "Subject: %s\r\n", subject)
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	b.WriteString("\r\n")
	b.WriteString(strings.ReplaceAll(body, "\n", "\r\n"))
	return []byte(b.String())
}
