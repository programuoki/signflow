// Package email defines an email Sender interface with two implementations: a
// console sender for development (prints the message + any links to the log, so
// registration, password reset, and signer invites all work with NO API key)
// and a Resend-backed sender for production.
package email

import (
	"context"
	"log/slog"
	"strings"
)

// Message is a single email to send.
type Message struct {
	To      string
	Subject string
	// HTML and Text are the two bodies. Text is what the console sender prints.
	HTML string
	Text string
}

// Sender delivers email. Implementations must be safe for concurrent use.
type Sender interface {
	Send(ctx context.Context, msg Message) error
}

// New picks an implementation from configuration. Anything other than "resend"
// (including the empty string) yields the console sender, so dev is zero-config.
func New(kind, apiKey, from string, log *slog.Logger) Sender {
	if strings.EqualFold(kind, "resend") {
		log.Info("email: using Resend sender", "from", from)
		return NewResendSender(apiKey, from)
	}
	log.Info("email: using console sender (links print to stdout, no email is sent)")
	return NewConsoleSender(nil)
}
