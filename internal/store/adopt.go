package store

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/identity"
	"github.com/DhanushSantosh/AgentComms/internal/model"
)

const (
	CutoverPrepared       = "PREPARED"
	CutoverAckPending     = "AGENT_ACK_PENDING"
	CutoverReady          = "READY"
	CutoverActivated      = "ACTIVATED"
	legacyCutoverRelPath  = "migrations/legacy-agents/cutover.json"
	legacyManifestRelPath = "migrations/legacy-agents/manifest.json"
	legacyIndexRelPath    = "migrations/legacy-agents/index.jsonl"
)

type LegacyManifest struct {
	SchemaVersion string    `json:"schema_version"`
	SourcePath    string    `json:"source_path"`
	ArchivePath   string    `json:"archive_path"`
	SHA256        string    `json:"sha256"`
	Size          int64     `json:"size"`
	PreservedAt   time.Time `json:"preserved_at"`
	IndexPath     string    `json:"index_path"`
	IndexTruth    bool      `json:"index_is_current_truth"`
}

type Cutover struct {
	SchemaVersion     string            `json:"schema_version"`
	State             string            `json:"state"`
	LegacySHA256      string            `json:"legacy_sha256"`
	ManifestPath      string            `json:"manifest_path"`
	ManifestSHA256    string            `json:"manifest_sha256"`
	Bootstrap         string            `json:"bootstrap"`
	RequiredAcks      []string          `json:"required_acknowledgements"`
	Acknowledged      map[string]string `json:"acknowledged_at"`
	SeedingVerifiedBy string            `json:"seeding_verified_by,omitempty"`
	SeedingVerifiedAt *time.Time        `json:"seeding_verified_at,omitempty"`
	PreparedAt        time.Time         `json:"prepared_at"`
	ActivatedAt       *time.Time        `json:"activated_at,omitempty"`
}

type AdoptionPreview struct {
	Cutover  Cutover        `json:"cutover"`
	Manifest LegacyManifest `json:"manifest"`
}

type LegacySearchResult struct {
	Source       string          `json:"source"`
	CurrentTruth bool            `json:"current_truth"`
	Record       json.RawMessage `json:"record"`
}

func (s *Store) SearchLegacy(query string) ([]LegacySearchResult, error) {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return nil, errors.New("legacy search query is required")
	}
	out := []LegacySearchResult{}
	indexPath := filepath.Join(s.runtime(), filepath.FromSlash(legacyIndexRelPath))
	if f, e := os.Open(indexPath); e == nil {
		scan := bufio.NewScanner(f)
		scan.Buffer(make([]byte, 64*1024), 2*1024*1024)
		for scan.Scan() {
			line := append([]byte(nil), scan.Bytes()...)
			if strings.Contains(strings.ToLower(string(line)), query) {
				out = append(out, LegacySearchResult{Source: legacyIndexRelPath, CurrentTruth: false, Record: line})
			}
		}
		_ = f.Close()
		if e = scan.Err(); e != nil {
			return nil, e
		}
	}
	files, _ := filepath.Glob(filepath.Join(s.runtime(), "legacy", "v1", "events", "*.json"))
	sort.Strings(files)
	for _, path := range files {
		b, e := os.ReadFile(path)
		if e != nil {
			return nil, e
		}
		if strings.Contains(strings.ToLower(string(b)), query) {
			rel, _ := filepath.Rel(s.runtime(), path)
			out = append(out, LegacySearchResult{Source: filepath.ToSlash(rel), CurrentTruth: false, Record: b})
		}
	}
	return out, nil
}

func (s *Store) PrepareLegacyAdoption(owner string) (AdoptionPreview, error) {
	if strings.TrimSpace(owner) == "" {
		return AdoptionPreview{}, errors.New("owner identity mapping is required")
	}
	if _, e := os.Stat(filepath.Join(s.Root, ".git")); e != nil {
		return AdoptionPreview{}, errors.New("target must be a Git repository")
	}
	legacyPath := filepath.Join(s.Root, ".agents")
	legacy, e := os.ReadFile(legacyPath)
	if e != nil {
		return AdoptionPreview{}, fmt.Errorf("read legacy .agents: %w", e)
	}
	if bytes.Equal(legacy, ManagedBootstrap()) {
		return AdoptionPreview{}, errors.New(".agents is already the managed bootstrap")
	}
	if _, e = os.Stat(s.runtime()); os.IsNotExist(e) {
		if e = s.initAdoptionRuntime(owner); e != nil {
			return AdoptionPreview{}, e
		}
	} else if e != nil {
		return AdoptionPreview{}, e
	} else {
		cfg, x := s.Config()
		if x != nil {
			return AdoptionPreview{}, x
		}
		if cfg.SchemaVersion == "1.0.0" {
			if x = s.migrateV1(owner, true); x != nil {
				return AdoptionPreview{}, x
			}
		} else if cfg.SchemaVersion != model.SchemaVersion {
			return AdoptionPreview{}, fmt.Errorf("unsupported runtime schema %q", cfg.SchemaVersion)
		} else {
			if x = s.Verify(); x != nil {
				return AdoptionPreview{}, fmt.Errorf("existing event chain integrity check failed; repair before adoption: %w", x)
			}
			if _, x = identity.ResolveCredential(s.Credentials, cfg.ProjectID, owner); x != nil {
				return AdoptionPreview{}, fmt.Errorf("owner credential %q not found in existing runtime; register the owner first or use --owner that matches an existing credential", owner)
			}
		}
	}
	release, e := s.acquire(owner)
	if e != nil {
		return AdoptionPreview{}, e
	}
	defer release()
	if e = s.markAdoptionRequired(); e != nil {
		return AdoptionPreview{}, e
	}
	if existing, x := s.Cutover(); x == nil {
		manifest, mx := s.LegacyManifest()
		return AdoptionPreview{Cutover: existing, Manifest: manifest}, mx
	}

	h := sha256.Sum256(legacy)
	hash := hex.EncodeToString(h[:])
	archiveRel := filepath.ToSlash(filepath.Join("legacy", "agents", hash, ".agents"))
	archivePath := filepath.Join(s.runtime(), filepath.FromSlash(archiveRel))
	if e = os.MkdirAll(filepath.Dir(archivePath), 0700); e != nil {
		return AdoptionPreview{}, e
	}
	if e = writeFileSync(archivePath, legacy, 0600); e != nil && !os.IsExist(e) {
		return AdoptionPreview{}, fmt.Errorf("preserve legacy .agents: %w", e)
	}
	check, e := os.ReadFile(archivePath)
	if e != nil || !bytes.Equal(check, legacy) {
		return AdoptionPreview{}, errors.New("legacy .agents preservation verification failed")
	}
	manifest := LegacyManifest{SchemaVersion: "legacy-agents-manifest/v1", SourcePath: ".agents", ArchivePath: archiveRel, SHA256: hash, Size: int64(len(legacy)), PreservedAt: s.Now(), IndexPath: legacyIndexRelPath, IndexTruth: false}
	if e = os.MkdirAll(filepath.Join(s.runtime(), "migrations", "legacy-agents"), 0700); e != nil {
		return AdoptionPreview{}, e
	}
	if e = writeJSON(filepath.Join(s.runtime(), filepath.FromSlash(legacyManifestRelPath)), manifest, 0600); e != nil {
		return AdoptionPreview{}, e
	}
	manifestBytes, e := os.ReadFile(filepath.Join(s.runtime(), filepath.FromSlash(legacyManifestRelPath)))
	if e != nil {
		return AdoptionPreview{}, e
	}
	manifestHash := sha256.Sum256(manifestBytes)
	if e = writeLegacyIndex(filepath.Join(s.runtime(), filepath.FromSlash(legacyIndexRelPath)), legacy); e != nil {
		return AdoptionPreview{}, e
	}
	cutover := Cutover{SchemaVersion: "legacy-cutover/v1", State: CutoverPrepared, LegacySHA256: hash, ManifestPath: legacyManifestRelPath, ManifestSHA256: hex.EncodeToString(manifestHash[:]), Bootstrap: string(ManagedBootstrap()), Acknowledged: map[string]string{}, PreparedAt: s.Now()}
	if e = writeJSON(filepath.Join(s.runtime(), filepath.FromSlash(legacyCutoverRelPath)), cutover, 0600); e != nil {
		return AdoptionPreview{}, e
	}
	if e = s.git("add", "legacy", "migrations", "AGENT_INSTRUCTIONS.md"); e != nil {
		return AdoptionPreview{}, e
	}
	if e = s.git("commit", "--no-gpg-sign", "-m", "Prepare legacy .agents adoption"); e != nil {
		return AdoptionPreview{}, e
	}
	return AdoptionPreview{Cutover: cutover, Manifest: manifest}, nil
}

func (s *Store) initAdoptionRuntime(owner string) error {
	stage := filepath.Join(s.Root, fmt.Sprintf(".%s.adopt-%d", strings.TrimPrefix(Runtime, "."), s.Now().UnixNano()))
	staged := *s
	staged.runtimePath = stage
	projectID, _, e := staged.initializeRuntime(owner)
	if e != nil {
		_ = os.RemoveAll(stage)
		return e
	}
	if e = os.Rename(stage, s.runtime()); e != nil {
		_ = os.RemoveAll(stage)
		_ = s.Credentials.Delete(projectID, owner)
		return e
	}
	if e = s.saveProfile(projectID, owner); e != nil {
		_ = os.RemoveAll(s.runtime())
		_ = s.Credentials.Delete(projectID, owner)
		return e
	}
	return nil
}

func (s *Store) markAdoptionRequired() error {
	cfg, e := s.Config()
	if e != nil {
		return e
	}
	if cfg.AdoptionRequired {
		return nil
	}
	cfg.AdoptionRequired = true
	if e = writeJSON(filepath.Join(s.runtime(), "config.json"), cfg, 0644); e != nil {
		return e
	}
	if e = s.git("add", "config.json"); e != nil {
		return e
	}
	return s.git("commit", "--no-gpg-sign", "-m", "Require governed legacy adoption")
}

func writeLegacyIndex(path string, legacy []byte) error {
	f, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	w := bufio.NewWriter(f)
	scan := bufio.NewScanner(bytes.NewReader(legacy))
	scan.Buffer(make([]byte, 64*1024), 2*1024*1024)
	line := 0
	for scan.Scan() {
		line++
		text := strings.TrimSpace(scan.Text())
		if text == "" {
			continue
		}
		record := map[string]any{"line": line, "text": text, "confidence": "UNVERIFIED", "current_truth": false}
		b, _ := json.Marshal(record)
		if _, e = w.Write(append(b, '\n')); e != nil {
			break
		}
	}
	if e == nil {
		e = scan.Err()
	}
	if e == nil {
		e = w.Flush()
	}
	if e == nil {
		e = f.Sync()
	}
	if closeErr := f.Close(); e == nil {
		e = closeErr
	}
	return e
}

func (s *Store) Cutover() (Cutover, error) {
	var c Cutover
	b, e := os.ReadFile(filepath.Join(s.runtime(), filepath.FromSlash(legacyCutoverRelPath)))
	if e != nil {
		return c, e
	}
	e = json.Unmarshal(b, &c)
	if c.Acknowledged == nil {
		c.Acknowledged = map[string]string{}
	}
	return c, e
}

func (s *Store) LegacyManifest() (LegacyManifest, error) {
	var m LegacyManifest
	b, e := os.ReadFile(filepath.Join(s.runtime(), filepath.FromSlash(legacyManifestRelPath)))
	if e != nil {
		return m, e
	}
	if c, x := s.Cutover(); x == nil && c.ManifestSHA256 != "" {
		h := sha256.Sum256(b)
		if hex.EncodeToString(h[:]) != c.ManifestSHA256 {
			return m, errors.New("legacy manifest hash mismatch")
		}
	}
	e = json.Unmarshal(b, &m)
	return m, e
}

func (s *Store) SetCutoverAcknowledgements(required []string) (Cutover, error) {
	release, e := s.acquire("migration")
	if e != nil {
		return Cutover{}, e
	}
	defer release()
	c, e := s.Cutover()
	if e != nil {
		return c, e
	}
	if c.State != CutoverPrepared && c.State != CutoverAckPending {
		return c, fmt.Errorf("cutover must be PREPARED, got %s", c.State)
	}
	if c.SeedingVerifiedAt == nil {
		return c, errors.New("explicit legacy seeding verification is required before acknowledgements")
	}
	uniq := map[string]bool{}
	for _, id := range required {
		id = strings.TrimSpace(id)
		if id != "" {
			uniq[id] = true
		}
	}
	c.RequiredAcks = c.RequiredAcks[:0]
	for id := range uniq {
		c.RequiredAcks = append(c.RequiredAcks, id)
	}
	sort.Strings(c.RequiredAcks)
	c.State = CutoverAckPending
	if len(c.RequiredAcks) == 0 {
		c.State = CutoverReady
	}
	return c, s.commitCutover(c, "Set legacy cutover acknowledgements")
}

func (s *Store) ConfirmLegacySeeding(actor string) (Cutover, error) {
	release, e := s.acquire(actor)
	if e != nil {
		return Cutover{}, e
	}
	defer release()
	c, e := s.Cutover()
	if e != nil {
		return c, e
	}
	if c.State != CutoverPrepared {
		return c, fmt.Errorf("seeding can only be confirmed in PREPARED, got %s", c.State)
	}
	if strings.TrimSpace(actor) == "" {
		return c, errors.New("verifying actor is required")
	}
	now := s.Now()
	c.SeedingVerifiedBy, c.SeedingVerifiedAt = actor, &now
	return c, s.commitCutover(c, "Verify explicit legacy state seeding")
}

func (s *Store) AcknowledgeCutover(actor string) (Cutover, error) {
	release, e := s.acquire(actor)
	if e != nil {
		return Cutover{}, e
	}
	defer release()
	c, e := s.Cutover()
	if e != nil {
		return c, e
	}
	if c.State != CutoverAckPending {
		return c, fmt.Errorf("cutover is not awaiting acknowledgements: %s", c.State)
	}
	required := false
	for _, id := range c.RequiredAcks {
		if id == actor {
			required = true
		}
	}
	if !required {
		return c, fmt.Errorf("%s is not a required acknowledging agent", actor)
	}
	c.Acknowledged[actor] = s.Now().Format(time.RFC3339Nano)
	ready := true
	for _, id := range c.RequiredAcks {
		if c.Acknowledged[id] == "" {
			ready = false
		}
	}
	if ready {
		c.State = CutoverReady
	}
	return c, s.commitCutover(c, "Acknowledge legacy cutover "+actor)
}

func (s *Store) ActivateCutover() (Cutover, error) {
	release, e := s.acquire("migration")
	if e != nil {
		return Cutover{}, e
	}
	defer release()
	c, e := s.Cutover()
	if e != nil {
		return c, e
	}
	if c.State != CutoverReady {
		return c, fmt.Errorf("cutover must be READY before activation, got %s", c.State)
	}
	manifest, e := s.LegacyManifest()
	if e != nil {
		return c, e
	}
	if e = s.verifyLegacyArchive(manifest); e != nil {
		return c, e
	}
	tmp := filepath.Join(s.Root, ".agents.agent-comms.activate.tmp")
	if e = writeFileSync(tmp, ManagedBootstrap(), 0644); e != nil {
		return c, e
	}
	if e = os.Rename(tmp, filepath.Join(s.Root, ".agents")); e != nil {
		_ = os.Remove(tmp)
		return c, e
	}
	if initFail("after-cutover-bootstrap-publish") {
		return c, errors.New("injected activation failure after bootstrap publish")
	}
	now := s.Now()
	c.State, c.ActivatedAt = CutoverActivated, &now
	if e = s.commitCutover(c, "Activate legacy .agents cutover"); e != nil {
		// The byte-identical archive is authoritative for rollback if committing
		// activation metadata is interrupted.
		_ = s.restoreLegacy(manifest)
		return c, e
	}
	return c, nil
}

func (s *Store) RollbackCutover() error {
	release, e := s.acquire("migration")
	if e != nil {
		return e
	}
	defer release()
	c, e := s.Cutover()
	if e != nil {
		return e
	}
	if c.State == CutoverActivated {
		return errors.New("rollback is only available before ACTIVATED; use migrate recover for an activated cutover")
	}
	m, e := s.LegacyManifest()
	if e != nil {
		return e
	}
	rootBytes, e := os.ReadFile(filepath.Join(s.Root, ".agents"))
	if e != nil {
		return fmt.Errorf("rollback requires the original root .agents: %w", e)
	}
	h := sha256.Sum256(rootBytes)
	if hex.EncodeToString(h[:]) != m.SHA256 {
		return errors.New("rollback refused because root .agents is not the preserved legacy file; use migrate recover")
	}
	cfg, _ := s.Config()
	if e = os.RemoveAll(s.runtime()); e != nil {
		return e
	}
	_ = s.Credentials.Delete(cfg.ProjectID, cfg.Owner)
	uc, _ := identity.LoadUserConfig()
	name := cfg.ProjectID + ":" + cfg.Owner
	delete(uc.Profiles, name)
	if uc.ActiveProfile == name {
		uc.ActiveProfile = ""
	}
	return identity.SaveUserConfig(uc)
}

func (s *Store) RecoverLegacy() error {
	release, e := s.acquire("migration")
	if e != nil {
		return e
	}
	defer release()
	c, e := s.Cutover()
	if e != nil {
		return e
	}
	if c.State != CutoverActivated && c.State != CutoverReady {
		return errors.New("recovery requires ACTIVATED or an interrupted READY activation")
	}
	m, e := s.LegacyManifest()
	if e != nil {
		return e
	}
	if e = s.restoreLegacy(m); e != nil {
		return e
	}
	c.State, c.ActivatedAt = CutoverPrepared, nil
	return s.commitCutover(c, "Recover byte-identical legacy .agents")
}

func (s *Store) restoreLegacy(m LegacyManifest) error {
	if e := s.verifyLegacyArchive(m); e != nil {
		return e
	}
	b, e := os.ReadFile(filepath.Join(s.runtime(), filepath.FromSlash(m.ArchivePath)))
	if e != nil {
		return e
	}
	tmp := filepath.Join(s.Root, ".agents.agent-comms.recover.tmp")
	if e = writeFileSync(tmp, b, 0644); e != nil {
		return e
	}
	return os.Rename(tmp, filepath.Join(s.Root, ".agents"))
}

func (s *Store) verifyLegacyArchive(m LegacyManifest) error {
	b, e := os.ReadFile(filepath.Join(s.runtime(), filepath.FromSlash(m.ArchivePath)))
	if e != nil {
		return e
	}
	h := sha256.Sum256(b)
	if hex.EncodeToString(h[:]) != m.SHA256 || int64(len(b)) != m.Size {
		return errors.New("legacy .agents archive hash or size mismatch")
	}
	return nil
}

func (s *Store) commitCutover(c Cutover, message string) error {
	if e := writeJSON(filepath.Join(s.runtime(), filepath.FromSlash(legacyCutoverRelPath)), c, 0600); e != nil {
		return e
	}
	if e := s.git("add", filepath.ToSlash(legacyCutoverRelPath)); e != nil {
		return e
	}
	return s.git("commit", "--no-gpg-sign", "-m", message)
}

func (s *Store) CutoverIncomplete() (bool, string) {
	c, e := s.Cutover()
	if os.IsNotExist(e) {
		if cfg, x := s.Config(); x == nil && cfg.AdoptionRequired {
			return true, "PREPARATION_INCOMPLETE"
		}
		if !s.ManagedBootstrapValid() {
			return true, "UNMANAGED_BOOTSTRAP"
		}
		return false, ""
	}
	if e != nil {
		return true, "INVALID"
	}
	return c.State != CutoverActivated, c.State
}

func (s *Store) ManagedBootstrapValid() bool {
	b, e := os.ReadFile(filepath.Join(s.Root, ".agents"))
	return e == nil && bytes.Equal(b, ManagedBootstrap())
}

func (s *Store) InstructionsPresent() bool {
	b, e := os.ReadFile(filepath.Join(s.runtime(), "AGENT_INSTRUCTIONS.md"))
	return e == nil && len(bytes.TrimSpace(b)) > 0
}

type LegacyCandidate struct {
	Kind         string   `json:"kind"`
	SuggestedID  string   `json:"suggested_id"`
	Title        string   `json:"title"`
	Body         string   `json:"body"`
	Tags         []string `json:"tags,omitempty"`
	Source       string   `json:"source"`
	Confidence   string   `json:"confidence"`
	CurrentTruth bool     `json:"current_truth"`
}

type ExtractedContext struct {
	Candidates []LegacyCandidate `json:"candidates"`
	Agents     []string          `json:"unverified_agent_mentions,omitempty"`
	TotalLines int               `json:"total_lines"`
	Durable    bool              `json:"durable"`
}

// ExtractLegacyContext returns review candidates only. It never appends
// events, registers identities, or changes the current projection.
func (s *Store) ExtractLegacyContext() (ExtractedContext, error) {
	var out ExtractedContext
	legacy, e := os.ReadFile(filepath.Join(s.Root, ".agents"))
	if e != nil {
		return out, fmt.Errorf("read .agents: %w", e)
	}
	source := ".agents"
	if bytes.Equal(bytes.TrimSpace(legacy), bytes.TrimSpace(ManagedBootstrap())) {
		if m, mErr := s.LegacyManifest(); mErr == nil && m.ArchivePath != "" {
			if archive, rErr := os.ReadFile(filepath.Join(s.runtime(), filepath.FromSlash(m.ArchivePath))); rErr == nil {
				legacy, source = archive, m.ArchivePath
			}
		}
		if bytes.Equal(bytes.TrimSpace(legacy), bytes.TrimSpace(ManagedBootstrap())) {
			return out, errors.New("legacy .agents content is unavailable: use `migrate adopt` before activation or restore a backup")
		}
	}

	type section struct{ name, content string }
	sections := []section{}
	var current *section
	scan := bufio.NewScanner(bytes.NewReader(legacy))
	scan.Buffer(make([]byte, 64*1024), 2*1024*1024)
	for scan.Scan() {
		out.TotalLines++
		line := strings.TrimSpace(scan.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			if current != nil {
				sections = append(sections, *current)
			}
			current = &section{name: strings.ToLower(strings.TrimSpace(strings.Trim(line, "[]")))}
			continue
		}
		if current != nil {
			if current.content != "" {
				current.content += "\n"
			}
			current.content += line
		}
	}
	if e = scan.Err(); e != nil {
		return out, e
	}
	if current != nil {
		sections = append(sections, *current)
	}

	counts := map[string]int{}
	add := func(kind, title, body string, tags ...string) {
		counts[kind]++
		out.Candidates = append(out.Candidates, LegacyCandidate{Kind: kind, SuggestedID: fmt.Sprintf("legacy-%s-%d", strings.ToLower(kind), counts[kind]), Title: title, Body: body, Tags: tags, Source: source, Confidence: "UNVERIFIED", CurrentTruth: false})
	}
	for _, sec := range sections {
		switch {
		case sec.name == "agents" || sec.name == "agent":
			for _, line := range strings.Split(sec.content, "\n") {
				if parts := strings.SplitN(line, ":", 2); len(parts) == 2 && strings.TrimSpace(parts[0]) != "" {
					out.Agents = append(out.Agents, strings.TrimSpace(parts[0]))
				}
			}
		case strings.Contains(sec.name, "decision"):
			add("DECISION", "Legacy decision: "+sec.name, sec.content, "legacy")
		case sec.name == "contracts" || sec.name == "contract" || sec.name == "guardrails" || sec.name == "rules":
			add("CONTRACT", "Legacy contract: "+sec.name, sec.content, "legacy")
		default:
			add("DOCUMENT", "Legacy context: "+sec.name, sec.content, "legacy")
		}
	}
	if len(sections) == 0 {
		add("DOCUMENT", "Legacy .agents context", string(legacy), "legacy")
	}
	return out, nil
}
