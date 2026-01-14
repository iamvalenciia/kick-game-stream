package chat

import "time"

// ChatMessage represents a raw message from Kick chat
type ChatMessage struct {
	ID         string
	ChatroomID int64
	Content    string
	Username   string
	UserID     int64
	ProfilePic string
	Timestamp  time.Time
}

// ChatCommand represents a parsed game command
type ChatCommand struct {
	Command    string   // "join", "heal", "buy", etc.
	Args       []string // Arguments after command
	Username   string
	UserID     int64
	ProfilePic string
	ReceivedAt time.Time
}

// CommandType for routing
type CommandType int

const (
	CmdJoin CommandType = iota
	CmdHeal
	CmdBuy
	CmdStats
	CmdShop
	CmdHelp
	CmdFocus // !focus <username>
	CmdTeam  // !team <subcommand>
	CmdUnknown
)

// SupportedCommands maps command strings to types
var SupportedCommands = map[string]CommandType{
	// Join variants
	"join":   CmdJoin,
	"unirse": CmdJoin,
	"entrar": CmdJoin,

	// Heal variants
	"heal":  CmdHeal,
	"vida":  CmdHeal,
	"curar": CmdHeal,

	// Buy variants
	"buy":     CmdBuy,
	"comprar": CmdBuy,

	// Stats variants
	"stats": CmdStats,
	"score": CmdStats,

	// Shop variants
	"shop":   CmdShop,
	"tienda": CmdShop,

	// Help variants
	"help":     CmdHelp,
	"ayuda":    CmdHelp,
	"commands": CmdHelp,
	"comandos": CmdHelp,

	// Focus variants
	"focus":    CmdFocus,
	"enfocar":  CmdFocus,
	"objetivo": CmdFocus,

	// Team variants
	"team":   CmdTeam,
	"equipo": CmdTeam,
}

// WeaponAliases maps weapon names to canonical IDs
var WeaponAliases = map[string]string{
	"sword":    "sword",
	"espada":   "sword",
	"spear":    "spear",
	"lanza":    "spear",
	"axe":      "axe",
	"hacha":    "axe",
	"bow":      "bow",
	"arco":     "bow",
	"scythe":   "scythe",
	"guadana":  "scythe",
	"hammer":   "hammer",
	"martillo": "hammer",
}

// GetCommandType returns the command type for a string (case-insensitive)
func GetCommandType(cmd string) CommandType {
	if t, ok := SupportedCommands[cmd]; ok {
		return t
	}
	return CmdUnknown
}

// GetWeaponID normalizes weapon name to canonical ID
func GetWeaponID(name string) (string, bool) {
	if id, ok := WeaponAliases[name]; ok {
		return id, true
	}
	return "", false
}
