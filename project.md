# Project Overview: Vivid Mistress

`vividMistress` is a Go-based Telegram bot worker that listens for messages via long polling, generates AI responses via Ollama (or mock mode), and optionally converts responses to speech using Microsoft Edge's TTS engine.

---

## Core Architecture

### 1. Telegram Bot (Long-Polling)
- Polls Telegram for incoming messages using bot token from `config.yaml`
- Supports two message sources:
  - **Direct Telegram messages** from the authorized user
  - **Web page input** sent through the Telegram API (marked with `▶` prefix)
- Filters out bot's own replies to prevent echo loops
- Maintains an in-memory chat timeline accessible via `/api/replies`

### 2. AI Response Generation
- **Ollama Online** (LLM API): enabled via `ollama.enabled: true` in config
- **Mock mode** (default): returns a canned test response for development/testing
- Role-based prompting via `roles/*.txt` files (e.g., `Mistress.txt`)

### 3. Tool Calling (chastity device)
- The LLM client supports upstream tool calling for both Ollama (`/api/chat`) and OpenAI-compatible (`/v1/chat/completions`) providers.
- A single tool `chastity` is always offered: `action` (`lock` | `unlock` | `shock`) is required; `duration_seconds` (integer, 5–30) is required only for `shock`.
- When the model returns tool calls instead of a reply, the server executes them (mutating the shared `ChastityState`), feeds results back as `role: "tool"` messages, and retries (up to 4 iterations) until a plain reply is produced.
- `POST /api/chat` and `POST /api/regenerate` responses include `tool_calls` (name/arguments/result per call) and `chastity` state (`locked`, `last_action`, `last_shock_secs`, `last_shock_at`).
- The web app renders each tool event as a short assistant notice (e.g., "🔐 chastity · lock → The chastity device is now locked.").

### 4. Text-to-Speech (vividvoice)
- Powered by the shared `vividvoice` module (`C:\dev\vividvoice`)
- **Primary engine**: Microsoft Edge TTS neural voices via WebSockets in Go — zero Python, zero API keys
- **Multi-engine fallback**: Automatic failover to local Windows SAPI (`System.Speech`) and optional online proxy URL
- Built-in text preprocessing (markdown stripping, emoji filtering, rate normalization)
- Configurable voice, rate, pitch settings via config.yaml

---

## API Endpoints (Local Test Server)

| Endpoint | Description |
|----------|-------------|
| `GET /health` | Worker status, bot ID, model, role info |
| `GET /roles` | List available role files |
| `GET /api/replies` | Chat timeline (long-poll friendly via `since_id`) |
| `GET /api/tts?text=...&voice=...` | Text-to-speech MP3 stream |

---

## Technical Stack

- **Backend:** Go 1.22
- **LLM:** Ollama API (configurable)
- **TTS:** `vividvoice` package (direct Microsoft Edge TTS neural → falls back to Windows SAPI / online URL)
- **Frontend:** Single HTML page (`index.html`) with Telegram integration
- **Config:** `config.yaml` (single source of truth, no env vars)

---

## Edge Cases & Constraints

1. **TTS Fallback Chain** — The `vividvoice` module calls Microsoft Edge TTS over WebSockets. If unreachable or failing, it automatically falls back to local Windows SAPI (offline desktop voice) or a configured online proxy URL without crashing or dropping responses.
2. **Echo Prevention** — The bot ID is derived from the bot token (numeric prefix) to recognize and skip the bot's own Telegram replies.
3. **Test API first** — The local HTTP server runs before Telegram polling starts, so health/roles endpoints respond immediately.
4. **Web page workflow** — The page sends messages through the Telegram API (not the worker), so the worker needs the same bot token as the page.
5. **No inbound ports** — The worker only outbound-polls Telegram; the test API is local-only (`127.0.0.1:8080`).