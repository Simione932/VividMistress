package db

import "time"

// Provider represents a configured LLM backend endpoint.
type Provider struct {
	ID      string `json:"id"`
	Name    string `json:"name"`     // e.g., "Main Rig - Ollama"
	BaseURL string `json:"base_url"` // e.g., "http://192.168.1.50:11434"
	Type    string `json:"type"`     // "ollama" or "llamacpp"
	APIKey  string `json:"api_key"`  // optional, for online/cloud providers
}

// SystemPrompt is a named, reusable system prompt.
// Scope is either "local" (per browser/client) or "global" (shared for all users).
type SystemPrompt struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Content string `json:"content"`
	Scope   string `json:"scope,omitempty"`
}

// Settings holds per-conversation configuration.
type Settings struct {
	ProviderID   string `json:"provider_id"`
	Model        string `json:"model"`
	SystemPrompt string `json:"system_prompt"`
	MaxTurns     int    `json:"max_turns"` // e.g., 6, 12, 20
	Mode         string `json:"mode"`      // "chat" sends both roles; "story" sends assistant replies + current user message
	Voice        bool   `json:"voice"`     // TTS on/off
}

// AppSettings holds application configuration for a single client
// (browser) — each browser gets its own settings via its client ID.
type AppSettings struct {
	DefaultSystemPromptID string `json:"default_system_prompt_id"`
	ReturnToSend          bool   `json:"return_to_send"`
	Voice                 bool   `json:"voice"`       // TTS on/off
	VoiceSpeed            int    `json:"voice_speed"` // 1-10, 1 is normal speed
	VoiceName             string `json:"voice_name"`  // Edge TTS voice ID
}

// Message is a single chat turn.
type Message struct {
	Role      string `json:"role"` // "user" or "assistant"
	Content   string `json:"content"`
	Timestamp int64  `json:"timestamp"`
}

// Conversation is a full chat thread.
type Conversation struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Settings  Settings  `json:"settings"`
	Messages  []Message `json:"messages"`
	UpdatedAt int64     `json:"updated_at"`
}

// ConversationSummary is a lightweight listing entry for the sidebar.
type ConversationSummary struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	UpdatedAt int64  `json:"updated_at"`
}

// ClientConversation pairs a conversation with the client (browser) that
// owns it. Used internally for cross-client maintenance (e.g., deleting a
// provider that may be referenced by conversations in any client's thread).
type ClientConversation struct {
	ClientID     string
	Conversation Conversation
}

// Store is the persistence interface.
//
// Providers are shared across all clients/browsers (they represent physical
// LLM backends on the network). Everything else — system prompts, app
// settings, and conversations — is scoped to a client ID so each browser
// gets its own isolated data.
type Store interface {
	// Providers (shared across all clients)
	ListProviders() ([]Provider, error)
	GetProvider(id string) (Provider, error)
	SaveProvider(p Provider) error
	DeleteProvider(id string) error

	// System prompts (scoped to a client/browser)
	ListSystemPrompts(clientID string) ([]SystemPrompt, error)
	GetSystemPrompt(clientID, id string) (SystemPrompt, error)
	SaveSystemPrompt(clientID string, p SystemPrompt) error
	DeleteSystemPrompt(clientID, id string) error

	// App settings (scoped to a client/browser)
	GetAppSettings(clientID string) (AppSettings, error)
	SaveAppSettings(clientID string, s AppSettings) error

	// Conversations (scoped to a client/browser)
	ListConversations(clientID string) ([]ConversationSummary, error)
	GetConversation(clientID, id string) (Conversation, error)
	SaveConversation(clientID string, c Conversation) error
	DeleteConversation(clientID, id string) error
	ClearConversations(clientID string) error

	// ListAllConversations returns every conversation across all clients
	// for cross-client maintenance (e.g., provider deletion).
	ListAllConversations() ([]ClientConversation, error)
}

// NewID generates a simple unique ID.
func NewID() string {
	return time.Now().UTC().Format("20060102T150405.000000000Z")
}
