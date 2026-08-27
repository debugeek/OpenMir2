package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type Store struct {
	path string
	mu   sync.Mutex
	db   database
}

type database struct {
	Accounts   map[string]Account   `json:"accounts"`
	Characters map[string]Character `json:"characters"`
	NextID     int                  `json:"next_id"`
}

type Account struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type Character struct {
	ID                 string              `json:"id"`
	Account            string              `json:"account"`
	Name               string              `json:"name"`
	Class              string              `json:"class"`
	Hair               int                 `json:"hair"`
	Sex                int                 `json:"sex"`
	Level              int                 `json:"level"`
	Experience         int                 `json:"experience"`
	HomeMap            string              `json:"home_map,omitempty"`
	HomeX              int                 `json:"home_x,omitempty"`
	HomeY              int                 `json:"home_y,omitempty"`
	MapID              string              `json:"map_id"`
	X                  int                 `json:"x"`
	Y                  int                 `json:"y"`
	Dir                int                 `json:"dir"`
	Sitting            bool                `json:"sitting"`
	HP                 int                 `json:"hp"`
	MaxHP              int                 `json:"max_hp"`
	MP                 int                 `json:"mp"`
	MaxMP              int                 `json:"max_mp"`
	IncHealth          int                 `json:"inc_health,omitempty"`
	IncSpell           int                 `json:"inc_spell,omitempty"`
	IncHealing         int                 `json:"inc_healing,omitempty"`
	IncHealthSpellAt   int64               `json:"inc_health_spell_at,omitempty"`
	Gold               int                 `json:"gold,omitempty"`
	PremiumGold        int                 `json:"game_gold,omitempty"`
	PremiumPoint       int                 `json:"game_point,omitempty"`
	AttackMode         int                 `json:"attack_mode,omitempty"`
	BonusPoint         int                 `json:"bonus_point,omitempty"`
	BonusAbil          BonusAbility        `json:"bonus_abil,omitempty"`
	ExtraAbil          [7]uint16           `json:"extra_abil,omitempty"`
	ExtraAbilTimes     [7]int64            `json:"extra_abil_times,omitempty"`
	SoftVersionDate    int                 `json:"soft_version_date,omitempty"`
	EquippedItems      map[int]UserItem    `json:"equipped_items,omitempty"`
	BagItems           []UserItem          `json:"bag_items"`
	StorageItems       []UserItem          `json:"storage_items"`
	Skills             SkillStates         `json:"skills,omitempty"`
	GroupOwnerID       string              `json:"group_owner_id,omitempty"`
	AllowGroup         bool                `json:"allow_group,omitempty"`
	GroupMembers       []string            `json:"group_members,omitempty"`
	WeaponUpgrade      *WeaponUpgradeState `json:"weapon_upgrade,omitempty"`
	DefenceUpUntil     int64               `json:"defence_up_until,omitempty"`
	MagDefenceUpUntil  int64               `json:"mag_defence_up_until,omitempty"`
	BubbleDefenceLevel byte                `json:"bubble_defence_level,omitempty"`
	BubbleDefenceUntil int64               `json:"bubble_defence_until,omitempty"`
	PoisonHealthLevel  byte                `json:"poison_health_level,omitempty"`
	PoisonHealthStartAt int64              `json:"poison_health_start_at,omitempty"`
	PoisonHealthUntil  int64               `json:"poison_health_until,omitempty"`
	PoisonHealthTickAt int64               `json:"poison_health_tick_at,omitempty"`
	PoisonArmorLevel   byte                `json:"poison_armor_level,omitempty"`
	PoisonArmorStartAt int64               `json:"poison_armor_start_at,omitempty"`
	PoisonArmorUntil   int64               `json:"poison_armor_until,omitempty"`
	TransparentUntil   int64               `json:"transparent_until,omitempty"`
	ShowHPOpenAt       int64               `json:"show_hp_open_at,omitempty"`
	ShowHPUntil        int64               `json:"show_hp_until,omitempty"`
}

type WeaponUpgradeState struct {
	Item      UserItem `json:"item"`
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
		if ch.Account == account {
			out = append(out, ch)
		}
	}
	return out
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
	s.db.NextID++
	ch.ID = id
	normalizeCharacterForSave(&ch)
	s.db.Characters[id] = ch
	if err := s.saveLocked(); err != nil {
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

func (s *Store) SaveCharacter(ch Character) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	normalizeCharacterForSave(&ch)
	s.db.Characters[ch.ID] = ch
	return s.saveLocked()
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
