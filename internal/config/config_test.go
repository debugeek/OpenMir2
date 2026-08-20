package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadReadsServerJSONFromConfigDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "server.json"), []byte(`{
  "server_name": "test",
  "storage_path": "var/state.json",
  "listeners": [
    {"name": "game", "addr": "127.0.0.1:7200"}
  ]
}`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.ServerName != "test" || cfg.StoragePath != "var/state.json" {
		t.Fatalf("config = %+v", cfg)
	}
	if len(cfg.Listeners) != 1 || cfg.Listeners[0].Name != "game" || cfg.Listeners[0].Addr != "127.0.0.1:7200" {
		t.Fatalf("listeners = %+v", cfg.Listeners)
	}
}

func TestLoadGameplayReadsTunableSettings(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "common.json"), []byte(`{
  "combat": {"hit_impact_delay_ms": 175},
  "progression": {"required_experience_per_level": 30},
	"monster": {"tick_ms": 900},
	"item": {"floor_drop_max_stack_per_tile": 7, "max_bag_item": 46}
}`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg, err := LoadGameplay(dir)
	if err != nil {
		t.Fatalf("LoadGameplay() error = %v", err)
	}
	if cfg.Combat.HitImpactDelayMS != 175 {
		t.Fatalf("HitImpactDelayMS = %d, want 175", cfg.Combat.HitImpactDelayMS)
	}
	if cfg.Progression.RequiredExperiencePerLevel != 30 {
		t.Fatalf("RequiredExperiencePerLevel = %d, want 30", cfg.Progression.RequiredExperiencePerLevel)
	}
	if cfg.Monster.TickMS != 900 {
		t.Fatalf("TickMS = %d, want 900", cfg.Monster.TickMS)
	}
	if cfg.Item.FloorDropMaxStackPerTile != 7 {
		t.Fatalf("FloorDropMaxStackPerTile = %d, want 7", cfg.Item.FloorDropMaxStackPerTile)
	}
	if cfg.Item.MaxBagItem != 46 {
		t.Fatalf("MaxBagItem = %d, want 46", cfg.Item.MaxBagItem)
	}
}

func TestLoadGameplayRejectsInvalidValues(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "common.json"), []byte(`{
  "combat": {"hit_impact_delay_ms": -1},
  "progression": {"required_experience_per_level": 0},
  "monster": {"tick_ms": 100},
  "item": {"floor_drop_max_stack_per_tile": 0, "max_bag_item": 0}
}`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if _, err := LoadGameplay(dir); err == nil {
		t.Fatalf("LoadGameplay() expected error for invalid settings")
	}
}
