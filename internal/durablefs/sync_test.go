package durablefs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSyncDirectoryAcceptsExistingDirectory(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "entry"), []byte("durable"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SyncDirectory(directory); err != nil {
		t.Fatal(err)
	}
}

func TestSyncDirectoryRejectsMissingDirectory(t *testing.T) {
	if err := SyncDirectory(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("expected a missing directory to fail synchronization")
	}
}
