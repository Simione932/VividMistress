package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"pi-chat-gateway/internal/db"
)

// TestChatWithTools verifies that tool calls are executed, their results fed
// back as tool messages, and retried until the model returns a plain reply.
func TestChatWithTools(t *testing.T) {
	calls := 0
	var secondReqBody []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"choices":[{"message":{"content":"","tool_calls":[{"id":"c1","function":{"name":"chastity","arguments":"{\"action\":\"shock\",\"duration_seconds\":20}"}}]}}]}`))
		} else {
			secondReqBody, _ = io.ReadAll(r.Body)
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"choices":[{"message":{"content":"Done."}}]}`))
		}
	}))
	defer upstream.Close()

	client := NewClient()
	provider := db.Provider{ID: "t", Name: "T", BaseURL: upstream.URL, Type: "llamacpp"}
	var execCalls []string
	reply, events, err := client.ChatWithTools(context.Background(), provider, "m", "sys",
		[]db.Message{{Role: "user", Content: "shock me"}}, 6,
		[]Tool{ChastityTestTool()}, func(name, args string) string {
			execCalls = append(execCalls, name+" "+args)
			return "Shock delivered for 20 seconds."
		})
	if err != nil {
		t.Fatalf("ChatWithTools failed: %v", err)
	}
	if reply != "Done." {
		t.Errorf("unexpected reply: %q", reply)
	}
	if len(execCalls) != 1 || execCalls[0] != "chastity {\"action\":\"shock\",\"duration_seconds\":20}" {
		t.Errorf("unexpected tool executions: %v", execCalls)
	}
	if len(events) != 1 || events[0].Name != "chastity" || events[0].Result != "Shock delivered for 20 seconds." {
		t.Errorf("unexpected events: %v", events)
	}

	// The retried request must carry the assistant tool_calls turn and the
	// tool result message.
	var secondReq struct {
		Messages []ChatMessage `json:"messages"`
	}
	if err := json.Unmarshal(secondReqBody, &secondReq); err != nil {
		t.Fatalf("second request is not valid JSON: %v", err)
	}
	var assistant, tool *ChatMessage
	for _, m := range secondReq.Messages {
		if m.Role == "assistant" && m.ToolCallID != "" || len(m.ToolCalls) > 0 {
			assistant = &m
		}
		if m.Role == "tool" {
			tool = &m
		}
	}
	if assistant == nil {
		t.Errorf("second request missing assistant tool_calls turn")
	}
	// The echoed tool_call arguments must be a JSON object, not a quoted
	// string, or Ollama rejects the retried request with:
	// "Value looks like object, but can't find closing '}' symbol".
	if len(assistant.ToolCalls) != 1 {
		t.Fatalf("expected 1 echoed tool call, got %d", len(assistant.ToolCalls))
	}
	argJSON := assistant.ToolCalls[0].Function.Arguments.String()
	var argVal any
	if err := json.Unmarshal([]byte(argJSON), &argVal); err != nil {
		t.Fatalf("echoed arguments are not valid JSON: %q (%v)", argJSON, err)
	}
	if _, ok := argVal.(map[string]any); !ok {
		t.Errorf("echoed arguments must be a JSON object, got %T: %q", argVal, argJSON)
	}
	if tool == nil || tool.ToolCallID != "c1" || tool.Content != "Shock delivered for 20 seconds." {
		t.Errorf("second request missing/incorrect tool message: %+v", tool)
	}
}

// TestChatWithToolsOllama verifies the Ollama response path synthesizes
// tool_call ids when the model omits them.
func TestChatWithToolsOllama(t *testing.T) {
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"message":{"role":"assistant","content":"","tool_calls":[{"function":{"name":"chastity","arguments":"{\"action\":\"lock\"}"}}]}}`))
		} else {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"message":{"role":"assistant","content":"Locked."}}`))
		}
	}))
	defer upstream.Close()

	client := NewClient()
	provider := db.Provider{ID: "t", Name: "T", BaseURL: upstream.URL, Type: "ollama"}
	reply, events, err := client.ChatWithTools(context.Background(), provider, "m", "sys",
		[]db.Message{{Role: "user", Content: "lock it"}}, 6,
		[]Tool{ChastityTestTool()}, func(name, args string) string { return "locked" })
	if err != nil {
		t.Fatalf("ChatWithTools failed: %v", err)
	}
	if reply != "Locked." || len(events) != 1 {
		t.Errorf("unexpected result: reply=%q events=%v", reply, events)
	}
}

// TestChatWithToolsOllamaArgumentsObject verifies the Ollama response path
// tolerates function.arguments being returned as a JSON object rather than a
// JSON string (a shape some Ollama versions/models emit).
func TestChatWithToolsOllamaArgumentsObject(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			body, _ := io.ReadAll(r.Body)
			if bytes.Contains(body, []byte("tool_calls")) {
				// Retried request: reply with a plain message.
				w.Write([]byte(`{"message":{"role":"assistant","content":"Locked."}}`))
				return
			}
		}
		// First request: emit a tool call whose arguments are a JSON object.
		resp, err := json.Marshal(map[string]any{
			"message": map[string]any{
				"role":    "assistant",
				"content": "",
				"tool_calls": []map[string]any{
					{"function": map[string]any{"name": "chastity", "arguments": map[string]any{"action": "lock"}}},
				},
			},
		})
		if err != nil {
			t.Fatalf("failed to build response: %v", err)
		}
		w.Write(resp)
	}))
	defer upstream.Close()

	client := NewClient()
	provider := db.Provider{ID: "t", Name: "T", BaseURL: upstream.URL, Type: "ollama"}
	reply, events, err := client.ChatWithTools(context.Background(), provider, "m", "sys",
		[]db.Message{{Role: "user", Content: "lock it"}}, 6,
		[]Tool{ChastityTestTool()}, func(name, args string) string {
			if name != "chastity" {
				t.Errorf("unexpected tool name: %q", name)
			}
			if args != "{\"action\":\"lock\"}" {
				t.Errorf("unexpected args: %q", args)
			}
			return "locked"
		})
	if err != nil {
		t.Fatalf("ChatWithTools failed: %v", err)
	}
	if reply != "Locked." || len(events) != 1 || events[0].Arguments != "{\"action\":\"lock\"}" {
		t.Errorf("unexpected result: reply=%q events=%v", reply, events)
	}
}

// ChastityTestTool is a minimal tool definition for tests.
func ChastityTestTool() Tool {
	return Tool{
		Type:     "function",
		Function: ToolFunc{Name: "chastity", Description: "test", Parameters: map[string]any{"type": "object"}},
	}
}
