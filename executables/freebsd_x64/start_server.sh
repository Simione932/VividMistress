#!/bin/bash
# start_server.sh - Run the Vivid Mistress server in the background on Linux/Unix.
#
# This script starts the server detached so it keeps running even after the
# terminal is closed or the SSH session ends.
#
# Usage:
#   ./start_server.sh            Start the server in the background
#   ./start_server.sh stop       Stop the background server
#   ./start_server.sh status     Show whether the server is running
#   ./start_server.sh restart    Stop, then start again
#
# Extra arguments are passed to the server, e.g.:
#   ./start_server.sh -addr :9090 -data /home/pi/chatdata
#
# The server keeps running after you close the terminal. Its output is
# written to server.log and its process ID to server.pid (both created
# in this script's folder).

set -u

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BIN="$DIR/server"
PID_FILE="$DIR/server.pid"
LOG_FILE="$DIR/server.log"

is_running() {
    [ -f "$PID_FILE" ] && kill -0 "$(cat "$PID_FILE")" 2>/dev/null
}

start() {
    if is_running; then
        echo "Server is already running (PID: $(cat "$PID_FILE"))."
        echo "Stop it first with: $0 stop"
        exit 0
    fi

    if [ ! -f "$BIN" ]; then
        echo "Error: '$BIN' not found." >&2
        echo "Put the 'server' binary in this folder ($DIR) first." >&2
        exit 1
    fi

    # Downloads often lose the executable bit - restore it if needed.
    [ -x "$BIN" ] || chmod +x "$BIN"

    nohup "$BIN" "$@" >> "$LOG_FILE" 2>&1 &
    echo $! > "$PID_FILE"

    # Give it a moment, then confirm it is still alive.
    sleep 1
    if ! kill -0 "$(cat "$PID_FILE")" 2>/dev/null; then
        echo "Error: the server exited immediately. Check $LOG_FILE" >&2
        rm -f "$PID_FILE"
        exit 1
    fi

    echo "Server started in the background (PID: $(cat "$PID_FILE"))."
    echo "  Log:      $LOG_FILE"
    echo "  Stop:     $0 stop"
    echo "  Status:   $0 status"
}

stop() {
    if ! is_running; then
        echo "Server is not running."
        rm -f "$PID_FILE"
        return 0
    fi

    PID="$(cat "$PID_FILE")"
    echo "Stopping server (PID: $PID)..."
    kill "$PID" 2>/dev/null

    # Wait up to 10 seconds for a graceful shutdown.
    i=0
    while kill -0 "$PID" 2>/dev/null && [ "$i" -lt 10 ]; do
        sleep 1
        i=$((i + 1))
    done

    if kill -0 "$PID" 2>/dev/null; then
        echo "Still running - sending forceful shutdown (SIGKILL)."
        kill -9 "$PID" 2>/dev/null
    fi

    rm -f "$PID_FILE"
    echo "Server stopped."
}

status() {
    if is_running; then
        echo "Server is running (PID: $(cat "$PID_FILE"))."
    else
        echo "Server is not running."
        rm -f "$PID_FILE"
    fi
}

case "${1:-}" in
    stop)
        stop
        ;;
    status)
        status
        ;;
    restart)
        shift
        stop
        start "$@"
        ;;
    "")
        start
        ;;
    *)
        # Anything else is passed through to the server.
        start "$@"
        ;;
esac