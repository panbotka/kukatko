package database

import (
	"context"

	"github.com/panbotka/kukatko/internal/config"
)

// NewWithApplicationName opens a DB whose connections announce appName instead
// of this process's ApplicationName, so an external test can stand in for a
// second Kukátko process.
func NewWithApplicationName(ctx context.Context, cfg config.DatabaseConfig, appName string) (*DB, error) {
	return newDB(ctx, cfg, appName)
}
