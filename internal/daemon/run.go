package daemon

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/buildinfo"
	"github.com/DhanushSantosh/AgentComms/internal/controlplane"
	"github.com/DhanushSantosh/AgentComms/internal/draftstore"
	"github.com/DhanushSantosh/AgentComms/internal/localcache"
	"github.com/DhanushSantosh/AgentComms/internal/personalauthority"
	"github.com/DhanushSantosh/AgentComms/internal/remote"
	"github.com/DhanushSantosh/AgentComms/internal/store"
)

const daemonShutdownTimeout = 10 * time.Second

type RunConfig struct {
	AuthorityURL         string
	ServicePublicKey     string
	CachePath            string
	Endpoint             string
	ConnectorConfigPath  string
	RuntimeMode          string
	PersonalDatabase     string
	ServicePrivateKey    string
	ProjectID            string
	ProductVersion       string
	BuildID              string
	ProjectFormatVersion int
	CacheSchemaVersion   int
	DraftSchemaVersion   int
	DraftPath            string
}

func Run(ctx context.Context, cfg RunConfig) error {
	if cfg.ProductVersion == "" {
		cfg.ProductVersion = buildinfo.Version
	}
	if cfg.BuildID == "" {
		cfg.BuildID = buildinfo.ResolvedBuildID()
	}
	if cfg.ProjectFormatVersion == 0 {
		cfg.ProjectFormatVersion = store.ProjectFormatVersion
	}
	if cfg.CacheSchemaVersion == 0 {
		cfg.CacheSchemaVersion = 1
	}
	if cfg.DraftSchemaVersion == 0 {
		cfg.DraftSchemaVersion = draftstore.SchemaVersion
	}
	if cfg.DraftPath == "" {
		cfg.DraftPath = filepath.Join(filepath.Dir(filepath.Dir(cfg.CachePath)), "data", "drafts.db")
	}
	cache, err := localcache.Open(cfg.CachePath, cfg.ServicePublicKey)
	if err != nil {
		return err
	}
	defer cache.Close()
	var client authorityClient
	var personalEngine *personalauthority.Engine
	if cfg.RuntimeMode == "personal" {
		signer, signerErr := controlplane.NewSigner(cfg.ServicePrivateKey)
		if signerErr != nil {
			return signerErr
		}
		personalEngine, err = personalauthority.Open(cfg.PersonalDatabase, signer)
		if err != nil {
			return err
		}
		defer personalEngine.Close()
		client = personalEngine
	} else {
		remoteClient, remoteErr := remote.New(cfg.AuthorityURL, controlplane.DefaultRequestTimeout)
		if remoteErr != nil {
			return remoteErr
		}
		client = remoteClient
	}
	instance, err := New(cache, client)
	if err != nil {
		return err
	}
	instance.SetPersonalMode(cfg.RuntimeMode == "personal")
	instance.SetIdentity(cfg.RuntimeMode, cfg.ProjectID)
	instance.SetCompatibility(cfg.ProductVersion, cfg.BuildID, cfg.ProjectFormatVersion, cfg.CacheSchemaVersion, cfg.DraftSchemaVersion)
	drafts, err := draftstore.Open(cfg.DraftPath)
	if err != nil {
		return err
	}
	defer drafts.Close()
	instance.SetDraftStore(drafts)
	shutdownRequested := make(chan struct{})
	var shutdownOnce sync.Once
	instance.SetShutdown(func() {
		shutdownOnce.Do(func() { close(shutdownRequested) })
	})
	configs, err := LoadConnectorConfigs(cfg.ConnectorConfigPath)
	if err != nil {
		return err
	}
	dispatcher, err := NewDispatcher(configs, instance.submitConnectorCommand)
	if err != nil {
		return err
	}
	instance.SetDispatcher(dispatcher)
	listener, err := ListenLocal(cfg.Endpoint)
	if err != nil {
		return err
	}
	defer listener.Close()
	server := &http.Server{
		Handler: instance.Handler(), ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second,
	}
	failures := make(chan error, 1)
	go func() { failures <- server.Serve(listener) }()
	select {
	case err = <-failures:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), daemonShutdownTimeout)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	case <-shutdownRequested:
		shutdownCtx, cancel := context.WithTimeout(context.Background(), daemonShutdownTimeout)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	}
}
