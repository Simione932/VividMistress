package server

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"pi-chat-gateway/internal/llm"
)

// ChastityState tracks the live state of the chastity device.
type ChastityState struct {
	mu                 sync.Mutex
	Locked             bool
	LastAction         string
	LastShockSecs      int
	LastShockIntensity int
	LastShockAt        int64
}

func NewChastityState() *ChastityState {
	return &ChastityState{}
}

// ChastityTool is the tool definition handed to the LLM.
var ChastityTool = llm.Tool{
	Type: "function",
	Function: llm.ToolFunc{
		Name:        "chastity",
		Description: "Controls the chastity device. Use action \"lock\" to lock it, \"unlock\" to unlock it, and \"shock\" to deliver an electric shock for a duration of 5 to 30 seconds (duration_seconds and intensity required for shock, intensity 0=weak to 100=very strong).",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"action": map[string]any{
					"type":        "string",
					"enum":        []string{"lock", "unlock", "shock"},
					"description": "The action to perform on the device.",
				},
				"duration_seconds": map[string]any{
					"type":        "integer",
					"minimum":     5,
					"maximum":     30,
					"description": "Shock duration in seconds, required when action is \"shock\" (5–30).",
				},
				"intensity": map[string]any{
					"type":        "integer",
					"minimum":     0,
					"maximum":     100,
					"description": "Shock intensity from 0 (weak) to 100 (very strong), required when action is \"shock\".",
				},
			},
			"required": []string{"action"},
		},
	},
}

// chastityExecutor is the tool executor for the chastity tool. It mutates
// the server's device state and returns a result string fed back to the model.
func (s *Server) chastityExecutor(name string, argsJSON string) string {
	var args struct {
		Action          string `json:"action"`
		DurationSeconds *int   `json:"duration_seconds"`
		Intensity       *int   `json:"intensity"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return fmt.Sprintf("Invalid arguments (%s). Retry with a valid action (lock, unlock or shock).", err.Error())
	}
	action := strings.ToLower(strings.TrimSpace(args.Action))
	switch action {
	case "lock":
		s.chastity.mu.Lock()
		already := s.chastity.Locked
		s.chastity.Locked = true
		s.chastity.LastAction = "lock"
		s.chastity.mu.Unlock()
		if already {
			return "The device is already locked. It remains locked."
		}
		return "The chastity device is now locked."
	case "unlock":
		s.chastity.mu.Lock()
		already := !s.chastity.Locked
		s.chastity.Locked = false
		s.chastity.LastAction = "unlock"
		s.chastity.mu.Unlock()
		if already {
			return "The device was not locked, so nothing was unlocked."
		}
		return "The chastity device is now unlocked."
	case "shock":
		if args.DurationSeconds == nil {
			return "Missing shock duration. Retry with duration_seconds between 5 and 30."
		}
		if *args.DurationSeconds < 5 || *args.DurationSeconds > 30 {
			return fmt.Sprintf("Invalid shock duration (%d seconds). Retry with a duration between 5 and 30 seconds.", *args.DurationSeconds)
		}
		if args.Intensity == nil {
			return "Missing shock intensity. Retry with intensity between 0 and 100."
		}
		if *args.Intensity < 0 || *args.Intensity > 100 {
			return fmt.Sprintf("Invalid shock intensity (%d). Retry with an intensity between 0 and 100.", *args.Intensity)
		}
		sec := *args.DurationSeconds
		intensity := *args.Intensity
		s.chastity.mu.Lock()
		s.chastity.LastAction = "shock"
		s.chastity.LastShockSecs = sec
		s.chastity.LastShockIntensity = intensity
		s.chastity.LastShockAt = time.Now().Unix()
		s.chastity.mu.Unlock()
		return fmt.Sprintf("Shock delivered for %d seconds at intensity %d.", sec, intensity)
	default:
		return fmt.Sprintf("Unknown action %q. Retry with lock, unlock or shock.", action)
	}
}

// chastityStatus returns a snapshot of the device state for the API responses.
func (s *Server) chastityStatus() map[string]any {
	s.chastity.mu.Lock()
	defer s.chastity.mu.Unlock()
	return map[string]any{
		"locked":               s.chastity.Locked,
		"last_action":          s.chastity.LastAction,
		"last_shock_secs":      s.chastity.LastShockSecs,
		"last_shock_intensity": s.chastity.LastShockIntensity,
		"last_shock_at":        s.chastity.LastShockAt,
	}
}
