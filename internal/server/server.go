package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"pi-chat-gateway/internal/db"
	"pi-chat-gateway/internal/llm"
	"pi-chat-gateway/internal/voice"
	"pi-chat-gateway/web"
)

// DefaultModel is the LLM model used when a conversation has no explicit
// model configured. Everything LLM related is managed in the back end now —
// the web page no longer exposes provider / model / role settings.
const DefaultModel = "gemma4:31b"

// DefaultRolesDir is the folder scanned for role .txt files when none is
// configured via the -roles flag.
const DefaultRolesDir = "roles"

// Config carries back-end-only configuration (no UI for any of this).
type Config struct {
	RolesDir string // folder containing role .txt files (DefaultRolesDir)
	Model    string // default model to use (DefaultModel)
}

// DefaultConfig returns the stock Vivid Mistress configuration.
func DefaultConfig() Config {
	return Config{
		RolesDir: DefaultRolesDir,
		Model:    DefaultModel,
	}
}

// Role is a single role discovered in the roles directory.
type Role struct {
	Name   string // file name without the .txt extension
	Prompt string // full file contents (the system prompt)
}

// Server is the HTTP API server.
type Server struct {
	store    db.Store
	llm      *llm.Client
	mux      *http.ServeMux
	cfg      Config
	chastity *ChastityState
}

// New creates a Server with all routes registered and default back-end config.
func New(store db.Store, llmClient *llm.Client) *Server {
	return NewWithConfig(store, llmClient, DefaultConfig())
}

// NewWithConfig creates a Server with the given back-end config.
func NewWithConfig(store db.Store, llmClient *llm.Client, cfg Config) *Server {
	if strings.TrimSpace(cfg.RolesDir) == "" {
		cfg.RolesDir = DefaultRolesDir
	}
	if strings.TrimSpace(cfg.Model) == "" {
		cfg.Model = DefaultModel
	}
	s := &Server{
		store:    store,
		llm:      llmClient,
		mux:      http.NewServeMux(),
		cfg:      cfg,
		chastity: NewChastityState(),
	}

	s.ensureProvider()
	s.mux.HandleFunc("GET /api/session", s.handleSession)

	s.mux.HandleFunc("GET /api/providers", s.handleListProviders)
	s.mux.HandleFunc("POST /api/providers", s.handleSaveProvider)
	s.mux.HandleFunc("PUT /api/providers/{id}", s.handleSaveProvider)
	s.mux.HandleFunc("DELETE /api/providers/{id}", s.handleDeleteProvider)
	s.mux.HandleFunc("GET /api/models", s.handleListModels)
	s.mux.HandleFunc("GET /api/system-prompts", s.handleListSystemPrompts)
	s.mux.HandleFunc("POST /api/system-prompts", s.handleSaveSystemPrompt)
	s.mux.HandleFunc("DELETE /api/system-prompts/{id}", s.handleDeleteSystemPrompt)
	s.mux.HandleFunc("GET /api/settings", s.handleGetSettings)
	s.mux.HandleFunc("PUT /api/settings", s.handleSaveSettings)
	s.mux.HandleFunc("GET /api/voice", s.handleGetVoice)
	s.mux.HandleFunc("PUT /api/voice", s.handleSetVoice)
	s.mux.HandleFunc("POST /api/voice/speak", s.handleVoiceSpeak)
	s.mux.HandleFunc("GET /api/conversations", s.handleListConversations)
	s.mux.HandleFunc("DELETE /api/conversations", s.handleClearConversations)
	s.mux.HandleFunc("POST /api/conversations", s.handleCreateConversation)
	s.mux.HandleFunc("GET /api/conversations/{id}", s.handleGetConversation)
	s.mux.HandleFunc("PUT /api/conversations/{id}", s.handleUpdateConversation)
	s.mux.HandleFunc("DELETE /api/conversations/{id}", s.handleDeleteConversation)
	s.mux.HandleFunc("POST /api/chat", s.handleChat)
	s.mux.HandleFunc("POST /api/chat/regenerate", s.handleRegenerate)
	s.mux.HandleFunc("POST /api/chat/suggestions", s.handleSuggestions)

	return s
}

// Handler returns the root http.Handler (serves embedded static + API).
func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			s.mux.ServeHTTP(w, r)
			return
		}
		web.ServeStatic(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// clientIDFromRequest extracts the client identifier from the request.
// It checks the X-Client-ID header, the client_id query param, and the client_id cookie.
// Falls back to "default" if none is present.
func clientIDFromRequest(r *http.Request) string {
	if id := strings.TrimSpace(r.Header.Get("X-Client-ID")); id != "" {
		return id
	}
	if id := strings.TrimSpace(r.URL.Query().Get("client_id")); id != "" {
		return id
	}
	if cookie, err := r.Cookie("client_id"); err == nil && strings.TrimSpace(cookie.Value) != "" {
		return strings.TrimSpace(cookie.Value)
	}
	return "default"
}

// --- Back-end helpers (roles / provider / model) ---

// loadRoles reads every role *.txt file in the configured roles directory,
// sorted by file name. Each role file becomes a system prompt.
func (s *Server) loadRoles() ([]Role, error) {
	dir := strings.TrimSpace(s.cfg.RolesDir)
	if dir == "" {
		dir = DefaultRolesDir
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("roles directory %q not found", dir)
		}
		return nil, err
	}
	var roles []Role
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.EqualFold(filepath.Ext(name), ".txt") {
			continue
		}
		// models.txt lists available LLM models — not a role prompt.
		if strings.EqualFold(name, "models.txt") {
			continue
		}
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			log.Printf("[roles] failed to read %s: %v", path, err)
			continue
		}
		roles = append(roles, Role{
			Name:   strings.TrimSuffix(name, filepath.Ext(name)),
			Prompt: strings.TrimSpace(string(data)),
		})
	}
	sort.Slice(roles, func(i, j int) bool {
		return strings.ToLower(roles[i].Name) < strings.ToLower(roles[j].Name)
	})
	return roles, nil
}

// loadModels reads the list of available LLM model names from models.txt
// in the configured roles directory. Each non-empty, non-comment line is a
// model name (e.g. "gemma4:31b"). Returns a single-entry default list when
// the file is missing or empty so the app still works out of the box.
func (s *Server) loadModels() ([]string, error) {
	dir := strings.TrimSpace(s.cfg.RolesDir)
	if dir == "" {
		dir = DefaultRolesDir
	}
	data, err := os.ReadFile(filepath.Join(dir, "models.txt"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []string{DefaultModel}, nil
		}
		return nil, err
	}
	var models []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		models = append(models, line)
	}
	if len(models) == 0 {
		return []string{DefaultModel}, nil
	}
	return models, nil
}

// roleList finds the loaded role by name (case-insensitive).
func roleByIndex(roles []Role, n int) (Role, bool) {
	if n < 1 || n > len(roles) {
		return Role{}, false
	}
	return roles[n-1], true
}

// defaultRole returns the role to apply when a conversation has no system
// prompt yet. Prefers "Default", otherwise the first role found.
func (s *Server) defaultRole() (Role, bool) {
	roles, err := s.loadRoles()
	if err != nil || len(roles) == 0 {
		return Role{}, false
	}
	for _, r := range roles {
		if strings.EqualFold(r.Name, "Default") {
			return r, true
		}
	}
	return roles[0], true
}

// roleNameForConversation returns the name of the role whose prompt content
// matches the conversation's system prompt, or "" when it doesn't match a
// known role file.
func (s *Server) roleNameForConversation(c db.Conversation) string {
	prompt := strings.TrimSpace(c.Settings.SystemPrompt)
	if prompt == "" {
		return ""
	}
	roles, err := s.loadRoles()
	if err != nil {
		return ""
	}
	for _, r := range roles {
		if strings.TrimSpace(r.Prompt) == prompt {
			return r.Name
		}
	}
	return ""
}

// formatRoleList renders the numbered role listing used by the "role"
// command, e.g. "1) Coach", "2) Default", ...
func (s *Server) formatRoleList() string {
	roles, err := s.loadRoles()
	if err != nil {
		return "No roles found: " + err.Error()
	}
	if len(roles) == 0 {
		return "No roles found in " + s.cfg.RolesDir
	}
	var b strings.Builder
	for i, r := range roles {
		fmt.Fprintf(&b, "%d) %s", i+1, r.Name)
		if i < len(roles)-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

// formatModelList renders the numbered model listing used by the "model"
// command, e.g. "1) gemma4:31b", "2) gpt-oss:120b", ...
func (s *Server) formatModelList() string {
	models, err := s.loadModels()
	if err != nil {
		return "No models available: " + err.Error()
	}
	var b strings.Builder
	for i, m := range models {
		fmt.Fprintf(&b, "%d) %s", i+1, m)
		if i < len(models)-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

// defaultProvider returns the single back-end provider. If the conversation
// references a provider that no longer exists it falls back to any available
// provider, so old data keeps working.
func (s *Server) providerFor(c *db.Conversation) (db.Provider, error) {
	if c != nil && strings.TrimSpace(c.Settings.ProviderID) != "" {
		if p, err := s.store.GetProvider(c.Settings.ProviderID); err == nil {
			return p, nil
		}
	}
	all, err := s.store.ListProviders()
	if err != nil {
		return db.Provider{}, err
	}
	if len(all) == 0 {
		return db.Provider{}, fmt.Errorf("no Ollama provider configured — add your base URL and API key to data/providers.json")
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Name < all[j].Name })
	return all[0], nil
}

// modelFor returns the model to use for a conversation, defaulting to the
// back-end configured model.
func (s *Server) modelFor(c *db.Conversation) string {
	if c != nil && strings.TrimSpace(c.Settings.Model) != "" {
		return strings.TrimSpace(c.Settings.Model)
	}
	if strings.TrimSpace(s.cfg.Model) != "" {
		return s.cfg.Model
	}
	return DefaultModel
}

// ensureProvider creates a default Ollama provider entry when none exist so
// the app works out of the box (add the API key to data/providers.json).
func (s *Server) ensureProvider() {
	all, err := s.store.ListProviders()
	if err != nil {
		log.Printf("[config] failed to list providers: %v", err)
		return
	}
	if len(all) > 0 {
		return
	}
	p := db.Provider{
		ID:      db.NewID(),
		Name:    "Ollama",
		BaseURL: "https://api.ollama.com",
		Type:    "ollama",
	}
	if err := s.store.SaveProvider(p); err != nil {
		log.Printf("[config] failed to save default provider: %v", err)
		return
	}
	log.Printf("[config] created default Ollama provider (https://api.ollama.com) — add your API key to data/providers.json")
}

// ensureConversationDefaults fills a single-chat conversation with the
// back-end defaults (provider, model, default role) when they are missing.
// It returns true if the conversation was changed.
func (s *Server) ensureConversationDefaults(c *db.Conversation) (bool, error) {
	changed := false
	if strings.TrimSpace(c.Settings.Model) == "" {
		c.Settings.Model = s.modelFor(c)
		changed = true
	}
	if strings.TrimSpace(c.Settings.ProviderID) == "" {
		if p, err := s.providerFor(c); err == nil {
			c.Settings.ProviderID = p.ID
			changed = true
		}
	}
	if strings.TrimSpace(c.Settings.SystemPrompt) == "" {
		if r, ok := s.defaultRole(); ok {
			c.Settings.SystemPrompt = r.Prompt
			log.Printf("[session] applied default role %q", r.Name)
			changed = true
		}
	}
	if c.Settings.MaxTurns <= 0 {
		c.Settings.MaxTurns = 20
		changed = true
	}
	return changed, nil
}

// helpText is returned by the "help" chat command. It is rendered as markdown
// (monospace command chips via `backticks` + bold) so each entry is easy to
// scan in the chat bubble — plain space-aligned text collapses in HTML.
const helpText = "**Commands:**\n" +
	"`help` – show this list\n" +
	"`status` – show current status: voice, role, model, speed, delay, buttons\n" +
	"`role` – list the available roles (numbered)\n" +
	"`role [n]` – switch to that role, e.g. `role 2`\n" +
	"`model` – list the available models (numbered)\n" +
	"`model [n]` – switch to that model, e.g. `model 1`\n" +
	"`voice` – list the voices (numbered)\n" +
	"`voice [n]` – switch to that voice, e.g. `voice 5`\n" +
	"`speed [n]` – voice speed: 3 = 1.3x, 5 = 1.5x (default 3)\n" +
	"`delay [n]` – pause after the voice reply before pushing continue (seconds, default 10)\n" +
	"`stop` – stop pushing messages automatically\n" +
	"`start` – start pushing messages automatically\n" +
	"`buttons [n]` – suggestion buttons per reply, 1–5 (default 5)\n" +
	"`clear` – clear the conversation context (keeps your role)\n" +
	"`snapshot` – describe the current scene and generate an image of it (takes a few minutes)\n" +
	"`voice on/off` – turn spoken (mp3) replies on or off\n" +
	"`text on/off` – show or hide the reply text on screen (voice and buttons keep working)"

// handleTextCommand recognises built-in chat commands (help / status / role /
// speed / clear). Commands are handled locally and are never sent to the LLM.
// The caller persists the conversation when a command returns handled=true.
func (s *Server) handleTextCommand(clientID string, c *db.Conversation, msg string) (bool, string) {
	lower := strings.ToLower(strings.TrimSpace(msg))
	switch {
	case lower == "help":
		return true, helpText
	case lower == "status":
		return true, s.formatStatus(clientID, c)
	case lower == "role":
		return true, "Available roles:\n" + s.formatRoleList() + "\n\nSwitch with:  role [n]"
	case strings.HasPrefix(lower, "role "):
		fields := strings.Fields(msg)
		if len(fields) == 2 {
			if n, err := strconv.Atoi(fields[1]); err == nil {
				roles, lerr := s.loadRoles()
				if lerr != nil {
					return true, "No roles available: " + lerr.Error()
				}
				if r, ok := roleByIndex(roles, n); ok {
					c.Settings.SystemPrompt = r.Prompt
					log.Printf("[command] role switched to %q", r.Name)
					return true, fmt.Sprintf("Role switched to %q.", r.Name)
				}
				return true, fmt.Sprintf("No role number %d. Available roles:\n%s", n, s.formatRoleList())
			}
		}
		return true, "Usage: role [n]. Run \"role\" to list the available roles."
	case lower == "model":
		return true, "Available models:\n" + s.formatModelList() + "\n\nSwitch with:  model [n]"
	case strings.HasPrefix(lower, "model "):
		fields := strings.Fields(msg)
		if len(fields) == 2 {
			if n, err := strconv.Atoi(fields[1]); err == nil {
				models, lerr := s.loadModels()
				if lerr != nil {
					return true, "No models available: " + lerr.Error()
				}
				if n >= 1 && n <= len(models) {
					c.Settings.Model = models[n-1]
					log.Printf("[command] model switched to %q", c.Settings.Model)
					return true, fmt.Sprintf("Model switched to %q.", c.Settings.Model)
				}
				return true, fmt.Sprintf("No model number %d. Available models:\n%s", n, s.formatModelList())
			}
		}
		return true, "Usage: model [n]. Run \"model\" to list the available models."
	case lower == "voice":
		return true, formatVoiceList()
	case strings.HasPrefix(lower, "voice "):
		arg := strings.ToLower(strings.TrimSpace(msg[len("voice "):]))
		switch arg {
		case "on", "off", "toggle", "on/off", "on off":
			// Handled by the voice toggle logic below — not a pick command.
			return false, ""
		}
		fields := strings.Fields(msg)
		if len(fields) == 2 {
			if n, err := strconv.Atoi(fields[1]); err == nil {
				if v, ok := voiceOptionByNumber(n); ok {
					settings, serr := s.store.GetAppSettings(clientID)
					if serr != nil {
						return true, "Could not read voice settings: " + serr.Error()
					}
					settings.VoiceName = v.ID
					if err := s.store.SaveAppSettings(clientID, settings); err != nil {
						return true, "Could not save voice: " + err.Error()
					}
					log.Printf("[command] voice set to %q (%s)", v.ID, v.Name)
					return true, fmt.Sprintf("Voice set to %s — %s.", v.Name, v.Detail)
				}
				return true, fmt.Sprintf("No voice number %d. Run \"voice\" for the full list.", n)
			}
		}
		return true, "Usage: voice [n]. Run \"voice\" for the full list."
	case strings.HasPrefix(lower, "speed "):
		fields := strings.Fields(msg)
		if len(fields) == 2 {
			if n, err := strconv.Atoi(fields[1]); err == nil && n >= 1 && n <= 9 {
				settings, serr := s.store.GetAppSettings(clientID)
				if serr != nil {
					return true, "Could not read voice settings: " + serr.Error()
				}
				settings.VoiceSpeed = n
				if err := s.store.SaveAppSettings(clientID, settings); err != nil {
					return true, "Could not save voice speed: " + err.Error()
				}
				log.Printf("[command] voice speed set to %d (%.1fx)", n, 1+float64(n)/10)
				return true, fmt.Sprintf("Voice speed set to %d (%.1fx).", n, 1+float64(n)/10)
			}
		}
		return true, "Usage: speed [n] where n is 1–9 (3 = 1.3x, 5 = 1.5x)."
	case lower == "clear":
		c.Messages = nil
		c.UpdatedAt = time.Now().Unix()
		log.Printf("[command] context cleared")
		return true, "Context cleared — starting fresh."
	}
	return false, ""
}

// formatStatus renders the current session status for the "status" command:
// the voice (working ON/OFF and which voice would speak), the role currently
// in use, and the current voice speed (1–9). New rows (e.g. volume, model,
// provider, turn count, …) can be appended below as the command grows.
func (s *Server) formatStatus(clientID string, c *db.Conversation) string {
	settings, err := s.store.GetAppSettings(clientID)
	if err != nil {
		return "Status unavailable: " + err.Error()
	}

	voiceState := "ON"
	if !settings.Voice {
		voiceState = "OFF (muted)"
	}

	roleName := s.roleNameForConversation(*c)
	if roleName == "" {
		roleName = "Custom (no matching role file)"
	}

	speed := voiceSpeed(settings.VoiceSpeed)
	voiceID := voiceName(settings.VoiceName)

	var b strings.Builder
	b.WriteString("**Status:**\n")
	fmt.Fprintf(&b, "  **Voice:** %s — %s\n", voiceState, voiceFriendlyLabel(voiceID))
	fmt.Fprintf(&b, "  **Role:** %s\n", roleName)
	fmt.Fprintf(&b, "  **Voice speed:** %d (%.1fx)\n", speed, 1+float64(speed)/10)
	fmt.Fprintf(&b, "  **Model:** %s\n", s.modelFor(c))
	return strings.TrimRight(b.String(), "\n")
}

// handleSession returns (creating if needed) the single always-on chat used
// by this browser/client. All provider/model/role values come from the
// back-end defaults.
func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	clientID := clientIDFromRequest(r)
	c, err := s.sessionConversation(clientID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	log.Printf("[session] client=%s conv=%s messages=%d role=%q",
		clientID, c.ID, len(c.Messages), s.roleNameForConversation(c))
	writeJSON(w, http.StatusOK, map[string]any{
		"conversation": c,
		"role":         s.roleNameForConversation(c),
		"model":        s.modelFor(&c),
	})
}

// sessionConversation loads the single chat for a client, creating it with
// back-end defaults when it does not exist yet.
func (s *Server) sessionConversation(clientID string) (db.Conversation, error) {
	list, err := s.store.ListConversations(clientID)
	if err != nil {
		return db.Conversation{}, err
	}
	var c db.Conversation
	if len(list) > 0 {
		c, err = s.store.GetConversation(clientID, list[0].ID)
		if err != nil {
			return db.Conversation{}, err
		}
	} else {
		c = db.Conversation{
			ID:        db.NewID(),
			Title:     "Vivid Mistress",
			Settings:  db.Settings{},
			Messages:  []db.Message{},
			UpdatedAt: time.Now().Unix(),
		}
	}
	changed, err := s.ensureConversationDefaults(&c)
	if err != nil {
		return c, err
	}
	if changed {
		if err := s.store.SaveConversation(clientID, c); err != nil {
			return c, err
		}
	}
	return c, nil
}

// --- Providers ---

// maskAPIKey redacts a stored API key for display. It keeps the last 4
// characters so the user can tell which key is configured.
func maskAPIKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	if len(key) <= 4 {
		return "••••"
	}
	return "••••••••" + key[len(key)-4:]
}

// isMaskedKey reports whether the submitted key is a masked placeholder
// (i.e. the client is re-submitting an existing key unchanged).
func isMaskedKey(key string) bool {
	key = strings.TrimSpace(key)
	return strings.HasPrefix(key, "••")
}

func (s *Server) handleListProviders(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListProviders()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Mask API keys before returning to the client.
	for i := range list {
		list[i].APIKey = maskAPIKey(list[i].APIKey)
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleSaveProvider(w http.ResponseWriter, r *http.Request) {
	var p db.Provider
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&p); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid provider payload: "+err.Error())
		return
	}
	if p.Name == "" || p.BaseURL == "" {
		writeErr(w, http.StatusBadRequest, "name and base_url are required")
		return
	}
	if p.ID == "" {
		p.ID = r.PathValue("id")
	}
	if p.Type == "ollama-online" {
		p.Type = "ollama"
	} else if p.Type == "ollama-local" {
		p.Type = "ollama"
		p.APIKey = ""
	}
	if p.Type != "ollama" && p.Type != "llamacpp" {
		p.Type = "llamacpp"
	}
	// Validate the base URL. Accept bare hostnames (e.g. "Vivid1" or
	// "192.168.1.50:11434") and prepend http://, but reject clearly
	// malformed values.
	base := strings.TrimSpace(p.BaseURL)
	if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		base = "http://" + base
	}
	u, err := url.Parse(base)
	if err != nil || u.Host == "" {
		writeErr(w, http.StatusBadRequest, "base_url must be a valid URL or hostname (e.g. http://192.168.1.50:11434)")
		return
	}
	p.BaseURL = base
	if p.ID == "" {
		p.ID = db.NewID()
	}

	// If the client submitted a masked key (or no key) for an existing
	// provider, preserve the previously stored key.
	if isMaskedKey(p.APIKey) || p.APIKey == "" {
		if existing, err := s.store.GetProvider(p.ID); err == nil {
			p.APIKey = existing.APIKey
		}
	}

	if err := s.store.SaveProvider(p); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Return the masked key so the client never sees the raw secret.
	p.APIKey = maskAPIKey(p.APIKey)
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleDeleteProvider(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.store.DeleteProvider(id); err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	// Reset any conversations that referenced the deleted provider and
	// clear their model so they don't point at a stale model name.
	convs, err := s.store.ListAllConversations()
	if err == nil {
		for _, cc := range convs {
			if cc.Conversation.Settings.ProviderID == id {
				cc.Conversation.Settings.ProviderID = ""
				cc.Conversation.Settings.Model = ""
				cc.Conversation.UpdatedAt = time.Now().Unix()
				_ = s.store.SaveConversation(cc.ClientID, cc.Conversation)
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// --- System prompts ---

func (s *Server) handleListSystemPrompts(w http.ResponseWriter, r *http.Request) {
	clientID := clientIDFromRequest(r)
	list, err := s.store.ListSystemPrompts(clientID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleSaveSystemPrompt(w http.ResponseWriter, r *http.Request) {
	clientID := clientIDFromRequest(r)
	var p db.SystemPrompt
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&p); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid system prompt payload: "+err.Error())
		return
	}
	if p.Name == "" {
		writeErr(w, http.StatusBadRequest, "name is required")
		return
	}
	if p.Scope == "" {
		p.Scope = "local"
	}
	if p.Scope != "local" && p.Scope != "global" {
		writeErr(w, http.StatusBadRequest, "scope must be local or global")
		return
	}
	if p.ID == "" {
		p.ID = db.NewID()
	}
	if err := s.store.SaveSystemPrompt(clientID, p); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleDeleteSystemPrompt(w http.ResponseWriter, r *http.Request) {
	clientID := clientIDFromRequest(r)
	id := r.PathValue("id")
	if err := s.store.DeleteSystemPrompt(clientID, id); err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	// If the deleted prompt was the default, clear the default.
	settings, err := s.store.GetAppSettings(clientID)
	if err == nil && settings.DefaultSystemPromptID == id {
		settings.DefaultSystemPromptID = ""
		_ = s.store.SaveAppSettings(clientID, settings)
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// --- Settings ---

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	clientID := clientIDFromRequest(r)
	settings, err := s.store.GetAppSettings(clientID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

func (s *Server) handleSaveSettings(w http.ResponseWriter, r *http.Request) {
	clientID := clientIDFromRequest(r)
	var settings db.AppSettings
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&settings); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid settings payload: "+err.Error())
		return
	}
	if err := s.store.SaveAppSettings(clientID, settings); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

// --- Voice ---

// handleGetVoice returns the current global voice setting and speed for this client.
func (s *Server) handleGetVoice(w http.ResponseWriter, r *http.Request) {
	clientID := clientIDFromRequest(r)
	settings, err := s.store.GetAppSettings(clientID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"voice":         settings.Voice,
		"voice_speed":   voiceSpeed(settings.VoiceSpeed),
		"voice_name":    voiceName(settings.VoiceName),
		"voice_display": voiceFriendlyName(voiceName(settings.VoiceName)),
		"voice_label":   voiceFriendlyLabel(voiceName(settings.VoiceName)),
	})
}

// handleSetVoice toggles the voice setting for this client.
// The request body may contain {"voice": true/false} to set a specific value,
// or be empty to toggle the current value. It may also contain
// {"voice_speed": 1-9} to set the speech speed (3 = 1.3x, 5 = 1.5x).
func (s *Server) handleSetVoice(w http.ResponseWriter, r *http.Request) {
	clientID := clientIDFromRequest(r)
	settings, err := s.store.GetAppSettings(clientID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	var req struct {
		Voice      *bool  `json:"voice"`
		VoiceSpeed *int   `json:"voice_speed"`
		VoiceName  string `json:"voice_name"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil && err != io.EOF {
		writeErr(w, http.StatusBadRequest, "invalid voice payload: "+err.Error())
		return
	}

	if req.Voice != nil {
		settings.Voice = *req.Voice
	} else {
		settings.Voice = !settings.Voice
	}

	if req.VoiceSpeed != nil {
		if *req.VoiceSpeed < 1 {
			*req.VoiceSpeed = 1
		}
		if *req.VoiceSpeed > 9 {
			*req.VoiceSpeed = 9
		}
		settings.VoiceSpeed = *req.VoiceSpeed
	}
	if strings.TrimSpace(req.VoiceName) != "" {
		settings.VoiceName = strings.TrimSpace(req.VoiceName)
	}

	if err := s.store.SaveAppSettings(clientID, settings); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"voice":         settings.Voice,
		"voice_speed":   settings.VoiceSpeed,
		"voice_name":    voiceName(settings.VoiceName),
		"voice_display": voiceFriendlyName(voiceName(settings.VoiceName)),
		"voice_label":   voiceFriendlyLabel(voiceName(settings.VoiceName)),
	})
}

func voiceName(name string) string {
	if strings.TrimSpace(name) == "" {
		return voice.DefaultVoice
	}
	return strings.TrimSpace(name)
}

func voiceSpeed(speed int) int {
	if speed < 1 || speed > 9 {
		return 3
	}
	return speed
}

// handleVoiceSpeak generates MP3 speech audio on demand for the provided text.
func (s *Server) handleVoiceSpeak(w http.ResponseWriter, r *http.Request) {
	clientID := clientIDFromRequest(r)
	var req struct {
		Text      string `json:"text"`
		Speed     *int   `json:"speed"`
		VoiceName string `json:"voice_name"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid payload: "+err.Error())
		return
	}
	text := strings.TrimSpace(req.Text)
	if text == "" {
		writeErr(w, http.StatusBadRequest, "text is required")
		return
	}

	settings, _ := s.store.GetAppSettings(clientID)
	speed := 3
	if req.Speed != nil && *req.Speed >= 1 && *req.Speed <= 9 {
		speed = *req.Speed
	} else {
		if settings.VoiceSpeed >= 1 && settings.VoiceSpeed <= 9 {
			speed = settings.VoiceSpeed
		}
	}
	selectedVoice := voiceName(req.VoiceName)
	if req.VoiceName == "" {
		selectedVoice = voiceName(settings.VoiceName)
	}

	audioBytes, err := voice.SpeakToBytesWithVoice(text, selectedVoice, speed, false)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "TTS generation failed: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"audio":      base64.StdEncoding.EncodeToString(audioBytes),
		"audio_mime": "audio/mpeg",
	})
}

// --- Models ---

func (s *Server) handleListModels(w http.ResponseWriter, r *http.Request) {
	providerID := r.URL.Query().Get("provider_id")
	if providerID == "" {
		writeErr(w, http.StatusBadRequest, "provider_id is required")
		return
	}
	p, err := s.store.GetProvider(providerID)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	models, err := s.llm.ListModels(ctx, p)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, models)
}

// --- Conversations ---

func (s *Server) handleListConversations(w http.ResponseWriter, r *http.Request) {
	clientID := clientIDFromRequest(r)
	list, err := s.store.ListConversations(clientID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleClearConversations(w http.ResponseWriter, r *http.Request) {
	clientID := clientIDFromRequest(r)
	if err := s.store.ClearConversations(clientID); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleCreateConversation(w http.ResponseWriter, r *http.Request) {
	clientID := clientIDFromRequest(r)
	var req struct {
		Title    string      `json:"title"`
		Settings db.Settings `json:"settings"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid payload: "+err.Error())
		return
	}
	now := time.Now().Unix()
	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = "New Chat"
	}
	if req.Settings.MaxTurns <= 0 {
		req.Settings.MaxTurns = 12
	}
	c := db.Conversation{
		ID:        db.NewID(),
		Title:     title,
		Settings:  req.Settings,
		Messages:  []db.Message{},
		UpdatedAt: now,
	}
	if err := s.store.SaveConversation(clientID, c); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) handleGetConversation(w http.ResponseWriter, r *http.Request) {
	clientID := clientIDFromRequest(r)
	id := r.PathValue("id")
	c, err := s.store.GetConversation(clientID, id)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) handleUpdateConversation(w http.ResponseWriter, r *http.Request) {
	clientID := clientIDFromRequest(r)
	id := r.PathValue("id")
	var c db.Conversation
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&c); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid payload: "+err.Error())
		return
	}
	if c.ID != "" && c.ID != id {
		writeErr(w, http.StatusBadRequest, "id mismatch")
		return
	}
	c.ID = id
	if c.Settings.MaxTurns <= 0 {
		c.Settings.MaxTurns = 12
	}
	if err := s.store.SaveConversation(clientID, c); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) handleDeleteConversation(w http.ResponseWriter, r *http.Request) {
	clientID := clientIDFromRequest(r)
	id := r.PathValue("id")
	if err := s.store.DeleteConversation(clientID, id); err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// --- Chat ---

// isVoiceToggle reports whether the message is a voice on/off command.
// Accepts: "voice", "voice on", "voice off", "voice on/off", "voice toggle".
func isVoiceToggle(msg string) bool {
	lower := strings.ToLower(strings.TrimSpace(msg))
	switch lower {
	case "voice", "voice on", "voice off", "voice on/off", "voice toggle", "voice on off":
		return true
	}
	return false
}

// voiceToggleValue parses the desired voice state from a toggle message.
// Returns the new state and whether the message explicitly requested on/off.
func voiceToggleValue(msg string, current bool) (bool, bool) {
	lower := strings.ToLower(strings.TrimSpace(msg))
	switch lower {
	case "voice on":
		return true, true
	case "voice off":
		return false, true
	default:
		// "voice" or "voice toggle" — flip the current state.
		return !current, false
	}
}

func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	clientID := clientIDFromRequest(r)
	var req struct {
		ConversationID string `json:"conversation_id"`
		UserMessage    string `json:"user_message"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid payload: "+err.Error())
		return
	}
	if req.ConversationID == "" {
		writeErr(w, http.StatusBadRequest, "conversation_id is required")
		return
	}
	msg := strings.TrimSpace(req.UserMessage)
	if msg == "" {
		writeErr(w, http.StatusBadRequest, "user_message is required")
		return
	}

	c, err := s.store.GetConversation(clientID, req.ConversationID)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}

	log.Printf("[chat] client=%s conv=%s user=%q", clientID, c.ID, msg)

	// Ensure single-chat defaults (provider / model / role) are in place for
	// conversations created before config moved into the back end.
	if changed, _ := s.ensureConversationDefaults(&c); changed {
		if err := s.store.SaveConversation(clientID, c); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	// Built-in text commands (help / status / role / speed / clear) are handled
	// locally and are never sent to the LLM.
	if handled, notice := s.handleTextCommand(clientID, &c, msg); handled {
		if err := s.store.SaveConversation(clientID, c); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		log.Printf("[chat] command handled: %q", msg)
		settings, _ := s.store.GetAppSettings(clientID)
		writeJSON(w, http.StatusOK, map[string]any{
			"conversation":  c,
			"notice":        notice,
			"auto":          false,
			"role":          s.roleNameForConversation(c),
			"voice":         settings.Voice,
			"voice_name":    voiceName(settings.VoiceName),
			"voice_display": voiceFriendlyName(voiceName(settings.VoiceName)),
			"voice_label":   voiceFriendlyLabel(voiceName(settings.VoiceName)),
			"voice_speed":   voiceSpeed(settings.VoiceSpeed),
		})
		return
	}

	// The `snapshot` command is a two-stage pipeline: the LLM describes the
	// current scene, then llama-swap renders it as an image. It never reaches
	// the normal LLM chat path.
	if strings.EqualFold(msg, "snapshot") {
		s.handleSnapshot(w, r, clientID, &c)
		return
	}

	// Handle voice toggle commands before sending to the LLM.
	if isVoiceToggle(msg) {
		settings, err := s.store.GetAppSettings(clientID)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		newState, _ := voiceToggleValue(msg, settings.Voice)
		settings.Voice = newState
		if err := s.store.SaveAppSettings(clientID, settings); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}

		// Append the user message and a system-style assistant reply.
		now := time.Now().Unix()
		c.Messages = append(c.Messages, db.Message{Role: "user", Content: msg, Timestamp: now})
		stateText := "ON"
		if !newState {
			stateText = "OFF"
		}
		reply := "🔊 Voice is now " + stateText + "."
		c.Messages = append(c.Messages, db.Message{Role: "assistant", Content: reply, Timestamp: time.Now().Unix()})
		c.UpdatedAt = time.Now().Unix()
		if err := s.store.SaveConversation(clientID, c); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		log.Printf("[chat] voice toggled to %v", newState)
		writeJSON(w, http.StatusOK, map[string]any{
			"conversation":  c,
			"auto":          false,
			"voice":         newState,
			"role":          s.roleNameForConversation(c),
			"voice_name":    voiceName(settings.VoiceName),
			"voice_display": voiceFriendlyName(voiceName(settings.VoiceName)),
			"voice_label":   voiceFriendlyLabel(voiceName(settings.VoiceName)),
			"voice_speed":   voiceSpeed(settings.VoiceSpeed),
		})
		return
	}

	// Resolve provider (back-end config; there is no UI for this any more).
	p, err := s.providerFor(&c)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	// Append the user message.
	now := time.Now().Unix()
	c.Messages = append(c.Messages, db.Message{Role: "user", Content: msg, Timestamp: now})

	// Auto-title on first user message.
	if len(c.Messages) == 1 {
		c.Title = summarizeTitle(msg)
	}

	// Story mode keeps the narrative and the latest prompt while omitting
	// prior user messages from the upstream context.
	history := c.Messages
	if c.Settings.Mode == "story" {
		history = storyContext(c.Messages)
	}

	// Call upstream with context sliding.
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	model := s.modelFor(&c)
	log.Printf("[chat] → sending to LLM: model=%q provider=%q role=%q history=%d messages", model, p.Name, s.roleNameForConversation(c), len(history))
	reply, toolCalls, err := s.llm.ChatWithTools(ctx, p, model, c.Settings.SystemPrompt, history, c.Settings.MaxTurns, []llm.Tool{ChastityTool}, s.chastityExecutor)
	if err != nil {
		// Persist the user message even on failure so it isn't lost.
		c.UpdatedAt = now
		_ = s.store.SaveConversation(clientID, c)
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}

	log.Printf("[chat] ← LLM reply: %d chars: %.240s", len(reply), reply)

	// Save assistant reply.
	c.Messages = append(c.Messages, db.Message{Role: "assistant", Content: reply, Timestamp: time.Now().Unix()})
	c.UpdatedAt = time.Now().Unix()
	if err := s.store.SaveConversation(clientID, c); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	// If voice is enabled, generate audio for the reply and return it in the
	// same response so the frontend can play it immediately.
	resp := map[string]any{
		"conversation": c,
		"auto":         true,
		"role":         s.roleNameForConversation(c),
		"tool_calls":   toolCalls,
		"chastity":     s.chastityStatus(),
	}
	settings, serr := s.store.GetAppSettings(clientID)
	if serr != nil || !settings.Voice {
		log.Printf("[chat] voice disabled — no mp3 for this reply")
	} else {
		speed := voiceSpeed(settings.VoiceSpeed)
		audioBytes, aerr := voice.SpeakToBytesWithVoice(reply, voiceName(settings.VoiceName), speed, false)
		if aerr != nil {
			log.Printf("[voice] TTS generation failed: %v", aerr)
		} else if len(audioBytes) > 0 {
			log.Printf("[chat] mp3 generated: %d bytes", len(audioBytes))
			resp["audio"] = base64.StdEncoding.EncodeToString(audioBytes)
			resp["audio_mime"] = "audio/mpeg"
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func storyContext(messages []db.Message) []db.Message {
	context := make([]db.Message, 0, len(messages))
	lastUser := -1
	for i, message := range messages {
		if message.Role == "user" {
			lastUser = i
		}
	}
	for i, message := range messages {
		if message.Role == "assistant" || i == lastUser {
			context = append(context, message)
		}
	}
	return context
}

// handleRegenerate removes the last assistant message from a conversation
// and generates a fresh response using the same conversation history.
func (s *Server) handleRegenerate(w http.ResponseWriter, r *http.Request) {
	clientID := clientIDFromRequest(r)
	var req struct {
		ConversationID string `json:"conversation_id"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid payload: "+err.Error())
		return
	}
	if req.ConversationID == "" {
		writeErr(w, http.StatusBadRequest, "conversation_id is required")
		return
	}

	c, err := s.store.GetConversation(clientID, req.ConversationID)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}

	// Find the last assistant message and the last user message before it.
	lastUserIdx := -1
	lastAssistantIdx := -1
	for i := len(c.Messages) - 1; i >= 0; i-- {
		if c.Messages[i].Role == "assistant" && lastAssistantIdx == -1 {
			lastAssistantIdx = i
		}
		if c.Messages[i].Role == "user" && lastUserIdx == -1 {
			lastUserIdx = i
		}
		if lastUserIdx != -1 && lastAssistantIdx != -1 {
			break
		}
	}

	if lastAssistantIdx == -1 || lastUserIdx == -1 || lastUserIdx >= lastAssistantIdx {
		writeErr(w, http.StatusBadRequest, "no assistant message to regenerate")
		return
	}

	// Remove the last assistant message and any messages after the last user
	// message so the LLM regenerates from a clean state.
	c.Messages = c.Messages[:lastUserIdx+1]

	// Resolve provider (back-end config; there is no UI for this any more).
	p, err := s.providerFor(&c)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	// Re-generate the response.
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	history := c.Messages
	if c.Settings.Mode == "story" {
		history = storyContext(c.Messages)
	}
	model := s.modelFor(&c)
	log.Printf("[regenerate] → sending to LLM: model=%q provider=%q role=%q history=%d messages", model, p.Name, s.roleNameForConversation(c), len(history))
	reply, toolCalls, err := s.llm.ChatWithTools(ctx, p, model, c.Settings.SystemPrompt, history, c.Settings.MaxTurns, []llm.Tool{ChastityTool}, s.chastityExecutor)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}

	log.Printf("[regenerate] ← LLM reply: %d chars: %.240s", len(reply), reply)

	// Save the new assistant reply.
	c.Messages = append(c.Messages, db.Message{Role: "assistant", Content: reply, Timestamp: time.Now().Unix()})
	c.UpdatedAt = time.Now().Unix()
	if err := s.store.SaveConversation(clientID, c); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	// If voice is enabled, generate audio for the reply.
	resp := map[string]any{
		"conversation": c,
		"auto":         true,
		"role":         s.roleNameForConversation(c),
		"tool_calls":   toolCalls,
		"chastity":     s.chastityStatus(),
	}
	settings, serr := s.store.GetAppSettings(clientID)
	if serr != nil || !settings.Voice {
		log.Printf("[regenerate] voice disabled — no mp3 for this reply")
	} else {
		speed := voiceSpeed(settings.VoiceSpeed)
		audioBytes, aerr := voice.SpeakToBytesWithVoice(reply, voiceName(settings.VoiceName), speed, false)
		if aerr != nil {
			log.Printf("[voice] TTS generation failed: %v", aerr)
		} else if len(audioBytes) > 0 {
			log.Printf("[regenerate] mp3 generated: %d bytes", len(audioBytes))
			resp["audio"] = base64.StdEncoding.EncodeToString(audioBytes)
			resp["audio_mime"] = "audio/mpeg"
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleSuggestions generates 5 response suggestions based on the last assistant message.
func (s *Server) handleSuggestions(w http.ResponseWriter, r *http.Request) {
	clientID := clientIDFromRequest(r)

	var req struct {
		ConversationID string `json:"conversation_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.ConversationID == "" {
		writeErr(w, http.StatusBadRequest, "conversation_id is required")
		return
	}

	c, err := s.store.GetConversation(clientID, req.ConversationID)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}

	// Find the last assistant message.
	lastAssistantMsg := ""
	for i := len(c.Messages) - 1; i >= 0; i-- {
		if c.Messages[i].Role == "assistant" {
			lastAssistantMsg = c.Messages[i].Content
			break
		}
	}

	if lastAssistantMsg == "" {
		writeErr(w, http.StatusBadRequest, "no assistant message found")
		return
	}

	// Resolve provider (back-end config; there is no UI for this any more).
	p, err := s.providerFor(&c)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	// Generate suggestions.
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()

	history := c.Messages
	if c.Settings.Mode == "story" {
		history = storyContext(c.Messages)
	}

	model := s.modelFor(&c)
	log.Printf("[suggestions] Generating buttons — conv=%s model=%q role=%q last_assistant=%d chars", req.ConversationID, model, s.roleNameForConversation(c), len(lastAssistantMsg))
	suggestions, err := s.llm.GenerateSuggestions(ctx, p, model, c.Settings.SystemPrompt, history, lastAssistantMsg)
	if err != nil {
		log.Printf("[suggestions] LLM call failed: %v", err)
		// Return fallback suggestions on error
		writeJSON(w, http.StatusOK, map[string]any{
			"suggestions": []string{"Tell me more", "I agree", "What do you mean?", "Continue", "I'm not sure"},
		})
		return
	}

	log.Printf("[suggestions] generated %d buttons: %q", len(suggestions), suggestions)

	writeJSON(w, http.StatusOK, map[string]any{
		"suggestions": suggestions,
	})
}

// summarizeTitle derives a short title from the first user message.
func summarizeTitle(msg string) string {
	msg = strings.TrimSpace(msg)
	// Collapse whitespace.
	fields := strings.Fields(msg)
	msg = strings.Join(fields, " ")
	if len(msg) > 40 {
		msg = msg[:40] + "…"
	}
	if msg == "" {
		return "New Chat"
	}
	return msg
}

// logMiddleware logs each request.
func logMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s (%s)", r.Method, r.URL.Path, time.Since(start))
	})
}

// Run starts the HTTP server on addr with the default back-end config.
func Run(addr string, store db.Store, llmClient *llm.Client) error {
	return RunWithConfig(addr, store, llmClient, DefaultConfig())
}

// RunWithConfig starts the HTTP server on addr with the given back-end config.
func RunWithConfig(addr string, store db.Store, llmClient *llm.Client, cfg Config) error {
	s := NewWithConfig(store, llmClient, cfg)
	return http.ListenAndServe(addr, logMiddleware(s.Handler()))
}
