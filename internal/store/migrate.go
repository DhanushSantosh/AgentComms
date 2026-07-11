package store

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/identity"
	"github.com/DhanushSantosh/AgentComms/internal/model"
)

type legacyEvent struct {
	SchemaVersion string         `json:"schema_version"`
	ID            string         `json:"id"`
	Sequence      uint64         `json:"sequence"`
	Time          time.Time      `json:"time"`
	Actor         string         `json:"actor"`
	Type          string         `json:"type"`
	EntityID      string         `json:"entity_id,omitempty"`
	Data          map[string]any `json:"data,omitempty"`
	PreviousHash  string         `json:"previous_hash,omitempty"`
	Hash          string         `json:"hash"`
	Signature     string         `json:"signature"`
}

func legacyCanonical(e legacyEvent) ([]byte, error) {
	e.Hash = ""
	e.Signature = ""
	return json.Marshal(e)
}
func (s *Store) VerifyLegacyV1() error {
	pubRaw, e := os.ReadFile(filepath.Join(s.runtime(), "signing.pub"))
	if e != nil {
		return e
	}
	pub, e := base64.StdEncoding.DecodeString(strings.TrimSpace(string(pubRaw)))
	if e != nil {
		return e
	}
	files, _ := filepath.Glob(filepath.Join(s.runtime(), "events", "*.json"))
	sort.Strings(files)
	prev := ""
	for i, f := range files {
		b, x := os.ReadFile(f)
		if x != nil {
			return x
		}
		var v legacyEvent
		if x = json.Unmarshal(b, &v); x != nil {
			return x
		}
		if v.SchemaVersion != "1.0.0" || v.Sequence != uint64(i+1) || v.PreviousHash != prev {
			return fmt.Errorf("legacy chain discontinuity at %s", v.ID)
		}
		c, _ := legacyCanonical(v)
		h := sha256.Sum256(c)
		hs := hex.EncodeToString(h[:])
		sig, x := base64.StdEncoding.DecodeString(v.Signature)
		if x != nil || hs != v.Hash || !ed25519.Verify(ed25519.PublicKey(pub), []byte(v.Hash), sig) {
			return fmt.Errorf("legacy integrity failure at %s", v.ID)
		}
		prev = v.Hash
	}
	return nil
}
func (s *Store) MigrateV1(owner string) error {
	cfg, e := s.Config()
	if e != nil {
		return e
	}
	if cfg.SchemaVersion == model.SchemaVersion {
		return nil
	}
	if cfg.SchemaVersion != "1.0.0" {
		return fmt.Errorf("unsupported source schema %q", cfg.SchemaVersion)
	}
	if owner == "" {
		return errors.New("owner identity mapping is required")
	}
	if e = s.VerifyLegacyV1(); e != nil {
		return e
	}
	release, e := s.acquire(owner)
	if e != nil {
		return e
	}
	locked := true
	defer func() {
		if locked {
			release()
		}
	}()
	stamp := s.Now().Format("20060102T150405Z")
	backup := filepath.Join(s.runtime(), "migrations", "v1-"+stamp)
	if e = os.MkdirAll(backup, 0700); e != nil {
		return e
	}
	journal := map[string]any{"source": "1.0.0", "target": model.SchemaVersion, "owner": owner, "started_at": s.Now(), "status": "IN_PROGRESS"}
	if e = writeJSON(filepath.Join(backup, "journal.json"), journal, 0600); e != nil {
		return e
	}
	for _, name := range []string{"config.json", "signing.pub"} {
		b, x := os.ReadFile(filepath.Join(s.runtime(), name))
		if x != nil {
			return x
		}
		if x = os.WriteFile(filepath.Join(backup, name), b, 0600); x != nil {
			return x
		}
	}
	legacyDir := filepath.Join(s.runtime(), "legacy", "v1")
	if e = os.MkdirAll(legacyDir, 0700); e != nil {
		return e
	}
	if e = os.Rename(filepath.Join(s.runtime(), "events"), filepath.Join(legacyDir, "events")); e != nil {
		return e
	}
	if e = os.MkdirAll(filepath.Join(s.runtime(), "events"), 0700); e != nil {
		return e
	}
	projectID := fmt.Sprintf("ac-%d", s.Now().UnixNano())
	cred, e := identity.Generate(projectID, owner)
	if e != nil {
		return e
	}
	if e = s.Credentials.Put(cred); e != nil {
		return e
	}
	next := Config{SchemaVersion: model.SchemaVersion, ProjectID: projectID, Owner: owner, DefaultLease: "4h", StaleGrace: "1h", ActiveRetention: "168h", SummaryLimit: 1200, ArtifactLimitBytes: 5 * 1024 * 1024}
	if e = writeJSON(filepath.Join(s.runtime(), "config.json"), next, 0644); e != nil {
		return e
	}
	journal["status"] = "EVENTS_PENDING"
	_ = writeJSON(filepath.Join(backup, "journal.json"), journal, 0600)
	if e = s.git("add", "config.json", "legacy", "migrations"); e != nil {
		return e
	}
	if e = s.git("commit", "--no-gpg-sign", "-m", "Migrate legacy v1 history"); e != nil {
		return e
	}
	release()
	locked = false
	if _, e = s.Append(owner, "agent.register", owner, model.AgentRegistered{PublicKey: cred.PublicKey, PrincipalType: model.PrincipalHuman, DisplayName: owner}); e != nil {
		return e
	}
	if _, e = s.Append(owner, "agent.activate", owner, model.AgentActivated{Role: model.RoleOwner, Capabilities: []string{"*"}, Scopes: []string{"*"}}); e != nil {
		return e
	}
	journal["status"] = "COMPLETE"
	journal["completed_at"] = s.Now()
	if e = writeJSON(filepath.Join(backup, "journal.json"), journal, 0600); e != nil {
		return e
	}
	if e = s.git("add", filepath.ToSlash(filepath.Join("migrations", "v1-"+stamp, "journal.json"))); e != nil {
		return e
	}
	return s.git("commit", "--no-gpg-sign", "-m", "Complete v1 migration journal")
}
func writeJSON(path string, v any, mode os.FileMode) error {
	b, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		return e
	}
	return os.WriteFile(path, append(b, '\n'), mode)
}
