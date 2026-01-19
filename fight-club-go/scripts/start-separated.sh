#!/bin/bash
# Start Fight Club with separated server and streamer processes
# This architecture isolates streaming from server load for smoother frames

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"

cd "$PROJECT_DIR"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}======================================${NC}"
echo -e "${BLUE}  FIGHT CLUB - SEPARATED ARCHITECTURE${NC}"
echo -e "${BLUE}======================================${NC}"
echo ""
echo -e "${YELLOW}This mode runs two separate processes:${NC}"
echo -e "  ${GREEN}1. Server${NC}: HTTP API, Game Engine, Webhooks"
echo -e "  ${GREEN}2. Streamer${NC}: Render + FFmpeg (isolated for smooth frames)"
echo ""

# Check if .env exists
if [ ! -f ".env" ] && [ ! -f "../.env" ]; then
    echo -e "${RED}Error: No .env file found!${NC}"
    echo "Create a .env file with your configuration."
    exit 1
fi

# Export IPC_ENABLED for server
export IPC_ENABLED=true

# Build both binaries
echo -e "${YELLOW}Building binaries...${NC}"
go build -o bin/server ./cmd/server
go build -o bin/streamer ./cmd/streamer
echo -e "${GREEN}Build complete!${NC}"
echo ""

# Function to cleanup on exit
cleanup() {
    echo ""
    echo -e "${YELLOW}Shutting down...${NC}"

    # Kill the server if running
    if [ ! -z "$SERVER_PID" ]; then
        echo "Stopping server (PID: $SERVER_PID)..."
        kill $SERVER_PID 2>/dev/null || true
    fi

    # Kill the streamer if running
    if [ ! -z "$STREAMER_PID" ]; then
        echo "Stopping streamer (PID: $STREAMER_PID)..."
        kill $STREAMER_PID 2>/dev/null || true
    fi

    # Cleanup socket
    rm -f /tmp/fight-club.sock

    echo -e "${GREEN}Goodbye!${NC}"
    exit 0
}

trap cleanup SIGINT SIGTERM

# Start server in background
echo -e "${GREEN}Starting server...${NC}"
IPC_ENABLED=true ./bin/server &
SERVER_PID=$!
echo -e "Server PID: $SERVER_PID"

# Wait for server to start and IPC socket to be ready
echo -e "${YELLOW}Waiting for IPC socket...${NC}"
for i in {1..10}; do
    if [ -S "/tmp/fight-club.sock" ]; then
        echo -e "${GREEN}IPC socket ready!${NC}"
        break
    fi
    sleep 1
done

if [ ! -S "/tmp/fight-club.sock" ]; then
    echo -e "${RED}Error: IPC socket not created after 10 seconds${NC}"
    cleanup
    exit 1
fi

# Wait a moment for server to fully initialize
sleep 2

# Start streamer in background
echo -e "${GREEN}Starting streamer...${NC}"
./bin/streamer &
STREAMER_PID=$!
echo -e "Streamer PID: $STREAMER_PID"

echo ""
echo -e "${GREEN}======================================${NC}"
echo -e "${GREEN}  Both processes running!${NC}"
echo -e "${GREEN}======================================${NC}"
echo ""
echo -e "Server PID:   $SERVER_PID"
echo -e "Streamer PID: $STREAMER_PID"
echo -e "IPC Socket:   /tmp/fight-club.sock"
echo ""
echo -e "${YELLOW}Press Ctrl+C to stop both processes${NC}"
echo ""

# Wait for either process to exit
wait $SERVER_PID $STREAMER_PID
