package store

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/identity"
	"github.com/DhanushSantosh/AgentComms/internal/onboarding"
)

const Runtime = ".agent-comms"

var (
	RuntimeVersion = "dev"
	RuntimeBuildID = "dev"
)

const (
	ProjectFormatVersion = 1
	ManagedFilesVersion  = 1
)

type Config struct {
	SchemaVersion        string            `json:"schema_version"`
	ToolkitVersion       string            `json:"toolkit_version,omitempty"`
	ToolkitBuildID       string            `json:"toolkit_build_id,omitempty"`
	MinimumToolkit       string            `json:"minimum_toolkit_version,omitempty"`
	ProjectFormatVersion int               `json:"project_format_version,omitempty"`
	ManagedFilesVersion  int               `json:"managed_files_version,omitempty"`
	ManagedFileHashes    map[string]string `json:"managed_file_hashes,omitempty"`
	RuntimeMode          string            `json:"runtime_mode"`
	AuthorityURL         string            `json:"authority_url,omitempty"`
	ServicePublicKey     string            `json:"service_public_key"`
	DaemonEndpoint       string            `json:"daemon_endpoint"`
	ProjectID            string            `json:"project_id"`
	Owner                string            `json:"owner"`
	DefaultLease         string            `json:"default_lease"`
	StaleGrace           string            `json:"stale_grace"`
	ActiveRetention      string            `json:"active_retention"`
	SummaryLimit         int               `json:"summary_limit"`
	ArtifactLimitBytes   int64             `json:"artifact_limit_bytes"`
	RequireReview        bool              `json:"require_review"`
}

type Store struct {
	Root        string
	Now         func() time.Time
	Credentials identity.Store
	LockTimeout time.Duration
}

func Open(root string) *Store {
	return &Store{
		Root: root, Now: func() time.Time { return time.Now().UTC() },
		Credentials: identity.DefaultStore(), LockTimeout: 10 * time.Second,
	}
}

func (s *Store) SetCredentialStore(credentials identity.Store) {
	s.Credentials = credentials
}

func (s *Store) Config() (Config, error) {
	raw, err := os.ReadFile(filepath.Join(s.Root, Runtime, "config.json"))
	if err != nil {
		return Config{}, err
	}
	var config Config
	if err = json.Unmarshal(raw, &config); err != nil {
		return Config{}, err
	}
	return validateConfig(config)
}

func (s *Store) ConfigStrict() (Config, error) {
	raw, err := os.ReadFile(filepath.Join(s.Root, Runtime, "config.json"))
	if err != nil {
		return Config{}, err
	}
	var config Config
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&config); err != nil {
		return Config{}, err
	}
	if err = ensureJSONEOF(decoder); err != nil {
		return Config{}, err
	}
	return validateConfig(config)
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("config contains multiple JSON values")
		}
		return err
	}
	return nil
}

func validateConfig(config Config) (Config, error) {
	if config.RuntimeMode != "personal" && config.RuntimeMode != "service" {
		return Config{}, fmt.Errorf("unsupported runtime mode %q", config.RuntimeMode)
	}
	return config, nil
}

// runtimeIgnoreRules are the project-root entries that keep Agent Comms'
// own bootstrap file and runtime directory out of the host repository's
// tracked/untracked-file surface.
var runtimeIgnoreRules = []string{"/.agents", "/.agent-comms/"}

// EnsureRuntimeHidden adds runtimeIgnoreRules to the project's .gitignore
// if the project root is a Git repository, creating the file if the
// project doesn't already have one -- so a freshly `git init`'d directory
// with no .gitignore yet still never shows Agent Comms' own bootstrap file
// or runtime directory as untracked after `init` runs. A prior version of
// this only wrote to the local, per-clone .git/info/exclude and only
// covered /.agent-comms/, which meant .agents always showed up untracked
// and the exclusion never traveled with the repo for other clones/
// collaborators; both gaps are why this now edits the real .gitignore.
func (s *Store) EnsureRuntimeHidden() error {
	if _, err := os.Stat(filepath.Join(s.Root, ".git")); err != nil {
		return nil
	}
	gitignorePath := filepath.Join(s.Root, ".gitignore")
	content, err := os.ReadFile(gitignorePath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read .gitignore: %w", err)
	}
	existing := map[string]bool{}
	for _, line := range strings.Split(string(content), "\n") {
		existing[strings.TrimSpace(line)] = true
	}
	var missing []string
	for _, rule := range runtimeIgnoreRules {
		if !existing[rule] {
			missing = append(missing, rule)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	if len(content) > 0 && content[len(content)-1] != '\n' {
		content = append(content, '\n')
	}
	for _, rule := range missing {
		content = append(content, rule...)
		content = append(content, '\n')
	}
	if err = os.WriteFile(gitignorePath, content, 0o644); err != nil {
		return fmt.Errorf("update .gitignore: %w", err)
	}
	return nil
}

func ServiceBootstrap() []byte {
	return []byte("# Agent Comms managed bootstrap\nruntime = .agent-comms\nmode = service\ninstructions = .agent-comms/AGENT_INSTRUCTIONS.md\n")
}

func PersonalBootstrap() []byte {
	return []byte("# Agent Comms managed bootstrap\nruntime = .agent-comms\nmode = personal\ninstructions = .agent-comms/AGENT_INSTRUCTIONS.md\n")
}

func AgentInstructions() []byte {
	rendered, err := onboarding.Render(onboarding.StaticData(""))
	if err != nil {
		// onboarding.Render only fails on a malformed embedded template,
		// which is a build-time invariant, not a runtime condition —
		// there is no recoverable fallback path worth writing for it.
		panic(err)
	}
	return []byte(rendered)
}

func ManagedFiles(config Config) map[string][]byte {
	bootstrap := PersonalBootstrap()
	if config.RuntimeMode == "service" {
		bootstrap = ServiceBootstrap()
	}
	return map[string][]byte{
		".agents": bootstrap,
		filepath.Join(Runtime, "AGENT_INSTRUCTIONS.md"): AgentInstructions(),
		filepath.Join(Runtime, ".gitignore"):            []byte("cache/\ntmp/\n"),
	}
}

func ManagedHashes(config Config) map[string]string {
	result := make(map[string]string)
	for path, content := range ManagedFiles(config) {
		sum := sha256.Sum256(content)
		result[filepath.ToSlash(path)] = fmt.Sprintf("%x", sum[:])
	}
	return result
}

func (s *Store) ManagedBootstrapValid() bool {
	config, err := s.Config()
	if err != nil {
		return false
	}
	actual, err := os.ReadFile(filepath.Join(s.Root, ".agents"))
	if err != nil {
		return false
	}
	expected := PersonalBootstrap()
	if config.RuntimeMode == "service" {
		expected = ServiceBootstrap()
	}
	return bytes.Equal(actual, expected)
}

func (s *Store) InstructionsPresent() bool {
	info, err := os.Stat(filepath.Join(s.Root, Runtime, "AGENT_INSTRUCTIONS.md"))
	return err == nil && info.Mode().IsRegular() && info.Size() > 0
}
