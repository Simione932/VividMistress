# PI-CHAT GATEWAY - Linux ARM64 / Raspberry Pi 3 (64-bit OS)

**Platform:** Linux ARM64 (AArch64, 64-bit)
**File:** `server`

## System Requirements

- Raspberry Pi 3 running a **64-bit** OS, or any other ARM64 Linux device
- No additional dependencies required — the web UI is embedded in the binary

## How to Run

1. **Download** `server` from this folder
2. **Make it executable** and run:
   ```bash
   chmod +x server
   ./server -addr :8080 -data ./data
   ```
3. Open `http://<pi-ip>:8080` in a browser

## Configuration

| Flag     | Default   | Description                |
|----------|-----------|----------------------------|
| `-addr`  | `:8080`   | HTTP listen address        |
| `-data`  | `./data`  | Directory for JSON storage |

All settings (LLM providers, models, system prompts, conversations) are managed through the web UI and stored as flat JSON files under the `-data` directory.

## Running in the Background

Use the helper script in the `executables/` folder to run the server detached (survives closing the terminal):

```bash
./backgnd.sh ./linux_arm64/server -addr :8080 -data ./data
```

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