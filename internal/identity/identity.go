package identity

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	keyring "github.com/zalando/go-keyring"
)

const Service = "agent-comms"

type Credential struct {
	ProjectID  string `json:"project_id"`
	Actor      string `json:"actor"`
	PublicKey  string `json:"public_key"`
	PrivateKey string `json:"private_key"`
}
type Store interface {
	Put(Credential) error
	Get(projectID, actor string) (Credential, error)
	Delete(projectID, actor string) error
}
type KeyringStore struct{}

func account(projectID, actor string) string { return projectID + ":" + actor }
func (KeyringStore) Put(c Credential) error {
	b, e := json.Marshal(c)
	if e != nil {
		return e
	}
	return keyring.Set(Service, account(c.ProjectID, c.Actor), string(b))
}
func (KeyringStore) Get(p, a string) (Credential, error) {
	v, e := keyring.Get(Service, account(p, a))
	if e != nil {
		return Credential{}, e
	}
	var c Credential
	e = json.Unmarshal([]byte(v), &c)
	return c, e
}
func (KeyringStore) Delete(p, a string) error { return keyring.Delete(Service, account(p, a)) }

// FileStore is an explicit headless credential backend. It is enabled only
// through AGENT_COMMS_CREDENTIAL_DIR and must point outside project history.
type FileStore struct{ Dir string }

func (f FileStore) path(p, a string) string { return filepath.Join(f.Dir, p+"--"+a+".json") }
func (f FileStore) Put(c Credential) error {
	if e := os.MkdirAll(f.Dir, 0700); e != nil {
		return e
	}
	b, e := json.Marshal(c)
	if e != nil {
		return e
	}
	return os.WriteFile(f.path(c.ProjectID, c.Actor), b, 0600)
}
func (f FileStore) Get(p, a string) (Credential, error) {
	b, e := os.ReadFile(f.path(p, a))
	if e != nil {
		return Credential{}, e
	}
	var c Credential
	e = json.Unmarshal(b, &c)
	return c, e
}
func (f FileStore) Delete(p, a string) error { return os.Remove(f.path(p, a)) }
func DefaultStore() Store {
	if d := os.Getenv("AGENT_COMMS_CREDENTIAL_DIR"); d != "" {
		return FileStore{Dir: d}
	}
	return KeyringStore{}
}

type MemoryStore struct {
	mu sync.RWMutex
	m  map[string]Credential
}

func NewMemoryStore() *MemoryStore { return &MemoryStore{m: map[string]Credential{}} }
func (m *MemoryStore) Put(c Credential) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.m[account(c.ProjectID, c.Actor)] = c
	return nil
}
func (m *MemoryStore) Get(p, a string) (Credential, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	c, ok := m.m[account(p, a)]
	if !ok {
		return Credential{}, errors.New("credential not found")
	}
	return c, nil
}
func (m *MemoryStore) Delete(p, a string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.m, account(p, a))
	return nil
}
func Generate(projectID, actor string) (Credential, error) {
	pub, priv, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		return Credential{}, e
	}
	return Credential{ProjectID: projectID, Actor: actor, PublicKey: base64.StdEncoding.EncodeToString(pub), PrivateKey: base64.StdEncoding.EncodeToString(priv)}, nil
}
func Sign(c Credential, hash string) (string, error) {
	b, e := base64.StdEncoding.DecodeString(c.PrivateKey)
	if e != nil {
		return "", e
	}
	if len(b) != ed25519.PrivateKeySize {
		return "", errors.New("invalid private key")
	}
	return base64.StdEncoding.EncodeToString(ed25519.Sign(ed25519.PrivateKey(b), []byte(hash))), nil
}
func Verify(publicKey, hash, sig string) bool {
	p, e := base64.StdEncoding.DecodeString(publicKey)
	if e != nil {
		return false
	}
	s, e := base64.StdEncoding.DecodeString(sig)
	return e == nil && ed25519.Verify(ed25519.PublicKey(p), []byte(hash), s)
}
func Fingerprint(publicKey string) string {
	b, _ := base64.StdEncoding.DecodeString(publicKey)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:8])
}

type Profile struct {
	Name        string `json:"name"`
	ProjectID   string `json:"project_id"`
	Actor       string `json:"actor"`
	ProjectRoot string `json:"project_root"`
	HostLabel   string `json:"host_label,omitempty"`
}

// FindProfileByProjectAndHost returns the actor from the single profile
// matching both projectID and hostLabel. If zero or more than one profile
// matches, it returns ok=false rather than guessing which one to use.
func FindProfileByProjectAndHost(profiles map[string]Profile, projectID, hostLabel string) (string, bool) {
	actor, found := "", false
	for _, p := range profiles {
		if p.ProjectID != projectID || p.HostLabel != hostLabel {
			continue
		}
		if found {
			return "", false
		}
		actor, found = p.Actor, true
	}
	return actor, found
}
type UserConfig struct {
	ActiveProfile string             `json:"active_profile,omitempty"`
	UpdateChannel string             `json:"update_channel"`
	CheckUpdates  bool               `json:"check_updates"`
	Theme         string             `json:"theme"`
	Profiles      map[string]Profile `json:"profiles"`
}

func ConfigDir() (string, error) {
	if v := os.Getenv("AGENT_COMMS_CONFIG_DIR"); v != "" {
		return v, nil
	}
	d, e := os.UserConfigDir()
	if e != nil {
		return "", e
	}
	return filepath.Join(d, "agent-comms"), nil
}
func LoadUserConfig() (UserConfig, error) {
	d, e := ConfigDir()
	if e != nil {
		return UserConfig{}, e
	}
	b, e := os.ReadFile(filepath.Join(d, "config.json"))
	if os.IsNotExist(e) {
		return UserConfig{UpdateChannel: "stable", Theme: "auto", Profiles: map[string]Profile{}}, nil
	}
	if e != nil {
		return UserConfig{}, e
	}
	var c UserConfig
	e = json.Unmarshal(b, &c)
	if c.Profiles == nil {
		c.Profiles = map[string]Profile{}
	}
	return c, e
}
func SaveUserConfig(c UserConfig) error {
	d, e := ConfigDir()
	if e != nil {
		return e
	}
	if e = os.MkdirAll(d, 0700); e != nil {
		return e
	}
	b, e := json.MarshalIndent(c, "", "  ")
	if e != nil {
		return e
	}
	tmp := filepath.Join(d, "config.json.tmp")
	if e = os.WriteFile(tmp, append(b, '\n'), 0600); e != nil {
		return e
	}
	return os.Rename(tmp, filepath.Join(d, "config.json"))
}
func DefaultInstallDir() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "AgentComms")
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".local", "bin")
}
func ResolveCredential(s Store, p, a string) (Credential, error) {
	if raw := os.Getenv("AGENT_COMMS_CREDENTIAL"); raw != "" {
		var c Credential
		if e := json.Unmarshal([]byte(raw), &c); e != nil {
			return Credential{}, fmt.Errorf("AGENT_COMMS_CREDENTIAL: %w", e)
		}
		if c.ProjectID != p || c.Actor != a {
			return Credential{}, errors.New("environment credential does not match project and actor")
		}
		return c, nil
	}
	return s.Get(p, a)
}
