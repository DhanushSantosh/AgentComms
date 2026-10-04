package opencodeclient

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// PreparationMessage identifies only the tool-disabled marker created by the
// worker. The worker must durably retain its minted ID before sending it so a
// lost response never requires searching or deleting conversation messages.
type PreparationMessage struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionID"`
	Agent     string `json:"agent"`
	Role      string `json:"role"`
}

// NewPreparationID mints a provider-compatible, unguessable message identity.
// Callers persist it with exact project/runtime/session ownership before use.
func NewPreparationID() (string, error) {
	var entropy [24]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return "", errors.New("cannot mint native preparation identity")
	}
	return "msg_" + hex.EncodeToString(entropy[:]), nil
}

// CreatePreparation establishes the native deny baseline without a model turn.
// It is not a policy grant: callers must install and verify restrictive rules,
// then successfully remove this exact marker before prompting.
func (c *Client) CreatePreparation(ctx context.Context, sessionID, markerID string) (PreparationMessage, error) {
	if sessionID == "" || !validPreparationID(markerID) {
		return PreparationMessage{}, errors.New("owned native preparation identity is required")
	}
	var response struct {
		Info  PreparationMessage `json:"info"`
		Parts []Part             `json:"parts"`
	}
	body := map[string]any{
		"messageID": markerID,
		"noReply":   true,
		"tools":     map[string]bool{"*": false},
		"parts":     []TextPart{},
	}
	if err := c.do(ctx, http.MethodPost, "/session/"+url.PathEscape(sessionID)+"/message", body, &response); err != nil {
		return PreparationMessage{}, errors.New("native preparation request failed; retained marker requires recovery")
	}
	if response.Info.ID != markerID || response.Info.SessionID != sessionID || response.Info.Agent == "" || response.Info.Role != "user" || response.Parts == nil || len(response.Parts) != 0 {
		return PreparationMessage{}, errors.New("native preparation response identity or content mismatch")
	}
	return response.Info, nil
}

// RemovePreparation deletes only the exact retained owned marker. A successful
// HTTP response without the provider's affirmative result is not cleanup proof.
func (c *Client) RemovePreparation(ctx context.Context, sessionID, markerID string) error {
	if sessionID == "" || !validPreparationID(markerID) {
		return errors.New("owned native preparation identity is required")
	}
	var removed bool
	if err := c.do(ctx, http.MethodDelete, "/session/"+url.PathEscape(sessionID)+"/message/"+url.PathEscape(markerID), nil, &removed); err != nil {
		return errors.New("native preparation cleanup failed")
	}
	if !removed {
		return errors.New("native preparation cleanup was not confirmed")
	}
	return nil
}

// PreparationExists inspects only an exact retained marker. A 404 is the sole
// absent result; transport errors and foreign/nonempty messages fail closed.
func (c *Client) PreparationExists(ctx context.Context, sessionID, markerID string) (bool, error) {
	if sessionID == "" || !validPreparationID(markerID) {
		return false, errors.New("owned native preparation identity is required")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/session/"+url.PathEscape(sessionID)+"/message/"+url.PathEscape(markerID), nil)
	if err != nil {
		return false, errors.New("native preparation inspection failed")
	}
	c.setDirectoryHeader(request)
	response, err := c.http.Do(request)
	if err != nil {
		return false, errors.New("native preparation inspection failed")
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if response.StatusCode != http.StatusOK {
		return false, errors.New("native preparation inspection failed")
	}
	var marker struct {
		Info  PreparationMessage `json:"info"`
		Parts []Part             `json:"parts"`
	}
	const maxInspectionBytes = 64 * 1024
	body, err := io.ReadAll(io.LimitReader(response.Body, maxInspectionBytes+1))
	if err != nil || len(body) > maxInspectionBytes {
		return false, errors.New("native preparation inspection response invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	if err := decoder.Decode(&marker); err != nil {
		return false, errors.New("native preparation inspection response invalid")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return false, errors.New("native preparation inspection response invalid")
	}
	if marker.Info.ID != markerID || marker.Info.SessionID != sessionID || marker.Info.Role != "user" || marker.Info.Agent == "" || marker.Parts == nil || len(marker.Parts) != 0 {
		return false, errors.New("retained native preparation marker no longer matches its owned identity/content")
	}
	return true, nil
}

func validPreparationID(id string) bool {
	if !strings.HasPrefix(id, "msg_") || len(id) != 52 {
		return false
	}
	_, err := hex.DecodeString(id[4:])
	return err == nil
}
