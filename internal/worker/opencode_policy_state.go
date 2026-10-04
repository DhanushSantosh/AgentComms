package worker

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/DhanushSantosh/AgentComms/internal/durablefs"
	"github.com/DhanushSantosh/AgentComms/internal/identity"
	"github.com/DhanushSantosh/AgentComms/internal/opencodeclient"
	"github.com/DhanushSantosh/AgentComms/internal/sessioncache"
)

// Local routing/recovery metadata, never signed authority state. Original
// restrictions are immutable; a changed provider policy requires reconciliation
// rather than guessing which rules were human or managed.
type openCodePolicyRecord struct {
	Version       int                             `json:"version"`
	RuntimeID     string                          `json:"runtime_id"`
	WorkDir       string                          `json:"work_dir"`
	SessionID     string                          `json:"session_id"`
	Original      []opencodeclient.PermissionRule `json:"original"`
	Managed       []opencodeclient.PermissionRule `json:"managed"`
	PendingMarker string                          `json:"pending_marker,omitempty"`
	PendingRules  []opencodeclient.PermissionRule `json:"pending_rules,omitempty"`
}

func openCodePolicyPath(workDir, runtimeID string) (string, error) {
	return sessioncache.Path(workDir, "opencode-live-policy-"+openCodeIdentityHash(runtimeID))
}

func openCodeIdentityHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func readOpenCodeRecord(path string, out any) (bool, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 1024*1024+1))
	if err != nil {
		return false, err
	}
	if len(data) > 1024*1024 {
		return false, errors.New("OpenCode ownership record exceeds its bound")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return false, errors.New("OpenCode ownership record is invalid")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return false, errors.New("OpenCode ownership record contains trailing data")
	}
	return true, nil
}

func writeOpenCodeRecord(path string, record any) (result error) {
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	if len(data) > 1024*1024 {
		return errors.New("OpenCode ownership record exceeds its bound")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".opencode-policy-*")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer func() {
		if err := os.Remove(temporary); err != nil && !errors.Is(err, os.ErrNotExist) {
			result = errors.Join(result, err)
		}
	}()
	_, writeErr := file.Write(data)
	syncErr := file.Sync()
	closeErr := file.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		return err
	}
	return durablefs.SyncDirectory(filepath.Dir(path))
}

func (a *openCodeLiveAdapter) initialize(config Config) error {
	root, err := filepath.EvalSymlinks(config.WorkDir)
	if err != nil {
		return fmt.Errorf("resolve OpenCode project: %w", err)
	}
	if config.RuntimeID == "" {
		return errors.New("OpenCode runtime identity is required")
	}
	if a.runtimeLock != nil {
		if root != a.workDir || config.RuntimeID != a.runtimeID {
			return errors.New("OpenCode worker ownership cannot change")
		}
		return nil
	}
	path, err := openCodePolicyPath(root, config.RuntimeID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	lock, err := lockOpenCodeFile(path + ".lock")
	if err != nil {
		return err
	}
	a.runtimeLock = lock
	a.workDir, a.runtimeID, a.recordPath = root, config.RuntimeID, path
	return nil
}

func (a *openCodeLiveAdapter) claimSession(sessionID string) error {
	if a.sessionLock != nil {
		if sessionID != a.sessionID {
			return errors.New("OpenCode worker cannot switch an owned session")
		}
		return nil
	}
	configDir, err := identity.ConfigDir()
	if err != nil {
		return err
	}
	path := filepath.Join(configDir, "sessions", "opencode-live-owned-"+openCodeIdentityHash(sessionID)+".json")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	lock, err := lockOpenCodeFile(path + ".lock")
	if err != nil {
		return err
	}
	accepted := false
	defer func() {
		if !accepted {
			_ = unlockOpenCodeFile(lock)
		}
	}()
	var owner struct {
		RuntimeID string `json:"runtime_id"`
		WorkDir   string `json:"work_dir"`
		SessionID string `json:"session_id"`
	}
	found, err := readOpenCodeRecord(path, &owner)
	if err != nil {
		return err
	}
	if found && (owner.RuntimeID != a.runtimeID || owner.WorkDir != a.workDir || owner.SessionID != sessionID) {
		return errors.New("native OpenCode session belongs to a different managed runtime/project")
	}
	if !found {
		owner.RuntimeID, owner.WorkDir, owner.SessionID = a.runtimeID, a.workDir, sessionID
		if err := writeOpenCodeRecord(path, owner); err != nil {
			return err
		}
	}
	a.sessionLock, a.sessionID = lock, sessionID
	accepted = true
	return nil
}

func (a *openCodeLiveAdapter) sessionRecord(ctx context.Context, client *opencodeclient.Client, config Config) (openCodePolicyRecord, opencodeclient.Session, error) {
	var record openCodePolicyRecord
	found, err := readOpenCodeRecord(a.recordPath, &record)
	if err != nil {
		return record, opencodeclient.Session{}, err
	}
	if found {
		if record.Version != 1 || record.RuntimeID != a.runtimeID || record.WorkDir != a.workDir || record.SessionID == "" || (config.SessionID != "" && config.SessionID != record.SessionID) {
			return record, opencodeclient.Session{}, errors.New("OpenCode policy record ownership/version mismatch")
		}
		if _, err := opencodeclient.RestrictivePermissions(nil, record.Original); err != nil {
			return record, opencodeclient.Session{}, err
		}
	} else {
		record = openCodePolicyRecord{Version: 1, RuntimeID: a.runtimeID, WorkDir: a.workDir, SessionID: config.SessionID}
		if record.SessionID == "" {
			// Read only the existing runtime's legacy routing record. Corruption is
			// not an invitation to silently create a new conversation.
			if filepath.Base(config.RuntimeID) != config.RuntimeID {
				return record, opencodeclient.Session{}, errors.New("invalid legacy OpenCode runtime identity")
			}
			var legacy struct {
				SessionID string `json:"session_id"`
			}
			legacyFound, err := readOpenCodeRecord(openCodeLiveSessionPath(config.WorkDir, config.RuntimeID), &legacy)
			if err != nil {
				return record, opencodeclient.Session{}, err
			}
			if legacyFound && legacy.SessionID == "" {
				return record, opencodeclient.Session{}, errors.New("legacy OpenCode session record lacks identity")
			}
			record.SessionID = legacy.SessionID
		}
	}
	var session opencodeclient.Session
	if record.SessionID == "" {
		session, err = client.CreateSession(ctx, a.workDir)
	} else {
		if err = a.claimSession(record.SessionID); err != nil {
			return record, session, err
		}
		session, err = client.GetSession(ctx, record.SessionID)
	}
	if err != nil {
		return record, session, errors.New("native OpenCode session lookup/create failed; no fresh-session fallback")
	}
	if session.ID == "" || session.Directory != a.workDir || (record.SessionID != "" && session.ID != record.SessionID) {
		return record, session, errors.New("native OpenCode session identity/project mismatch")
	}
	if err = a.claimSession(session.ID); err != nil {
		return record, session, err
	}
	if !found {
		record.SessionID = session.ID
		record.Original = append([]opencodeclient.PermissionRule(nil), session.Permission...)
		if _, err := opencodeclient.RestrictivePermissions(nil, record.Original); err != nil {
			return record, session, err
		}
		if err = writeOpenCodeRecord(a.recordPath, record); err != nil {
			return record, session, err
		}
	}
	return record, session, nil
}

func sameOpenCodeRules(left, right []opencodeclient.PermissionRule) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func (a *openCodeLiveAdapter) prepareSession(ctx context.Context, client *opencodeclient.Client, config Config) (string, string, error) {
	record, session, err := a.sessionRecord(ctx, client, config)
	if err != nil {
		return "", "", err
	}
	// OpenCode performs revert.cleanup before honoring noReply; sending even
	// an empty preparation message would delete an undoable history tail.
	if len(session.Revert) != 0 && !bytes.Equal(bytes.TrimSpace(session.Revert), []byte("null")) {
		return "", "", errors.New("native OpenCode session has pending undo/revert state; restore or resolve it in the native client before retrying; history left untouched")
	}
	baseline := []opencodeclient.PermissionRule{{Permission: "*", Pattern: "*", Action: "deny"}}
	if record.PendingMarker != "" {
		known := sameOpenCodeRules(session.Permission, record.Original) || sameOpenCodeRules(session.Permission, record.Managed) || sameOpenCodeRules(session.Permission, baseline) || (record.PendingRules != nil && sameOpenCodeRules(session.Permission, record.PendingRules))
		if !known {
			return "", "", errors.New("interrupted OpenCode preparation has unknown native rules; reconcile without discarding native restrictions")
		}
		exists, err := client.PreparationExists(ctx, session.ID, record.PendingMarker)
		if err != nil {
			return "", "", err
		}
		if exists {
			if err := client.RemovePreparation(ctx, session.ID, record.PendingMarker); err != nil {
				return "", "", err
			}
		}
		record.PendingMarker, record.PendingRules = "", nil
		record.Managed = append([]opencodeclient.PermissionRule(nil), session.Permission...)
		if err := writeOpenCodeRecord(a.recordPath, record); err != nil {
			return "", "", err
		}
	}
	expected := record.Managed
	if expected == nil {
		expected = record.Original
	}
	if !sameOpenCodeRules(session.Permission, expected) {
		return "", "", errors.New("native OpenCode restrictions changed outside this worker; reconcile the owned session before retrying")
	}
	markerID, err := opencodeclient.NewPreparationID()
	if err != nil {
		return "", "", err
	}
	exists, err := client.PreparationExists(ctx, session.ID, markerID)
	if err != nil {
		return "", "", err
	}
	if exists {
		return "", "", errors.New("new native preparation identity already exists; existing message left untouched")
	}
	record.PendingMarker = markerID
	if err := writeOpenCodeRecord(a.recordPath, record); err != nil {
		return "", "", err
	}
	marker, err := client.CreatePreparation(ctx, session.ID, markerID)
	if err != nil {
		return "", "", err
	}
	agent, err := client.ResolveAgent(ctx, marker.Agent)
	if err != nil {
		return "", "", err
	}
	rules, err := opencodeclient.RestrictivePermissions(agent.Permission, record.Original)
	if err != nil {
		return "", "", err
	}
	record.PendingRules = append(append([]opencodeclient.PermissionRule(nil), baseline...), rules...)
	if err := writeOpenCodeRecord(a.recordPath, record); err != nil {
		return "", "", err
	}
	patched, err := client.AppendSessionPermissions(ctx, session.ID, rules)
	if err != nil || patched.ID != session.ID || patched.Directory != a.workDir {
		return "", "", errors.New("native OpenCode policy installation failed or returned foreign identity")
	}
	verified, err := client.GetSession(ctx, session.ID)
	if err != nil || verified.ID != session.ID || verified.Directory != a.workDir || !sameOpenCodeRules(verified.Permission, record.PendingRules) {
		return "", "", errors.New("native OpenCode policy readback disagrees; prompt refused")
	}
	if err := client.RemovePreparation(ctx, session.ID, marker.ID); err != nil {
		return "", "", err
	}
	exists, err = client.PreparationExists(ctx, session.ID, marker.ID)
	if err != nil {
		return "", "", err
	}
	if exists {
		return "", "", errors.New("native preparation marker remains after cleanup; prompt refused")
	}
	record.Managed = record.PendingRules
	record.PendingMarker, record.PendingRules = "", nil
	if err := writeOpenCodeRecord(a.recordPath, record); err != nil {
		return "", "", err
	}
	return session.ID, agent.Name, nil
}
