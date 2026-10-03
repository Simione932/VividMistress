package db

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// JSONStore implements Store using flat JSON files on disk.
//
// Layout (v2, per-client):
//
//	<dir>/providers.json
//	<dir>/clients/<client-id>/settings.json
//	<dir>/clients/<client-id>/system_prompts.json
//	<dir>/clients/<client-id>/conversations/<id>.json
//
// Legacy layout (v1) is automatically migrated into the first client that
// accesses storage:
//
//	<dir>/settings.json
//	<dir>/system_prompts.json
//	<dir>/conversations/<id>.json
type JSONStore struct {
	mu   sync.RWMutex
	dir  string
	prov map[string]Provider
}

// NewJSONStore creates a store rooted at dir, loading existing data.
func NewJSONStore(dir string) (*JSONStore, error) {
	s := &JSONStore{
		dir:  dir,
		prov: make(map[string]Provider),
	}
	// Create the root data dir. Per-client conversation dirs are created
	// lazily (see ensureClientLocked) so the first browser can claim any
	// legacy v1 data.
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	if err := s.loadProviders(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *JSONStore) loadProviders() error {
	path := filepath.Join(s.dir, "providers.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	var list []Provider
	if err := json.Unmarshal(data, &list); err != nil {
		return err
	}
	for _, p := range list {
		s.prov[p.ID] = p
	}
	return nil
}

func (s *JSONStore) saveProviders() error {
	list := make([]Provider, 0, len(s.prov))
	for _, p := range s.prov {
		list = append(list, p)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.dir, "providers.json"), data, 0o644)
}

// --- Client namespacing & legacy migration ---

func (s *JSONStore) clientsDir() string {
	return filepath.Join(s.dir, "clients")
}

func (s *JSONStore) clientDir(clientID string) string {
	return filepath.Join(s.clientsDir(), sanitizeClientID(clientID))
}

// sanitizeClientID strips path separators and other risky characters from a
// client ID so it is safe to use as a directory name. Empty or all-invalid
// IDs fall back to "default".
func sanitizeClientID(clientID string) string {
	var b strings.Builder
	for _, r := range clientID {
		isSafe := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '_' || r == '-'
		if isSafe {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "default"
	}
	return b.String()
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// hasLegacyLocked reports whether any pre-v2 data exists at the store root.
// Must be called with the write lock held.
func (s *JSONStore) hasLegacyLocked() bool {
	if fileExists(filepath.Join(s.dir, "settings.json")) {
		return true
	}
	if fileExists(filepath.Join(s.dir, "system_prompts.json")) {
		return true
	}
	convDir := filepath.Join(s.dir, "conversations")
	entries, err := os.ReadDir(convDir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".json" {
			return true
		}
	}
	return false
}

// ensureClientLocked creates the per-client storage directory, performing the
// one-time migration of legacy v1 data into the first client that requests
// storage. Must be called with the write lock held.
func (s *JSONStore) ensureClientLocked(clientID string) (string, error) {
	clientID = sanitizeClientID(clientID)
	dir := s.clientDir(clientID)

	// Already exists — nothing to do.
	if stat, err := os.Stat(dir); err == nil && stat.IsDir() {
		return dir, nil
	}

	// First client ever? Also the only client that can claim legacy data.
	if _, err := os.Stat(s.clientsDir()); errors.Is(err, os.ErrNotExist) {
		if s.hasLegacyLocked() {
			if err := s.migrateLegacyLocked(clientID); err != nil {
				return "", err
			}
			return dir, nil
		}
	}

	if err := os.MkdirAll(filepath.Join(dir, "conversations"), 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// migrateLegacyLocked moves the legacy v1 files (<dir>/settings.json,
// <dir>/system_prompts.json, <dir>/conversations/*.json) into the given
// client's directory. The clients dir is created only after all files are
// moved, so a partially completed migration is retried by the next client.
// Must be called with the write lock held.
func (s *JSONStore) migrateLegacyLocked(clientID string) error {
	clientDir := s.clientDir(clientID)
	convDir := filepath.Join(clientDir, "conversations")
	if err := os.MkdirAll(convDir, 0o755); err != nil {
		return err
	}

	// Move legacy conversation files.
	legacyConvDir := filepath.Join(s.dir, "conversations")
	if entries, err := os.ReadDir(legacyConvDir); err == nil {
		for _, e := range entries {
			if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
				continue
			}
			if err := moveFile(filepath.Join(legacyConvDir, e.Name()), filepath.Join(convDir, e.Name())); err != nil {
				return err
			}
		}
	}

	// Move legacy settings and system prompts.
	for _, name := range []string{"settings.json", "system_prompts.json"} {
		src := filepath.Join(s.dir, name)
		if !fileExists(src) {
			continue
		}
		if err := moveFile(src, filepath.Join(clientDir, name)); err != nil {
			return err
		}
	}

	// Mark migration complete.
	return os.MkdirAll(s.clientsDir(), 0o755)
}

// moveFile renames src to dst, falling back to copy+remove if the rename
// fails (e.g. exotic filesystems). Missing sources are treated as success so
// a retried migration can proceed.
func moveFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	data, err := os.ReadFile(src)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return err
	}
	return os.Remove(src)
}

// --- System prompts (local to a client/browser and global across all users) ---

func (s *JSONStore) globalSystemPromptsPath() string {
	return filepath.Join(s.dir, "system_prompts.json")
}

func (s *JSONStore) normalizeSystemPromptScope(p SystemPrompt) SystemPrompt {
	if p.Scope == "" {
		p.Scope = "local"
	}
	return p
}

func (s *JSONStore) mergeSystemPrompts(globalPrompts, localPrompts []SystemPrompt) []SystemPrompt {
	merged := make([]SystemPrompt, 0, len(globalPrompts)+len(localPrompts))
	byID := make(map[string]SystemPrompt, len(globalPrompts)+len(localPrompts))
	for _, p := range globalPrompts {
		p = s.normalizeSystemPromptScope(p)
		byID[p.ID] = p
	}
	for _, p := range localPrompts {
		p = s.normalizeSystemPromptScope(p)
		byID[p.ID] = p
	}
	for _, p := range byID {
		merged = append(merged, p)
	}
	sort.Slice(merged, func(i, j int) bool {
		if merged[i].Scope == merged[j].Scope {
			return merged[i].Name < merged[j].Name
		}
		if merged[i].Scope == "global" {
			return true
		}
		return false
	})
	return merged
}

func (s *JSONStore) ListSystemPrompts(clientID string) ([]SystemPrompt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	dir, err := s.ensureClientLocked(clientID)
	if err != nil {
		return nil, err
	}
	globalPrompts, err := s.loadSystemPromptsFromPath(s.globalSystemPromptsPath())
	if err != nil {
		return nil, err
	}
	localPrompts, err := s.loadSystemPromptsFromDir(dir)
	if err != nil {
		return nil, err
	}
	return s.mergeSystemPrompts(globalPrompts, localPrompts), nil
}

func (s *JSONStore) GetSystemPrompt(clientID, id string) (SystemPrompt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	dir, err := s.ensureClientLocked(clientID)
	if err != nil {
		return SystemPrompt{}, err
	}
	localPrompts, err := s.loadSystemPromptsFromDir(dir)
	if err != nil {
		return SystemPrompt{}, err
	}
	for _, p := range localPrompts {
		if p.ID == id {
			return s.normalizeSystemPromptScope(p), nil
		}
	}
	globalPrompts, err := s.loadSystemPromptsFromPath(s.globalSystemPromptsPath())
	if err != nil {
		return SystemPrompt{}, err
	}
	for _, p := range globalPrompts {
		if p.ID == id {
			return s.normalizeSystemPromptScope(p), nil
		}
	}
	return SystemPrompt{}, errors.New("system prompt not found")
}

func (s *JSONStore) SaveSystemPrompt(clientID string, p SystemPrompt) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p = s.normalizeSystemPromptScope(p)
	if p.Scope == "global" {
		prompts, err := s.loadSystemPromptsFromPath(s.globalSystemPromptsPath())
		if err != nil {
			return err
		}
		updated := false
		for i, existing := range prompts {
			if existing.ID == p.ID {
				prompts[i] = p
				updated = true
				break
			}
		}
		if !updated {
			prompts = append(prompts, p)
		}
		return s.saveSystemPromptsToPath(s.globalSystemPromptsPath(), prompts)
	}
	if _, err := s.ensureClientLocked(clientID); err != nil {
		return err
	}
	dir := s.clientDir(clientID)
	prompts, err := s.loadSystemPromptsFromDir(dir)
	if err != nil {
		return err
	}
	updated := false
	for i, existing := range prompts {
		if existing.ID == p.ID {
			prompts[i] = p
			updated = true
			break
		}
	}
	if !updated {
		prompts = append(prompts, p)
	}
	return s.saveSystemPromptsToDir(dir, prompts)
}

func (s *JSONStore) DeleteSystemPrompt(clientID, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	dir, err := s.ensureClientLocked(clientID)
	if err != nil {
		return err
	}
	localPrompts, err := s.loadSystemPromptsFromDir(dir)
	if err != nil {
		return err
	}
	for _, p := range localPrompts {
		if p.ID == id {
			filtered := make([]SystemPrompt, 0, len(localPrompts))
			for _, existing := range localPrompts {
				if existing.ID == id {
					continue
				}
				filtered = append(filtered, existing)
			}
			return s.saveSystemPromptsToDir(dir, filtered)
		}
	}
	globalPrompts, err := s.loadSystemPromptsFromPath(s.globalSystemPromptsPath())
	if err != nil {
		return err
	}
	filtered := make([]SystemPrompt, 0, len(globalPrompts))
	found := false
	for _, p := range globalPrompts {
		if p.ID == id {
			found = true
			continue
		}
		filtered = append(filtered, p)
	}
	if !found {
		return errors.New("system prompt not found")
	}
	return s.saveSystemPromptsToPath(s.globalSystemPromptsPath(), filtered)
}

func (s *JSONStore) loadSystemPromptsFromPath(path string) ([]SystemPrompt, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []SystemPrompt{}, nil
		}
		return nil, err
	}
	var prompts []SystemPrompt
	if err := json.Unmarshal(data, &prompts); err != nil {
		return nil, err
	}
	if prompts == nil {
		prompts = []SystemPrompt{}
	}
	for i := range prompts {
		prompts[i] = s.normalizeSystemPromptScope(prompts[i])
	}
	return prompts, nil
}

func (s *JSONStore) loadSystemPromptsFromDir(dir string) ([]SystemPrompt, error) {
	return s.loadSystemPromptsFromPath(filepath.Join(dir, "system_prompts.json"))
}

func (s *JSONStore) saveSystemPromptsToPath(path string, prompts []SystemPrompt) error {
	for i := range prompts {
		prompts[i] = s.normalizeSystemPromptScope(prompts[i])
	}
	data, err := json.MarshalIndent(prompts, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func (s *JSONStore) saveSystemPromptsToDir(dir string, prompts []SystemPrompt) error {
	return s.saveSystemPromptsToPath(filepath.Join(dir, "system_prompts.json"), prompts)
}

// --- App settings (scoped to a client/browser) ---

func (s *JSONStore) GetAppSettings(clientID string) (AppSettings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	dir, err := s.ensureClientLocked(clientID)
	if err != nil {
		return AppSettings{}, err
	}
	data, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return AppSettings{}, nil
		}
		return AppSettings{}, err
	}
	var settings AppSettings
	if err := json.Unmarshal(data, &settings); err != nil {
		return AppSettings{}, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return AppSettings{}, err
	}
	if _, ok := fields["return_to_send"]; !ok {
		settings.ReturnToSend = true
	}
	return settings, nil
}

func (s *JSONStore) SaveAppSettings(clientID string, settings AppSettings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	dir, err := s.ensureClientLocked(clientID)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "settings.json"), data, 0o644)
}

// --- Providers (shared across all clients) ---

func (s *JSONStore) ListProviders() ([]Provider, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	list := make([]Provider, 0, len(s.prov))
	for _, p := range s.prov {
		list = append(list, p)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	return list, nil
}

func (s *JSONStore) GetProvider(id string) (Provider, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.prov[id]
	if !ok {
		return Provider{}, errors.New("provider not found")
	}
	return p, nil
}

func (s *JSONStore) SaveProvider(p Provider) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prov[p.ID] = p
	return s.saveProviders()
}

func (s *JSONStore) DeleteProvider(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.prov[id]; !ok {
		return errors.New("provider not found")
	}
	delete(s.prov, id)
	return s.saveProviders()
}

// --- Conversations (scoped to a client/browser) ---

func (s *JSONStore) convPathInDir(dir, id string) string {
	return filepath.Join(dir, "conversations", id+".json")
}

func (s *JSONStore) ListConversations(clientID string) ([]ConversationSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	dir, err := s.ensureClientLocked(clientID)
	if err != nil {
		return nil, err
	}
	convDir := filepath.Join(dir, "conversations")
	entries, err := os.ReadDir(convDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []ConversationSummary{}, nil
		}
		return nil, err
	}
	sums := make([]ConversationSummary, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		id := e.Name()[:len(e.Name())-5]
		data, err := os.ReadFile(filepath.Join(convDir, e.Name()))
		if err != nil {
			continue
		}
		var c Conversation
		if err := json.Unmarshal(data, &c); err != nil {
			continue
		}
		if c.ID == "" {
			c.ID = id
		}
		sums = append(sums, ConversationSummary{ID: c.ID, Title: c.Title, UpdatedAt: c.UpdatedAt})
	}
	sort.Slice(sums, func(i, j int) bool { return sums[i].UpdatedAt > sums[j].UpdatedAt })
	return sums, nil
}

func (s *JSONStore) GetConversation(clientID, id string) (Conversation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	dir, err := s.ensureClientLocked(clientID)
	if err != nil {
		return Conversation{}, err
	}
	data, err := os.ReadFile(s.convPathInDir(dir, id))
	if err != nil {
		return Conversation{}, err
	}
	var c Conversation
	if err := json.Unmarshal(data, &c); err != nil {
		return Conversation{}, err
	}
	return c, nil
}

func (s *JSONStore) SaveConversation(clientID string, c Conversation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	dir, err := s.ensureClientLocked(clientID)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.convPathInDir(dir, c.ID), data, 0o644)
}

func (s *JSONStore) DeleteConversation(clientID, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	dir, err := s.ensureClientLocked(clientID)
	if err != nil {
		return err
	}
	err = os.Remove(s.convPathInDir(dir, id))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (s *JSONStore) ClearConversations(clientID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	dir, err := s.ensureClientLocked(clientID)
	if err != nil {
		return err
	}
	convDir := filepath.Join(dir, "conversations")
	entries, err := os.ReadDir(convDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		if err := os.Remove(filepath.Join(convDir, e.Name())); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

// ListAllConversations returns every conversation across all clients. The
// returned client IDs are the sanitized directory names so callers can
// round-trip them back through the client-scoped API.
func (s *JSONStore) ListAllConversations() ([]ClientConversation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []ClientConversation{}
	clientsDir := s.clientsDir()
	entries, err := os.ReadDir(clientsDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return out, nil
		}
		return nil, err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		convDir := filepath.Join(clientsDir, e.Name(), "conversations")
		convEntries, err := os.ReadDir(convDir)
		if err != nil {
			continue
		}
		for _, ce := range convEntries {
			if ce.IsDir() || filepath.Ext(ce.Name()) != ".json" {
				continue
			}
			id := ce.Name()[:len(ce.Name())-5]
			data, err := os.ReadFile(filepath.Join(convDir, ce.Name()))
			if err != nil {
				continue
			}
			var c Conversation
			if err := json.Unmarshal(data, &c); err != nil {
				continue
			}
			if c.ID == "" {
				c.ID = id
			}
			out = append(out, ClientConversation{ClientID: e.Name(), Conversation: c})
		}
	}
	return out, nil
}
