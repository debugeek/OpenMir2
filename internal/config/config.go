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
	Recovery    RecoverySettings    `json:"recovery"`
	Progression ProgressionSettings `json:"progression"`
	Monster     MonsterSettings     `json:"monster"`
	Movement    MovementSettings    `json:"movement"`
	Item        ItemSettings        `json:"item"`
	Guild       GuildSettings       `json:"guild"`
	Castle      CastleSettings      `json:"castle"`
}

type RecoverySettings struct {
	HealthFillTimeMS int `json:"health_fill_time_ms"`
	SpellFillTimeMS  int `json:"spell_fill_time_ms"`
	RevivalTimeMS    int `json:"revival_time_ms"`
}

type CombatSettings struct {
	HitImpactDelayMS           int    `json:"hit_impact_delay_ms"`
	HitIntervalMS              int    `json:"hit_interval_ms"`
	HitSpeedStepMS             int    `json:"hit_speed_step_ms"`
	HitDropOverSpeedMS         int    `json:"hit_drop_over_speed_ms"`
	MaxHitMessages             int    `json:"max_hit_messages"`
	MaxSpellMessages           int    `json:"max_spell_messages"`
	ActionIntervalMS           int    `json:"action_interval_ms"`
	MagicHitIntervalMS         int    `json:"magic_hit_interval_ms"`
	StruckTimeMS               int    `json:"struck_time_ms"`
	TurnIntervalMS             int    `json:"turn_interval_ms"`
	MaxTurnMessages            int    `json:"max_turn_messages"`
	MaxSitDownMessages         int    `json:"max_sit_down_messages"`
	ControlActionInterval      bool   `json:"control_action_interval"`
	ControlWalkHit             bool   `json:"control_walk_hit"`
	ControlRunLongHit          bool   `json:"control_run_long_hit"`
	ControlRunHit              bool   `json:"control_run_hit"`
	ControlRunMagic            bool   `json:"control_run_magic"`
	WalkHitIntervalMS          int    `json:"walk_hit_interval_ms"`
	RunHitIntervalMS           int    `json:"run_hit_interval_ms"`
	RunLongHitIntervalMS       int    `json:"run_long_hit_interval_ms"`
	RunMagicIntervalMS         int    `json:"run_magic_interval_ms"`
	WalkIntervalMS             int    `json:"walk_interval_ms"`
	RunIntervalMS              int    `json:"run_interval_ms"`
	MaxWalkMessages            int    `json:"max_walk_messages"`
	MaxRunMessages             int    `json:"max_run_messages"`
	SpeedControlMode           int    `json:"speed_control_mode"`
	NonPKServer                bool   `json:"non_pk_server"`
	ParalyCanWalk              bool   `json:"paraly_can_walk"`
	ParalyCanRun               bool   `json:"paraly_can_run"`
	ParalyCanHit               bool   `json:"paraly_can_hit"`
	ParalyCanSpell             bool   `json:"paraly_can_spell"`
	DisableStruck              bool   `json:"disable_struck"`
	DisableSelfStruck          bool   `json:"disable_self_struck"`
	DisableFireCrossInSafeZone bool   `json:"disable_fire_cross_in_safe_zone"`
	PKLevelProtect             bool   `json:"pk_level_protect"`
	PKProtectLevel             int    `json:"pk_protect_level"`
	RedPKProtectLevel          int    `json:"red_pk_protect_level"`
	SafeZoneSize               int    `json:"safe_zone_size"`
	RedHomeMap                 string `json:"red_home_map"`
	RedHomeX                   int    `json:"red_home_x"`
	RedHomeY                   int    `json:"red_home_y"`
	MapMoveProtectMS           int    `json:"map_move_protect_ms"`
}

type ProgressionSettings struct {
	RequiredExperiencePerLevel int `json:"required_experience_per_level"`
}

type MonsterSettings struct {
	TickMS int `json:"tick_ms"`
}

type MovementSettings struct {
	UserMoveCanDupObj  bool `json:"user_move_can_dup_obj"`
	UserMoveCanOnItem  bool `json:"user_move_can_on_item"`
	UserMoveCooldownMS int  `json:"user_move_cooldown_ms"`
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
	if cfg.Combat.HitIntervalMS < 0 {
		return cfg, fmt.Errorf("combat.hit_interval_ms must be >= 0")
	}
	if cfg.Combat.HitSpeedStepMS < 0 {
		return cfg, fmt.Errorf("combat.hit_speed_step_ms must be >= 0")
	}
	if cfg.Combat.HitDropOverSpeedMS < 0 {
		return cfg, fmt.Errorf("combat.hit_drop_over_speed_ms must be >= 0")
	}
	if cfg.Combat.MaxHitMessages <= 0 {
		return cfg, fmt.Errorf("combat.max_hit_messages must be > 0")
	}
	if cfg.Combat.MaxSpellMessages <= 0 {
		return cfg, fmt.Errorf("combat.max_spell_messages must be > 0")
	}
	if cfg.Combat.ActionIntervalMS < 0 {
		return cfg, fmt.Errorf("combat.action_interval_ms must be >= 0")
	}
	if cfg.Combat.MagicHitIntervalMS < 0 {
		return cfg, fmt.Errorf("combat.magic_hit_interval_ms must be >= 0")
	}
	if cfg.Combat.WalkHitIntervalMS < 0 {
		return cfg, fmt.Errorf("combat.walk_hit_interval_ms must be >= 0")
	}
	if cfg.Combat.RunHitIntervalMS < 0 {
		return cfg, fmt.Errorf("combat.run_hit_interval_ms must be >= 0")
	}
	if cfg.Combat.RunLongHitIntervalMS < 0 {
		return cfg, fmt.Errorf("combat.run_long_hit_interval_ms must be >= 0")
	}
	if cfg.Combat.RunMagicIntervalMS < 0 {
		return cfg, fmt.Errorf("combat.run_magic_interval_ms must be >= 0")
	}
	if cfg.Combat.WalkIntervalMS < 0 {
		return cfg, fmt.Errorf("combat.walk_interval_ms must be >= 0")
	}
	if cfg.Combat.RunIntervalMS < 0 {
		return cfg, fmt.Errorf("combat.run_interval_ms must be >= 0")
	}
	if cfg.Movement.UserMoveCooldownMS < 0 {
		return cfg, fmt.Errorf("movement.user_move_cooldown_ms must be >= 0")
	}
	if cfg.Combat.MaxWalkMessages <= 0 {
		return cfg, fmt.Errorf("combat.max_walk_messages must be > 0")
	}
	if cfg.Combat.MaxRunMessages <= 0 {
		return cfg, fmt.Errorf("combat.max_run_messages must be > 0")
	}
	if cfg.Combat.SpeedControlMode < 0 || cfg.Combat.SpeedControlMode > 1 {
		return cfg, fmt.Errorf("combat.speed_control_mode must be 0 or 1")
	}
	if cfg.Combat.StruckTimeMS < 0 {
		return cfg, fmt.Errorf("combat.struck_time_ms must be >= 0")
	}
	if cfg.Combat.TurnIntervalMS < 0 {
		return cfg, fmt.Errorf("combat.turn_interval_ms must be >= 0")
	}
	if cfg.Combat.MaxTurnMessages <= 0 {
		return cfg, fmt.Errorf("combat.max_turn_messages must be > 0")
	}
	if cfg.Combat.MaxSitDownMessages <= 0 {
		return cfg, fmt.Errorf("combat.max_sit_down_messages must be > 0")
	}
	if cfg.Recovery.HealthFillTimeMS <= 0 {
		return cfg, fmt.Errorf("recovery.health_fill_time_ms must be > 0")
	}
	if cfg.Recovery.SpellFillTimeMS <= 0 {
		return cfg, fmt.Errorf("recovery.spell_fill_time_ms must be > 0")
	}
	if cfg.Recovery.RevivalTimeMS <= 0 {
		return cfg, fmt.Errorf("recovery.revival_time_ms must be > 0")
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
			HitImpactDelayMS:      200,
			HitIntervalMS:         900,
			HitSpeedStepMS:        25,
			HitDropOverSpeedMS:    10,
			MaxHitMessages:        1,
			MaxSpellMessages:      1,
			ActionIntervalMS:      350,
			MagicHitIntervalMS:    800,
			StruckTimeMS:          100,
			TurnIntervalMS:        600,
			MaxTurnMessages:       1,
			MaxSitDownMessages:    1,
			ControlActionInterval: true,
			ControlWalkHit:        true,
			ControlRunLongHit:     true,
			ControlRunHit:         true,
			ControlRunMagic:       true,
			WalkHitIntervalMS:     800,
			RunHitIntervalMS:      800,
			RunLongHitIntervalMS:  800,
			RunMagicIntervalMS:    900,
			WalkIntervalMS:        600,
			RunIntervalMS:         600,
			MaxWalkMessages:       1,
			MaxRunMessages:        1,
			PKProtectLevel:        10,
			RedPKProtectLevel:     10,
			SafeZoneSize:          10,
			RedHomeMap:            "3",
			RedHomeX:              845,
			RedHomeY:              674,
			MapMoveProtectMS:      3000,
		},
		Recovery: RecoverySettings{
			HealthFillTimeMS: 300,
			SpellFillTimeMS:  800,
			RevivalTimeMS:    60 * 1000,
		},
		Progression: ProgressionSettings{
			RequiredExperiencePerLevel: 20,
		},
		Monster: MonsterSettings{
			TickMS: 100,
		},
		Movement: MovementSettings{
			UserMoveCanDupObj:  false,
			UserMoveCanOnItem:  true,
			UserMoveCooldownMS: 10000,
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
