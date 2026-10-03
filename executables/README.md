# Quickstart — Download the Executable for Your Operating System

Pre-built binaries for PI-CHAT GATEWAY are available below. Download the executable that matches your operating system and architecture, then follow the platform-specific instructions.

---

## Windows

| Architecture | File | Download |
|---|---|---|
| Windows x64 (Intel/AMD 64-bit) | `server.exe` | [Download](https://github.com/vividchatapp/vividhome/raw/refs/heads/main/executables/windows_x64/server.exe) |
| Windows ARM64 | `server.exe` | [Download](https://github.com/vividchatapp/vividhome/raw/refs/heads/main/executables/windows_arm64/server.exe) |
| Windows x86 (32-bit) | `server.exe` | [Download](https://github.com/vividchatapp/vividhome/raw/refs/heads/main/executables/windows_x86/server.exe) |

**How to run:** Double-click the `.exe` file, or run from Command Prompt / PowerShell:
```
server.exe -addr :8080 -data ./data
```

---

## Linux

| Architecture | File | Download |
|---|---|---|
| Linux x64 (Intel/AMD 64-bit) | `server` | [Download](https://github.com/vividchatapp/vividhome/raw/refs/heads/main/executables/linux_x64/server) |
| [Linux ARM64 / Raspberry Pi 3 (64-bit OS)](https://github.com/vividchatapp/vividhome/tree/main/executables/linux_arm64) | `server` | [Download](https://github.com/vividchatapp/vividhome/raw/refs/heads/main/executables/linux_arm64/server) |
| Raspberry Pi 2 (32-bit OS, ARMv7) | `server` | [Download](https://github.com/vividchatapp/vividhome/raw/refs/heads/main/executables/linux_arm_pi2/server) |
| Raspberry Pi Zero W / Pi 1 (ARMv6) | `server` | [Download](https://github.com/vividchatapp/vividhome/raw/refs/heads/main/executables/linux_arm_pi_zero_w/server) |
| [Raspberry Pi 3 (32-bit OS, ARMv7)](https://github.com/vividchatapp/vividhome/tree/main/executables/linux_arm_pi3_32) | `server` | [Download](https://github.com/vividchatapp/vividhome/raw/refs/heads/main/executables/linux_arm_pi3_32/server) |
| Linux x86 (32-bit) | `server` | [Download](https://github.com/vividchatapp/vividhome/raw/refs/heads/main/executables/linux_x86/server) |

Raspberry Pi 3 is available for both 32-bit and 64-bit operating systems under the `linux_arm_pi3_32` and `linux_arm64` folders. The Zero W / Pi 1 build is 32-bit ARMv6 only.

**How to run:**
```bash
chmod +x server
./server -addr :8080 -data ./data
```

To run it in the background (survives closing the terminal), use the helper script in this folder:
```bash
./backgnd.sh ./linux_x64/server -addr :8080 -data ./data
```

---

## macOS

| Architecture | File | Download |
|---|---|---|
| macOS Intel (x64) | `server` | [Download](https://github.com/vividchatapp/vividhome/raw/refs/heads/main/executables/mac_x64/server) |
| macOS Apple Silicon (M1/M2/M3/M4) | `server` | [Download](https://github.com/vividchatapp/vividhome/raw/refs/heads/main/executables/mac_arm64/server) |

---

## FreeBSD

| Architecture | File | Download |
|---|---|---|
| FreeBSD x64 | `server` | [Download](https://github.com/vividchatapp/vividhome/raw/refs/heads/main/executables/freebsd_x64/server) |

**How to run:**
```bash
chmod +x server
./server -addr :8080 -data ./data
```

> **Note:** macOS may block the executable because it's from an unidentified developer. To bypass this:
> - Go to **System Settings > Privacy & Security**
> - Scroll down and click **"Open Anyway"** next to the blocked message
> - Or in Terminal, run `xattr -d com.apple.quarantine server`

---

## First Time Setup

When you start the server for the first time, it will:
1. Create the necessary directory structure (`data/`) with flat-file JSON storage
2. Serve the embedded web UI at `http://<host>:8080`
3. Let you add LLM providers (Ollama / llama.cpp endpoints), models, and system prompts through the web UI — no config files to edit by hand

## Building from Source

If you prefer to build from source, see the main project [README.md](../README.md) for instructions, or run `build_all.bat` / `build_all.ps1` in this folder.
