package health

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type FileHeartbeat struct {
	path string
	now  func() time.Time
}

func NewFileHeartbeat(path string) *FileHeartbeat {
	return &FileHeartbeat{
		path: strings.TrimSpace(path),
		now:  func() time.Time { return time.Now().UTC() },
	}
}

func (h *FileHeartbeat) Enabled() bool {
	return h != nil && h.path != ""
}

func (h *FileHeartbeat) Touch() error {
	if h == nil {
		return nil
	}
	return WriteHeartbeatFile(h.path, h.now())
}

func WriteHeartbeatFile(path string, at time.Time) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create heartbeat directory %q: %w", dir, err)
	}

	tempPath := path + ".tmp"
	payload := []byte(at.UTC().Format(time.RFC3339Nano))
	if err := os.WriteFile(tempPath, payload, 0o644); err != nil {
		return fmt.Errorf("write heartbeat temp file %q: %w", tempPath, err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("replace heartbeat file %q: %w", path, err)
	}
	return nil
}

func VerifyHeartbeatFile(path string, maxAge time.Duration, now time.Time) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	if maxAge <= 0 {
		return fmt.Errorf("heartbeat max age must be positive")
	}

	payload, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read heartbeat file %q: %w", path, err)
	}

	lastSeen, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(string(payload)))
	if err != nil {
		return fmt.Errorf("parse heartbeat file %q: %w", path, err)
	}

	age := now.UTC().Sub(lastSeen.UTC())
	if age < 0 {
		age = 0
	}
	if age > maxAge {
		return fmt.Errorf("heartbeat file %q is stale by %s (max %s)", path, age.Round(time.Second), maxAge)
	}
	return nil
}
