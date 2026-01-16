#!/bin/bash
# =============================================================================
# Fight Club - Stream Diagnostics
# =============================================================================
# Quick diagnostic check of the running stream
# Usage: ./stream_diagnostics.sh
# =============================================================================

CONTAINER_NAME="fight-club"

echo "=============================================="
echo "  Fight Club Stream Diagnostics"
echo "=============================================="
echo ""

# 1. Container Status
echo "📦 Container Status:"
if docker ps --format '{{.Names}}' | grep -q "^${CONTAINER_NAME}$"; then
    echo "   ✅ Container is running"

    # Get container stats
    STATS=$(docker stats --no-stream --format "{{.CPUPerc}}\t{{.MemUsage}}" ${CONTAINER_NAME} 2>/dev/null)
    CPU=$(echo "$STATS" | cut -f1)
    MEM=$(echo "$STATS" | cut -f2)
    echo "   CPU: ${CPU}"
    echo "   Memory: ${MEM}"
else
    echo "   ❌ Container is NOT running"
    echo ""
    echo "   Start with: docker-compose up -d"
    exit 1
fi
echo ""

# 2. FFmpeg Process
echo "🎥 FFmpeg Process:"
FFMPEG_PID=$(docker exec ${CONTAINER_NAME} pgrep ffmpeg 2>/dev/null || echo "")
if [ -n "$FFMPEG_PID" ]; then
    echo "   ✅ FFmpeg is running (PID: ${FFMPEG_PID})"
else
    echo "   ❌ FFmpeg is NOT running"
    echo "   Stream may not be active"
fi
echo ""

# 3. Recent FFmpeg Speed
echo "⚡ Recent FFmpeg Speed (last 10 samples):"
docker logs --tail 100 ${CONTAINER_NAME} 2>&1 | \
    grep -oP 'speed=\s*[0-9.]+x' | \
    tail -10 | \
    while read line; do
        speed=$(echo "$line" | grep -oP '[0-9.]+')
        if (( $(echo "$speed >= 1.0" | bc -l) )); then
            echo "   ✅ $line"
        elif (( $(echo "$speed >= 0.9" | bc -l) )); then
            echo "   ⚠️ $line"
        else
            echo "   ❌ $line"
        fi
    done
echo ""

# 4. Frame Rate
echo "📊 Frame Rate Check:"
FPS_LINE=$(docker logs --tail 50 ${CONTAINER_NAME} 2>&1 | grep -oP 'fps=\s*[0-9.]+' | tail -1)
if [ -n "$FPS_LINE" ]; then
    FPS=$(echo "$FPS_LINE" | grep -oP '[0-9.]+')
    echo "   Current: ${FPS} fps"
    if (( $(echo "$FPS >= 23" | bc -l) )); then
        echo "   ✅ FPS is good (target: 24)"
    else
        echo "   ⚠️ FPS is low (target: 24)"
    fi
else
    echo "   ⚠️ Could not determine FPS"
fi
echo ""

# 5. Bitrate
echo "📡 Bitrate Check:"
BITRATE_LINE=$(docker logs --tail 50 ${CONTAINER_NAME} 2>&1 | grep -oP 'bitrate=\s*[0-9.]+kbits/s' | tail -1)
if [ -n "$BITRATE_LINE" ]; then
    BITRATE=$(echo "$BITRATE_LINE" | grep -oP '[0-9.]+')
    echo "   Current: ${BITRATE} kbits/s"
    echo "   Target: 4000 kbits/s"
else
    echo "   ⚠️ Could not determine bitrate"
fi
echo ""

# 6. Error Check
echo "🔍 Recent Errors:"
ERRORS=$(docker logs --tail 200 ${CONTAINER_NAME} 2>&1 | grep -iE "(error|fail|panic)" | tail -5)
if [ -n "$ERRORS" ]; then
    echo "$ERRORS" | while read line; do
        echo "   ❌ $line"
    done
else
    echo "   ✅ No recent errors"
fi
echo ""

# 7. Uptime
echo "⏱️ Container Uptime:"
UPTIME=$(docker ps --format '{{.Status}}' --filter "name=${CONTAINER_NAME}")
echo "   $UPTIME"
echo ""

echo "=============================================="
echo "  Quick Commands:"
echo "=============================================="
echo ""
echo "  Monitor speed:     ./scripts/monitor_stream.sh 30"
echo "  View live logs:    docker logs -f ${CONTAINER_NAME}"
echo "  Restart stream:    docker-compose restart"
echo "  Check resources:   docker stats ${CONTAINER_NAME}"
echo ""
