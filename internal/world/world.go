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
	mu                     sync.Mutex
	data                   data.StdBundle
	store                  *storage.Store
	monsters               map[string]*Monster
	occupied               map[monsterPosition]string
	drops                  map[string]GroundDrop
	fireFields             map[fireFieldKey]fireField
	groundEvents           map[int32]SpellGroundEvent
	spawns                 map[string]*spawnState
	npcActors              map[string]int32
	merchantStocks         map[string][]storage.UserItem
	merchantNextID         map[string]int32
	pendingSpells          []pendingSpell
	pendingCharacterDeaths map[string]struct{}
	nextID                 int
	nextObjectOrder        uint64
	nextNPCID              int32
	nextEventID            int32
	nextFireFieldID        uint64
	rand                   *rand.Rand
	gameplay               config.Gameplay
}

func (w *World) CanSpellWhileParalyzed() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.gameplay.Combat.ParalyCanSpell
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
	ID                  string
	TemplateID          string
	Name                string
	Race                int
	RaceImg             int
	MonsterWeapon       int
	Appr                int
	Level               int
	Undead              int
	MapID               string
	X                   int
	Y                   int
	Dir                 int
	TargetX             int
	TargetY             int
	CoolEye             int
	ViewRange           int
	LeashRange          int
	SearchNoTargetMS    int
	SearchHasTargetMS   int
	HP                  int
	MaxHP               int
	MP                  int
	MaxMP               int
	MinAttack           int
	MaxAttack           int
	Defense             int
	MagicDefense        int
	MagicDefenseMax     int
	AntiMagic           int
	AntiPoison          int
	MagicAttack         int
	TaoAttack           int
	Speed               int
	Hit                 int
	WalkSpeedMS         int
	WalkStep            int
	WalkWait            int
	AttackIntervalMS    int
	Experience          int
	IncHealing          int
	PerHealing          int
	IncHealthSpellAt    int64
	DropTable           string
	Alive               bool
	RespawnAt           time.Time
	Spawn               data.StdSpawn
	Hidden              bool
	FixedHideMode       bool
	AdminMode           bool
	StoneMode           bool
	Animal              bool
	NoTame              bool
	FleeOnSight         bool
	RunAwayMode         bool
	RunAwayUntil        time.Time
	FirstRevealPending  bool
	GuardDirection      int
	AttackCount         int
	AttackMax           int
	UseMagic            bool
	AppearStartAt       time.Time
	ParentID            string
	ExplosionStartAt    time.Time
	TargetCharacterID   string
	PendingDeath        bool
	DeathHitterID       string
	LastHitterID        string
	LastHitterAt        time.Time
	ExpHitterID         string
	ExpHitterAt         time.Time
	ObjectOrder         uint64
	TargetFocusAt       time.Time
	TransparentUntil    time.Time
	ShowHPOpenAt        int64
	ShowHPUntil         int64
	ShowHPDuration      int64
	LastAttackAt        time.Time
	LastWalkAt          time.Time
	WalkCount           int
	WalkWaitTick        time.Time
	WalkWaitLocked      bool
	NextSearchAt        time.Time
	PoisonHealthLevel   byte
	PoisonHealthStartAt time.Time
	PoisonHealthUntil   time.Time
	PoisonHealthTickAt  time.Time
	PoisonArmorLevel    byte
	PoisonArmorStartAt  time.Time
	PoisonArmorUntil    time.Time
	PoisonSourceID      string
	HolySeizeUntil      time.Time
	CrazyUntil          time.Time
	ParalyzedUntil      time.Time
	DefenceUpUntil      int64
	MagDefenceUpUntil   int64
	MasterID            string
	MasterName          string
	MasterExpiresAt     time.Time
	MasterTick          time.Time
	SlaveMakeLevel      byte
	MasterDeadSince     time.Time
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

func (w *World) nextGroundEventIDLocked() int32 {
	w.nextEventID++
	return w.nextEventID
}

func (w *World) GroundEventsAround(mapID string, x, y, radius int, now time.Time) []SpellGroundEvent {
	w.mu.Lock()
	defer w.mu.Unlock()
	events := make([]SpellGroundEvent, 0, len(w.groundEvents))
	for _, event := range w.groundEvents {
		if event.MapID != mapID || now.After(event.StartAt.Add(event.Duration)) || abs(event.X-x) > radius || abs(event.Y-y) > radius {
			continue
		}
		events = append(events, event)
	}
	sort.Slice(events, func(i, j int) bool { return events[i].ID < events[j].ID })
	return events
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
	MonsterID            string
	Connected            bool
	Magic                bool
	MonsterHealthChanged bool
	ImpactDelay          time.Duration
	ImmediateImpact      bool
	Damage               int
	MonsterHP            int
	MonsterMaxHP         int
	MonsterMP            int
	MonsterMaxMP         int
	MonsterRaceImg       int
	MonsterWeapon        int
	MonsterAppr          int
	MonsterX             int
	MonsterY             int
	MonsterMapID         string
	MonsterDir           int
	MonsterStatus        int32
	Dead                 bool
	Experience           int
	CurrentExp           int
	LevelUp              bool
	SkillChanged         bool
	SkillExp             bool
	SkillLevelUp         bool
	SkillMagicID         uint16
	SkillLevel           byte
	SkillTrain           int
	SkillExpDelay        time.Duration
	SkillExperiences     []AttackSkillExperience
	Drops                []GroundDrop
	CharacterHits        []CharacterHit
	MonsterHits          []AttackResult
	Character            storage.Character
}

type AttackSkillExperience struct {
	MagicID uint16
	Level   byte
	Train   int
	Delay   time.Duration
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
	CharacterDeaths          []storage.Character
	MonsterHits              []AttackResult
	MonsterDeaths            []AttackResult
	AffectedMonsters         []Monster
	NameMonsters             []Monster
	NameColorMonsters        []Monster
	NameColorCharacters      []storage.Character
	AffectedCharacters       []storage.Character
	StateRefreshCharacters   []storage.Character
	StatusRefreshCharacters  []storage.Character
	StatusRefreshMonsters    []Monster
	OrderedStatusRefreshes   []StatusRefreshEvent
	AbilityRefreshCharacters []storage.Character
	ShowHPOpenedCharacters   []storage.Character
	ShowHPExpiredCharacters  []storage.Character
	ShowHPOpenedMonsters     []Monster
	ShowHPExpiredMonsters    []Monster
	HealingCharacters        []string
	GroundEvents             []SpellGroundEvent
	GroundEventHides         []int32
	Characters               []storage.Character
	SpellExperience          []SpellExperience
	PoisonNotifications      []PoisonNotification
	OrderedSpellEvents       []OrderedSpellEvent
}

type OrderedSpellEventKind uint8

const (
	OrderedSpellEventCharacterStatus OrderedSpellEventKind = iota + 1
	OrderedSpellEventMonsterStatus
	OrderedSpellEventCharacterOpenHealth
	OrderedSpellEventMonsterOpenHealth
	OrderedSpellEventCharacterHit
	OrderedSpellEventMonsterHit
	OrderedSpellEventPoisonNotification
)

type OrderedSpellEvent struct {
	Kind               OrderedSpellEventKind
	Character          storage.Character
	Monster            Monster
	CharacterHit       CharacterHit
	MonsterHit         AttackResult
	PoisonNotification PoisonNotification
}

type StatusRefreshEvent struct {
	Character *storage.Character
	Monster   *Monster
}

type SpellExperience struct {
	CharacterID string
	Experience  int
	CurrentExp  int
	LevelUp     bool
	Character   storage.Character
}

type PoisonNotification struct {
	Character storage.Character
	Seconds   int
	Points    int
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
	Status        int32
	Kind          MonsterActionKind
}

type MonsterActionKind int

const (
	MonsterActionWalk MonsterActionKind = iota + 1
	MonsterActionHit
	MonsterActionTurn
	MonsterActionReveal
	MonsterActionHide
	MonsterActionPush
)

type CharacterHit struct {
	Character                storage.Character
	Connected                bool
	Magic                    bool
	Damage                   int
	Durability               []SpellDurability
	DeletedItems             []storage.UserItem
	FeatureChanged           bool
	ImpactDelay              time.Duration
	AttackerID               string
	AttackerActor            int32
	AttackerRaceImg          int
	AttackerAppr             int
	AttackerX                int
	AttackerY                int
	AttackerNameColorChanged bool
	Dead                     bool
	DeathDeferred            bool
}

func New(bundle data.StdBundle, store *storage.Store, gameplayConfig ...config.Gameplay) *World {
	gameplay := config.DefaultGameplay()
	if len(gameplayConfig) > 0 {
		gameplay = gameplayConfig[0]
	}
	bundle = normalizeStdBundle(bundle)
	w := &World{
		data:                   bundle,
		store:                  store,
		monsters:               map[string]*Monster{},
		occupied:               map[monsterPosition]string{},
		drops:                  map[string]GroundDrop{},
		fireFields:             map[fireFieldKey]fireField{},
		groundEvents:           map[int32]SpellGroundEvent{},
		spawns:                 map[string]*spawnState{},
		npcActors:              map[string]int32{},
		merchantStocks:         map[string][]storage.UserItem{},
		merchantNextID:         map[string]int32{},
		pendingCharacterDeaths: map[string]struct{}{},
		nextID:                 1,
		nextObjectOrder:        1,
		nextNPCID:              300000,
		nextEventID:            400000,
		rand:                   rand.New(rand.NewSource(1)),
		gameplay:               gameplay,
	}
	w.initNPCActors()
	w.spawnInitial()
	return w
}

func (w *World) RegisterCharacter(ch storage.Character) storage.Character {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.refreshCharacterObjectOrderLocked(&ch)
	return ch
}

func (w *World) deferCharacterDeathLocked(ch storage.Character) {
	if ch.ID == "" || ch.HP > 0 {
		return
	}
	if w.pendingCharacterDeaths == nil {
		w.pendingCharacterDeaths = map[string]struct{}{}
	}
	w.pendingCharacterDeaths[ch.ID] = struct{}{}
}

func (w *World) refreshCharacterObjectOrderLocked(ch *storage.Character) {
	if w.nextObjectOrder == 0 {
		return
	}
	ch.ObjectOrder = w.nextObjectOrder
	w.nextObjectOrder++
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
