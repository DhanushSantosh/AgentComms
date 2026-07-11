package model

import "time"

const SchemaVersion = "1.0.0"

type Event struct {
	SchemaVersion string         `json:"schema_version"`
	ID            string         `json:"id"`
	Sequence      uint64         `json:"sequence"`
	Time          time.Time      `json:"time"`
	Actor         string         `json:"actor"`
	Type          string         `json:"type"`
	EntityID      string         `json:"entity_id,omitempty"`
	Data          map[string]any `json:"data,omitempty"`
	PreviousHash  string         `json:"previous_hash,omitempty"`
	Hash          string         `json:"hash"`
	Signature     string         `json:"signature"`
}

type Agent struct {
	ID, Status           string
	Capabilities, Scopes []string
}
type Task struct {
	ID, Title, Status, Owner, Repository, Branch string
	Resources                                    []string
	LeaseUntil, StaleUntil                       time.Time
	HandoffTo                                    string
}
type Message struct {
	ID, Kind, From        string
	To                    []string
	Subject, Body, Status string
}
type Approval struct {
	ID, Kind, Status, Requester string
	Affected                    []string
}
type State struct {
	Agents    map[string]Agent          `json:"agents"`
	Tasks     map[string]Task           `json:"tasks"`
	Messages  map[string]Message        `json:"messages"`
	Approvals map[string]Approval       `json:"approvals"`
	Decisions map[string]map[string]any `json:"decisions"`
	Sessions  map[string]map[string]any `json:"sessions"`
}
