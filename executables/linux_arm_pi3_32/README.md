# PI-CHAT GATEWAY - Raspberry Pi 3 (32-bit OS)

**Platform:** Linux ARMv7 (32-bit)
**File:** `server`

## System Requirements

- Raspberry Pi 3 running a **32-bit** OS (ARMv7), or any other ARMv7 Linux device
- No additional dependencies required — the web UI is embedded in the binary

> **Note:** If your Pi 3 runs a **64-bit** OS, use [linux_arm64](../linux_arm64) instead. For a Pi Zero W / Pi 1, use [linux_arm_pi_zero_w](../linux_arm_pi_zero_w).

## How to Run

1. **Download** `server` from this folder
2. **Copy it to the Pi** and make it executable:
   ```bash
   scp server pi@<pi-ip>:~/
   ssh pi@<pi-ip>
   chmod +x server
   ./server -addr :8080 -data ./data
   ```
3. Open `http://<pi-ip>:8080` in a browser on any device on your network

## Configuration

| Flag     | Default   | Description                |
|----------|-----------|----------------------------|
| `-addr`  | `:8080`   | HTTP listen address        |
| `-data`  | `./data`  | Directory for JSON storage |

All settings (LLM providers, models, system prompts, conversations) are managed through the web UI and stored as flat JSON files under the `-data` directory.

## First Time Setup

When you start the server for the first time, it will:
1. Create the necessary directory structure (`data/`) with flat-file JSON storage
2. Serve the embedded web UI at `http://<pi-ip>:8080`
3. Let you add LLM providers (Ollama / llama.cpp endpoints), models, and system prompts through the web UI

## Running as a Service (systemd)

To run the server as a background service:

```bash
sudo nano /etc/systemd/system/server.service
```

Add the following (adjust paths as needed):

```ini
[Unit]
Description=PI-CHAT GATEWAY
After=network.target

[Service]
Type=simple
User=pi
WorkingDirectory=/home/pi/server
ExecStart=/home/pi/server/server -addr :8080 -data ./data
Restart=on-failure
RestartSec=10

[Install]
WantedBy=multi-user.target
```

Then enable and start the service:

```bash
sudo systemctl daemon-reload
sudo systemctl enable server
sudo systemctl start server
```

## Building from Source

If you prefer to build from source, see the main project [README.md](../../README.md) for instructions, or run `build_all.bat` / `build_all.ps1` in the `executables/` folder (requires a Windows machine).