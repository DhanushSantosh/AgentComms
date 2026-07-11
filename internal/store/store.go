package store

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/model"
)

const Runtime = ".agent-comms"

var processMu sync.Mutex

type Store struct {
	Root string
	Now  func() time.Time
}

func Open(root string) *Store {
	return &Store{Root: root, Now: func() time.Time { return time.Now().UTC() }}
}
func (s *Store) runtime() string { return filepath.Join(s.Root, Runtime) }

func (s *Store) Init(owner string) error {
	if owner == "" {
		owner = "owner"
	}
	r := s.runtime()
	if _, err := os.Stat(r); err == nil {
		return errors.New("runtime already initialized")
	}
	for _, d := range []string{"events", "artifacts/sha256", "tmp", "cache", "schemas"} {
		if err := os.MkdirAll(filepath.Join(r, d), 0700); err != nil {
			return err
		}
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(r, "signing.key"), []byte(base64.StdEncoding.EncodeToString(priv)), 0600); err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(r, "signing.pub"), []byte(base64.StdEncoding.EncodeToString(pub)), 0644); err != nil {
		return err
	}
	cfg := map[string]any{"schema_version": model.SchemaVersion, "owner": owner, "default_lease": "4h", "stale_grace": "1h", "active_retention": "168h", "summary_limit": 1200, "artifact_limit_bytes": 5242880}
	b, _ := json.MarshalIndent(cfg, "", "  ")
	if err = os.WriteFile(filepath.Join(r, "config.json"), append(b, '\n'), 0644); err != nil {
		return err
	}
	bootstrap := []byte("# Agent Comms bootstrap\n.runtime = .agent-comms\n")
	if err = os.WriteFile(filepath.Join(s.Root, ".agents"), bootstrap, 0644); err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(r, ".gitignore"), []byte("tmp/\ncache/\nsigning.key\n"), 0644); err != nil {
		return err
	}
	if err = s.git("init"); err != nil {
		return err
	}
	_ = s.git("config", "user.name", "Agent Comms")
	_ = s.git("config", "user.email", "agent-comms@localhost")
	_ = s.git("config", "commit.gpgsign", "false")
	if err = s.git("add", "."); err != nil {
		return err
	}
	return s.git("commit", "-m", "Initialize Agent Comms runtime")
}

func (s *Store) git(args ...string) error {
	c := exec.Command("git", args...)
	c.Dir = s.runtime()
	var e bytes.Buffer
	c.Stderr = &e
	if err := c.Run(); err != nil {
		return fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(e.String()))
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
func (s *Store) Append(actor, typ, entity string, data map[string]any) (model.Event, error) {
	processMu.Lock()
	defer processMu.Unlock()
	if err := s.Recover(); err != nil {
		return model.Event{}, err
	}
	events, err := s.Events()
	if err != nil {
		return model.Event{}, err
	}
	var seq uint64 = 1
	prev := ""
	if len(events) > 0 {
		seq = events[len(events)-1].Sequence + 1
		prev = events[len(events)-1].Hash
	}
	e := model.Event{SchemaVersion: model.SchemaVersion, ID: fmt.Sprintf("evt-%020d", seq), Sequence: seq, Time: s.Now(), Actor: actor, Type: typ, EntityID: entity, Data: data, PreviousHash: prev}
	c, err := canonical(e)
	if err != nil {
		return e, err
	}
	h := sha256.Sum256(c)
	e.Hash = hex.EncodeToString(h[:])
	kb, err := os.ReadFile(filepath.Join(s.runtime(), "signing.key"))
	if err != nil {
		return e, err
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(kb)))
	if err != nil {
		return e, err
	}
	e.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(ed25519.PrivateKey(raw), []byte(e.Hash)))
	b, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return e, err
	}
	tmp := filepath.Join(s.runtime(), "tmp", e.ID+".json.tmp")
	dst := filepath.Join(s.runtime(), "events", e.ID+".json")
	if err = os.WriteFile(tmp, append(b, '\n'), 0600); err != nil {
		return e, err
	}
	f, err := os.OpenFile(tmp, os.O_RDWR, 0600)
	if err != nil {
		return e, err
	}
	err = f.Sync()
	_ = f.Close()
	if err != nil {
		return e, err
	}
	if err = os.Rename(tmp, dst); err != nil {
		return e, err
	}
	if err = s.git("add", filepath.ToSlash(filepath.Join("events", e.ID+".json"))); err != nil {
		return e, err
	}
	if typ == "artifact.add" {
		if err = s.git("add", filepath.ToSlash(filepath.Join("artifacts", "sha256", entity))); err != nil {
			return e, err
		}
	}
	if err = s.git("commit", "--no-gpg-sign", "-m", fmt.Sprintf("%s %s", typ, entity)); err != nil {
		return e, err
	}
	return e, nil
}
func (s *Store) Events() ([]model.Event, error) {
	files, err := filepath.Glob(filepath.Join(s.runtime(), "events", "*.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	out := make([]model.Event, 0, len(files))
	for _, f := range files {
		b, e := os.ReadFile(f)
		if e != nil {
			return nil, e
		}
		var v model.Event
		if e = json.Unmarshal(b, &v); e != nil {
			return nil, fmt.Errorf("%s: %w", f, e)
		}
		out = append(out, v)
	}
	return out, nil
}
func (s *Store) Verify() error {
	ev, err := s.Events()
	if err != nil {
		return err
	}
	pb, err := os.ReadFile(filepath.Join(s.runtime(), "signing.pub"))
	if err != nil {
		return err
	}
	pub, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(pb)))
	if err != nil {
		return err
	}
	prev := ""
	for i, e := range ev {
		if e.Sequence != uint64(i+1) || e.PreviousHash != prev {
			return fmt.Errorf("chain discontinuity at %s", e.ID)
		}
		c, _ := canonical(e)
		h := sha256.Sum256(c)
		hs := hex.EncodeToString(h[:])
		if hs != e.Hash {
			return fmt.Errorf("hash mismatch at %s", e.ID)
		}
		sig, x := base64.StdEncoding.DecodeString(e.Signature)
		if x != nil || !ed25519.Verify(pub, []byte(e.Hash), sig) {
			return fmt.Errorf("signature mismatch at %s", e.ID)
		}
		prev = e.Hash
	}
	return nil
}
func (s *Store) Recover() error {
	files, _ := filepath.Glob(filepath.Join(s.runtime(), "tmp", "*.tmp"))
	for _, f := range files {
		if err := os.Remove(f); err != nil {
			return err
		}
	}
	return nil
}
func (s *Store) Checkpoint() error {
	if _, err := s.gitOut("remote"); err != nil {
		return err
	}
	rem, _ := s.gitOut("remote")
	if strings.TrimSpace(rem) == "" {
		return nil
	}
	return s.git("push")
}
func (s *Store) Head() string { x, _ := s.gitOut("rev-parse", "HEAD"); return x }
func SequenceFromID(id string) uint64 {
	n, _ := strconv.ParseUint(strings.TrimPrefix(id, "evt-"), 10, 64)
	return n
}
