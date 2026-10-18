package world

import (
	"fmt"
	"math/rand"
	"path/filepath"
	"testing"
	"time"

	"openmir2/internal/data"
	"openmir2/internal/storage"
)

func lifecycleWorld() *World {
	return New(data.StdBundle{
		Maps:     map[string]data.StdMap{"m": {ID: "m", Width: 20, Height: 20, MonsterSpawnRate: 10}},
		Monsters: map[string]data.StdMonster{"rat": {ID: "rat", Name: "rat", HP: 10, MP: 1, MaxAttack: 1, ViewRange: 6}},
		Spawns:   []data.StdSpawn{{ID: "s", MapID: "m", MonsterID: "rat", X: 5, Y: 5, Count: 1, Range: 0, RespawnSeconds: 1}},
	}, nil)
}

func TestNewMonsterUsesReferenceInitialRunDelay(t *testing.T) {
	w := lifecycleWorld()
	before := time.Now()
	mon := newMonster(w, "initial-delay", data.StdMonster{ID: "rat", Name: "rat", HP: 10, MP: 1}, "m", 5, 5, data.StdSpawn{MapID: "m", MonsterID: "rat"})
	after := time.Now()
	if mon.NextRunAt.Before(before) || mon.NextRunAt.After(after.Add(1500*time.Millisecond)) {
		t.Fatalf("NextRunAt = %v, want creation time plus 0..1499ms", mon.NextRunAt)
	}
	if mon.LastAttackAt.IsZero() || mon.LastAttackAt.Before(before.Add(-4*time.Second)) || mon.LastAttackAt.After(after) {
		t.Fatalf("LastAttackAt = %v, want creation time minus 0..2999ms", mon.LastAttackAt)
	}
}

func TestNewMonsterUsesReferenceInitialMana(t *testing.T) {
	w := lifecycleWorld()
	mon := newMonster(w, "initial-mana", data.StdMonster{ID: "mage", HP: 10, MP: 37}, "m", 5, 5, data.StdSpawn{})
	if mon.MP != 0 || mon.MaxMP != 37 {
		t.Fatalf("initial monster mana = %d/%d, want 0/37", mon.MP, mon.MaxMP)
	}
}

func TestMonsterRespawnRestoresReferenceInitialMana(t *testing.T) {
	w := lifecycleWorld()
	now := time.Unix(100, 0)
	spawn := data.StdSpawn{ID: "respawn-mana", MapID: "m", MonsterID: "mana-monster", X: 5, Y: 5}
	w.data.Monsters[spawn.MonsterID] = data.StdMonster{ID: spawn.MonsterID, Name: spawn.MonsterID, HP: 40, MP: 17}
	mon := newMonster(w, "respawn-mana-monster", w.data.Monsters[spawn.MonsterID], "m", 5, 5, spawn)
	mon.MP = 13
	mon.MaxMP = 29
	mon.Alive = false
	mon.RespawnAt = now
	w.monsters[mon.ID] = mon
	w.respawnLocked(now, nil, nil)
	if mon.MP != 0 || mon.MaxMP != 17 {
		t.Fatalf("respawn monster mana = %d/%d, want 0/17", mon.MP, mon.MaxMP)
	}
}

func TestMonsterRespawnSnapshotFollowsReferenceInitializationOrder(t *testing.T) {
	w := lifecycleWorld()
	now := time.Unix(100, 0)
	spawn := data.StdSpawn{ID: "respawn-snapshot", MapID: "m", MonsterID: "snapshot-monster", X: 5, Y: 5}
	w.data.Monsters[spawn.MonsterID] = data.StdMonster{ID: spawn.MonsterID, Name: spawn.MonsterID, HP: 40, MP: 17}
	mon := newMonster(w, "respawn-snapshot-monster", w.data.Monsters[spawn.MonsterID], "m", 5, 5, spawn)
	mon.TargetCharacterID = "old-target"
	mon.LastHitterID = "old-hitter"
	mon.PoisonHealthLevel = 3
	mon.Alive = false
	mon.RespawnAt = now
	w.monsters[mon.ID] = mon
	result := TickResult{}
	w.respawnLocked(now, nil, &result)
	if len(result.RespawnedMonsters) != 1 {
		t.Fatalf("respawn snapshots = %d, want 1", len(result.RespawnedMonsters))
	}
	snapshot := result.RespawnedMonsters[0]
	if snapshot.TargetCharacterID != "" || snapshot.LastHitterID != "" || snapshot.PoisonHealthLevel != 0 {
		t.Fatalf("respawn snapshot retained old state: %+v", snapshot)
	}
}

func TestMonsterRespawnRestoresReferenceRecoveryState(t *testing.T) {
	w := lifecycleWorld()
	now := time.Unix(100, 0)
	spawn := data.StdSpawn{ID: "respawn-recovery", MapID: "m", MonsterID: "recovery-monster", X: 5, Y: 5}
	w.data.Monsters[spawn.MonsterID] = data.StdMonster{ID: spawn.MonsterID, Name: spawn.MonsterID, HP: 40, MP: 17}
	mon := newMonster(w, "respawn-recovery-monster", w.data.Monsters[spawn.MonsterID], "m", 5, 5, spawn)
	mon.IncHealth, mon.IncSpell, mon.IncHealing = 8, 7, 6
	mon.PerHealth, mon.PerSpell, mon.PerHealing = 1, 2, 3
	mon.HealthTick, mon.SpellTick = 99, 88
	mon.HealthTickAt, mon.SpellTickAt = 1, 2
	mon.IncHealthSpellAt = 3
	mon.CoolEye, mon.BodyLeathery = 1, 150
	mon.MasterID, mon.MasterName = "old-master", "old-master"
	mon.SlaveMakeLevel, mon.SlaveExpLevel, mon.SlaveExpProgress = 1, 2, 3
	mon.SlaveNameColor, mon.SlaveColorSet = 4, true
	mon.Alive = false
	mon.RespawnAt = now
	w.monsters[mon.ID] = mon
	w.respawnLocked(now, nil, nil)
	if mon.IncHealth != 0 || mon.IncSpell != 0 || mon.IncHealing != 0 || mon.PerHealth != 5 || mon.PerSpell != 5 || mon.PerHealing != 5 || mon.HealthTick != 0 || mon.SpellTick != 0 || mon.HealthTickAt != now.UnixMilli() || mon.SpellTickAt != now.UnixMilli() || mon.IncHealthSpellAt != now.UnixMilli() || mon.CoolEye != 0 || mon.BodyLeathery != 0 || mon.MasterID != "" || mon.MasterName != "" || mon.SlaveMakeLevel != 0 || mon.SlaveExpLevel != 0 || mon.SlaveExpProgress != 0 || mon.SlaveNameColor != 0 || mon.SlaveColorSet {
		t.Fatalf("respawn recovery state = %+v", mon)
	}
}

func TestMonsterRespawnRestoresReferenceTemplateStats(t *testing.T) {
	w := lifecycleWorld()
	now := time.Unix(100, 0)
	spawn := data.StdSpawn{ID: "respawn-template", MapID: "m", MonsterID: "template-monster", X: 5, Y: 5}
	tpl := data.StdMonster{ID: spawn.MonsterID, Name: spawn.MonsterID, Race: 112, RaceImg: 7, MP: 19, HP: 40, Appr: 3, Level: 9, Undead: 1, NoTame: true, Experience: 27, MinAttack: 4, MaxAttack: 8, Defense: 11, MagicDefense: 12, MagicDefenseMax: 13, AntiMagic: 14, AntiPoison: 15, MagicAttack: 16, MagicAttackMax: 17, TaoAttack: 18, Speed: 19, Hit: 20, WalkSpeedMS: 21}
	w.data.Monsters[spawn.MonsterID] = tpl
	mon := newMonster(w, "respawn-template-monster", tpl, "m", 5, 5, spawn)
	mon.Race, mon.RaceImg, mon.Appr, mon.Level, mon.Undead, mon.NoTame, mon.WantRefMsg, mon.Experience = 1, 2, 3, 4, 5, false, false, 6
	mon.MinAttack, mon.MaxAttack, mon.Defense, mon.MagicDefense = 6, 7, 8, 9
	mon.MagicDefenseMax, mon.AntiMagic, mon.AntiPoison = 10, 11, 12
	mon.MagicAttack, mon.MagicAttackMax, mon.TaoAttack, mon.Speed, mon.Hit = 13, 14, 15, 16, 17
	mon.Alive = false
	mon.RespawnAt = now
	w.monsters[mon.ID] = mon
	w.respawnLocked(now, nil, nil)
	if mon.Race != tpl.Race || mon.RaceImg != tpl.RaceImg || mon.Appr != tpl.Appr || mon.Level != tpl.Level || mon.Undead != tpl.Undead || mon.NoTame != tpl.NoTame || !mon.WantRefMsg || mon.Experience != tpl.Experience || mon.MinAttack != tpl.MinAttack || mon.MaxAttack != tpl.MaxAttack || mon.Defense != tpl.Defense || mon.MagicDefense != tpl.MagicDefense || mon.MagicDefenseMax != tpl.MagicDefenseMax || mon.AntiMagic != tpl.AntiMagic || mon.AntiPoison != tpl.AntiPoison || mon.MagicAttack != tpl.MagicAttack || mon.MagicAttackMax != tpl.MagicAttackMax || mon.TaoAttack != tpl.TaoAttack || mon.Speed != tpl.Speed || mon.Hit != tpl.Hit {
		t.Fatalf("respawn template stats = %+v, want template values", mon)
	}
}

func TestMonsterZenTimeMatchesReferencePopulationAcceleration(t *testing.T) {
	base := 29 * time.Minute
	if got := monsterZenTime(base, monsterUserFull); got != base {
		t.Fatalf("zen time at user-full threshold = %s, want %s", got, base)
	}
	if got := monsterZenTime(base, monsterUserFull+monsterZenFastStep); got != 1566*time.Second {
		t.Fatalf("zen time at one fast step = %s, want 1566s", got)
	}
	if got := monsterZenTime(base, monsterUserFull+monsterZenFastStep*10); got != 696*time.Second {
		t.Fatalf("zen time at capped acceleration = %s, want 696s", got)
	}
}

func TestReferenceAnimalFlagsForSpecialMonsterRaces(t *testing.T) {
	for _, race := range []int{82, 85} {
		mon := &Monster{Race: race}
		applyMonsterTemplateState(mon, data.StdMonster{Race: race})
		if !mon.Animal {
			t.Fatalf("race %d animal state = false, want true", race)
		}
	}
	for _, race := range []int{118, 119} {
		mon := &Monster{Race: race}
		applyMonsterTemplateState(mon, data.StdMonster{Race: race})
		if mon.Animal {
			t.Fatalf("race %d animal state = true, want false", race)
		}
	}
}

func TestReferenceCoolEyeUsesConfiguredPercentage(t *testing.T) {
	w := lifecycleWorld()
	tpl := data.StdMonster{Race: 95, CoolEye: 1}
	w.rand = rand.New(zeroSource{})
	mon := &Monster{}
	w.initializeMonsterGenerationStateLocked(mon, tpl, false)
	if mon.CoolEye != 1 {
		t.Fatalf("cool-eye at zero reference roll = %d, want 1", mon.CoolEye)
	}
	w.rand = rand.New(oneSource{})
	mon = &Monster{}
	w.initializeMonsterGenerationStateLocked(mon, tpl, false)
	if mon.CoolEye != 0 {
		t.Fatalf("cool-eye at high reference roll = %d, want 0", mon.CoolEye)
	}
}

func TestArcherGuardUsesReferenceUnsetDefaultDirection(t *testing.T) {
	w := lifecycleWorld()
	mon := newMonster(w, "archer-guard", data.StdMonster{ID: "archer-guard", Name: "archer-guard", Race: 112, HP: 10, MP: 1}, "m", 5, 5, data.StdSpawn{MapID: "m", MonsterID: "archer-guard"})
	if mon.GuardDirection != -1 {
		t.Fatalf("archer guard direction = %d, want reference unset direction -1", mon.GuardDirection)
	}
	if !mon.WantRefMsg {
		t.Fatal("archer guard WantRefMsg = false, want reference true")
	}
}

func TestDualAxeMonsterUsesReferenceConstructorAttackCounts(t *testing.T) {
	w := lifecycleWorld()
	for _, tc := range []struct {
		race int
		want int
	}{
		{race: 87, want: 2},
		{race: 93, want: 3},
		{race: 104, want: 6},
	} {
		mon := newMonster(w, fmt.Sprintf("dual-axe-%d", tc.race), data.StdMonster{ID: "dual-axe", Race: tc.race, HP: 10, MP: 1}, "m", 5, 5, data.StdSpawn{MapID: "m", MonsterID: "dual-axe"})
		if mon.AttackMax != tc.want {
			t.Fatalf("race %d AttackMax = %d, want %d", tc.race, mon.AttackMax, tc.want)
		}
	}
}

type oneSource struct{}

func (oneSource) Int63() int64 { return (1 << 62) + (1 << 32) }
func (oneSource) Seed(int64)   {}

func TestNewMonsterAllocatesStableActorIDForNonNumericRuntimeID(t *testing.T) {
	w := lifecycleWorld()
	first := newMonster(w, "castle-door:one", data.StdMonster{ID: "rat", Name: "rat", HP: 10, MP: 1}, "m", 5, 5, data.StdSpawn{MapID: "m", MonsterID: "rat"})
	second := newMonster(w, "castle-door:two", data.StdMonster{ID: "rat", Name: "rat", HP: 10, MP: 1}, "m", 6, 5, data.StdSpawn{MapID: "m", MonsterID: "rat"})
	w.monsters[first.ID] = first
	w.monsters[second.ID] = second

	if got := MonsterActorID(*first); got == MonsterActorID(*second) {
		t.Fatalf("non-numeric monster actor IDs collide: %d", got)
	}
	firstActorID := MonsterActorID(*first)
	if got := w.MonsterActorIDByID(first.ID); got != firstActorID {
		t.Fatalf("world actor ID = %d, want %d", got, firstActorID)
	}
	first.ObjectOrder++
	if got := MonsterActorID(*first); got != firstActorID {
		t.Fatalf("non-numeric monster actor ID changed after object-order refresh: got %d, want %d", got, firstActorID)
	}
}

func TestCharacterHitDoesNotRespawnMonsterOutsideWorldTick(t *testing.T) {
	w := lifecycleWorld()
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("storage.Open() error = %v", err)
	}
	w.store = store
	ch, err := w.CreateCharacterWithAppearance("account", "tester", "warrior", 0, 0, "m", 5, 5)
	if err != nil {
		t.Fatalf("CreateCharacterWithAppearance() error = %v", err)
	}
	mon := &Monster{
		ID: "hit-respawn", TemplateID: "rat", MapID: "m", X: 6, Y: 5,
		MaxHP: 10, HP: 0, RespawnAt: time.Unix(1, 0), Spawn: data.StdSpawn{
			MapID: "m", MonsterID: "rat", X: 6, Y: 5, Range: 0,
		},
	}
	w.mu.Lock()
	w.monsters = map[string]*Monster{mon.ID: mon}
	w.mu.Unlock()
	if _, err := w.Hit(ch, ch.X, ch.Y, 0); err != nil {
		t.Fatalf("Hit() error = %v", err)
	}
	w.mu.Lock()
	respawned := w.monsters[mon.ID].Alive
	w.mu.Unlock()
	if respawned {
		t.Fatal("character hit should not respawn a monster")
	}
}

func TestMonsterActionCarriesStableActorID(t *testing.T) {
	w := lifecycleWorld()
	mon := &Monster{ID: "castle-door:action", ActorID: 500123, MapID: "m", X: 5, Y: 5, Alive: true}
	action := w.monsterActionLocked(mon, MonsterActionHit)
	if action.ActorID != mon.ActorID {
		t.Fatalf("monster action actor ID = %d, want %d", action.ActorID, mon.ActorID)
	}
}

func TestNewMonsterUsesReferenceInitialSearchDelay(t *testing.T) {
	w := lifecycleWorld()
	before := time.Now()
	mon := newMonster(w, "initial-search-delay", data.StdMonster{ID: "rat", Name: "rat", HP: 10, MP: 1}, "m", 5, 5, data.StdSpawn{MapID: "m", MonsterID: "rat"})
	after := time.Now()
	if mon.NextSearchAt.Before(before.Add(3*time.Second)) || mon.NextSearchAt.After(after.Add(5*time.Second)) {
		t.Fatalf("NextSearchAt = %v, want creation time plus 3000..4999ms", mon.NextSearchAt)
	}
}

func TestNewCowKingUsesReferenceInitialSearchDelay(t *testing.T) {
	w := lifecycleWorld()
	before := time.Now()
	mon := newMonster(w, "initial-cow-king-search-delay", data.StdMonster{ID: "cow-king", Name: "cow-king", Race: 92, HP: 10, MP: 1}, "m", 5, 5, data.StdSpawn{MapID: "m", MonsterID: "cow-king"})
	after := time.Now()
	if mon.NextSearchAt.Before(before.Add(500*time.Millisecond)) || mon.NextSearchAt.After(after.Add(2*time.Second)) {
		t.Fatalf("NextSearchAt = %v, want creation time plus 500..1999ms", mon.NextSearchAt)
	}
}

func TestNewMonsterRestoresReferenceStoneKingState(t *testing.T) {
	w := lifecycleWorld()
	mon := newMonster(w, "stone-king", data.StdMonster{ID: "stone-king", Name: "stone-king", Race: 102, HP: 10, MP: 1}, "m", 5, 5, data.StdSpawn{MapID: "m", MonsterID: "stone-king"})
	if !mon.StoneMode || mon.Dir != 5 || mon.StoneDangerLevel != 5 {
		t.Fatalf("stone king initial state = stone:%t dir:%d danger:%d, want true/5/5", mon.StoneMode, mon.Dir, mon.StoneDangerLevel)
	}

	zilkin := &Monster{Race: 96, ZilkinKillCount: 2}
	w.initializeMonsterGenerationStateLocked(zilkin, data.StdMonster{Race: 96}, false)
	if zilkin.ZilkinKillCount != 2 {
		t.Fatalf("zilkin respawn count = %d, want preserved count 2", zilkin.ZilkinKillCount)
	}
}

func TestNewCentipedeMonsterRestoresReferenceFixedHideState(t *testing.T) {
	w := lifecycleWorld()
	mon := newMonster(w, "centipede-fixed-hide", data.StdMonster{ID: "centipede", Race: 107, HP: 10, MP: 1}, "m", 5, 5, data.StdSpawn{MapID: "m", MonsterID: "centipede"})
	if !mon.Hidden || !mon.FixedHideMode {
		t.Fatalf("centipede hidden state = hidden:%t fixed:%t, want both true", mon.Hidden, mon.FixedHideMode)
	}
}

func TestNewWolfRestoresReferenceBodyLeathery(t *testing.T) {
	w := lifecycleWorld()
	mon := newMonster(w, "wolf-body-leathery", data.StdMonster{ID: "wolf", Name: "wolf", Race: 53, HP: 10, MP: 1}, "m", 5, 5, data.StdSpawn{MapID: "m", MonsterID: "wolf"})
	if mon.BodyLeathery != 150 {
		t.Fatalf("wolf body leathery = %d, want 150", mon.BodyLeathery)
	}
}

func TestNewWolfUsesOrdinaryAnimalGenerationBranch(t *testing.T) {
	w := lifecycleWorld()
	w.rand = rand.New(twoSource{})
	mon := newMonster(w, "wolf-generation", data.StdMonster{ID: "wolf", Name: "wolf", Race: 53, HP: 10, MP: 1}, "m", 5, 5, data.StdSpawn{MapID: "m", MonsterID: "wolf"})
	if mon.FleeOnSight || mon.MeatQuality < 8000 || mon.MeatQuality >= 16000 {
		t.Fatalf("wolf generation = flee:%t meat:%d, want flee:false meat in [8000,16000)", mon.FleeOnSight, mon.MeatQuality)
	}
}

func TestMonsterDeathClearsReferenceRecoveryIncrements(t *testing.T) {
	w := lifecycleWorld()
	mon := &Monster{ID: "recovery-death", Alive: true, MapID: "m", X: 5, Y: 5, IncHealth: 3, IncSpell: 4, IncHealing: 5}
	w.removeMonsterLocked(mon, false)
	if mon.IncHealth != 0 || mon.IncSpell != 0 || mon.IncHealing != 0 {
		t.Fatalf("death recovery increments = %d/%d/%d, want 0/0/0", mon.IncHealth, mon.IncSpell, mon.IncHealing)
	}
}

func TestMonsterDeathClearsBothTargetReferences(t *testing.T) {
	w := lifecycleWorld()
	mon := &Monster{ID: "target-death", Alive: true, TargetCharacterID: "character", TargetMonsterID: "monster"}
	w.removeMonsterLocked(mon, false)
	if mon.TargetCharacterID != "" || mon.TargetMonsterID != "" {
		t.Fatalf("death targets = character:%q monster:%q, want both cleared", mon.TargetCharacterID, mon.TargetMonsterID)
	}

	mon = &Monster{ID: "animal-death", Alive: true, TargetCharacterID: "character", TargetMonsterID: "monster"}
	w.killAnimalLocked(mon)
	if mon.TargetCharacterID != "" || mon.TargetMonsterID != "" {
		t.Fatalf("animal death targets = character:%q monster:%q, want both cleared", mon.TargetCharacterID, mon.TargetMonsterID)
	}
}

func TestSummonedMonsterGainsReferenceKillExperience(t *testing.T) {
	w := lifecycleWorld()
	w.gameplay.Monster.SlaveExpRate = 16
	w.gameplay.Monster.SlaveExpBase = 100
	w.gameplay.Monster.SlaveExpKillCounts = []int{0, 0, 50}
	slave := &Monster{ID: "slave", MasterID: "player", Level: 1, SlaveMakeLevel: 1, BaseMaxHP: 100, BaseMinAttack: 5, BaseMaxAttack: 8, MaxHP: 100, MaxAttack: 8}
	if w.gainMonsterSlaveExpLocked(slave, 114) {
		t.Fatal("summoned monster leveled before reaching the reference threshold")
	}
	if slave.SlaveExpProgress != 114 || slave.SlaveExpLevel != 0 {
		t.Fatalf("summoned monster progress = %d/%d, want 114/0", slave.SlaveExpProgress, slave.SlaveExpLevel)
	}
	if !w.gainMonsterSlaveExpLocked(slave, 1) {
		t.Fatal("summoned monster did not level at the reference threshold")
	}
	if slave.SlaveExpProgress != 0 || slave.SlaveExpLevel != 1 || slave.MaxHP != 115 || slave.MaxAttack != 10 {
		t.Fatalf("summoned monster growth = progress:%d level:%d hp:%d attack:%d, want 0/1/115/10", slave.SlaveExpProgress, slave.SlaveExpLevel, slave.MaxHP, slave.MaxAttack)
	}
}

func TestMonsterRecoveryAppendsOrderedHealthEvent(t *testing.T) {
	w := lifecycleWorld()
	now := time.Unix(100, 0)
	mon := &Monster{
		ID:               "ordered-recovery",
		MapID:            "m",
		HP:               1,
		MaxHP:            100,
		MP:               1,
		MaxMP:            100,
		Level:            1,
		Alive:            true,
		IncHealth:        5,
		IncHealthSpellAt: now.Add(-time.Second).UnixMilli(),
		PerHealth:        5,
		PerSpell:         5,
		PerHealing:       5,
	}
	result := TickResult{}
	w.applyMonsterRecoveryTickLocked(mon, now, &result)
	if len(result.OrderedMonsterEvents) != 1 || result.OrderedMonsterEvents[0].Kind != OrderedMonsterEventHealth {
		t.Fatalf("ordered monster events = %+v, want one health event", result.OrderedMonsterEvents)
	}
	if result.OrderedMonsterEvents[0].Monster.HP <= 1 {
		t.Fatalf("ordered monster health = %d, want recovery above 1", result.OrderedMonsterEvents[0].Monster.HP)
	}
}

func TestWorldTickDoesNotRespawnOrdinaryMonsterFromRespawnAt(t *testing.T) {
	w := lifecycleWorld()
	var mon *Monster
	for _, candidate := range w.monsters {
		mon = candidate
		break
	}
	if mon == nil {
		t.Fatal("expected initial monster")
	}
	now := time.Now()
	w.mu.Lock()
	mon.Alive = false
	mon.RespawnAt = now.Add(-time.Second)
	mon.NextRunAt = time.Time{}
	mon.NextSearchAt = time.Time{}
	w.mu.Unlock()
	result, err := w.Tick(nil, now)
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if len(result.RespawnedMonsters) != 0 {
		t.Fatalf("respawned monsters = %+v, want none for ordinary monster", result.RespawnedMonsters)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if mon.Alive {
		t.Fatalf("ordinary monster alive = %t, want dead until group replenishment", mon.Alive)
	}
}

func TestMonsterProcessOrderUsesReferenceGroupAndMemberOrder(t *testing.T) {
	first := &Monster{ID: "first", Spawn: data.StdSpawn{ReferenceOrder: 12}, SpawnMemberOrder: 2, ObjectOrder: 9}
	second := &Monster{ID: "second", Spawn: data.StdSpawn{ReferenceOrder: 11}, SpawnMemberOrder: 99, ObjectOrder: 1}
	third := &Monster{ID: "third", Spawn: data.StdSpawn{ReferenceOrder: 12}, SpawnMemberOrder: 1, ObjectOrder: 20}
	firstGroup, firstMember, _, _ := monsterProcessOrder(first)
	secondGroup, secondMember, _, _ := monsterProcessOrder(second)
	thirdGroup, thirdMember, _, _ := monsterProcessOrder(third)
	if secondGroup >= firstGroup || thirdGroup != firstGroup || thirdMember >= firstMember || secondMember <= firstMember {
		t.Fatalf("process order = (%d,%d), (%d,%d), (%d,%d), want group then member order", firstGroup, firstMember, secondGroup, secondMember, thirdGroup, thirdMember)
	}
}

func TestMonsterProcessCursorDoesNotWrapWithinOnePass(t *testing.T) {
	w := lifecycleWorld()
	w.mu.Lock()
	defer w.mu.Unlock()
	w.monsters = make(map[string]*Monster)
	w.spawns = make(map[string]*spawnState)
	w.monsterProcessGroups = nil
	w.monsterProcessGroupByID = nil
	for i, referenceOrder := range []int{1, 2, 3} {
		mon := &Monster{
			ID:          fmt.Sprintf("cursor-%d", i),
			Spawn:       data.StdSpawn{ID: fmt.Sprintf("cursor-spawn-%d", i), ReferenceOrder: referenceOrder},
			Alive:       true,
			ObjectOrder: uint64(i + 1),
		}
		w.registerMonsterLocked(mon, nil)
	}
	w.monsterProcessGroupCursor = 1
	w.monsterProcessMemberCursor = 0
	processed := make(map[string]struct{})
	for _, wantID := range []string{"cursor-1", "cursor-2"} {
		entry, ok := w.nextMonsterProcessEntryLocked(processed)
		if !ok || entry.monster.ID != wantID {
			t.Fatalf("next entry = (%v, %t), want %s", entry.monster, ok, wantID)
		}
		processed[entry.monster.ID] = struct{}{}
		w.monsterProcessGroupCursor = entry.nextGroup
		w.monsterProcessMemberCursor = entry.nextMember
	}
	if _, ok := w.nextMonsterProcessEntryLocked(processed); ok {
		t.Fatal("process cursor wrapped to the first group within one pass")
	}
	if w.monsterProcessGroupCursor != 0 {
		t.Fatalf("process cursor after completed pass = %d, want 0", w.monsterProcessGroupCursor)
	}
}

func TestMonsterProcessBudgetAtGroupEndResumesNextGroup(t *testing.T) {
	w := lifecycleWorld()
	w.mu.Lock()
	defer w.mu.Unlock()
	w.monsters = make(map[string]*Monster)
	w.spawns = make(map[string]*spawnState)
	w.monsterProcessGroups = nil
	w.monsterProcessGroupByID = nil
	for i, referenceOrder := range []int{1, 2} {
		mon := &Monster{
			ID:    fmt.Sprintf("budget-cursor-%d", i),
			Spawn: data.StdSpawn{ID: fmt.Sprintf("budget-spawn-%d", i), ReferenceOrder: referenceOrder},
			Alive: true,
		}
		w.registerMonsterLocked(mon, nil)
	}
	processed := make(map[string]struct{})
	entry, ok := w.nextMonsterProcessEntryLocked(processed)
	if !ok || entry.monster.ID != "budget-cursor-0" {
		t.Fatalf("first entry = (%v, %t), want first group", entry.monster, ok)
	}
	processed[entry.monster.ID] = struct{}{}
	w.monsterProcessGroupCursor = entry.group
	w.monsterProcessMemberCursor = entry.member + 1
	next, ok := w.nextMonsterProcessEntryLocked(processed)
	if !ok || next.monster.ID != "budget-cursor-1" {
		t.Fatalf("resumed entry = (%v, %t), want next group", next.monster, ok)
	}
}

func TestMonsterProcessCursorKeepsNextMemberAfterDeletingCurrentMember(t *testing.T) {
	w := lifecycleWorld()
	w.mu.Lock()
	defer w.mu.Unlock()
	w.monsters = make(map[string]*Monster)
	w.spawns = make(map[string]*spawnState)
	w.monsterProcessGroups = nil
	w.monsterProcessGroupByID = nil
	spawn := data.StdSpawn{ID: "delete-cursor-spawn", ReferenceOrder: 1}
	first := &Monster{ID: "delete-cursor-first", Spawn: spawn, Alive: false, Ghost: true}
	second := &Monster{ID: "delete-cursor-second", Spawn: spawn, Alive: true}
	third := &Monster{ID: "delete-cursor-third", Spawn: spawn, Alive: true}
	w.registerMonsterLocked(first, nil)
	w.registerMonsterLocked(second, nil)
	w.registerMonsterLocked(third, nil)
	processed := make(map[string]struct{})
	entry, ok := w.nextMonsterProcessEntryLocked(processed)
	if !ok || entry.monster.ID != first.ID {
		t.Fatalf("first entry = (%v, %t), want ghost member", entry.monster, ok)
	}
	processed[first.ID] = struct{}{}
	w.monsterProcessGroupCursor = entry.nextGroup
	w.monsterProcessMemberCursor = entry.nextMember
	w.unregisterMonsterLocked(first.ID)
	next, ok := w.nextMonsterProcessEntryLocked(processed)
	if !ok || next.monster.ID != second.ID {
		t.Fatalf("next entry = (%v, %t), want second member after deletion", next.monster, ok)
	}
}

func TestUnregisterMonsterClearsVisibleAndStatusState(t *testing.T) {
	w := lifecycleWorld()
	w.mu.Lock()
	defer w.mu.Unlock()
	mon := &Monster{ID: "cleanup-monster", Spawn: data.StdSpawn{ID: "cleanup-spawn", ReferenceOrder: 1}}
	w.registerMonsterLocked(mon, nil)
	w.visibleActors[mon.ID] = []string{monsterVisibleActorKey(mon.ID)}
	w.monsterStatusClocks[mon.ID] = map[statusClockKind]statusClock{statusClockParalyzed: {SourceUntil: 1, Remaining: 1}}
	w.unregisterMonsterLocked(mon.ID)
	if _, ok := w.visibleActors[mon.ID]; ok {
		t.Fatalf("visible actors for unregistered monster remain: %v", w.visibleActors[mon.ID])
	}
	if _, ok := w.monsterStatusClocks[mon.ID]; ok {
		t.Fatalf("status clocks for unregistered monster remain: %v", w.monsterStatusClocks[mon.ID])
	}
}

func TestStatusClockMatchesReferenceOneSecondDecrements(t *testing.T) {
	w := lifecycleWorld()
	now := time.Unix(1000, 0)
	w.mu.Lock()
	defer w.mu.Unlock()
	w.resetCharacterStatusClockLocked("status-character", statusClockDefence, now.Add(3*time.Second).UnixNano(), now)
	until := now.Add(3 * time.Second).UnixNano()
	got, active, changed := w.advanceCharacterStatusClockLocked("status-character", statusClockDefence, until, now.Add(500*time.Millisecond))
	if got != until || !active || changed {
		t.Fatalf("status clock before one second = %d/%t/%t, want original/active/unchanged", got, active, changed)
	}
	for step := 1; step <= 3; step++ {
		current := now.Add(time.Duration(step)*time.Second + 100*time.Millisecond)
		until, active, changed = w.advanceCharacterStatusClockLocked("status-character", statusClockDefence, until, current)
		if step < 3 && (!active || !changed) {
			t.Fatalf("status clock step %d = %d/%t/%t, want active and changed", step, until, active, changed)
		}
		if step == 3 && (active || !changed || until != 0) {
			t.Fatalf("status clock expiry = %d/%t/%t, want cleared/changed", until, active, changed)
		}
	}
}

func TestInvisibleMonsterUsesReferenceProcessIntervalOnInitialSearch(t *testing.T) {
	w := lifecycleWorld()
	var mon *Monster
	for _, candidate := range w.monsters {
		mon = candidate
		break
	}
	if mon == nil {
		t.Fatal("expected initial monster")
	}
	now := time.Unix(1000, 0)
	w.mu.Lock()
	mon.NextRunAt = time.Time{}
	mon.NextVisibleSearchAt = time.Time{}
	mon.VisibleSearchIntervalMS = 0
	mon.ProcessRunCount = 0
	w.monsterProcessNextAt = time.Time{}
	w.mu.Unlock()
	if _, err := w.Tick(nil, now); err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if mon.ProcessRunCount != 1 {
		t.Fatalf("ProcessRunCount = %d, want 1 after first invisible pass", mon.ProcessRunCount)
	}
	if mon.NextVisibleSearchAt.IsZero() {
		t.Fatal("initial invisible search did not establish the next search deadline")
	}
}

func TestRespawnUsesReferenceProcessOrder(t *testing.T) {
	makeWorld := func(reverse bool) *World {
		w := lifecycleWorld()
		spawn := data.StdSpawn{ID: "respawn-order", MapID: "m", MonsterID: "rat", X: 5, Y: 5, Range: 2, RespawnSeconds: 1, ReferenceOrder: 4}
		first := newMonster(w, "respawn-first", w.data.Monsters["rat"], "m", 5, 5, spawn)
		first.Alive = false
		first.RespawnAt = time.Unix(100, 0)
		first.SpawnMemberOrder = 1
		second := newMonster(w, "respawn-second", w.data.Monsters["rat"], "m", 5, 5, spawn)
		second.Alive = false
		second.RespawnAt = time.Unix(100, 0)
		second.SpawnMemberOrder = 2
		w.monsters = make(map[string]*Monster, 2)
		if reverse {
			w.monsters[second.ID] = second
			w.monsters[first.ID] = first
		} else {
			w.monsters[first.ID] = first
			w.monsters[second.ID] = second
		}
		w.rand = rand.New(rand.NewSource(17))
		return w
	}

	now := time.Unix(200, 0)
	firstWorld := makeWorld(false)
	secondWorld := makeWorld(true)
	firstWorld.mu.Lock()
	firstWorld.respawnLocked(now, nil, nil)
	firstWorld.mu.Unlock()
	secondWorld.mu.Lock()
	secondWorld.respawnLocked(now, nil, nil)
	secondWorld.mu.Unlock()

	for _, id := range []string{"respawn-first", "respawn-second"} {
		first := firstWorld.monsters[id]
		second := secondWorld.monsters[id]
		if first.X != second.X || first.Y != second.Y || first.Dir != second.Dir || !first.LastWalkAt.Equal(second.LastWalkAt) {
			t.Fatalf("respawn %s differs by map insertion: first=(%d,%d,%d,%v), second=(%d,%d,%d,%v)", id, first.X, first.Y, first.Dir, first.LastWalkAt, second.X, second.Y, second.Dir, second.LastWalkAt)
		}
	}
}

func TestMonsterMoveRefreshesReferenceObjectOrder(t *testing.T) {
	w := lifecycleWorld()
	mon := &Monster{ID: "moving-object-order", MapID: "m", X: 5, Y: 5, Alive: true, ObjectOrder: 4}
	w.mu.Lock()
	w.nextObjectOrder = 9
	w.monsters[mon.ID] = mon
	w.occupyMonsterLocked(mon)
	w.moveMonsterLocked(mon, 6, 5)
	w.mu.Unlock()

	if mon.ObjectOrder != 9 {
		t.Fatalf("moved monster object order = %d, want 9", mon.ObjectOrder)
	}
	if w.nextObjectOrder != 10 {
		t.Fatalf("next object order = %d, want 10", w.nextObjectOrder)
	}
}

func TestWorldTickReplenishesDeadSpawnWithNewObject(t *testing.T) {
	w := lifecycleWorld()
	var old *Monster
	for _, mon := range w.monsters {
		old = mon
		break
	}
	if old == nil {
		t.Fatal("expected initial monster")
	}
	now := time.Unix(100, 0)
	w.mu.Lock()
	w.actionNow = now
	w.removeMonsterLocked(old, true)
	w.actionNow = time.Time{}
	w.mu.Unlock()
	result, err := w.Tick(nil, now.Add(61*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.SpawnedMonsters) != 1 || result.SpawnedMonsters[0].ID == old.ID {
		t.Fatalf("spawned = %+v, want one replacement with a new id", result.SpawnedMonsters)
	}
	if _, ok := w.monsters[old.ID]; !ok {
		t.Fatal("dead object should remain until ghost cleanup")
	}
}

func TestWorldTickGhostsAndCleansDeadMonster(t *testing.T) {
	w := lifecycleWorld()
	var old *Monster
	for _, mon := range w.monsters {
		old = mon
		break
	}
	now := time.Unix(1000, 0)
	w.mu.Lock()
	old.Alive = false
	old.DeathAt = now.Add(-3*time.Minute - time.Second)
	old.RunIntervalMS = 0
	old.NextRunAt = time.Time{}
	old.ProcessRunCount = monsterProcessInterval
	old.VisibleSearchIntervalMS = 0
	w.mu.Unlock()
	if _, err := w.Tick(nil, now); err != nil {
		t.Fatal(err)
	}
	if !old.Ghost {
		t.Fatal("expected dead monster to become ghost")
	}
	if monsters, _ := w.SnapshotAround(old.MapID, old.X, old.Y, 0); len(monsters) != 0 {
		t.Fatalf("ghost monster snapshot = %+v, want none", monsters)
	}
	if _, err := w.Tick(nil, now.Add(5*time.Minute+time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, ok := w.monsters[old.ID]; ok {
		t.Fatal("expected ghost monster to be removed")
	}
}

func TestWorldTickUsesStrictMonsterGhostBoundaries(t *testing.T) {
	w := lifecycleWorld()
	var old *Monster
	for _, mon := range w.monsters {
		old = mon
		break
	}
	now := time.Unix(2000, 0)
	w.mu.Lock()
	old.Alive = false
	old.DeathAt = now
	old.RunIntervalMS = 0
	old.NextRunAt = time.Time{}
	old.ProcessRunCount = monsterProcessInterval
	old.VisibleSearchIntervalMS = 0
	w.mu.Unlock()
	if _, err := w.Tick(nil, now.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if old.Ghost {
		t.Fatal("monster became ghost at exact death grace boundary")
	}
	w.mu.Lock()
	w.monsterProcessNextAt = time.Time{}
	w.mu.Unlock()
	if _, err := w.Tick(nil, now.Add(3*time.Minute+time.Nanosecond)); err != nil {
		t.Fatal(err)
	}
	if !old.Ghost {
		t.Fatal("monster did not become ghost after death grace boundary")
	}
	ghostAt := old.GhostAt
	if _, err := w.Tick(nil, ghostAt.Add(5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, ok := w.monsters[old.ID]; !ok {
		t.Fatal("monster was removed at exact ghost grace boundary")
	}
	w.mu.Lock()
	w.monsterProcessNextAt = time.Time{}
	w.mu.Unlock()
	if _, err := w.Tick(nil, ghostAt.Add(5*time.Minute+time.Nanosecond)); err != nil {
		t.Fatal(err)
	}
	if _, ok := w.monsters[old.ID]; ok {
		t.Fatal("monster was not removed after ghost grace boundary")
	}
}

func TestWorldTickEntersMonsterStageAtReferenceInterval(t *testing.T) {
	w := lifecycleWorld()
	var ghost *Monster
	for _, mon := range w.monsters {
		ghost = mon
		break
	}
	if ghost == nil {
		t.Fatal("expected initial monster")
	}
	base := time.Unix(3000, 0)
	ghost.Alive = false
	ghost.Ghost = true
	ghost.GhostAt = base
	w.mu.Lock()
	w.monsterProcessNextAt = base.Add(5*time.Minute + 4*time.Millisecond)
	w.mu.Unlock()
	if _, err := w.Tick(nil, base.Add(5*time.Minute+5*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if _, ok := w.monsters[ghost.ID]; ok {
		t.Fatal("monster stage did not run one millisecond after its reference interval")
	}
}

func TestWorldReplenishesSpawnOnlyAfterReferenceInterval(t *testing.T) {
	w := lifecycleWorld()
	var old *Monster
	for _, mon := range w.monsters {
		old = mon
		break
	}
	now := time.Unix(3000, 0)
	w.removeMonsterLocked(old, true)
	state := w.spawnStateForLocked(old.Spawn)
	state.lastRegenAt = now.Add(-time.Minute)
	w.monsterRegenAt = now
	result := TickResult{}
	w.replenishMonsterSpawnLocked(now, &result)
	if len(result.SpawnedMonsters) != 0 {
		t.Fatalf("spawn replenished at exact reference regen tick = %+v, want none", result.SpawnedMonsters)
	}
	w.monsterRegenAt = time.Time{}
	result = TickResult{}
	w.replenishMonsterSpawnLocked(now.Add(time.Nanosecond), &result)
	if len(result.SpawnedMonsters) != 1 {
		t.Fatalf("spawn replenished after reference interval = %+v, want one", result.SpawnedMonsters)
	}
	w2 := lifecycleWorld()
	var old2 *Monster
	for _, mon := range w2.monsters {
		old2 = mon
		break
	}
	w2.removeMonsterLocked(old2, true)
	state2 := w2.spawnStateForLocked(old2.Spawn)
	state2.lastRegenAt = now.Add(-time.Minute - time.Nanosecond)
	w2.monsterRegenAt = now.Add(-20 * time.Millisecond)
	result = TickResult{}
	w2.replenishMonsterSpawnLocked(now, &result)
	if len(result.SpawnedMonsters) != 1 {
		t.Fatalf("spawn did not use 20ms reference scheduler interval = %+v, want one", result.SpawnedMonsters)
	}
}

func TestMonsterProcessBudgetMatchesReferenceRuntime(t *testing.T) {
	if monsterProcessBudget != 25*time.Millisecond {
		t.Fatalf("monster process budget = %s, want 25ms", monsterProcessBudget)
	}
}

func TestSpawnPositionUsesReferenceAddBaseObjectAdjustment(t *testing.T) {
	w := lifecycleWorld()
	w.data.Maps["m"] = data.StdMap{ID: "m", Width: 100, Height: 100, Blocked: []data.StdPoint{{X: 10, Y: 10}}}
	w.rand = rand.New(rand.NewSource(1))
	x, y, ok := w.spawnPositionForSpawnLocked(data.StdSpawn{MapID: "m", X: 10, Y: 10}, "")
	if !ok || x != 13 || y != 10 {
		t.Fatalf("spawn position = (%d,%d), ok=%t; want reference three-tile x adjustment", x, y, ok)
	}
}

func TestSpawnPositionAllowsExistingMonsterLikeReferenceAddToMap(t *testing.T) {
	w := lifecycleWorld()
	w.data.Maps["m"] = data.StdMap{ID: "m", Width: 100, Height: 100}
	w.monsters["existing"] = &Monster{ID: "existing", MapID: "m", X: 10, Y: 10, Alive: true}
	w.rand = rand.New(rand.NewSource(1))
	x, y, ok := w.resolveReferenceSpawnPositionLocked("m", 10, 10, "")
	if !ok || x != 10 || y != 10 {
		t.Fatalf("spawn position = (%d,%d), ok=%t; want occupied walkable tile like reference", x, y, ok)
	}
}

func TestMonsterCellQueriesRetainOverlappingObjects(t *testing.T) {
	w := lifecycleWorld()
	w.monsters = map[string]*Monster{}
	first := &Monster{ID: "overlap-first", MapID: "m", X: 10, Y: 10, Alive: true}
	second := &Monster{ID: "overlap-second", MapID: "m", X: 10, Y: 10, Alive: true}
	w.monsters[first.ID] = first
	w.monsters[second.ID] = second
	w.occupyMonsterLocked(first)
	w.occupyMonsterLocked(second)
	if !w.monsterAtLocked("m", 10, 10, first.ID) {
		t.Fatal("overlapping second monster was not visible after excluding first")
	}
	w.vacateMonsterLocked(first)
	if !w.monsterAtLocked("m", 10, 10, "") {
		t.Fatal("overlapping second monster disappeared after first vacated")
	}
}
