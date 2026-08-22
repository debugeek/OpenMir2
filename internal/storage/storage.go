package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
	ID               string           `json:"id"`
	Account          string           `json:"account"`
	Name             string           `json:"name"`
	Class            string           `json:"class"`
	Hair             int              `json:"hair"`
	Sex              int              `json:"sex"`
	Level            int              `json:"level"`
	Experience       int              `json:"experience"`
	HomeMap          string           `json:"home_map,omitempty"`
	HomeX            int              `json:"home_x,omitempty"`
	HomeY            int              `json:"home_y,omitempty"`
	MapID            string           `json:"map_id"`
	X                int              `json:"x"`
	Y                int              `json:"y"`
	Dir              int              `json:"dir"`
	Sitting          bool             `json:"sitting"`
	HP               int              `json:"hp"`
	MaxHP            int              `json:"max_hp"`
	MP               int              `json:"mp"`
	MaxMP            int              `json:"max_mp"`
	IncHealth        int              `json:"inc_health,omitempty"`
	IncSpell         int              `json:"inc_spell,omitempty"`
	IncHealing       int              `json:"inc_healing,omitempty"`
	IncHealthSpellAt int64            `json:"inc_health_spell_at,omitempty"`
	Gold             int              `json:"gold,omitempty"`
	PremiumGold      int              `json:"game_gold,omitempty"`
	PremiumPoint     int              `json:"game_point,omitempty"`
	AttackMode       int              `json:"attack_mode,omitempty"`
	BonusPoint       int              `json:"bonus_point,omitempty"`
	BonusAbil        BonusAbility     `json:"bonus_abil,omitempty"`
	ExtraAbil        [7]uint16        `json:"extra_abil,omitempty"`
	ExtraAbilTimes   [7]int64         `json:"extra_abil_times,omitempty"`
	SoftVersionDate  int              `json:"soft_version_date,omitempty"`
	EquippedItems    map[int]UserItem `json:"equipped_items,omitempty"`
	BagItems         []UserItem       `json:"bag_items"`
	StorageItems     []UserItem       `json:"storage_items"`
	Skills           []string         `json:"skills,omitempty"`
	GroupOwnerID     string           `json:"group_owner_id,omitempty"`
	AllowGroup       bool             `json:"allow_group,omitempty"`
	GroupMembers     []string         `json:"group_members,omitempty"`
}

type BonusAbility struct {
	DC       int `json:"dc,omitempty"`
	MC       int `json:"mc,omitempty"`
	SC       int `json:"sc,omitempty"`
	AC       int `json:"ac,omitempty"`
	MAC      int `json:"mac,omitempty"`
	HP       int `json:"hp,omitempty"`
	MP       int `json:"mp,omitempty"`
	Hit      int `json:"hit,omitempty"`
	Speed    int `json:"speed,omitempty"`
	Reserved int `json:"reserved,omitempty"`
}

// UserItem stores the persistent item fields used by equipped, bag, and storage containers.
type UserItem struct {
	ItemID    string `json:"item_id"`
	MakeIndex int32  `json:"make_index,omitempty"`
	Index     uint16 `json:"index,omitempty"`
	Dura      uint16 `json:"dura,omitempty"`
	DuraMax   uint16 `json:"dura_max,omitempty"`
	Desc      [14]byte
	ColorR    byte
	ColorG    byte
	ColorB    byte
	Prefix    [13]byte
}

func ItemDescBytes(desc string) [14]byte {
	var out [14]byte
	copy(out[:], []byte(desc))
	return out
}

func ItemDescString(desc [14]byte) string {
	return string(desc[:])
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
