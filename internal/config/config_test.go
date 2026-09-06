package config

import (
	"fmt"
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
	"combat": {"hit_impact_delay_ms": 175, "hit_interval_ms": 850, "hit_speed_step_ms": 20, "hit_drop_over_speed_ms": 12, "max_hit_messages": 2, "max_spell_messages": 3, "action_interval_ms": 275, "magic_hit_interval_ms": 645, "struck_time_ms": 225, "control_action_interval": false, "control_walk_hit": false, "control_run_long_hit": false, "control_run_hit": false, "control_run_magic": false, "walk_hit_interval_ms": 700, "run_hit_interval_ms": 710, "run_long_hit_interval_ms": 720, "run_magic_interval_ms": 730, "walk_interval_ms": 500, "run_interval_ms": 510, "max_walk_messages": 2, "max_run_messages": 3, "speed_control_mode": 1, "paraly_can_hit": true, "paraly_can_walk": true, "paraly_can_run": true},
	"recovery": {"health_fill_time_ms": 350, "spell_fill_time_ms": 900},
  "progression": {"required_experience_per_level": 30},
	"monster": {"tick_ms": 900},
	"movement": {"user_move_can_dup_obj": true, "user_move_can_on_item": false, "user_move_cooldown_ms": 12500},
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
	if cfg.Combat.StruckTimeMS != 225 {
		t.Fatalf("StruckTimeMS = %d, want 225", cfg.Combat.StruckTimeMS)
	}
	if cfg.Combat.HitIntervalMS != 850 || cfg.Combat.HitSpeedStepMS != 20 {
		t.Fatalf("hit interval settings = %d/%d, want 850/20", cfg.Combat.HitIntervalMS, cfg.Combat.HitSpeedStepMS)
	}
	if cfg.Combat.HitDropOverSpeedMS != 12 || cfg.Combat.MaxHitMessages != 2 {
		t.Fatalf("hit overload settings = %d/%d, want 12/2", cfg.Combat.HitDropOverSpeedMS, cfg.Combat.MaxHitMessages)
	}
	if cfg.Combat.MaxSpellMessages != 3 {
		t.Fatalf("spell overload settings = %d, want 3", cfg.Combat.MaxSpellMessages)
	}
	if cfg.Combat.ActionIntervalMS != 275 {
		t.Fatalf("ActionIntervalMS = %d, want 275", cfg.Combat.ActionIntervalMS)
	}
	if cfg.Combat.MagicHitIntervalMS != 645 {
		t.Fatalf("MagicHitIntervalMS = %d, want 645", cfg.Combat.MagicHitIntervalMS)
	}
	if cfg.Combat.ControlWalkHit || cfg.Combat.ControlRunHit || cfg.Combat.ControlRunLongHit || cfg.Combat.WalkHitIntervalMS != 700 || cfg.Combat.RunHitIntervalMS != 710 || cfg.Combat.RunLongHitIntervalMS != 720 {
		t.Fatalf("directional hit settings = %+v", cfg.Combat)
	}
	if cfg.Combat.ControlRunMagic || cfg.Combat.RunMagicIntervalMS != 730 {
		t.Fatalf("run-magic settings = %+v", cfg.Combat)
	}
	if cfg.Combat.WalkIntervalMS != 500 || cfg.Combat.RunIntervalMS != 510 || cfg.Combat.MaxWalkMessages != 2 || cfg.Combat.MaxRunMessages != 3 || !cfg.Combat.ParalyCanWalk || !cfg.Combat.ParalyCanRun {
		t.Fatalf("movement settings = %+v", cfg.Combat)
	}
	if cfg.Combat.SpeedControlMode != 1 {
		t.Fatalf("SpeedControlMode = %d, want 1", cfg.Combat.SpeedControlMode)
	}
	if cfg.Combat.ControlActionInterval {
		t.Fatal("ControlActionInterval = true, want false")
	}
	if !cfg.Combat.ParalyCanHit {
		t.Fatal("ParalyCanHit = false, want true")
	}
	if cfg.Recovery.HealthFillTimeMS != 350 || cfg.Recovery.SpellFillTimeMS != 900 {
		t.Fatalf("Recovery = %+v, want 350/900", cfg.Recovery)
	}
	if cfg.Progression.RequiredExperiencePerLevel != 30 {
		t.Fatalf("RequiredExperiencePerLevel = %d, want 30", cfg.Progression.RequiredExperiencePerLevel)
	}
	if cfg.Monster.TickMS != 900 {
		t.Fatalf("TickMS = %d, want 900", cfg.Monster.TickMS)
	}
	if !cfg.Movement.UserMoveCanDupObj || cfg.Movement.UserMoveCanOnItem || cfg.Movement.UserMoveCooldownMS != 12500 {
		t.Fatalf("UserMove settings = %+v", cfg.Movement)
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

func TestLoadGameplayRejectsInvalidHitThrottleValues(t *testing.T) {
	for _, field := range []string{"hit_interval_ms", "hit_speed_step_ms", "hit_drop_over_speed_ms", "max_hit_messages", "max_spell_messages", "action_interval_ms", "magic_hit_interval_ms", "walk_hit_interval_ms", "run_interval_ms", "run_long_hit_interval_ms", "walk_interval_ms", "max_walk_messages", "max_run_messages"} {
		t.Run(field, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "common.json"), []byte(fmt.Sprintf(`{
  "combat": {"%s": -1},
  "progression": {"required_experience_per_level": 20},
  "monster": {"tick_ms": 100},
  "item": {"floor_drop_max_stack_per_tile": 5, "max_bag_item": 46}
}`, field)), 0o644); err != nil {
				t.Fatalf("WriteFile() error = %v", err)
			}
			if _, err := LoadGameplay(dir); err == nil {
				t.Fatalf("LoadGameplay() expected error for negative %s", field)
			}
		})
	}
}

func TestLoadGameplayRejectsUnsupportedSpeedControlMode(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "common.json"), []byte(`{
  "combat": {"speed_control_mode": 2},
  "progression": {"required_experience_per_level": 20},
  "monster": {"tick_ms": 100},
  "item": {"floor_drop_max_stack_per_tile": 5, "max_bag_item": 46}
}`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if _, err := LoadGameplay(dir); err == nil {
		t.Fatal("LoadGameplay() expected error for speed_control_mode=2")
	}
}
