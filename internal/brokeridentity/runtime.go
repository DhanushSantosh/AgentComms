// Package brokeridentity owns the non-authoritative transport identity used
// by project-aware clients of the host-shared live brokers.
package brokeridentity

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"
)

var runtimeIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// ValidRuntimeID preserves the brokers' existing logical/transport ID rules.
func ValidRuntimeID(id string) bool { return runtimeIDPattern.MatchString(id) }

// RuntimeKey maps a stored project identity and logical runtime ID to one
// bounded broker key. It is a namespace, not an authentication credential.
func RuntimeKey(projectID, runtimeID string) (string, error) {
	if strings.TrimSpace(projectID) == "" {
		return "", errors.New("live broker project ID is required")
	}
	if !ValidRuntimeID(runtimeID) {
		return "", errors.New("invalid runtime ID")
	}
	raw := []byte("agent-comms/live-runtime/v1\x00")
	raw = binary.BigEndian.AppendUint64(raw, uint64(len(projectID)))
	raw = append(raw, projectID...)
	raw = binary.BigEndian.AppendUint64(raw, uint64(len(runtimeID)))
	raw = append(raw, runtimeID...)
	digest := sha256.Sum256(raw)
	return "p-" + hex.EncodeToString(digest[:]), nil
}
