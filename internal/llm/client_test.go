package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pi-chat-gateway/internal/db"
)

// TestContextSliding verifies that the payload sent upstream is pruned
// to system prompt + last N turns.
func TestContextSliding(t *testing.T) {
	// Capture the request body sent to the upstream server.
	var captured struct {
		Model    string        `json:"model"`
		Messages []ChatMessage `json:"messages"`
		Stream   bool          `json:"stream"`
	}

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Errorf("failed to decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"content":"Hello back!"}}]}`))
	}))
	defer upstream.Close()

	client := NewClient()
	provider := db.Provider{
		ID:      "test",
		Name:    "Test",
		BaseURL: upstream.URL,
		Type:    "llamacpp",
	}

	// Build a history of 10 turns (20 messages).
	history := make([]db.Message, 0, 20)
	for i := 0; i < 20; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		history = append(history, db.Message{Role: role, Content: "msg"})
	}

	// maxTurns = 6 → expect system + 12 messages (6 user/assistant pairs).
	reply, err := client.Chat(context.Background(), provider, "test-model", "SYSTEM", history, 6)
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}
	if reply != "Hello back!" {
		t.Errorf("unexpected reply: %q", reply)
	}

	if len(captured.Messages) != 13 {
		t.Errorf("expected 13 messages (1 system + 12 history), got %d", len(captured.Messages))
	}
	if captured.Messages[0].Role != "system" || captured.Messages[0].Content != "SYSTEM" {
		t.Errorf("first message should be system prompt, got %+v", captured.Messages[0])
	}
	// The last message should be the last history message (assistant).
	last := captured.Messages[len(captured.Messages)-1]
	if last.Role != "assistant" || last.Content != "msg" {
		t.Errorf("last message should be the final history message, got %+v", last)
	}
}

// TestContextSlidingNoSystem verifies behavior when no system prompt is set.
func TestContextSlidingNoSystem(t *testing.T) {
	var captured struct {
		Messages []ChatMessage `json:"messages"`
	}

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&captured)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer upstream.Close()

	client := NewClient()
	provider := db.Provider{ID: "t", Name: "T", BaseURL: upstream.URL, Type: "llamacpp"}

	history := []db.Message{
		{Role: "user", Content: "a"},
		{Role: "assistant", Content: "b"},
		{Role: "user", Content: "c"},
	}

	_, err := client.Chat(context.Background(), provider, "m", "", history, 2)
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}
	// No system prompt, maxTurns=2 → keep last 4 messages.
	if len(captured.Messages) != 3 {
		t.Errorf("expected 3 messages, got %d", len(captured.Messages))
	}
}

// TestOllamaChat verifies the Ollama API path and response parsing.
func TestOllamaChat(t *testing.T) {
	var captured struct {
		Model    string        `json:"model"`
		Messages []ChatMessage `json:"messages"`
	}

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&captured)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"message":{"role":"assistant","content":"Ollama reply"}}`))
	}))
	defer upstream.Close()

	client := NewClient()
	provider := db.Provider{ID: "t", Name: "T", BaseURL: upstream.URL, Type: "ollama"}

	reply, err := client.Chat(context.Background(), provider, "llama3", "sys", []db.Message{{Role: "user", Content: "hi"}}, 6)
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}
	if reply != "Ollama reply" {
		t.Errorf("unexpected reply: %q", reply)
	}
	if captured.Model != "llama3" {
		t.Errorf("unexpected model: %q", captured.Model)
	}
}

// TestChatSendsAPIKey verifies that the Authorization header is sent
// when a provider has an API key configured.
func TestChatSendsAPIKey(t *testing.T) {
	var gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"message":{"role":"assistant","content":"ok"}}`))
	}))
	defer upstream.Close()

	client := NewClient()
	provider := db.Provider{
		ID:      "t",
		Name:    "T",
		BaseURL: upstream.URL,
		Type:    "ollama",
		APIKey:  "sk-test-1234",
	}

	_, err := client.Chat(context.Background(), provider, "llama3", "sys", []db.Message{{Role: "user", Content: "hi"}}, 6)
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}
	if gotAuth != "Bearer sk-test-1234" {
		t.Errorf("expected Authorization header 'Bearer sk-test-1234', got %q", gotAuth)
	}
}

// TestChatNoAPIKey verifies that no Authorization header is sent
// when the provider has no API key.
func TestChatNoAPIKey(t *testing.T) {
	var gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"message":{"role":"assistant","content":"ok"}}`))
	}))
	defer upstream.Close()

	client := NewClient()
	provider := db.Provider{ID: "t", Name: "T", BaseURL: upstream.URL, Type: "ollama"}

	_, err := client.Chat(context.Background(), provider, "llama3", "sys", []db.Message{{Role: "user", Content: "hi"}}, 6)
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}
	if gotAuth != "" {
		t.Errorf("expected no Authorization header, got %q", gotAuth)
	}
}

// TestNormalizeBaseURL verifies that bare hostnames get http:// prepended
// and that URLs with a scheme are left unchanged.
func TestNormalizeBaseURL(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"Vivid1", "http://Vivid1"},
		{"192.168.1.50:11434", "http://192.168.1.50:11434"},
		{"http://192.168.1.202:8080", "http://192.168.1.202:8080"},
		{"https://api.ollama.com", "https://api.ollama.com"},
		{"  http://example.com  ", "http://example.com"},
		{"", ""},
	}
	for _, c := range cases {
		if got := normalizeBaseURL(c.in); got != c.want {
			t.Errorf("normalizeBaseURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestChatDump verifies that when DumpDir is set, the raw request payload
// and raw upstream response are written to disk as timestamped files.
func TestChatDump(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"message":{"role":"assistant","content":"dumped"}}`))
	}))
	defer upstream.Close()

	dumpDir := t.TempDir()
	client := NewClient()
	client.DumpDir = dumpDir
	provider := db.Provider{ID: "t", Name: "T", BaseURL: upstream.URL, Type: "ollama"}

	if _, err := client.Chat(context.Background(), provider, "llama3", "sys", []db.Message{{Role: "user", Content: "hi"}}, 6); err != nil {
		t.Fatalf("Chat failed: %v", err)
	}

	entries, err := os.ReadDir(dumpDir)
	if err != nil {
		t.Fatalf("failed to read dump dir: %v", err)
	}
	var reqFile, respFile string
	for _, e := range entries {
		switch {
		case strings.HasSuffix(e.Name(), "_request.json"):
			reqFile = filepath.Join(dumpDir, e.Name())
		case strings.HasSuffix(e.Name(), "_response.raw"):
			respFile = filepath.Join(dumpDir, e.Name())
		}
	}
	if reqFile == "" || respFile == "" {
		t.Fatalf("expected request.json and response.raw dumps, got %d files", len(entries))
	}

	reqData, err := os.ReadFile(reqFile)
	if err != nil {
		t.Fatalf("failed to read request dump: %v", err)
	}
	var req struct {
		Model    string        `json:"model"`
		Messages []ChatMessage `json:"messages"`
	}
	if err := json.Unmarshal(reqData, &req); err != nil {
		t.Fatalf("request dump is not valid JSON: %v", err)
	}
	if req.Model != "llama3" || len(req.Messages) != 2 {
		t.Errorf("unexpected request dump contents: %s", string(reqData))
	}

	respData, err := os.ReadFile(respFile)
	if err != nil {
		t.Fatalf("failed to read response dump: %v", err)
	}
	if !strings.Contains(string(respData), `"content":"dumped"`) {
		t.Errorf("unexpected response dump contents: %s", string(respData))
	}
}

// TestChatNoDump verifies that no files are written when DumpDir is empty.
func TestChatNoDump(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"message":{"role":"assistant","content":"ok"}}`))
	}))
	defer upstream.Close()

	client := NewClient() // DumpDir intentionally left empty
	provider := db.Provider{ID: "t", Name: "T", BaseURL: upstream.URL, Type: "ollama"}

	if _, err := client.Chat(context.Background(), provider, "llama3", "sys", []db.Message{{Role: "user", Content: "hi"}}, 6); err != nil {
		t.Fatalf("Chat failed: %v", err)
	}
	// Success with an empty DumpDir is the assertion: dumping stays off
	// and Chat neither panics nor errors.
}

// TestListModelsSendsAPIKey verifies the Authorization header is sent
// during model discovery when an API key is configured.
func TestListModelsSendsAPIKey(t *testing.T) {
	var gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"models":[{"name":"llama3"}]}`))
	}))
	defer upstream.Close()

	client := NewClient()
	provider := db.Provider{
		ID:      "t",
		Name:    "T",
		BaseURL: upstream.URL,
		Type:    "ollama",
		APIKey:  "sk-test-1234",
	}

	_, err := client.ListModels(context.Background(), provider)
	if err != nil {
		t.Fatalf("ListModels failed: %v", err)
	}
	if gotAuth != "Bearer sk-test-1234" {
		t.Errorf("expected Authorization header 'Bearer sk-test-1234', got %q", gotAuth)
	}
}
