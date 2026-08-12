package main

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/embedding"
)

// captureLogger returns a debug-level JSON logger writing into buf, so a test can
// assert on both the level and the attributes of what the check emitted.
func captureLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// TestEmbeddingClientConfig_carriesTextURL guards the one line that connects the
// config key to the client: without it embedding.text_url would be accepted,
// documented and silently ignored, and every search would keep waiting on the box.
func TestEmbeddingClientConfig_carriesTextURL(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{}
	cfg.Embedding.URL = "http://box:8000"
	cfg.Embedding.TextURL = "http://embeddings-text:8000"

	got := embeddingClientConfig(cfg)
	if got.BaseURL != "http://box:8000" {
		t.Errorf("BaseURL = %q, want the box", got.BaseURL)
	}
	if got.TextBaseURL != "http://embeddings-text:8000" {
		t.Errorf("TextBaseURL = %q, want the text instance", got.TextBaseURL)
	}
}

// TestLogEmbeddingDim covers the three outcomes of the startup comparison. The
// mismatch case is the one that matters operationally: it must be a warning and
// it must name both widths, because that line is the only place the swap is
// visible before it turns into a queue full of failed jobs.
func TestLogEmbeddingDim(t *testing.T) {
	t.Parallel()
	siglip := embedding.SidecarHealth{
		Model: "ViT-SO400M-14-SigLIP2-378", Pretrained: "webli", Dim: 1152, Precision: "fp16",
	}
	tests := []struct {
		name        string
		health      embedding.SidecarHealth
		want        int
		wantLevel   string
		wantContain []string
	}{
		{
			name: "mismatch warns naming both widths", health: siglip, want: 768,
			wantLevel:   "WARN",
			wantContain: []string{`"sidecar_dim":1152`, `"configured_image_dim":768`, "ViT-SO400M-14-SigLIP2-378"},
		},
		{
			name: "agreement records the model", health: siglip, want: 1152,
			wantLevel:   "INFO",
			wantContain: []string{`"dim":1152`, "webli", "fp16"},
		},
		{
			name:      "no reported dimension is unknown, not a mismatch",
			health:    embedding.SidecarHealth{Model: "older-build"},
			want:      1152,
			wantLevel: "DEBUG",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			logEmbeddingDim(captureLogger(&buf), tt.health, tt.want)
			got := buf.String()
			if !strings.Contains(got, `"level":"`+tt.wantLevel+`"`) {
				t.Errorf("log = %s, want level %s", got, tt.wantLevel)
			}
			for _, want := range tt.wantContain {
				if !strings.Contains(got, want) {
					t.Errorf("log = %s, want it to contain %s", got, want)
				}
			}
		})
	}
}

// TestVerifyEmbeddingDim_warnsOnLiveMismatch drives the whole check against a
// sidecar that answers /health, proving the wiring — client construction, the
// route, the JSON shape — and not only the comparison.
func TestVerifyEmbeddingDim_warnsOnLiveMismatch(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != embedding.DefaultHealthPath {
			t.Errorf("path = %s, want %s", r.URL.Path, embedding.DefaultHealthPath)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"clip":{"model":"m","pretrained":"p","dim":1152,"precision":"fp16"}}`))
	}))
	defer srv.Close()

	var buf bytes.Buffer
	cfg := &config.Config{}
	cfg.Embedding.URL = srv.URL
	cfg.Embedding.ImageDim = 768
	verifyEmbeddingDim(context.Background(), cfg, captureLogger(&buf))

	if got := buf.String(); !strings.Contains(got, `"level":"WARN"`) ||
		!strings.Contains(got, `"sidecar_dim":1152`) {
		t.Errorf("log = %s, want a WARN naming the sidecar dimension", got)
	}
}

// TestVerifyEmbeddingDim_quietWhenUnverifiable pins that the check never shouts
// about what it could not check: an unconfigured sidecar says nothing at all, and
// an offline box — the normal state here — stays at debug level rather than
// looking like a misconfiguration.
func TestVerifyEmbeddingDim_quietWhenUnverifiable(t *testing.T) {
	t.Parallel()
	t.Run("no url configured", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		verifyEmbeddingDim(context.Background(), &config.Config{}, captureLogger(&buf))
		if buf.Len() != 0 {
			t.Errorf("log = %s, want nothing", buf.String())
		}
	})

	t.Run("box offline", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		url := srv.URL
		srv.Close()

		var buf bytes.Buffer
		cfg := &config.Config{}
		cfg.Embedding.URL = url
		cfg.Embedding.ImageDim = 1152
		verifyEmbeddingDim(context.Background(), cfg, captureLogger(&buf))

		if got := buf.String(); !strings.Contains(got, `"level":"DEBUG"`) || strings.Contains(got, "WARN") {
			t.Errorf("log = %s, want a DEBUG line and no warning", got)
		}
	})
}
