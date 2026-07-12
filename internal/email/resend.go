package email

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// ResendSender delivers email via the Resend HTTP API (https://resend.com).
// We hit the REST endpoint directly rather than pull in an SDK — it's one POST.
type ResendSender struct {
	apiKey string
	from   string
	client *http.Client
}

func NewResendSender(apiKey, from string) *ResendSender {
	if from == "" {
		from = "SignFlow <onboarding@resend.dev>"
	}
	return &ResendSender{
		apiKey: apiKey,
		from:   from,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

func (s *ResendSender) Send(ctx context.Context, msg Message) error {
	payload := map[string]any{
		"from":    s.from,
		"to":      []string{msg.To},
		"subject": msg.Subject,
		"html":    msg.HTML,
		"text":    msg.Text,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.resend.com/emails", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("resend: unexpected status %d", resp.StatusCode)
	}
	return nil
}
