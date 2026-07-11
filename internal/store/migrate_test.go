package store

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/identity"
)

func TestMigrateLegacyV1PreservesAndReanchorsHistory(t *testing.T) {
	root := t.TempDir()
	rt := filepath.Join(root, Runtime)
	for _, d := range []string{"events", "tmp", "migrations"} {
		if e := os.MkdirAll(filepath.Join(rt, d), 0700); e != nil {
			t.Fatal(e)
		}
	}
	pub, priv, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(rt, "signing.pub"), []byte(base64.StdEncoding.EncodeToString(pub)), 0644); e != nil {
		t.Fatal(e)
	}
	cfg := map[string]any{"schema_version": "1.0.0", "owner": "owner"}
	b, _ := json.Marshal(cfg)
	_ = os.WriteFile(filepath.Join(rt, "config.json"), b, 0644)
	ev := legacyEvent{SchemaVersion: "1.0.0", ID: "evt-00000000000000000001", Sequence: 1, Time: time.Now().UTC(), Actor: "owner", Type: "message.post", EntityID: "legacy", Data: map[string]any{"kind": "FYI"}}
	c, _ := legacyCanonical(ev)
	h := sha256.Sum256(c)
	ev.Hash = hex.EncodeToString(h[:])
	ev.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(priv, []byte(ev.Hash)))
	b, _ = json.Marshal(ev)
	_ = os.WriteFile(filepath.Join(rt, "events", ev.ID+".json"), b, 0600)
	for _, args := range [][]string{{"init"}, {"config", "user.name", "Migration Test"}, {"config", "user.email", "migration@example.invalid"}, {"add", "."}, {"commit", "-m", "Legacy runtime"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = rt
		if out, e := cmd.CombinedOutput(); e != nil {
			t.Fatalf("git %v: %s", args, out)
		}
	}
	s := Open(root)
	s.SetCredentialStore(identity.NewMemoryStore())
	if e = s.MigrateV1("owner"); e != nil {
		t.Fatal(e)
	}
	if e = s.Verify(); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(filepath.Join(rt, "legacy", "v1", "events", ev.ID+".json")); e != nil {
		t.Fatal("legacy event not preserved")
	}
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = rt
	if out, e := cmd.CombinedOutput(); e != nil || len(out) != 0 {
		t.Fatalf("migration left dirty runtime: %s %v", out, e)
	}
}
