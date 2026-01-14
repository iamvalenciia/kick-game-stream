package game

// Weapon represents a weapon configuration
type Weapon struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	MinDamage int     `json:"minDamage"`
	MaxDamage int     `json:"maxDamage"`
	Range     float64 `json:"range"`
	Cooldown  float64 `json:"cooldown"` // seconds
	Price     int     `json:"price"`
	Color     string  `json:"color"`
	Emoji     string  `json:"emoji"`
}

// Weapons is the map of all available weapons
// NOTE: Range must be > 60 (two player radii = 30 + 30) to hit
var Weapons = map[string]Weapon{
	"fists": {
		ID:        "fists",
		Name:      "Fists",
		MinDamage: 8,
		MaxDamage: 15,
		Range:     80,  // Was 50, needs to be > 60 to hit
		Cooldown:  0.4, // Faster attacks
		Price:     0,
		Color:     "#ffeb3b",
		Emoji:     "👊",
	},
	"knife": {
		ID:        "knife",
		Name:      "Knife",
		MinDamage: 12,
		MaxDamage: 22,
		Range:     90,
		Cooldown:  0.35,
		Price:     50,
		Color:     "#9e9e9e",
		Emoji:     "🔪",
	},
	"sword": {
		ID:        "sword",
		Name:      "Sword",
		MinDamage: 18,
		MaxDamage: 35,
		Range:     100,
		Cooldown:  0.5,
		Price:     100, // Updated from 250
		Color:     "#2196f3",
		Emoji:     "⚔️",
	},
	"spear": {
		ID:        "spear",
		Name:      "Spear",
		MinDamage: 15,
		MaxDamage: 30,
		Range:     150, // Long range, line attack
		Cooldown:  0.6,
		Price:     200,
		Color:     "#607d8b",
		Emoji:     "🔱",
	},
	"axe": {
		ID:        "axe",
		Name:      "Battle Axe",
		MinDamage: 30,
		MaxDamage: 50,
		Range:     95,
		Cooldown:  0.8,
		Price:     300, // Updated from 400
		Color:     "#795548",
		Emoji:     "🪓",
	},
	"bow": {
		ID:        "bow",
		Name:      "Bow",
		MinDamage: 20,
		MaxDamage: 40,
		Range:     250, // Ranged projectile
		Cooldown:  1.0,
		Price:     400,
		Color:     "#8bc34a",
		Emoji:     "🏹",
	},
	"scythe": {
		ID:        "scythe",
		Name:      "Scythe",
		MinDamage: 40,
		MaxDamage: 65,
		Range:     140,
		Cooldown:  0.7,
		Price:     500, // Updated from 800
		Color:     "#9c27b0",
		Emoji:     "🌙",
	},
	"katana": {
		ID:        "katana",
		Name:      "Katana",
		MinDamage: 25,
		MaxDamage: 40,
		Range:     120,
		Cooldown:  0.45,
		Price:     350,
		Color:     "#e91e63",
		Emoji:     "🗡️",
	},
	"hammer": {
		ID:        "hammer",
		Name:      "War Hammer",
		MinDamage: 45,
		MaxDamage: 75,
		Range:     90,
		Cooldown:  1.2,
		Price:     600,
		Color:     "#ff5722",
		Emoji:     "🔨",
	},
}

// GetWeapon returns a weapon by ID, defaults to fists
func GetWeapon(id string) Weapon {
	if w, ok := Weapons[id]; ok {
		return w
	}
	return Weapons["fists"]
}

// GetAllWeapons returns all weapons as a slice
func GetAllWeapons() []Weapon {
	weapons := make([]Weapon, 0, len(Weapons))
	for _, w := range Weapons {
		weapons = append(weapons, w)
	}
	return weapons
}
