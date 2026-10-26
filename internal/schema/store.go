package schema

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	// BindingExchange marks a schema bound to an exchange.
	BindingExchange = "exchange"
	// BindingQueue marks a schema bound to a queue.
	BindingQueue = "queue"
)

// Binding attaches a saved schema to an exchange or a queue so the UI can
// suggest it automatically.
type Binding struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

// SchemaMeta is the metadata of a saved schema.
type SchemaMeta struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	MessageType string    `json:"message_type"`
	Bindings    []Binding `json:"bindings,omitempty"`
	Files       []string  `json:"files"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Store errors.
var (
	ErrNotFound = errors.New("schema not found")
	ErrExists   = errors.New("schema already exists")
)

// Store persists schemas as directories on disk:
// <dir>/<id>/schema.json (metadata) and <dir>/<id>/files/*.proto (sources).
type Store struct {
	dir string
	mu  sync.Mutex
}

// NewStore returns a store rooted at dir. The directory is created lazily
// on the first write, so a missing directory is not an error.
func NewStore(dir string) *Store {
	return &Store{dir: dir}
}

// Dir returns the root directory of the store.
func (s *Store) Dir() string {
	return s.dir
}

// List returns the metadata of all saved schemas sorted by ID.
// A missing or unreadable root yields an empty list.
func (s *Store) List() ([]SchemaMeta, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read schemas dir: %w", err)
	}
	metas := make([]SchemaMeta, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		m, err := readMeta(filepath.Join(s.dir, e.Name()))
		if err != nil {
			continue
		}
		metas = append(metas, *m)
	}
	sort.Slice(metas, func(i, j int) bool { return metas[i].ID < metas[j].ID })
	return metas, nil
}

// Get returns the metadata of one schema.
func (s *Store) Get(id string) (*SchemaMeta, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.get(id)
}

func (s *Store) get(id string) (*SchemaMeta, error) {
	if !safeID(id) {
		return nil, ErrNotFound
	}
	m, err := readMeta(s.schemaDir(id))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return m, nil
}

// Files returns the proto sources of a schema keyed by file name.
func (s *Store) Files(id string) (map[string]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.get(id); err != nil {
		return nil, err
	}
	return s.readFiles(id)
}

func (s *Store) readFiles(id string) (map[string]string, error) {
	root := s.filesDir(id)
	files := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		raw, err := os.ReadFile(path) //nolint:gosec // path is built from sanitized names
		if err != nil {
			return fmt.Errorf("read schema file %s: %w", rel, err)
		}
		files[filepath.ToSlash(rel)] = string(raw)
		return nil
	})
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("schema %s has no files", id)
	}
	if err != nil {
		return nil, fmt.Errorf("read schema files: %w", err)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("schema %s has no files", id)
	}
	return files, nil
}

// Create persists a new schema. The ID is derived from the name; when taken,
// a numeric suffix (-2, -3, ...) is appended. Files are written atomically
// (the schema directory appears only when fully written).
func (s *Store) Create(meta SchemaMeta, files map[string]string) (*SchemaMeta, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := validateSave(meta, files); err != nil {
		return nil, err
	}
	id := slugify(meta.Name)
	base := id
	for i := 2; ; i++ {
		if _, err := s.get(id); errors.Is(err, ErrNotFound) {
			break
		}
		if _, err := s.get(id); err != nil && !errors.Is(err, ErrNotFound) {
			return nil, err
		}
		id = fmt.Sprintf("%s-%d", base, i)
	}
	now := time.Now().UTC()
	meta.ID = id
	meta.CreatedAt = now
	meta.UpdatedAt = now
	tmp, err := s.buildTmp(id, meta, files)
	if err != nil {
		return nil, err
	}
	final := s.schemaDir(id)
	if err := os.MkdirAll(s.dir, 0o750); err != nil {
		_ = os.RemoveAll(tmp)
		return nil, fmt.Errorf("create schemas dir: %w", err)
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.RemoveAll(tmp)
		return nil, fmt.Errorf("install schema dir: %w", err)
	}
	return &meta, nil
}

// Update replaces metadata and files of an existing schema. nil files keeps
// the existing proto sources; an empty name keeps the current name.
// The ID never changes.
func (s *Store) Update(id string, meta SchemaMeta, files map[string]string) (*SchemaMeta, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, err := s.get(id)
	if err != nil {
		return nil, err
	}
	updated := *existing
	if meta.Name != "" {
		updated.Name = meta.Name
	}
	if meta.MessageType != "" {
		updated.MessageType = meta.MessageType
	}
	if meta.Bindings != nil {
		updated.Bindings = meta.Bindings
	}
	if files == nil {
		files, err = s.readFiles(id)
		if err != nil {
			return nil, err
		}
	}
	if err := validateSave(updated, files); err != nil {
		return nil, err
	}
	updated.UpdatedAt = time.Now().UTC()

	tmp, err := s.buildTmp(id, updated, files)
	if err != nil {
		return nil, err
	}
	final := s.schemaDir(id)
	old := final + ".old"
	if err := os.MkdirAll(s.dir, 0o750); err != nil {
		_ = os.RemoveAll(tmp)
		return nil, fmt.Errorf("create schemas dir: %w", err)
	}
	if err := os.Rename(final, old); err != nil {
		_ = os.RemoveAll(tmp)
		return nil, fmt.Errorf("replace schema dir: %w", err)
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Rename(old, final)
		_ = os.RemoveAll(tmp)
		return nil, fmt.Errorf("install schema dir: %w", err)
	}
	if err := os.RemoveAll(old); err != nil {
		return nil, fmt.Errorf("cleanup old schema dir: %w", err)
	}
	return &updated, nil
}

// Delete removes a schema.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.get(id); err != nil {
		return err
	}
	if err := os.RemoveAll(s.schemaDir(id)); err != nil {
		return fmt.Errorf("remove schema: %w", err)
	}
	return nil
}

func (s *Store) schemaDir(id string) string {
	return filepath.Join(s.dir, id)
}

func (s *Store) filesDir(id string) string {
	return filepath.Join(s.dir, id, "files")
}

// buildTmp materializes a complete schema directory under .tmp-<id>
// (files first, schema.json last) and returns its path. The caller renames
// it into place.
func (s *Store) buildTmp(id string, meta SchemaMeta, files map[string]string) (string, error) {
	tmp := s.schemaDir(".tmp-" + id)
	if err := os.MkdirAll(filepath.Join(tmp, "files"), 0o750); err != nil {
		return "", fmt.Errorf("create schema dir: %w", err)
	}

	names := make([]string, 0, len(files))
	for name := range files {
		if err := validateFileName(name); err != nil {
			_ = os.RemoveAll(tmp)
			return "", err
		}
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if len(files[name]) > MaxFileSize {
			_ = os.RemoveAll(tmp)
			return "", fmt.Errorf("file %s too large: %d > %d bytes", name, len(files[name]), MaxFileSize)
		}
		target := filepath.Join(tmp, "files", filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
			_ = os.RemoveAll(tmp)
			return "", fmt.Errorf("create schema file dir: %w", err)
		}
		if err := atomicWrite(target, []byte(files[name])); err != nil {
			_ = os.RemoveAll(tmp)
			return "", err
		}
	}

	meta.Files = names
	raw, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		_ = os.RemoveAll(tmp)
		return "", fmt.Errorf("encode schema meta: %w", err)
	}
	if err := atomicWrite(filepath.Join(tmp, "schema.json"), raw); err != nil {
		_ = os.RemoveAll(tmp)
		return "", err
	}
	return tmp, nil
}

func atomicWrite(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("install %s: %w", filepath.Base(path), err)
	}
	return nil
}

func readMeta(schemaDir string) (*SchemaMeta, error) {
	raw, err := os.ReadFile(filepath.Join(schemaDir, "schema.json")) //nolint:gosec // path is built from sanitized ids
	if err != nil {
		return nil, err
	}
	var meta SchemaMeta
	if err := json.Unmarshal(raw, &meta); err != nil {
		return nil, fmt.Errorf("parse schema.json: %w", err)
	}
	return &meta, nil
}

func validateSave(meta SchemaMeta, files map[string]string) error {
	if strings.TrimSpace(meta.Name) == "" {
		return fmt.Errorf("schema name is required")
	}
	if strings.TrimSpace(meta.MessageType) == "" {
		return fmt.Errorf("message_type is required")
	}
	if len(files) == 0 {
		return fmt.Errorf("schema has no files")
	}
	if len(files) > MaxFiles {
		return fmt.Errorf("too many files: %d > %d", len(files), MaxFiles)
	}
	for _, b := range meta.Bindings {
		if b.Kind != BindingExchange && b.Kind != BindingQueue {
			return fmt.Errorf("invalid binding kind %q", b.Kind)
		}
		if strings.TrimSpace(b.Name) == "" {
			return fmt.Errorf("binding name is required")
		}
	}
	return nil
}

// validateFileName rejects path traversal and separators outside the
// POSIX subset.
func validateFileName(name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("empty file name")
	}
	if strings.Contains(name, "..") || strings.HasPrefix(name, "/") || strings.Contains(name, "\\") {
		return fmt.Errorf("invalid file name %q", name)
	}
	return nil
}

// safeID guards path traversal when an ID is used in file paths.
func safeID(id string) bool {
	return id != "" && !strings.Contains(id, "..") && !strings.Contains(id, "/") && !strings.Contains(id, "\\")
}

// slugify converts a schema name into a filesystem-safe ID.
func slugify(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	out := b.String()
	for strings.Contains(out, "--") {
		out = strings.ReplaceAll(out, "--", "-")
	}
	out = strings.Trim(out, "-")
	if len(out) > 64 {
		out = out[:64]
	}
	out = strings.Trim(out, "-")
	if out == "" {
		out = "schema"
	}
	return out
}
