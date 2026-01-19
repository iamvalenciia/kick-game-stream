package streaming

import (
	"fight-club/internal/game"
	"fight-club/internal/ipc"
	"sync/atomic"
)

// SnapshotSource is an interface for getting game snapshots
// This allows the StreamManager to work with either a local engine or IPC
type SnapshotSource interface {
	GetSnapshot() *game.GameSnapshot
}

// LocalEngineSource wraps a local game.Engine as a SnapshotSource
type LocalEngineSource struct {
	engine *game.Engine
}

// NewLocalEngineSource creates a SnapshotSource from a local engine
func NewLocalEngineSource(engine *game.Engine) *LocalEngineSource {
	return &LocalEngineSource{engine: engine}
}

// GetSnapshot returns the latest snapshot from the local engine
func (s *LocalEngineSource) GetSnapshot() *game.GameSnapshot {
	return s.engine.GetSnapshot()
}

// IPCSnapshotSource wraps an IPC subscriber as a SnapshotSource
type IPCSnapshotSource struct {
	subscriber *ipc.Subscriber

	// Cached conversion to avoid allocations
	lastSnapshot atomic.Value // *game.GameSnapshot
	lastSequence uint64
}

// NewIPCSnapshotSource creates a SnapshotSource from an IPC subscriber
func NewIPCSnapshotSource(subscriber *ipc.Subscriber) *IPCSnapshotSource {
	source := &IPCSnapshotSource{
		subscriber: subscriber,
	}

	// Set up callback to convert snapshots as they arrive
	subscriber.OnSnapshot(func(msg *ipc.SnapshotMessage) {
		// Convert to GameSnapshot and cache
		snap := msg.ToGameSnapshot()
		source.lastSnapshot.Store(snap)
		source.lastSequence = msg.Sequence
	})

	return source
}

// GetSnapshot returns the latest snapshot from IPC
func (s *IPCSnapshotSource) GetSnapshot() *game.GameSnapshot {
	if val := s.lastSnapshot.Load(); val != nil {
		return val.(*game.GameSnapshot)
	}
	return nil
}

// GetSequence returns the last received sequence number
func (s *IPCSnapshotSource) GetSequence() uint64 {
	return s.lastSequence
}
