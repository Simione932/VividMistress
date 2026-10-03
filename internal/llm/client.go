package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"pi-chat-gateway/internal/db"
)

// Client talks to upstream LLM backends (Ollama or llama.cpp).
type Client struct {
	http    *http.Client
	DumpDir string // optional: when set, raw request/response bodies are written here
}

// NewClient creates an LLM client with a sane timeout.
func NewClient() *Client {
	return &Client{
		http: &http.Client{Timeout: 5 * time.Minute},
	}
}

// ModelInfo is a discovered model name.
type ModelInfo struct {
	Name string `json:"name"`
}

// dump writes data to DumpDir as "<stamp>_<name>" when dumping is enabled.
// Failures are logged but never affect the chat flow.
func (c *Client) dump(stamp, name string, data []byte) {
	if c.DumpDir == "" || len(data) == 0 {
		return
	}
	path := filepath.Join(c.DumpDir, fmt.Sprintf("%s_%s", stamp, name))
	if err := os.WriteFile(path, data, 0o644); err != nil {
		log.Printf("llm: failed to write dump %s: %v", path, err)
		return
	}
	log.Printf("llm: dumped %s", path)
}

// applyAuth sets the Authorization header on req if the provider has an API key.
func applyAuth(req *http.Request, p db.Provider) {
	if strings.TrimSpace(p.APIKey) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(p.APIKey))
	}
}

// normalizeBaseURL ensures the provider base URL has a scheme. If the user
// entered a bare hostname (e.g. "Vivid1" or "192.168.1.50:11434"), we prepend
// "http://" so the request can be constructed.
func normalizeBaseURL(base string) string {
	base = strings.TrimSpace(base)
	if base == "" {
		return base
	}
	if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		return "http://" + base
	}
	return base
}

// ListModels fetches available models from the provider.
func (c *Client) ListModels(ctx context.Context, p db.Provider) ([]ModelInfo, error) {
	base := normalizeBaseURL(p.BaseURL)
	var url string
	switch p.Type {
	case "ollama":
		url = strings.TrimRight(base, "/") + "/api/tags"
	default: // llamacpp
		url = strings.TrimRight(base, "/") + "/v1/models"
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	applyAuth(req, p)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("model discovery failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("model discovery returned %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}

	var models []ModelInfo
	switch p.Type {
	case "ollama":
		var tags struct {
			Models []struct {
				Name string `json:"name"`
			} `json:"models"`
		}
		if err := json.Unmarshal(body, &tags); err != nil {
			return nil, err
		}
		for _, m := range tags.Models {
			models = append(models, ModelInfo{Name: m.Name})
		}
	default:
		var list struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &list); err != nil {
			return nil, err
		}
		for _, m := range list.Data {
			models = append(models, ModelInfo{Name: m.ID})
		}
	}
	sort.Slice(models, func(i, j int) bool {
		return strings.ToLower(models[i].Name) < strings.ToLower(models[j].Name)
	})
	return models, nil
}

// ChatRequest is the payload sent to the upstream LLM.
type ChatRequest struct {
	Model    string        `json:"model"`
	Messages []ChatMessage `json:"messages"`
	Tools    []Tool        `json:"tools,omitempty"`
	Stream   bool          `json:"stream"`
}

// ChatMessage is a single message in the upstream payload.
type ChatMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
}

// ChatResponse is the parsed upstream response.
type ChatResponse struct {
	Content string
}

// Chat sends a chat request to the provider with context sliding applied.
// It returns the assistant's reply text.
func (c *Client) Chat(ctx context.Context, p db.Provider, model string, systemPrompt string, history []db.Message, maxTurns int) (string, error) {
	req := ChatRequest{
		Model:    model,
		Messages: buildMessages(systemPrompt, history, maxTurns),
		Stream:   false,
	}
	content, _, err := c.doChat(ctx, p, req)
	return content, err
}

// buildMessages constructs the pruned payload: system prompt + last N
// user/assistant turns (context sliding). A turn is one user + one assistant
// message pair; if the last message is a user message without a reply yet,
// it is kept as the final user turn.
func buildMessages(systemPrompt string, history []db.Message, maxTurns int) []ChatMessage {
	messages := make([]ChatMessage, 0, maxTurns+1)
	if strings.TrimSpace(systemPrompt) != "" {
		messages = append(messages, ChatMessage{Role: "system", Content: systemPrompt})
	}
	start := 0
	if len(history) > maxTurns*2 {
		start = len(history) - maxTurns*2
	}
	for _, m := range history[start:] {
		messages = append(messages, ChatMessage{Role: m.Role, Content: m.Content})
	}
	return messages
}

// doChat POSTs a ChatRequest to the upstream provider and parses the reply.
// It returns the assistant content and any tool calls the model emitted.
func (c *Client) doChat(ctx context.Context, p db.Provider, req ChatRequest) (string, []ToolCall, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return "", nil, err
	}

	base := normalizeBaseURL(p.BaseURL)
	var url string
	switch p.Type {
	case "ollama":
		url = strings.TrimRight(base, "/") + "/api/chat"
	default:
		url = strings.TrimRight(base, "/") + "/v1/chat/completions"
	}

	reqHTTP, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", nil, err
	}
	reqHTTP.Header.Set("Content-Type", "application/json")
	applyAuth(reqHTTP, p)

	resp, err := c.http.Do(reqHTTP)
	if err != nil {
		return "", nil, fmt.Errorf("upstream request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return "", nil, err
	}

	// Dump the exact request payload and raw upstream response for inspection.
	stamp := time.Now().Format("20060102_150405.000000000")
	c.dump(stamp, "request.json", body)
	c.dump(stamp, "response.raw", respBody)

	if resp.StatusCode != http.StatusOK {
		return "", nil, fmt.Errorf("upstream returned %d: %s", resp.StatusCode, string(respBody))
	}

	switch p.Type {
	case "ollama":
		var ollamaResp struct {
			Message struct {
				Content   string     `json:"content"`
				ToolCalls []ToolCall `json:"tool_calls"`
			} `json:"message"`
			Error string `json:"error"`
		}
		if err := json.Unmarshal(respBody, &ollamaResp); err != nil {
			return "", nil, err
		}
		if ollamaResp.Error != "" {
			return "", nil, errors.New(ollamaResp.Error)
		}
		for i := range ollamaResp.Message.ToolCalls {
			if ollamaResp.Message.ToolCalls[i].ID == "" {
				ollamaResp.Message.ToolCalls[i].ID = fmt.Sprintf("call_%d", i)
			}
		}
		return ollamaResp.Message.Content, ollamaResp.Message.ToolCalls, nil
	default:
		var openAIResp struct {
			Choices []struct {
				Message struct {
					Content   string     `json:"content"`
					ToolCalls []ToolCall `json:"tool_calls"`
				} `json:"message"`
			} `json:"choices"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(respBody, &openAIResp); err != nil {
			return "", nil, err
		}
		if openAIResp.Error != nil {
			return "", nil, errors.New(openAIResp.Error.Message)
		}
		if len(openAIResp.Choices) == 0 {
			return "", nil, errors.New("upstream returned no choices")
		}
		return openAIResp.Choices[0].Message.Content, openAIResp.Choices[0].Message.ToolCalls, nil
	}
}

// GenerateSuggestions asks the LLM to generate 5 short response suggestions
// based on the last assistant message and conversation context.
// Returns a slice of up to 5 suggestion strings.
func (c *Client) GenerateSuggestions(ctx context.Context, p db.Provider, model string, systemPrompt string, history []db.Message, lastAssistantMsg string) ([]string, error) {
	// Build messages for the suggestion request
	messages := make([]ChatMessage, 0, 4)
	if strings.TrimSpace(systemPrompt) != "" {
		messages = append(messages, ChatMessage{Role: "system", Content: systemPrompt})
	}

	// Include recent conversation context (last few messages)
	start := 0
	if len(history) > 6 {
		start = len(history) - 6
	}
	for _, m := range history[start:] {
		messages = append(messages, ChatMessage{Role: m.Role, Content: m.Content})
	}

	// Add the instruction to generate suggestions
	instruction := fmt.Sprintf(
		"The assistant just said to the user: \"%s\"\n\n"+
			"Generate 5 short, natural response options the user might say next. "+
			"Each response should be 1-10 words, conversational, and represent different possible reactions "+
			"(e.g., agreement, question, surprise, emotional response, or continuation). "+
			"Return ONLY a JSON array of strings, nothing else. Example: [\"response1\", \"response2\", \"response3\", \"response4\", \"response5\"]",
		lastAssistantMsg,
	)
	messages = append(messages, ChatMessage{Role: "user", Content: instruction})

	payload := ChatRequest{
		Model:    model,
		Messages: messages,
		Stream:   false,
	}

	base := normalizeBaseURL(p.BaseURL)
	var url string
	switch p.Type {
	case "ollama":
		url = strings.TrimRight(base, "/") + "/api/chat"
	default:
		url = strings.TrimRight(base, "/") + "/v1/chat/completions"
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	applyAuth(req, p)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("suggestion request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("suggestion request returned %d: %s", resp.StatusCode, string(respBody))
	}

	var rawContent string
	switch p.Type {
	case "ollama":
		var ollamaResp struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			Error string `json:"error"`
		}
		if err := json.Unmarshal(respBody, &ollamaResp); err != nil {
			return nil, err
		}
		if ollamaResp.Error != "" {
			return nil, errors.New(ollamaResp.Error)
		}
		rawContent = ollamaResp.Message.Content
	default:
		var openAIResp struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(respBody, &openAIResp); err != nil {
			return nil, err
		}
		if openAIResp.Error != nil {
			return nil, errors.New(openAIResp.Error.Message)
		}
		if len(openAIResp.Choices) == 0 {
			return nil, errors.New("upstream returned no choices for suggestions")
		}
		rawContent = openAIResp.Choices[0].Message.Content
	}

	// Parse the JSON array from the response
	rawContent = strings.TrimSpace(rawContent)

	// Try to extract JSON array from the response (handle cases where LLM adds extra text)
	startIdx := strings.Index(rawContent, "[")
	endIdx := strings.LastIndex(rawContent, "]")
	if startIdx != -1 && endIdx != -1 && endIdx > startIdx {
		rawContent = rawContent[startIdx : endIdx+1]
	}

	var suggestions []string
	if err := json.Unmarshal([]byte(rawContent), &suggestions); err != nil {
		// If parsing fails, return a fallback
		return []string{"Tell me more", "I agree", "What do you mean?", "Continue", "I'm not sure"}, nil
	}

	// Limit to 5 suggestions and ensure none are empty
	result := make([]string, 0, 5)
	for _, s := range suggestions {
		s = strings.TrimSpace(s)
		if s != "" && len(result) < 5 {
			result = append(result, s)
		}
	}

	// If we got no valid suggestions, provide fallbacks
	if len(result) == 0 {
		return []string{"Tell me more", "I agree", "What do you mean?", "Continue", "I'm not sure"}, nil
	}

	return result, nil
}
