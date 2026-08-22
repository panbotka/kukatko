package main

import (
	"fmt"

	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/mailer"
	"github.com/panbotka/kukatko/internal/mailjob"
)

// buildMailServiceOrNil assembles the mail-delivery service — the `mail_send` job
// handler that renders a queued message and hands it to the SMTP server — or nil
// when mail is switched off.
//
// A nil service registers no handler, which is exactly right for an instance with
// mail off: nothing enqueues a `mail_send` job there either (mailjob.Enqueuer
// refuses while disabled), so no job is left waiting for a claimant. Should one
// exist from a period when mail was on, it simply waits in the queue until mail
// is configured again — the queue's whole point.
func buildMailServiceOrNil(cfg *config.Config) (*mailjob.Service, error) {
	if !cfg.Mail.Enabled {
		return nil, nil //nolint:nilnil // a disabled feature has no service, and that is not an error
	}
	sender, err := mailer.NewSMTP(mailer.SMTPConfig{
		Host:        cfg.Mail.Host,
		Port:        cfg.Mail.Port,
		Username:    cfg.Mail.Username,
		Password:    cfg.Mail.Password,
		Encryption:  cfg.Mail.Encryption,
		FromAddress: cfg.Mail.FromAddress,
		FromName:    cfg.Mail.FromName,
		Timeout:     cfg.Mail.Timeout,
	})
	if err != nil {
		return nil, fmt.Errorf("building the mail sender: %w", err)
	}
	return mailjob.NewService(mailjob.ServiceConfig{Sender: sender}), nil
}
