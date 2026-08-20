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
		},
	}
}
