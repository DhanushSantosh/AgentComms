package store

import (
	"bytes"
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
	"github.com/DhanushSantosh/AgentComms/internal/model"
)

func createLegacyRuntime(t *testing.T, root string) (legacyEvent, []byte) {
	t.Helper()
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
	cfg := map[string]any{"schema_version": "1.0.0", "owner": "legacy-owner"}
	b, _ := json.Marshal(cfg)
	_ = os.WriteFile(filepath.Join(rt, "config.json"), b, 0644)
	ev := legacyEvent{SchemaVersion: "1.0.0", ID: "evt-00000000000000000001", Sequence: 1, Time: time.Now().UTC(), Actor: "legacy-owner", Type: "task.create", EntityID: "legacy-current-work", Data: map[string]any{"title": "Untrusted legacy task"}}
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
	return ev, b
}

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

func TestAdoptLargeLegacyAgentsAndGovernedCutover(t *testing.T) {
	root := t.TempDir()
	cmd := exec.Command("git", "init")
	cmd.Dir = root
	if out, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("git init project: %s", out)
	}
	legacyLine := []byte("agent note | decision candidate | blocker text\r\n")
	legacy := bytes.Repeat(legacyLine, (585*1024/len(legacyLine))+1)
	legacy = legacy[:585*1024]
	legacyPath := filepath.Join(root, ".agents")
	if e := os.WriteFile(legacyPath, legacy, 0600); e != nil {
		t.Fatal(e)
	}
	legacyHash := sha256.Sum256(legacy)
	oldEvent, oldEventBytes := createLegacyRuntime(t, root)
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(root, "user"))
	creds := identity.NewMemoryStore()
	s := Open(root)
	s.SetCredentialStore(creds)
	preview, e := s.PrepareLegacyAdoption("verified-owner")
	if e != nil {
		t.Fatal(e)
	}
	if preview.Cutover.State != CutoverPrepared || preview.Manifest.SHA256 != hex.EncodeToString(legacyHash[:]) || preview.Manifest.Size != int64(len(legacy)) {
		t.Fatalf("bad preview: %#v", preview)
	}
	rootBytes, _ := os.ReadFile(legacyPath)
	if !bytes.Equal(rootBytes, legacy) {
		t.Fatal("legacy root file changed during PREPARED")
	}
	archive, e := os.ReadFile(filepath.Join(root, Runtime, filepath.FromSlash(preview.Manifest.ArchivePath)))
	if e != nil || !bytes.Equal(archive, legacy) {
		t.Fatal("byte-identical legacy archive not retained")
	}
	preservedEvent, e := os.ReadFile(filepath.Join(root, Runtime, "legacy", "v1", "events", oldEvent.ID+".json"))
	if e != nil || !bytes.Equal(preservedEvent, oldEventBytes) {
		t.Fatal("v1 event bytes were not preserved")
	}
	legacyResults, e := s.SearchLegacy("legacy-current-work")
	if e != nil || len(legacyResults) == 0 || legacyResults[0].CurrentTruth {
		t.Fatalf("v1 evidence was not searchable as non-current truth: %#v %v", legacyResults, e)
	}
	indexResults, e := s.SearchLegacy("blocker text")
	if e != nil || len(indexResults) == 0 || indexResults[0].CurrentTruth {
		t.Fatalf("legacy index was not searchable as unverified evidence: %#v %v", indexResults, e)
	}
	events, e := s.Events()
	if e != nil || len(events) != 2 {
		t.Fatalf("legacy active state was silently imported: events=%d err=%v", len(events), e)
	}
	cfg, _ := s.Config()
	agentCred, _ := identity.Generate(cfg.ProjectID, "verified-agent")
	if e = creds.Put(agentCred); e != nil {
		t.Fatal(e)
	}
	if _, e = s.AppendWithCredential("verified-agent", "agent.register", "verified-agent", model.AgentRegistered{PublicKey: agentCred.PublicKey, PrincipalType: model.PrincipalAgent, DisplayName: "Verified Agent"}, agentCred); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Append("verified-owner", "agent.activate", "verified-agent", model.AgentActivated{Role: model.RoleAgent, Capabilities: []string{"coordination"}, Scopes: []string{"path:src/**"}}); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Append("verified-owner", "task.create", "verified-work", model.TaskCreated{Title: "Explicitly seeded work", Repository: "local", Branch: "main", Resources: []string{"path:src/**"}}); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Append("verified-owner", "decision.create", "verified-decision", model.DecisionPayload{Title: "Explicit decision", Statement: "Reviewed from legacy archive"}); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Append("verified-owner", "message.post", "verified-contract", model.MessagePosted{Kind: "CONTRACT", To: []string{"verified-agent"}, Subject: "Explicit contract"}); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ConfirmLegacySeeding("verified-owner"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.SetCutoverAcknowledgements([]string{"verified-agent"}); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ActivateCutover(); e == nil {
		t.Fatal("activation succeeded before required acknowledgement")
	}
	if _, e = s.AcknowledgeCutover("verified-agent"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ActivateCutover(); e != nil {
		t.Fatal(e)
	}
	managed, _ := os.ReadFile(legacyPath)
	if !bytes.Equal(managed, ManagedBootstrap()) {
		t.Fatal("managed bootstrap was not atomically activated")
	}
	if e = s.RecoverLegacy(); e != nil {
		t.Fatal(e)
	}
	recovered, _ := os.ReadFile(legacyPath)
	if !bytes.Equal(recovered, legacy) || sha256.Sum256(recovered) != legacyHash {
		t.Fatal("recovery did not restore exact legacy bytes")
	}
	cmd = exec.Command("git", "status", "--porcelain")
	cmd.Dir = filepath.Join(root, Runtime)
	if out, e := cmd.CombinedOutput(); e != nil || len(out) != 0 {
		t.Fatalf("runtime is not clean: %s %v", out, e)
	}
	clone := filepath.Join(t.TempDir(), "runtime-clone")
	cmd = exec.Command("git", "clone", filepath.Join(root, Runtime), clone)
	if out, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("clone migrated runtime: %s", out)
	}
	clonedArchive, e := os.ReadFile(filepath.Join(clone, filepath.FromSlash(preview.Manifest.ArchivePath)))
	if e != nil || !bytes.Equal(clonedArchive, legacy) {
		t.Fatal("fresh clone did not retain byte-identical legacy evidence")
	}
}

func TestInterruptedActivationRecoversLegacyBytes(t *testing.T) {
	root := t.TempDir()
	cmd := exec.Command("git", "init")
	cmd.Dir = root
	if out, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("git init: %s", out)
	}
	legacy := []byte("exact legacy bytes\r\nwith append-only history\x00")
	if e := os.WriteFile(filepath.Join(root, ".agents"), legacy, 0600); e != nil {
		t.Fatal(e)
	}
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(root, "user"))
	t.Setenv("AGENT_COMMS_TEST_INIT_FAIL_AT", "after-cutover-bootstrap-publish")
	s := Open(root)
	s.SetCredentialStore(identity.NewMemoryStore())
	if _, e := s.PrepareLegacyAdoption("owner"); e != nil {
		t.Fatal(e)
	}
	if _, e := s.ConfirmLegacySeeding("owner"); e != nil {
		t.Fatal(e)
	}
	if _, e := s.SetCutoverAcknowledgements(nil); e != nil {
		t.Fatal(e)
	}
	if _, e := s.ActivateCutover(); e == nil {
		t.Fatal("injected activation failure did not occur")
	}
	if e := s.RollbackCutover(); e == nil {
		t.Fatal("unsafe rollback removed the only archive after partial activation")
	}
	if e := s.RecoverLegacy(); e != nil {
		t.Fatal(e)
	}
	recovered, _ := os.ReadFile(filepath.Join(root, ".agents"))
	if !bytes.Equal(recovered, legacy) {
		t.Fatal("interrupted activation recovery changed legacy bytes")
	}
}

func TestInterruptedV1MigrationResumes(t *testing.T) {
	root := t.TempDir()
	createLegacyRuntime(t, root)
	creds := identity.NewMemoryStore()
	s := Open(root)
	s.SetCredentialStore(creds)
	t.Setenv("AGENT_COMMS_TEST_INIT_FAIL_AT", "after-v1-history-migration")
	if e := s.MigrateV1("owner"); e == nil {
		t.Fatal("injected migration interruption did not occur")
	}
	t.Setenv("AGENT_COMMS_TEST_INIT_FAIL_AT", "")
	if e := s.MigrateV1("owner"); e != nil {
		t.Fatal(e)
	}
	if e := s.Verify(); e != nil {
		t.Fatal(e)
	}
	events, _ := s.Events()
	if len(events) != 2 {
		t.Fatalf("resumed migration created %d current events, want owner register/activate only", len(events))
	}
	_, journal, e := s.incompleteV1Journal()
	if e != nil || journal != nil {
		t.Fatalf("migration journal remained incomplete: %#v %v", journal, e)
	}
}

func TestLegacyManifestTamperDetected(t *testing.T) {
	root := t.TempDir()
	cmd := exec.Command("git", "init")
	cmd.Dir = root
	if out, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("git init: %s", out)
	}
	if e := os.WriteFile(filepath.Join(root, ".agents"), []byte("legacy evidence"), 0600); e != nil {
		t.Fatal(e)
	}
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(root, "user"))
	s := Open(root)
	s.SetCredentialStore(identity.NewMemoryStore())
	if _, e := s.PrepareLegacyAdoption("owner"); e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(root, Runtime, filepath.FromSlash(legacyManifestRelPath))
	b, _ := os.ReadFile(path)
	b = append(b, ' ')
	if e := os.WriteFile(path, b, 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := s.LegacyManifest(); e == nil {
		t.Fatal("tampered legacy manifest was accepted")
	}
}
