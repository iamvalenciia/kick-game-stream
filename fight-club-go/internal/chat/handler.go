package chat

import (
	"log"
	"strings"

	"fight-club/internal/game"
)

// Handler processes chat commands and applies them to the game
type Handler struct {
	engine      *game.Engine
	rateLimiter *RateLimiter
}

// NewHandler creates a new command handler
func NewHandler(engine *game.Engine) *Handler {
	return &Handler{
		engine:      engine,
		rateLimiter: NewRateLimiter(DefaultRateLimitConfig),
	}
}

// ProcessCommand handles a single command
func (h *Handler) ProcessCommand(cmd ChatCommand) {
	// Rate limit check
	if !h.rateLimiter.Allow(cmd.Username) {
		log.Printf("🚫 Rate limited: %s", cmd.Username)
		return
	}

	cmdType := GetCommandType(cmd.Command)

	switch cmdType {
	case CmdJoin:
		h.handleJoin(cmd)
	case CmdHeal:
		h.handleHeal(cmd)
	case CmdBuy:
		h.handleBuy(cmd)
	case CmdStats:
		h.handleStats(cmd)
	case CmdShop:
		h.handleShop(cmd)
	case CmdHelp:
		h.handleHelp(cmd)
	case CmdFocus:
		h.handleFocus(cmd)
	case CmdTeam:
		h.handleTeam(cmd)
	default:
		// Check if it's a direct weapon command (e.g., !sword)
		if weaponID, ok := GetWeaponID(cmd.Command); ok {
			h.handleBuyWeapon(cmd, weaponID)
		}
		// Unknown command - silently ignore
	}
}

// ProcessChatMessage handles non-command chat messages for chat bubbles
func (h *Handler) ProcessChatMessage(username, message string) {
	h.engine.SetChatBubble(username, message)
}

// handleJoin adds a player to the game
func (h *Handler) handleJoin(cmd ChatCommand) {
	opts := game.PlayerOptions{
		ProfilePic: cmd.ProfilePic,
	}

	player := h.engine.AddPlayer(cmd.Username, opts)
	if player == nil {
		log.Printf("⚠️ Failed to add player: %s (limit reached?)", cmd.Username)
		return
	}

	// Check player state for respawn vs new join
	if player.IsDead || player.State == game.StateDead {
		player.Respawn()
		log.Printf("🔄 %s respawned!", cmd.Username)
	} else {
		log.Printf("⚔️ %s joined the arena!", cmd.Username)
	}
}

// handleHeal heals the player (costs money)
func (h *Handler) handleHeal(cmd ChatCommand) {
	player := h.engine.GetPlayer(cmd.Username)
	if player == nil {
		log.Printf("⚠️ %s not in game (tried !heal)", cmd.Username)
		return
	}

	if player.IsDead {
		log.Printf("⚠️ %s is dead (tried !heal)", cmd.Username)
		return
	}

	const healCost = 20 // Changed from 50 to 20 per user request
	const healAmount = 20

	if player.Money < healCost {
		log.Printf("💰 %s needs $%d to heal (has $%d)", cmd.Username, healCost, player.Money)
		return
	}

	if player.HP >= player.MaxHP {
		log.Printf("💚 %s already at full HP", cmd.Username)
		return
	}

	// Charge and heal
	player.Money -= healCost
	healed := h.engine.HealPlayer(cmd.Username, healAmount)
	if healed {
		log.Printf("💚 %s healed for %d HP (cost: $%d)", cmd.Username, healAmount, healCost)
	}
}

// handleBuy handles weapon purchase
func (h *Handler) handleBuy(cmd ChatCommand) {
	if len(cmd.Args) == 0 {
		log.Printf("ℹ️ %s: Usage: !buy <weapon>", cmd.Username)
		return
	}

	weaponName := strings.ToLower(cmd.Args[0])
	weaponID, ok := GetWeaponID(weaponName)
	if !ok {
		log.Printf("⚠️ %s: Unknown weapon '%s'", cmd.Username, weaponName)
		return
	}

	h.handleBuyWeapon(cmd, weaponID)
}

// handleBuyWeapon processes weapon purchase
func (h *Handler) handleBuyWeapon(cmd ChatCommand, weaponID string) {
	player := h.engine.GetPlayer(cmd.Username)
	if player == nil {
		log.Printf("⚠️ %s not in game (tried !buy)", cmd.Username)
		return
	}

	if player.IsDead {
		log.Printf("⚠️ %s is dead (tried !buy)", cmd.Username)
		return
	}

	// Get weapon info
	weapon := game.GetWeapon(weaponID)
	if weapon.ID == "" {
		log.Printf("⚠️ Invalid weapon ID: %s", weaponID)
		return
	}

	// Check if already has this weapon
	if player.Weapon == weaponID {
		log.Printf("🗡️ %s already has %s", cmd.Username, weapon.Name)
		return
	}

	// Check money
	if player.Money < weapon.Price {
		log.Printf("💰 %s needs $%d for %s (has $%d)", cmd.Username, weapon.Price, weapon.Name, player.Money)
		return
	}

	// Purchase
	player.Money -= weapon.Price
	player.Weapon = weaponID
	log.Printf("🗡️ %s bought %s for $%d!", cmd.Username, weapon.Name, weapon.Price)
}

// handleStats shows player stats
func (h *Handler) handleStats(cmd ChatCommand) {
	targetName := cmd.Username
	if len(cmd.Args) > 0 {
		targetName = cmd.Args[0]
	}

	player := h.engine.GetPlayer(targetName)
	if player == nil {
		log.Printf("ℹ️ %s not found", targetName)
		return
	}

	weapon := game.GetWeapon(player.Weapon)
	teamInfo := ""
	if player.TeamID != "" {
		team := h.engine.GetTeamManager().GetTeam(player.TeamID)
		if team != nil {
			teamInfo = " | Team: " + team.Name
		}
	}
	log.Printf("📊 %s: HP %d/%d | $%d | K:%d D:%d | %s%s",
		player.Name, player.HP, player.MaxHP, player.Money,
		player.Kills, player.Deaths, weapon.Name, teamInfo)
}

// handleShop shows available weapons
func (h *Handler) handleShop(cmd ChatCommand) {
	log.Printf("🏪 Shop: sword $100 | spear $200 | axe $300 | bow $400 | scythe $500")
}

// handleHelp shows available commands
func (h *Handler) handleHelp(cmd ChatCommand) {
	log.Printf("📜 Commands: !join | !heal ($20) | !buy <weapon> | !stats | !shop | !focus <user> | !team <cmd>")
}

// handleFocus sets a combat focus target
func (h *Handler) handleFocus(cmd ChatCommand) {
	player := h.engine.GetPlayer(cmd.Username)
	if player == nil {
		log.Printf("⚠️ %s not in game (tried !focus)", cmd.Username)
		return
	}

	if player.IsDead {
		log.Printf("⚠️ %s is dead (tried !focus)", cmd.Username)
		return
	}

	if len(cmd.Args) == 0 {
		// Clear focus
		h.engine.ClearFocus(cmd.Username)
		log.Printf("🎯 %s cleared focus target", cmd.Username)
		return
	}

	targetName := cmd.Args[0]

	// Can't focus yourself
	if targetName == cmd.Username {
		log.Printf("⚠️ %s tried to focus themselves", cmd.Username)
		return
	}

	if h.engine.SetFocus(cmd.Username, targetName, 60.0) { // 60 second focus duration
		log.Printf("🎯 %s is now focusing %s", cmd.Username, targetName)
	} else {
		log.Printf("⚠️ %s: Cannot focus %s (not found or teammate)", cmd.Username, targetName)
	}
}

// handleTeam handles team commands
func (h *Handler) handleTeam(cmd ChatCommand) {
	player := h.engine.GetPlayer(cmd.Username)
	if player == nil {
		log.Printf("⚠️ %s not in game (tried !team)", cmd.Username)
		return
	}

	if len(cmd.Args) == 0 {
		// Show team info
		h.showTeamInfo(cmd.Username)
		return
	}

	subCmd := strings.ToLower(cmd.Args[0])
	tm := h.engine.GetTeamManager()

	switch subCmd {
	case "create", "crear":
		h.handleTeamCreate(cmd, tm)
	case "invite", "invitar":
		h.handleTeamInvite(cmd, tm)
	case "join", "unirse":
		h.handleTeamJoin(cmd, tm)
	case "leave", "salir":
		h.handleTeamLeave(cmd, tm)
	case "rename", "nombre":
		h.handleTeamRename(cmd, tm)
	case "color":
		h.handleTeamColor(cmd, tm)
	default:
		log.Printf("ℹ️ Team commands: create/invite/join/leave/rename/color")
	}
}

func (h *Handler) showTeamInfo(username string) {
	tm := h.engine.GetTeamManager()
	team := tm.GetTeamByMember(username)
	if team == nil {
		log.Printf("ℹ️ %s is not in a team. Use !team create <name>", username)
		return
	}

	members := make([]string, 0, len(team.Members))
	for m := range team.Members {
		members = append(members, m)
	}
	log.Printf("👥 Team %s [%s] | Leader: %s | Members: %d | Kills: %d",
		team.Name, team.Color, team.LeaderID, len(team.Members), team.Kills)
}

func (h *Handler) handleTeamCreate(cmd ChatCommand, tm *game.TeamManager) {
	// Check if already in a team
	if existing := tm.GetTeamByMember(cmd.Username); existing != nil {
		log.Printf("⚠️ %s already in team %s", cmd.Username, existing.Name)
		return
	}

	teamName := cmd.Username + "'s Team"
	if len(cmd.Args) > 1 {
		teamName = strings.Join(cmd.Args[1:], " ")
	}

	team, err := tm.CreateTeam(cmd.Username, teamName)
	if err != nil {
		log.Printf("⚠️ %s: Failed to create team: %v", cmd.Username, err)
		return
	}

	// Update player's team ID
	h.engine.SetPlayerTeam(cmd.Username, team.ID)
	log.Printf("👥 %s created team '%s'", cmd.Username, team.Name)
}

func (h *Handler) handleTeamInvite(cmd ChatCommand, tm *game.TeamManager) {
	team := tm.GetTeamByLeader(cmd.Username)
	if team == nil {
		log.Printf("⚠️ %s is not a team leader", cmd.Username)
		return
	}

	if len(cmd.Args) < 2 {
		log.Printf("ℹ️ Usage: !team invite <username>")
		return
	}

	targetName := cmd.Args[1]

	// Check target exists
	target := h.engine.GetPlayer(targetName)
	if target == nil {
		log.Printf("⚠️ Player %s not found", targetName)
		return
	}

	// Check target not already in a team
	if tm.GetTeamByMember(targetName) != nil {
		log.Printf("⚠️ %s is already in a team", targetName)
		return
	}

	err := tm.InvitePlayer(team.ID, cmd.Username, targetName)
	if err != nil {
		log.Printf("⚠️ %s: %v", cmd.Username, err)
		return
	}

	log.Printf("👥 %s invited %s to team %s (60s to accept with !team join %s)",
		cmd.Username, targetName, team.Name, cmd.Username)
}

func (h *Handler) handleTeamJoin(cmd ChatCommand, tm *game.TeamManager) {
	// Check not already in team
	if existing := tm.GetTeamByMember(cmd.Username); existing != nil {
		log.Printf("⚠️ %s already in team %s", cmd.Username, existing.Name)
		return
	}

	if len(cmd.Args) < 2 {
		log.Printf("ℹ️ Usage: !team join <leader_name>")
		return
	}

	leaderName := cmd.Args[1]
	team, err := tm.AcceptInvite(cmd.Username, leaderName)
	if err != nil {
		log.Printf("⚠️ %s: %v", cmd.Username, err)
		return
	}

	h.engine.SetPlayerTeam(cmd.Username, team.ID)
	log.Printf("👥 %s joined team %s", cmd.Username, team.Name)
}

func (h *Handler) handleTeamLeave(cmd ChatCommand, tm *game.TeamManager) {
	err := tm.LeaveTeam(cmd.Username)
	if err != nil {
		log.Printf("⚠️ %s: %v", cmd.Username, err)
		return
	}

	h.engine.SetPlayerTeam(cmd.Username, "")
	log.Printf("👥 %s left their team", cmd.Username)
}

func (h *Handler) handleTeamRename(cmd ChatCommand, tm *game.TeamManager) {
	if len(cmd.Args) < 2 {
		log.Printf("ℹ️ Usage: !team rename <new_name>")
		return
	}

	newName := strings.Join(cmd.Args[1:], " ")
	err := tm.RenameTeam(cmd.Username, newName)
	if err != nil {
		log.Printf("⚠️ %s: %v", cmd.Username, err)
		return
	}

	log.Printf("👥 %s renamed team to '%s'", cmd.Username, newName)
}

func (h *Handler) handleTeamColor(cmd ChatCommand, tm *game.TeamManager) {
	if len(cmd.Args) < 2 {
		log.Printf("ℹ️ Colors: red|blue|green|yellow|purple|orange|pink|cyan|white|black")
		return
	}

	color := strings.ToLower(cmd.Args[1])
	err := tm.SetTeamColor(cmd.Username, color)
	if err != nil {
		log.Printf("⚠️ %s: %v", cmd.Username, err)
		return
	}

	log.Printf("👥 %s set team color to %s", cmd.Username, color)
}

// Run starts processing commands from a channel (call in goroutine)
func (h *Handler) Run(commands <-chan ChatCommand) {
	for cmd := range commands {
		h.ProcessCommand(cmd)
	}
	log.Println("📜 Command handler stopped")
}
