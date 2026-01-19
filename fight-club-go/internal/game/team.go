package game

import (
	"fmt"
	"sort"
	"sync"
	"time"
)

// Team represents a player team
type Team struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Color     string          `json:"color"`
	LeaderID  string          `json:"leaderId"`
	Members   map[string]bool `json:"-"` // Player names -> membership
	Kills     int             `json:"kills"`
	CreatedAt time.Time       `json:"createdAt"`

	// Pending invites (username -> expiry time)
	Invites map[string]time.Time `json:"-"`
}

// TeamManager handles team operations
type TeamManager struct {
	mu    sync.RWMutex
	teams map[string]*Team // Team ID -> Team
}

// InviteDuration is how long team invites last
const InviteDuration = 60 * time.Second

// MaxTeamSize limits team membership
const MaxTeamSize = 10

// MaxTeams limits total teams
const MaxTeams = 50

// Team colors available
var TeamColors = []string{
	"red", "blue", "green", "yellow", "purple",
	"orange", "pink", "cyan", "white", "black",
}

// NewTeamManager creates a new team manager
func NewTeamManager() *TeamManager {
	return &TeamManager{
		teams: make(map[string]*Team),
	}
}

// CreateTeam creates a new team with the player as leader
func (tm *TeamManager) CreateTeam(leaderName, teamName string) (*Team, error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	// Check team limit
	if len(tm.teams) >= MaxTeams {
		return nil, fmt.Errorf("maximum teams reached")
	}

	// Generate team ID
	teamID := fmt.Sprintf("team_%s_%d", leaderName, time.Now().UnixNano())

	team := &Team{
		ID:        teamID,
		Name:      teamName,
		Color:     TeamColors[len(tm.teams)%len(TeamColors)],
		LeaderID:  leaderName,
		Members:   make(map[string]bool),
		Invites:   make(map[string]time.Time),
		CreatedAt: time.Now(),
	}

	// Leader is first member
	team.Members[leaderName] = true

	tm.teams[teamID] = team
	return team, nil
}

// GetTeam returns a team by ID
func (tm *TeamManager) GetTeam(teamID string) *Team {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return tm.teams[teamID]
}

// GetTeamByLeader returns a team where player is leader
func (tm *TeamManager) GetTeamByLeader(leaderName string) *Team {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	for _, team := range tm.teams {
		if team.LeaderID == leaderName {
			return team
		}
	}
	return nil
}

// GetTeamByMember returns the team a player belongs to
func (tm *TeamManager) GetTeamByMember(playerName string) *Team {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	for _, team := range tm.teams {
		if team.Members[playerName] {
			return team
		}
	}
	return nil
}

// InvitePlayer sends a team invite
func (tm *TeamManager) InvitePlayer(teamID, inviterName, targetName string) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	team, ok := tm.teams[teamID]
	if !ok {
		return fmt.Errorf("team not found")
	}

	// Only leader can invite
	if team.LeaderID != inviterName {
		return fmt.Errorf("only team leader can invite")
	}

	// Check team size
	if len(team.Members) >= MaxTeamSize {
		return fmt.Errorf("team is full")
	}

	// Check if already member
	if team.Members[targetName] {
		return fmt.Errorf("player already in team")
	}

	// Clean expired invites
	now := time.Now()
	for name, expiry := range team.Invites {
		if now.After(expiry) {
			delete(team.Invites, name)
		}
	}

	// Add invite
	team.Invites[targetName] = now.Add(InviteDuration)
	return nil
}

// AcceptInvite accepts a team invite
func (tm *TeamManager) AcceptInvite(playerName, leaderName string) (*Team, error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	// Find team by leader
	var targetTeam *Team
	for _, team := range tm.teams {
		if team.LeaderID == leaderName {
			targetTeam = team
			break
		}
	}

	if targetTeam == nil {
		return nil, fmt.Errorf("team not found")
	}

	// Check invite exists and not expired
	expiry, hasInvite := targetTeam.Invites[playerName]
	if !hasInvite || time.Now().After(expiry) {
		return nil, fmt.Errorf("no valid invite")
	}

	// Check team size
	if len(targetTeam.Members) >= MaxTeamSize {
		return nil, fmt.Errorf("team is full")
	}

	// Join team
	delete(targetTeam.Invites, playerName)
	targetTeam.Members[playerName] = true

	return targetTeam, nil
}

// LeaveTeam removes a player from their team
func (tm *TeamManager) LeaveTeam(playerName string) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	for teamID, team := range tm.teams {
		if team.Members[playerName] {
			delete(team.Members, playerName)

			// If leader left, disband or transfer
			if team.LeaderID == playerName {
				if len(team.Members) == 0 {
					// Disband empty team
					delete(tm.teams, teamID)
				} else {
					// Transfer to first remaining member
					for member := range team.Members {
						team.LeaderID = member
						break
					}
				}
			}
			return nil
		}
	}
	return fmt.Errorf("not in a team")
}

// RenameTeam renames a team (leader only)
func (tm *TeamManager) RenameTeam(leaderName, newName string) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	for _, team := range tm.teams {
		if team.LeaderID == leaderName {
			team.Name = newName
			return nil
		}
	}
	return fmt.Errorf("not a team leader")
}

// SetTeamColor sets team color (leader only)
func (tm *TeamManager) SetTeamColor(leaderName, color string) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	// Validate color
	validColor := false
	for _, c := range TeamColors {
		if c == color {
			validColor = true
			break
		}
	}
	if !validColor {
		return fmt.Errorf("invalid color")
	}

	for _, team := range tm.teams {
		if team.LeaderID == leaderName {
			team.Color = color
			return nil
		}
	}
	return fmt.Errorf("not a team leader")
}

// AddKill adds a kill to the team counter
func (tm *TeamManager) AddKill(teamID string) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if team, ok := tm.teams[teamID]; ok {
		team.Kills++
	}
}

// GetTopTeams returns teams sorted by kills
func (tm *TeamManager) GetTopTeams(limit int) []*Team {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	teams := make([]*Team, 0, len(tm.teams))
	for _, team := range tm.teams {
		teams = append(teams, team)
	}

	// Sort by kills descending using O(n log n) sort instead of O(n²) bubble sort
	sort.Slice(teams, func(i, j int) bool {
		return teams[i].Kills > teams[j].Kills
	})

	if len(teams) > limit {
		teams = teams[:limit]
	}
	return teams
}

// GetAllTeams returns all teams
func (tm *TeamManager) GetAllTeams() []*Team {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	teams := make([]*Team, 0, len(tm.teams))
	for _, t := range tm.teams {
		teams = append(teams, t)
	}
	return teams
}
