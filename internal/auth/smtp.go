package auth

import (
	"fmt"
	"net/smtp"
)

// SMTPMailer is a tiny stdlib SMTP wrapper. Configured via env in main.go;
// pass nil for dev-mode (auth.IssueToken will log links to stdout).
type SMTPMailer struct {
	Host string
	Port string
	User string
	Pass string
	From string
}

func (m *SMTPMailer) Send(to, subject, body string) error {
	addr := m.Host + ":" + m.Port
	auth := smtp.PlainAuth("", m.User, m.Pass, m.Host)
	msg := []byte(fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\n\r\n%s",
		m.From, to, subject, body))
	return smtp.SendMail(addr, auth, m.From, []string{to}, msg)
}
