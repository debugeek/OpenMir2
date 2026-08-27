package world

import (
	"math/rand"
	"sort"
	"strings"
	"sync"
	"time"

	"openmir2/internal/config"
	"openmir2/internal/data"
	"openmir2/internal/npc"
	"openmir2/internal/storage"
)

type World struct {
	mu             sync.Mutex
	data           data.StdBundle
	store          *storage.Store
	monsters       map[string]*Monster
	occupied       map[monsterPosition]string
	drops          map[string]GroundDrop
	fireFields     map[fireFieldKey]fireField
	spawns         map[string]*spawnState
	npcActors      map[string]int32
	merchantStocks map[string][]storage.UserItem
	merchantNextID map[string]int32
	nextID         int
	nextNPCID      int32
	rand           *rand.Rand
	gameplay       config.Gameplay
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
	PoisonHealthLevel  byte
	PoisonHealthStartAt time.Time
	PoisonHealthUntil  time.Time
	PoisonHealthTickAt time.Time
	PoisonArmorLevel   byte
	PoisonArmorStartAt time.Time
	PoisonArmorUntil   time.Time
	PoisonSourceID     string
	DefenceUpUntil     int64
	MagDefenceUpUntil  int64
	MasterID           string
	MasterExpiresAt    time.Time
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
	Teleport       *TeleportEvent
	AbilityChanged bool
	SkillChanged   bool
	HealthChanged  bool
	RemovedItems   []storage.UserItem
	AddedItems     []storage.UserItem
	Experience     int
	CurrentExp     int
	LevelUp        bool
}

type EquipResult struct {
	Character     storage.Character
	SwappedOut    storage.UserItem
	HasSwappedOut bool
}

type UnequipResult struct {
	Character      storage.Character
	RemovedItem    storage.UserItem
	HasRemovedItem bool
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
	SkillChanged   bool
	Drops          []GroundDrop
	CharacterHits  []CharacterHit
	Character      storage.Character
}

type SpawnResult struct {
	Monsters []Monster
}

type PlayerSnapshot struct {
	Character storage.Character
}

type TickResult struct {
	MonsterActions           []MonsterAction
	CharacterHits            []CharacterHit
	MonsterHits              []AttackResult
	StateRefreshCharacters   []storage.Character
	AbilityRefreshCharacters []storage.Character
	ShowHPOpenedCharacters   []storage.Character
	ShowHPExpiredCharacters  []storage.Character
	Characters               []storage.Character
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
		data:           bundle,
		store:          store,
		monsters:       map[string]*Monster{},
		occupied:       map[monsterPosition]string{},
		drops:          map[string]GroundDrop{},
		fireFields:     map[fireFieldKey]fireField{},
		spawns:         map[string]*spawnState{},
		npcActors:      map[string]int32{},
		merchantStocks: map[string][]storage.UserItem{},
		merchantNextID: map[string]int32{},
		nextID:         1,
		nextNPCID:      300000,
		rand:           rand.New(rand.NewSource(1)),
		gameplay:       gameplay,
	}
	w.initNPCActors()
	w.spawnInitial()
	return w
}

func (w *World) initNPCActors() {
	ids := make([]string, 0, len(w.data.NPCs.Entities))
	for id := range w.data.NPCs.Entities {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		w.npcActors[id] = w.nextNPCID
		w.nextNPCID++
		w.initMerchantStockLocked(id)
	}
}

func (w *World) Gameplay() config.Gameplay {
	return w.gameplay
}

func normalizeStdBundle(bundle data.StdBundle) data.StdBundle {
	if len(bundle.ItemOrder) == 0 && len(bundle.Items) > 0 {
		ids := make([]string, 0, len(bundle.Items))
		for id := range bundle.Items {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		bundle.ItemOrder = ids
	}
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

func (w *World) NPCActorID(id string) int32 {
	if actorID, ok := w.npcActors[id]; ok {
		return actorID
	}
	return 300000
}

func (w *World) initMerchantStockLocked(npcID string) []storage.UserItem {
	stocks, ok := w.merchantStocks[npcID]
	if ok {
		return stocks
	}
	entity, ok := w.data.NPCs.Entities[npcID]
	if !ok || len(entity.Merchant.Stock) == 0 {
		w.merchantStocks[npcID] = nil
		w.merchantNextID[npcID] = 1
		return nil
	}
	stocks = make([]storage.UserItem, 0, len(entity.Merchant.Stock))
	nextID := int32(1)
	for _, stock := range entity.Merchant.Stock {
		if stock.Count <= 0 {
			continue
		}
		item, ok := w.data.Items[stock.ItemID]
		if !ok {
			continue
		}
		duraMax := itemDuraMax(item)
		for i := 0; i < stock.Count; i++ {
			entry := storage.UserItem{ItemID: stock.ItemID, MakeIndex: nextID, DuraMax: duraMax, Dura: duraMax}
			stocks = append(stocks, entry)
			nextID++
		}
	}
	w.merchantStocks[npcID] = stocks
	w.merchantNextID[npcID] = nextID
	return stocks
}

func (w *World) nextMerchantStockIndexLocked(npcID string) int32 {
	nextID, ok := w.merchantNextID[npcID]
	if !ok || nextID <= 0 {
		nextID = int32(len(w.merchantStocks[npcID])) + 1
		if nextID <= 0 {
			nextID = 1
		}
	}
	w.merchantNextID[npcID] = nextID + 1
	return nextID
}

func (w *World) MerchantStock(npcID string) []storage.UserItem {
	w.mu.Lock()
	defer w.mu.Unlock()
	stocks := w.initMerchantStockLocked(npcID)
	out := make([]storage.UserItem, len(stocks))
	copy(out, stocks)
	return out
}

func (w *World) ConsumeMerchantStock(npcID string, makeIndex int32, itemID string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	stocks := w.initMerchantStockLocked(npcID)
	for i := range stocks {
		if makeIndex > 0 && stocks[i].MakeIndex != makeIndex {
			continue
		}
		if itemID != "" && !sameItemID(stocks[i].ItemID, itemID) {
			continue
		}
		stocks = append(stocks[:i], stocks[i+1:]...)
		w.merchantStocks[npcID] = stocks
		return true
	}
	return false
}

func (w *World) AddMerchantStock(npcID string, item storage.UserItem) {
	if item.ItemID == "" {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	stocks := w.initMerchantStockLocked(npcID)
	if item.DuraMax == 0 {
		if std, ok := w.data.Items[item.ItemID]; ok {
			item.DuraMax = itemDuraMax(std)
		}
	}
	if item.Dura == 0 && item.DuraMax > 0 {
		item.Dura = item.DuraMax
	}
	item.MakeIndex = w.nextMerchantStockIndexLocked(npcID)
	stocks = append(stocks, item)
	w.merchantStocks[npcID] = stocks
}

func sameItemID(a, b string) bool {
	return strings.EqualFold(a, b)
}

func (w *World) NPCByActorID(actorID int32) (npc.Entity, bool) {
	for id, mapped := range w.npcActors {
		if mapped == actorID {
			entity, ok := w.data.NPCs.Entities[id]
			return entity, ok
		}
	}
	return npc.Entity{}, false
}

func (w *World) NPCFeature(entity npc.Entity) int32 {
	raceImg := entity.RaceImg
	if raceImg == 0 {
		raceImg = 50
	}
	return int32(uint32(raceImg&0xFF) | uint32(entity.MonsterWeapon&0xFF)<<8 | uint32(entity.Appr&0xFFFF)<<16)
}
