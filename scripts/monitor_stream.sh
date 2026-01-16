#!/bin/bash
# =============================================================================
# Fight Club - Real Stream Performance Monitor
# =============================================================================
# Monitors FFmpeg output from the running Docker container and reports speed
# Usage: ./monitor_stream.sh [duration_seconds]
# =============================================================================

set -e

DURATION=${1:-30}  # Default 30 seconds
CONTAINER_NAME="fight-club"

echo "=============================================="
echo "  Fight Club Stream Performance Monitor"
echo "=============================================="
echo ""

# Check if container is running
if ! docker ps --format '{{.Names}}' | grep -q "^${CONTAINER_NAME}$"; then
    echo "❌ Error: Container '${CONTAINER_NAME}' is not running"
    echo "   Start the stream first with: docker-compose up -d"
    exit 1
fi

echo "📊 Monitoring FFmpeg speed for ${DURATION} seconds..."
echo "   Container: ${CONTAINER_NAME}"
echo ""

# Temp file for speed samples
SPEED_FILE=$(mktemp)
trap "rm -f $SPEED_FILE" EXIT

# Capture FFmpeg output and extract speed values
timeout ${DURATION} docker logs -f ${CONTAINER_NAME} 2>&1 | \
    grep --line-buffered -oP 'speed=\s*\K[0-9.]+(?=x)' | \
    while read speed; do
        echo "$speed" >> "$SPEED_FILE"
        # Show progress
        if [ $(wc -l < "$SPEED_FILE") -eq 1 ]; then
            echo -n "   Samples: "
        fi
        count=$(wc -l < "$SPEED_FILE")
        if [ $((count % 5)) -eq 0 ]; then
            echo -n "${count}.. "
        fi
    done || true

echo ""
echo ""

# Analyze results
if [ ! -s "$SPEED_FILE" ]; then
    echo "❌ No FFmpeg speed data captured!"
    echo "   Make sure the stream is actively running."
    echo ""
    echo "   Check container logs with: docker logs ${CONTAINER_NAME}"
    exit 1
fi

# Calculate statistics
SAMPLES=$(wc -l < "$SPEED_FILE")
MIN_SPEED=$(sort -n "$SPEED_FILE" | head -1)
MAX_SPEED=$(sort -n "$SPEED_FILE" | tail -1)
AVG_SPEED=$(awk '{sum+=$1} END {printf "%.2f", sum/NR}' "$SPEED_FILE")
SLOW_COUNT=$(awk '$1 < 1.0 {count++} END {print count+0}' "$SPEED_FILE")
CRITICAL_COUNT=$(awk '$1 < 0.8 {count++} END {print count+0}' "$SPEED_FILE")

# Calculate percentages
SLOW_PCT=$(awk "BEGIN {printf \"%.1f\", ($SLOW_COUNT / $SAMPLES) * 100}")
CRITICAL_PCT=$(awk "BEGIN {printf \"%.1f\", ($CRITICAL_COUNT / $SAMPLES) * 100}")

echo "=============================================="
echo "  RESULTS"
echo "=============================================="
echo ""
echo "  Duration:        ${DURATION}s"
echo "  Samples:         ${SAMPLES}"
echo ""
echo "  Min Speed:       ${MIN_SPEED}x"
echo "  Max Speed:       ${MAX_SPEED}x"
echo "  Avg Speed:       ${AVG_SPEED}x"
echo ""
echo "  Slow (<1.0x):    ${SLOW_COUNT} (${SLOW_PCT}%)"
echo "  Critical (<0.8x): ${CRITICAL_COUNT} (${CRITICAL_PCT}%)"
echo ""

# Pass/Fail determination
PASS=true
ISSUES=""

# Check average speed
if (( $(echo "$AVG_SPEED < 0.95" | bc -l) )); then
    PASS=false
    ISSUES="${ISSUES}\n  ❌ Average speed ${AVG_SPEED}x < 0.95x (too slow)"
fi

# Check minimum speed
if (( $(echo "$MIN_SPEED < 0.8" | bc -l) )); then
    PASS=false
    ISSUES="${ISSUES}\n  ❌ Minimum speed ${MIN_SPEED}x < 0.8x (critical drops)"
fi

# Check slow percentage
if (( $(echo "$SLOW_PCT > 10" | bc -l) )); then
    PASS=false
    ISSUES="${ISSUES}\n  ❌ Too many slow samples: ${SLOW_PCT}% > 10%"
fi

echo "=============================================="
if [ "$PASS" = true ]; then
    echo "  ✅ PASS - Stream is running smoothly!"
else
    echo "  ❌ FAIL - Performance issues detected:"
    echo -e "$ISSUES"
fi
echo "=============================================="
echo ""

# Show speed distribution
echo "Speed Distribution:"
echo "  >= 1.0x (good):     $(awk '$1 >= 1.0 {count++} END {print count+0}' "$SPEED_FILE")"
echo "  0.9-1.0x (ok):      $(awk '$1 >= 0.9 && $1 < 1.0 {count++} END {print count+0}' "$SPEED_FILE")"
echo "  0.8-0.9x (slow):    $(awk '$1 >= 0.8 && $1 < 0.9 {count++} END {print count+0}' "$SPEED_FILE")"
echo "  < 0.8x (critical):  ${CRITICAL_COUNT}"
echo ""

# Exit with appropriate code
if [ "$PASS" = true ]; then
    exit 0
else
    exit 1
fi
