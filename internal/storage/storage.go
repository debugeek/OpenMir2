package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Store struct {
	path string
	mu   sync.Mutex
	db   database
}

type database struct {
	Accounts   map[string]Account   `json:"accounts"`
	Characters map[string]Character `json:"characters"`
	Guilds     map[string]Guild     `json:"guilds,omitempty"`
	Castles    map[string]Castle    `json:"castles,omitempty"`
	NextID     int                  `json:"next_id"`
}

type Account struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type Guild struct {
	ID             string               `json:"id"`
	Notice         string               `json:"notice,omitempty"`
	Ranks          []GuildRank          `json:"ranks,omitempty"`
	Alliance       string               `json:"alliance,omitempty"`
	Alliances      []string             `json:"alliances,omitempty"`
	Wars           map[string]bool      `json:"wars,omitempty"`
	WarOrder       []string             `json:"war_order,omitempty"`
	WarExpires     map[string]time.Time `json:"war_expires,omitempty"`
	EnableAuthAlly bool                 `json:"-"`
}

type GuildRank struct {
	Number  int      `json:"number"`
	Name    string   `json:"name"`
	Members []string `json:"members,omitempty"`
}

type ExpiredGuildWar struct {
	GuildID  string
	TargetID string
}

type Castle struct {
	ID            string           `json:"id"`
	OwnerGuildID  string           `json:"owner_guild_id,omitempty"`
	Gold          int              `json:"gold,omitempty"`
	TodayIncome   int              `json:"today_income,omitempty"`
	UnderWar      bool             `json:"under_war,omitempty"`
	MainDoorOpen  bool             `json:"main_door_open,omitempty"`
	MainDoorHP    int              `json:"main_door_hp,omitempty"`
	MainDoorHitAt int64            `json:"main_door_hit_at,omitempty"`
	WallHP        [3]int           `json:"wall_hp,omitempty"`
	WallHitAt     [3]int64         `json:"wall_hit_at,omitempty"`
	GuardSlots    [4]string        `json:"guard_slots,omitempty"`
	ArcherSlots   [12]string       `json:"archer_slots,omitempty"`
	GuardHP       [4]int           `json:"guard_hp,omitempty"`
	ArcherHP      [12]int          `json:"archer_hp,omitempty"`
	DefenseHPSet  bool             `json:"defense_hp_set,omitempty"`
	Attackers     map[string]int64 `json:"attackers,omitempty"`
}

type Character struct {
	ID                  string              `json:"id"`
	Account             string              `json:"account"`
	Name                string              `json:"name"`
	Class               string              `json:"class"`
	Hair                int                 `json:"hair"`
	Sex                 int                 `json:"sex"`
	Selected            bool                `json:"selected,omitempty"`
	Deleted             bool                `json:"deleted,omitempty"`
	Level               int                 `json:"level"`
	Experience          int                 `json:"experience"`
	ExperienceMultiple  int                 `json:"experience_multiple,omitempty"`
	ExperienceRate      int                 `json:"experience_rate,omitempty"`
	BodyLuck            float64             `json:"body_luck,omitempty"`
	BodyLuckLevel       int                 `json:"body_luck_level,omitempty"`
	HomeMap             string              `json:"home_map,omitempty"`
	HomeX               int                 `json:"home_x,omitempty"`
	HomeY               int                 `json:"home_y,omitempty"`
	MapID               string              `json:"map_id"`
	X                   int                 `json:"x"`
	Y                   int                 `json:"y"`
	Dir                 int                 `json:"dir"`
	TargetID            string              `json:"-"`
	Sitting             bool                `json:"-"`
	ParalyzedUntil      int64               `json:"paralyzed_until,omitempty"`
	HP                  int                 `json:"hp"`
	MaxHP               int                 `json:"max_hp"`
	MP                  int                 `json:"mp"`
	MaxMP               int                 `json:"max_mp"`
	FightZoneDieCount   int                 `json:"fight_zone_die_count,omitempty"`
	DieInFight3Zone     bool                `json:"-"`
	IncHealth           int                 `json:"inc_health,omitempty"`
	IncSpell            int                 `json:"inc_spell,omitempty"`
	IncHealing          int                 `json:"inc_healing,omitempty"`
	IncHealthSpellAt    int64               `json:"inc_health_spell_at,omitempty"`
	PerHealth           int                 `json:"-"`
	PerSpell            int                 `json:"-"`
	PerHealing          int                 `json:"-"`
	HealthTick          int                 `json:"-"`
	HealthTickAt        int64               `json:"-"`
	SpellTick           int                 `json:"-"`
	SpellTickAt         int64               `json:"-"`
	Gold                int                 `json:"gold,omitempty"`
	AttackMode          int                 `json:"attack_mode,omitempty"`
	AdminMode           bool                `json:"-"`
	ObserverMode        bool                `json:"-"`
	Ghost               bool                `json:"-"`
	StoneMode           bool                `json:"-"`
	DrugUnlocked        bool                `json:"-"`
	PKPoint             int                 `json:"pk_point,omitempty"`
	PKPointDecayAt      int64               `json:"-"`
	PKFlag              bool                `json:"-"`
	PKFlagUntil         int64               `json:"-"`
	TargetFocusAt       int64               `json:"-"`
	LastHitterID        string              `json:"-"`
	LastHitterAt        int64               `json:"-"`
	ExpHitterID         string              `json:"-"`
	ExpHitterAt         int64               `json:"-"`
	FireHitArmed        bool                `json:"-"`
	FireHitLatestAt     int64               `json:"-"`
	PowerHitArmed       bool                `json:"-"`
	FreePKArea          bool                `json:"-"`
	MapMoveAt           int64               `json:"-"`
	TeleportRingAt      int64               `json:"-"`
	ObjectOrder         uint64              `json:"-"`
	AntiPoison          int                 `json:"anti_poison,omitempty"`
	ThrustingDisabled   bool                `json:"-"`
	HalfMoonDisabled    bool                `json:"-"`
	BonusPoint          int                 `json:"bonus_point,omitempty"`
	CreditPoint         int                 `json:"credit_point,omitempty"`
	BonusAbil           BonusAbility        `json:"bonus_abil,omitempty"`
	ExtraAbil           [6]uint16           `json:"-"`
	ExtraAbilTimes      [6]int64            `json:"-"`
	EquippedItems       map[int]UserItem    `json:"equipped_items,omitempty"`
	BagItems            []UserItem          `json:"bag_items"`
	StorageItems        []UserItem          `json:"storage_items"`
	Skills              SkillStates         `json:"skills,omitempty"`
	ItemSkillHotkeys    map[string]byte     `json:"item_skill_hotkeys,omitempty"`
	GroupOwnerID        string              `json:"group_owner_id,omitempty"`
	AllowGroup          bool                `json:"allow_group,omitempty"`
	AllowGroupRecall    bool                `json:"allow_group_recall,omitempty"`
	SlaveRelax          bool                `json:"-"`
	GroupRecallUntil    int64               `json:"group_recall_until,omitempty"`
	GroupMembers        []string            `json:"group_members,omitempty"`
	GuildID             string              `json:"guild_id,omitempty"`
	GuildRank           int                 `json:"guild_rank,omitempty"`
	GuildRankName       string              `json:"guild_rank_name,omitempty"`
	GuildNotice         string              `json:"guild_notice,omitempty"`
	GuildAllianceID     string              `json:"guild_alliance_id,omitempty"`
	GuildAllianceIDs    []string            `json:"guild_alliance_ids,omitempty"`
	GuildWarArea        bool                `json:"guild_war_area,omitempty"`
	AllowGuild          bool                `json:"-"`
	ChatPreferencesInit bool                `json:"-"`
	BanShout            bool                `json:"-"`
	BanGuildChat        bool                `json:"-"`
	WeaponUpgrade       *WeaponUpgradeState `json:"weapon_upgrade,omitempty"`
	DefenceUpUntil      int64               `json:"defence_up_until,omitempty"`
	MagDefenceUpUntil   int64               `json:"mag_defence_up_until,omitempty"`
	BubbleDefenceLevel  byte                `json:"bubble_defence_level,omitempty"`
	BubbleDefenceUntil  int64               `json:"bubble_defence_until,omitempty"`
	BubbleDefenceActive *bool               `json:"-"`
	PoisonHealthLevel   byte                `json:"poison_health_level,omitempty"`
	PoisonHealthPoint   int                 `json:"-"`
	PoisonHealthStartAt int64               `json:"poison_health_start_at,omitempty"`
	PoisonHealthUntil   int64               `json:"poison_health_until,omitempty"`
	PoisonHealthTickAt  int64               `json:"poison_health_tick_at,omitempty"`
	PoisonArmorLevel    byte                `json:"poison_armor_level,omitempty"`
	PoisonArmorStartAt  int64               `json:"poison_armor_start_at,omitempty"`
	PoisonArmorUntil    int64               `json:"poison_armor_until,omitempty"`
	TransparentUntil    int64               `json:"transparent_until,omitempty"`
	TransparentHideMode *bool               `json:"-"`
	ShowHPOpenAt        int64               `json:"show_hp_open_at,omitempty"`
	ShowHPDuration      int64               `json:"show_hp_duration,omitempty"`
	ShowHPUntil         int64               `json:"show_hp_until,omitempty"`
}

const characterStatusTimeFormat = "remaining_ms"

type characterJSON Character

func (ch Character) MarshalJSON() ([]byte, error) {
	b, err := json.Marshal(characterJSON(ch))
	if err != nil {
		return nil, err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	delete(raw, "bubble_defence_level")
	raw["status_time_format"] = json.RawMessage(`"` + characterStatusTimeFormat + `"`)
	now := time.Now().UnixNano()
	for key, deadline := range map[string]int64{
		"paralyzed_until":      ch.ParalyzedUntil,
		"defence_up_until":     ch.DefenceUpUntil,
		"mag_defence_up_until": ch.MagDefenceUpUntil,
		"bubble_defence_until": ch.BubbleDefenceUntil,
		"poison_health_until":  ch.PoisonHealthUntil,
		"poison_armor_until":   ch.PoisonArmorUntil,
		"transparent_until":    ch.TransparentUntil,
		"show_hp_until":        ch.ShowHPUntil,
	} {
		remaining := int64(0)
		if deadline > now {
			remaining = (deadline - now) / int64(time.Millisecond)
			if remaining == 0 {
				remaining = 1
			}
		}
		value, err := json.Marshal(remaining)
		if err != nil {
			return nil, err
		}
		raw[key] = value
	}
	for _, key := range []string{
		"inc_health_spell_at",
		"poison_health_level",
		"poison_armor_level",
		"poison_health_start_at",
		"poison_health_tick_at",
		"poison_armor_start_at",
		"show_hp_open_at",
		"show_hp_duration",
		"show_hp_until",
	} {
		raw[key] = json.RawMessage(`0`)
	}
	return json.Marshal(raw)
}

func (ch *Character) UnmarshalJSON(data []byte) error {
	var value characterJSON
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	*ch = Character(value)
	transparentHideMode := false
	ch.TransparentHideMode = &transparentHideMode
	bubbleDefenceActive := false
	ch.BubbleDefenceActive = &bubbleDefenceActive
	ch.BubbleDefenceLevel = 0
	var meta struct {
		StatusTimeFormat string `json:"status_time_format"`
	}
	if err := json.Unmarshal(data, &meta); err != nil {
		return err
	}
	if meta.StatusTimeFormat != characterStatusTimeFormat {
		return nil
	}
	ch.IncHealthSpellAt = 0
	ch.PoisonHealthLevel = 0
	ch.PoisonArmorLevel = 0
	ch.ShowHPUntil = 0
	now := time.Now()
	for _, remaining := range []*int64{
		&ch.ParalyzedUntil,
		&ch.DefenceUpUntil,
		&ch.MagDefenceUpUntil,
		&ch.BubbleDefenceUntil,
		&ch.PoisonHealthUntil,
		&ch.PoisonArmorUntil,
		&ch.TransparentUntil,
		&ch.ShowHPUntil,
	} {
		if *remaining > 0 {
			*remaining = now.Add(time.Duration(*remaining) * time.Millisecond).UnixNano()
		} else {
			*remaining = 0
		}
	}
	if ch.PoisonHealthUntil > 0 {
		ch.PoisonHealthStartAt = 0
		ch.PoisonHealthTickAt = now.UnixNano()
	}
	if ch.PoisonArmorUntil > 0 {
		ch.PoisonArmorStartAt = 0
	}
	return nil
}

type WeaponUpgradeState struct {
	Item      UserItem `json:"item"`
	NPCID     string   `json:"npc_id,omitempty"`
	StartedAt int64    `json:"started_at,omitempty"`
	BonusDC   byte     `json:"bonus_dc,omitempty"`
	BonusMC   byte     `json:"bonus_mc,omitempty"`
	BonusSC   byte     `json:"bonus_sc,omitempty"`
	BonusDura byte     `json:"bonus_dura,omitempty"`
}

type BonusAbility struct {
	DC    int `json:"dc,omitempty"`
	MC    int `json:"mc,omitempty"`
	SC    int `json:"sc,omitempty"`
	AC    int `json:"ac,omitempty"`
	MAC   int `json:"mac,omitempty"`
	HP    int `json:"hp,omitempty"`
	MP    int `json:"mp,omitempty"`
	Hit   int `json:"hit,omitempty"`
	Speed int `json:"speed,omitempty"`
	X2    int `json:"x2,omitempty"`
}

type UserItem struct {
	ItemID    string `json:"item_id"`
	MakeIndex int32  `json:"make_index,omitempty"`
	Dura      uint16 `json:"dura,omitempty"`
	DuraMax   uint16 `json:"dura_max,omitempty"`
	Desc      [14]byte
}

type SkillState struct {
	ID         string `json:"id"`
	Level      byte   `json:"level,omitempty"`
	Train      int    `json:"train,omitempty"`
	Hotkey     byte   `json:"hotkey,omitempty"`
	LastCastAt int64  `json:"last_cast_at,omitempty"`
	Locked     bool   `json:"locked,omitempty"`
}

type SkillStates []SkillState

func (ss SkillStates) Has(skillID string) bool {
	for _, state := range ss {
		if state.ID == skillID {
			return true
		}
	}
	return false
}

func (ss SkillStates) Get(skillID string) (SkillState, int, bool) {
	for i, state := range ss {
		if state.ID == skillID {
			return state, i, true
		}
	}
	return SkillState{}, -1, false
}

func (ss *SkillStates) Learn(skillID string) bool {
	if ss == nil {
		return false
	}
	if (*ss).Has(skillID) {
		return false
	}
	*ss = append(*ss, SkillState{ID: skillID})
	return true
}

func (ss SkillStates) IDs() []string {
	out := make([]string, 0, len(ss))
	for _, state := range ss {
		if state.ID == "" {
			continue
		}
		out = append(out, state.ID)
	}
	return out
}

func (ss SkillStates) MarshalJSON() ([]byte, error) {
	out := make([]SkillState, len(ss))
	copy(out, ss)
	return json.Marshal(out)
}

func (ss *SkillStates) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "null" {
		*ss = nil
		return nil
	}
	var raw []json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	out := make(SkillStates, 0, len(raw))
	for _, elem := range raw {
		if len(elem) == 0 || string(elem) == "null" {
			continue
		}
		var state SkillState
		if err := json.Unmarshal(elem, &state); err == nil && state.ID != "" {
			out = append(out, state)
			continue
		}
		var skillID string
		if err := json.Unmarshal(elem, &skillID); err == nil && skillID != "" {
			out = append(out, SkillState{ID: skillID})
			continue
		}
		return fmt.Errorf("invalid skill entry %s", string(elem))
	}
	*ss = out
	return nil
}

func Open(path string) (*Store, error) {
	s := &Store{path: path}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		s.db = database{
			Accounts:   map[string]Account{},
			Characters: map[string]Character{},
			Guilds:     map[string]Guild{},
			Castles:    map[string]Castle{},
			NextID:     1,
		}
		s.ensureDefaultAccounts()
		if err := s.saveLocked(); err != nil {
			return nil, err
		}
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &s.db); err != nil {
		return nil, err
	}
	if s.db.Accounts == nil {
		s.db.Accounts = map[string]Account{}
	}
	if s.db.Characters == nil {
		s.db.Characters = map[string]Character{}
	}
	if s.db.Guilds == nil {
		s.db.Guilds = map[string]Guild{}
	}
	if s.db.Castles == nil {
		s.db.Castles = map[string]Castle{}
	}
	if s.db.NextID == 0 {
		s.db.NextID = 1
	}
	if s.ensureDefaultAccounts() {
		if err := s.saveLocked(); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func (s *Store) Castle(id string) (Castle, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	castle, ok := s.db.Castles[id]
	return castle, ok
}

func (s *Store) SaveCastle(castle Castle) error {
	if castle.ID == "" {
		return errors.New("castle id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db.Castles == nil {
		s.db.Castles = map[string]Castle{}
	}
	previous, existed := s.db.Castles[castle.ID]
	s.db.Castles[castle.ID] = castle
	if err := s.saveLocked(); err != nil {
		if existed {
			s.db.Castles[castle.ID] = previous
		} else {
			delete(s.db.Castles, castle.ID)
		}
		return err
	}
	return nil
}

func (s *Store) Guild(id string) (Guild, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	guild, ok := s.db.Guilds[id]
	if !ok {
		return Guild{}, false
	}
	return cloneGuild(guild), true
}

func (s *Store) SaveGuild(guild Guild) error {
	if guild.ID == "" {
		return errors.New("guild id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db.Guilds == nil {
		s.db.Guilds = map[string]Guild{}
	}
	previous, existed := s.db.Guilds[guild.ID]
	s.db.Guilds[guild.ID] = guild
	if err := s.saveLocked(); err != nil {
		if existed {
			s.db.Guilds[guild.ID] = previous
		} else {
			delete(s.db.Guilds, guild.ID)
		}
		return err
	}
	return nil
}

func (s *Store) UpdateGuildNoticeBestEffort(guildID, notice string) error {
	if guildID == "" {
		return errors.New("guild id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	guild, ok := s.db.Guilds[guildID]
	if !ok {
		return errors.New("guild not found")
	}
	guild.Notice = notice
	s.db.Guilds[guildID] = guild
	return s.saveLocked()
}

func (s *Store) UpdateGuildRanksBestEffort(guildID string, ranks []GuildRank) error {
	if guildID == "" {
		return errors.New("guild id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	guild, ok := s.db.Guilds[guildID]
	if !ok {
		return errors.New("guild not found")
	}
	guild.Ranks = append([]GuildRank(nil), ranks...)
	for i := range guild.Ranks {
		guild.Ranks[i].Members = append([]string(nil), guild.Ranks[i].Members...)
	}
	s.db.Guilds[guildID] = guild
	return s.saveLocked()
}

func (s *Store) SetGuildAuthAllyRuntime(guildID string, enabled bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	guild, ok := s.db.Guilds[guildID]
	if !ok {
		return false
	}
	guild.EnableAuthAlly = enabled
	s.db.Guilds[guildID] = guild
	return true
}

func (s *Store) AddGuildWar(guildID, targetID string, duration ...time.Duration) error {
	if guildID == "" || targetID == "" {
		return errors.New("invalid guild war")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	guild, ok := s.db.Guilds[guildID]
	if !ok {
		return errors.New("guild not found")
	}
	target, ok := s.db.Guilds[targetID]
	if !ok {
		return errors.New("target guild not found")
	}
	if guild.Wars == nil {
		guild.Wars = map[string]bool{}
	}
	if target.Wars == nil {
		target.Wars = map[string]bool{}
	}
	previousGuild := cloneGuild(guild)
	previousTarget := cloneGuild(target)
	if guild.Wars[targetID] || target.Wars[guildID] {
		if len(duration) == 0 || duration[0] <= 0 {
			return errors.New("guild war already exists")
		}
	}
	guild.Wars[targetID] = true
	target.Wars[guildID] = true
	if len(duration) > 0 && duration[0] > 0 {
		if guild.WarExpires == nil {
			guild.WarExpires = map[string]time.Time{}
		}
		if target.WarExpires == nil {
			target.WarExpires = map[string]time.Time{}
		}
		expires := time.Now().Add(duration[0])
		guild.WarExpires[targetID] = expires
		target.WarExpires[guildID] = expires
	}
	guild.WarOrder = appendGuildWarOrder(guild.WarOrder, targetID)
	target.WarOrder = appendGuildWarOrder(target.WarOrder, guildID)
	s.db.Guilds[guildID] = guild
	s.db.Guilds[targetID] = target
	if err := s.saveLocked(); err != nil {
		s.db.Guilds[guildID] = previousGuild
		s.db.Guilds[targetID] = previousTarget
		return err
	}
	return nil
}

func (s *Store) ExpireGuildWars(now time.Time) ([]ExpiredGuildWar, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := false
	expired := make([]ExpiredGuildWar, 0)
	for guildID, guild := range s.db.Guilds {
		for targetID, expires := range guild.WarExpires {
			if expires.IsZero() || now.Before(expires) {
				continue
			}
			delete(guild.WarExpires, targetID)
			if guild.Wars != nil {
				delete(guild.Wars, targetID)
			}
			if targetID == guildID {
				expired = append(expired, ExpiredGuildWar{GuildID: guildID, TargetID: targetID})
				changed = true
				continue
			}
			if target, ok := s.db.Guilds[targetID]; ok {
				if target.Wars != nil {
					delete(target.Wars, guildID)
				}
				if target.WarExpires != nil {
					delete(target.WarExpires, guildID)
				}
				s.db.Guilds[targetID] = target
				expired = append(expired, ExpiredGuildWar{GuildID: targetID, TargetID: guildID})
			}
			expired = append(expired, ExpiredGuildWar{GuildID: guildID, TargetID: targetID})
			changed = true
		}
		s.db.Guilds[guildID] = guild
	}
	if !changed {
		return expired, nil
	}
	if err := s.saveLocked(); err != nil {
		return nil, err
	}
	return expired, nil
}

func (s *Store) GuildAtWar(guildID, targetID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	guild, ok := s.db.Guilds[guildID]
	return ok && guild.Wars[targetID]
}

func (s *Store) DisbandGuild(id string) ([]Character, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id == "" {
		return nil, errors.New("guild id is required")
	}
	guild, ok := s.db.Guilds[id]
	if !ok {
		return nil, errors.New("guild not found")
	}
	previousGuild := cloneGuild(guild)
	previousCharacters := make(map[string]Character)
	updated := make([]Character, 0)
	for key, ch := range s.db.Characters {
		if ch.GuildID != id {
			continue
		}
		previousCharacters[key] = ch
		ch.GuildID = ""
		ch.GuildRank = 0
		ch.GuildRankName = ""
		ch.GuildNotice = ""
		ch.GuildAllianceID = ""
		ch.GuildAllianceIDs = nil
		s.db.Characters[key] = ch
		updated = append(updated, ch)
	}
	delete(s.db.Guilds, id)
	if err := s.saveLocked(); err != nil {
		s.db.Guilds[id] = previousGuild
		for key, ch := range previousCharacters {
			s.db.Characters[key] = ch
		}
		return nil, err
	}
	return updated, nil
}

func (s *Store) DeleteGuild(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id == "" {
		return errors.New("guild id is required")
	}
	previous, ok := s.db.Guilds[id]
	if !ok {
		return errors.New("guild not found")
	}
	delete(s.db.Guilds, id)
	if err := s.saveLocked(); err != nil {
		s.db.Guilds[id] = previous
		return err
	}
	return nil
}

func (s *Store) DeleteGuildBestEffort(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id == "" {
		return errors.New("guild id is required")
	}
	if _, ok := s.db.Guilds[id]; !ok {
		return errors.New("guild not found")
	}
	delete(s.db.Guilds, id)
	return s.saveLocked()
}

func (s *Store) ensureDefaultAccounts() bool {
	if _, ok := s.db.Accounts["test"]; ok {
		return false
	}
	s.db.Accounts["test"] = Account{Username: "test", Password: "test"}
	return true
}

func (s *Store) Authenticate(username, password string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	acc, ok := s.db.Accounts[username]
	return ok && acc.Password == password
}

func (s *Store) Characters(account string) []Character {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []Character{}
	for _, ch := range s.db.Characters {
		if ch.Account == account && !ch.Deleted {
			out = append(out, ch)
		}
	}
	return out
}

func (s *Store) CharacterNameExists(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, ch := range s.db.Characters {
		if ch.Name == name {
			return true
		}
	}
	return false
}

func (s *Store) SelectCharacter(account, name string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	previous := make(map[string]Character, len(s.db.Characters))
	found := false
	for id, ch := range s.db.Characters {
		if ch.Account != account || ch.Deleted {
			continue
		}
		previous[id] = ch
		ch.Selected = ch.Name == name
		if ch.Selected {
			found = true
		}
		s.db.Characters[id] = ch
	}
	if !found {
		for id, ch := range previous {
			s.db.Characters[id] = ch
		}
		return false, nil
	}
	if err := s.saveLocked(); err != nil {
		for id, ch := range previous {
			s.db.Characters[id] = ch
		}
		return false, err
	}
	return true, nil
}

func (s *Store) DeleteCharacter(account, name string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var id string
	var previous Character
	for candidateID, ch := range s.db.Characters {
		if ch.Account == account && ch.Name == name {
			id = candidateID
			previous = ch
			break
		}
	}
	if id == "" {
		return false, nil
	}
	ch := previous
	ch.Deleted = true
	ch.Selected = false
	s.db.Characters[id] = ch
	if err := s.saveLocked(); err != nil {
		s.db.Characters[id] = previous
		return false, err
	}
	return true, nil
}

func (s *Store) InsertCharacter(ch Character) (Character, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.db.Characters {
		if existing.Name == ch.Name {
			return Character{}, fmt.Errorf("character name already exists")
		}
	}
	id := fmt.Sprintf("char-%d", s.db.NextID)
	previousNextID := s.db.NextID
	s.db.NextID++
	ch.ID = id
	normalizeCharacterForSave(&ch)
	s.db.Characters[id] = ch
	if err := s.saveLocked(); err != nil {
		delete(s.db.Characters, id)
		s.db.NextID = previousNextID
		return Character{}, err
	}
	return ch, nil
}

func (s *Store) Character(id string) (Character, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ch, ok := s.db.Characters[id]
	return ch, ok
}

func (s *Store) ExpireWeaponUpgrades(before time.Time) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff := before.UnixMilli()
	changed := make([]string, 0)
	previous := make(map[string]Character)
	for id, ch := range s.db.Characters {
		if ch.WeaponUpgrade == nil || ch.WeaponUpgrade.StartedAt <= 0 || ch.WeaponUpgrade.StartedAt > cutoff {
			continue
		}
		previous[id] = ch
		ch.WeaponUpgrade = nil
		s.db.Characters[id] = ch
		changed = append(changed, id)
	}
	if len(changed) == 0 {
		return nil, nil
	}
	if err := s.saveLocked(); err != nil {
		for id, ch := range previous {
			s.db.Characters[id] = ch
		}
		return nil, err
	}
	return changed, nil
}

func (s *Store) SaveCharacter(ch Character) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	normalizeCharacterForSave(&ch)
	previous, existed := s.db.Characters[ch.ID]
	s.db.Characters[ch.ID] = ch
	if err := s.saveLocked(); err != nil {
		if existed {
			s.db.Characters[ch.ID] = previous
		} else {
			delete(s.db.Characters, ch.ID)
		}
		return err
	}
	return nil
}

func (s *Store) SetCharacterRuntime(ch Character) error {
	if ch.ID == "" {
		return errors.New("character id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.db.Characters[ch.ID] = ch
	return nil
}

func (s *Store) SetCharacterRuntimePair(first, second Character) error {
	if first.ID == "" || second.ID == "" || first.ID == second.ID {
		return errors.New("distinct character ids are required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.db.Characters[first.ID] = first
	s.db.Characters[second.ID] = second
	return nil
}

func (s *Store) SetCharacterRuntimeBatch(characters []Character) error {
	if len(characters) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(characters))
	for _, character := range characters {
		if character.ID == "" {
			return errors.New("character id is required")
		}
		if _, ok := seen[character.ID]; ok {
			return errors.New("character ids must be distinct")
		}
		seen[character.ID] = struct{}{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, character := range characters {
		s.db.Characters[character.ID] = character
	}
	return nil
}

func (s *Store) SaveCharacterPair(first, second Character) error {
	if first.ID == "" || second.ID == "" || first.ID == second.ID {
		return errors.New("distinct character ids are required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	normalizeCharacterForSave(&first)
	normalizeCharacterForSave(&second)
	previousFirst, firstExists := s.db.Characters[first.ID]
	previousSecond, secondExists := s.db.Characters[second.ID]
	s.db.Characters[first.ID] = first
	s.db.Characters[second.ID] = second
	if err := s.saveLocked(); err != nil {
		if firstExists {
			s.db.Characters[first.ID] = previousFirst
		} else {
			delete(s.db.Characters, first.ID)
		}
		if secondExists {
			s.db.Characters[second.ID] = previousSecond
		} else {
			delete(s.db.Characters, second.ID)
		}
		return err
	}
	return nil
}

func (s *Store) SaveCharacterBatch(characters []Character) error {
	if len(characters) == 0 {
		return nil
	}
	previous := make(map[string]Character, len(characters))
	existed := make(map[string]bool, len(characters))
	for i := range characters {
		if characters[i].ID == "" {
			return errors.New("character id is required")
		}
		if _, ok := previous[characters[i].ID]; ok {
			return errors.New("character ids must be distinct")
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range characters {
		normalizeCharacterForSave(&characters[i])
		previous[characters[i].ID], existed[characters[i].ID] = s.db.Characters[characters[i].ID]
		s.db.Characters[characters[i].ID] = characters[i]
	}
	if err := s.saveLocked(); err != nil {
		for _, character := range characters {
			if existed[character.ID] {
				s.db.Characters[character.ID] = previous[character.ID]
			} else {
				delete(s.db.Characters, character.ID)
			}
		}
		return err
	}
	return nil
}

func (s *Store) UpdateGuildRanks(guildID string, ranks []GuildRank) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	previous := make(map[string]Character)
	for id, ch := range s.db.Characters {
		if ch.GuildID != guildID {
			continue
		}
		previous[id] = ch
		ch.GuildRank = 0
		ch.GuildRankName = ""
		for _, rank := range ranks {
			for _, member := range rank.Members {
				if strings.EqualFold(member, ch.Name) {
					ch.GuildRank = rank.Number
					ch.GuildRankName = rank.Name
				}
			}
		}
		s.db.Characters[id] = ch
	}
	if err := s.saveLocked(); err != nil {
		for id, ch := range previous {
			s.db.Characters[id] = ch
		}
		return err
	}
	return nil
}

func (s *Store) UpdateGuildNotice(guildID, notice string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	previous := make(map[string]Character)
	for id, ch := range s.db.Characters {
		if ch.GuildID != guildID {
			continue
		}
		previous[id] = ch
		ch.GuildNotice = notice
		s.db.Characters[id] = ch
	}
	if err := s.saveLocked(); err != nil {
		for id, ch := range previous {
			s.db.Characters[id] = ch
		}
		return err
	}
	return nil
}

func (s *Store) UpdateGuildAlliance(guildID, allianceID string) error {
	if allianceID == "" {
		return s.UpdateGuildAlliances(guildID, nil)
	}
	return s.UpdateGuildAlliances(guildID, []string{allianceID})
}

func (s *Store) UpdateGuildAlliances(guildID string, alliances []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	previous := make(map[string]Character)
	for id, ch := range s.db.Characters {
		if ch.GuildID != guildID {
			continue
		}
		previous[id] = ch
		ch.GuildAllianceIDs = append([]string(nil), alliances...)
		ch.GuildAllianceID = ""
		if len(alliances) > 0 {
			ch.GuildAllianceID = alliances[0]
		}
		s.db.Characters[id] = ch
	}
	if err := s.saveLocked(); err != nil {
		for id, ch := range previous {
			s.db.Characters[id] = ch
		}
		return err
	}
	return nil
}

func (s *Store) SaveGuildAlliancePair(guild, ally Guild, guildAlliances, allyAlliances []string) error {
	if guild.ID == "" || ally.ID == "" {
		return errors.New("guild alliance pair is invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	previousGuild, guildExists := s.db.Guilds[guild.ID]
	previousAlly, allyExists := s.db.Guilds[ally.ID]
	guild.Alliances = append([]string(nil), guildAlliances...)
	guild.Alliance = ""
	if len(guildAlliances) > 0 {
		guild.Alliance = guildAlliances[0]
	}
	ally.Alliances = append([]string(nil), allyAlliances...)
	ally.Alliance = ""
	if len(allyAlliances) > 0 {
		ally.Alliance = allyAlliances[0]
	}
	s.db.Guilds[guild.ID] = guild
	if guild.ID == ally.ID {
		return s.saveLocked()
	}
	s.db.Guilds[ally.ID] = ally
	if err := s.saveLocked(); err != nil {
		if guildExists {
			s.db.Guilds[guild.ID] = previousGuild
		} else {
			delete(s.db.Guilds, guild.ID)
		}
		if allyExists {
			s.db.Guilds[ally.ID] = previousAlly
		} else {
			delete(s.db.Guilds, ally.ID)
		}
		return err
	}
	return nil
}

func (s *Store) SaveGuildAlliancePairBestEffort(guild, ally Guild, guildAlliances, allyAlliances []string) error {
	if guild.ID == "" || ally.ID == "" {
		return errors.New("guild alliance pair is invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	guild.Alliances = append([]string(nil), guildAlliances...)
	guild.Alliance = ""
	if len(guildAlliances) > 0 {
		guild.Alliance = guildAlliances[0]
	}
	ally.Alliances = append([]string(nil), allyAlliances...)
	ally.Alliance = ""
	if len(allyAlliances) > 0 {
		ally.Alliance = allyAlliances[0]
	}
	s.db.Guilds[guild.ID] = guild
	if guild.ID == ally.ID {
		return s.saveLocked()
	}
	s.db.Guilds[ally.ID] = ally
	return s.saveLocked()
}

func cloneGuild(guild Guild) Guild {
	guild.Ranks = append([]GuildRank(nil), guild.Ranks...)
	for i := range guild.Ranks {
		guild.Ranks[i].Members = append([]string(nil), guild.Ranks[i].Members...)
	}
	guild.Alliances = append([]string(nil), guild.Alliances...)
	guild.WarOrder = append([]string(nil), guild.WarOrder...)
	if guild.WarExpires != nil {
		guild.WarExpires = make(map[string]time.Time, len(guild.WarExpires))
		for id, expires := range guild.WarExpires {
			guild.WarExpires[id] = expires
		}
	}
	if guild.Wars != nil {
		wars := make(map[string]bool, len(guild.Wars))
		for id, atWar := range guild.Wars {
			wars[id] = atWar
		}
		guild.Wars = wars
	}
	return guild
}

func appendGuildWarOrder(order []string, guildID string) []string {
	for _, existing := range order {
		if existing == guildID {
			return order
		}
	}
	return append(order, guildID)
}

func (s *Store) RemoveGuildMember(guildID, name string) (Character, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	guild, ok := s.db.Guilds[guildID]
	if !ok {
		return Character{}, false, errors.New("guild not found")
	}
	previousGuild := cloneGuild(guild)
	found := false
	removed := false
	for i := range guild.Ranks {
		filtered := guild.Ranks[i].Members[:0]
		for _, member := range guild.Ranks[i].Members {
			if !removed && member == name {
				found = true
				removed = true
				continue
			}
			filtered = append(filtered, member)
		}
		guild.Ranks[i].Members = filtered
		if removed {
			break
		}
	}
	if !found {
		return Character{}, false, nil
	}
	s.db.Guilds[guildID] = guild
	if err := s.saveLocked(); err != nil {
		s.db.Guilds[guildID] = previousGuild
		return Character{}, false, err
	}
	return Character{}, true, nil
}

func (s *Store) RemoveGuildMemberBestEffort(guildID, name string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	guild, ok := s.db.Guilds[guildID]
	if !ok {
		return false, errors.New("guild not found")
	}
	removed := false
	for i := range guild.Ranks {
		for j, member := range guild.Ranks[i].Members {
			if member != name {
				continue
			}
			guild.Ranks[i].Members = append(guild.Ranks[i].Members[:j], guild.Ranks[i].Members[j+1:]...)
			removed = true
			break
		}
		if removed {
			break
		}
	}
	if !removed {
		return false, nil
	}
	s.db.Guilds[guildID] = guild
	return true, s.saveLocked()
}

func (s *Store) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s.db, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, b, 0o600)
}

func normalizeCharacterForSave(ch *Character) {
	if ch.BagItems == nil {
		ch.BagItems = []UserItem{}
	}
	if ch.StorageItems == nil {
		ch.StorageItems = []UserItem{}
	}
	if ch.EquippedItems == nil {
		ch.EquippedItems = map[int]UserItem{}
	}
}
