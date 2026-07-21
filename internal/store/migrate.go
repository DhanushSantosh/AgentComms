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
	if len(files) == 0 {
		files, _ = filepath.Glob(filepath.Join(s.runtime(), "legacy", "v1", "events", "*.json"))
	}
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
	return s.migrateV1(owner, false)
}

func (s *Store) migrateV1(owner string, legacyAdoption bool) error {
	cfg, e := s.Config()
	if e != nil {
		return e
	}
	if cfg.SchemaVersion == model.SchemaVersion {
		return s.resumeV1Migration(owner)
	}
	if cfg.SchemaVersion != "1.0.0" {
		return fmt.Errorf("unsupported source schema %q", cfg.SchemaVersion)
	}
	if owner == "" {
		return errors.New("owner identity mapping is required")
	}
	if b, x := os.ReadFile(filepath.Join(s.Root, ".agents")); x == nil && !strings.EqualFold(strings.TrimSpace(string(b)), strings.TrimSpace(string(ManagedBootstrap()))) && !legacyAdoption {
		return errors.New("legacy .agents requires dedicated `agent-comms migrate adopt`; plain runtime migration refused")
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
	next := Config{SchemaVersion: model.SchemaVersion, ToolkitVersion: RuntimeVersion, ProjectID: projectID, Owner: owner, DefaultLease: "4h", StaleGrace: "1h", ActiveRetention: "168h", SummaryLimit: 1200, ArtifactLimitBytes: 5 * 1024 * 1024}
	if e = writeJSON(filepath.Join(s.runtime(), "config.json"), next, 0644); e != nil {
		return e
	}
	if e = os.WriteFile(filepath.Join(s.runtime(), "AGENT_INSTRUCTIONS.md"), AgentInstructions(), 0644); e != nil {
		return e
	}
	journal["status"] = "EVENTS_PENDING"
	_ = writeJSON(filepath.Join(backup, "journal.json"), journal, 0600)
	if e = s.git("add", "config.json", "AGENT_INSTRUCTIONS.md", "legacy", "migrations"); e != nil {
		return e
	}
	if e = s.git("commit", "--no-gpg-sign", "-m", "Migrate legacy v1 history"); e != nil {
		return e
	}
	if initFail("after-v1-history-migration") {
		return errors.New("injected migration failure after legacy history preservation")
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
	if e = s.git("commit", "--no-gpg-sign", "-m", "Complete v1 migration journal"); e != nil {
		return e
	}
	return s.ensureBootstrapIfAbsent()
}

func (s *Store) resumeV1Migration(owner string) error {
	journalPath, journal, e := s.incompleteV1Journal()
	if e != nil || journalPath == "" {
		return e
	}
	journalOwner, _ := journal["owner"].(string)
	if owner == "" {
		owner = journalOwner
	}
	if owner == "" || (journalOwner != "" && owner != journalOwner) {
		return errors.New("matching owner identity mapping is required to resume migration")
	}
	events, e := s.Events()
	if e != nil {
		return e
	}
	if len(events) == 0 {
		cfg, x := s.Config()
		if x != nil {
			return x
		}
		cred, x := identity.ResolveCredential(s.Credentials, cfg.ProjectID, owner)
		if x != nil {
			return fmt.Errorf("resume owner credential: %w", x)
		}
		if _, x = s.AppendWithCredential(owner, "agent.register", owner, model.AgentRegistered{PublicKey: cred.PublicKey, PrincipalType: model.PrincipalHuman, DisplayName: owner}, cred); x != nil {
			return x
		}
		events, _ = s.Events()
	}
	if len(events) == 1 {
		if _, e = s.Append(owner, "agent.activate", owner, model.AgentActivated{Role: model.RoleOwner, Capabilities: []string{"*"}, Scopes: []string{"*"}}); e != nil {
			return e
		}
	}
	journal["status"] = "COMPLETE"
	journal["completed_at"] = s.Now()
	if e = writeJSON(journalPath, journal, 0600); e != nil {
		return e
	}
	rel, _ := filepath.Rel(s.runtime(), journalPath)
	if e = s.git("add", filepath.ToSlash(rel)); e != nil {
		return e
	}
	if e = s.git("commit", "--no-gpg-sign", "-m", "Complete resumed v1 migration journal"); e != nil {
		return e
	}
	return s.ensureBootstrapIfAbsent()
}

func (s *Store) ensureBootstrapIfAbsent() error {
	path := filepath.Join(s.Root, ".agents")
	if _, e := os.Lstat(path); e == nil {
		return nil
	} else if !os.IsNotExist(e) {
		return e
	}
	tmp := path + ".agent-comms.migrate.tmp"
	if e := writeFileSync(tmp, ManagedBootstrap(), 0644); e != nil {
		return e
	}
	if e := os.Rename(tmp, path); e != nil {
		_ = os.Remove(tmp)
		return e
	}
	return nil
}

func (s *Store) incompleteV1Journal() (string, map[string]any, error) {
	files, _ := filepath.Glob(filepath.Join(s.runtime(), "migrations", "v1-*", "journal.json"))
	sort.Sort(sort.Reverse(sort.StringSlice(files)))
	for _, path := range files {
		b, e := os.ReadFile(path)
		if e != nil {
			return "", nil, e
		}
		var j map[string]any
		if e = json.Unmarshal(b, &j); e != nil {
			return "", nil, e
		}
		if j["status"] != "COMPLETE" {
			return path, j, nil
		}
	}
	return "", nil, nil
}

func (s *Store) MigrationIncomplete() (bool, string) {
	path, journal, e := s.incompleteV1Journal()
	if e != nil {
		return true, "INVALID"
	}
	if path == "" {
		return false, ""
	}
	status, _ := journal["status"].(string)
	return true, status
}
func writeJSON(path string, v any, mode os.FileMode) error {
	b, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		return e
	}
	return os.WriteFile(path, append(b, '\n'), mode)
}
