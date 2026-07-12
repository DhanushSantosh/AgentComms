package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"time"
)

type BusyError struct {
	HolderPID   int           `json:"holder_pid"`
	HolderActor string        `json:"holder_actor,omitempty"`
	Since       time.Time     `json:"since,omitempty"`
	Timeout     time.Duration `json:"timeout"`
}

func (e *BusyError) Error() string {
	return fmt.Sprintf("runtime busy after %s (pid %d, actor %s)", e.Timeout, e.HolderPID, e.HolderActor)
}

type lockMeta struct {
	PID   int       `json:"pid"`
	Actor string    `json:"actor"`
	Since time.Time `json:"since"`
}

func (s *Store) acquire(actor string) (func(), error) {
	deadline := time.Now().Add(s.LockTimeout)
	dir := filepath.Join(s.runtime(), "tmp", "transaction.lock")
	m := lockMeta{PID: os.Getpid(), Actor: actor, Since: time.Now().UTC()}
	for {
		e := os.Mkdir(dir, 0700)
		if e == nil {
			b, _ := json.Marshal(m)
			if e = os.WriteFile(filepath.Join(dir, "holder.json"), b, 0600); e != nil {
				_ = os.RemoveAll(dir)
				return nil, e
			}
			return func() { _ = os.RemoveAll(dir) }, nil
		}
		if !errors.Is(e, os.ErrExist) {
			return nil, e
		}
		if s.clearDeadLock(dir) {
			continue
		}
		if time.Now().After(deadline) {
			h := readLock(dir)
			return nil, &BusyError{HolderPID: h.PID, HolderActor: h.Actor, Since: h.Since, Timeout: s.LockTimeout}
		}
		time.Sleep(time.Duration(40+rand.IntN(80)) * time.Millisecond)
	}
}
func readLock(dir string) lockMeta {
	b, _ := os.ReadFile(filepath.Join(dir, "holder.json"))
	var m lockMeta
	_ = json.Unmarshal(b, &m)
	return m
}
func (s *Store) clearDeadLock(dir string) bool {
	m := readLock(dir)
	if m.PID <= 0 || time.Since(m.Since) < time.Minute {
		return false
	}
	if processAlive(m.PID) {
		return false
	}
	return os.RemoveAll(dir) == nil
}
