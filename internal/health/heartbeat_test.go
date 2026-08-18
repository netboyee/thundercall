package health

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWriteAndVerifyHeartbeatFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "worker.heartbeat")
	at := time.Date(2026, time.August, 18, 12, 0, 0, 0, time.UTC)

	if err := WriteHeartbeatFile(path, at); err != nil {
		t.Fatalf("WriteHeartbeatFile() error = %v", err)
	}

	if err := VerifyHeartbeatFile(path, time.Minute, at.Add(30*time.Second)); err != nil {
		t.Fatalf("VerifyHeartbeatFile() error = %v", err)
	}
}

func TestVerifyHeartbeatFileRejectsStaleHeartbeat(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ingest.heartbeat")
	at := time.Date(2026, time.August, 18, 12, 0, 0, 0, time.UTC)

	if err := WriteHeartbeatFile(path, at); err != nil {
		t.Fatalf("WriteHeartbeatFile() error = %v", err)
	}

	err := VerifyHeartbeatFile(path, time.Minute, at.Add(2*time.Minute))
	if err == nil {
		t.Fatal("expected stale heartbeat error, got nil")
	}
	if !strings.Contains(err.Error(), "stale") {
		t.Fatalf("expected stale heartbeat error, got %v", err)
	}
}
