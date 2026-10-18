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
	mu                                 sync.Mutex
	data                               data.StdBundle
	store                              *storage.Store
	monsters                           map[string]*Monster
	monsterCells                       map[monsterPosition]map[string]struct{}
	monsterCellByID                    map[string]monsterPosition
	occupied                           map[monsterPosition]string
	castleBlocked                      map[monsterPosition]struct{}
	drops                              map[string]GroundDrop
	mineCounts                         map[monsterPosition]int
	mineAddCounts                      map[monsterPosition]int
	mineDepletedAt                     map[monsterPosition]time.Time
	fireFields                         map[fireFieldKey]fireField
	groundEvents                       map[int32]SpellGroundEvent
	closedGroundEvents                 map[int32]SpellGroundEvent
	doorStates                         map[doorStateKey]doorState
	spawns                             map[string]*spawnState
	monsterProcessGroups               []string
	monsterProcessGroupByID            map[string]string
	monsterProcessGroupCursor          int
	monsterProcessMemberCursor         int
	visibleActors                      map[string][]string
	npcActors                          map[string]int32
	npcCombat                          map[string]*NPCCombatState
	npcFixedHides                      map[string]bool
	npcNextRunAt                       map[string]time.Time
	npcMerchantProcessCursor           int
	npcProcessCursor                   int
	merchantStocks                     map[string][]storage.UserItem
	castleRuntime                      map[string]storage.Castle
	dirtyCastles                       map[string]struct{}
	disconnectedCharacters             map[string]disconnectedCharacter
	pendingSpells                      []pendingSpell
	pendingMonsterSpawns               []pendingMonsterSpawn
	pendingMonsterAttacks              []pendingMonsterAttack
	nextPendingOrder                   uint64
	pendingCharacterDeaths             map[string]struct{}
	finalizedCharacterDeaths           map[string]struct{}
	characterDeathAt                   map[string]time.Time
	lastCharacterRevivalAt             map[string]int64
	groupVerifyAt                      map[string]int64
	characterGameTimes                 map[string]byte
	characterSearchAt                  map[string]time.Time
	characterCastleCheckAt             map[string]time.Time
	characterCastleTauntAt             map[string]time.Time
	characterStatusClocks              map[string]map[statusClockKind]statusClock
	monsterStatusClocks                map[string]map[statusClockKind]statusClock
	characterNextRunAt                 map[string]time.Time
	characterProcessCursor             int
	characterProcessBudget             time.Duration
	characterDuplicateCheckAt          map[string]time.Time
	characterDuplicateAt               map[string]time.Time
	nextID                             int
	nextItemID                         int32
	nextObjectOrder                    uint64
	nextMonsterActorID                 int32
	tickCharacterProcessOrders         map[string]uint64
	nextNPCID                          int32
	nextEventID                        int32
	nextFireFieldID                    uint64
	rand                               *rand.Rand
	gameplay                           config.Gameplay
	monsterTraceEnabled                bool
	monsterTraceTick                   int64
	monsterTraceCurrent                *MonsterTickTrace
	actionNow                          time.Time
	monsterProcessNextAt               time.Time
	monsterRegenAt                     time.Time
	monsterRegenCursor                 int
	monsterBehaviorHits                []AttackResult
	monsterBehaviorGroundHits          []AttackResult
	monsterBehaviorGroundHitStart      int
	monsterBehaviorStatuses            []Monster
	monsterBehaviorCharacterStatuses   []storage.Character
	monsterBehaviorPoisonNotifications []PoisonNotification
	monsterHealingOrder                map[string]uint64
}

const characterRunInterval = 250 * time.Millisecond
const characterProcessBudget = 30 * time.Millisecond
const monsterStageInterval = time.Millisecond
const monsterProcessBudget = 25 * time.Millisecond
const monsterProcessInterval = 2
const monsterRegenInterval = 20 * time.Millisecond

type pendingMonsterSpawn struct {
	EnqueueOrder uint64
	DueAt        time.Time
	ParentID     string
	ChildName    string
}

type pendingMonsterAttack struct {
	EnqueueOrder    uint64
	DueAt           time.Time
	MonsterID       string
	StruckCharacter *storage.Character
	StruckPlayers   []storage.Character
	Targets         []pendingMonsterTarget
	TargetIDs       []string
	MonsterTargets  []pendingMonsterTarget
	Damage          int
	ImpactDelay     time.Duration
}

func (w *World) nextPendingOrderLocked() uint64 {
	w.nextPendingOrder++
	if w.nextPendingOrder == 0 {
		w.nextPendingOrder++
	}
	return w.nextPendingOrder
}

func (w *World) enqueuePendingMonsterAttackLocked(event pendingMonsterAttack) {
	if event.EnqueueOrder == 0 {
		event.EnqueueOrder = w.nextPendingOrderLocked()
	} else if event.EnqueueOrder > w.nextPendingOrder {
		w.nextPendingOrder = event.EnqueueOrder
	}
	w.pendingMonsterAttacks = append(w.pendingMonsterAttacks, event)
}

func (w *World) enqueuePendingSpellLocked(event pendingSpell) {
	if event.EnqueueOrder == 0 {
		event.EnqueueOrder = w.nextPendingOrderLocked()
	} else if event.EnqueueOrder > w.nextPendingOrder {
		w.nextPendingOrder = event.EnqueueOrder
	}
	w.pendingSpells = append(w.pendingSpells, event)
}

type pendingMonsterTarget struct {
	ID                string
	Damage            int
	PhysicalDamage    int
	MagicDamage       int
	Monster           bool
	Order             uint64
	TargetX           int
	TargetY           int
	TargetRange       int
	ReferenceDelay    bool
	DirectMagicStrike bool
	StruckOnly        bool
}

func (w *World) CanSpellWhileParalyzed() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.gameplay.Combat.ParalyCanSpell
}

func (w *World) CanHitWhileParalyzed() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.gameplay.Combat.ParalyCanHit
}

type spawnState struct {
	spawn           data.StdSpawn
	activeCount     int
	nextMemberOrder uint64
	memberIDs       []string
	lastRegenAt     time.Time
}

type disconnectedCharacter struct {
	Character storage.Character
	Since     time.Time
	Until     time.Time
}

const disconnectedCharacterGrace = 5 * time.Minute

type doorStateKey struct {
	MapID string
	Group int
	X     int
	Y     int
}

type doorState struct {
	OpenedAt time.Time
}

type monsterPosition struct {
	MapID string
	X     int
	Y     int
}

type Monster struct {
	ID                      string
	TemplateID              string
	Name                    string
	Behavior                string
	Race                    int
	RaceImg                 int
	MonsterWeapon           int
	Appr                    int
	Light                   int
	Level                   int
	Undead                  int
	MapID                   string
	X                       int
	Y                       int
	Dir                     int
	TargetX                 int
	TargetY                 int
	CoolEye                 int
	ViewRange               int
	LeashRange              int
	SearchNoTargetMS        int
	SearchHasTargetMS       int
	HP                      int
	MaxHP                   int
	MP                      int
	MaxMP                   int
	MinAttack               int
	MaxAttack               int
	Defense                 int
	MagicDefense            int
	MagicDefenseMax         int
	AntiMagic               int
	AntiPoison              int
	MagicAttack             int
	MagicAttackMax          int
	TaoAttack               int
	Speed                   int
	Hit                     int
	WalkSpeedMS             int
	WalkStep                int
	WalkWait                int
	RunIntervalMS           int
	AttackIntervalMS        int
	Experience              int
	IncHealth               int
	IncSpell                int
	IncHealing              int
	PerHealing              int
	PerHealth               int
	PerSpell                int
	IncHealthSpellAt        int64
	HealthTick              int
	HealthTickAt            int64
	SpellTick               int
	SpellTickAt             int64
	DropTable               string
	CorpseItems             []storage.UserItem
	CorpseGold              int
	Alive                   bool
	RespawnAt               time.Time
	DeathAt                 time.Time
	GhostAt                 time.Time
	Ghost                   bool
	ZilkinKillCount         int
	ZilkinRebirth           bool
	Spawn                   data.StdSpawn
	Hidden                  bool
	FixedHideMode           bool
	AdminMode               bool
	StoneMode               bool
	Animal                  bool
	MeatQuality             int
	BodyLeathery            int
	Skeleton                bool
	NoTame                  bool
	NoItem                  bool
	WantRefMsg              bool
	FleeOnSight             bool
	RunAwayMode             bool
	RunAwayUntil            time.Time
	FirstRevealPending      bool
	GuardDirection          int
	CastleID                string
	CastleDoor              bool
	CastleWall              bool
	CastleWallIndex         int
	CastleDefenseArcher     bool
	CastleDefenseIndex      int
	CastleDefenseGuard      bool
	CastleTauntAt           time.Time
	AttackCount             int
	AttackMax               int
	UseMagic                bool
	AppearStartAt           time.Time
	ParentID                string
	ChildIDs                map[string]struct{}
	ExplosionStartAt        time.Time
	TargetCharacterID       string
	TargetMonsterID         string
	PendingDeath            bool
	DeathHitterID           string
	LastHitterID            string
	LastHitterAt            time.Time
	ExpHitterID             string
	ExpHitterAt             time.Time
	ObjectOrder             uint64
	ActorID                 int32
	SpawnMemberOrder        uint64
	TargetFocusAt           time.Time
	ThinkAt                 time.Time
	DupMode                 bool
	LastTargetSearchAt      time.Time
	CentipedeAttackAt       time.Time
	CentipedeHideAt         time.Time
	TransparentUntil        time.Time
	ShowHPOpenAt            int64
	ShowHPUntil             int64
	ShowHPDuration          int64
	LastAttackAt            time.Time
	LastWalkAt              time.Time
	WalkCount               int
	WalkWaitTick            time.Time
	WalkWaitLocked          bool
	NextSearchAt            time.Time
	NextVisibleSearchAt     time.Time
	VisibleSearchIntervalMS int
	ProcessRunCount         int
	NextRunAt               time.Time
	CowKingPhase            int
	CowKingState            int
	CowKingPhaseAt          time.Time
	CowKingStoredAttack     int
	CowKingStoredWalk       int
	CowKingMoveAt           time.Time
	StoneDangerLevel        int
	PoisonHealthLevel       byte
	PoisonHealthPoint       int
	PoisonHealthStartAt     time.Time
	PoisonHealthUntil       time.Time
	PoisonHealthTickAt      time.Time
	PoisonArmorLevel        byte
	PoisonArmorStartAt      time.Time
	PoisonArmorUntil        time.Time
	PoisonSourceID          string
	HolySeizeUntil          time.Time
	CrazyUntil              time.Time
	ParalyzedUntil          time.Time
	DefenceUpUntil          int64
	MagDefenceUpUntil       int64
	MasterID                string
	MasterName              string
	MasterExpiresAt         time.Time
	MasterTick              time.Time
	SlaveMakeLevel          byte
	SlaveExpLevel           byte
	SlaveExpProgress        int
	SlaveNameColor          uint16
	SlaveColorSet           bool
	BaseMaxHP               int
	BaseMinAttack           int
	BaseMaxAttack           int
	BaseWalkSpeedMS         int
	MasterDeadSince         time.Time
	MasterGhostSince        time.Time
}

type GroundDrop struct {
	ID                string
	ObjectOrder       uint64
	MapID             string
	X                 int
	Y                 int
	ItemID            string
	Count             int
	MakeIndex         int32
	Dura              uint16
	DuraMax           uint16
	OwnerID           string
	OwnerGroupID      string
	CreatedAt         time.Time
	PickupAt          time.Time
	Desc              [14]byte
	SourceCharacterID string
	SourceMapID       string
	SourceX           int
	SourceY           int
}

func (w *World) nextGroundEventIDLocked() int32 {
	w.nextEventID++
	return w.nextEventID
}

func (w *World) addGroundEventLocked(event SpellGroundEvent) {
	if event.RunTick <= 0 {
		event.RunTick = 500 * time.Millisecond
	}
	if event.ObjectOrder == 0 {
		w.nextObjectOrder++
		event.ObjectOrder = w.nextObjectOrder
	}
	w.groundEvents[event.ID] = event
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
	sort.Slice(events, func(i, j int) bool {
		return groundEventBefore(events[i], events[j])
	})
	return events
}

func groundEventBefore(left, right SpellGroundEvent) bool {
	if left.MapID != right.MapID {
		return left.MapID < right.MapID
	}
	if left.X != right.X {
		return left.X < right.X
	}
	if left.Y != right.Y {
		return left.Y < right.Y
	}
	if left.ObjectOrder != 0 || right.ObjectOrder != 0 {
		if left.ObjectOrder == 0 {
			return false
		}
		if right.ObjectOrder == 0 {
			return true
		}
		if left.ObjectOrder != right.ObjectOrder {
			return left.ObjectOrder < right.ObjectOrder
		}
	}
	return left.ID < right.ID
}

type ItemUseResult struct {
	Character                  storage.Character
	Consumed                   bool
	Teleport                   *TeleportEvent
	AbilityChanged             bool
	SubAbilityChanged          bool
	Durability                 *SpellDurability
	SystemMessage              string
	SystemMessages             []ItemUseSystemMessage
	SystemMessageEvents        []ItemUseSystemMessage
	SystemMessageBeforeAbility bool
	SkillChanged               bool
	HealthChanged              bool
	RemovedItems               []storage.UserItem
	SuppressRemovedSync        bool
	AddedItems                 []storage.UserItem
	AddedSkills                []storage.SkillState
	EnableThrusting            bool
	EnableHalfMoon             bool
	LevelUp                    bool
}

type ItemUseSystemMessage struct {
	Text       string
	Foreground byte
	Background byte
}

type ButcherResult struct {
	Character           storage.Character
	Monster             Monster
	AddedItem           storage.UserItem
	AddedItems          []storage.UserItem
	AddedItemCharacters []storage.Character
	Skeleton            bool
	FoundItem           bool
	NothingFound        bool
}

type EquipResult struct {
	Character     storage.Character
	SwappedOut    storage.UserItem
	HasSwappedOut bool
	AddedSkills   []storage.SkillState
	RemovedSkills []string
}

type UnequipResult struct {
	Character      storage.Character
	RemovedItem    storage.UserItem
	HasRemovedItem bool
	AddedSkills    []storage.SkillState
	RemovedSkills  []string
}

type AttackResult struct {
	MonsterID            string
	ActorID              int32
	Connected            bool
	WeaponDamageEligible bool
	Magic                bool
	MonsterHealthChanged bool
	ImpactDelay          time.Duration
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
	DeathDeferred        bool
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
	Durability           []SpellDurability
	UpdatedItems         []storage.UserItem
	DeletedItems         []storage.UserItem
	FeatureChanged       bool
	AbilityChanged       bool
	SubAbilityChanged    bool
	WeaponBroken         bool
	AddedItems           []storage.UserItem
	GroundEvents         []SpellGroundEvent
	HeavyHitFragment     bool
	HeavyHitMining       bool
	NPCSpeeches          []NPCSpeech
	NPCStruckHits        []NPCStruckHit
	NPCCombatEvents      []NPCCombatEvent
}

type NPCCombatState struct {
	DamageSum int
	HitCount  int
	LastHitAt time.Time
}

type NPCSpeech struct {
	NPC     npc.Entity
	ActorID int32
	Text    string
}

type NPCStruckHit struct {
	NPC        npc.Entity
	Damage     int
	AttackerID string
	Magic      bool
}

type NPCCombatEvent struct {
	Speech    NPCSpeech
	HasSpeech bool
	Struck    NPCStruckHit
	HasStruck bool
}

type NPCActionKind uint8

const (
	NPCActionTurn NPCActionKind = iota
	NPCActionHit
	NPCActionDisappear
	NPCActionReveal
)

type NPCAction struct {
	NPC  npc.Entity
	Kind NPCActionKind
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
	Character    storage.Character
	ProcessOrder uint64
}

type CharacterRunResult struct {
	Character  storage.Character
	EventStart int
	EventEnd   int
}

type TickResult struct {
	CharacterStageProcessed               bool
	PreMonsterCharacters                  []storage.Character
	RunCharacters                         []storage.Character
	CharacterRuns                         []CharacterRunResult
	MonsterActions                        []MonsterAction
	OrderedMonsterEvents                  []OrderedMonsterEvent
	MonsterTraces                         []MonsterTickTrace
	CharacterHits                         []CharacterHit
	CharacterDeaths                       []storage.Character
	CharacterGhosts                       []storage.Character
	CharacterDrops                        []GroundDrop
	PKDeathMessages                       []PKDeathMessage
	CharacterRevivals                     []CharacterRevival
	MonsterHits                           []AttackResult
	NPCActions                            []NPCAction
	NPCSpeeches                           []NPCSpeech
	NPCStruckHits                         []NPCStruckHit
	NPCCombatEvents                       []NPCCombatEvent
	MonsterDeaths                         []AttackResult
	RespawnedMonsters                     []Monster
	AffectedMonsters                      []Monster
	NameMonsters                          []Monster
	SpawnedMonsters                       []Monster
	NameColorMonsters                     []Monster
	NameColorCharacters                   []storage.Character
	AffectedCharacters                    []storage.Character
	StateRefreshCharacters                []storage.Character
	StatusRefreshCharacters               []storage.Character
	StatusRefreshMonsters                 []Monster
	PostActionMonsterStatusRefreshes      []Monster
	PostActionMonsterHealthRefreshes      []Monster
	PostActionMonsterCloseHealthRefreshes []Monster
	OrderedStatusRefreshes                []StatusRefreshEvent
	AbilityRefreshCharacters              []storage.Character
	ShowHPOpenedCharacters                []storage.Character
	ShowHPExpiredCharacters               []storage.Character
	ShowHPOpenedMonsters                  []Monster
	ShowHPExpiredMonsters                 []Monster
	HealingCharacters                     []string
	OrderedHealthSpellChanges             []storage.Character
	RuntimeCharacters                     []storage.Character
	GroundEvents                          []SpellGroundEvent
	GroundEventHides                      []int32
	GroundEventHideDetails                []SpellGroundEvent
	Characters                            []storage.Character
	SpellExperience                       []SpellExperience
	GroupSyncEvents                       []GroupSyncEvent
	PoisonNotifications                   []PoisonNotification
	OrderedSpellEvents                    []OrderedSpellEvent
}

type PKDeathMessage struct {
	Character storage.Character
	Text      string
}

type MonsterTraceState struct {
	MapID             string `json:"map_id"`
	X                 int    `json:"x"`
	Y                 int    `json:"y"`
	Dir               int    `json:"dir"`
	TargetCharacterID string `json:"target_character_id"`
	TargetX           int    `json:"target_x"`
	TargetY           int    `json:"target_y"`
	RunAwayMode       bool   `json:"run_away_mode"`
	Alive             bool   `json:"alive"`
	Hidden            bool   `json:"hidden"`
	FixedHideMode     bool   `json:"fixed_hide_mode"`
	AdminMode         bool   `json:"admin_mode"`
	LastWalkAtMS      int64  `json:"last_walk_at_ms"`
	WalkWaitLocked    bool   `json:"walk_wait_locked"`
	WalkWaitTickMS    int64  `json:"walk_wait_tick_ms"`
	WalkCount         int    `json:"walk_count"`
	ThinkAtMS         int64  `json:"think_at_ms"`
	DupMode           bool   `json:"dup_mode"`
}

type MonsterRandomTrace struct {
	Label string `json:"label"`
	Limit int    `json:"limit"`
	Value int    `json:"value"`
}

type MonsterTickTrace struct {
	Tick        int64                `json:"tick"`
	NowMS       int64                `json:"now_ms"`
	MonsterID   string               `json:"monster_id"`
	Decision    string               `json:"decision"`
	StateBefore MonsterTraceState    `json:"state_before"`
	StateAfter  MonsterTraceState    `json:"state_after"`
	Random      []MonsterRandomTrace `json:"random"`
	Actions     []MonsterAction      `json:"actions"`
}

func (w *World) EnableMonsterTrace(enabled bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.monsterTraceEnabled = enabled
	if !enabled {
		w.monsterTraceCurrent = nil
	}
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
	OrderedSpellEventCharacterPush
	OrderedSpellEventCharacterNotice
	OrderedSpellEventCharacterDrop
	OrderedSpellEventCharacterDeath
	OrderedSpellEventPKDeathNotice
	OrderedSpellEventCharacterGroupSync
	OrderedSpellEventCharacterRevival
	OrderedSpellEventCharacterCloseHealth
	OrderedSpellEventCharacterAbility
	OrderedSpellEventCharacterNameColor
	OrderedSpellEventCharacterHealth
	OrderedSpellEventCharacterExperience
	OrderedSpellEventCharacterSnapshot
	OrderedSpellEventCharacterDayChanging
)

type OrderedSpellEvent struct {
	Kind               OrderedSpellEventKind
	Character          storage.Character
	Monster            Monster
	CharacterHit       CharacterHit
	MonsterHit         AttackResult
	PoisonNotification PoisonNotification
	CharacterPush      CharacterPush
	CharacterNotice    CharacterNotice
	CharacterDrop      GroundDrop
	CharacterDeath     storage.Character
	PKDeathNotice      PKDeathMessage
	CharacterGroupSync GroupSyncEvent
	CharacterRevival   CharacterRevival
	Experience         SpellExperience
	CharacterSnapshot  storage.Character
	GameTime           byte
	DayBright          byte
}

type CharacterNotice struct {
	Character storage.Character
	Text      string
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
	ActorID       int32
	Name          string
	RaceImg       int
	MonsterWeapon int
	Appr          int
	Light         int
	MapID         string
	X             int
	Y             int
	Dir           int
	Status        int32
	Kind          MonsterActionKind
	PreviousMapID string
	PreviousX     int
	PreviousY     int
	TargetActor   int32
	TargetX       int
	TargetY       int
	TargetEffects []MonsterTargetEffect
}

type MonsterTargetEffect struct {
	CharacterID string
	X           int
	Y           int
	Type        int
}

type OrderedMonsterEventKind uint8

const (
	OrderedMonsterEventAction OrderedMonsterEventKind = iota + 1
	OrderedMonsterEventSpawn
	OrderedMonsterEventHealth
	OrderedMonsterEventStatus
	OrderedMonsterEventCloseHealth
	OrderedMonsterEventOpenHealth
	OrderedMonsterEventNameColor
	OrderedMonsterEventUsername
	OrderedMonsterEventHit
	OrderedMonsterEventCharacterHit
	OrderedMonsterEventCharacterStatus
	OrderedMonsterEventCharacterPoisonNotice
	OrderedMonsterEventExperience
	OrderedMonsterEventDeath
)

type OrderedMonsterEvent struct {
	Kind               OrderedMonsterEventKind
	Action             MonsterAction
	Monster            Monster
	Hit                AttackResult
	CharacterHit       CharacterHit
	Character          storage.Character
	PoisonNotification PoisonNotification
	Experience         SpellExperience
	Death              AttackResult
}

type MonsterActionKind int

const (
	MonsterActionWalk MonsterActionKind = iota + 1
	MonsterActionHit
	MonsterActionFlyAxe
	MonsterActionLighting
	MonsterActionTurn
	MonsterActionReveal
	MonsterActionHide
	MonsterActionPush
	MonsterActionSpaceMove
	MonsterActionCastleTurn
	MonsterActionCastleDig
)

type CharacterHit struct {
	Character                storage.Character
	Connected                bool
	WeaponDamageEligible     bool
	Magic                    bool
	Damage                   int
	StruckDamage             int
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

type CharacterRevival struct {
	Character    storage.Character
	Durabilities []SpellDurability
	RemovedItems []storage.UserItem
	Message      string
}

func New(bundle data.StdBundle, store *storage.Store, gameplayConfig ...config.Gameplay) *World {
	gameplay := config.DefaultGameplay()
	if len(gameplayConfig) > 0 {
		gameplay = gameplayConfig[0]
	}
	bundle = normalizeStdBundle(bundle)
	w := &World{
		data:                     bundle,
		store:                    store,
		monsters:                 map[string]*Monster{},
		monsterCells:             map[monsterPosition]map[string]struct{}{},
		monsterCellByID:          map[string]monsterPosition{},
		occupied:                 map[monsterPosition]string{},
		castleBlocked:            map[monsterPosition]struct{}{},
		drops:                    map[string]GroundDrop{},
		mineCounts:               map[monsterPosition]int{},
		mineAddCounts:            map[monsterPosition]int{},
		mineDepletedAt:           map[monsterPosition]time.Time{},
		fireFields:               map[fireFieldKey]fireField{},
		groundEvents:             map[int32]SpellGroundEvent{},
		closedGroundEvents:       map[int32]SpellGroundEvent{},
		doorStates:               map[doorStateKey]doorState{},
		spawns:                   map[string]*spawnState{},
		monsterProcessGroupByID:  map[string]string{},
		visibleActors:            map[string][]string{},
		npcActors:                map[string]int32{},
		npcCombat:                map[string]*NPCCombatState{},
		npcFixedHides:            map[string]bool{},
		npcNextRunAt:             map[string]time.Time{},
		merchantStocks:           map[string][]storage.UserItem{},
		castleRuntime:            map[string]storage.Castle{},
		dirtyCastles:             map[string]struct{}{},
		disconnectedCharacters:   map[string]disconnectedCharacter{},
		pendingCharacterDeaths:   map[string]struct{}{},
		finalizedCharacterDeaths: map[string]struct{}{},
		characterDeathAt:         map[string]time.Time{},
		lastCharacterRevivalAt:   map[string]int64{},
		groupVerifyAt:            map[string]int64{},
		characterGameTimes:       map[string]byte{},
		characterSearchAt:        map[string]time.Time{},
		characterCastleCheckAt:   map[string]time.Time{},
		characterCastleTauntAt:   map[string]time.Time{},
		characterStatusClocks:    map[string]map[statusClockKind]statusClock{},
		monsterStatusClocks:      map[string]map[statusClockKind]statusClock{},
		characterNextRunAt:       map[string]time.Time{},
		characterProcessBudget:   characterProcessBudget,
		nextID:                   1,
		nextItemID:               itemMakeIndexStart,
		nextObjectOrder:          1,
		nextMonsterActorID:       500000,
		nextNPCID:                300000,
		nextEventID:              400000,
		rand:                     rand.New(rand.NewSource(time.Now().UnixNano())),
		gameplay:                 gameplay,
	}
	w.characterDuplicateCheckAt = map[string]time.Time{}
	w.characterDuplicateAt = map[string]time.Time{}
	w.initNPCActors()
	w.spawnInitial()
	w.spawnCastleDoor()
	w.spawnCastleWalls()
	w.spawnCastleDefenders()
	return w
}

func (w *World) SendRefMsgRange() int {
	if w.gameplay.Combat.SendRefMsgRange <= 0 {
		return 12
	}
	return w.gameplay.Combat.SendRefMsgRange
}

func (w *World) RegisterCharacter(ch storage.Character) storage.Character {
	w.mu.Lock()
	defer w.mu.Unlock()
	now := time.Now()
	delete(w.disconnectedCharacters, ch.ID)
	delete(w.characterGameTimes, ch.ID)
	delete(w.characterSearchAt, ch.ID)
	delete(w.characterCastleCheckAt, ch.ID)
	delete(w.characterDuplicateCheckAt, ch.ID)
	delete(w.characterDuplicateAt, ch.ID)
	if w.characterNextRunAt == nil {
		w.characterNextRunAt = make(map[string]time.Time)
	}
	w.characterNextRunAt[ch.ID] = now
	if w.characterDuplicateCheckAt == nil {
		w.characterDuplicateCheckAt = make(map[string]time.Time)
	}
	w.characterDuplicateCheckAt[ch.ID] = now
	w.applyStealthRingStateLocked(&ch, now)
	w.refreshCharacterObjectOrderLocked(&ch)
	return ch
}

func (w *World) ClearDisconnectedCharacter(id string) {
	if id == "" {
		return
	}
	w.mu.Lock()
	delete(w.disconnectedCharacters, id)
	delete(w.characterNextRunAt, id)
	delete(w.characterGameTimes, id)
	delete(w.characterSearchAt, id)
	delete(w.characterCastleCheckAt, id)
	delete(w.characterDuplicateCheckAt, id)
	delete(w.characterDuplicateAt, id)
	w.mu.Unlock()
}

func (w *World) HandleCharacterDisconnect(ch storage.Character, now time.Time) {
	if ch.ID == "" {
		return
	}
	ch.Ghost = true
	w.mu.Lock()
	w.disconnectedCharacters[ch.ID] = disconnectedCharacter{Character: ch, Since: now, Until: now.Add(disconnectedCharacterGrace)}
	w.mu.Unlock()
}

func (w *World) reviveCharacterLocked(ch *storage.Character, now time.Time) (CharacterRevival, bool, error) {
	if ch == nil || ch.ID == "" || ch.HP > 0 {
		return CharacterRevival{}, false, nil
	}
	last := w.lastCharacterRevivalAt[ch.ID]
	revivalTime := w.gameplay.Recovery.RevivalTimeMS
	if revivalTime <= 0 {
		revivalTime = 60 * 1000
	}
	if last > 0 && now.UnixMilli()-last <= int64(revivalTime) {
		return CharacterRevival{}, false, nil
	}
	durabilities := make([]SpellDurability, 0)
	removedItems := make([]storage.UserItem, 0)
	revivalFound := false
	if ch.EquippedItems != nil {
		items := make(map[int]storage.UserItem, len(ch.EquippedItems))
		for slot, entry := range ch.EquippedItems {
			items[slot] = entry
		}
		ch.EquippedItems = items
	}
	for slot := 0; slot < useSlotCount; slot++ {
		entry, equipped := ch.EquippedItems[slot]
		if !equipped || entry.ItemID == "" || entry.Dura <= 0 {
			continue
		}
		item, ok := w.data.Items[entry.ItemID]
		if ok && revivalAbilityItem(item, slot) {
			revivalFound = true
			break
		}
	}
	if !revivalFound {
		return CharacterRevival{}, false, nil
	}
	for slot := 0; slot < useSlotCount; slot++ {
		entry, equipped := ch.EquippedItems[slot]
		if !equipped || entry.ItemID == "" {
			continue
		}
		item, ok := w.data.Items[entry.ItemID]
		if !ok || !revivalDamageItem(item, slot) {
			continue
		}
		oldDisplayDura := referenceRound(float64(entry.Dura) / 1000)
		removedItem := entry
		if entry.Dura <= 1000 {
			entry.Dura = 0
			delete(ch.EquippedItems, slot)
			removedItem.Dura = 0
			removedItems = append(removedItems, removedItem)
		} else {
			entry.Dura -= 1000
			ch.EquippedItems[slot] = entry
		}
		if oldDisplayDura != referenceRound(float64(entry.Dura)/1000) {
			durabilities = append(durabilities, SpellDurability{Slot: slot, Dura: entry.Dura, DuraMax: entry.DuraMax})
		}
	}
	ch.HP = ch.MaxHP
	w.lastCharacterRevivalAt[ch.ID] = now.UnixMilli()
	return CharacterRevival{
		Character:    *ch,
		Durabilities: durabilities,
		RemovedItems: removedItems,
		Message:      "复活戒指恢复有效，生命恢复。",
	}, true, nil
}

func (w *World) characterHasUnrevivalItemLocked(ch storage.Character) bool {
	for slot, entry := range ch.EquippedItems {
		if entry.ItemID == "" || entry.Dura <= 0 {
			continue
		}
		item, ok := w.data.Items[entry.ItemID]
		if !ok {
			continue
		}
		if item.Shape == 144 && slot != SlotDress && slot != SlotWeapon && slot != SlotRightHand {
			return true
		}
		if (slot == SlotDress || slot == SlotWeapon || slot == SlotRightHand) && item.AniCount == 144 {
			return true
		}
	}
	return false
}

func revivalAbilityItem(item data.StdItem, slot int) bool {
	if slot != SlotDress && slot != SlotWeapon && slot != SlotRightHand && (item.Shape == 114 || item.Shape == 160 || item.Shape == 161 || item.Shape == 162) {
		return true
	}
	return (slot == SlotDress || slot == SlotWeapon || slot == SlotRightHand) &&
		(item.AniCount == 114 || item.AniCount == 160 || item.AniCount == 161 || item.AniCount == 162)
}

func revivalDamageItem(item data.StdItem, slot int) bool {
	if item.Shape == 114 || item.Shape == 160 || item.Shape == 161 || item.Shape == 162 {
		return true
	}
	return (slot == SlotWeapon || slot == SlotRightHand) &&
		(item.AniCount == 114 || item.AniCount == 160 || item.AniCount == 161 || item.AniCount == 162)
}

func revivalItem(item data.StdItem, slot int) bool {
	return revivalAbilityItem(item, slot)
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

func (w *World) isCharacterDeadLocked(id string) bool {
	if id == "" {
		return false
	}
	_, pending := w.pendingCharacterDeaths[id]
	_, finalized := w.finalizedCharacterDeaths[id]
	return pending || finalized
}

func (w *World) characterDeadLocked(ch storage.Character) bool {
	return ch.HP <= 0
}

func (w *World) IsCharacterDead(id string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.isCharacterDeadLocked(id)
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
		w.npcNextRunAt[id] = time.Now().Add(time.Duration(w.rand.Intn(1500)) * time.Millisecond)
		if npc.IsTrainer(w.data.NPCs.Entities[id]) {
			w.npcCombat[id] = &NPCCombatState{}
		}
		w.initMerchantStockLocked(id)
	}
}

func (w *World) Gameplay() config.Gameplay {
	return w.gameplay
}

func (w *World) RandomIntn(n int) int {
	if n <= 0 {
		return 0
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.rand.Intn(n)
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
			if mon.Race == 107 || mon.Behavior == "centipede_king" {
				mon.ViewRange = 6
			}
		}
		switch mon.Race {
		case 95:
			mon.ViewRange = 7
		case 96:
			mon.ViewRange = 6
		case 100:
			mon.ViewRange = 6
		case 85:
			mon.ViewRange = 7
		case 101:
			mon.ViewRange = 7
		case 102:
			mon.ViewRange = 8
		case 105, 106:
			mon.ViewRange = 7
		case 103, 116:
			mon.ViewRange = 9
		case 104:
			mon.ViewRange = 5
		case 115:
			mon.ViewRange = 16
		case 112:
			mon.ViewRange = 12
		}
		if mon.LeashRange <= 0 {
			mon.LeashRange = 15
		}
		if mon.SearchNoTargetMS <= 0 {
			if mon.Race == 97 {
				mon.SearchNoTargetMS = 1500
			} else {
				mon.SearchNoTargetMS = 1000
			}
		}
		if mon.SearchHasTargetMS <= 0 {
			mon.SearchHasTargetMS = 8000
		}
		if mon.WalkSpeedMS <= 0 {
			mon.WalkSpeedMS = 800
		}
		if mon.AttackIntervalMS <= 0 {
			mon.AttackIntervalMS = 2000
		}
		if mon.Race == 93 && mon.AttackMax <= 0 {
			mon.AttackMax = 3
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
		return nil
	}
	stocks = make([]storage.UserItem, 0, len(entity.Merchant.Stock))
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
			entry := storage.UserItem{ItemID: stock.ItemID, MakeIndex: w.nextItemMakeIndexLocked(), DuraMax: duraMax, Dura: duraMax}
			stocks = append(stocks, entry)
		}
	}
	w.merchantStocks[npcID] = stocks
	return stocks
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
	item.MakeIndex = w.nextItemMakeIndexLocked()
	stocks = append(stocks, item)
	w.merchantStocks[npcID] = stocks
}

func (w *World) AddMerchantStockFromItemID(npcID, itemID string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	item, ok := w.data.Items[itemID]
	if !ok {
		return false
	}
	stocks := w.initMerchantStockLocked(npcID)
	stocks = append(stocks, w.createUserItemFromStd(item, 0, [14]byte{}))
	w.merchantStocks[npcID] = stocks
	return true
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

func NPCDisplayName(entity npc.Entity) string {
	return filterDisplayName(entity.Name)
}

func NPCNameColor(entity npc.Entity) uint16 {
	return 255
}
