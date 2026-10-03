# PI-CHAT GATEWAY - macOS Intel (x64)

**Platform:** macOS 64-bit Intel (x86_64)
**File:** `server`

## System Requirements

- Mac with an Intel processor
- No additional dependencies required — the web UI is embedded in the binary

## How to Run

1. **Download** `server` from this folder
2. **Make it executable** and run:
   ```bash
   chmod +x server
   ./server -addr :8080 -data ./data
   ```
3. Open `http://localhost:8080` in a browser

> **Note:** macOS may block the executable because it's from an unidentified developer. To bypass this:
> - Go to **System Settings > Privacy & Security**
> - Scroll down and click **"Open Anyway"** next to the blocked message
> - Or in Terminal, run: `xattr -d com.apple.quarantine server`

## Configuration

| Flag     | Default   | Description                |
|----------|-----------|----------------------------|
| `-addr`  | `:8080`   | HTTP listen address        |
| `-data`  | `./data`  | Directory for JSON storage |

All settings (LLM providers, models, system prompts, conversations) are managed through the web UI and stored as flat JSON files under the `-data` directory.

## First Time Setup

When you start the server for the first time, it will:
1. Create the necessary directory structure (`data/`) with flat-file JSON storage
2. Serve the embedded web UI at `http://<host>:8080`
3. Let you add LLM providers (Ollama / llama.cpp endpoints), models, and system prompts through the web UI

## Building from Source

If you prefer to build from source, see the main project [README.md](../../README.md) for instructions, or run `build_all.bat` / `build_all.ps1` in the `executables/` folder.