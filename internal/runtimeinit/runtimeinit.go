package runtimeinit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/controlplane"
	"github.com/DhanushSantosh/AgentComms/internal/identity"
	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/DhanushSantosh/AgentComms/internal/personalauthority"
	"github.com/DhanushSantosh/AgentComms/internal/remote"
	"github.com/DhanushSantosh/AgentComms/internal/store"
	"github.com/google/uuid"
)

const personalAuthorityActor = "__personal_authority__"

type Config struct {
	ProjectRoot      string
	Owner            string
	Mode             string
	AuthorityURL     string
	ServicePublicKey string
	DaemonEndpoint   string
}

type Result struct {
	ProjectID      string `json:"project_id"`
	RuntimeMode    string `json:"runtime_mode"`
	Database       string `json:"database,omitempty"`
	AuthorityURL   string `json:"authority_url,omitempty"`
	DaemonEndpoint string `json:"daemon_endpoint"`
}

func DatabasePath(projectRoot string) string {
	return filepath.Join(projectRoot, store.Runtime, "cache", "personal-authority.db")
}

func ProjectionPath(projectRoot string) string {
	return filepath.Join(projectRoot, store.Runtime, "cache", "personal-projection.db")
}

func DraftPath(projectRoot string) string {
	return filepath.Join(projectRoot, store.Runtime, "data", "drafts.db")
}

func Initialize(ctx context.Context, config Config) (Result, error) {
	config.ProjectRoot = filepath.Clean(config.ProjectRoot)
	config.Owner = strings.TrimSpace(config.Owner)
	config.Mode = strings.ToLower(strings.TrimSpace(config.Mode))
	if config.ProjectRoot == "" || config.Owner == "" {
		return Result{}, errors.New("project root and owner are required")
	}
	if config.Mode != "personal" && config.Mode != "service" {
		return Result{}, errors.New("runtime mode must be personal or service")
	}
	runtimePath := filepath.Join(config.ProjectRoot, store.Runtime)
	if _, err := os.Stat(runtimePath); err == nil {
		return Result{}, errors.New("runtime is already initialized for Agent Comms")
	} else if !os.IsNotExist(err) {
		return Result{}, fmt.Errorf("inspect runtime: %w", err)
	}
	bootstrapPath := filepath.Join(config.ProjectRoot, ".agents")
	if _, err := os.Lstat(bootstrapPath); err == nil {
		return Result{}, errors.New(".agents already exists; remove or rename it before initializing Agent Comms")
	} else if !os.IsNotExist(err) {
		return Result{}, fmt.Errorf("inspect .agents: %w", err)
	}

	projectID := "ac-" + uuid.NewString()
	if config.DaemonEndpoint == "" {
		config.DaemonEndpoint = DaemonEndpoint(config.ProjectRoot, projectID)
	}
	ownerCredential, err := identity.Generate(projectID, config.Owner)
	if err != nil {
		return Result{}, err
	}
	credentialStore := identity.DefaultStore()
	if err = credentialStore.Put(ownerCredential); err != nil {
		return Result{}, fmt.Errorf("store owner credential: %w", err)
	}
	keepOwnerCredential := false
	keepAuthorityCredential := false
	defer func() {
		if !keepOwnerCredential {
			_ = credentialStore.Delete(projectID, config.Owner)
		}
		if config.Mode == "personal" && !keepAuthorityCredential {
			_ = credentialStore.Delete(projectID, personalAuthorityActor)
		}
	}()

	stagePath := filepath.Join(config.ProjectRoot, "."+strings.TrimPrefix(store.Runtime, ".")+"-init-"+uuid.NewString())
	if err = os.MkdirAll(filepath.Join(stagePath, "cache"), 0o700); err != nil {
		return Result{}, err
	}
	defer os.RemoveAll(stagePath)

	runtimeConfig := store.Config{
		SchemaVersion: model.SchemaVersion, ToolkitVersion: store.RuntimeVersion,
		ToolkitBuildID:       store.RuntimeBuildID,
		ProjectFormatVersion: store.ProjectFormatVersion, ManagedFilesVersion: store.ManagedFilesVersion,
		RuntimeMode: config.Mode, AuthorityURL: config.AuthorityURL,
		ServicePublicKey: config.ServicePublicKey, DaemonEndpoint: config.DaemonEndpoint,
		ProjectID: projectID, Owner: config.Owner, DefaultLease: "4h", StaleGrace: "1h",
		ActiveRetention: "168h", SummaryLimit: 1200, ArtifactLimitBytes: 5 * 1024 * 1024,
	}
	runtimeConfig.MinimumToolkit = store.RuntimeVersion
	runtimeConfig.ManagedFileHashes = store.ManagedHashes(runtimeConfig)
	result := Result{ProjectID: projectID, RuntimeMode: config.Mode, DaemonEndpoint: config.DaemonEndpoint}
	switch config.Mode {
	case "personal":
		databasePath := filepath.Join(stagePath, "cache", "personal-authority.db")
		publicKey, initializeErr := initializePersonal(ctx, databasePath, projectID, config.Owner, ownerCredential, credentialStore)
		if initializeErr != nil {
			return Result{}, initializeErr
		}
		runtimeConfig.ServicePublicKey = publicKey
		result.Database = filepath.Join(runtimePath, "cache", "personal-authority.db")
	case "service":
		if config.AuthorityURL == "" || config.ServicePublicKey == "" {
			return Result{}, errors.New("service mode requires authority URL and service public key")
		}
		if err = initializeService(ctx, config.AuthorityURL, projectID, config.Owner, ownerCredential); err != nil {
			return Result{}, err
		}
		result.AuthorityURL = config.AuthorityURL
	}

	if err = writeRuntimeFiles(stagePath, runtimeConfig); err != nil {
		return Result{}, err
	}
	if err = os.Rename(stagePath, runtimePath); err != nil {
		return Result{}, fmt.Errorf("publish runtime: %w", err)
	}
	published := true
	defer func() {
		if published {
			return
		}
		_ = os.RemoveAll(runtimePath)
	}()
	projectStore := store.Open(config.ProjectRoot)
	if err = projectStore.EnsureRuntimeHidden(); err != nil {
		published = false
		return Result{}, err
	}
	bootstrap := store.PersonalBootstrap()
	if config.Mode == "service" {
		bootstrap = store.ServiceBootstrap()
	}
	if err = writeExclusive(bootstrapPath, bootstrap, 0o644); err != nil {
		published = false
		return Result{}, fmt.Errorf("publish bootstrap: %w", err)
	}
	if err = saveProfile(config.ProjectRoot, projectID, config.Owner); err != nil {
		_ = os.Remove(bootstrapPath)
		published = false
		return Result{}, fmt.Errorf("save owner profile: %w", err)
	}
	keepOwnerCredential = true
	keepAuthorityCredential = true
	return result, nil
}

func initializePersonal(ctx context.Context, databasePath, projectID, owner string, ownerCredential identity.Credential, credentials identity.Store) (string, error) {
	authorityCredential, err := identity.Generate(projectID, personalAuthorityActor)
	if err != nil {
		return "", err
	}
	if err = credentials.Put(authorityCredential); err != nil {
		return "", fmt.Errorf("store personal authority credential: %w", err)
	}
	keepCredential := false
	defer func() {
		if !keepCredential {
			_ = credentials.Delete(projectID, personalAuthorityActor)
		}
	}()
	signer, err := controlplane.NewSigner(authorityCredential.PrivateKey)
	if err != nil {
		return "", err
	}
	engine, err := personalauthority.Open(databasePath, signer)
	if err != nil {
		return "", err
	}
	defer engine.Close()
	if err = engine.CreateProject(ctx, projectID, owner); err != nil {
		return "", err
	}
	for _, command := range initialCommands(projectID, owner, ownerCredential) {
		if _, _, err = engine.Command(ctx, command); err != nil {
			return "", fmt.Errorf("initialize personal authority: %w", err)
		}
	}
	keepCredential = true
	return signer.PublicKey(), nil
}

func initializeService(ctx context.Context, authorityURL, projectID, owner string, ownerCredential identity.Credential) error {
	client, err := remote.New(authorityURL, controlplane.DefaultRequestTimeout)
	if err != nil {
		return err
	}
	if err = client.CreateProject(ctx, projectID, owner); err != nil {
		return fmt.Errorf("create authoritative project: %w", err)
	}
	for _, command := range initialCommands(projectID, owner, ownerCredential) {
		if _, _, err = client.Command(ctx, command); err != nil {
			return fmt.Errorf("initialize authoritative project: %w", err)
		}
	}
	return nil
}

func initialCommands(projectID, owner string, credential identity.Credential) []controlplane.Command {
	payloads := []struct {
		eventType string
		payload   any
	}{
		{"agent.register", model.AgentRegistered{PublicKey: credential.PublicKey, PrincipalType: model.PrincipalHuman, DisplayName: owner}},
		{"agent.activate", model.AgentActivated{Role: model.RoleOwner, Capabilities: []string{"*"}, Scopes: []string{"*"}}},
	}
	commands := make([]controlplane.Command, 0, len(payloads))
	for _, item := range payloads {
		payload, _ := model.EncodePayload(item.eventType, item.payload)
		command := controlplane.Command{
			ProjectID: projectID, Actor: owner, Type: item.eventType, EntityID: owner,
			Payload: payload, IdempotencyKey: uuid.NewString(), IssuedAt: time.Now().UTC(),
		}
		if item.eventType == "agent.register" {
			command.PublicKey = credential.PublicKey
		}
		_ = command.Sign(credential.PrivateKey)
		commands = append(commands, command)
	}
	return commands
}

func writeRuntimeFiles(runtimePath string, config store.Config) error {
	for _, directory := range []string{"artifacts/sha256", "cache", "data", "tmp"} {
		if err := os.MkdirAll(filepath.Join(runtimePath, directory), 0o700); err != nil {
			return err
		}
	}
	configJSON, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(runtimePath, "config.json"), append(configJSON, '\n'), 0o600); err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(runtimePath, "AGENT_INSTRUCTIONS.md"), store.AgentInstructions(), 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(runtimePath, ".gitignore"), []byte("cache/\ntmp/\n"), 0o644)
}

func writeExclusive(path string, data []byte, mode os.FileMode) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err = file.Write(data); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	return err
}

func saveProfile(projectRoot, projectID, owner string) error {
	userConfig, err := identity.LoadUserConfig()
	if err != nil {
		return err
	}
	name := projectID + ":" + owner
	userConfig.ActiveProfile = name
	userConfig.Profiles[name] = identity.Profile{
		Name: name, ProjectID: projectID, Actor: owner, ProjectRoot: projectRoot,
	}
	return identity.SaveUserConfig(userConfig)
}

func DaemonEndpoint(projectRoot, projectID string) string {
	if runtime.GOOS == "windows" {
		return `\\.\pipe\agent-comms-` + projectID
	}
	return filepath.Join(os.TempDir(), "agent-comms", projectID+".sock")
}
