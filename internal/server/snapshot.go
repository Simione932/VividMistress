package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"pi-chat-gateway/internal/db"
)

const (
	llamaSwapBaseURL = "http://xeonpair:8080"
	kreaModel        = "krea-2"
)

// generateImage renders a prompt through the llama-swap endpoint and returns
// the raw base64 payload plus its MIME type.
func generateImage(ctx context.Context, prompt string) (string, string, error) {
	payload, err := json.Marshal(map[string]string{
		"model":  kreaModel,
		"prompt": prompt,
	})
	if err != nil {
		return "", "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, llamaSwapBaseURL+"/v1/images/generations", bytes.NewReader(payload))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := (&http.Client{Timeout: 10 * time.Minute}).Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return "", "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("llama-swap returned %d: %s", resp.StatusCode, string(body[:min(len(body), 512)]))
	}

	var out struct {
		Data []struct {
			B64JSON string `json:"b64_json"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", "", err
	}
	if len(out.Data) == 0 || out.Data[0].B64JSON == "" {
		return "", "", fmt.Errorf("llama-swap returned no image data")
	}

	b64 := out.Data[0].B64JSON
	mime := "image/png"
	if strings.HasPrefix(b64, "/9j/") {
		mime = "image/jpeg"
	}
	return b64, mime, nil
}

// handleSnapshot implements the `snapshot` chat command: the LLM describes
// the current scene, llama-swap renders it, and the result is stored in the
// conversation so it survives reloads.
func (s *Server) handleSnapshot(w http.ResponseWriter, r *http.Request, clientID string, c *db.Conversation) {
	p, err := s.providerFor(c)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	now := time.Now().Unix()
	c.Messages = append(c.Messages, db.Message{Role: "user", Content: "snapshot", Timestamp: now})
	if len(c.Messages) == 1 {
		c.Title = summarizeTitle("snapshot")
	}

	history := c.Messages
	if c.Settings.Mode == "story" {
		history = storyContext(c.Messages)
	}

	descCtx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	desc, err := s.llm.SceneDescription(descCtx, p, s.modelFor(c), c.Settings.SystemPrompt, history, c.Settings.MaxTurns)
	if err != nil {
		log.Printf("[snapshot] description failed: %v", err)
		c.UpdatedAt = now
		_ = s.store.SaveConversation(clientID, *c)
		writeErr(w, http.StatusBadGateway, "scene description failed: "+err.Error())
		return
	}
	desc = strings.TrimSpace(desc)
	if desc == "" {
		desc = "the current scene"
	}
	log.Printf("[snapshot] description: %.160s", desc)

	imgCtx, cancelImg := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancelImg()
	b64, mime, err := generateImage(imgCtx, desc)
	if err != nil {
		log.Printf("[snapshot] image generation failed: %v", err)
		c.UpdatedAt = now
		_ = s.store.SaveConversation(clientID, *c)
		writeErr(w, http.StatusBadGateway, "image generation failed: "+err.Error())
		return
	}

	c.Messages = append(c.Messages, db.Message{
		Role:      "assistant",
		Content:   "📸 " + desc,
		Image:     b64,
		ImageMime: mime,
		Timestamp: time.Now().Unix(),
	})
	c.UpdatedAt = time.Now().Unix()
	if err := s.store.SaveConversation(clientID, *c); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	log.Printf("[snapshot] saved %s image (%d base64 chars)", mime, len(b64))
	writeJSON(w, http.StatusOK, map[string]any{
		"conversation": c,
		"auto":         false,
		"role":         s.roleNameForConversation(*c),
	})
}
