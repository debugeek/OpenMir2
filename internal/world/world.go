package world

import (
	"math/rand"
	"sync"
	"time"

	"openmir2/internal/config"
	"openmir2/internal/data"
	"openmir2/internal/storage"
)

type World struct {
	mu       sync.Mutex
	data     data.StdBundle
	store    *storage.Store
	monsters map[string]*Monster
	occupied map[monsterPosition]string
	drops    map[string]GroundDrop
	spawns   map[string]*spawnState
	nextID   int
	rand     *rand.Rand
	gameplay config.Gameplay
}

type spawnState struct {
	spawn       data.StdSpawn
	activeCount int
}

type monsterPosition struct {
	MapID string
	X     int
	Y     int
}

type Monster struct {
	ID                 string
	TemplateID         string
	Name               string
	Race               int
	RaceImg            int
	MonsterWeapon      int
	Appr               int
	Level              int
	Undead             int
	MapID              string
	X                  int
	Y                  int
	Dir                int
	TargetX            int
	TargetY            int
	CoolEye            int
	ViewRange          int
	LeashRange         int
	SearchNoTargetMS   int
	SearchHasTargetMS  int
	HP                 int
	MaxHP              int
	MP                 int
	MaxMP              int
	MinAttack          int
	MaxAttack          int
	Defense            int
	MagicDefense       int
	MagicAttack        int
	TaoAttack          int
	Speed              int
	Hit                int
	WalkSpeedMS        int
	WalkStep           int
	WalkWait           int
	AttackIntervalMS   int
	Experience         int
	DropTable          string
	Alive              bool
	RespawnAt          time.Time
	Spawn              data.StdSpawn
	Hidden             bool
	FixedHideMode      bool
	StoneMode          bool
	Animal             bool
	FleeOnSight        bool
	RunAwayMode        bool
	FirstRevealPending bool
	GuardDirection     int
	AttackCount        int
	AttackMax          int
	UseMagic           bool
	AppearStartAt      time.Time
	ParentID           string
	ExplosionStartAt   time.Time
	TargetCharacterID  string
	TargetFocusAt      time.Time
	LastAttackAt       time.Time
	LastWalkAt         time.Time
	WalkCount          int
	WalkWaitTick       time.Time
	WalkWaitLocked     bool
	NextSearchAt       time.Time
}

type GroundDrop struct {
	ID        string
	MapID     string
	X         int
	Y         int
	ItemID    string
	Count     int
	MakeIndex int32
	Dura      uint16
	DuraMax   uint16
	OwnerID   string
	PickupAt  time.Time
	Desc      [14]byte
}

type ItemUseResult struct {
	Character      storage.Character
	Consumed       bool
	AbilityChanged bool
	AddedItems     []storage.UserItem
	Experience     int
	CurrentExp     int
	LevelUp        bool
}

type AttackResult struct {
	MonsterID      string
	Damage         int
	MonsterHP      int
	MonsterMaxHP   int
	MonsterRaceImg int
	MonsterWeapon  int
	MonsterAppr    int
	MonsterX       int
	MonsterY       int
	MonsterDir     int
	Dead           bool
	Experience     int
	CurrentExp     int
	LevelUp        bool
	Drops          []GroundDrop
	Character      storage.Character
}

type SpawnResult struct {
	Monsters []Monster
}

type PlayerSnapshot struct {
	Character storage.Character
}

type TickResult struct {
	MonsterActions []MonsterAction
	CharacterHits  []CharacterHit
	Characters     []storage.Character
}

type MonsterAction struct {
	MonsterID     string
	Name          string
	RaceImg       int
	MonsterWeapon int
	Appr          int
	MapID         string
	X             int
	Y             int
	Dir           int
	Kind          MonsterActionKind
}

type MonsterActionKind int

const (
	MonsterActionWalk MonsterActionKind = iota + 1
	MonsterActionHit
	MonsterActionTurn
	MonsterActionReveal
	MonsterActionHide
)

type CharacterHit struct {
	Character       storage.Character
	Damage          int
	AttackerID      string
	AttackerRaceImg int
	AttackerAppr    int
	AttackerX       int
	AttackerY       int
	Dead            bool
}

func New(bundle data.StdBundle, store *storage.Store, gameplayConfig ...config.Gameplay) *World {
	gameplay := config.DefaultGameplay()
	if len(gameplayConfig) > 0 {
		gameplay = gameplayConfig[0]
	}
	bundle = normalizeStdBundle(bundle)
	w := &World{
		data:     bundle,
		store:    store,
		monsters: map[string]*Monster{},
		occupied: map[monsterPosition]string{},
		drops:    map[string]GroundDrop{},
		spawns:   map[string]*spawnState{},
		nextID:   1,
		rand:     rand.New(rand.NewSource(1)),
		gameplay: gameplay,
	}
	w.spawnInitial()
	return w
}

func normalizeStdBundle(bundle data.StdBundle) data.StdBundle {
	for id, mp := range bundle.Maps {
		if mp.MonsterSpawnRate <= 0 {
			mp.MonsterSpawnRate = 10
		}
		bundle.Maps[id] = mp
	}
	for id, mon := range bundle.Monsters {
		if mon.ViewRange <= 0 {
			mon.ViewRange = 5
		}
		if mon.LeashRange <= 0 {
			mon.LeashRange = 15
		}
		if mon.SearchNoTargetMS <= 0 {
			mon.SearchNoTargetMS = 1000
		}
		if mon.SearchHasTargetMS <= 0 {
			mon.SearchHasTargetMS = 8000
		}
		if mon.WalkSpeedMS <= 0 {
			mon.WalkSpeedMS = 800
		}
		if mon.AttackIntervalMS <= 0 {
			mon.AttackIntervalMS = 1800
		}
		bundle.Monsters[id] = mon
	}
	return bundle
}
