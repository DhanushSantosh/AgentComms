package personalauthority

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/controlplane"
)

// TestOpenWaitsForALockHeldByAReplacedDaemon covers a daemon replacing
// another: the old process can hold the database briefly while it exits.
// Open must wait for the lock, not fail at once with SQLITE_BUSY on its
// first read (the schema-version check), which broke daemon replacement
// on Windows.
func TestOpenWaitsForALockHeldByAReplacedDaemon(t *testing.T) {
	path := filepath.Join(t.TempDir(), "authority.db")
	signer, err := controlplane.GenerateSigner()
	if err != nil {
		t.Fatal(err)
	}
	engine, err := Open(path, signer)
	if err != nil {
		t.Fatal(err)
	}
	if err = engine.Close(); err != nil {
		t.Fatal(err)
	}

	holder, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	holder.SetMaxOpenConns(1)
	// In WAL mode an ordinary write does not block readers; exclusive
	// locking mode does, as a connection recovering or checkpointing on
	// shutdown can. Writing under it takes the lock.
	if _, err = holder.Exec(`PRAGMA locking_mode=EXCLUSIVE`); err != nil {
		t.Fatal(err)
	}
	if _, err = holder.Exec(`BEGIN EXCLUSIVE; CREATE TABLE IF NOT EXISTS lock_probe(x); COMMIT`); err != nil {
		t.Fatal(err)
	}
	released := make(chan struct{})
	go func() {
		time.Sleep(300 * time.Millisecond)
		_ = holder.Close() // closing the only connection releases the lock
		close(released)
	}()

	reopened, err := Open(path, signer)
	if err != nil {
		t.Fatalf("Open must wait for a briefly held lock: %v", err)
	}
	<-released
	if err = reopened.Close(); err != nil {
		t.Fatal(err)
	}
}
