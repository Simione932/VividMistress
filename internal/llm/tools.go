package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"pi-chat-gateway/internal/db"
)

// ToolCallArguments holds the function arguments returned by an upstream model
// inside a tool call. Different models (and different Ollama versions) encode
// these differently: some return a JSON-encoded string (e.g.
// "{\"action\":\"lock\"}") while others return an already-parsed JSON
// object ({}). We accept both and always expose the arguments as raw JSON text.
//
// This tolerance is what avoids:
//
//	json: cannot unmarshal object into Go struct field .message.tool_calls.0.function.arguments of type string
type ToolCallArguments struct {
	text string
}

// NewToolCallArguments wraps an already-known arguments string, trimming
// surrounding whitespace.
func NewToolCallArguments(s string) ToolCallArguments {
	return ToolCallArguments{text: strings.TrimSpace(s)}
}

// UnmarshalJSON accepts either a JSON string or any other JSON value. A string
// is stored verbatim; anything else is re-serialized to compact JSON text.
func (a *ToolCallArguments) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		a.text = s
		return nil
	}
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	out, err := json.Marshal(v)
	if err != nil {
		return err
	}
	a.text = string(out)
	return nil
}

// MarshalJSON emits the arguments as the JSON value they represent (an object
// for function calls) when we echo tool calls back in a retried request.
//
// Ollama parses tool_calls[].function.arguments and requires an object. Emitting
// it as a quoted string makes Ollama fail with:
//
//	"Value looks like object, but can't find closing '}' symbol"
//
// We only fall back to a quoted string when the stored text is not itself valid
// JSON (e.g. a bare value), so the common object case round-trips correctly.
func (a ToolCallArguments) MarshalJSON() ([]byte, error) {
	text := strings.TrimSpace(a.text)
	if text == "" {
		return []byte("{}"), nil
	}
	var v any
	if err := json.Unmarshal([]byte(text), &v); err == nil {
		return json.Marshal(v)
	}
	return json.Marshal(text)
}

// String returns the arguments as raw JSON text.
func (a ToolCallArguments) String() string {
	return a.text
}

// Tool describes an upstream function definition (OpenAI-style schema;
// Ollama accepts the same format in its /api/chat request).
type Tool struct {
	Type     string   `json:"type"` // always "function"
	Function ToolFunc `json:"function"`
}

// ToolFunc is the function schema inside a Tool.
type ToolFunc struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  any    `json:"parameters"`
}

// ToolCall is a tool invocation returned by the upstream model.
type ToolCall struct {
	ID       string `json:"id,omitempty"`
	Function struct {
		Name      string            `json:"name"`
		Arguments ToolCallArguments `json:"arguments"`
	} `json:"function"`
}

// ToolCallEvent records one executed tool call (for logging and the UI).
type ToolCallEvent struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
	Result    string `json:"result"`
}

// ToolExecutor executes a tool call and returns a result string fed back
// to the model as a tool message.
type ToolExecutor func(name string, arguments string) string

// ChatWithTools performs a chat request with tool definitions. When the
// model returns tool calls instead of a plain reply, they are executed via
// exec, their results fed back as tool messages, and the request retried
// (up to maxIterations) until a plain reply is produced. Returns the final
// reply text and the list of executed tool call events.
func (c *Client) ChatWithTools(ctx context.Context, p db.Provider, model string, systemPrompt string, history []db.Message, maxTurns int, tools []Tool, exec ToolExecutor) (string, []ToolCallEvent, error) {
	if len(tools) == 0 {
		reply, err := c.Chat(ctx, p, model, systemPrompt, history, maxTurns)
		return reply, nil, err
	}
	if exec == nil {
		return "", nil, fmt.Errorf("tool executor is nil")
	}

	messages := buildMessages(systemPrompt, history, maxTurns)
	const maxIterations = 4
	var lastContent string
	events := []ToolCallEvent{}
	for i := 0; i < maxIterations; i++ {
		req := ChatRequest{Model: model, Messages: messages, Tools: tools, Stream: false}
		content, calls, err := c.doChat(ctx, p, req)
		if err != nil {
			return "", nil, err
		}
		lastContent = content
		if len(calls) == 0 {
			return content, events, nil
		}

		// Append the assistant turn carrying its tool calls, then each
		// executed tool result as a tool message.
		assistant := ChatMessage{Role: "assistant", Content: content}
		assistant.ToolCalls = calls
		messages = append(messages, assistant)
		for _, call := range calls {
			args := call.Function.Arguments.String()
			result := exec(call.Function.Name, args)
			log.Printf("llm: tool call %s(%s) -> %s", call.Function.Name, args, result)
			events = append(events, ToolCallEvent{Name: call.Function.Name, Arguments: args, Result: result})
			messages = append(messages, ChatMessage{Role: "tool", ToolCallID: call.ID, Content: result})
		}
	}
	return lastContent, nil, fmt.Errorf("tool call loop exceeded %d iterations", maxIterations)
}
