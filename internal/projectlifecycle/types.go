package projectlifecycle

import "time"

// This file is the pure types layer of internal/projectlifecycle: the
// declarations callers need to NAME lifecycle state, with no dependency on
// SQLite, the filesystem, or a running daemon. It carries no build
// constraint, so it is available on every platform including js/wasm --
// where the SQLite-backed inspection machinery in lifecycle.go cannot exist
// (modernc.org/libc has no js port, and a browser sandbox has no on-disk
// project databases to inspect in the first place).

const (
	PersonalAuthoritySchemaVersion = 1
	ProjectionCacheSchemaVersion   = 3
	DraftStoreSchemaVersion        = 1
)

type ErrorCode string

const (
	CodeUpgradeRequired    ErrorCode = "UPGRADE_REQUIRED"
	CodeProjectTooNew      ErrorCode = "PROJECT_TOO_NEW"
	CodeUpgradeUnsupported ErrorCode = "UPGRADE_UNSUPPORTED"
	CodeUpgradeFailed      ErrorCode = "UPGRADE_FAILED"
	CodeConflict           ErrorCode = "CONFLICT"
)

type Error struct {
	Code    ErrorCode
	Message string
}

func (e *Error) Error() string { return e.Message }

type Action struct {
	Component string `json:"component"`
	From      string `json:"from"`
	To        string `json:"to"`
	Operation string `json:"operation"`
	Automatic bool   `json:"automatic"`
}

type Plan struct {
	ProjectRoot          string   `json:"project_root"`
	ProjectID            string   `json:"project_id"`
	CurrentBuildID       string   `json:"current_build_id,omitempty"`
	TargetBuildID        string   `json:"target_build_id"`
	Actions              []Action `json:"actions"`
	RequiresConfirmation bool     `json:"requires_confirmation"`
	Interrupted          bool     `json:"interrupted"`
}

type Result struct {
	Plan             Plan   `json:"plan"`
	Changed          bool   `json:"changed"`
	Resumed          bool   `json:"resumed"`
	BackupPath       string `json:"backup_path,omitempty"`
	DaemonStopped    bool   `json:"daemon_stopped"`
	CacheInvalidated bool   `json:"cache_invalidated"`
	Verified         bool   `json:"verified"`
}

type Options struct {
	Root       string
	Version    string
	BuildID    string
	Apply      bool
	Approved   bool
	Timeout    time.Duration
	StopDaemon bool
}
