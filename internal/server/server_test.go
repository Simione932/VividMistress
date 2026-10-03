package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pi-chat-gateway/internal/db"
)

// fakeStore is a minimal in-memory Store for testing the server handlers.
type fakeStore struct {
	providers     map[string]db.Provider
	settings      map[string]db.AppSettings
	prompts       map[string]map[string]db.SystemPrompt
	globalPrompts map[string]db.SystemPrompt
	convs         map[string]map[string]db.Conversation
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		providers:     make(map[string]db.Provider),
		settings:      make(map[string]db.AppSettings),
		prompts:       make(map[string]map[string]db.SystemPrompt),
		globalPrompts: make(map[string]db.SystemPrompt),
		convs:         make(map[string]map[string]db.Conversation),
	}
}

func TestStoryContextKeepsAssistantRepliesAndLatestUserMessage(t *testing.T) {
	messages := []db.Message{
		{Role: "user", Content: "old prompt"},
		{Role: "assistant", Content: "first scene"},
		{Role: "user", Content: "old direction"},
		{Role: "assistant", Content: "second scene"},
		{Role: "user", Content: "continue"},
	}

	got := storyContext(messages)
	if len(got) != 3 {
		t.Fatalf("expected 3 story context messages, got %d", len(got))
	}
	expected := []string{"first scene", "second scene", "continue"}
	for i, message := range got {
		if message.Content != expected[i] {
			t.Errorf("message %d: expected %q, got %q", i, expected[i], message.Content)
		}
	}
}

func TestSystemPromptsMergeGlobalAndLocal(t *testing.T) {
	store := newFakeStore()
	store.SaveSystemPrompt("global", db.SystemPrompt{ID: "g1", Name: "Global prompt", Content: "global content", Scope: "global"})
	store.SaveSystemPrompt("client_a", db.SystemPrompt{ID: "l1", Name: "Local prompt", Content: "local content"})
	store.SaveSystemPrompt("client_a", db.SystemPrompt{ID: "g1", Name: "Local override", Content: "override content"})

	prompts, err := store.ListSystemPrompts("client_a")
	if err != nil {
		t.Fatalf("expected list to succeed, got %v", err)
	}
	if len(prompts) != 2 {
		t.Fatalf("expected 2 merged prompts, got %d: %+v", len(prompts), prompts)
	}

	foundLocal := false
	for _, p := range prompts {
		if p.ID == "g1" && p.Content == "override content" {
			foundLocal = true
		}
	}
	if !foundLocal {
		t.Fatalf("expected local prompt to override the global prompt with same id, got %+v", prompts)
	}
}

func (f *fakeStore) ListProviders() ([]db.Provider, error) {
	list := make([]db.Provider, 0, len(f.providers))
	for _, p := range f.providers {
		list = append(list, p)
	}
	return list, nil
}

func (f *fakeStore) GetProvider(id string) (db.Provider, error) {
	p, ok := f.providers[id]
	if !ok {
		return db.Provider{}, http.ErrNoLocation
	}
	return p, nil
}

func (f *fakeStore) SaveProvider(p db.Provider) error {
	f.providers[p.ID] = p
	return nil
}

func (f *fakeStore) DeleteProvider(id string) error {
	delete(f.providers, id)
	return nil
}

func (f *fakeStore) ListSystemPrompts(clientID string) ([]db.SystemPrompt, error) {
	list := make([]db.SystemPrompt, 0)
	merged := make(map[string]db.SystemPrompt)
	for _, p := range f.globalPrompts {
		merged[p.ID] = p
	}
	if m, ok := f.prompts[clientID]; ok {
		for _, p := range m {
			merged[p.ID] = p
		}
	}
	for _, p := range merged {
		list = append(list, p)
	}
	return list, nil
}

func (f *fakeStore) GetSystemPrompt(clientID, id string) (db.SystemPrompt, error) {
	if p, ok := f.globalPrompts[id]; ok {
		return p, nil
	}
	if m, ok := f.prompts[clientID]; ok {
		if p, ok := m[id]; ok {
			return p, nil
		}
	}
	return db.SystemPrompt{}, http.ErrNoLocation
}

func (f *fakeStore) SaveSystemPrompt(clientID string, p db.SystemPrompt) error {
	if p.Scope == "global" {
		f.globalPrompts[p.ID] = p
		return nil
	}
	if f.prompts[clientID] == nil {
		f.prompts[clientID] = make(map[string]db.SystemPrompt)
	}
	f.prompts[clientID][p.ID] = p
	return nil
}

func (f *fakeStore) DeleteSystemPrompt(clientID, id string) error {
	if m, ok := f.prompts[clientID]; ok {
		delete(m, id)
	}
	return nil
}

func (f *fakeStore) GetAppSettings(clientID string) (db.AppSettings, error) {
	return f.settings[clientID], nil
}

func (f *fakeStore) SaveAppSettings(clientID string, s db.AppSettings) error {
	f.settings[clientID] = s
	return nil
}

func (f *fakeStore) ListConversations(clientID string) ([]db.ConversationSummary, error) {
	sums := make([]db.ConversationSummary, 0)
	if m, ok := f.convs[clientID]; ok {
		for _, c := range m {
			sums = append(sums, db.ConversationSummary{ID: c.ID, Title: c.Title, UpdatedAt: c.UpdatedAt})
		}
	}
	return sums, nil
}

func (f *fakeStore) GetConversation(clientID, id string) (db.Conversation, error) {
	if m, ok := f.convs[clientID]; ok {
		if c, ok := m[id]; ok {
			return c, nil
		}
	}
	return db.Conversation{}, http.ErrNoLocation
}

func (f *fakeStore) SaveConversation(clientID string, c db.Conversation) error {
	if f.convs[clientID] == nil {
		f.convs[clientID] = make(map[string]db.Conversation)
	}
	f.convs[clientID][c.ID] = c
	return nil
}

func (f *fakeStore) DeleteConversation(clientID, id string) error {
	if m, ok := f.convs[clientID]; ok {
		delete(m, id)
	}
	return nil
}

func (f *fakeStore) ClearConversations(clientID string) error {
	delete(f.convs, clientID)
	return nil
}

func (f *fakeStore) ListAllConversations() ([]db.ClientConversation, error) {
	out := []db.ClientConversation{}
	for clientID, m := range f.convs {
		for _, c := range m {
			out = append(out, db.ClientConversation{ClientID: clientID, Conversation: c})
		}
	}
	return out, nil
}

// TestVoiceToggle verifies the voice toggle API endpoints.
func TestVoiceToggle(t *testing.T) {
	store := newFakeStore()
	s := New(store, nil)

	// 1. GET /api/voice → default is off.
	req := httptest.NewRequest(http.MethodGet, "/api/voice", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["voice"] != false {
		t.Errorf("expected voice to be off by default")
	}

	// 2. PUT /api/voice with {"voice": true} → turns on.
	body, _ := json.Marshal(map[string]bool{"voice": true})
	req = httptest.NewRequest(http.MethodPut, "/api/voice", bytes.NewReader(body))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["voice"] != true {
		t.Errorf("expected voice to be on")
	}
	if !store.settings["default"].Voice {
		t.Errorf("expected store voice setting to be true")
	}

	// 3. PUT /api/voice with {"voice": false} → turns off.
	body, _ = json.Marshal(map[string]bool{"voice": false})
	req = httptest.NewRequest(http.MethodPut, "/api/voice", bytes.NewReader(body))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["voice"] != false {
		t.Errorf("expected voice to be off")
	}
	if store.settings["default"].Voice {
		t.Errorf("expected store voice setting to be false")
	}
}

// TestVoiceSpeed verifies the voice speed setting API.
func TestVoiceSpeed(t *testing.T) {
	store := newFakeStore()
	s := New(store, nil)

	// 1. GET /api/voice → default speed should be 3.
	req := httptest.NewRequest(http.MethodGet, "/api/voice", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["voice_speed"] != float64(3) {
		t.Errorf("expected default voice_speed to be 3, got %v", resp["voice_speed"])
	}

	// 2. PUT /api/voice with {"voice_speed": 7} → sets speed.
	body, _ := json.Marshal(map[string]int{"voice_speed": 7})
	req = httptest.NewRequest(http.MethodPut, "/api/voice", bytes.NewReader(body))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["voice_speed"] != float64(7) {
		t.Errorf("expected voice_speed to be 7, got %v", resp["voice_speed"])
	}
	if store.settings["default"].VoiceSpeed != 7 {
		t.Errorf("expected store voice_speed to be 7, got %d", store.settings["default"].VoiceSpeed)
	}

	// 3. PUT /api/voice with {"voice_speed": 15} → clamped to 9.
	body, _ = json.Marshal(map[string]int{"voice_speed": 15})
	req = httptest.NewRequest(http.MethodPut, "/api/voice", bytes.NewReader(body))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["voice_speed"] != float64(9) {
		t.Errorf("expected voice_speed to be clamped to 9, got %v", resp["voice_speed"])
	}

	// 4. PUT /api/voice with {"voice_speed": 0} → clamped to 1.
	body, _ = json.Marshal(map[string]int{"voice_speed": 0})
	req = httptest.NewRequest(http.MethodPut, "/api/voice", bytes.NewReader(body))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["voice_speed"] != float64(1) {
		t.Errorf("expected voice_speed to be clamped to 1, got %v", resp["voice_speed"])
	}
}

// TestVoiceToggleMessage verifies that sending "voice" as a chat message
// toggles the voice setting without calling the LLM.
func TestVoiceToggleMessage(t *testing.T) {
	store := newFakeStore()
	store.providers["p1"] = db.Provider{
		ID:      "p1",
		Name:    "Test",
		BaseURL: "http://localhost:11434",
		Type:    "ollama",
	}
	store.convs["default"] = map[string]db.Conversation{
		"c1": {
			ID:    "c1",
			Title: "Test Chat",
			Settings: db.Settings{
				ProviderID: "p1",
				Model:      "test-model",
				MaxTurns:   12,
			},
			Messages: []db.Message{},
		},
	}
	s := New(store, nil)

	// Send "voice on" as a message → should toggle voice on.
	body, _ := json.Marshal(map[string]string{
		"conversation_id": "c1",
		"user_message":    "voice on",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/chat", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !store.settings["default"].Voice {
		t.Errorf("expected voice to be toggled on")
	}

	// Verify the conversation has the user message and assistant reply.
	c, _ := store.GetConversation("default", "c1")
	if len(c.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(c.Messages))
	}
	if c.Messages[0].Role != "user" || c.Messages[0].Content != "voice on" {
		t.Errorf("expected user message 'voice on', got %q", c.Messages[0].Content)
	}
	if c.Messages[1].Role != "assistant" {
		t.Errorf("expected assistant reply, got role %q", c.Messages[1].Role)
	}

	// Send "voice off" → should turn voice off.
	body, _ = json.Marshal(map[string]string{
		"conversation_id": "c1",
		"user_message":    "voice off",
	})
	req = httptest.NewRequest(http.MethodPost, "/api/chat", bytes.NewReader(body))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if store.settings["default"].Voice {
		t.Errorf("expected voice to be toggled off")
	}

	// Send "voice on" → should turn voice on.
	body, _ = json.Marshal(map[string]string{
		"conversation_id": "c1",
		"user_message":    "voice on",
	})
	req = httptest.NewRequest(http.MethodPost, "/api/chat", bytes.NewReader(body))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !store.settings["default"].Voice {
		t.Errorf("expected voice to be toggled on")
	}
}

// TestProviderAPIKeyMasking verifies that API keys are masked in list
// responses and preserved when a masked key is re-submitted.
func TestProviderAPIKeyMasking(t *testing.T) {
	store := newFakeStore()
	store.providers["p1"] = db.Provider{
		ID:      "p1",
		Name:    "Cloud Ollama",
		BaseURL: "https://ollama.example.com",
		Type:    "ollama",
		APIKey:  "sk-super-secret-1234",
	}
	s := New(store, nil)

	// 1. List providers → API key should be masked.
	req := httptest.NewRequest(http.MethodGet, "/api/providers", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var list []db.Provider
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 provider, got %d", len(list))
	}
	if list[0].APIKey == "sk-super-secret-1234" {
		t.Errorf("API key should be masked in list response")
	}
	if list[0].APIKey == "" {
		t.Errorf("masked API key should not be empty")
	}

	// 2. Re-submit with the masked key → stored key should be preserved.
	body, _ := json.Marshal(map[string]string{
		"id":       "p1",
		"name":     "Cloud Ollama",
		"base_url": "https://ollama.example.com",
		"type":     "ollama",
		"api_key":  list[0].APIKey,
	})
	req = httptest.NewRequest(http.MethodPost, "/api/providers", bytes.NewReader(body))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	stored, _ := store.GetProvider("p1")
	if stored.APIKey != "sk-super-secret-1234" {
		t.Errorf("expected stored key to be preserved, got %q", stored.APIKey)
	}

	// 3. Submit a new key → stored key should be updated.
	body, _ = json.Marshal(map[string]string{
		"id":       "p1",
		"name":     "Cloud Ollama",
		"base_url": "https://ollama.example.com",
		"type":     "ollama",
		"api_key":  "sk-new-key-5678",
	})
	req = httptest.NewRequest(http.MethodPost, "/api/providers", bytes.NewReader(body))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	stored, _ = store.GetProvider("p1")
	if stored.APIKey != "sk-new-key-5678" {
		t.Errorf("expected stored key to be updated, got %q", stored.APIKey)
	}
}

// TestClientIsolation verifies that two different client IDs have completely
// isolated conversations, settings, and system prompts, while sharing providers.
func TestClientIsolation(t *testing.T) {
	store := newFakeStore()
	store.providers["p1"] = db.Provider{
		ID:      "p1",
		Name:    "Shared Provider",
		BaseURL: "http://localhost:11434",
		Type:    "ollama",
	}
	s := New(store, nil)

	// 1. Client A creates a conversation.
	createBody, _ := json.Marshal(map[string]any{
		"title": "Client A Chat",
		"settings": db.Settings{
			ProviderID: "p1",
			Model:      "llama3",
		},
	})
	reqA := httptest.NewRequest(http.MethodPost, "/api/conversations", bytes.NewReader(createBody))
	reqA.Header.Set("X-Client-ID", "client_a")
	recA := httptest.NewRecorder()
	s.Handler().ServeHTTP(recA, reqA)
	if recA.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recA.Code)
	}
	var convA db.Conversation
	if err := json.Unmarshal(recA.Body.Bytes(), &convA); err != nil {
		t.Fatalf("failed to decode convA: %v", err)
	}

	// 2. Client B lists conversations -> should be empty.
	reqB := httptest.NewRequest(http.MethodGet, "/api/conversations", nil)
	reqB.Header.Set("X-Client-ID", "client_b")
	recB := httptest.NewRecorder()
	s.Handler().ServeHTTP(recB, reqB)
	if recB.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recB.Code)
	}
	var convsB []db.ConversationSummary
	if err := json.Unmarshal(recB.Body.Bytes(), &convsB); err != nil {
		t.Fatalf("failed to decode convsB: %v", err)
	}
	if len(convsB) != 0 {
		t.Fatalf("expected client_b to have 0 conversations, got %d", len(convsB))
	}

	// 3. Client B attempts to get Client A's conversation -> should return 404.
	reqBGet := httptest.NewRequest(http.MethodGet, "/api/conversations/"+convA.ID, nil)
	reqBGet.Header.Set("X-Client-ID", "client_b")
	recBGet := httptest.NewRecorder()
	s.Handler().ServeHTTP(recBGet, reqBGet)
	if recBGet.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for client_b accessing client_a's conversation, got %d", recBGet.Code)
	}

	// 4. Client A saves a system prompt.
	promptBody, _ := json.Marshal(map[string]string{
		"name":    "A's prompt",
		"content": "You are assistant for A",
	})
	reqPromptA := httptest.NewRequest(http.MethodPost, "/api/system-prompts", bytes.NewReader(promptBody))
	reqPromptA.Header.Set("X-Client-ID", "client_a")
	recPromptA := httptest.NewRecorder()
	s.Handler().ServeHTTP(recPromptA, reqPromptA)
	if recPromptA.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recPromptA.Code)
	}

	// 5. Client B lists system prompts -> should be empty.
	reqPromptB := httptest.NewRequest(http.MethodGet, "/api/system-prompts", nil)
	reqPromptB.Header.Set("X-Client-ID", "client_b")
	recPromptB := httptest.NewRecorder()
	s.Handler().ServeHTTP(recPromptB, reqPromptB)
	if recPromptB.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recPromptB.Code)
	}
	var promptsB []db.SystemPrompt
	if err := json.Unmarshal(recPromptB.Body.Bytes(), &promptsB); err != nil {
		t.Fatalf("failed to decode promptsB: %v", err)
	}
	if len(promptsB) != 0 {
		t.Fatalf("expected client_b to have 0 system prompts, got %d", len(promptsB))
	}

	// 6. Both clients can list shared providers.
	reqProvB := httptest.NewRequest(http.MethodGet, "/api/providers", nil)
	reqProvB.Header.Set("X-Client-ID", "client_b")
	recProvB := httptest.NewRecorder()
	s.Handler().ServeHTTP(recProvB, reqProvB)
	if recProvB.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recProvB.Code)
	}
	var provsB []db.Provider
	if err := json.Unmarshal(recProvB.Body.Bytes(), &provsB); err != nil {
		t.Fatalf("failed to decode provsB: %v", err)
	}
	if len(provsB) != 1 || provsB[0].ID != "p1" {
		t.Fatalf("expected client_b to see shared provider p1, got %+v", provsB)
	}
}

// TestVoiceSpeakEndpoint verifies the on-demand TTS API endpoint.
func TestVoiceSpeakEndpoint(t *testing.T) {
	store := newFakeStore()
	s := New(store, nil)

	// 1. Missing text -> returns 400.
	body, _ := json.Marshal(map[string]string{"text": ""})
	req := httptest.NewRequest(http.MethodPost, "/api/voice/speak", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty text, got %d", rec.Code)
	}

}

// TestStatusCommand verifies the built-in "status" chat command reports the
// current voice state/name, the role in use and the voice speed setting.
func TestStatusCommand(t *testing.T) {
	roleContent := "You are the default test role."
	roleDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(roleDir, "Default.txt"), []byte(roleContent), 0o644); err != nil {
		t.Fatalf("failed to write role file: %v", err)
	}

	store := newFakeStore()
	store.settings["default"] = db.AppSettings{
		Voice:      true,
		VoiceSpeed: 5,
		VoiceName:  "en-GB-LibbyNeural",
	}
	s := NewWithConfig(store, nil, Config{RolesDir: roleDir, Model: "test-model"})

	c := db.Conversation{
		ID: "c1",
		Settings: db.Settings{
			SystemPrompt: roleContent,
			MaxTurns:     12,
		},
	}

	handled, notice := s.handleTextCommand("default", &c, "status")
	if !handled {
		t.Fatalf("expected status to be handled as a local command")
	}
	for _, want := range []string{
		"**Voice:** ON — Libby — British, Soft/Storytelling",
		"**Role:** Default",
		"**Voice speed:** 5 (1.5x)",
	} {
		if !strings.Contains(notice, want) {
			t.Errorf("expected status notice to contain %q, got:\n%s", want, notice)
		}
	}

	// Voice muted -> shows OFF; unknown speed 0 -> defaulted to 3.
	store.settings["default"] = db.AppSettings{Voice: false}
	handled, notice = s.handleTextCommand("default", &c, "status")
	if !handled {
		t.Fatalf("expected status to be handled as a local command")
	}
	if !strings.Contains(notice, "**Voice:** OFF (muted)") {
		t.Errorf("expected muted voice to show OFF, got:\n%s", notice)
	}
	if !strings.Contains(notice, "**Voice speed:** 3 (1.3x)") {
		t.Errorf("expected voice speed default of 3 when unset, got:\n%s", notice)
	}
}

// TestHelpTextListsStatus verifies the "help" command documents the status
// command and uses the [n] notation (never <number>).
func TestHelpTextListsStatus(t *testing.T) {
	if !strings.Contains(helpText, "status") {
		t.Errorf("expected helpText to document the status command, got:\n%s", helpText)
	}
	for _, want := range []string{"`delay [n]`", "`stop`", "`start`", "`buttons [n]`", "`text on/off`", "`role [n]`", "`voice [n]`", "`speed [n]`"} {
		if !strings.Contains(helpText, want) {
			t.Errorf("expected helpText to contain %q, got:\n%s", want, helpText)
		}
	}
	if strings.Contains(helpText, "<number>") {
		t.Errorf("expected helpText to use [n] instead of <number>, got:\n%s", helpText)
	}
}
