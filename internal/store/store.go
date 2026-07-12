package store

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/identity"
	"github.com/DhanushSantosh/AgentComms/internal/model"
)

const Runtime = ".agent-comms"

var RuntimeVersion = "dev"

type Config struct {
	SchemaVersion      string `json:"schema_version"`
	ToolkitVersion     string `json:"toolkit_version,omitempty"`
	ProjectID          string `json:"project_id"`
	Owner              string `json:"owner"`
	DefaultLease       string `json:"default_lease"`
	StaleGrace         string `json:"stale_grace"`
	ActiveRetention    string `json:"active_retention"`
	SummaryLimit       int    `json:"summary_limit"`
	ArtifactLimitBytes int64  `json:"artifact_limit_bytes"`
	RequireReview      bool   `json:"require_review"`
	AdoptionRequired   bool   `json:"adoption_required,omitempty"`
}
type Store struct {
	Root        string
	Now         func() time.Time
	Credentials identity.Store
	LockTimeout time.Duration
	runtimePath string
}

func Open(root string) *Store {
	return &Store{Root: root, Now: func() time.Time { return time.Now().UTC() }, Credentials: identity.DefaultStore(), LockTimeout: 10 * time.Second}
}
func (s *Store) runtime() string {
	if s.runtimePath != "" {
		return s.runtimePath
	}
	return filepath.Join(s.Root, Runtime)
}
func (s *Store) SetCredentialStore(c identity.Store) { s.Credentials = c }
func (s *Store) Config() (Config, error) {
	b, e := os.ReadFile(filepath.Join(s.runtime(), "config.json"))
	if e != nil {
		return Config{}, e
	}
	var c Config
	e = json.Unmarshal(b, &c)
	return c, e
}
func (s *Store) Init(owner string) error {
	if owner == "" {
		return errors.New("owner is required")
	}
	r := s.runtime()
	if _, e := os.Stat(r); e == nil {
		return errors.New("runtime already initialized")
	}
	bootstrap := filepath.Join(s.Root, ".agents")
	if _, e := os.Lstat(bootstrap); e == nil {
		return errors.New("legacy .agents exists; initialization refused: use `agent-comms migrate adopt` to preserve and govern cutover")
	} else if !os.IsNotExist(e) {
		return fmt.Errorf("inspect existing .agents: %w", e)
	}
	if _, e := os.Stat(filepath.Join(s.Root, ".git")); e != nil {
		return errors.New("target must be a Git repository")
	}
	stage := filepath.Join(s.Root, fmt.Sprintf(".%s.init-%d", strings.TrimPrefix(Runtime, "."), s.Now().UnixNano()))
	staged := *s
	staged.runtimePath = stage
	projectID, _, e := staged.initializeRuntime(owner)
	if e != nil {
		_ = os.RemoveAll(stage)
		return e
	}
	rollbackCredential := true
	defer func() {
		if rollbackCredential {
			_ = s.Credentials.Delete(projectID, owner)
		}
	}()
	if initFail("before-runtime-publish") {
		_ = os.RemoveAll(stage)
		return errors.New("injected initialization failure before runtime publish")
	}
	if e = os.Rename(stage, r); e != nil {
		_ = os.RemoveAll(stage)
		return fmt.Errorf("publish runtime: %w", e)
	}
	bootstrapTmp := bootstrap + ".agent-comms.tmp"
	if e = writeFileSync(bootstrapTmp, ManagedBootstrap(), 0644); e != nil {
		_ = os.RemoveAll(r)
		return e
	}
	if initFail("before-bootstrap-publish") {
		_ = os.Remove(bootstrapTmp)
		_ = os.RemoveAll(r)
		return errors.New("injected initialization failure before bootstrap publish")
	}
	if e = os.Rename(bootstrapTmp, bootstrap); e != nil {
		_ = os.Remove(bootstrapTmp)
		_ = os.RemoveAll(r)
		return fmt.Errorf("publish bootstrap: %w", e)
	}
	if e = s.saveProfile(projectID, owner); e != nil {
		_ = os.Remove(bootstrap)
		_ = os.RemoveAll(r)
		return fmt.Errorf("save owner profile: %w", e)
	}
	rollbackCredential = false
	return nil
}

func ManagedBootstrap() []byte {
	return []byte("# Agent Comms managed bootstrap\nruntime = .agent-comms\ninstructions = .agent-comms/AGENT_INSTRUCTIONS.md\n")
}

func AgentInstructions() []byte {
	return []byte("# Agent Comms agent instructions\n\nRun `agent-comms status --json` before work. Register and become ACTIVE before claiming. Never write resources covered by another lease. Use durable messages for contracts, blockers, actions, and decisions.\n")
}

func initFail(point string) bool { return os.Getenv("AGENT_COMMS_TEST_INIT_FAIL_AT") == point }

func writeFileSync(path string, data []byte, mode os.FileMode) error {
	f, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if e != nil {
		return e
	}
	if _, e = f.Write(data); e == nil {
		e = f.Sync()
	}
	if closeErr := f.Close(); e == nil {
		e = closeErr
	}
	return e
}

func (s *Store) initializeRuntime(owner string) (string, identity.Credential, error) {
	r := s.runtime()
	for _, d := range []string{"events", "artifacts/sha256", "tmp", "cache", "schemas", "migrations"} {
		if e := os.MkdirAll(filepath.Join(r, d), 0700); e != nil {
			return "", identity.Credential{}, e
		}
	}
	projectID := fmt.Sprintf("ac-%d", s.Now().UnixNano())
	cred, e := identity.Generate(projectID, owner)
	if e != nil {
		return "", cred, e
	}
	if e = s.Credentials.Put(cred); e != nil {
		return "", cred, fmt.Errorf("store owner credential: %w", e)
	}
	cfg := Config{SchemaVersion: model.SchemaVersion, ToolkitVersion: RuntimeVersion, ProjectID: projectID, Owner: owner, DefaultLease: "4h", StaleGrace: "1h", ActiveRetention: "168h", SummaryLimit: 1200, ArtifactLimitBytes: 5 * 1024 * 1024}
	b, _ := json.MarshalIndent(cfg, "", "  ")
	if e = os.WriteFile(filepath.Join(r, "config.json"), append(b, '\n'), 0644); e != nil {
		return "", cred, e
	}
	if e = os.WriteFile(filepath.Join(r, "AGENT_INSTRUCTIONS.md"), AgentInstructions(), 0644); e != nil {
		return "", cred, e
	}
	if e = os.WriteFile(filepath.Join(r, ".gitignore"), []byte("tmp/\ncache/\n"), 0644); e != nil {
		return "", cred, e
	}
	if e = s.git("init"); e != nil {
		return "", cred, e
	}
	_ = s.git("config", "user.name", "Agent Comms")
	_ = s.git("config", "user.email", "agent-comms@localhost")
	_ = s.git("config", "commit.gpgsign", "false")
	if e = s.git("add", "."); e != nil {
		return "", cred, e
	}
	if e = s.git("commit", "--no-gpg-sign", "-m", "Initialize Agent Comms runtime"); e != nil {
		return "", cred, e
	}
	p := model.AgentRegistered{PublicKey: cred.PublicKey, PrincipalType: model.PrincipalHuman, DisplayName: owner}
	if _, e = s.Append(owner, "agent.register", owner, p); e != nil {
		return "", cred, e
	}
	_, e = s.Append(owner, "agent.activate", owner, model.AgentActivated{Role: model.RoleOwner, Capabilities: []string{"*"}, Scopes: []string{"*"}})
	return projectID, cred, e
}

func (s *Store) saveProfile(projectID, owner string) error {
	uc, _ := identity.LoadUserConfig()
	name := projectID + ":" + owner
	uc.ActiveProfile = name
	uc.Profiles[name] = identity.Profile{Name: name, ProjectID: projectID, Actor: owner, ProjectRoot: s.Root}
	return identity.SaveUserConfig(uc)
}
func (s *Store) git(args ...string) error {
	c := exec.Command("git", args...)
	c.Dir = s.runtime()
	var b bytes.Buffer
	c.Stderr = &b
	if e := c.Run(); e != nil {
		return fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(b.String()))
	}
	return nil
}
func (s *Store) gitOut(args ...string) (string, error) {
	c := exec.Command("git", args...)
	c.Dir = s.runtime()
	b, e := c.CombinedOutput()
	if e != nil {
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(string(b)))
	}
	return strings.TrimSpace(string(b)), nil
}
func canonical(e model.Event) ([]byte, error) { e.Hash = ""; e.Signature = ""; return json.Marshal(e) }
func (s *Store) Append(actor, typ, entity string, payload any) (model.Event, error) {
	cfg, err := s.Config()
	if err != nil {
		return model.Event{}, err
	}
	cred, err := identity.ResolveCredential(s.Credentials, cfg.ProjectID, actor)
	if err != nil {
		return model.Event{}, fmt.Errorf("credential for %s: %w", actor, err)
	}
	return s.AppendWithCredential(actor, typ, entity, payload, cred)
}

func (s *Store) AppendWithCredential(actor, typ, entity string, payload any, cred identity.Credential) (model.Event, error) {
	release, e := s.acquire(actor)
	if e != nil {
		return model.Event{}, e
	}
	defer release()
	if e = s.Recover(); e != nil {
		return model.Event{}, e
	}
	raw, e := model.EncodePayload(typ, payload)
	if e != nil {
		return model.Event{}, e
	}
	events, e := s.Events()
	if e != nil {
		return model.Event{}, e
	}
	seq := uint64(1)
	prev := ""
	if len(events) > 0 {
		seq = events[len(events)-1].Sequence + 1
		prev = events[len(events)-1].Hash
	}
	ev := model.Event{SchemaVersion: model.SchemaVersion, PayloadVersion: 1, ID: fmt.Sprintf("evt-%020d", seq), Sequence: seq, Time: s.Now(), Actor: actor, Type: typ, EntityID: entity, Data: raw, PreviousHash: prev, KeyFingerprint: identity.Fingerprint(cred.PublicKey)}
	c, e := canonical(ev)
	if e != nil {
		return ev, e
	}
	h := sha256.Sum256(c)
	ev.Hash = hex.EncodeToString(h[:])
	ev.Signature, e = identity.Sign(cred, ev.Hash)
	if e != nil {
		return ev, e
	}
	b, _ := json.MarshalIndent(ev, "", "  ")
	tmp := filepath.Join(s.runtime(), "tmp", ev.ID+".json.tmp")
	dst := filepath.Join(s.runtime(), "events", ev.ID+".json")
	if e = os.WriteFile(tmp, append(b, '\n'), 0600); e != nil {
		return ev, e
	}
	f, e := os.OpenFile(tmp, os.O_RDWR, 0600)
	if e != nil {
		return ev, e
	}
	e = f.Sync()
	_ = f.Close()
	if e != nil {
		return ev, e
	}
	if e = os.Rename(tmp, dst); e != nil {
		return ev, e
	}
	if e = s.git("add", filepath.ToSlash(filepath.Join("events", ev.ID+".json"))); e != nil {
		return ev, e
	}
	if typ == "artifact.add" {
		_ = s.git("add", filepath.ToSlash(filepath.Join("artifacts", "sha256", entity)))
	}
	if e = s.git("commit", "--no-gpg-sign", "-m", fmt.Sprintf("%s %s", typ, entity)); e != nil {
		return ev, e
	}
	return ev, nil
}
func (s *Store) Events() ([]model.Event, error) {
	fs, e := filepath.Glob(filepath.Join(s.runtime(), "events", "*.json"))
	if e != nil {
		return nil, e
	}
	sort.Strings(fs)
	out := make([]model.Event, 0, len(fs))
	for _, f := range fs {
		b, x := os.ReadFile(f)
		if x != nil {
			return nil, x
		}
		var v model.Event
		if x = json.Unmarshal(b, &v); x != nil {
			return nil, fmt.Errorf("%s: %w", f, x)
		}
		out = append(out, v)
	}
	return out, nil
}
func (s *Store) Verify() error {
	ev, e := s.Events()
	if e != nil {
		return e
	}
	pub := map[string]string{}
	prev := ""
	for i, v := range ev {
		if v.Sequence != uint64(i+1) || v.PreviousHash != prev {
			return fmt.Errorf("chain discontinuity at %s", v.ID)
		}
		c, _ := canonical(v)
		h := sha256.Sum256(c)
		if hex.EncodeToString(h[:]) != v.Hash {
			return fmt.Errorf("hash mismatch at %s", v.ID)
		}
		if v.Type == "agent.register" {
			p, x := model.DecodePayload(v.Type, v.Data)
			if x == nil {
				a := p.(*model.AgentRegistered)
				pub[v.Actor] = a.PublicKey
			}
		}
		key := pub[v.Actor]
		if key == "" {
			return fmt.Errorf("no active public key for %s at %s", v.Actor, v.ID)
		}
		if identity.Fingerprint(key) != v.KeyFingerprint || !identity.Verify(key, v.Hash, v.Signature) {
			return fmt.Errorf("signature mismatch at %s", v.ID)
		}
		if v.Type == "agent.rotate-key" {
			p, _ := model.DecodePayload(v.Type, v.Data)
			pub[v.EntityID] = p.(*model.AgentKeyRotated).PublicKey
		}
		prev = v.Hash
	}
	return nil
}
func (s *Store) Recover() error {
	fs, _ := filepath.Glob(filepath.Join(s.runtime(), "tmp", "*.tmp"))
	for _, f := range fs {
		if e := os.Remove(f); e != nil {
			return e
		}
	}
	return nil
}
func (s *Store) Head() string   { x, _ := s.gitOut("rev-parse", "HEAD"); return x }
func (s *Store) Remote() string { x, _ := s.gitOut("remote", "get-url", "origin"); return x }
func (s *Store) Checkpoint() error {
	if s.Remote() == "" {
		return nil
	}
	return s.git("push", "origin", "HEAD")
}

func (s *Store) SetupRemote(url string) error {
	if strings.TrimSpace(url) == "" {
		return errors.New("remote URL is required")
	}
	if s.Remote() != "" {
		return errors.New("origin remote is already configured")
	}
	return s.git("remote", "add", "origin", url)
}

func (s *Store) SyncPull() error {
	if s.Remote() == "" {
		return errors.New("checkpoint remote is not configured")
	}
	if e := s.git("fetch", "origin"); e != nil {
		return e
	}
	branch, e := s.gitOut("branch", "--show-current")
	if e != nil {
		return e
	}
	if branch == "" {
		branch = "main"
	}
	return s.git("merge", "--ff-only", "origin/"+branch)
}
