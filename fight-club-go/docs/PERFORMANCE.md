# Fight Club Performance Testing Guide

## Overview

This document describes the performance testing strategy, KPIs (Key Performance Indicators),
and thresholds used to ensure the game stream maintains smooth 24 FPS rendering on VPS hardware.

## Target Environment

- **Platform**: Contabo VPS (ARM64)
- **CPU**: Limited to ~150-200% utilization across containers
- **Target FPS**: 24 frames per second
- **Target Frame Time**: 41.67ms per frame

## Performance KPIs

### 1. Engine Tick Time

The game engine tick is the core simulation loop that updates all player positions,
resolves collisions, and processes combat.

| Player Count | Max Acceptable | Target | Critical |
|--------------|----------------|--------|----------|
| 10 players   | 500µs          | 250µs  | 1ms      |
| 50 players   | 2ms            | 1ms    | 4ms      |
| 100 players  | 5ms            | 2.5ms  | 10ms     |
| 200 players  | 15ms           | 8ms    | 25ms     |

**Why it matters**: If tick time exceeds frame time (41.67ms), the game will skip frames
and appear laggy.

### 2. Snapshot Generation

Snapshot generation creates an immutable copy of game state for the renderer.

| Player Count | Max Acceptable | Target |
|--------------|----------------|--------|
| 50 players   | 500µs          | 250µs  |
| 100 players  | 1ms            | 500µs  |
| 200 players  | 2ms            | 1ms    |

**Why it matters**: Snapshots are generated every tick. High snapshot time adds directly
to the tick-to-render latency.

### 3. Memory Allocations

Go's garbage collector can cause latency spikes. Minimizing allocations per tick
ensures smooth frame timing.

| Operation        | Max Allocs/op | Max Bytes/op |
|------------------|---------------|--------------|
| Engine Tick      | 50            | 5KB          |
| Snapshot         | 20            | 10KB         |
| Spatial Query    | 5             | 1KB          |

**Why it matters**: Each allocation is potential GC work. Too many allocations cause
GC pauses that appear as frame stutters.

### 4. Stress Test Metrics

| Metric              | Threshold        | Description                          |
|---------------------|------------------|--------------------------------------|
| Sustained TPS       | ≥ 21.6 (90%)     | Ticks per second under load          |
| P99 Latency         | ≤ 50ms           | 99th percentile tick time            |
| Spike Recovery      | < 100ms          | Max tick time during player spike    |
| Memory Stability    | < 50 players     | Final player count after churn test  |

## Running Tests

### Quick Benchmark (Development)

```bash
cd fight-club-go

# Run all benchmarks with memory stats
go test -bench=. -benchmem ./internal/game/...

# Run specific benchmark with more iterations
go test -bench=BenchmarkEngineTick_50Players -benchtime=5s -count=5 ./internal/game/...
```

### Full Benchmark Suite (Pre-deploy)

```bash
# Run with multiple iterations for statistical significance
go test -bench=. -benchmem -benchtime=3s -count=3 ./internal/game/...
```

### Stress Tests

```bash
# Run all stress tests (requires ~2 minutes)
go test -v -run=TestStress -timeout=120s ./internal/game/...

# Run specific stress test
go test -v -run=TestStress_SustainedLoad -timeout=60s ./internal/game/...
go test -v -run=TestStress_SpikeLoad -timeout=60s ./internal/game/...
go test -v -run=TestStress_ConcurrentCommands -timeout=60s ./internal/game/...
go test -v -run=TestStress_MemoryPressure -timeout=60s ./internal/game/...
```

### Latency Tests

```bash
go test -v -run=TestLatency -timeout=60s ./internal/game/...
```

### Memory Profiling

```bash
# Generate memory profile
go test -bench=BenchmarkMemoryAllocation -benchmem -memprofile=mem.prof ./internal/game/...

# Analyze profile
go tool pprof mem.prof
```

### CPU Profiling

```bash
# Generate CPU profile
go test -bench=BenchmarkEngineTick_100Players -cpuprofile=cpu.prof -benchtime=10s ./internal/game/...

# Analyze profile
go tool pprof cpu.prof
```

## CI/CD Integration

Performance tests run automatically on:
- Pull requests to main/master (paths: `fight-club-go/**/*.go`)
- Pushes to main/master

### Pipeline Jobs

1. **Benchmark Tests**: Runs all benchmarks, checks against thresholds
2. **Stress Tests**: Validates stability under sustained and spike loads
3. **Memory Analysis**: Checks for memory leaks and excessive allocations
4. **Regression Check**: Compares PR performance against base branch

### Failing the Build

The build will fail if:
- Any benchmark exceeds its threshold
- Stress tests detect instability (TPS < 90% of target)
- Memory pressure test shows leaks (player count > 50 after cleanup)
- Latency tests show average > 2 frames (83ms at 24 FPS)

## Known Bottlenecks and Optimizations

### Current Optimizations

1. **Spatial Grid**: O(1) collision lookups instead of O(n²)
2. **Sweep and Prune**: Efficient broad-phase collision detection
3. **Flow Fields**: Pre-computed pathfinding, reused across players
4. **Triple-buffered Snapshots**: Lock-free producer/consumer
5. **Pre-allocated Slices**: Avoid GC pressure during ticks
6. **Async Frame Writer**: Decouples render loop from FFmpeg pipe

### Areas for Future Optimization

1. **Focus Target Lookup**: Currently O(n) scan, could be O(1) with hash map
2. **SAP Active Set**: Linear removal, could use indexed data structure
3. **Constellation Background**: Re-renders every frame, could be cached
4. **Player Slice Reuse**: Snapshot allocates new player array

## Interpreting Results

### Benchmark Output

```
BenchmarkEngineTick_50Players-4    5000    234567 ns/op    4096 B/op    42 allocs/op
```

- `5000`: Number of iterations run
- `234567 ns/op`: Nanoseconds per operation (234µs)
- `4096 B/op`: Bytes allocated per operation
- `42 allocs/op`: Number of allocations per operation

### Stress Test Output

```
Stress Test Results:
  Duration: 5.003s
  Total Ticks: 120
  Avg Tick Time: 1.234ms
  Max Tick Time: 4.567ms
  TPS: 23.98
  Commands Handled: 250
  Peak Players: 45
```

- **TPS**: Should be ≥ 21.6 (90% of 24 FPS)
- **Avg Tick Time**: Should be well under 41.67ms
- **Max Tick Time**: Spikes should stay under 50ms for smooth playback

## Troubleshooting Performance Issues

### Stream Lagging

1. Check FFmpeg encoding speed: `docker logs fight-club 2>&1 | grep speed`
   - Should show `speed=1.0x` or higher
   - If < 1.0x, encoding is bottleneck

2. Check CPU usage: `docker stats`
   - fight-club should be < 150%
   - ffmpeg should be < 100%

3. Run benchmarks to identify slow operations:
   ```bash
   go test -bench=. -benchmem ./internal/game/... | sort -t'/' -k2 -n
   ```

### Memory Growth

1. Run memory pressure test:
   ```bash
   go test -v -run=TestStress_MemoryPressure ./internal/game/...
   ```

2. Profile allocations:
   ```bash
   go test -bench=BenchmarkEngineTick_50Players -memprofile=mem.prof ./internal/game/...
   go tool pprof -top mem.prof
   ```

### Frame Stutters

Frame stutters are usually caused by GC pauses. Check allocation rate:

```bash
GODEBUG=gctrace=1 go test -bench=BenchmarkEngineTick_50Players -benchtime=30s ./internal/game/...
```

Look for GC pauses > 5ms. If found, reduce allocations in hot paths.
