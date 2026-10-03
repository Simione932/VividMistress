package db

import (
	"os"
	"path/filepath"
	"testing"
)

func TestJSONStoreClientIsolation(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "jsonstore_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewJSONStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	// 1. Save provider (shared)
	prov := Provider{
		ID:      "p1",
		Name:    "Test Provider",
		BaseURL: "http://localhost:11434",
		Type:    "ollama",
	}
	if err := store.SaveProvider(prov); err != nil {
		t.Fatalf("failed to save provider: %v", err)
	}

	// 2. Client A creates a conversation
	convA := Conversation{
		ID:        "conv_a1",
		Title:     "Chat A",
		Settings:  Settings{ProviderID: "p1", Model: "llama3", MaxTurns: 12},
		Messages:  []Message{{Role: "user", Content: "Hello from A"}},
		UpdatedAt: 100,
	}
	if err := store.SaveConversation("client_a", convA); err != nil {
		t.Fatalf("failed to save conversation for client_a: %v", err)
	}

	// 3. Client B creates a conversation
	convB := Conversation{
		ID:        "conv_b1",
		Title:     "Chat B",
		Settings:  Settings{ProviderID: "p1", Model: "llama3", MaxTurns: 12},
		Messages:  []Message{{Role: "user", Content: "Hello from B"}},
		UpdatedAt: 200,
	}
	if err := store.SaveConversation("client_b", convB); err != nil {
		t.Fatalf("failed to save conversation for client_b: %v", err)
	}

	// 4. Verify Client A only lists its own conversations
	sumsA, err := store.ListConversations("client_a")
	if err != nil {
		t.Fatalf("failed to list client_a convs: %v", err)
	}
	if len(sumsA) != 1 || sumsA[0].ID != "conv_a1" {
		t.Fatalf("expected client_a to have conv_a1, got %+v", sumsA)
	}

	// 5. Verify Client B only lists its own conversations
	sumsB, err := store.ListConversations("client_b")
	if err != nil {
		t.Fatalf("failed to list client_b convs: %v", err)
	}
	if len(sumsB) != 1 || sumsB[0].ID != "conv_b1" {
		t.Fatalf("expected client_b to have conv_b1, got %+v", sumsB)
	}

	// 6. Verify ListAllConversations returns both
	all, err := store.ListAllConversations()
	if err != nil {
		t.Fatalf("failed to list all convs: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 conversations across all clients, got %d", len(all))
	}

	// 7. Verify Client A clear conversations does not delete Client B's
	if err := store.ClearConversations("client_a"); err != nil {
		t.Fatalf("failed to clear client_a convs: %v", err)
	}
	sumsAAfter, _ := store.ListConversations("client_a")
	if len(sumsAAfter) != 0 {
		t.Fatalf("expected client_a to have 0 conversations, got %d", len(sumsAAfter))
	}
	sumsBAfter, _ := store.ListConversations("client_b")
	if len(sumsBAfter) != 1 {
		t.Fatalf("expected client_b to still have 1 conversation, got %d", len(sumsBAfter))
	}
}

func TestJSONStoreLegacyMigration(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "jsonstore_mig_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create legacy v1 layout:
	// <tmpDir>/conversations/legacy_conv.json
	// <tmpDir>/settings.json
	// <tmpDir>/system_prompts.json
	legacyConvDir := filepath.Join(tmpDir, "conversations")
	if err := os.MkdirAll(legacyConvDir, 0o755); err != nil {
		t.Fatalf("failed to create legacy conv dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(legacyConvDir, "c1.json"), []byte(`{"id":"c1","title":"Legacy Chat","settings":{},"messages":[],"updated_at":10}`), 0o644); err != nil {
		t.Fatalf("failed to write legacy conv: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "settings.json"), []byte(`{"default_system_prompt_id":"p1","voice":true,"voice_speed":4}`), 0o644); err != nil {
		t.Fatalf("failed to write legacy settings: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "system_prompts.json"), []byte(`[{"id":"p1","name":"Legacy Prompt","content":"prompt"}]`), 0o644); err != nil {
		t.Fatalf("failed to write legacy system prompts: %v", err)
	}

	store, err := NewJSONStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	// First client accesses storage -> claims legacy data
	convs, err := store.ListConversations("first_client")
	if err != nil {
		t.Fatalf("failed to list conversations for first_client: %v", err)
	}
	if len(convs) != 1 || convs[0].ID != "c1" {
		t.Fatalf("expected first_client to claim legacy conv c1, got %+v", convs)
	}

	settings, err := store.GetAppSettings("first_client")
	if err != nil {
		t.Fatalf("failed to get app settings for first_client: %v", err)
	}
	if !settings.Voice || settings.VoiceSpeed != 4 {
		t.Fatalf("expected first_client to claim voice settings, got %+v", settings)
	}

	prompts, err := store.ListSystemPrompts("first_client")
	if err != nil {
		t.Fatalf("failed to get system prompts for first_client: %v", err)
	}
	if len(prompts) != 1 || prompts[0].Name != "Legacy Prompt" {
		t.Fatalf("expected first_client to claim legacy prompt, got %+v", prompts)
	}

	// Second client connects -> gets clean/isolated data
	convs2, _ := store.ListConversations("second_client")
	if len(convs2) != 0 {
		t.Fatalf("expected second_client to have 0 conversations, got %d", len(convs2))
	}
}
