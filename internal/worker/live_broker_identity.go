package worker

import (
	"errors"

	"github.com/DhanushSantosh/AgentComms/internal/brokeridentity"
)

// Resolve identity from the stored project, never from a relocatable workdir.
func liveBrokerProjectID(config Config) (string, error) {
	if config.Service == nil || config.Service.Store == nil {
		return "", errors.New("live broker requires a configured project service")
	}
	cfg, err := config.Service.Store.ConfigStrict()
	if err != nil {
		return "", err
	}
	if _, err := brokeridentity.RuntimeKey(cfg.ProjectID, config.RuntimeID); err != nil {
		return "", err
	}
	return cfg.ProjectID, nil
}
