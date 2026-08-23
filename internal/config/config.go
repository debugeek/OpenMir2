package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type Config struct {
	ServerName  string     `json:"server_name"`
	StoragePath string     `json:"storage_path"`
	Listeners   []Listener `json:"listeners"`
}

type Listener struct {
	Name string `json:"name"`
	Addr string `json:"addr"`
}

type Gameplay struct {
	Combat      CombatSettings      `json:"combat"`
	Progression ProgressionSettings `json:"progression"`
	Monster     MonsterSettings     `json:"monster"`
	Item        ItemSettings        `json:"item"`
	Guild       GuildSettings       `json:"guild"`
	Castle      CastleSettings      `json:"castle"`
}

type CombatSettings struct {
	HitImpactDelayMS int `json:"hit_impact_delay_ms"`
}

type ProgressionSettings struct {
	RequiredExperiencePerLevel int `json:"required_experience_per_level"`
}

type MonsterSettings struct {
	TickMS int `json:"tick_ms"`
}

type ItemSettings struct {
	FloorDropMaxStackPerTile int `json:"floor_drop_max_stack_per_tile"`
	FloorItemCanPickUpMS     int `json:"floor_item_can_pick_up_ms"`
	MaxBagItem               int `json:"max_bag_item"`
	UpgradeWeaponPrice       int `json:"upgrade_weapon_price"`
	UpgradeWeaponGetBackMS   int `json:"upgrade_weapon_get_back_ms"`
}

type GuildSettings struct {
	BuildGuildPrice int `json:"build_guild_price"`
	GuildWarPrice   int `json:"guild_war_price"`
}

type CastleSettings struct {
	RepairDoorPrice      int `json:"repair_door_price"`
	RepairWallPrice      int `json:"repair_wall_price"`
	HireGuardPrice       int `json:"hire_guard_price"`
	HireArcherPrice      int `json:"hire_archer_price"`
	SuperRepairPriceRate int `json:"super_repair_price_rate"`
}

func Load(dir string) (Config, error) {
	var cfg Config
	b, err := os.ReadFile(filepath.Join(dir, "server.json"))
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		return cfg, err
	}
	if cfg.ServerName == "" {
		return cfg, fmt.Errorf("server_name is required")
	}
	if cfg.StoragePath == "" {
		return cfg, fmt.Errorf("storage_path is required")
	}
	seen := map[string]bool{}
	for _, ln := range cfg.Listeners {
		if ln.Name == "" || ln.Addr == "" {
			return cfg, fmt.Errorf("listener name and addr are required")
		}
		if seen[ln.Name] {
			return cfg, fmt.Errorf("duplicate listener %q", ln.Name)
		}
		seen[ln.Name] = true
	}
	return cfg, nil
}

func LoadGameplay(dir string) (Gameplay, error) {
	cfg := DefaultGameplay()
	b, err := os.ReadFile(filepath.Join(dir, "common.json"))
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		return cfg, err
	}
	if cfg.Combat.HitImpactDelayMS < 0 {
		return cfg, fmt.Errorf("combat.hit_impact_delay_ms must be >= 0")
	}
	if cfg.Progression.RequiredExperiencePerLevel <= 0 {
		return cfg, fmt.Errorf("progression.required_experience_per_level must be > 0")
	}
	if cfg.Monster.TickMS <= 0 {
		return cfg, fmt.Errorf("monster.tick_ms must be > 0")
	}
	if cfg.Item.FloorDropMaxStackPerTile <= 0 {
		return cfg, fmt.Errorf("item.floor_drop_max_stack_per_tile must be > 0")
	}
	if cfg.Item.FloorItemCanPickUpMS < 0 {
		return cfg, fmt.Errorf("item.floor_item_can_pick_up_ms must be >= 0")
	}
	if cfg.Item.MaxBagItem <= 0 {
		return cfg, fmt.Errorf("item.max_bag_item must be > 0")
	}
	if cfg.Item.UpgradeWeaponPrice <= 0 {
		return cfg, fmt.Errorf("item.upgrade_weapon_price must be > 0")
	}
	if cfg.Item.UpgradeWeaponGetBackMS <= 0 {
		return cfg, fmt.Errorf("item.upgrade_weapon_get_back_ms must be > 0")
	}
	if cfg.Guild.BuildGuildPrice <= 0 {
		return cfg, fmt.Errorf("guild.build_guild_price must be > 0")
	}
	if cfg.Guild.GuildWarPrice <= 0 {
		return cfg, fmt.Errorf("guild.guild_war_price must be > 0")
	}
	if cfg.Castle.RepairDoorPrice <= 0 {
		return cfg, fmt.Errorf("castle.repair_door_price must be > 0")
	}
	if cfg.Castle.RepairWallPrice <= 0 {
		return cfg, fmt.Errorf("castle.repair_wall_price must be > 0")
	}
	if cfg.Castle.HireGuardPrice <= 0 {
		return cfg, fmt.Errorf("castle.hire_guard_price must be > 0")
	}
	if cfg.Castle.HireArcherPrice <= 0 {
		return cfg, fmt.Errorf("castle.hire_archer_price must be > 0")
	}
	if cfg.Castle.SuperRepairPriceRate <= 0 {
		return cfg, fmt.Errorf("castle.super_repair_price_rate must be > 0")
	}
	return cfg, nil
}

func DefaultGameplay() Gameplay {
	return Gameplay{
		Combat: CombatSettings{
			HitImpactDelayMS: 200,
		},
		Progression: ProgressionSettings{
			RequiredExperiencePerLevel: 20,
		},
		Monster: MonsterSettings{
			TickMS: 100,
		},
		Item: ItemSettings{
			FloorDropMaxStackPerTile: 5,
			FloorItemCanPickUpMS:     2 * 60 * 1000,
			MaxBagItem:               46,
			UpgradeWeaponPrice:       10000,
			UpgradeWeaponGetBackMS:   60 * 60 * 1000,
		},
		Guild: GuildSettings{
			BuildGuildPrice: 1000000,
			GuildWarPrice:   30000,
		},
		Castle: CastleSettings{
			RepairDoorPrice:      2000000,
			RepairWallPrice:      500000,
			HireGuardPrice:       300000,
			HireArcherPrice:      300000,
			SuperRepairPriceRate: 3,
		},
	}
}
