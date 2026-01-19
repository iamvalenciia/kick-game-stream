// Package ipc provides inter-process communication between server and streamer
// Uses Unix domain sockets for low-latency, zero-copy communication
package ipc

import (
	"encoding/binary"
	"encoding/gob"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"
)

const (
	// DefaultSocketPath is the Unix socket path for IPC
	DefaultSocketPath = "/tmp/fight-club.sock"

	// Message types
	MsgTypeSnapshot byte = 0x01
	MsgTypePing     byte = 0x02
	MsgTypePong     byte = 0x03
	MsgTypeConfig   byte = 0x04

	// Protocol version for compatibility checking
	ProtocolVersion uint16 = 1

	// Connection settings
	MaxMessageSize = 1024 * 1024 // 1MB max message
	WriteTimeout   = 50 * time.Millisecond
	ReadTimeout    = 100 * time.Millisecond
	ReconnectDelay = 500 * time.Millisecond
	MaxReconnects  = 20
)

// SnapshotMessage wraps a game snapshot for IPC transmission
type SnapshotMessage struct {
	Sequence   uint64
	Timestamp  int64 // Unix nano
	TickNumber uint64

	// Player data
	Players []PlayerData

	// Visual effects
	Particles   []ParticleData
	Effects     []EffectData
	Texts       []TextData
	Trails      []TrailData
	Flashes     []FlashData
	Projectiles []ProjectileData

	// Screen shake
	ShakeOffsetX   float64
	ShakeOffsetY   float64
	ShakeIntensity float64

	// Aggregate stats
	PlayerCount int
	AliveCount  int
	TotalKills  int
}

// PlayerData is the IPC representation of a player
type PlayerData struct {
	ID              string
	Name            string
	X, Y            float64
	VX, VY          float64
	HP, MaxHP       int
	Money           int
	Kills           int
	Deaths          int
	Weapon          string
	Color           string
	Avatar          string
	AttackAngle     float64
	IsDead          bool
	IsRagdoll       bool
	RagdollRotation float64
	SpawnProtection bool
	IsAttacking     bool
	ProfilePic      string
	IsDodging       bool
	DodgeDirection  float64
	ComboCount      int
	Stamina         float64
}

// ParticleData is the IPC representation of a particle
type ParticleData struct {
	X, Y  float64
	Color string
	Alpha float64
}

// EffectData is the IPC representation of an attack effect
type EffectData struct {
	X, Y   float64
	TX, TY float64
	Color  string
	Timer  int
}

// TextData is the IPC representation of floating text
type TextData struct {
	X, Y  float64
	Text  string
	Color string
	Alpha float64
}

// TrailData is the IPC representation of a weapon trail
type TrailData struct {
	Points   [8]TrailPointData
	Count    int
	Color    string
	Alpha    float64
	PlayerID string
}

// TrailPointData is a single point in a trail
type TrailPointData struct {
	X, Y  float64
	Alpha float64
}

// FlashData is the IPC representation of an impact flash
type FlashData struct {
	X, Y      float64
	Radius    float64
	Color     string
	Intensity float64
}

// ProjectileData is the IPC representation of a projectile
type ProjectileData struct {
	X, Y       float64
	Rotation   float64
	Color      string
	TrailX     [4]float64
	TrailY     [4]float64
	TrailCount int
}

// ConfigMessage contains streaming configuration
type ConfigMessage struct {
	Width   int
	Height  int
	FPS     int
	Bitrate int
}

// Header is the message header for framing
type Header struct {
	Version  uint16
	Type     byte
	Reserved byte
	Length   uint32
}

const HeaderSize = 8 // 2 + 1 + 1 + 4

// WriteMessage writes a framed message to the connection
func WriteMessage(w io.Writer, msgType byte, data interface{}) error {
	// Encode data to buffer
	var buf []byte
	if data != nil {
		// Use gob encoding for complex types
		var gobBuf = getBuffer()
		defer putBuffer(gobBuf)

		enc := gob.NewEncoder(gobBuf)
		if err := enc.Encode(data); err != nil {
			return fmt.Errorf("gob encode: %w", err)
		}
		buf = gobBuf.Bytes()
	}

	if len(buf) > MaxMessageSize {
		return fmt.Errorf("message too large: %d > %d", len(buf), MaxMessageSize)
	}

	// Write header
	header := Header{
		Version: ProtocolVersion,
		Type:    msgType,
		Length:  uint32(len(buf)),
	}

	headerBuf := make([]byte, HeaderSize)
	binary.LittleEndian.PutUint16(headerBuf[0:2], header.Version)
	headerBuf[2] = header.Type
	headerBuf[3] = header.Reserved
	binary.LittleEndian.PutUint32(headerBuf[4:8], header.Length)

	if _, err := w.Write(headerBuf); err != nil {
		return fmt.Errorf("write header: %w", err)
	}

	// Write body
	if len(buf) > 0 {
		if _, err := w.Write(buf); err != nil {
			return fmt.Errorf("write body: %w", err)
		}
	}

	return nil
}

// ReadMessage reads a framed message from the connection
func ReadMessage(r io.Reader) (byte, []byte, error) {
	// Read header
	headerBuf := make([]byte, HeaderSize)
	if _, err := io.ReadFull(r, headerBuf); err != nil {
		return 0, nil, fmt.Errorf("read header: %w", err)
	}

	header := Header{
		Version: binary.LittleEndian.Uint16(headerBuf[0:2]),
		Type:    headerBuf[2],
		Length:  binary.LittleEndian.Uint32(headerBuf[4:8]),
	}

	if header.Version != ProtocolVersion {
		return 0, nil, fmt.Errorf("version mismatch: got %d, want %d", header.Version, ProtocolVersion)
	}

	if header.Length > MaxMessageSize {
		return 0, nil, fmt.Errorf("message too large: %d > %d", header.Length, MaxMessageSize)
	}

	// Read body
	var body []byte
	if header.Length > 0 {
		body = make([]byte, header.Length)
		if _, err := io.ReadFull(r, body); err != nil {
			return 0, nil, fmt.Errorf("read body: %w", err)
		}
	}

	return header.Type, body, nil
}

// DecodeSnapshot decodes a snapshot from gob bytes
func DecodeSnapshot(data []byte) (*SnapshotMessage, error) {
	var buf = getBytesBuffer(data)
	defer putBytesBuffer(buf)

	dec := gob.NewDecoder(buf)
	var msg SnapshotMessage
	if err := dec.Decode(&msg); err != nil {
		return nil, fmt.Errorf("gob decode snapshot: %w", err)
	}
	return &msg, nil
}

// DecodeConfig decodes a config from gob bytes
func DecodeConfig(data []byte) (*ConfigMessage, error) {
	var buf = getBytesBuffer(data)
	defer putBytesBuffer(buf)

	dec := gob.NewDecoder(buf)
	var msg ConfigMessage
	if err := dec.Decode(&msg); err != nil {
		return nil, fmt.Errorf("gob decode config: %w", err)
	}
	return &msg, nil
}

// CleanupSocket removes the socket file if it exists
func CleanupSocket(path string) error {
	if _, err := os.Stat(path); err == nil {
		return os.Remove(path)
	}
	return nil
}

// CreateListener creates a Unix domain socket listener
func CreateListener(path string) (net.Listener, error) {
	// Clean up existing socket
	if err := CleanupSocket(path); err != nil {
		return nil, fmt.Errorf("cleanup socket: %w", err)
	}

	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("listen unix: %w", err)
	}

	// Set socket permissions
	if err := os.Chmod(path, 0666); err != nil {
		listener.Close()
		return nil, fmt.Errorf("chmod socket: %w", err)
	}

	return listener, nil
}

// Connect connects to the IPC socket with retries
func Connect(path string) (net.Conn, error) {
	var lastErr error
	for i := 0; i < MaxReconnects; i++ {
		conn, err := net.DialTimeout("unix", path, time.Second)
		if err == nil {
			return conn, nil
		}
		lastErr = err
		time.Sleep(ReconnectDelay)
	}
	return nil, fmt.Errorf("connect failed after %d attempts: %w", MaxReconnects, lastErr)
}

// Buffer pool for encoding
var bufferPool = sync.Pool{
	New: func() interface{} {
		return new(gobBuffer)
	},
}

type gobBuffer struct {
	buf []byte
}

func (b *gobBuffer) Write(p []byte) (n int, err error) {
	b.buf = append(b.buf, p...)
	return len(p), nil
}

func (b *gobBuffer) Bytes() []byte {
	return b.buf
}

func (b *gobBuffer) Reset() {
	b.buf = b.buf[:0]
}

func getBuffer() *gobBuffer {
	buf := bufferPool.Get().(*gobBuffer)
	buf.Reset()
	return buf
}

func putBuffer(buf *gobBuffer) {
	bufferPool.Put(buf)
}

// Bytes buffer pool for decoding
var bytesBufferPool = sync.Pool{
	New: func() interface{} {
		return &bytesReader{}
	},
}

type bytesReader struct {
	data []byte
	pos  int
}

func (b *bytesReader) Read(p []byte) (n int, err error) {
	if b.pos >= len(b.data) {
		return 0, io.EOF
	}
	n = copy(p, b.data[b.pos:])
	b.pos += n
	return n, nil
}

func (b *bytesReader) Reset(data []byte) {
	b.data = data
	b.pos = 0
}

func getBytesBuffer(data []byte) *bytesReader {
	buf := bytesBufferPool.Get().(*bytesReader)
	buf.Reset(data)
	return buf
}

func putBytesBuffer(buf *bytesReader) {
	bytesBufferPool.Put(buf)
}
