package daemon

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/controlplane"
	"github.com/DhanushSantosh/AgentComms/internal/localcache"
	"github.com/DhanushSantosh/AgentComms/internal/personalauthority"
	"github.com/DhanushSantosh/AgentComms/internal/remote"
)

const daemonShutdownTimeout = 10 * time.Second

type RunConfig struct {
	AuthorityURL        string
	ServicePublicKey    string
	CachePath           string
	Endpoint            string
	ConnectorConfigPath string
	RuntimeMode         string
	PersonalDatabase    string
	ServicePrivateKey   string
}

func Run(ctx context.Context, cfg RunConfig) error {
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
	}
}
