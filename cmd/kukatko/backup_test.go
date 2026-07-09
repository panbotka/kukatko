package main

import (
	"errors"
	"testing"

	"github.com/panbotka/kukatko/internal/backup"
	"github.com/panbotka/kukatko/internal/config"
)

// r2StorageConfig returns a storage config on the "r2" backend whose primary
// bucket is named bucket, with just enough of the R2 settings for the backup
// wiring (the media/signing keys are irrelevant here).
func r2StorageConfig(endpoint, bucket string) config.StorageConfig {
	return config.StorageConfig{
		Backend: config.StorageBackendR2,
		R2: config.R2Config{
			Endpoint:  endpoint,
			Region:    "auto",
			Bucket:    bucket,
			AccessKey: "primary-key",
			SecretKey: "primary-secret",
		},
	}
}

// backupS3Config returns a backup destination pointing at bucket on endpoint.
func backupS3Config(endpoint, bucket string) config.S3Config {
	return config.S3Config{
		Endpoint:  endpoint,
		Region:    "auto",
		Bucket:    bucket,
		AccessKey: "backup-key",
		SecretKey: "backup-secret",
	}
}

func TestBuildBackupOriginals_backendSelectsSource(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cfg     config.Config
		wantSrc string
		wantErr error
	}{
		{
			name: "fs backend walks the local originals root",
			cfg: config.Config{
				Storage: config.StorageConfig{Backend: config.StorageBackendFS, OriginalsPath: "/var/lib/kukatko/originals"},
				Backup:  backupCfg("https://s3.example.com", "kukatko-backups"),
			},
			wantSrc: "disk",
		},
		{
			name: "r2 backend copies from the primary bucket",
			cfg: config.Config{
				Storage: r2StorageConfig("https://acct.r2.cloudflarestorage.com", "kukatko-originals"),
				Backup:  backupCfg("https://s3.example.com", "kukatko-backups"),
			},
			wantSrc: "bucket",
		},
		{
			name: "backing up a bucket onto itself fails loudly",
			cfg: config.Config{
				Storage: r2StorageConfig("https://acct.r2.cloudflarestorage.com", "kukatko-originals"),
				Backup:  backupCfg("https://acct.r2.cloudflarestorage.com", "kukatko-originals"),
			},
			wantErr: errBackupSameBucket,
		},
		{
			name: "same bucket name at another provider is a different bucket",
			cfg: config.Config{
				Storage: r2StorageConfig("https://acct.r2.cloudflarestorage.com", "kukatko-originals"),
				Backup:  backupCfg("https://s3.eu-central-1.amazonaws.com", "kukatko-originals"),
			},
			wantSrc: "bucket",
		},
		{
			name: "r2 backend without a primary bucket fails loudly",
			cfg: config.Config{
				Storage: r2StorageConfig("https://acct.r2.cloudflarestorage.com", ""),
				Backup:  backupCfg("https://s3.example.com", "kukatko-backups"),
			},
			wantErr: backup.ErrNotConfigured,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := buildBackupOriginals(&tt.cfg)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("buildBackupOriginals() error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				return
			}
			if gotSrc := sourceKind(got); gotSrc != tt.wantSrc {
				t.Errorf("buildBackupOriginals() source = %s, want %s", gotSrc, tt.wantSrc)
			}
		})
	}
}

// backupCfg returns a BackupConfig with the destination bucket on endpoint.
func backupCfg(endpoint, bucket string) config.BackupConfig {
	return config.BackupConfig{S3: backupS3Config(endpoint, bucket)}
}

// sourceKind names the concrete originals source, so the table can assert which
// one the storage backend selected.
func sourceKind(src backup.OriginalSource) string {
	switch src.(type) {
	case *backup.DiskOriginals:
		return "disk"
	case *backup.BucketOriginals:
		return "bucket"
	default:
		return "unknown"
	}
}

func TestSameBucket(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		dst  config.S3Config
		src  config.R2Config
		want bool
	}{
		{
			name: "identical endpoint and bucket",
			dst:  config.S3Config{Endpoint: "https://acct.r2.cloudflarestorage.com", Bucket: "originals"},
			src:  config.R2Config{Endpoint: "https://acct.r2.cloudflarestorage.com", Bucket: "originals"},
			want: true,
		},
		{
			name: "endpoint case is insignificant for a host name",
			dst:  config.S3Config{Endpoint: "https://ACCT.r2.cloudflarestorage.com", Bucket: "originals"},
			src:  config.R2Config{Endpoint: "https://acct.r2.cloudflarestorage.com", Bucket: "originals"},
			want: true,
		},
		{
			name: "same endpoint, different bucket",
			dst:  config.S3Config{Endpoint: "https://acct.r2.cloudflarestorage.com", Bucket: "backups"},
			src:  config.R2Config{Endpoint: "https://acct.r2.cloudflarestorage.com", Bucket: "originals"},
			want: false,
		},
		{
			name: "same bucket name, different endpoint",
			dst:  config.S3Config{Endpoint: "https://s3.eu-central-1.amazonaws.com", Bucket: "originals"},
			src:  config.R2Config{Endpoint: "https://acct.r2.cloudflarestorage.com", Bucket: "originals"},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := sameBucket(tt.dst, tt.src); got != tt.want {
				t.Errorf("sameBucket() = %v, want %v", got, tt.want)
			}
		})
	}
}
