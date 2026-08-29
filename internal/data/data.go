package data

import "openmir2/internal/npc"

type StdBundle struct {
	Items     map[string]StdItem
	ItemOrder []string
	Skills    map[string]StdSkill
	Monsters  map[string]StdMonster
	Drops     map[string]StdDropTable
	Maps      map[string]StdMap
	Spawns    []StdSpawn
	MakeItems map[string][]StdMakeIngredient
	NPCs      npc.Library
}

type StdLoadReport struct {
	Skipped []StdLoadSkip
}

type StdLoadSkip struct {
	Kind   string
	File   string
	MapID  string
	ID     string
	Reason string
}

type StdItem struct {
	ID           string       `json:"id"`
	Name         string       `json:"name"`
	Kind         string       `json:"kind"`
	StdMode      int          `json:"std_mode,omitempty"`
	Shape        int          `json:"shape,omitempty"`
	Looks        int          `json:"looks,omitempty"`
	DuraMax      int          `json:"dura_max,omitempty"`
	AniCount     int          `json:"ani_count,omitempty"`
	SpecialPwr   int          `json:"special_pwr,omitempty"`
	ItemDesc     int          `json:"item_desc,omitempty"`
	Need         int          `json:"need,omitempty"`
	NeedLevel    int          `json:"need_level,omitempty"`
	NeedIdentify int          `json:"need_identify,omitempty"`
	Price        int          `json:"price,omitempty"`
	Stock        int          `json:"stock,omitempty"`
	Color        int          `json:"color,omitempty"`
	AtkSpd       int          `json:"atk_spd,omitempty"`
	Agility      int          `json:"agility,omitempty"`
	Accurate     int          `json:"accurate,omitempty"`
	MgAvoid      int          `json:"mg_avoid,omitempty"`
	Strong       int          `json:"strong,omitempty"`
	Undead       int          `json:"undead,omitempty"`
	HpAdd        int          `json:"hp_add,omitempty"`
	MpAdd        int          `json:"mp_add,omitempty"`
	ExpAdd       int          `json:"exp_add,omitempty"`
	EffType1     int          `json:"eff_type_1,omitempty"`
	EffRate1     int          `json:"eff_rate_1,omitempty"`
	EffValue1    int          `json:"eff_value_1,omitempty"`
	EffType2     int          `json:"eff_type_2,omitempty"`
	EffRate2     int          `json:"eff_rate_2,omitempty"`
	EffValue2    int          `json:"eff_value_2,omitempty"`
	Slowdown     int          `json:"slowdown,omitempty"`
	Tox          int          `json:"tox,omitempty"`
	ToxAvoid     int          `json:"tox_avoid,omitempty"`
	UniqueItem   int          `json:"unique_item,omitempty"`
	OverlapItem  int          `json:"overlap_item,omitempty"`
	Light        int          `json:"light,omitempty"`
	ItemType     int          `json:"item_type,omitempty"`
	ItemSet      int          `json:"item_set,omitempty"`
	Reference    string       `json:"reference,omitempty"`
	Weight       int          `json:"weight"`
	Stats        StdItemStats `json:"stats"`
}

type StdItemStats struct {
	DcMin  int `json:"min_attack"`
	DcMax  int `json:"max_attack"`
	AcMin  int `json:"defense"`
	AcMax  int `json:"defense_max"`
	MacMin int `json:"magic_defense"`
	MacMax int `json:"magic_defense_max"`
	McMin  int `json:"magic_attack"`
	McMax  int `json:"magic_attack_max"`
	ScMin  int `json:"tao_attack"`
	ScMax  int `json:"tao_attack_max"`
}

type StdSkill struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Job         int    `json:"job"`
	EffectType  int    `json:"effect_type"`
	Effect      int    `json:"effect"`
	Power       int    `json:"power"`
	MaxPower    int    `json:"max_power"`
	Spell       int    `json:"spell"`
	Delay       int    `json:"delay"`
	NeedLevel1  int    `json:"need_level_1"`
	TrainLevel1 int    `json:"train_level_1"`
}

type StdMonster struct {
	ID                 string `json:"id"`
	Name               string `json:"name"`
	Race               int    `json:"race"`
	RaceImg            int    `json:"race_img"`
	Appr               int    `json:"appr"`
	Level              int    `json:"level"`
	Undead             int    `json:"undead"`
	CoolEye            int    `json:"cool_eye"`
	Animal             bool   `json:"animal"`
	FleeOnSight        bool   `json:"flee_on_sight"`
	UseMagic           bool   `json:"use_magic"`
	Hidden             bool   `json:"hidden"`
	FixedHideMode      bool   `json:"fixed_hide_mode"`
	StoneMode          bool   `json:"stone_mode"`
	FirstRevealPending bool   `json:"first_reveal_pending"`
	AttackMax          int    `json:"attack_max"`
	ViewRange          int    `json:"view_range"`
	LeashRange         int    `json:"leash_range"`
	SearchNoTargetMS   int    `json:"search_no_target_ms"`
	SearchHasTargetMS  int    `json:"search_has_target_ms"`
	HP                 int    `json:"hp"`
	MP                 int    `json:"mp"`
	MinAttack          int    `json:"min_attack"`
	MaxAttack          int    `json:"max_attack"`
	Defense            int    `json:"defense"`
	MagicDefense       int    `json:"magic_defense"`
	MagicAttack        int    `json:"magic_attack"`
	TaoAttack          int    `json:"tao_attack"`
	Experience         int    `json:"experience"`
	Speed              int    `json:"speed"`
	Hit                int    `json:"hit"`
	WalkSpeedMS        int    `json:"walk_speed_ms"`
	WalkStep           int    `json:"walk_step"`
	WalkWait           int    `json:"walk_wait"`
	AttackIntervalMS   int    `json:"attack_interval_ms"`
}

type StdDropTable struct {
	ID      string         `json:"id"`
	Entries []StdDropEntry `json:"entries"`
}

type StdDropEntry struct {
	ItemID   string  `json:"item_id"`
	Chance   float64 `json:"chance"`
	MinCount int     `json:"min_count"`
	MaxCount int     `json:"max_count"`
}

type StdMakeIngredient struct {
	ItemID string `json:"item_id"`
	Count  int    `json:"count"`
}

type StdMap struct {
	ID               string             `json:"id"`
	Name             string             `json:"name"`
	Light            int                `json:"light,omitempty"`
	Width            int                `json:"width"`
	Height           int                `json:"height"`
	MonsterSpawnRate int                `json:"monster_spawn_rate"`
	Blocked          []StdPoint         `json:"blocked"`
	StartPoints      []StdStartPoint    `json:"start_points,omitempty"`
	Connections      []StdMapConnection `json:"connections"`
	Spawns           []StdMapSpawn      `json:"monster_spawns"`
}

type StdStartPoint struct {
	MapID string `json:"-"`
	X     int    `json:"x"`
	Y     int    `json:"y"`
}

type StdMapConnection struct {
	ToMap string `json:"to_map"`
	FromX int    `json:"from_x"`
	FromY int    `json:"from_y"`
	ToX   int    `json:"to_x"`
	ToY   int    `json:"to_y"`
}

type StdMapSpawn struct {
	MonsterID      string `json:"monster_id"`
	X              int    `json:"x"`
	Y              int    `json:"y"`
	Range          int    `json:"range"`
	Count          int    `json:"count"`
	RespawnSeconds int    `json:"respawn_seconds"`
	MissionGenRate int    `json:"mission_gen_rate"`
}

type StdPoint struct {
	X int `json:"x"`
	Y int `json:"y"`
}

type StdSpawn struct {
	ID             string `json:"id"`
	MapID          string `json:"map_id"`
	MonsterID      string `json:"monster_id"`
	X              int    `json:"x"`
	Y              int    `json:"y"`
	Range          int    `json:"range"`
	Count          int    `json:"count"`
	RespawnSeconds int    `json:"respawn_seconds"`
	MissionGenRate int    `json:"mission_gen_rate"`
}
