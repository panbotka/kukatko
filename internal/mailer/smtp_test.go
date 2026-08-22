package mailer

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// TestNewSMTP_defaults verifies the optional settings are filled in rather than
// refused: no port means the submission port, no timeout means the default one,
// and no encryption means STARTTLS.
func TestNewSMTP_defaults(t *testing.T) {
	t.Parallel()

	sender, err := NewSMTP(SMTPConfig{Host: " smtp.example.com ", FromAddress: " kukatko@example.com "})
	if err != nil {
		t.Fatalf("NewSMTP returned error: %v", err)
	}
	if sender.host != "smtp.example.com" {
		t.Errorf("host = %q, want it trimmed", sender.host)
	}
	if sender.port != DefaultPort {
		t.Errorf("port = %d, want %d", sender.port, DefaultPort)
	}
	if sender.timeout != DefaultTimeout {
		t.Errorf("timeout = %s, want %s", sender.timeout, DefaultTimeout)
	}
	if sender.encryption != EncryptionSTARTTLS {
		t.Errorf("encryption = %q, want %q", sender.encryption, EncryptionSTARTTLS)
	}
	if sender.from.Address != "kukatko@example.com" {
		t.Errorf("from = %q, want it trimmed", sender.from.Address)
	}
	if got := sender.tlsConfig().ServerName; got != "smtp.example.com" {
		t.Errorf("tlsConfig.ServerName = %q, want the configured host", got)
	}
}

// TestNewSMTP_validation covers the configurations NewSMTP refuses and the ones
// it accepts, including the three encryption modes.
func TestNewSMTP_validation(t *testing.T) {
	t.Parallel()

	base := SMTPConfig{Host: "smtp.example.com", FromAddress: "kukatko@example.com"}
	withHost := func(host string) SMTPConfig { cfg := base; cfg.Host = host; return cfg }
	withFrom := func(from string) SMTPConfig { cfg := base; cfg.FromAddress = from; return cfg }
	withEncryption := func(mode string) SMTPConfig { cfg := base; cfg.Encryption = mode; return cfg }

	tests := []struct {
		name    string
		cfg     SMTPConfig
		wantErr bool
	}{
		{name: "minimal config", cfg: base},
		{name: "starttls", cfg: withEncryption("starttls")},
		{name: "tls", cfg: withEncryption("tls")},
		{name: "none", cfg: withEncryption("none")},
		{name: "mode is case-insensitive", cfg: withEncryption("STARTTLS")},
		{name: "empty host", cfg: withHost(""), wantErr: true},
		{name: "blank host", cfg: withHost("   "), wantErr: true},
		{name: "empty from", cfg: withFrom(""), wantErr: true},
		{name: "unparseable from", cfg: withFrom("nobody"), wantErr: true},
		{name: "unknown encryption", cfg: withEncryption("ssl"), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := NewSMTP(tt.cfg)
			if tt.wantErr && !errors.Is(err, ErrIncompleteConfig) {
				t.Fatalf("NewSMTP error = %v, want ErrIncompleteConfig", err)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("NewSMTP returned unexpected error: %v", err)
			}
		})
	}
}

// TestNewSMTP_errorNamesTheProblem verifies a refusal says which setting is at
// fault, since a deployment reads only the startup error.
func TestNewSMTP_errorNamesTheProblem(t *testing.T) {
	t.Parallel()

	_, err := NewSMTP(SMTPConfig{Host: "smtp.example.com", FromAddress: "a@b.cz", Encryption: "ssl"})
	if err == nil || !strings.Contains(err.Error(), `"ssl"`) {
		t.Fatalf("NewSMTP error = %v, want it to name the unknown mode", err)
	}
}

// TestNormalizeEncryption covers the mode mapping on its own.
func TestNormalizeEncryption(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{in: "", want: EncryptionSTARTTLS},
		{in: " starttls ", want: EncryptionSTARTTLS},
		{in: "TLS", want: EncryptionTLS},
		{in: "none", want: EncryptionNone},
		{in: "plain", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			got, err := normalizeEncryption(tt.in)
			if tt.wantErr {
				if !errors.Is(err, ErrIncompleteConfig) {
					t.Fatalf("normalizeEncryption(%q) error = %v, want ErrIncompleteConfig", tt.in, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeEncryption(%q) returned error: %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("normalizeEncryption(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestSMTPSender_Send_refusesBadRecipients verifies the recipient guard runs
// before anything is dialled: the sender points at a port nothing listens on and
// at a timeout short enough that a connection attempt would show up as a
// deadline error rather than as the address error asserted here.
func TestSMTPSender_Send_refusesBadRecipients(t *testing.T) {
	t.Parallel()

	sender, err := NewSMTP(SMTPConfig{
		Host:        "smtp.example.invalid",
		Port:        1,
		FromAddress: "kukatko@example.com",
		Timeout:     time.Nanosecond,
	})
	if err != nil {
		t.Fatalf("NewSMTP returned error: %v", err)
	}

	tests := []struct {
		name    string
		to      string
		wantErr error
	}{
		{name: "placeholder", to: "jan@kukatko.invalid", wantErr: ErrPlaceholderAddress},
		{name: "empty", to: "", wantErr: ErrInvalidAddress},
		{name: "malformed", to: "jan(at)example.com", wantErr: ErrInvalidAddress},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := sender.Send(context.Background(), Message{To: tt.to, Subject: "Test", Body: "tělo\n"})
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Send(%q) = %v, want %v", tt.to, err, tt.wantErr)
			}
			if errors.Is(err, ErrSendFailed) {
				t.Fatalf("Send(%q) reached the network: %v", tt.to, err)
			}
		})
	}
}
