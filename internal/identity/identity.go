package identity

import (
	"crypto/aes"
	"crypto/cipher"
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
	"sort"
	"strings"
	"sync"
	"time"

	keyring "github.com/zalando/go-keyring"
	"golang.org/x/crypto/argon2"
)

const Service = "agent-comms"

const (
	hostIDFileName = "host-id"
	hostIDBytes    = 16
)

type Credential struct {
	ProjectID  string `json:"project_id"`
	Actor      string `json:"actor"`
	PublicKey  string `json:"public_key"`
	PrivateKey string `json:"private_key"`
	// Encrypted, Salt, and Nonce are set only for a passphrase-protected
	// elevated credential (see GenerateEncrypted/Decrypted). When Encrypted
	// is true, PrivateKey holds AES-256-GCM ciphertext, not a usable key.
	Encrypted bool   `json:"encrypted,omitempty"`
	Salt      string `json:"salt,omitempty"`
	Nonce     string `json:"nonce,omitempty"`
}

// ElevatedActor is the credential-store account name for actor's elevated
// key, distinct from its everyday primary credential (stored under actor
// itself). Store.Get/Put/Delete take this as the actor parameter directly —
// no Store interface changes needed.
func ElevatedActor(actor string) string { return actor + ":elevated" }

const (
	argon2Time      = 3
	argon2MemoryKiB = 64 * 1024
	argon2Threads   = 4
	argon2KeyLen    = 32
	saltSize        = 16
)

func deriveKey(passphrase string, salt []byte) []byte {
	return argon2.IDKey([]byte(passphrase), salt, argon2Time, argon2MemoryKiB, argon2Threads, argon2KeyLen)
}

// GenerateEncrypted creates a fresh Ed25519 keypair whose private key is
// encrypted at rest with passphrase (Argon2id key derivation + AES-256-GCM).
// Recovering the raw key requires calling Decrypted with the same
// passphrase; there is no other recovery path — this is the point.
func GenerateEncrypted(projectID, actor, passphrase string) (Credential, error) {
	pub, priv, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		return Credential{}, e
	}
	c := Credential{ProjectID: projectID, Actor: actor, PublicKey: base64.StdEncoding.EncodeToString(pub)}
	return c.encrypt(priv, passphrase)
}

func (c Credential) encrypt(rawPrivateKey ed25519.PrivateKey, passphrase string) (Credential, error) {
	salt := make([]byte, saltSize)
	if _, e := rand.Read(salt); e != nil {
		return Credential{}, e
	}
	gcm, e := newGCM(deriveKey(passphrase, salt))
	if e != nil {
		return Credential{}, e
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, e := rand.Read(nonce); e != nil {
		return Credential{}, e
	}
	c.Encrypted = true
	c.Salt = base64.StdEncoding.EncodeToString(salt)
	c.Nonce = base64.StdEncoding.EncodeToString(nonce)
	c.PrivateKey = base64.StdEncoding.EncodeToString(gcm.Seal(nil, nonce, rawPrivateKey, nil))
	return c, nil
}

// Decrypted returns a copy of c with PrivateKey replaced by the decrypted
// raw Ed25519 private key (base64), ready for Sign/command.Sign. A no-op
// (returns c unchanged) if c isn't Encrypted. A wrong passphrase fails via
// AES-GCM's built-in authentication check, not a plausible-looking garbage
// key.
func (c Credential) Decrypted(passphrase string) (Credential, error) {
	if !c.Encrypted {
		return c, nil
	}
	salt, e := base64.StdEncoding.DecodeString(c.Salt)
	if e != nil {
		return Credential{}, fmt.Errorf("invalid salt: %w", e)
	}
	nonce, e := base64.StdEncoding.DecodeString(c.Nonce)
	if e != nil {
		return Credential{}, fmt.Errorf("invalid nonce: %w", e)
	}
	ciphertext, e := base64.StdEncoding.DecodeString(c.PrivateKey)
	if e != nil {
		return Credential{}, fmt.Errorf("invalid ciphertext: %w", e)
	}
	gcm, e := newGCM(deriveKey(passphrase, salt))
	if e != nil {
		return Credential{}, e
	}
	raw, e := gcm.Open(nil, nonce, ciphertext, nil)
	if e != nil {
		return Credential{}, errors.New("incorrect passphrase")
	}
	c.PrivateKey = base64.StdEncoding.EncodeToString(raw)
	c.Encrypted, c.Salt, c.Nonce = false, "", ""
	return c, nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, e := aes.NewCipher(key)
	if e != nil {
		return nil, e
	}
	return cipher.NewGCM(block)
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

const (
	ActorSourceFlag           = "actor_flag"
	ActorSourceProfileFlag    = "profile_flag"
	ActorSourceEnvironment    = "actor_environment"
	ActorSourceHostBinding    = "host_binding"
	ActorSourceSessionProfile = "session_profile"
	ActorSourceActiveProfile  = "active_profile"
	ActorSourceProjectOwner   = "project_owner"
)

type ActorResolutionRequest struct {
	ProjectID    string
	ProjectOwner string
	// ProviderSessionID scopes the active-profile lookup to one specific
	// agent/human session (see UserConfig.ActiveProfileFor) instead of the
	// single machine-wide legacy default -- see RFC 0016. Typically
	// sessionbind.Capture()'s first return value; empty for a plain
	// interactive terminal with no recognized provider session.
	ProviderSessionID string
	ExplicitActor     string
	ExplicitProfile   string
	EnvironmentActor  string
	HostLabel         string
	UserConfig        UserConfig
}

type ActorResolution struct {
	Actor     string `json:"actor"`
	Source    string `json:"source"`
	Profile   string `json:"profile,omitempty"`
	HostLabel string `json:"host_label,omitempty"`
	ProjectID string `json:"project_id"`
}

func ResolveActor(request ActorResolutionRequest) (ActorResolution, error) {
	request.ExplicitActor = strings.TrimSpace(request.ExplicitActor)
	request.ExplicitProfile = strings.TrimSpace(request.ExplicitProfile)
	request.EnvironmentActor = strings.TrimSpace(request.EnvironmentActor)
	request.HostLabel = strings.TrimSpace(request.HostLabel)
	if request.ProjectID == "" || request.ProjectOwner == "" {
		return ActorResolution{}, errors.New("project ID and owner are required for actor resolution")
	}
	result := ActorResolution{ProjectID: request.ProjectID, HostLabel: request.HostLabel}
	if request.ExplicitActor != "" {
		result.Actor, result.Source = request.ExplicitActor, ActorSourceFlag
		return result, nil
	}
	if request.ExplicitProfile != "" {
		profile, found := request.UserConfig.Profiles[request.ExplicitProfile]
		if !found {
			return ActorResolution{}, fmt.Errorf("profile %q not found", request.ExplicitProfile)
		}
		if profile.ProjectID != request.ProjectID {
			return ActorResolution{}, fmt.Errorf(
				"profile %q belongs to project %s, not %s",
				request.ExplicitProfile, profile.ProjectID, request.ProjectID,
			)
		}
		result.Actor, result.Source, result.Profile = profile.Actor, ActorSourceProfileFlag, request.ExplicitProfile
		return result, nil
	}
	if request.EnvironmentActor != "" {
		result.Actor, result.Source = request.EnvironmentActor, ActorSourceEnvironment
		return result, nil
	}
	if request.HostLabel != "" {
		matches := ProfilesByProjectAndHost(request.UserConfig.Profiles, request.ProjectID, request.HostLabel)
		if len(matches) > 1 {
			return ActorResolution{}, fmt.Errorf(
				"host label %q is bound to multiple actors in project %s; use --actor or --profile",
				request.HostLabel, request.ProjectID,
			)
		}
		if len(matches) == 1 {
			result.Actor, result.Source, result.Profile = matches[0].Actor, ActorSourceHostBinding, matches[0].Name
			return result, nil
		}
		result.Actor, result.Source = request.ProjectOwner, ActorSourceProjectOwner
		return result, nil
	}
	if name := request.UserConfig.ActiveProfileFor(request.ProviderSessionID); name != "" {
		profile, found := request.UserConfig.Profiles[name]
		if found && profile.ProjectID == request.ProjectID {
			source := ActorSourceActiveProfile
			if request.ProviderSessionID != "" {
				source = ActorSourceSessionProfile
			}
			result.Actor, result.Source, result.Profile = profile.Actor, source, name
			return result, nil
		}
	}
	result.Actor, result.Source = request.ProjectOwner, ActorSourceProjectOwner
	return result, nil
}

func ProfilesByProjectAndHost(profiles map[string]Profile, projectID, hostLabel string) []Profile {
	matches := make([]Profile, 0)
	for _, profile := range profiles {
		if profile.ProjectID == projectID && profile.HostLabel == hostLabel {
			matches = append(matches, profile)
		}
	}
	sort.Slice(matches, func(left, right int) bool {
		return matches[left].Name < matches[right].Name
	})
	return matches
}

// FindProfileByProjectAndHost returns the actor from the single profile
// matching both projectID and hostLabel. If zero or more than one profile
// matches, it returns ok=false rather than guessing which one to use.
func FindProfileByProjectAndHost(profiles map[string]Profile, projectID, hostLabel string) (string, bool) {
	matches := ProfilesByProjectAndHost(profiles, projectID, hostLabel)
	if len(matches) != 1 {
		return "", false
	}
	return matches[0].Actor, true
}

type UserConfig struct {
	ActiveProfile string `json:"active_profile,omitempty"`
	// ActiveProfileBySession scopes "which local profile is active by
	// default" to the exact agent/human session that set it, keyed by a
	// provider session ID (see DetectProviderSessionID) -- see RFC 0016.
	// ActiveProfile alone is a single value in one file shared by every
	// process under this OS account: one agent session running
	// `profile use` for its own ordinary convenience ("default my own
	// commands to me") silently redirected every OTHER concurrent
	// session's default actor too, including genuine signing, since every
	// registered actor's real private key lives in the same shared OS
	// keyring (KeyringStore). ActiveProfile remains the fallback used only
	// when no recognized session ID is present at all -- a genuine plain
	// interactive terminal, which never had this problem in the first
	// place and keeps behaving exactly as before.
	ActiveProfileBySession map[string]SessionProfile `json:"active_profile_by_session,omitempty"`
	UpdateChannel          string                    `json:"update_channel"`
	CheckUpdates           bool                      `json:"check_updates"`
	Theme                  string                    `json:"theme"`
	Profiles               map[string]Profile        `json:"profiles"`
}

// SessionProfile is one entry in UserConfig.ActiveProfileBySession: which
// profile a specific session made active, and when. The timestamp lets a
// long-finished session's entry be pruned instead of accumulating forever
// across every agent conversation ever run on the machine.
type SessionProfile struct {
	Profile string    `json:"profile"`
	SetAt   time.Time `json:"set_at"`
}

// sessionProfileTTL bounds how long an unrefreshed session-scoped active
// profile is honored before being treated as stale and pruned -- long
// enough that an actual ongoing agent conversation never loses it (the
// entry refreshes on every profile-changing call, not just once), short
// enough that a session that's genuinely over doesn't leave a phantom
// entry around indefinitely.
const sessionProfileTTL = 30 * 24 * time.Hour

// ActiveProfileFor returns the profile name active for sessionID -- the
// legacy machine-wide ActiveProfile only when sessionID is empty (no
// recognized provider session in the environment, i.e. a genuine plain
// interactive terminal). A real, recognized session with no profile of
// its own set yet returns "" rather than falling through to the shared
// legacy field: that fallthrough is exactly the cross-session leak this
// type exists to close, so a session must resolve to the safe
// project-owner default until it explicitly sets its own.
func (c UserConfig) ActiveProfileFor(sessionID string) string {
	if sessionID == "" {
		return c.ActiveProfile
	}
	entry, ok := c.ActiveProfileBySession[sessionID]
	if !ok || time.Since(entry.SetAt) > sessionProfileTTL {
		return ""
	}
	return entry.Profile
}

// SetActiveProfileFor sets the active profile for sessionID (the legacy
// machine-wide field when sessionID is empty), and opportunistically
// prunes any other session entries that have aged past sessionProfileTTL
// so ActiveProfileBySession doesn't grow forever.
func (c *UserConfig) SetActiveProfileFor(sessionID, profile string) {
	if sessionID == "" {
		c.ActiveProfile = profile
		return
	}
	if c.ActiveProfileBySession == nil {
		c.ActiveProfileBySession = map[string]SessionProfile{}
	}
	now := time.Now().UTC()
	for id, entry := range c.ActiveProfileBySession {
		if id != sessionID && now.Sub(entry.SetAt) > sessionProfileTTL {
			delete(c.ActiveProfileBySession, id)
		}
	}
	c.ActiveProfileBySession[sessionID] = SessionProfile{Profile: profile, SetAt: now}
}

// ProfileCountForProject counts locally-saved profiles scoped to projectID
// -- Profiles spans every project ever touched on this machine, so this
// filters rather than just returning len(Profiles). Used to decide whether
// the legacy ActiveProfile fallback is genuinely ambiguous for a given
// project (2+ locally-registered identities to silently choose between) or
// safe (0-1, nothing else it could reasonably mean) -- see RFC 0017.
func (c UserConfig) ProfileCountForProject(projectID string) int {
	count := 0
	for _, p := range c.Profiles {
		if p.ProjectID == projectID {
			count++
		}
	}
	return count
}

// DetectProviderSessionID checks the current process environment for the
// two provider session variables their respective CLIs guarantee to
// inject into every subprocess they spawn -- CLAUDE_CODE_SESSION_ID and
// CODEX_THREAD_ID -- not something a caller has to remember to export.
// This is the subset internal/sessionbind.Capture also checks (and now
// delegates to for these two); kept here too, rather than imported,
// specifically so internal/identity (a zero-internal-dependency leaf
// package) and packages like internal/service that cannot import
// internal/sessionbind without an import cycle through internal/worker
// can use it without pulling in sessionbind's broader
// declarative-adapter detection. See RFC 0016.
func DetectProviderSessionID() string {
	if id := strings.TrimSpace(os.Getenv("CLAUDE_CODE_SESSION_ID")); id != "" {
		return id
	}
	if id := strings.TrimSpace(os.Getenv("CODEX_THREAD_ID")); id != "" {
		return id
	}
	return ""
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

func LoadOrCreateHostID() (string, error) {
	directory, err := ConfigDir()
	if err != nil {
		return "", err
	}
	if err = os.MkdirAll(directory, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(directory, hostIDFileName)
	read := func() (string, error) { return readHostID(path, true) }
	if value, readErr := read(); readErr == nil {
		return value, nil
	} else if !os.IsNotExist(readErr) {
		return "", readErr
	}
	random := make([]byte, hostIDBytes)
	if _, err = rand.Read(random); err != nil {
		return "", err
	}
	value := hex.EncodeToString(random)
	file, createErr := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if createErr != nil {
		if os.IsExist(createErr) {
			return read()
		}
		return "", createErr
	}
	if _, err = file.WriteString(value + "\n"); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return "", err
	}
	if closeErr != nil {
		return "", closeErr
	}
	return value, nil
}

func LoadHostID() (string, error) {
	directory, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return readHostID(filepath.Join(directory, hostIDFileName), false)
}

func readHostID(path string, securePermissions bool) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	value := strings.TrimSpace(string(raw))
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != hostIDBytes {
		return "", errors.New("agent-comms host ID is malformed")
	}
	if securePermissions {
		if err = os.Chmod(path, 0o600); err != nil {
			return "", err
		}
	}
	return value, nil
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
