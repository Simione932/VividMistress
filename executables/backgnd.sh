#!/bin/bash
# backgnd.sh - Run an executable in the background on Linux/Unix systems.
#
# Usage:
#   ./backgnd.sh <executable> [args...]
#
# Example:
#   ./backgnd.sh ./linux_x64/server -addr :8080 -data ./data
#
# The process is started detached (survives terminal close via nohup),
# its output is written to a log file, and its PID is saved to a .pid file.

set -u

if [ $# -lt 1 ]; then
    echo "Usage: $0 <executable> [args...]" >&2
    exit 1
fi

EXEC="$1"
shift # remaining arguments are passed to the executable

if [ ! -f "$EXEC" ]; then
    echo "Error: '$EXEC' does not exist." >&2
    exit 1
fi

if [ ! -x "$EXEC" ]; then
    echo "Error: '$EXEC' is not executable. Run: chmod +x "$EXEC"" >&2
    exit 1
fi

BASE="$(basename "$EXEC")"
LOG_FILE="${BASE}.log"
PID_FILE="${BASE}.pid"

nohup "$EXEC" "$@" > "$LOG_FILE" 2>&1 &
PID=$!

echo "$PID" > "$PID_FILE"

echo "Started '$EXEC' in the background (PID: $PID)"
echo "  Log file: $(pwd)/$LOG_FILE"
echo "  PID file: $(pwd)/$PID_FILE"
echo "To stop it: kill \$(cat $PID_FILE)"