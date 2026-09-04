package world

import (
	"fmt"
	"math"
	"math/rand"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"

	"openmir2/internal/config"
	"openmir2/internal/data"
	"openmir2/internal/protocol/mir176"
	"openmir2/internal/storage"
)

const (
	testConfigsDir      = "../../configs"
	testMapID           = "0"
	testMonsterID       = "鸡"
	testWeaponID        = "木剑"
	testArmorID         = "布衣(男)"
	testHPItemID        = "金创药(小量)"
	testInstantHPItemID = "太阳水"
	testMPItemID        = "魔法药(小量)"
)

func TestStandardSkillMagicIDsMatchReference(t *testing.T) {
	want := map[string]uint16{
		"火球术": 1, "治愈术": 2, "基本剑术": 3, "精神力战法": 4,
		"大火球": 5, "施毒术": 6, "攻杀剑术": 7, "抗拒火环": 8,
		"地狱火": 9, "疾光电影": 10, "雷电术": 11, "刺杀剑术": 12,
		"灵魂火符": 13, "幽灵盾": 14, "神圣战甲术": 15, "困魔咒": 50,
		"召唤骷髅": 17, "隐身术": 18, "集体隐身术": 19, "诱惑之光": 20,
		"瞬息移动": 21, "火墙": 22, "爆裂火焰": 23, "地狱雷光": 24,
		"半月弯刀": 25, "烈火剑法": 26, "野蛮冲撞": 27, "心灵启示": 28,
		"群体治疗术": 29, "召唤神兽": 30, "魔法盾": 31, "圣言术": 32,
		"冰咆哮": 33,
	}

	w := &World{}
	for name, wantID := range want {
		gotID, ok := w.MagicIDByName(name)
		if !ok || gotID != wantID {
			t.Errorf("MagicIDByName(%q) = (%d, %t), want (%d, true)", name, gotID, ok, wantID)
		}
		gotName, ok := w.SkillIDByMagicID(wantID)
		if !ok || gotName != name {
			t.Errorf("SkillIDByMagicID(%d) = (%q, %t), want (%q, true)", wantID, gotName, ok, name)
		}
	}
}

func TestEveryStandardConfiguredSkillHasReferenceMapping(t *testing.T) {
	bundle := loadTestBundle(t)
	for name := range bundle.Skills {
		if _, ok := (&World{}).MagicIDByName(name); !ok {
			t.Errorf("configured skill %q has no standard magic mapping", name)
		}
	}
}

func TestCharacterDeathIsEmittedByNextWorldTick(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	ch.HP = 0
	w.mu.Lock()
	w.deferCharacterDeathLocked(ch)
	w.mu.Unlock()

	result, err := w.Tick([]PlayerSnapshot{{Character: ch}}, time.Now())
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if len(result.CharacterDeaths) != 1 || result.CharacterDeaths[0].ID != ch.ID {
		t.Fatalf("CharacterDeaths = %+v, want one death for %q", result.CharacterDeaths, ch.ID)
	}
	result, err = w.Tick([]PlayerSnapshot{{Character: ch}}, time.Now().Add(time.Second))
	if err != nil {
		t.Fatalf("second Tick() error = %v", err)
	}
	if len(result.CharacterDeaths) != 0 {
		t.Fatalf("second CharacterDeaths = %+v, want no duplicate death", result.CharacterDeaths)
	}
}

func TestCharacterDeathIsDiscardedWhenObjectLeavesWorldSnapshot(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	ch.HP = 0
	w.mu.Lock()
	w.deferCharacterDeathLocked(ch)
	w.mu.Unlock()

	result, err := w.Tick(nil, time.Now())
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if len(result.CharacterDeaths) != 0 {
		t.Fatalf("CharacterDeaths = %+v, want no death for missing object", result.CharacterDeaths)
	}
	w.mu.Lock()
	_, pending := w.pendingCharacterDeaths[ch.ID]
	w.mu.Unlock()
	if pending {
		t.Fatalf("pending death for %q was retained after object removal", ch.ID)
	}
}

func TestUnsupportedReferenceMagicIDDoesNotAliasStandardSkill(t *testing.T) {
	w := &World{}
	if name, ok := w.SkillIDByMagicID(16); ok {
		t.Fatalf("SkillIDByMagicID(16) = (%q, true), want no standard alias", name)
	}
}

func loadTestBundle(t *testing.T) data.StdBundle {
	t.Helper()
	bundle, _, err := data.LoadConfigsWithReport(testConfigsDir)
	if err != nil {
		t.Fatalf("LoadConfigsWithReport() error = %v", err)
	}
	bundle.Spawns = nil
	return bundle
}

func allStartPoints(bundle data.StdBundle) []data.StdStartPoint {
	points := make([]data.StdStartPoint, 0)
	mapIDs := make([]string, 0, len(bundle.Maps))
	for mapID := range bundle.Maps {
		mapIDs = append(mapIDs, mapID)
	}
	sort.Slice(mapIDs, func(i, j int) bool {
		return compareMapIDs(mapIDs[i], mapIDs[j]) < 0
	})
	for _, mapID := range mapIDs {
		mp := bundle.Maps[mapID]
		for _, sp := range mp.StartPoints {
			sp.MapID = mapID
			points = append(points, sp)
		}
	}
	return points
}

func defaultSpawn(bundle data.StdBundle) (string, int, int) {
	points := allStartPoints(bundle)
	if len(points) == 0 {
		return "0", 0, 0
	}
	sp := points[0]
	return sp.MapID, sp.X, sp.Y
}

func startCoordsForMap(t *testing.T, bundle data.StdBundle, mapID string) (int, int) {
	t.Helper()
	mp, ok := bundle.Maps[mapID]
	if !ok {
		t.Fatalf("map %s missing from configs", mapID)
	}
	if len(mp.StartPoints) > 0 {
		return mp.StartPoints[0].X, mp.StartPoints[0].Y
	}
	return mp.Width / 2, mp.Height / 2
}

func countBagItems(items []storage.UserItem) int {
	return len(items)
}

func spawnMonsterForTest(t *testing.T, w *World, mapID string, x, y int, name string, count int) (SpawnResult, error) {
	t.Helper()
	return w.spawnMonsterByName(mapID, x, y, name, count, -1, -1)
}

func attackMonsterForTest(w *World, ch storage.Character, monsterID string) (AttackResult, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	mon, ok := w.monsters[monsterID]
	if !ok || !mon.Alive {
		return AttackResult{}, fmt.Errorf("monster not found")
	}
	return w.attackLocked(ch, mon, mir176.CMHit)
}

func setEquippedItem(ch *storage.Character, slot int, item storage.UserItem) {
	if ch.EquippedItems == nil {
		ch.EquippedItems = map[int]storage.UserItem{}
	}
	if item.ItemID == "" {
		delete(ch.EquippedItems, slot)
		return
	}
	ch.EquippedItems[slot] = item
}

type zeroSource struct{}

func (zeroSource) Int63() int64 { return 0 }
func (zeroSource) Seed(int64)   {}

type twoSource struct{}

func (twoSource) Int63() int64 { return 2 }
func (twoSource) Seed(int64)   {}

type seqSource struct {
	vals []int64
	idx  int
}

func (s *seqSource) Int63() int64 {
	if len(s.vals) == 0 {
		return 0
	}
	if s.idx >= len(s.vals) {
		return s.vals[len(s.vals)-1]
	}
	v := s.vals[s.idx]
	s.idx++
	return v
}

func (s *seqSource) Seed(int64) {}

func TestWorldNewNormalizesMonsterDefaults(t *testing.T) {
	bundle := loadTestBundle(t)
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	w := New(bundle, store)
	redMoon := w.data.Monsters["赤月恶魔"]
	if redMoon.WalkSpeedMS != 800 {
		t.Fatalf("red moon walk speed = %d, want 800", redMoon.WalkSpeedMS)
	}
	if redMoon.AttackIntervalMS != 2000 {
		t.Fatalf("red moon attack interval = %d, want 2000", redMoon.AttackIntervalMS)
	}
	spider := w.data.Monsters["爆裂蜘蛛"]
	if spider.AttackIntervalMS != 1800 {
		t.Fatalf("explosion spider attack interval = %d, want 1800", spider.AttackIntervalMS)
	}
}

func addSpawnNearDefault(t *testing.T, bundle *data.StdBundle, monsterID string, dx, dy int) {
	t.Helper()
	x, y := startCoordsForMap(t, *bundle, testMapID)
	bundle.Spawns = []data.StdSpawn{{
		MapID:          testMapID,
		MonsterID:      monsterID,
		X:              x + dx,
		Y:              y + dy,
		Count:          1,
		RespawnSeconds: 10,
	}}
}

func newAIWorldCharacter(t *testing.T) (*World, storage.Character) {
	t.Helper()
	bundle := loadTestBundle(t)
	addSpawnNearDefault(t, &bundle, "鹿", 2, 0)
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	settings := config.DefaultGameplay()
	bundle.Monsters["鹿"] = func() data.StdMonster {
		mon := bundle.Monsters["鹿"]
		mon.WalkSpeedMS = 1
		return mon
	}()
	mon := bundle.Monsters["鹿"]
	mon.SearchNoTargetMS = 1
	mon.SearchHasTargetMS = 1
	bundle.Monsters["鹿"] = mon
	w := New(bundle, store, settings)
	primeMonsterTimersForTests(w)
	mapID, x, y := defaultSpawn(bundle)
	ch, err := w.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	return w, ch
}

func newAggressiveAIWorldCharacter(t *testing.T) (*World, storage.Character) {
	t.Helper()
	bundle := loadTestBundle(t)
	addSpawnNearDefault(t, &bundle, "半兽人", 2, 0)
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	settings := config.DefaultGameplay()
	bundle.Monsters["半兽人"] = func() data.StdMonster {
		mon := bundle.Monsters["半兽人"]
		mon.WalkSpeedMS = 1
		return mon
	}()
	mon := bundle.Monsters["半兽人"]
	mon.SearchNoTargetMS = 1
	mon.SearchHasTargetMS = 1
	bundle.Monsters["半兽人"] = mon
	w := New(bundle, store, settings)
	primeMonsterTimersForTests(w)
	mapID, x, y := defaultSpawn(bundle)
	ch, err := w.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	return w, ch
}

func newRealDataWorldCharacter(t *testing.T) (*World, storage.Character) {
	t.Helper()
	bundle := loadTestBundle(t)
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	w := New(bundle, store)
	primeMonsterTimersForTests(w)
	mapID, x, y := defaultSpawn(bundle)
	ch, err := w.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	return w, ch
}

func firstWalkableAround(t *testing.T, w *World, mapID string, x, y, radius int) (int, int) {
	t.Helper()
	mp, ok := w.data.Maps[mapID]
	if !ok {
		t.Fatalf("map %s missing", mapID)
	}
	for dy := -radius; dy <= radius; dy++ {
		for dx := -radius; dx <= radius; dx++ {
			nx, ny := x+dx, y+dy
			if mp.Walkable(nx, ny) {
				return nx, ny
			}
		}
	}
	t.Fatalf("no walkable tile near %s (%d,%d) within radius %d", mapID, x, y, radius)
	return 0, 0
}

func otherMapID(t *testing.T, w *World, current string) string {
	t.Helper()
	for id := range w.data.Maps {
		if id != current {
			return id
		}
	}
	t.Fatal("expected at least two maps")
	return ""
}

func primeMonsterTimersForTests(w *World) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, mon := range w.monsters {
		mon.LastWalkAt = time.Time{}
		mon.NextSearchAt = time.Time{}
		mon.WalkWaitTick = time.Time{}
	}
}

func TestMonsterTickChasesNearbyCharacter(t *testing.T) {
	w, ch := newAggressiveAIWorldCharacter(t)
	monsters, _ := w.SnapshotAround(ch.MapID, 0, 0, 99999)
	if len(monsters) == 0 {
		t.Fatalf("expected monsters")
	}
	mon := monsters[0]
	ch.X, ch.Y = mon.X-2, mon.Y
	result, err := w.Tick([]PlayerSnapshot{{Character: ch}}, time.Unix(10, 0))
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if len(result.MonsterActions) != 1 || result.MonsterActions[0].Kind != MonsterActionWalk {
		t.Fatalf("MonsterActions = %+v, want one walk", result.MonsterActions)
	}
	action := result.MonsterActions[0]
	if action.MonsterID != mon.ID || action.X != mon.X-1 || action.Y != mon.Y || action.Dir != 6 {
		t.Fatalf("walk action = %+v, want monster %s to (%d,%d) dir west", action, mon.ID, mon.X-1, mon.Y)
	}
}

func TestMonsterSpawnFacesSouth(t *testing.T) {
	w, _ := newAIWorldCharacter(t)
	monsters, _ := w.SnapshotAround(testMapID, 0, 0, 99999)
	if len(monsters) == 0 {
		t.Fatalf("expected spawned monsters")
	}
	if monsters[0].Dir != 4 {
		t.Fatalf("spawned monster dir = %d, want 4", monsters[0].Dir)
	}
}

func TestMonsterTickWandersForwardWhenIdle(t *testing.T) {
	bundle := loadTestBundle(t)
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	w := New(bundle, store)
	w.mu.Lock()
	w.monsters = map[string]*Monster{}
	w.occupied = map[monsterPosition]string{}
	mp := bundle.Maps[testMapID]
	x, y := startCoordsForMap(t, bundle, testMapID)
	found := false
	for dx := 1; dx < 10; dx++ {
		if mp.Walkable(x+dx, y) && mp.Walkable(x+dx+1, y) {
			x += dx
			found = true
			break
		}
	}
	if !found {
		w.mu.Unlock()
		t.Fatalf("could not find a walkable east-facing tile near spawn")
	}
	mon := &Monster{
		ID:                "mon-1",
		TemplateID:        "鹿",
		Name:              "鹿",
		MapID:             testMapID,
		X:                 x,
		Y:                 y,
		Dir:               2,
		ViewRange:         5,
		LeashRange:        15,
		SearchNoTargetMS:  1,
		SearchHasTargetMS: 1,
		HP:                1,
		MaxHP:             1,
		Alive:             true,
		WalkSpeedMS:       1,
		WalkStep:          1,
	}
	w.monsters[mon.ID] = mon
	w.occupyMonsterLocked(mon)
	w.rand = rand.New(rand.NewSource(11))
	w.mu.Unlock()

	result, err := w.Tick(nil, time.Unix(10, 0))
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if len(result.MonsterActions) != 1 {
		t.Fatalf("MonsterActions = %+v, want one wander action", result.MonsterActions)
	}
	action := result.MonsterActions[0]
	if action.Kind != MonsterActionWalk || action.X != x+1 || action.Y != y || action.Dir != 2 {
		t.Fatalf("wander action = %+v, want walk east to (%d,%d) dir 2", action, x+1, y)
	}
}

func TestMonsterTickWandersByTurningWhenIdle(t *testing.T) {
	bundle := loadTestBundle(t)
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	w := New(bundle, store)
	w.mu.Lock()
	w.monsters = map[string]*Monster{}
	w.occupied = map[monsterPosition]string{}
	mp := bundle.Maps[testMapID]
	x, y := startCoordsForMap(t, bundle, testMapID)
	found := false
	for dx := 1; dx < 10; dx++ {
		if mp.Walkable(x+dx, y) {
			x += dx
			found = true
			break
		}
	}
	if !found {
		w.mu.Unlock()
		t.Fatalf("could not find a walkable tile near spawn")
	}
	mon := &Monster{
		ID:                "mon-1",
		TemplateID:        "鹿",
		Name:              "鹿",
		MapID:             testMapID,
		X:                 x,
		Y:                 y,
		Dir:               2,
		ViewRange:         5,
		LeashRange:        15,
		SearchNoTargetMS:  1,
		SearchHasTargetMS: 1,
		HP:                1,
		MaxHP:             1,
		Alive:             true,
		WalkSpeedMS:       1,
		WalkStep:          1,
	}
	w.monsters[mon.ID] = mon
	w.occupyMonsterLocked(mon)
	w.rand = rand.New(rand.NewSource(137))
	w.mu.Unlock()

	result, err := w.Tick(nil, time.Unix(10, 0))
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if len(result.MonsterActions) != 1 {
		t.Fatalf("MonsterActions = %+v, want one turn action", result.MonsterActions)
	}
	action := result.MonsterActions[0]
	if action.Kind != MonsterActionTurn || action.Dir != 1 || action.X != x || action.Y != y {
		t.Fatalf("wander action = %+v, want turn in place to dir 1", action)
	}
}

func TestAnimalMonsterDoesNotRunAwayFromPlayer(t *testing.T) {
	bundle := loadTestBundle(t)
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	w := New(bundle, store)
	w.mu.Lock()
	w.monsters = map[string]*Monster{}
	w.occupied = map[monsterPosition]string{}
	mp := bundle.Maps[testMapID]
	x, y := startCoordsForMap(t, bundle, testMapID)
	found := false
	for dx := 1; dx < 10; dx++ {
		if mp.Walkable(x+dx, y) && mp.Walkable(x-dx, y) {
			x += dx
			found = true
			break
		}
	}
	if !found {
		w.mu.Unlock()
		t.Fatalf("could not find a walkable passive-monster test tile")
	}
	mon := &Monster{
		ID:                "mon-1",
		TemplateID:        "鹿",
		Name:              "鹿",
		Race:              52,
		Animal:            true,
		MapID:             testMapID,
		X:                 x,
		Y:                 y,
		Dir:               2,
		ViewRange:         5,
		LeashRange:        15,
		SearchNoTargetMS:  1,
		SearchHasTargetMS: 1,
		HP:                25,
		MaxHP:             25,
		Alive:             true,
		WalkSpeedMS:       1,
		WalkStep:          1,
	}
	w.monsters[mon.ID] = mon
	w.occupyMonsterLocked(mon)
	w.mu.Unlock()

	ch := storage.Character{ID: "player-1", MapID: testMapID, X: x + 1, Y: y, HP: 20, MaxHP: 20}
	result, err := w.Tick([]PlayerSnapshot{{Character: ch}}, time.Unix(10, 0))
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if len(result.CharacterHits) != 0 {
		t.Fatalf("CharacterHits = %+v, want no passive attack", result.CharacterHits)
	}
	w.mu.Lock()
	updated := w.monsters[mon.ID]
	w.mu.Unlock()
	if updated == nil || updated.RunAwayMode || updated.TargetCharacterID != "" {
		t.Fatalf("updated monster = %+v, want ordinary idle animal behavior", updated)
	}
	if len(result.MonsterActions) > 0 {
		action := result.MonsterActions[0]
		if action.X == ch.X && action.Y == ch.Y {
			t.Fatalf("monster action = %+v, want not to overlap the player", action)
		}
	}
}

func TestSpecialAnimalMonsterRunsAwayFromPlayer(t *testing.T) {
	bundle := loadTestBundle(t)
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	w := New(bundle, store)
	w.mu.Lock()
	w.monsters = map[string]*Monster{}
	w.occupied = map[monsterPosition]string{}
	mp := bundle.Maps[testMapID]
	x, y := startCoordsForMap(t, bundle, testMapID)
	found := false
	for dx := 1; dx < 10; dx++ {
		if mp.Walkable(x+dx, y) && mp.Walkable(x-dx, y) {
			x += dx
			found = true
			break
		}
	}
	if !found {
		w.mu.Unlock()
		t.Fatalf("could not find a walkable passive-monster test tile")
	}
	mon := &Monster{
		ID:                "mon-1",
		TemplateID:        "鹿",
		Name:              "鹿",
		Race:              52,
		Animal:            true,
		FleeOnSight:       true,
		MapID:             testMapID,
		X:                 x,
		Y:                 y,
		Dir:               2,
		ViewRange:         5,
		LeashRange:        15,
		SearchNoTargetMS:  1,
		SearchHasTargetMS: 1,
		HP:                25,
		MaxHP:             25,
		Alive:             true,
		WalkSpeedMS:       1,
		WalkStep:          1,
	}
	w.monsters[mon.ID] = mon
	w.occupyMonsterLocked(mon)
	w.mu.Unlock()

	ch := storage.Character{ID: "player-1", MapID: testMapID, X: x + 1, Y: y, HP: 20, MaxHP: 20}
	result, err := w.Tick([]PlayerSnapshot{{Character: ch}}, time.Unix(10, 0))
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if len(result.CharacterHits) != 0 {
		t.Fatalf("CharacterHits = %+v, want no passive attack", result.CharacterHits)
	}
	if len(result.MonsterActions) != 1 {
		t.Fatalf("MonsterActions = %+v, want one flee action", result.MonsterActions)
	}
	action := result.MonsterActions[0]
	if action.Kind != MonsterActionWalk {
		t.Fatalf("flee action = %+v, want move away from player", action)
	}
	if action.X == ch.X && action.Y == ch.Y {
		t.Fatalf("flee action = %+v, want not to overlap the player", action)
	}
	w.mu.Lock()
	updated := w.monsters[mon.ID]
	w.mu.Unlock()
	if updated == nil || !updated.RunAwayMode || updated.TargetX <= updated.X {
		t.Fatalf("updated monster = %+v, want run-away target ahead of current position", updated)
	}
	if updated.X == ch.X && updated.Y == ch.Y {
		t.Fatalf("updated monster = %+v, want not to overlap the player", updated)
	}
}

func TestWhiteSkeletonRevealsOnFirstTick(t *testing.T) {
	bundle := loadTestBundle(t)
	addSpawnNearDefault(t, &bundle, "变异骷髅", 2, 0)
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	w := New(bundle, store)
	w.mu.Lock()
	var mon *Monster
	for _, candidate := range w.monsters {
		if candidate.TemplateID == "变异骷髅" {
			mon = candidate
			break
		}
	}
	w.mu.Unlock()
	if mon == nil {
		t.Fatalf("expected spawned 变异骷髅")
	}
	w.mu.Lock()
	mon.WalkSpeedMS = 1
	mon.WalkStep = 1
	w.mu.Unlock()
	result, err := w.Tick(nil, time.Unix(10, 0))
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if len(result.MonsterActions) == 0 || result.MonsterActions[0].Kind != MonsterActionReveal {
		t.Fatalf("MonsterActions = %+v, want first tick reveal", result.MonsterActions)
	}
	w.mu.Lock()
	updated := w.monsters[mon.ID]
	w.mu.Unlock()
	if updated == nil || updated.Hidden || updated.FixedHideMode || updated.Dir != 5 {
		t.Fatalf("updated monster = %+v, want revealed white skeleton facing south", updated)
	}
}

func TestStickMonsterHidesWhenTargetLeaves(t *testing.T) {
	bundle := loadTestBundle(t)
	addSpawnNearDefault(t, &bundle, "食人花", 2, 0)
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	w := New(bundle, store)
	w.mu.Lock()
	var mon *Monster
	for _, candidate := range w.monsters {
		if candidate.TemplateID == "食人花" {
			mon = candidate
			break
		}
	}
	w.mu.Unlock()
	if mon == nil {
		t.Fatalf("expected spawned 食人花")
	}
	w.mu.Lock()
	mon.WalkSpeedMS = 1
	mon.WalkStep = 1
	mon.SearchNoTargetMS = 1
	mon.SearchHasTargetMS = 1
	w.mu.Unlock()
	ch := storage.Character{ID: "player-1", MapID: testMapID, X: mon.X + 1, Y: mon.Y, HP: 20, MaxHP: 20}
	result, err := w.Tick([]PlayerSnapshot{{Character: ch}}, time.Unix(10, 0))
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if len(result.MonsterActions) == 0 || result.MonsterActions[0].Kind != MonsterActionReveal {
		t.Fatalf("MonsterActions = %+v, want reveal on approach", result.MonsterActions)
	}
	ch.X, ch.Y = mon.X+20, mon.Y
	result, err = w.Tick([]PlayerSnapshot{{Character: ch}}, time.Unix(11, 0))
	if err != nil {
		t.Fatalf("second Tick() error = %v", err)
	}
	if len(result.MonsterActions) != 1 || result.MonsterActions[0].Kind != MonsterActionHide {
		t.Fatalf("MonsterActions = %+v, want hide after target leaves", result.MonsterActions)
	}
	w.mu.Lock()
	updated := w.monsters[mon.ID]
	w.mu.Unlock()
	if updated == nil || !updated.Hidden || !updated.FixedHideMode {
		t.Fatalf("updated monster = %+v, want hidden flower after come-down", updated)
	}
}

func TestCentipedeKingDoesNotHideImmediately(t *testing.T) {
	bundle := loadTestBundle(t)
	addSpawnNearDefault(t, &bundle, "触龙神", 2, 0)
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	w := New(bundle, store)
	w.mu.Lock()
	var mon *Monster
	for _, candidate := range w.monsters {
		if candidate.TemplateID == "触龙神" {
			mon = candidate
			break
		}
	}
	w.mu.Unlock()
	if mon == nil {
		t.Fatalf("expected spawned 触龙神")
	}
	w.mu.Lock()
	mon.WalkSpeedMS = 1
	mon.WalkStep = 1
	mon.SearchNoTargetMS = 1
	mon.SearchHasTargetMS = 1
	w.mu.Unlock()
	ch := storage.Character{ID: "player-1", MapID: testMapID, X: mon.X + 1, Y: mon.Y, HP: 20, MaxHP: 20}
	result, err := w.Tick([]PlayerSnapshot{{Character: ch}}, time.Unix(10, 0))
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if len(result.MonsterActions) == 0 || result.MonsterActions[0].Kind != MonsterActionReveal {
		t.Fatalf("MonsterActions = %+v, want reveal on approach", result.MonsterActions)
	}
	ch.X, ch.Y = mon.X+20, mon.Y
	result, err = w.Tick([]PlayerSnapshot{{Character: ch}}, time.Unix(11, 0))
	if err != nil {
		t.Fatalf("second Tick() error = %v", err)
	}
	for _, action := range result.MonsterActions {
		if action.Kind == MonsterActionHide {
			t.Fatalf("MonsterActions = %+v, want no immediate hide", result.MonsterActions)
		}
	}
	result, err = w.Tick([]PlayerSnapshot{{Character: ch}}, time.Unix(21, 0))
	if err != nil {
		t.Fatalf("third Tick() error = %v", err)
	}
	hasHide := false
	for _, action := range result.MonsterActions {
		if action.Kind == MonsterActionHide {
			hasHide = true
			break
		}
	}
	if !hasHide {
		t.Fatalf("MonsterActions = %+v, want hide after timeout", result.MonsterActions)
	}
}

func TestArcherGuardTurnsBackWhenIdle(t *testing.T) {
	bundle := loadTestBundle(t)
	addSpawnNearDefault(t, &bundle, "弓箭护卫", 2, 0)
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	w := New(bundle, store)
	w.mu.Lock()
	var mon *Monster
	for _, candidate := range w.monsters {
		if candidate.TemplateID == "弓箭护卫" {
			mon = candidate
			break
		}
	}
	w.mu.Unlock()
	if mon == nil {
		t.Fatalf("expected spawned 弓箭护卫")
	}
	w.mu.Lock()
	mon.Dir = 1
	mon.GuardDirection = 4
	mon.TargetCharacterID = "player-1"
	w.mu.Unlock()
	ch := storage.Character{ID: "player-1", MapID: testMapID, X: mon.X + 13, Y: mon.Y, HP: 20, MaxHP: 20}
	result, err := w.Tick([]PlayerSnapshot{{Character: ch}}, time.Unix(10, 0))
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if len(result.MonsterActions) != 1 || result.MonsterActions[0].Kind != MonsterActionTurn || result.MonsterActions[0].Dir != 4 {
		t.Fatalf("MonsterActions = %+v, want turn back to guard direction", result.MonsterActions)
	}
}

func TestStoneMonsterRevealsWhenApproached(t *testing.T) {
	bundle := loadTestBundle(t)
	addSpawnNearDefault(t, &bundle, "祖玛雕像", 2, 0)
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	w := New(bundle, store)
	w.mu.Lock()
	var mon *Monster
	for _, candidate := range w.monsters {
		if candidate.TemplateID == "祖玛雕像" {
			mon = candidate
			break
		}
	}
	w.mu.Unlock()
	if mon == nil {
		t.Fatalf("expected spawned 祖玛雕像")
	}
	w.mu.Lock()
	mon.StoneMode = true
	mon.SearchNoTargetMS = 1
	mon.SearchHasTargetMS = 1
	w.mu.Unlock()
	ch := storage.Character{ID: "player-1", MapID: testMapID, X: mon.X + 1, Y: mon.Y, HP: 20, MaxHP: 20}
	result, err := w.Tick([]PlayerSnapshot{{Character: ch}}, time.Unix(10, 0))
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if len(result.MonsterActions) == 0 || result.MonsterActions[0].Kind != MonsterActionReveal {
		t.Fatalf("MonsterActions = %+v, want stone monster reveal", result.MonsterActions)
	}
	w.mu.Lock()
	updated := w.monsters[mon.ID]
	w.mu.Unlock()
	if updated == nil || updated.StoneMode {
		t.Fatalf("updated monster = %+v, want stone mode cleared after reveal", updated)
	}
}

func TestBigHeartMonsterHitsMultipleTargets(t *testing.T) {
	bundle := loadTestBundle(t)
	addSpawnNearDefault(t, &bundle, "千年树妖", 2, 0)
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	w := New(bundle, store)
	w.mu.Lock()
	var mon *Monster
	for _, candidate := range w.monsters {
		if candidate.TemplateID == "千年树妖" {
			mon = candidate
			break
		}
	}
	w.mu.Unlock()
	if mon == nil {
		t.Fatalf("expected spawned 千年树妖")
	}
	w.mu.Lock()
	mon.AttackIntervalMS = 1
	mon.WalkSpeedMS = 1
	w.mu.Unlock()
	players := []PlayerSnapshot{
		{Character: storage.Character{ID: "p1", MapID: testMapID, X: mon.X + 1, Y: mon.Y, HP: 20, MaxHP: 20}},
		{Character: storage.Character{ID: "p2", MapID: testMapID, X: mon.X, Y: mon.Y + 1, HP: 20, MaxHP: 20}},
	}
	result, err := w.Tick(players, time.Unix(10, 0))
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if len(result.CharacterHits) < 2 {
		t.Fatalf("CharacterHits = %+v, want big heart to hit multiple nearby targets", result.CharacterHits)
	}
}

func TestSpiderHouseSpawnsChildOnTarget(t *testing.T) {
	bundle := loadTestBundle(t)
	addSpawnNearDefault(t, &bundle, "幻影蜘蛛", 2, 0)
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	w := New(bundle, store)
	w.mu.Lock()
	var mon *Monster
	for _, candidate := range w.monsters {
		if candidate.TemplateID == "幻影蜘蛛" {
			mon = candidate
			break
		}
	}
	w.mu.Unlock()
	if mon == nil {
		t.Fatalf("expected spawned 幻影蜘蛛")
	}
	w.mu.Lock()
	mon.AttackIntervalMS = 1
	mon.WalkSpeedMS = 1
	w.mu.Unlock()
	ch := storage.Character{ID: "player-1", MapID: testMapID, X: mon.X + 1, Y: mon.Y, HP: 20, MaxHP: 20}
	result, err := w.Tick([]PlayerSnapshot{{Character: ch}}, time.Unix(10, 0))
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if len(result.MonsterActions) == 0 {
		t.Fatalf("MonsterActions = %+v, want spider house to act", result.MonsterActions)
	}
	w.mu.Lock()
	childCount := 0
	for _, candidate := range w.monsters {
		if candidate.ParentID == mon.ID && candidate.Alive {
			childCount++
		}
	}
	w.mu.Unlock()
	if childCount == 0 {
		t.Fatalf("want spawned spider child, got none")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, candidate := range w.monsters {
		if candidate.ParentID == mon.ID && candidate.Alive {
			if candidate.TemplateID != "爆裂蜘蛛" || candidate.Name != "爆裂蜘蛛" {
				t.Fatalf("spawned child = %+v, want 爆裂蜘蛛", candidate)
			}
		}
	}
}

func TestSpiderHouseStillSpawnsWhenChildTileIsOccupied(t *testing.T) {
	bundle := loadTestBundle(t)
	addSpawnNearDefault(t, &bundle, "幻影蜘蛛", 2, 0)
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	w := New(bundle, store)
	w.mu.Lock()
	var mon *Monster
	for _, candidate := range w.monsters {
		if candidate.TemplateID == "幻影蜘蛛" {
			mon = candidate
			break
		}
	}
	if mon == nil {
		w.mu.Unlock()
		t.Fatalf("expected spawned 幻影蜘蛛")
	}
	blocker := &Monster{
		ID:         "mon-block",
		TemplateID: "鸡",
		Name:       "鸡",
		MapID:      mon.MapID,
		X:          mon.X,
		Y:          mon.Y + 1,
		Alive:      true,
	}
	w.monsters[blocker.ID] = blocker
	w.occupyMonsterLocked(blocker)
	mon.AttackIntervalMS = 1
	mon.WalkSpeedMS = 1
	w.mu.Unlock()
	ch := storage.Character{ID: "player-1", MapID: testMapID, X: mon.X + 1, Y: mon.Y, HP: 20, MaxHP: 20}
	result, err := w.Tick([]PlayerSnapshot{{Character: ch}}, time.Unix(10, 0))
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if len(result.MonsterActions) == 0 {
		t.Fatalf("MonsterActions = %+v, want spider house to act", result.MonsterActions)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	childCount := 0
	for _, candidate := range w.monsters {
		if candidate.ParentID == mon.ID && candidate.Alive {
			childCount++
		}
	}
	if childCount == 0 {
		t.Fatalf("want spawned spider child even when child tile is occupied, got none")
	}
}

func TestBeeQueenSpawnsBeeOnTarget(t *testing.T) {
	bundle := loadTestBundle(t)
	addSpawnNearDefault(t, &bundle, "角蝇", 2, 0)
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	w := New(bundle, store)
	w.mu.Lock()
	var mon *Monster
	for _, candidate := range w.monsters {
		if candidate.TemplateID == "角蝇" {
			mon = candidate
			break
		}
	}
	w.mu.Unlock()
	if mon == nil {
		t.Fatalf("expected spawned 角蝇")
	}
	w.mu.Lock()
	mon.AttackIntervalMS = 1
	mon.WalkSpeedMS = 1
	w.mu.Unlock()
	ch := storage.Character{ID: "player-1", MapID: testMapID, X: mon.X + 1, Y: mon.Y, HP: 20, MaxHP: 20}
	result, err := w.Tick([]PlayerSnapshot{{Character: ch}}, time.Unix(10, 0))
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if len(result.MonsterActions) == 0 {
		t.Fatalf("MonsterActions = %+v, want bee queen to act", result.MonsterActions)
	}
	w.mu.Lock()
	childCount := 0
	for _, candidate := range w.monsters {
		if candidate.ParentID == mon.ID && candidate.Alive {
			childCount++
		}
	}
	w.mu.Unlock()
	if childCount == 0 {
		t.Fatalf("want spawned bee child, got none")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, candidate := range w.monsters {
		if candidate.ParentID == mon.ID && candidate.Alive {
			if candidate.TemplateID != "蝙蝠" || candidate.Name != "蝙蝠" {
				t.Fatalf("spawned child = %+v, want 蝙蝠", candidate)
			}
		}
	}
}

func TestStickMonsterStaysFixedWhenPlayerApproaches(t *testing.T) {
	bundle := loadTestBundle(t)
	addSpawnNearDefault(t, &bundle, "食人花", 2, 0)
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	w := New(bundle, store)
	w.mu.Lock()
	var mon *Monster
	for _, candidate := range w.monsters {
		if candidate.TemplateID == "食人花" {
			mon = candidate
			break
		}
	}
	w.mu.Unlock()
	if mon == nil {
		t.Fatalf("expected spawned 食人花")
	}
	ch := storage.Character{ID: "player-1", MapID: testMapID, X: mon.X + 1, Y: mon.Y, HP: 20, MaxHP: 20}
	result, err := w.Tick([]PlayerSnapshot{{Character: ch}}, time.Unix(10, 0))
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if len(result.MonsterActions) != 1 {
		t.Fatalf("MonsterActions = %+v, want one fixed-position action", result.MonsterActions)
	}
	action := result.MonsterActions[0]
	if action.MonsterID != mon.ID || action.X != mon.X || action.Y != mon.Y {
		t.Fatalf("stick action = %+v, want monster to stay at (%d,%d)", action, mon.X, mon.Y)
	}
	if action.Kind != MonsterActionReveal && action.Kind != MonsterActionHit && action.Kind != MonsterActionHide {
		t.Fatalf("stick action kind = %v, want reveal, hide, or hit", action.Kind)
	}
}

func TestCentipedeKingStaysFixedWhenPlayerApproaches(t *testing.T) {
	bundle := loadTestBundle(t)
	addSpawnNearDefault(t, &bundle, "触龙神", 2, 0)
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	w := New(bundle, store)
	w.mu.Lock()
	var mon *Monster
	for _, candidate := range w.monsters {
		if candidate.TemplateID == "触龙神" {
			mon = candidate
			break
		}
	}
	w.mu.Unlock()
	if mon == nil {
		t.Fatalf("expected spawned 触龙神")
	}
	ch := storage.Character{ID: "player-1", MapID: testMapID, X: mon.X + 1, Y: mon.Y, HP: 20, MaxHP: 20}
	result, err := w.Tick([]PlayerSnapshot{{Character: ch}}, time.Unix(10, 0))
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if len(result.MonsterActions) != 1 {
		t.Fatalf("MonsterActions = %+v, want one fixed-position action", result.MonsterActions)
	}
	action := result.MonsterActions[0]
	if action.MonsterID != mon.ID || action.X != mon.X || action.Y != mon.Y {
		t.Fatalf("centipede action = %+v, want monster to stay at (%d,%d)", action, mon.X, mon.Y)
	}
	if action.Kind != MonsterActionReveal && action.Kind != MonsterActionHit && action.Kind != MonsterActionHide {
		t.Fatalf("centipede action kind = %v, want reveal, hide, or hit", action.Kind)
	}
}

func TestMonsterTickRoutesAroundBlockingMonster(t *testing.T) {
	w, ch := newAggressiveAIWorldCharacter(t)
	monsters, _ := w.SnapshotAround(ch.MapID, 0, 0, 99999)
	if len(monsters) == 0 {
		t.Fatalf("expected monsters")
	}
	mon := monsters[0]
	w.mu.Lock()
	w.monsters["blocker"] = &Monster{
		ID:         "blocker",
		TemplateID: mon.TemplateID,
		Name:       "Blocker",
		MapID:      mon.MapID,
		X:          mon.X - 1,
		Y:          mon.Y,
		HP:         100,
		MaxHP:      100,
		Alive:      true,
	}
	w.occupyMonsterLocked(w.monsters["blocker"])
	w.mu.Unlock()
	ch.X, ch.Y = mon.X-2, mon.Y
	result, err := w.Tick([]PlayerSnapshot{{Character: ch}}, time.Unix(10, 0))
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	var action MonsterAction
	for _, candidate := range result.MonsterActions {
		if candidate.MonsterID == mon.ID {
			action = candidate
			break
		}
	}
	if action.MonsterID == "" || action.Kind != MonsterActionWalk {
		t.Fatalf("MonsterActions = %+v, want primary monster walk", result.MonsterActions)
	}
	if action.X == mon.X-1 && action.Y == mon.Y {
		t.Fatalf("walk action = %+v, wanted route around blocker", action)
	}
	if abs(action.X-mon.X) > 1 || abs(action.Y-mon.Y) > 1 {
		t.Fatalf("walk action = %+v, wanted one-tile step from (%d,%d)", action, mon.X, mon.Y)
	}
}

func TestMonsterTickAttacksAdjacentCharacter(t *testing.T) {
	w, ch := newAggressiveAIWorldCharacter(t)
	monsters, _ := w.SnapshotAround(ch.MapID, 0, 0, 99999)
	if len(monsters) == 0 {
		t.Fatalf("expected monsters")
	}
	mon := monsters[0]
	ch.X, ch.Y = mon.X-1, mon.Y
	result, err := w.Tick([]PlayerSnapshot{{Character: ch}}, time.Unix(10, 0))
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if len(result.MonsterActions) != 1 || result.MonsterActions[0].Kind != MonsterActionHit {
		t.Fatalf("MonsterActions = %+v, want one hit", result.MonsterActions)
	}
	if len(result.CharacterHits) != 1 {
		t.Fatalf("CharacterHits = %+v, want one hit", result.CharacterHits)
	}
	hit := result.CharacterHits[0]
	if hit.Character.ID != ch.ID || hit.Character.HP >= ch.HP || hit.Damage <= 0 {
		t.Fatalf("hit = %+v, original HP=%d", hit, ch.HP)
	}
	if hit.Character.SpellTick != 0 || hit.Character.LastHitterID != mon.ID || hit.Character.LastHitterAt == 0 {
		t.Fatalf("hit character state = %+v, want cleared SpellTick and monster last hitter", hit.Character)
	}
}

func TestMonsterTickStopsChasingPastLeashRange(t *testing.T) {
	w, ch := newAggressiveAIWorldCharacter(t)
	monsters, _ := w.SnapshotAround(ch.MapID, 0, 0, 99999)
	if len(monsters) == 0 {
		t.Fatalf("expected monsters")
	}
	mon := monsters[0]
	ch.X, ch.Y = mon.X-2, mon.Y
	if _, err := w.Tick([]PlayerSnapshot{{Character: ch}}, time.Unix(10, 0)); err != nil {
		t.Fatalf("first Tick() error = %v", err)
	}
	ch.X, ch.Y = mon.X-20, mon.Y
	result, err := w.Tick([]PlayerSnapshot{{Character: ch}}, time.Unix(11, 0))
	if err != nil {
		t.Fatalf("second Tick() error = %v", err)
	}
	if len(result.MonsterActions) != 0 || len(result.CharacterHits) != 0 {
		t.Fatalf("tick result after leash break = %+v, want no actions", result)
	}
}

func TestCombatDropPickupAndPersistence(t *testing.T) {
	bundle := loadTestBundle(t)
	addSpawnNearDefault(t, &bundle, testMonsterID, 2, 0)
	path := filepath.Join(t.TempDir(), "state.json")
	store, err := storage.Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	w := New(bundle, store)
	mapID, x, y := defaultSpawn(bundle)
	ch, err := w.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch, err = w.Move(ch, x+1, y)
	if err != nil {
		t.Fatalf("Move() error = %v", err)
	}
	monsters, _ := w.SnapshotAround(ch.MapID, 0, 0, 99999)
	if len(monsters) == 0 {
		t.Fatalf("expected monsters")
	}
	var result AttackResult
	for i := 0; i < 10; i++ {
		result, err = attackMonsterForTest(w, ch, monsters[0].ID)
		if err != nil {
			t.Fatalf("Attack() error = %v", err)
		}
		ch = result.Character
		if result.MonsterHP == 0 {
			break
		}
	}
	tick, err := w.Tick([]PlayerSnapshot{{Character: ch}}, time.Now())
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if len(tick.MonsterDeaths) != 1 {
		t.Fatalf("monster did not die")
	}
	result = tick.MonsterDeaths[0]
	ch = result.Character
	if result.Experience == 0 {
		t.Fatalf("expected experience")
	}
	if len(result.Drops) == 0 {
		t.Fatalf("expected drops")
	}
	ch.X, ch.Y = result.Drops[0].X, result.Drops[0].Y
	ch, _, err = w.PickupWithResult(ch, result.Drops[0].ID)
	if err != nil {
		t.Fatalf("Pickup() error = %v", err)
	}
	reopened, err := storage.Open(path)
	if err != nil {
		t.Fatalf("reopen error = %v", err)
	}
	saved, ok := reopened.Character(ch.ID)
	if !ok {
		t.Fatalf("saved character missing")
	}
	if saved.Experience == 0 {
		t.Fatalf("experience was not persisted")
	}
	if len(saved.BagItems) < 2 {
		t.Fatalf("pickup was not persisted")
	}
}

func TestMonsterDeathResetsSearchCooldown(t *testing.T) {
	bundle := loadTestBundle(t)
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	w := New(bundle, store)
	mon := &Monster{
		ID:                "mon-1",
		MapID:             testMapID,
		X:                 10,
		Y:                 10,
		Dir:               4,
		ViewRange:         5,
		LeashRange:        15,
		SearchNoTargetMS:  1000,
		SearchHasTargetMS: 8000,
		HP:                1,
		MaxHP:             1,
		Alive:             true,
		MinAttack:         1,
		MaxAttack:         1,
	}
	ch := storage.Character{ID: "player-1", MapID: testMapID, X: 11, Y: 10, HP: 20, MaxHP: 20}
	_, hit, err := w.monsterAttackCharacterWithDamageLocked(mon, ch, 999)
	if err != nil {
		t.Fatalf("monsterAttackCharacterWithDamageLocked() error = %v", err)
	}
	if !hit.Dead {
		t.Fatalf("hit.Dead = false, want true")
	}
	if mon.TargetCharacterID != "" {
		t.Fatalf("TargetCharacterID = %q, want empty", mon.TargetCharacterID)
	}
	if !mon.TargetFocusAt.IsZero() {
		t.Fatalf("TargetFocusAt = %v, want zero", mon.TargetFocusAt)
	}
	if mon.NextSearchAt.IsZero() {
		t.Fatalf("NextSearchAt = zero, want immediate re-search")
	}
	_, _, _, err = w.tickNormalMonsterLocked(mon, map[string]storage.Character{ch.ID: ch}, mon.NextSearchAt.Add(time.Millisecond))
	if err != nil {
		t.Fatalf("tickNormalMonsterLocked() error = %v", err)
	}
	if mon.TargetCharacterID != ch.ID {
		t.Fatalf("TargetCharacterID = %q, want %q after cooldown reset", mon.TargetCharacterID, ch.ID)
	}
}

func TestMonsterCharacterDamageDecrementsRecoveryCounters(t *testing.T) {
	w, _ := newTestWorldCharacter(t)
	mon := &Monster{ID: "mon-hit", MapID: testMapID, HP: 100, MaxHP: 100, Alive: true, MinAttack: 1, MaxAttack: 1}
	ch := storage.Character{ID: "player-hit", MapID: testMapID, HP: 100, MaxHP: 100, PerHealth: 5, PerSpell: 5, SpellTick: 700}
	next, hit, err := w.monsterAttackCharacterWithDamageLocked(mon, ch, 10)
	if err != nil {
		t.Fatalf("monsterAttackCharacterWithDamageLocked() error = %v", err)
	}
	if hit.Damage <= 0 || next.SpellTick != 0 {
		t.Fatalf("monster damage result = hit:%+v character:%+v, want positive damage and zero SpellTick", hit, next)
	}
	if next.PerHealth != 4 || next.PerSpell != 4 {
		t.Fatalf("recovery counters = %d/%d, want 4/4", next.PerHealth, next.PerSpell)
	}
}

func TestExplosionSpiderCharacterDamageUsesDelayedImpact(t *testing.T) {
	w, _ := newTestWorldCharacter(t)
	mon := &Monster{ID: "exploder", MapID: testMapID, X: 10, Y: 10, MinAttack: 4, MaxAttack: 4, Alive: true, HP: 1, MaxHP: 1}
	ch := storage.Character{ID: "player-explode", MapID: testMapID, X: 10, Y: 10, HP: 100, MaxHP: 100, PerHealth: 5, PerSpell: 5, SpellTick: 700}
	_, hits, updated, err := w.explosionSpiderLocked(mon, map[string]storage.Character{ch.ID: ch})
	if err != nil {
		t.Fatalf("explosionSpiderLocked() error = %v", err)
	}
	if len(hits) != 1 || len(updated) != 1 {
		t.Fatalf("explosion result = hits:%d updated:%d, want one each", len(hits), len(updated))
	}
	if hits[0].ImpactDelay != 700*time.Millisecond {
		t.Fatalf("ImpactDelay = %s, want 700ms", hits[0].ImpactDelay)
	}
	if updated[0].PerHealth != 4 || updated[0].PerSpell != 4 || updated[0].SpellTick != 0 {
		t.Fatalf("recovery state = %+v, want counters 4/4 and zero SpellTick", updated[0])
	}
	if hits[0].Damage != 4 {
		t.Fatalf("Damage = %d, want combined physical and magic halves", hits[0].Damage)
	}
}

func TestRandomNewCharacterSpawnUsesConfiguredDefaultPoints(t *testing.T) {
	bundle := loadTestBundle(t)
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	w := New(bundle, store)
	points := allStartPoints(bundle)
	if len(points) < 2 {
		t.Fatal("need at least two start points for random spawn test")
	}
	seen := map[string]bool{}
	for i := 0; i < 16; i++ {
		mapID, x, y := w.RandomNewCharacterSpawn()
		matched := false
		for _, sp := range points[:2] {
			if sp.MapID == mapID && sp.X == x && sp.Y == y {
				seen[fmt.Sprintf("%s:%d:%d", sp.MapID, sp.X, sp.Y)] = true
				matched = true
				break
			}
		}
		if !matched {
			t.Fatalf("RandomNewCharacterSpawn() = (%s,%d,%d), want one of first two configured start points", mapID, x, y)
		}
	}
	if len(seen) < 2 {
		t.Fatalf("RandomNewCharacterSpawn() only hit %d start point(s), want both configured new-character points", len(seen))
	}
}

func TestStartPointUpdatesHomeWhenNearby(t *testing.T) {
	bundle := loadTestBundle(t)
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	w := New(bundle, store)
	sp := allStartPoints(bundle)[0]
	ch := storage.Character{
		ID:      "char-1",
		Account: "test",
		Name:    "tester-home",
		Class:   "warrior",
		MapID:   sp.MapID,
		X:       sp.X + 1,
		Y:       sp.Y,
	}
	updated, changed, err := w.SyncCharacterHomeFromStartPoint(ch)
	if err != nil {
		t.Fatalf("SyncCharacterHomeFromStartPoint() error = %v", err)
	}
	if !changed {
		t.Fatal("SyncCharacterHomeFromStartPoint() changed = false, want true")
	}
	if updated.HomeMap != sp.MapID || updated.HomeX != sp.X || updated.HomeY != sp.Y {
		t.Fatalf("home = %s (%d,%d), want %s (%d,%d)", updated.HomeMap, updated.HomeX, updated.HomeY, sp.MapID, sp.X, sp.Y)
	}
}

func TestOfficialConfigMapSupportsWalkAndCombat(t *testing.T) {
	bundle := loadTestBundle(t)
	addSpawnNearDefault(t, &bundle, testMonsterID, 3, 0)
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	w := New(bundle, store)
	startX, startY := startCoordsForMap(t, bundle, testMapID)
	ch, err := w.CreateCharacterWithAppearance("test", "tester3", "warrior", 0, 0, testMapID, startX, startY)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch, err = w.Walk(ch, startX+1, startY, 2)
	if err != nil {
		t.Fatalf("Walk() on config map error = %v", err)
	}
	ch, err = w.Walk(ch, startX+2, startY, 2)
	if err != nil {
		t.Fatalf("Walk() on config map error = %v", err)
	}
	monsters, _ := w.SnapshotAround(ch.MapID, 0, 0, 99999)
	if len(monsters) == 0 {
		t.Fatalf("expected monsters on config map")
	}
	if _, err := attackMonsterForTest(w, ch, monsters[0].ID); err != nil {
		t.Fatalf("Attack() on config map error = %v", err)
	}
}

func newTestWorldCharacter(t *testing.T) (*World, storage.Character) {
	t.Helper()
	bundle := loadTestBundle(t)
	addSpawnNearDefault(t, &bundle, testMonsterID, 2, 0)
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	w := New(bundle, store)
	mapID, x, y := defaultSpawn(bundle)
	ch, err := w.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch.ClientTick = 1
	return w, ch
}

func TestDoSpellRejectsWarriorEntrySkills(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	for _, skillID := range []string{"基本剑术", "精神力战法", "攻杀剑术", "刺杀剑术", "半月弯刀", "烈火剑法", "野蛮冲撞"} {
		ch.MP = 100
		result, err := w.DoSpell(ch, skillID, ch.X, ch.Y, 0, nil)
		if err == nil {
			t.Fatalf("DoSpell(%q) error = nil, want entry boundary rejection", skillID)
		}
		if result.Character.MP != 0 || result.SpellStarted {
			t.Fatalf("DoSpell(%q) result = %+v, want no resource mutation", skillID, result)
		}
	}
}

func TestDoSpellConsumesResourceBeforeOutOfRangeFailure(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	caster.Skills = storage.SkillStates{{ID: "火球术", Level: 0, Train: 0}}
	caster.MP = 100
	skill, ok := w.Skill("火球术")
	if !ok {
		t.Fatal("skill 火球术 missing from config")
	}
	cost := w.SpellCost(skill, caster.Skills[0])
	result, err := w.DoSpell(caster, "火球术", caster.X+9, caster.Y, 0, nil)
	if err == nil {
		t.Fatal("DoSpell() expected range rejection")
	}
	if result.SpellStarted || result.ManaCost != cost || result.Character.MP != caster.MP-cost || !result.ManaConsumed || len(result.Events) != 0 {
		t.Fatalf("range failure result = %+v, want consumed resource and no start event", result)
	}
}

func TestHealingGaugePrecedesMagicFire(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	target, err := w.CreateCharacterWithAppearance("healing-order-target", "healing-order-target", "warrior", 0, 0, caster.MapID, caster.X+1, caster.Y)
	if err != nil {
		t.Fatalf("CreateCharacterWithAppearance() error = %v", err)
	}
	caster.Skills = storage.SkillStates{
		{ID: "治愈术", Level: 0},
		{ID: "心灵启示", Level: 2},
	}
	caster.MP = 100
	target.HP = 1
	target.MaxHP = 100

	result, err := w.DoSpell(caster, "治愈术", target.X, target.Y, CharacterActorID(target), []storage.Character{target})
	if err != nil {
		t.Fatalf("DoSpell() error = %v", err)
	}
	gaugeIndex, magicFireIndex := -1, -1
	for i, event := range result.Events {
		switch event.Kind {
		case SpellEventHealingGauge:
			gaugeIndex = i
		case SpellEventMagicFire:
			magicFireIndex = i
		}
	}
	if gaugeIndex < 0 || magicFireIndex < 0 || gaugeIndex >= magicFireIndex {
		t.Fatalf("events = %+v, want healing gauge before magic fire", result.Events)
	}
}

func TestDeadExplicitSpellTargetOnlyAffectsStartSnapshot(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	target, err := w.CreateCharacterWithAppearance("dead-target-order", "dead-target-order", "warrior", 0, 0, caster.MapID, caster.X+1, caster.Y)
	if err != nil {
		t.Fatalf("CreateCharacterWithAppearance() error = %v", err)
	}
	caster.Skills = storage.SkillStates{{ID: "火球术", Level: 0}}
	caster.MP = 100
	target.HP = 0

	result, err := w.DoSpell(caster, "火球术", target.X-1, target.Y, CharacterActorID(target), []storage.Character{target})
	if err != nil {
		t.Fatalf("DoSpell() error = %v", err)
	}
	var start, magicFire *SpellEvent
	for i := range result.Events {
		switch result.Events[i].Kind {
		case SpellEventStart:
			start = &result.Events[i]
		case SpellEventMagicFire:
			magicFire = &result.Events[i]
		}
	}
	if start == nil || start.TargetID != CharacterActorID(target) || start.TargetX != target.X || start.TargetY != target.Y {
		t.Fatalf("start event = %+v, want dead target snapshot", start)
	}
	if magicFire == nil || magicFire.TargetID != 0 || magicFire.TargetX != target.X || magicFire.TargetY != target.Y {
		t.Fatalf("magic fire event = %+v, want cleared dead target", magicFire)
	}
}

func TestGroundEventsAroundFiltersExpiredAndOutOfRange(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	now := time.Now()
	w.mu.Lock()
	w.groundEvents = map[int32]SpellGroundEvent{
		30: {ID: 30, MapID: ch.MapID, X: ch.X, Y: ch.Y, Duration: time.Minute, StartAt: now},
		10: {ID: 10, MapID: ch.MapID, X: ch.X + 1, Y: ch.Y, Duration: time.Minute, StartAt: now},
		20: {ID: 20, MapID: ch.MapID, X: ch.X, Y: ch.Y, Duration: time.Second, StartAt: now.Add(-2 * time.Second)},
		40: {ID: 40, MapID: "other", X: ch.X, Y: ch.Y, Duration: time.Minute, StartAt: now},
		50: {ID: 50, MapID: ch.MapID, X: ch.X + 3, Y: ch.Y, Duration: time.Minute, StartAt: now},
	}
	w.mu.Unlock()

	events := w.GroundEventsAround(ch.MapID, ch.X, ch.Y, 1, now)
	if got := []int32{events[0].ID, events[1].ID}; !reflect.DeepEqual(got, []int32{10, 30}) {
		t.Fatalf("GroundEventsAround() IDs = %v, want [10 30]", got)
	}
}

func TestGroundEventsAroundKeepsEventAtExactExpiry(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	now := time.Now()
	w.mu.Lock()
	w.groundEvents = map[int32]SpellGroundEvent{
		1: {ID: 1, MapID: ch.MapID, X: ch.X, Y: ch.Y, Duration: time.Second, StartAt: now.Add(-time.Second)},
	}
	w.mu.Unlock()

	events := w.GroundEventsAround(ch.MapID, ch.X, ch.Y, 0, now)
	if len(events) != 1 || events[0].ID != 1 {
		t.Fatalf("GroundEventsAround() at exact expiry = %+v, want event 1", events)
	}
}

func TestInstantTeleportOrdersMagicFireBeforeSpaceMove(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	ch.Skills = storage.SkillStates{{ID: "瞬息移动", Level: 5, Train: 0}}
	skill, ok := w.Skill("瞬息移动")
	if !ok {
		t.Fatal("skill 瞬息移动 missing from config")
	}
	ch.MP = w.SpellCost(skill, ch.Skills[0]) + 1
	w.rand.Seed(1)
	result, err := w.DoSpell(ch, "瞬息移动", ch.X, ch.Y, 0, nil)
	if err != nil {
		t.Fatalf("DoSpell() error = %v", err)
	}
	fireIndex, mapChangeIndex, showIndex := -1, -1, -1
	magicFireIndex := -1
	for index, event := range result.Events {
		switch event.Kind {
		case SpellEventSpaceMoveFire:
			fireIndex = index
		case SpellEventSpaceMoveMapChange:
			mapChangeIndex = index
		case SpellEventSpaceMoveShow:
			showIndex = index
		case SpellEventMagicFire:
			magicFireIndex = index
			if event.Caster.X != ch.X || event.Caster.Y != ch.Y {
				t.Fatalf("space move magic-fire caster position = (%d,%d), want source (%d,%d)", event.Caster.X, event.Caster.Y, ch.X, ch.Y)
			}
		}
	}
	if magicFireIndex < 0 || fireIndex < 0 || mapChangeIndex < 0 || showIndex < 0 || !(magicFireIndex < fireIndex && fireIndex < mapChangeIndex && mapChangeIndex < showIndex) {
		t.Fatalf("teleport event order = magic:%d fire:%d map:%d show:%d", magicFireIndex, fireIndex, mapChangeIndex, showIndex)
	}
	for _, event := range result.Events {
		if event.Kind == SpellEventTeleport {
			t.Fatal("successful space move emitted generic teleport event")
		}
		if event.Kind == SpellEventCharacter {
			t.Fatal("successful space move emitted generic character event")
		}
	}
}

func TestWalkMovesOneTileAndSetsDirection(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	updated, err := w.Walk(ch, ch.X+1, ch.Y, 2)
	if err != nil {
		t.Fatalf("Walk() error = %v", err)
	}
	if updated.X != ch.X+1 || updated.Y != ch.Y || updated.Dir != 2 {
		t.Fatalf("Walk() = %+v", updated)
	}
}

func TestMoveShortensTransparentDurationOnMovement(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	before := time.Now().Add(10 * time.Second).UnixNano()
	ch.TransparentUntil = before
	updated, err := w.Move(ch, ch.X+1, ch.Y)
	if err != nil {
		t.Fatalf("Move() error = %v", err)
	}
	if updated.TransparentUntil == before {
		t.Fatalf("TransparentUntil = %d, want shortened", updated.TransparentUntil)
	}
	if updated.TransparentUntil <= time.Now().UnixNano() {
		t.Fatalf("TransparentUntil = %d, want future expiration", updated.TransparentUntil)
	}
	if updated.TransparentUntil > time.Now().Add(2*time.Second).UnixNano() {
		t.Fatalf("TransparentUntil = %d, want near-term expiration", updated.TransparentUntil)
	}
}

func TestWalkRejectsTooFar(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	if _, err := w.Walk(ch, ch.X+2, ch.Y, 2); err == nil {
		t.Fatalf("Walk() expected error for a two-tile step")
	}
}

func TestRunMovesTwoTiles(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	updated, err := w.Run(ch, ch.X+2, ch.Y, 2)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if updated.X != ch.X+2 || updated.Dir != 2 {
		t.Fatalf("Run() = %+v", updated)
	}
}

func TestRunRejectsCoordinatesOffTheDirectionLine(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	if _, err := w.Run(ch, ch.X+2, ch.Y+1, 2); err == nil {
		t.Fatalf("Run() expected error for a destination direction 2 (right) cannot reach")
	}
}

func TestRunRejectsBlockedIntermediateTile(t *testing.T) {
	bundle := loadTestBundle(t)
	mp := bundle.Maps[testMapID]
	startX, startY := startCoordsForMap(t, bundle, testMapID)
	mp.Blocked = append(mp.Blocked, data.StdPoint{X: startX + 1, Y: startY})
	bundle.Maps[testMapID] = mp
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	w := New(bundle, store)
	ch, err := w.CreateCharacterWithAppearance("test", "tester1", "warrior", 0, 0, testMapID, startX, startY)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	if _, err := w.Run(ch, startX+2, startY, 2); err == nil {
		t.Fatalf("Run() expected error crossing the blocked intermediate tile")
	}
}

func TestSitDownTogglesSitting(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	updated, err := w.SitDown(ch, ch.X, ch.Y, 4)
	if err != nil {
		t.Fatalf("SitDown() error = %v", err)
	}
	if !updated.Sitting {
		t.Fatalf("expected Sitting = true after first SitDown()")
	}
	updated, err = w.SitDown(updated, updated.X, updated.Y, 4)
	if err != nil {
		t.Fatalf("SitDown() error = %v", err)
	}
	if updated.Sitting {
		t.Fatalf("expected Sitting = false after second SitDown()")
	}
}

func TestSitDownRejectsCoordinateMismatch(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	if _, err := w.SitDown(ch, ch.X+1, ch.Y, 4); err == nil {
		t.Fatalf("SitDown() expected error for mismatched coordinates")
	}
}

func TestHitConnectsWithMonsterInFront(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	ch, err := w.Walk(ch, ch.X+1, ch.Y, 2)
	if err != nil {
		t.Fatalf("Walk() error = %v", err)
	}
	monsters, _ := w.SnapshotAround(ch.MapID, 0, 0, 99999)
	if len(monsters) == 0 {
		t.Fatalf("expected monsters")
	}
	result, err := w.Hit(ch, ch.X, ch.Y, 2)
	if err != nil {
		t.Fatalf("Hit() error = %v", err)
	}
	if result.MonsterID == "" {
		t.Fatalf("expected Hit() to connect with a monster")
	}
	if result.Character.Dir != 2 {
		t.Fatalf("Hit() did not persist facing direction")
	}
}

func TestHitMissesWithoutTargetInFront(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	result, err := w.Hit(ch, ch.X, ch.Y, 0)
	if err != nil {
		t.Fatalf("Hit() error = %v", err)
	}
	if result.MonsterID != "" {
		t.Fatalf("expected a miss, got monster %q", result.MonsterID)
	}
	if result.Character.Dir != 0 {
		t.Fatalf("Hit() did not persist facing direction on a miss")
	}
}

func TestHitRejectsCoordinateMismatch(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	if _, err := w.Hit(ch, ch.X+1, ch.Y, 2); err == nil {
		t.Fatalf("Hit() expected error for mismatched coordinates")
	}
}

func prepareHitDamageTestWorld(t *testing.T, skills storage.SkillStates) (*World, storage.Character) {
	t.Helper()
	w, ch := newTestWorldCharacter(t)
	w.mu.Lock()
	w.monsters = map[string]*Monster{}
	w.occupied = map[monsterPosition]string{}
	w.rand = rand.New(rand.NewSource(1))
	w.mu.Unlock()
	ch.Skills = skills
	result, err := w.SpawnMonsterByNameAt(ch.MapID, ch.X+1, ch.Y, "鸡", 1)
	if err != nil {
		t.Fatalf("SpawnMonsterByNameAt() error = %v", err)
	}
	if len(result.Monsters) != 1 {
		t.Fatalf("SpawnMonsterByNameAt() monsters = %d, want 1", len(result.Monsters))
	}
	w.mu.Lock()
	mon := w.monsters[result.Monsters[0].ID]
	mon.HP = 1000
	mon.MaxHP = 1000
	w.mu.Unlock()
	return w, ch
}

func TestSpecialMeleeHitUpdatesReferenceTargetLock(t *testing.T) {
	w, ch := prepareHitDamageTestWorld(t, storage.SkillStates{{ID: "半月弯刀", Level: 0}})
	w.mu.Lock()
	w.rand = rand.New(zeroSource{})
	var mon *Monster
	for _, candidate := range w.monsters {
		mon = candidate
		break
	}
	mon.X = ch.X + 1
	mon.Y = ch.Y - 1
	mon.Speed = 1
	w.mu.Unlock()

	result, err := w.HitWithIdent(ch, ch.X, ch.Y, 2, mir176.CMWideHit)
	if err != nil {
		t.Fatalf("HitWithIdent() error = %v", err)
	}
	if result.Character.TargetID != mon.ID {
		t.Fatalf("target lock = %q, want %q", result.Character.TargetID, mon.ID)
	}
}

func TestMeleeHitUpdatesReferenceTargetLock(t *testing.T) {
	w, ch := prepareHitDamageTestWorld(t, nil)
	w.mu.Lock()
	w.rand = rand.New(zeroSource{})
	var mon *Monster
	for _, candidate := range w.monsters {
		mon = candidate
		break
	}
	mon.Speed = 1
	w.mu.Unlock()

	result, err := w.HitWithIdent(ch, ch.X, ch.Y, 2, mir176.CMHit)
	if err != nil {
		t.Fatalf("HitWithIdent() error = %v", err)
	}
	if result.Character.TargetID != mon.ID {
		t.Fatalf("target lock = %q, want %q", result.Character.TargetID, mon.ID)
	}
}

func TestCombatStatsIncludesWeaponSkillBonuses(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	ch.Skills = storage.SkillStates{
		{ID: "基本剑术", Level: 0, Train: 0},
		{ID: "精神力战法", Level: 1, Train: 0},
	}
	got := w.CombatStats(ch)
	if got.Hit != 3 {
		t.Fatalf("CombatStats().Hit = %d, want basic sword hit bonus", got.Hit)
	}
	if got.Speed != 0 {
		t.Fatalf("CombatStats().Speed = %d, want no speed bonus from skills", got.Speed)
	}
}

func TestAbilitiesIncludesSpiritPowerBonus(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	ch.Skills = storage.SkillStates{
		{ID: "基本剑术", Level: 0, Train: 0},
		{ID: "精神力战法", Level: 1, Train: 0},
	}
	base := Base(ch.Class, ch.Level)
	got := w.Abilities(ch)
	if got.DC != PackWord(base.DC, base.DCMax, 0, 0) {
		t.Fatalf("Abilities().DC = %d, want no basic sword bonus there", got.DC)
	}
	if got.SC != PackWord(base.SC+2, base.SCMax+2, 0, 0) {
		t.Fatalf("Abilities().SC = %d, want spirit power bonus applied", got.SC)
	}
}

func TestHitWithWarriorSkillsImprovesHitChance(t *testing.T) {
	baseW, baseCh := prepareHitDamageTestWorld(t, nil)
	baseHit := baseW.characterHitPointLocked(baseCh)
	baseW.mu.Lock()
	for _, mon := range baseW.monsters {
		mon.Defense = 0
		mon.Speed = 1
		break
	}
	baseW.mu.Unlock()
	skillW, skillCh := prepareHitDamageTestWorld(t, storage.SkillStates{{ID: "基本剑术", Level: 0, Train: 0}})
	skillHit := skillW.characterHitPointLocked(skillCh)
	skillW.mu.Lock()
	for _, mon := range skillW.monsters {
		mon.Defense = 0
		mon.Speed = skillHit + 1
		break
	}
	skillW.mu.Unlock()
	roll := int64(baseHit+1) << 32
	baseW.mu.Lock()
	for _, mon := range baseW.monsters {
		mon.Speed = skillHit + 1
		break
	}
	baseW.rand = rand.New(&seqSource{vals: []int64{roll}})
	baseW.mu.Unlock()
	skillW.rand = rand.New(&seqSource{vals: []int64{roll, 7}})
	baseResult, err := baseW.HitWithIdent(baseCh, baseCh.X, baseCh.Y, 2, mir176.CMHit)
	if err != nil {
		t.Fatalf("base HitWithIdent() error = %v", err)
	}
	skillResult, err := skillW.HitWithIdent(skillCh, skillCh.X, skillCh.Y, 2, mir176.CMHit)
	if err != nil {
		t.Fatalf("skill HitWithIdent() error = %v", err)
	}
	if baseResult.Damage != 0 {
		t.Fatalf("base damage = %d, want miss at this roll", baseResult.Damage)
	}
	if skillResult.Damage <= 0 {
		t.Fatalf("skill damage = %d, want hit after basic sword bonus", skillResult.Damage)
	}
}

func TestHitWithIdentSkillLevelUpSendsReferenceExpMessage(t *testing.T) {
	w, ch := prepareHitDamageTestWorld(t, storage.SkillStates{{ID: "基本剑术", Level: 0, Train: 99}})
	ch.Level = 7
	w.mu.Lock()
	for _, mon := range w.monsters {
		mon.Speed = 1
		mon.Defense = 0
	}
	w.mu.Unlock()
	result, err := w.HitWithIdent(ch, ch.X, ch.Y, 2, mir176.CMHit)
	if err != nil {
		t.Fatalf("HitWithIdent() error = %v", err)
	}
	if !result.SkillExp {
		t.Fatal("SkillExp = false, want delayed message after skill level-up")
	}
	if result.SkillExpDelay != 800*time.Millisecond {
		t.Fatalf("SkillExpDelay = %s, want 800ms after skill level-up", result.SkillExpDelay)
	}
	if result.SkillLevel != 1 {
		t.Fatalf("SkillLevel = %d, want 1 after skill level-up", result.SkillLevel)
	}
}

func TestSkillTrainingUsesCurrentLevelRequirements(t *testing.T) {
	skill := data.StdSkill{
		NeedLevel1:  7,
		NeedLevel2:  20,
		NeedLevel3:  30,
		TrainLevel1: 10,
		TrainLevel2: 30,
		TrainLevel3: 30,
	}
	state := storage.SkillState{Level: 1, Train: 19}
	w := &World{}
	if w.applySkillTrainingLocked(19, skill, &state, 1) {
		t.Fatal("training should be gated by NeedLevel2")
	}
	if state.Train != 19 || state.Level != 1 {
		t.Fatalf("state after gated training = %+v, want unchanged", state)
	}
	if !w.applySkillTrainingLocked(20, skill, &state, 1) {
		t.Fatal("training should pass NeedLevel2")
	}
	if state.Level != 1 || state.Train != 20 {
		t.Fatalf("state after level 2 training = %+v, want level 1/train 20", state)
	}
	state.Train = 29
	if !w.applySkillTrainingLocked(20, skill, &state, 1) {
		t.Fatal("second-level training should remain available")
	}
	if state.Level != 2 || state.Train != 0 {
		t.Fatalf("state after level-up = %+v, want level 2/train 0", state)
	}
}

func TestSkillTrainingPreservesReferenceIntegerRange(t *testing.T) {
	w, _ := newTestWorldCharacter(t)
	skill := data.StdSkill{ID: "训练测试", NeedLevel1: 1, TrainLevel1: 100000}
	state := storage.SkillState{ID: skill.ID, Level: 0, Train: 65535}
	if !w.applySkillTrainingLocked(1, skill, &state, 1) {
		t.Fatal("applySkillTrainingLocked() = false, want true")
	}
	if state.Train != 65536 {
		t.Fatalf("Train = %d, want 65536 without uint16 truncation", state.Train)
	}
}

func TestHeavyHitDoesNotTrainPowerHitSkill(t *testing.T) {
	w, ch := prepareHitDamageTestWorld(t, storage.SkillStates{{ID: "攻杀剑术", Level: 0, Train: 0}})
	result, err := w.HitWithIdent(ch, ch.X, ch.Y, 2, mir176.CMHeavyHit)
	if err != nil {
		t.Fatalf("HitWithIdent() error = %v", err)
	}
	if len(result.Character.Skills) != 1 {
		t.Fatalf("skills = %+v, want one learned skill", result.Character.Skills)
	}
	if got := result.Character.Skills[0].Train; got != 0 {
		t.Fatalf("skill train = %d, want unchanged for heavy hit", got)
	}
}

func TestPowerHitTrainingUsesReferenceRandomRange(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	seenAboveOne := false
	for i := 0; i < 32; i++ {
		points, ok := meleeSkillTrainPoints(r, mir176.CMPowerHit)
		if !ok || points < 1 || points > 3 {
			t.Fatalf("power-hit training points = %d, ok=%t; want 1..3", points, ok)
		}
		seenAboveOne = seenAboveOne || points > 1
	}
	if !seenAboveOne {
		t.Fatal("power-hit training never produced more than one point")
	}
}

func TestHitWithWarriorSkillsTrainsBasicSword(t *testing.T) {
	w, ch := prepareHitDamageTestWorld(t, storage.SkillStates{{ID: "基本剑术", Level: 0, Train: 0}})
	ch.Level = 7
	result, err := w.HitWithIdent(ch, ch.X, ch.Y, 2, mir176.CMHit)
	if err != nil {
		t.Fatalf("HitWithIdent() error = %v", err)
	}
	if len(result.Character.Skills) != 1 {
		t.Fatalf("skills = %+v, want one learned skill", result.Character.Skills)
	}
	if got := result.Character.Skills[0].Train; got < 1 || got > 3 {
		t.Fatalf("skill train = %d, want 1..3 after hit", got)
	}
}

func TestSpecialMeleeHitTrainsBasicAndSpecialSkills(t *testing.T) {
	w, ch := prepareHitDamageTestWorld(t, storage.SkillStates{
		{ID: "基本剑术", Level: 0, Train: 0},
		{ID: "刺杀剑术", Level: 0, Train: 0},
	})
	ch.Level = 25
	w.mu.Lock()
	w.rand = rand.New(zeroSource{})
	for _, mon := range w.monsters {
		mon.Speed = 1
		mon.Defense = 0
	}
	w.mu.Unlock()

	result, err := w.HitWithIdent(ch, ch.X, ch.Y, 2, mir176.CMLongHit)
	if err != nil {
		t.Fatalf("HitWithIdent() error = %v", err)
	}
	basic, _, ok := result.Character.Skills.Get("基本剑术")
	if !ok || basic.Train != 1 {
		t.Fatalf("basic sword = %+v, want one training point", basic)
	}
	thrust, _, ok := result.Character.Skills.Get("刺杀剑术")
	if !ok || thrust.Train != 1 {
		t.Fatalf("thrusting sword = %+v, want one training point", thrust)
	}
	if len(result.SkillExperiences) != 2 {
		t.Fatalf("SkillExperiences = %+v, want basic and special notifications", result.SkillExperiences)
	}
}

func TestHitWithWarriorSkillsDoesNotTrainBasicSwordOnZeroDamage(t *testing.T) {
	w, ch := prepareHitDamageTestWorld(t, storage.SkillStates{{ID: "基本剑术", Level: 0, Train: 0}})
	w.mu.Lock()
	for _, mon := range w.monsters {
		mon.Defense = 9999
		break
	}
	w.mu.Unlock()
	hitResult, err := w.HitWithIdent(ch, ch.X, ch.Y, 2, mir176.CMHit)
	if err != nil {
		t.Fatalf("HitWithIdent() error = %v", err)
	}
	if hitResult.MonsterID == "" || hitResult.Damage != 0 {
		t.Fatalf("AttackResult = %+v, want zero damage on blocked hit", hitResult)
	}
	if len(hitResult.Character.Skills) != 1 {
		t.Fatalf("skills = %+v, want one learned skill", hitResult.Character.Skills)
	}
	if got := hitResult.Character.Skills[0].Train; got != 0 {
		t.Fatalf("skill train = %d, want unchanged on blocked hit", got)
	}
}

func TestFireHitUsesFireSwordBonus(t *testing.T) {
	baseW, baseCh := prepareHitDamageTestWorld(t, storage.SkillStates{{ID: "烈火剑法", Level: 0, Train: 0}})
	baseW.mu.Lock()
	for _, mon := range baseW.monsters {
		mon.Speed = 1
		break
	}
	baseW.mu.Unlock()
	baseResult, err := baseW.HitWithIdent(baseCh, baseCh.X, baseCh.Y, 2, mir176.CMHit)
	if err != nil {
		t.Fatalf("base HitWithIdent() error = %v", err)
	}
	fireW, fireCh := prepareHitDamageTestWorld(t, storage.SkillStates{{ID: "烈火剑法", Level: 0, Train: 0}})
	fireW.mu.Lock()
	for _, mon := range fireW.monsters {
		mon.Speed = 1
		break
	}
	fireW.mu.Unlock()
	fireResult, err := fireW.HitWithIdent(fireCh, fireCh.X, fireCh.Y, 2, mir176.CMFireHit)
	if err != nil {
		t.Fatalf("fire HitWithIdent() error = %v", err)
	}
	if fireResult.Damage <= baseResult.Damage {
		t.Fatalf("fire damage = %d, base = %d, want higher with fire sword", fireResult.Damage, baseResult.Damage)
	}
}

func TestCastSkillDoesNotAddIndependentCooldown(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	ch.Skills = storage.SkillStates{{ID: "火球术", Level: 0, Train: 0}}
	ch.MP = 100

	first, err := w.CastSkillWithPlayers(ch, "火球术", ch.X+1, ch.Y, 0, nil)
	if err != nil {
		t.Fatalf("first CastSkillWithPlayers() error = %v", err)
	}

	second, err := w.CastSkillWithPlayers(first.Character, "火球术", ch.X+1, ch.Y, 0, nil)
	if err != nil {
		t.Fatalf("second CastSkillWithPlayers() error = %v, want world execution to remain available", err)
	}
	if second.SkillID != "火球术" {
		t.Fatalf("second result skill = %q, want 火球术", second.SkillID)
	}
}

func TestCastSkillFireballDefersMonsterHit(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	w.mu.Lock()
	w.monsters = map[string]*Monster{}
	w.occupied = map[monsterPosition]string{}
	w.mu.Unlock()
	mapID := caster.MapID
	targetX, targetY := caster.X+1, caster.Y
	spawned, err := w.SpawnMonsterByNameAt(mapID, targetX, targetY, "鸡", 1)
	if err != nil {
		t.Fatalf("SpawnMonsterByNameAt() error = %v", err)
	}
	caster.Skills = storage.SkillStates{{ID: "火球术", Level: 0, Train: 0}}
	caster.MP = 100
	targetID := MonsterActorID(spawned.Monsters[0])
	cast, err := w.DoSpell(caster, "火球术", targetX, targetY, targetID, nil)
	if err != nil {
		t.Fatalf("DoSpell() error = %v", err)
	}
	if cast.MonsterHit != nil || len(cast.MonsterHits) != 0 {
		t.Fatalf("cast hits = %+v, want deferred hit", cast)
	}
	if len(cast.Events) < 4 || cast.Events[0].Kind != SpellEventCasterState || cast.Events[1].Kind != SpellEventStart || cast.Events[2].Kind != SpellEventCharacter || cast.Events[3].Kind != SpellEventMagicFire {
		t.Fatalf("cast events = %+v, want caster state, start, character result, then magic fire", cast.Events)
	}
	if cast.Events[1].TargetID != targetID {
		t.Fatalf("spell start target = %d, want explicit monster actor %d", cast.Events[1].TargetID, targetID)
	}
	deadCaster := cast.Character
	deadCaster.HP = 0
	players := []PlayerSnapshot{{Character: deadCaster}}
	before, err := w.Tick(players, time.Now().Add(500*time.Millisecond))
	if err != nil {
		t.Fatalf("early Tick() error = %v", err)
	}
	if len(before.MonsterHits) != 0 {
		t.Fatalf("early MonsterHits = %d, want 0", len(before.MonsterHits))
	}
	after, err := w.Tick(players, time.Now().Add(2*time.Second))
	if err != nil {
		t.Fatalf("late Tick() error = %v", err)
	}
	if len(after.MonsterHits) != 1 {
		t.Fatalf("late MonsterHits = %d, want 1", len(after.MonsterHits))
	}
	if after.MonsterHits[0].MonsterID != spawned.Monsters[0].ID {
		t.Fatalf("late MonsterHit ID = %q, want %q", after.MonsterHits[0].MonsterID, spawned.Monsters[0].ID)
	}
}

func TestSingleTargetMonsterSpellsOnlyTrainOnAnimalTargets(t *testing.T) {
	for _, skillID := range []string{"火球术", "雷电术", "灵魂火符"} {
		t.Run(skillID, func(t *testing.T) {
			w, caster := newTestWorldCharacter(t)
			w.mu.Lock()
			w.monsters = map[string]*Monster{}
			w.occupied = map[monsterPosition]string{}
			target := &Monster{ID: "humanoid", Race: 1, MapID: caster.MapID, X: caster.X + 1, Y: caster.Y, HP: 100, MaxHP: 100, Alive: true}
			w.monsters[target.ID] = target
			w.occupied[monsterPosition{MapID: target.MapID, X: target.X, Y: target.Y}] = target.ID
			w.mu.Unlock()
			caster.Skills = storage.SkillStates{{ID: skillID, Level: 0, Train: 0}}
			caster.MP = 100
			if skillID == "灵魂火符" {
				caster.EquippedItems = map[int]storage.UserItem{SlotBujuk: {ItemID: "护身符", Dura: 10000}}
			}
			result, err := w.DoSpell(caster, skillID, target.X, target.Y, MonsterActorID(*target), nil)
			if err != nil {
				t.Fatalf("DoSpell() error = %v", err)
			}
			if result.SkillTraining {
				t.Fatalf("SkillTraining = true for non-animal target, result = %+v", result)
			}
		})
	}
}

func TestDelayedMonsterMagicHitReportsHealthWhenShown(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	caster.Skills = storage.SkillStates{{ID: "火球术", Level: 0, Train: 0}}
	caster.MP = 100
	targetX, targetY := caster.X+1, caster.Y
	spawned, err := w.SpawnMonsterByNameAt(caster.MapID, targetX, targetY, "鸡", 1)
	if err != nil {
		t.Fatalf("SpawnMonsterByNameAt() error = %v", err)
	}
	w.mu.Lock()
	w.monsters[spawned.Monsters[0].ID].ShowHPUntil = time.Now().Add(time.Minute).UnixNano()
	w.monsters[spawned.Monsters[0].ID].MP = 7
	w.monsters[spawned.Monsters[0].ID].MaxMP = 13
	w.mu.Unlock()
	cast, err := w.DoSpell(caster, "火球术", targetX, targetY, MonsterActorID(spawned.Monsters[0]), nil)
	if err != nil {
		t.Fatalf("DoSpell() error = %v", err)
	}
	tick, err := w.Tick([]PlayerSnapshot{{Character: cast.Character}}, time.Now().Add(2*time.Second))
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if len(tick.MonsterHits) != 1 || !tick.MonsterHits[0].MonsterHealthChanged {
		t.Fatalf("monster hits = %+v, want shown-health synchronization before magic struck", tick.MonsterHits)
	}
	if tick.MonsterHits[0].MonsterMP != 7 || tick.MonsterHits[0].MonsterMaxMP != 13 {
		t.Fatalf("monster health snapshot = %d/%d, want MP/MaxMP 7/13", tick.MonsterHits[0].MonsterMP, tick.MonsterHits[0].MonsterMaxMP)
	}
}

func TestCastSkillFireballDoesNotTrainOnPlayerTarget(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	target, err := w.CreateCharacterWithAppearance("test2", "target", "warrior", 0, 0, caster.MapID, caster.X+1, caster.Y)
	if err != nil {
		t.Fatalf("CreateCharacterWithAppearance() error = %v", err)
	}
	caster.Skills = storage.SkillStates{{ID: "火球术", Level: 0, Train: 0}}
	caster.MP = 100
	w.rand = rand.New(zeroSource{})
	result, err := w.DoSpell(caster, "火球术", target.X, target.Y, CharacterActorID(target), []storage.Character{target})
	if err != nil {
		t.Fatalf("DoSpell() error = %v", err)
	}
	if result.SkillTraining || result.Character.Skills[0].Train != 0 {
		t.Fatalf("player-target training = %v, train=%d; want no training", result.SkillTraining, result.Character.Skills[0].Train)
	}
}

func TestCastSkillSingleTargetSpellsDoNotTrainOnPlayerTarget(t *testing.T) {
	for _, skillID := range []string{"灵魂火符", "雷电术"} {
		t.Run(skillID, func(t *testing.T) {
			w, caster := newTestWorldCharacter(t)
			target, err := w.CreateCharacterWithAppearance("test2", "target", "warrior", 0, 0, caster.MapID, caster.X+1, caster.Y)
			if err != nil {
				t.Fatalf("CreateCharacterWithAppearance() error = %v", err)
			}
			caster.Skills = storage.SkillStates{{ID: skillID, Level: 0, Train: 0}}
			caster.EquippedItems = map[int]storage.UserItem{SlotBujuk: {ItemID: "护身符", Dura: 10000}}
			caster.MP = 100
			w.rand = rand.New(twoSource{})
			result, err := w.DoSpell(caster, skillID, target.X, target.Y, CharacterActorID(target), []storage.Character{target})
			if err != nil {
				t.Fatalf("DoSpell() error = %v", err)
			}
			if !result.SpellStarted || result.SkillTraining || result.Character.Skills[0].Train != 0 {
				t.Fatalf("player-target %s training = %v, train=%d; want no training", skillID, result.SkillTraining, result.Character.Skills[0].Train)
			}
		})
	}
}

func TestCastSkillFireballDoesNotRetargetAfterInvalidMonsterTarget(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	target, err := w.CreateCharacterWithAppearance("test2", "target", "warrior", 0, 0, caster.MapID, caster.X+1, caster.Y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	caster.Skills = storage.SkillStates{{ID: "火球术", Level: 0, Train: 0}}
	caster.MP = 100
	mon := &Monster{ID: "invalid-monster", Name: "invalid", MapID: caster.MapID, X: target.X, Y: target.Y, HP: 100, MaxHP: 100, Alive: true, AdminMode: true}
	w.mu.Lock()
	w.monsters[mon.ID] = mon
	w.mu.Unlock()
	result, err := w.DoSpell(caster, "火球术", target.X, target.Y, MonsterActorID(*mon), []storage.Character{target})
	if err != nil {
		t.Fatalf("DoSpell() error = %v", err)
	}
	if len(w.pendingSpells) != 0 || result.MagicTargetID != 0 {
		t.Fatalf("invalid monster target result = %+v, pending=%d; want no retarget", result, len(w.pendingSpells))
	}
}

func TestCastSkillSingleTargetDoesNotRetargetAfterInvalidMonsterTarget(t *testing.T) {
	for _, skillID := range []string{"灵魂火符", "雷电术"} {
		t.Run(skillID, func(t *testing.T) {
			w, caster := newTestWorldCharacter(t)
			target, err := w.CreateCharacterWithAppearance("test2", "target", "warrior", 0, 0, caster.MapID, caster.X+1, caster.Y)
			if err != nil {
				t.Fatalf("CreateCharacter() error = %v", err)
			}
			caster.Skills = storage.SkillStates{{ID: skillID, Level: 0, Train: 0}}
			caster.MP = 100
			if skillID == "灵魂火符" {
				caster.EquippedItems = map[int]storage.UserItem{SlotBujuk: {ItemID: "护身符", Dura: 10000}}
			}
			mon := &Monster{ID: "invalid-" + skillID, Name: "invalid", MapID: caster.MapID, X: target.X, Y: target.Y, HP: 100, MaxHP: 100, Alive: true, AdminMode: true}
			w.mu.Lock()
			w.monsters[mon.ID] = mon
			w.mu.Unlock()
			result, err := w.DoSpell(caster, skillID, target.X, target.Y, MonsterActorID(*mon), []storage.Character{target})
			if err != nil {
				t.Fatalf("DoSpell() error = %v", err)
			}
			if len(w.pendingSpells) != 0 || result.MagicTargetID != 0 {
				t.Fatalf("invalid monster target result = %+v, pending=%d; want no retarget", result, len(w.pendingSpells))
			}
		})
	}
}

func TestCastSkillDoesNotResolveDeadTargetID(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	target, err := w.CreateCharacterWithAppearance("test2", "dead", "warrior", 0, 0, caster.MapID, caster.X+1, caster.Y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	target.HP = 0
	caster.Skills = storage.SkillStates{{ID: "火球术", Level: 0, Train: 0}}
	caster.MP = 100
	result, err := w.DoSpell(caster, "火球术", target.X, target.Y, CharacterActorID(target), []storage.Character{target})
	if err != nil {
		t.Fatalf("DoSpell() error = %v", err)
	}
	for _, event := range result.Events {
		if event.Kind == SpellEventMagicFire && event.TargetID != 0 {
			t.Fatalf("dead target magic fire ID = %d, want 0", event.TargetID)
		}
	}
}

func TestDoSpellUsesResolvedTargetDirection(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	target, err := w.CreateCharacterWithAppearance("test2", "direction-target", "warrior", 0, 0, caster.MapID, caster.X+1, caster.Y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	caster.Skills = storage.SkillStates{{ID: "火球术", Level: 0, Train: 0}}
	caster.MP = 100
	result, err := w.DoSpell(caster, "火球术", target.X+1, target.Y+1, CharacterActorID(target), []storage.Character{target})
	if err != nil {
		t.Fatalf("DoSpell() error = %v", err)
	}
	if result.Character.Dir != Direction(caster.X, caster.Y, target.X, target.Y) {
		t.Fatalf("direction = %d, want resolved-target direction %d", result.Character.Dir, Direction(caster.X, caster.Y, target.X, target.Y))
	}
}

func TestSpellEventsPreserveResolvedTargetForMagicFire(t *testing.T) {
	w := &World{}
	const targetID int32 = 0x12345678
	events := w.spellEvents(storage.Character{ID: "caster"}, SkillCastResult{
		Character:        storage.Character{ID: "caster"},
		SkillID:          "爆裂火焰",
		TargetIDResolved: true,
	}, data.StdSkill{}, 10, 11, targetID)
	for _, event := range events {
		if event.Kind == SpellEventMagicFire {
			if event.TargetID != targetID {
				t.Fatalf("magic fire target = %d, want resolved actor %d", event.TargetID, targetID)
			}
			return
		}
	}
	t.Fatal("missing magic fire event")
}

func TestDoSpellLeavesEntrySpellTickUntouched(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	caster.Skills = storage.SkillStates{{ID: "魔法盾", Level: 0, Train: 0}}
	caster.MP = 100
	caster.SpellTick = 700
	result, err := w.DoSpell(caster, "魔法盾", caster.X, caster.Y, 0, nil)
	if err != nil {
		t.Fatalf("DoSpell() error = %v", err)
	}
	if result.Character.SpellTick != 700 {
		t.Fatalf("SpellTick = %d, want unchanged at 700 in world core", result.Character.SpellTick)
	}
}

func TestWorldTickSortsUpdatedCharactersForStableSpellOrder(t *testing.T) {
	w, first := newTestWorldCharacter(t)
	second := first
	first.ID = "character-z"
	second.ID = "character-a"
	now := time.Now()
	for i := range []storage.Character{first, second} {
		if i == 0 {
			first.SpellTick = 799
			first.SpellTickAt = now.Add(-20 * time.Millisecond).UnixMilli()
			first.MP = 0
		} else {
			second.SpellTick = 799
			second.SpellTickAt = now.Add(-20 * time.Millisecond).UnixMilli()
			second.MP = 0
		}
	}
	result, err := w.Tick([]PlayerSnapshot{{Character: first}, {Character: second}}, now)
	if err != nil {
		t.Fatalf("World.Tick() error = %v", err)
	}
	if len(result.Characters) != 2 {
		t.Fatalf("updated characters = %d, want 2", len(result.Characters))
	}
	if result.Characters[0].ID != "character-a" || result.Characters[1].ID != "character-z" {
		t.Fatalf("updated character order = [%s %s], want [character-a character-z]", result.Characters[0].ID, result.Characters[1].ID)
	}
}

func TestDelayedMonsterSpellDefenseDoesNotUseCasterUndeadBonus(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	w.mu.Lock()
	w.monsters = map[string]*Monster{}
	w.occupied = map[monsterPosition]string{}
	w.mu.Unlock()
	caster.Skills = storage.SkillStates{{ID: "火球术", Level: 0, Train: 0}}
	caster.MP = 100
	targetX, targetY := caster.X+1, caster.Y
	spawned, err := w.SpawnMonsterByNameAt(caster.MapID, targetX, targetY, "鸡", 1)
	if err != nil || len(spawned.Monsters) != 1 {
		t.Fatalf("SpawnMonsterByNameAt() = %+v, %v", spawned.Monsters, err)
	}
	mon := spawned.Monsters[0]
	w.mu.Lock()
	w.monsters[mon.ID].MagicDefense = 100000
	for itemID, item := range w.data.Items {
		item.Undead = 1000
		w.data.Items[itemID] = item
		caster.EquippedItems = map[int]storage.UserItem{0: {ItemID: itemID}}
		break
	}
	w.mu.Unlock()
	cast, err := w.DoSpell(caster, "火球术", targetX, targetY, MonsterActorID(mon), nil)
	if err != nil {
		t.Fatalf("DoSpell() error = %v", err)
	}
	tick, err := w.Tick([]PlayerSnapshot{{Character: cast.Character}}, time.Now().Add(2*time.Second))
	if err != nil {
		t.Fatalf("World.Tick() error = %v", err)
	}
	if len(tick.MonsterHits) != 0 {
		t.Fatalf("MonsterHits = %d, want 0 after target defense blocks delayed spell", len(tick.MonsterHits))
	}
	for _, ch := range tick.Characters {
		if ch.ID == caster.ID && ch.TargetID != "" {
			t.Fatalf("caster TargetID = %q, want empty when RM_DELAYMAGIC defense check fails", ch.TargetID)
		}
	}
}

func TestDelayedMonsterSpellAppliesReferenceAnimalPowerScale(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	w.mu.Lock()
	w.monsters = map[string]*Monster{}
	w.occupied = map[monsterPosition]string{}
	w.mu.Unlock()
	caster.Skills = storage.SkillStates{{ID: "火球术", Level: 0, Train: 0}}
	caster.MP = 100
	targetX, targetY := caster.X+1, caster.Y
	spawned, err := w.SpawnMonsterByNameAt(caster.MapID, targetX, targetY, "鸡", 1)
	if err != nil || len(spawned.Monsters) != 1 {
		t.Fatalf("SpawnMonsterByNameAt() = %+v, %v", spawned.Monsters, err)
	}
	mon := spawned.Monsters[0]
	w.mu.Lock()
	w.monsters[mon.ID].MagicDefense = 0
	w.monsters[mon.ID].MagicDefenseMax = 0
	w.mu.Unlock()
	if mon.Race != 51 || mon.Animal {
		t.Fatalf("test target classification = race:%d animal:%t, want race 51 and animal false", mon.Race, mon.Animal)
	}
	cast, err := w.DoSpell(caster, "火球术", targetX, targetY, MonsterActorID(mon), nil)
	if err != nil {
		t.Fatalf("DoSpell() error = %v", err)
	}
	w.mu.Lock()
	baseDamage := -1
	for _, pending := range w.pendingSpells {
		if pending.TargetMonsterID == mon.ID {
			baseDamage = pending.Damage
			break
		}
	}
	w.mu.Unlock()
	if baseDamage < 0 {
		t.Fatal("missing delayed monster spell after cast")
	}
	tick, err := w.Tick([]PlayerSnapshot{{Character: cast.Character}}, time.Now().Add(2*time.Second))
	if err != nil {
		t.Fatalf("World.Tick() error = %v", err)
	}
	if len(tick.MonsterHits) != 1 {
		t.Fatalf("MonsterHits = %d, want 1", len(tick.MonsterHits))
	}
	wantDamage := referenceRound(float64(baseDamage) / 1.2)
	if tick.MonsterHits[0].Damage != wantDamage {
		t.Fatalf("delayed animal damage = %d, want %d from base %d", tick.MonsterHits[0].Damage, wantDamage, baseDamage)
	}
}

func TestDelayedMonsterSpellAppliesMagicDefenseOnce(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	w.mu.Lock()
	w.monsters = map[string]*Monster{}
	w.occupied = map[monsterPosition]string{}
	w.mu.Unlock()
	spawned, err := w.SpawnMonsterByNameAt(caster.MapID, caster.X+1, caster.Y, "鸡", 1)
	if err != nil || len(spawned.Monsters) != 1 {
		t.Fatalf("SpawnMonsterByNameAt() = %+v, %v", spawned.Monsters, err)
	}
	mon := spawned.Monsters[0]
	w.mu.Lock()
	w.monsters[mon.ID].MagicDefense = 3
	w.monsters[mon.ID].MagicDefenseMax = 3
	w.pendingSpells = []pendingSpell{{
		DueAt: time.Now().Add(-time.Second), CasterID: caster.ID, TargetMonsterID: mon.ID,
		TargetX: mon.X, TargetY: mon.Y, Damage: 10,
	}}
	result := TickResult{}
	players := map[string]storage.Character{caster.ID: caster}
	updated := map[string]storage.Character{}
	w.applyPendingSpellTicksLocked(&result, players, updated, time.Now())
	w.mu.Unlock()
	if len(result.MonsterHits) != 1 || result.MonsterHits[0].Damage != 5 {
		t.Fatalf("delayed monster hit = %+v, want one hit for 5 after reference scaling and one defense application", result.MonsterHits)
	}
}

func TestDelayedMonsterSpellDoesNotStunWhenDefensePrecheckFails(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	w.mu.Lock()
	w.monsters = map[string]*Monster{}
	w.occupied = map[monsterPosition]string{}
	w.mu.Unlock()
	targetX, targetY := caster.X+1, caster.Y
	spawned, err := w.SpawnMonsterByNameAt(caster.MapID, targetX, targetY, "鸡", 1)
	if err != nil || len(spawned.Monsters) != 1 {
		t.Fatalf("SpawnMonsterByNameAt() = %+v, %v", spawned.Monsters, err)
	}
	monID := spawned.Monsters[0].ID
	w.mu.Lock()
	mon := w.monsters[monID]
	mon.MagicDefense = 100000
	mon.MagicDefenseMax = 100000
	mon.LastWalkAt = time.Now().Add(time.Hour)
	wantLastWalkAt := mon.LastWalkAt
	w.mu.Unlock()
	now := time.Now()
	w.mu.Lock()
	w.pendingSpells = []pendingSpell{{
		DueAt: now.Add(-time.Second), CasterID: caster.ID, TargetMonsterID: monID,
		TargetX: targetX, TargetY: targetY, Damage: 1,
	}}
	w.mu.Unlock()
	w.mu.Lock()
	result := TickResult{}
	players := map[string]storage.Character{caster.ID: caster}
	updated := map[string]storage.Character{}
	w.applyPendingSpellTicksLocked(&result, players, updated, now)
	lastWalkAt := w.monsters[monID].LastWalkAt
	w.mu.Unlock()
	if !lastWalkAt.Equal(wantLastWalkAt) {
		t.Fatalf("LastWalkAt = %v, want unchanged %v when RM_DELAYMAGIC precheck fails", lastWalkAt, wantLastWalkAt)
	}
}

func TestDelayedCharacterSpellIsDiscardedWhenTargetIsGone(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	now := time.Now()
	w.mu.Lock()
	w.pendingSpells = []pendingSpell{{
		DueAt: now.Add(-time.Second), CasterID: caster.ID, TargetCharacterID: "gone",
		CharacterDamage: true, TargetX: caster.X + 1, TargetY: caster.Y, Damage: 10,
	}}
	result := TickResult{}
	players := map[string]storage.Character{caster.ID: caster}
	updated := map[string]storage.Character{}
	err := w.applyPendingSpellTicksLocked(&result, players, updated, now)
	w.mu.Unlock()
	if err != nil {
		t.Fatalf("applyPendingSpellTicksLocked() error = %v", err)
	}
	if len(w.pendingSpells) != 0 {
		t.Fatalf("pending spells = %d, want discarded missing-target entry", len(w.pendingSpells))
	}
	if len(result.CharacterHits) != 0 || len(result.CharacterDeaths) != 0 {
		t.Fatalf("result = %+v, want no hit or death for missing target", result)
	}
}

func TestDelayedMonsterSpellAppliesMagicStruckWalkDelayOnce(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	w.mu.Lock()
	w.monsters = map[string]*Monster{}
	w.occupied = map[monsterPosition]string{}
	w.rand = rand.New(zeroSource{})
	w.mu.Unlock()
	targetX, targetY := caster.X+1, caster.Y
	spawned, err := w.SpawnMonsterByNameAt(caster.MapID, targetX, targetY, "鸡", 1)
	if err != nil || len(spawned.Monsters) != 1 {
		t.Fatalf("SpawnMonsterByNameAt() = %+v, %v", spawned.Monsters, err)
	}
	monID := spawned.Monsters[0].ID
	now := time.Now()
	w.mu.Lock()
	w.monsters[monID].LastWalkAt = now.Add(time.Second)
	w.pendingSpells = []pendingSpell{{
		DueAt: now.Add(-time.Second), CasterID: caster.ID, TargetMonsterID: monID,
		TargetX: targetX, TargetY: targetY, Damage: 10,
	}}
	result := TickResult{}
	players := map[string]storage.Character{caster.ID: caster}
	updated := map[string]storage.Character{}
	w.applyPendingSpellTicksLocked(&result, players, updated, now)
	lastWalkAt := w.monsters[monID].LastWalkAt
	w.mu.Unlock()
	if len(result.MonsterHits) != 1 {
		t.Fatalf("MonsterHits = %d, want one delayed magic hit", len(result.MonsterHits))
	}
	if !lastWalkAt.Equal(now.Add(1800 * time.Millisecond)) {
		t.Fatalf("LastWalkAt = %v, want one 800ms magic-struck delay from %v", lastWalkAt, now.Add(time.Second))
	}
}

func TestDelayedSingleMagicStrikeSkipsMovedTarget(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	target, err := w.CreateCharacterWithAppearance("moved-target", "moved-target", "warrior", 0, 0, caster.MapID, caster.X+1, caster.Y)
	if err != nil {
		t.Fatalf("CreateCharacterWithAppearance() error = %v", err)
	}
	target.HP = 100
	target.MaxHP = 100
	now := time.Now()
	w.mu.Lock()
	w.pendingSpells = []pendingSpell{{
		DueAt: now.Add(-time.Second), CasterID: caster.ID, TargetCharacterID: target.ID,
		CharacterDamage: true, TargetX: target.X, TargetY: target.Y, Damage: 10,
	}}
	moved := target
	moved.X += 3
	players := map[string]storage.Character{caster.ID: caster, target.ID: moved}
	result := TickResult{}
	updated := map[string]storage.Character{}
	w.applyPendingSpellTicksLocked(&result, players, updated, now)
	w.mu.Unlock()
	if len(result.CharacterHits) != 0 {
		t.Fatalf("CharacterHits = %d, want no hit after target moved out of delayed range", len(result.CharacterHits))
	}
	if got := players[target.ID].HP; got != target.HP {
		t.Fatalf("moved target HP = %d, want unchanged %d", got, target.HP)
	}
}

func TestDelayedSpellEventsPreservePendingOrder(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	caster.FreePKArea = true
	target, err := w.CreateCharacterWithAppearance("ordered-target", "ordered-target", "warrior", 0, 0, caster.MapID, caster.X+1, caster.Y)
	if err != nil {
		t.Fatalf("CreateCharacterWithAppearance() error = %v", err)
	}
	target.HP = 100
	target.MaxHP = 100
	now := time.Now()
	w.mu.Lock()
	w.pendingSpells = []pendingSpell{
		{DueAt: now.Add(-time.Second), CasterID: caster.ID, TargetCharacterID: target.ID, PoisonHealth: true, PoisonHealthLevel: 1, PoisonDuration: time.Minute, PoisonNotification: true, PoisonPoint: 1},
		{DueAt: now.Add(-time.Second), CasterID: caster.ID, TargetCharacterID: target.ID, CharacterDamage: true, TargetX: target.X, TargetY: target.Y, Damage: 10},
	}
	result := TickResult{}
	players := map[string]storage.Character{caster.ID: caster, target.ID: target}
	updated := map[string]storage.Character{}
	w.applyPendingSpellTicksLocked(&result, players, updated, now)
	w.mu.Unlock()

	if len(result.OrderedSpellEvents) != 3 {
		t.Fatalf("ordered spell events = %d, want status, poison notification, and hit", len(result.OrderedSpellEvents))
	}
	if updatedCaster := updated[caster.ID]; updatedCaster.TargetID != target.ID {
		t.Fatalf("poison caster TargetID = %q, want %q", updatedCaster.TargetID, target.ID)
	}
	if updatedTarget := updated[target.ID]; !updatedTarget.PKFlag || updatedTarget.PKFlagUntil <= now.UnixNano() {
		t.Fatalf("poison target PK state = %+v, want active marker", updatedTarget)
	}
	if len(result.NameColorCharacters) != 2 || result.NameColorCharacters[0].ID != target.ID || !result.NameColorCharacters[0].PKFlag || result.NameColorCharacters[1].ID != caster.ID || !result.NameColorCharacters[1].PKFlag {
		t.Fatalf("spell name color characters = %+v, want target then caster markers", result.NameColorCharacters)
	}
	wantKinds := []OrderedSpellEventKind{OrderedSpellEventCharacterStatus, OrderedSpellEventPoisonNotification, OrderedSpellEventCharacterHit}
	for i, want := range wantKinds {
		if result.OrderedSpellEvents[i].Kind != want {
			t.Fatalf("ordered spell event %d = %d, want %d", i, result.OrderedSpellEvents[i].Kind, want)
		}
	}
}

func TestDelayedMonsterSpellPrecheckUsesCasterUndeadBonus(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	w.mu.Lock()
	w.monsters = map[string]*Monster{}
	w.occupied = map[monsterPosition]string{}
	w.rand = rand.New(zeroSource{})
	w.mu.Unlock()
	caster.Skills = storage.SkillStates{{ID: "火球术", Level: 0, Train: 0}}
	caster.MP = 100
	w.data.Items["test-undead-bonus"] = data.StdItem{ID: "test-undead-bonus", Undead: 1000}
	caster.EquippedItems = map[int]storage.UserItem{0: {ItemID: "test-undead-bonus"}}
	targetX, targetY := caster.X+1, caster.Y
	spawned, err := w.SpawnMonsterByNameAt(caster.MapID, targetX, targetY, "鸡", 1)
	if err != nil || len(spawned.Monsters) != 1 {
		t.Fatalf("SpawnMonsterByNameAt() = %+v, %v", spawned.Monsters, err)
	}
	monID := spawned.Monsters[0].ID
	w.mu.Lock()
	baseDamage := w.spellMonsterDamageLocked(caster, w.data.Skills["火球术"], caster.Skills[0])
	mon := w.monsters[monID]
	mon.Undead = 1
	mon.MagicDefense = baseDamage
	mon.MagicDefenseMax = baseDamage
	w.mu.Unlock()
	cast, err := w.DoSpell(caster, "火球术", targetX, targetY, MonsterActorID(spawned.Monsters[0]), nil)
	if err != nil {
		t.Fatalf("DoSpell() error = %v", err)
	}
	tick, err := w.Tick([]PlayerSnapshot{{Character: cast.Character}}, time.Now().Add(2*time.Second))
	if err != nil {
		t.Fatalf("World.Tick() error = %v", err)
	}
	if len(tick.MonsterHits) != 0 {
		t.Fatalf("MonsterHits = %d, want no damage when target defense consumes base power", len(tick.MonsterHits))
	}
	for _, updated := range tick.Characters {
		if updated.ID == caster.ID && updated.TargetID != monID {
			t.Fatalf("caster TargetID = %q, want %q after reference precheck", updated.TargetID, monID)
		}
	}
}

func TestDelayedCharacterSpellCommitsPrecheckProtectionStateWhenFinalDamageIsZero(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	caster.FreePKArea = true
	now := time.Now()
	target := storage.Character{
		ID: "target", MapID: caster.MapID, X: caster.X + 1, Y: caster.Y,
		HP: 100, MaxHP: 100, FreePKArea: true, BubbleDefenceLevel: 0,
		BubbleDefenceUntil: now.Add(10 * time.Second).UnixNano(),
		EquippedItems:      map[int]storage.UserItem{SlotWeapon: {ItemID: "delayed-magic-defense"}},
	}
	players := map[string]storage.Character{caster.ID: caster, target.ID: target}
	w.mu.Lock()
	w.data.Items["delayed-magic-defense"] = data.StdItem{
		ID:    "delayed-magic-defense",
		Stats: data.StdItemStats{MacMin: 5 | 5<<8},
	}
	w.pendingSpells = []pendingSpell{{
		DueAt: now.Add(-time.Second), CasterID: caster.ID, TargetCharacterID: target.ID,
		CharacterDamage: true, TargetX: target.X, TargetY: target.Y, Damage: 20,
	}}
	result := TickResult{}
	updated := map[string]storage.Character{}
	w.applyPendingSpellTicksLocked(&result, players, updated, now)
	w.mu.Unlock()

	updatedTarget, ok := updated[target.ID]
	if !ok {
		t.Fatalf("updated target missing, want precheck protection state committed")
	}
	if len(result.CharacterHits) != 1 {
		t.Fatalf("CharacterHits = %d, want 1 after final RM_MAGSTRUCK reuses original power", len(result.CharacterHits))
	}
	if result.CharacterHits[0].Damage != 2 {
		t.Fatalf("CharacterHits[0].Damage = %d, want 2 after two magic defense checks", result.CharacterHits[0].Damage)
	}
	updatedCaster, ok := updated[caster.ID]
	if !ok || !updatedCaster.PKFlag || updatedCaster.PKFlagUntil <= now.UnixNano() {
		t.Fatalf("updated caster PK state = %+v, want active marker", updatedCaster)
	}
	if len(result.NameColorCharacters) != 1 || result.NameColorCharacters[0].ID != caster.ID || !result.NameColorCharacters[0].PKFlag {
		t.Fatalf("NameColorCharacters = %+v, want active caster marker", result.NameColorCharacters)
	}
	if updatedTarget.BubbleDefenceUntil >= target.BubbleDefenceUntil {
		t.Fatalf("BubbleDefenceUntil = %d, want reduced below %d after precheck", updatedTarget.BubbleDefenceUntil, target.BubbleDefenceUntil)
	}
}

func TestImmediateCharacterSpellPersistsCasterPKFlag(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	caster.FreePKArea = true
	target := storage.Character{ID: "target", MapID: caster.MapID, X: caster.X + 1, Y: caster.Y, HP: 100, MaxHP: 100, FreePKArea: true}
	w.mu.Lock()
	_, hit, err := w.spellCharacterDamageWithPowerLocked(caster, target, 20)
	w.mu.Unlock()
	if err != nil {
		t.Fatalf("spellCharacterDamageWithPowerLocked() error = %v", err)
	}
	if hit.Damage <= 0 {
		t.Fatalf("hit damage = %d, want positive damage", hit.Damage)
	}
	updated, ok := w.store.Character(caster.ID)
	if !ok || !updated.PKFlag || updated.PKFlagUntil <= time.Now().UnixNano() {
		t.Fatalf("stored caster PK state = %+v, want active marker", updated)
	}
	updatedTarget, ok := w.store.Character(target.ID)
	if !ok || updatedTarget.LastHitterID != caster.ID || updatedTarget.LastHitterAt == 0 {
		t.Fatalf("stored target last hitter = %+v, want attacker %q", updatedTarget, caster.ID)
	}
}

func TestImmediateCharacterSpellMarksCasterOnKillingHit(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	caster.FreePKArea = true
	target := storage.Character{ID: "target", MapID: caster.MapID, X: caster.X + 1, Y: caster.Y, HP: 1, MaxHP: 1, FreePKArea: true}
	w.mu.Lock()
	w.rand = rand.New(zeroSource{})
	_, hit, err := w.spellCharacterDamageWithPowerLocked(caster, target, 20)
	w.mu.Unlock()
	if err != nil {
		t.Fatalf("spellCharacterDamageWithPowerLocked() error = %v", err)
	}
	if hit.Damage <= 0 || !hit.Dead {
		t.Fatalf("hit = %+v, want positive killing hit", hit)
	}
	updated, ok := w.store.Character(caster.ID)
	if !ok || !updated.PKFlag {
		t.Fatalf("stored caster PK state = %+v, want active marker after killing hit", updated)
	}
}

func TestDelayedMabeCharacterSpellCarriesPrecheckedBubbleState(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	now := time.Now()
	before := now.Add(10 * time.Second).UnixNano()
	after := now.Add(7 * time.Second).UnixNano()
	target := storage.Character{ID: "target", MapID: caster.MapID, X: caster.X + 1, Y: caster.Y, HP: 100, MaxHP: 100, BubbleDefenceUntil: before, BubbleDefenceLevel: 1}
	players := map[string]storage.Character{caster.ID: caster, target.ID: target}
	w.mu.Lock()
	w.pendingSpells = []pendingSpell{{
		DueAt: now.Add(-time.Second), CasterID: caster.ID, TargetCharacterID: target.ID,
		CharacterDamage: true, TargetX: target.X, TargetY: target.Y, Damage: 0,
		CharacterBubbleBefore: before, CharacterBubbleAfter: after, CharacterBubbleLevel: 3,
	}}
	result := TickResult{}
	updated := map[string]storage.Character{}
	w.applyPendingSpellTicksLocked(&result, players, updated, now)
	w.mu.Unlock()
	updatedTarget, ok := updated[target.ID]
	if !ok {
		t.Fatalf("updated target missing, want prechecked bubble state")
	}
	if updatedTarget.BubbleDefenceUntil != after || updatedTarget.BubbleDefenceLevel != 3 {
		t.Fatalf("bubble state = (%d, %d), want (%d, %d)", updatedTarget.BubbleDefenceUntil, updatedTarget.BubbleDefenceLevel, after, 3)
	}
}

func TestSpellEventsSkipHealthSyncWithoutManaCost(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	skill, ok := w.data.Skills["火球术"]
	if !ok {
		t.Fatal("skill 火球术 missing from config")
	}
	events := w.spellEvents(ch, SkillCastResult{SkillID: "火球术", Character: ch}, skill, ch.X, ch.Y, 0)
	if len(events) < 2 || events[0].Kind != SpellEventCasterState || events[1].Kind != SpellEventStart || events[0].SendHealth {
		t.Fatalf("zero-cost spell events = %+v, want caster state before spell start without health sync", events)
	}
}

func TestDoSpellWithoutTargetPreservesReferenceSpellEvents(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	w.mu.Lock()
	w.monsters = map[string]*Monster{}
	w.occupied = map[monsterPosition]string{}
	w.mu.Unlock()
	caster.Skills = storage.SkillStates{{ID: "火球术", Level: 0, Train: 0}}
	skill, ok := w.Skill("火球术")
	if !ok {
		t.Fatal("skill 火球术 missing from config")
	}
	cost := w.SpellCost(skill, caster.Skills[0])
	caster.MP = cost + 10
	result, err := w.DoSpell(caster, "火球术", caster.X+1, caster.Y, 0, nil)
	if err != nil {
		t.Fatalf("DoSpell() error = %v, want no-target cast to complete", err)
	}
	if !result.SpellStarted || result.ManaCost != cost || result.Character.MP != caster.MP-cost {
		t.Fatalf("no-target result = %+v, want started cast with deducted MP", result)
	}
	if len(result.Events) != 4 || result.Events[0].Kind != SpellEventCasterState || result.Events[1].Kind != SpellEventStart || result.Events[2].Kind != SpellEventCharacter || result.Events[3].Kind != SpellEventMagicFire {
		t.Fatalf("no-target events = %+v, want caster state, spell start, character, then magic fire", result.Events)
	}
	if !result.Events[0].SendHealth || result.Events[1].SendHealth {
		t.Fatalf("health update events = %+v, want only caster state health sync", result.Events)
	}
}

func TestDoSpellWithoutTargetDoesNotSelectObjectByCoordinates(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	w.mu.Lock()
	w.monsters = map[string]*Monster{}
	w.occupied = map[monsterPosition]string{}
	w.mu.Unlock()
	targetX, targetY := caster.X+1, caster.Y
	spawned, err := w.SpawnMonsterByNameAt(caster.MapID, targetX, targetY, "鸡", 1)
	if err != nil || len(spawned.Monsters) != 1 {
		t.Fatalf("SpawnMonsterByNameAt() = %+v, %v", spawned.Monsters, err)
	}
	caster.Skills = storage.SkillStates{{ID: "火球术", Level: 0, Train: 0}}
	caster.MP = 100
	result, err := w.DoSpell(caster, "火球术", targetX, targetY, 0, nil)
	if err != nil {
		t.Fatalf("DoSpell() error = %v", err)
	}
	if len(w.pendingSpells) != 0 || result.MagicTargetID != 0 {
		t.Fatalf("no-target result = %+v, pending=%d; want no selected object", result, len(w.pendingSpells))
	}
}

func TestDoSpellClearsUnresolvedTargetActorFromMagicFire(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	caster.Skills = storage.SkillStates{{ID: "火球术", Level: 0, Train: 0}}
	caster.MP = 100
	result, err := w.DoSpell(caster, "火球术", caster.X+1, caster.Y, MonsterActorID(Monster{ID: "missing"}), nil)
	if err != nil {
		t.Fatalf("DoSpell() error = %v", err)
	}
	for _, event := range result.Events {
		if event.Kind == SpellEventStart || event.Kind == SpellEventMagicFire {
			if event.TargetID != 0 {
				t.Fatalf("event %v target ID = %d, want 0 for unresolved actor", event.Kind, event.TargetID)
			}
		}
	}
}

func TestCastSkillResolvesMovedTargetWithinReferenceNearRange(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	target, err := w.CreateCharacterWithAppearance("test2", "target", "warrior", 0, 0, caster.MapID, caster.X+1, caster.Y)
	if err != nil {
		t.Fatalf("CreateCharacterWithAppearance() error = %v", err)
	}
	caster.Skills = storage.SkillStates{{ID: "火球术", Level: 0, Train: 0}}
	caster.MP = 100
	requestedX, requestedY := target.X, target.Y
	target.X++
	players := []storage.Character{caster, target}
	result, err := w.DoSpell(caster, "火球术", requestedX, requestedY, CharacterActorID(target), players)
	if err != nil {
		t.Fatalf("DoSpell() error = %v", err)
	}
	if len(w.pendingSpells) != 1 || w.pendingSpells[0].TargetCharacterID != target.ID {
		t.Fatalf("pending spells = %+v, want moved explicit target %q", w.pendingSpells, target.ID)
	}
	if w.pendingSpells[0].TargetX != target.X || w.pendingSpells[0].TargetY != target.Y {
		t.Fatalf("pending target coordinates = (%d,%d), want current target coordinates (%d,%d)", w.pendingSpells[0].TargetX, w.pendingSpells[0].TargetY, target.X, target.Y)
	}
	if !result.TargetIDResolved || result.MagicTargetID != CharacterActorID(target) {
		t.Fatalf("target resolution = %+v, want explicit moved target", result)
	}
	if result.TargetX != target.X || result.TargetY != target.Y {
		t.Fatalf("result target coordinates = (%d,%d), want current target coordinates (%d,%d)", result.TargetX, result.TargetY, target.X, target.Y)
	}
	for _, event := range result.Events {
		if event.Kind == SpellEventStart || event.Kind == SpellEventMagicFire {
			if event.TargetX != target.X || event.TargetY != target.Y {
				t.Fatalf("event %v target coordinates = (%d,%d), want current target coordinates (%d,%d)", event.Kind, event.TargetX, event.TargetY, target.X, target.Y)
			}
		}
	}
}

func TestCastSkillDoesNotRetargetAfterExplicitTargetLeavesReferenceNearRange(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	target, err := w.CreateCharacterWithAppearance("test2", "target", "warrior", 0, 0, caster.MapID, caster.X+1, caster.Y)
	if err != nil {
		t.Fatalf("CreateCharacterWithAppearance() error = %v", err)
	}
	caster.Skills = storage.SkillStates{{ID: "火球术", Level: 0, Train: 0}}
	caster.MP = 100
	requestedX, requestedY := target.X, target.Y
	target.X += 2
	players := []storage.Character{caster, target}
	result, err := w.DoSpell(caster, "火球术", requestedX, requestedY, CharacterActorID(target), players)
	if err != nil {
		t.Fatalf("DoSpell() error = %v", err)
	}
	if result.TargetIDResolved || result.MagicTargetID != 0 || len(w.pendingSpells) != 0 {
		t.Fatalf("invalid explicit target result = %+v, pending=%d; want no retarget", result, len(w.pendingSpells))
	}
}

func TestCastSkillRejectsTargetsOutOfRange(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	caster.Skills = storage.SkillStates{{ID: "火球术", Level: 0, Train: 0}}
	caster.MP = 100
	skill, ok := w.Skill("火球术")
	if !ok {
		t.Fatal("skill 火球术 missing from config")
	}
	cost := w.SpellCost(skill, caster.Skills[0])
	result, err := w.CastSkillWithPlayers(caster, "火球术", caster.X+9, caster.Y, 0, nil)
	if err == nil {
		t.Fatal("CastSkillWithPlayers() expected range rejection")
	}
	if result.SpellStarted || result.ManaCost != cost || result.Character.ID == "" || result.Character.MP != caster.MP-cost || !result.ManaConsumed || len(result.Events) != 0 {
		t.Fatalf("range failure result = %+v, want consumed resource and no start event", result)
	}
	persisted, ok := w.store.Character(caster.ID)
	if !ok || persisted.MP != caster.MP-cost {
		t.Fatalf("persisted caster MP = %d (found=%t), want %d", persisted.MP, ok, caster.MP-cost)
	}
}

func TestDoSpellMissingAmuletPreservesSpellStart(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	target := caster
	target.ID = "target"
	target.X++
	caster.Skills = storage.SkillStates{{ID: "隐身术", Level: 0, Train: 0}}
	skill, ok := w.Skill("隐身术")
	if !ok {
		t.Fatal("skill 隐身术 missing from config")
	}
	caster.MP = w.SpellCost(skill, caster.Skills[0]) + 10
	result, err := w.DoSpell(caster, "隐身术", target.X, target.Y, CharacterActorID(target), []storage.Character{target})
	if err == nil {
		t.Fatal("DoSpell() error = nil, want missing amulet failure")
	}
	if !result.SpellStarted || len(result.Events) != 2 || result.Events[0].Kind != SpellEventCasterState || result.Events[1].Kind != SpellEventStart {
		t.Fatalf("missing amulet result = %+v, want caster state then spell start events", result)
	}
	if result.ManaCost != w.SpellCost(skill, caster.Skills[0]) || result.Character.MP != caster.MP-result.ManaCost {
		t.Fatalf("missing amulet mana result = %+v, want deducted MP", result)
	}
	persisted, ok := w.store.Character(caster.ID)
	if !ok || persisted.MP != caster.MP-result.ManaCost {
		t.Fatalf("persisted missing-amulet MP = %d (found=%t), want %d", persisted.MP, ok, caster.MP-result.ManaCost)
	}
	if result.Events[1].TargetID != CharacterActorID(target) {
		t.Fatalf("missing amulet spell start target = %d, want %d", result.Events[1].TargetID, CharacterActorID(target))
	}
}

func TestDoSpellSummonWithoutAmuletPreservesSpellStart(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	caster.Skills = storage.SkillStates{{ID: "召唤骷髅", Level: 0}}
	skill, ok := w.Skill("召唤骷髅")
	if !ok {
		t.Fatal("skill 召唤骷髅 missing from config")
	}
	caster.MP = w.SpellCost(skill, caster.Skills[0]) + 10
	result, err := w.DoSpell(caster, "召唤骷髅", caster.X, caster.Y, 0, nil)
	if err == nil || !result.SpellStarted || !result.SpellFailed {
		t.Fatalf("summon result = %+v, error = %v, want started failed spell", result, err)
	}
	if len(result.SummonedMonsters) != 0 {
		t.Fatalf("summoned monsters = %d, want 0", len(result.SummonedMonsters))
	}
}

func TestSpellEventsPlaceSummonBeforeMagicFire(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	skill, ok := w.Skill("召唤骷髅")
	if !ok {
		t.Fatal("skill 召唤骷髅 missing from config")
	}
	mon := Monster{ID: "summoned", MapID: caster.MapID, X: caster.X + 1, Y: caster.Y}
	events := w.spellEvents(caster, SkillCastResult{
		SkillID:          "召唤骷髅",
		Character:        caster,
		SummonedMonsters: []Monster{mon},
	}, skill, caster.X, caster.Y, 0)
	magicFireIndex, summonIndex := -1, -1
	for i, event := range events {
		if event.Kind == SpellEventMagicFire && magicFireIndex == -1 {
			magicFireIndex = i
		}
		if event.Kind == SpellEventSummon && summonIndex == -1 {
			summonIndex = i
		}
	}
	if magicFireIndex == -1 || summonIndex == -1 || summonIndex >= magicFireIndex {
		t.Fatalf("spell events = %+v, want summon visibility before magic fire", events)
	}
}

func TestSpellFailureEventsPreserveConsumedAmuletDeletion(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	caster.EquippedItems = map[int]storage.UserItem{
		SlotBujuk: {ItemID: "护身符", MakeIndex: 9, Dura: 100, DuraMax: 100},
	}
	updated := caster
	updated.EquippedItems = map[int]storage.UserItem{}
	events := w.spellFailureEvents(caster, SkillCastResult{SkillID: "召唤骷髅", Character: updated, ManaConsumed: true}, data.StdSkill{}, caster.X, caster.Y, 0)
	if len(events) != 3 || events[0].Kind != SpellEventCasterState || events[1].Kind != SpellEventStart || events[2].Kind != SpellEventItemDelete {
		t.Fatalf("failure events = %+v, want state, start, item delete", events)
	}
	if events[2].DeletedItem.ItemID != "护身符" || events[2].DeletedItem.MakeIndex != 9 {
		t.Fatalf("deleted item = %+v, want consumed amulet", events[2].DeletedItem)
	}
}

func TestSpellEventsSendStartBeforeAmuletDurability(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	caster.EquippedItems = map[int]storage.UserItem{
		SlotBujuk: {ItemID: "护身符", MakeIndex: 9, Dura: 100, DuraMax: 200},
	}
	updated := caster
	updated.EquippedItems = map[int]storage.UserItem{
		SlotBujuk: {ItemID: "护身符", MakeIndex: 9, Dura: 0, DuraMax: 200},
	}
	events := w.spellEvents(caster, SkillCastResult{SkillID: "召唤骷髅", Character: updated}, data.StdSkill{}, caster.X, caster.Y, 0)
	stateIndex, durabilityIndex, startIndex := -1, -1, -1
	for i, event := range events {
		switch event.Kind {
		case SpellEventCasterState:
			stateIndex = i
		case SpellEventDurability:
			durabilityIndex = i
		case SpellEventStart:
			startIndex = i
		}
	}
	if stateIndex != 0 || startIndex <= stateIndex || durabilityIndex <= startIndex {
		t.Fatalf("success events = %+v, want state, start, durability", events)
	}
}

func TestDoSpellAmuletExhaustionEmitsItemDeleteEvent(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	target := caster
	target.ID = "target"
	target.X++
	caster.Skills = storage.SkillStates{{ID: "隐身术", Level: 0, Train: 0}}
	caster.MP = 100
	caster.EquippedItems = map[int]storage.UserItem{
		SlotBujuk: {ItemID: "护身符", MakeIndex: 42, Dura: 100, DuraMax: 100},
	}
	result, err := w.DoSpell(caster, "隐身术", target.X, target.Y, CharacterActorID(target), []storage.Character{target})
	if err != nil {
		t.Fatalf("DoSpell() error = %v, want successful spell with consumed amulet", err)
	}
	if !result.SpellStarted {
		t.Fatalf("SpellStarted = false, want true: %+v", result)
	}
	for _, event := range result.Events {
		if event.Kind == SpellEventItemDelete {
			if event.DeletedItem.ItemID != "护身符" || event.DeletedItem.MakeIndex != 42 {
				t.Fatalf("deleted item = %+v, want amulet make index 42", event.DeletedItem)
			}
			return
		}
	}
	t.Fatalf("events = %+v, want item delete event", result.Events)
}

func TestDoSpellAmuletUseEmitsDurabilityEvent(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	target := caster
	target.ID = "target"
	target.X++
	caster.Skills = storage.SkillStates{{ID: "隐身术", Level: 0, Train: 0}}
	caster.MP = 100
	caster.EquippedItems = map[int]storage.UserItem{
		SlotBujuk: {ItemID: "护身符", MakeIndex: 43, Dura: 200, DuraMax: 200},
	}
	result, err := w.DoSpell(caster, "隐身术", target.X, target.Y, CharacterActorID(target), []storage.Character{target})
	if err != nil {
		t.Fatalf("DoSpell() error = %v, want successful spell with consumed amulet", err)
	}
	for _, event := range result.Events {
		if event.Kind == SpellEventDurability {
			if event.Durability.Slot != SlotBujuk || event.Durability.Dura != 100 || event.Durability.DuraMax != 200 {
				t.Fatalf("durability event = %+v, want slot %d, dura 100/200", event.Durability, SlotBujuk)
			}
			return
		}
	}
	t.Fatalf("events = %+v, want durability event", result.Events)
}

func TestLongHitConnectsTwoTilesAhead(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	w.mu.Lock()
	w.monsters = map[string]*Monster{}
	w.occupied = map[monsterPosition]string{}
	w.rand = rand.New(rand.NewSource(1))
	w.mu.Unlock()
	mapID := ch.MapID
	targetX, targetY := ch.X+2, ch.Y
	if !w.data.Maps[mapID].Walkable(targetX, targetY) {
		t.Fatal("expected walkable tile for long hit test")
	}
	result, err := w.SpawnMonsterByNameAt(mapID, targetX, targetY, "鸡", 1)
	if err != nil {
		t.Fatalf("SpawnMonsterByNameAt() error = %v", err)
	}
	if len(result.Monsters) != 1 {
		t.Fatalf("SpawnMonsterByNameAt() monsters = %d, want 1", len(result.Monsters))
	}
	w.mu.Lock()
	w.monsters[result.Monsters[0].ID].Speed = 1
	w.mu.Unlock()
	hit, err := w.HitWithIdent(ch, ch.X, ch.Y, 2, mir176.CMLongHit)
	if err != nil {
		t.Fatalf("HitWithIdent() error = %v", err)
	}
	if hit.MonsterID == "" {
		t.Fatal("expected long hit to connect")
	}
}

func TestLongHitUsesReferenceMultiplier(t *testing.T) {
	baseW, baseCh := prepareHitDamageTestWorld(t, nil)
	baseW.mu.Lock()
	var baseMon *Monster
	for _, mon := range baseW.monsters {
		mon.Defense = 0
		mon.Speed = 1
		baseMon = mon
		break
	}
	baseDamage := baseW.characterAttackDamageLocked(baseCh, baseMon, mir176.CMHit)
	baseW.mu.Unlock()

	skillW, skillCh := newTestWorldCharacter(t)
	skillW.mu.Lock()
	skillW.monsters = map[string]*Monster{}
	skillW.occupied = map[monsterPosition]string{}
	skillW.rand = rand.New(rand.NewSource(1))
	skillW.mu.Unlock()
	targetX, targetY := skillCh.X+2, skillCh.Y
	if !skillW.data.Maps[skillCh.MapID].Walkable(targetX, targetY) {
		t.Fatal("expected walkable tile for long hit multiplier test")
	}
	result, err := skillW.SpawnMonsterByNameAt(skillCh.MapID, targetX, targetY, "鸡", 1)
	if err != nil {
		t.Fatalf("SpawnMonsterByNameAt() error = %v", err)
	}
	if len(result.Monsters) != 1 {
		t.Fatalf("SpawnMonsterByNameAt() monsters = %d, want 1", len(result.Monsters))
	}
	skillCh.Skills = storage.SkillStates{{ID: "刺杀剑术", Level: 0, Train: 0}}
	skillW.mu.Lock()
	var skillMon *Monster
	for _, mon := range skillW.monsters {
		mon.Defense = 0
		mon.Speed = 1
		skillMon = mon
		break
	}
	skillW.mu.Unlock()
	skillResult, err := skillW.HitWithIdent(skillCh, skillCh.X, skillCh.Y, 2, mir176.CMLongHit)
	if err != nil {
		t.Fatalf("skill HitWithIdent() error = %v", err)
	}
	_, ok := skillW.Skill("刺杀剑术")
	if !ok {
		t.Fatal("skill 刺杀剑术 missing from config")
	}
	want := int(math.Round(float64(baseDamage) / float64(spellTrainLevel+2) * float64(2)))
	if skillResult.Damage != want {
		t.Fatalf("long hit damage = %d, want %d", skillResult.Damage, want)
	}
	if skillResult.MonsterID != skillMon.ID {
		t.Fatalf("skill target = %q, want %q", skillResult.MonsterID, skillMon.ID)
	}
}

func TestWideHitConnectsInReferenceArc(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	w.mu.Lock()
	w.monsters = map[string]*Monster{}
	w.occupied = map[monsterPosition]string{}
	w.rand = rand.New(rand.NewSource(1))
	w.mu.Unlock()
	mapID := ch.MapID
	dir := 2
	relDirs := []int{7, 1, 2}
	targetX, targetY := -1, -1
	for _, rel := range relDirs {
		fd := (dir + rel) % 8
		off := dirOffsets[fd]
		tx, ty := ch.X+off[0], ch.Y+off[1]
		if !w.data.Maps[mapID].Walkable(tx, ty) {
			continue
		}
		targetX, targetY = tx, ty
		break
	}
	if targetX < 0 {
		t.Fatal("could not find walkable tile for wide hit test")
	}
	result, err := w.SpawnMonsterByNameAt(mapID, targetX, targetY, "鸡", 1)
	if err != nil {
		t.Fatalf("SpawnMonsterByNameAt() error = %v", err)
	}
	if len(result.Monsters) != 1 {
		t.Fatalf("SpawnMonsterByNameAt() monsters = %d, want 1", len(result.Monsters))
	}
	w.mu.Lock()
	w.monsters[result.Monsters[0].ID].Speed = 1
	w.mu.Unlock()
	hit, err := w.HitWithIdent(ch, ch.X, ch.Y, dir, mir176.CMWideHit)
	if err != nil {
		t.Fatalf("HitWithIdent() error = %v", err)
	}
	if hit.MonsterID == "" {
		t.Fatal("expected wide hit to connect")
	}
}

func TestWideHitUsesReferenceMultiplier(t *testing.T) {
	baseW, baseCh := prepareHitDamageTestWorld(t, nil)
	baseW.mu.Lock()
	var baseMon *Monster
	for _, mon := range baseW.monsters {
		mon.Defense = 0
		mon.Speed = 1
		baseMon = mon
		break
	}
	baseDamage := baseW.characterAttackDamageLocked(baseCh, baseMon, mir176.CMHit)
	baseW.mu.Unlock()

	skillW, skillCh := newTestWorldCharacter(t)
	skillW.mu.Lock()
	skillW.monsters = map[string]*Monster{}
	skillW.occupied = map[monsterPosition]string{}
	skillW.rand = rand.New(rand.NewSource(1))
	skillW.mu.Unlock()
	dir := 2
	relDirs := []int{7, 1, 2}
	targetX, targetY := -1, -1
	for _, rel := range relDirs {
		fd := (dir + rel) % 8
		off := dirOffsets[fd]
		tx, ty := skillCh.X+off[0], skillCh.Y+off[1]
		if !skillW.data.Maps[skillCh.MapID].Walkable(tx, ty) {
			continue
		}
		targetX, targetY = tx, ty
		break
	}
	if targetX < 0 {
		t.Fatal("could not find walkable tile for wide hit multiplier test")
	}
	result, err := skillW.SpawnMonsterByNameAt(skillCh.MapID, targetX, targetY, "鸡", 1)
	if err != nil {
		t.Fatalf("SpawnMonsterByNameAt() error = %v", err)
	}
	if len(result.Monsters) != 1 {
		t.Fatalf("SpawnMonsterByNameAt() monsters = %d, want 1", len(result.Monsters))
	}
	skillCh.Skills = storage.SkillStates{{ID: "半月弯刀", Level: 0, Train: 0}}
	skillW.mu.Lock()
	var skillMon *Monster
	for _, mon := range skillW.monsters {
		mon.Defense = 0
		mon.Speed = 1
		skillMon = mon
		break
	}
	skillW.mu.Unlock()
	skillResult, err := skillW.HitWithIdent(skillCh, skillCh.X, skillCh.Y, dir, mir176.CMWideHit)
	if err != nil {
		t.Fatalf("skill HitWithIdent() error = %v", err)
	}
	_, ok := skillW.Skill("半月弯刀")
	if !ok {
		t.Fatal("skill 半月弯刀 missing from config")
	}
	want := int(math.Round(float64(baseDamage) / float64(spellTrainLevel+10) * float64(2)))
	if skillResult.Damage != want {
		t.Fatalf("wide hit damage = %d, want %d", skillResult.Damage, want)
	}
	if skillResult.MonsterID != skillMon.ID {
		t.Fatalf("skill target = %q, want %q", skillResult.MonsterID, skillMon.ID)
	}
}

func TestWideHitPrefersFirstReferenceArcPoint(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	w.mu.Lock()
	w.monsters = map[string]*Monster{}
	w.occupied = map[monsterPosition]string{}
	w.rand = rand.New(rand.NewSource(1))
	w.mu.Unlock()
	mapID := ch.MapID
	dir := 2
	points := [][2]int{}
	for _, rel := range []int{7, 1, 2} {
		fd := (dir + rel) % 8
		off := dirOffsets[fd]
		points = append(points, [2]int{ch.X + off[0], ch.Y + off[1]})
	}
	tpl, ok := w.data.Monsters["鸡"]
	if !ok {
		t.Fatal("monster 鸡 missing from configs")
	}
	w.mu.Lock()
	for i, pt := range points {
		if !w.data.Maps[mapID].Walkable(pt[0], pt[1]) {
			w.mu.Unlock()
			t.Fatalf("point %d = (%d,%d) not walkable", i, pt[0], pt[1])
		}
		id := fmt.Sprintf("wide-%d", i)
		mon := newMonster(w, id, tpl, mapID, pt[0], pt[1], data.StdSpawn{MapID: mapID, MonsterID: tpl.ID, X: pt[0], Y: pt[1]})
		mon.Level = 1
		w.monsters[id] = mon
		w.occupyMonsterLocked(mon)
	}
	w.mu.Unlock()
	w.mu.Lock()
	for _, mon := range w.monsters {
		mon.Speed = 1
	}
	w.mu.Unlock()
	hit, err := w.HitWithIdent(ch, ch.X, ch.Y, dir, mir176.CMWideHit)
	if err != nil {
		t.Fatalf("HitWithIdent() error = %v", err)
	}
	if hit.MonsterID == "" {
		t.Fatal("expected wide hit to connect")
	}
	firstID := ""
	w.mu.Lock()
	if mon := w.monsters["wide-0"]; mon != nil {
		firstID = mon.ID
	}
	w.mu.Unlock()
	if firstID == "" {
		t.Fatal("could not find first arc monster")
	}
	if hit.MonsterID != firstID {
		t.Fatalf("wide hit monster = %q, want first arc point %q", hit.MonsterID, firstID)
	}
	if len(hit.MonsterHits) != 3 {
		t.Fatalf("wide arc MonsterHits = %d, want 3", len(hit.MonsterHits))
	}
	for _, arcHit := range hit.MonsterHits[1:] {
		if arcHit.Damage != hit.MonsterHits[0].Damage {
			t.Fatalf("wide arc damages = %+v, want one shared secondary power", hit.MonsterHits)
		}
	}
}

func TestWideHitProcessesArcBeforePrimaryTarget(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	w.mu.Lock()
	w.monsters = map[string]*Monster{}
	w.occupied = map[monsterPosition]string{}
	w.rand = rand.New(rand.NewSource(1))
	w.mu.Unlock()
	mapID := ch.MapID
	dir := 2
	arcOff := dirOffsets[(dir+7)%8]
	frontOff := dirOffsets[dir]
	arcX, arcY := ch.X+arcOff[0], ch.Y+arcOff[1]
	frontX, frontY := ch.X+frontOff[0], ch.Y+frontOff[1]
	if !w.data.Maps[mapID].Walkable(arcX, arcY) || !w.data.Maps[mapID].Walkable(frontX, frontY) {
		t.Fatal("expected walkable arc and primary target tiles")
	}
	for i, point := range [][2]int{{arcX, arcY}, {frontX, frontY}} {
		tpl := w.data.Monsters[testMonsterID]
		id := fmt.Sprintf("wide-primary-%d", i)
		mon := newMonster(w, id, tpl, mapID, point[0], point[1], data.StdSpawn{MapID: mapID, MonsterID: tpl.ID, X: point[0], Y: point[1]})
		mon.Level = 1
		w.mu.Lock()
		w.monsters[id] = mon
		w.occupyMonsterLocked(mon)
		w.mu.Unlock()
	}
	w.mu.Lock()
	stats := w.combatStatsLocked(ch)
	minAttack := 3 + max(ch.Level, 1) + stats.DC
	maxAttack := 3 + max(ch.Level, 1) + stats.DCMax
	if maxAttack < minAttack {
		maxAttack = minAttack
	}
	w.rand = rand.New(&seqSource{vals: []int64{1, 0, 0, 0}})
	w.mu.Unlock()
	hit, err := w.HitWithIdent(ch, ch.X, ch.Y, dir, mir176.CMWideHit)
	if err != nil {
		t.Fatalf("HitWithIdent() error = %v", err)
	}
	if len(hit.MonsterHits) != 2 {
		t.Fatalf("MonsterHits = %d, want arc and primary results", len(hit.MonsterHits))
	}
	if hit.MonsterHits[0].MonsterID != "wide-primary-0" || hit.MonsterHits[1].MonsterID != "wide-primary-1" {
		t.Fatalf("MonsterHits = %+v, want arc then primary order", hit.MonsterHits)
	}
	w.mu.Lock()
	primary := w.monsters["wide-primary-1"]
	wantPrimaryDamage := minAttack
	if maxAttack > minAttack {
		wantPrimaryDamage += 1 % (maxAttack - minAttack + 1)
	}
	wantPrimaryDamage -= primary.Defense
	w.mu.Unlock()
	if hit.MonsterHits[1].Damage != wantPrimaryDamage {
		t.Fatalf("wide primary damage = %d, want base damage %d", hit.MonsterHits[1].Damage, wantPrimaryDamage)
	}
}

func TestWideHitProcessesCharacterArcBeforePrimaryTarget(t *testing.T) {
	w, attacker := newTestWorldCharacter(t)
	w.mu.Lock()
	w.monsters = map[string]*Monster{}
	w.occupied = map[monsterPosition]string{}
	w.rand = rand.New(zeroSource{})
	w.mu.Unlock()

	dir := 2
	arcOff := dirOffsets[(dir+7)%8]
	frontOff := dirOffsets[dir]
	arcX, arcY := attacker.X+arcOff[0], attacker.Y+arcOff[1]
	frontX, frontY := attacker.X+frontOff[0], attacker.Y+frontOff[1]
	if !w.data.Maps[attacker.MapID].Walkable(arcX, arcY) || !w.data.Maps[attacker.MapID].Walkable(frontX, frontY) {
		t.Fatal("expected walkable arc and primary target tiles")
	}
	arc, err := w.CreateCharacterWithAppearance("wide-arc-target", "wide-arc-target", "warrior", 0, 0, attacker.MapID, arcX, arcY)
	if err != nil {
		t.Fatalf("CreateCharacterWithAppearance() arc error = %v", err)
	}
	primary, err := w.CreateCharacterWithAppearance("wide-primary-target", "wide-primary-target", "warrior", 0, 0, attacker.MapID, frontX, frontY)
	if err != nil {
		t.Fatalf("CreateCharacterWithAppearance() primary error = %v", err)
	}
	arc.BonusAbil.Speed = -100
	primary.BonusAbil.Speed = -100
	arc.HP, arc.MaxHP = 100, 100
	primary.HP, primary.MaxHP = 100, 100

	w.mu.Lock()
	baseDamage := w.characterAttackDamageLocked(attacker, nil, mir176.CMHit)
	w.mu.Unlock()
	attacker.Skills = storage.SkillStates{{ID: "半月弯刀", Level: 0, Train: 0}}
	hit, err := w.HitWithIdent(attacker, attacker.X, attacker.Y, dir, mir176.CMWideHit, arc, primary)
	if err != nil {
		t.Fatalf("HitWithIdent() error = %v", err)
	}
	if len(hit.CharacterHits) != 2 {
		t.Fatalf("CharacterHits = %+v, want arc and primary results", hit.CharacterHits)
	}
	if hit.CharacterHits[0].Character.ID != arc.ID || hit.CharacterHits[1].Character.ID != primary.ID {
		t.Fatalf("CharacterHits = %+v, want arc then primary order", hit.CharacterHits)
	}
	if hit.CharacterHits[0].ImpactDelay != 500*time.Millisecond || hit.CharacterHits[1].ImpactDelay != 200*time.Millisecond {
		t.Fatalf("character impact delays = %s, %s; want 500ms secondary then 200ms primary", hit.CharacterHits[0].ImpactDelay, hit.CharacterHits[1].ImpactDelay)
	}
	if hit.CharacterHits[1].Damage != baseDamage {
		t.Fatalf("wide primary character damage = %d, want base damage %d", hit.CharacterHits[1].Damage, baseDamage)
	}
}

func TestWideHitOmitsMissedCharacterArcResult(t *testing.T) {
	w, attacker := newTestWorldCharacter(t)
	w.mu.Lock()
	w.monsters = map[string]*Monster{}
	w.occupied = map[monsterPosition]string{}
	w.rand = rand.New(&seqSource{vals: []int64{1 << 62, 0, 0, 0}})
	w.mu.Unlock()

	dir := 2
	arcOff := dirOffsets[(dir+7)%8]
	frontOff := dirOffsets[dir]
	arc, err := w.CreateCharacterWithAppearance("wide-missed-arc", "wide-missed-arc", "warrior", 0, 0, attacker.MapID, attacker.X+arcOff[0], attacker.Y+arcOff[1])
	if err != nil {
		t.Fatalf("CreateCharacterWithAppearance() arc error = %v", err)
	}
	primary, err := w.CreateCharacterWithAppearance("wide-hit-primary", "wide-hit-primary", "warrior", 0, 0, attacker.MapID, attacker.X+frontOff[0], attacker.Y+frontOff[1])
	if err != nil {
		t.Fatalf("CreateCharacterWithAppearance() primary error = %v", err)
	}
	arc.BonusAbil.Speed = 10000
	primary.BonusAbil.Speed = -100
	arc.HP, arc.MaxHP = 100, 100
	primary.HP, primary.MaxHP = 100, 100
	attacker.Skills = storage.SkillStates{{ID: "半月弯刀", Level: 0, Train: 0}}

	hit, err := w.HitWithIdent(attacker, attacker.X, attacker.Y, dir, mir176.CMWideHit, arc, primary)
	if err != nil {
		t.Fatalf("HitWithIdent() error = %v", err)
	}
	if len(hit.CharacterHits) != 1 || hit.CharacterHits[0].Character.ID != primary.ID {
		t.Fatalf("CharacterHits = %+v, want only connected primary target", hit.CharacterHits)
	}
}

func TestSecondaryMeleeDamageSkipsTargetDefense(t *testing.T) {
	w, attacker := prepareHitDamageTestWorld(t, nil)
	w.mu.Lock()
	w.rand = rand.New(zeroSource{})
	var mon *Monster
	for _, candidate := range w.monsters {
		mon = candidate
		break
	}
	mon.HP = 100
	mon.MaxHP = 100
	mon.DefenceUpUntil = time.Now().Add(time.Minute).UnixNano()
	w.mu.Unlock()
	w.mu.Lock()
	directHit, err := w.attackMonsterDirectDamageLocked(attacker, mon, 20)
	if err != nil {
		w.mu.Unlock()
		t.Fatalf("attackMonsterDirectDamageLocked() error = %v", err)
	}
	mon.HP = 100
	regularHit, err := w.attackMonsterWithDamageLocked(attacker, mon, 20)
	w.mu.Unlock()
	if err != nil {
		t.Fatalf("attackMonsterWithDamageLocked() error = %v", err)
	}
	if directHit.Damage != 20 {
		t.Fatalf("secondary damage = %d, want direct damage 20", directHit.Damage)
	}
	if regularHit.Damage >= directHit.Damage {
		t.Fatalf("regular damage = %d, want target defense to reduce direct damage %d", regularHit.Damage, directHit.Damage)
	}
}

func TestSecondaryMeleeDamageUsesReferenceHitCheck(t *testing.T) {
	w, attacker := prepareHitDamageTestWorld(t, nil)
	w.mu.Lock()
	w.rand = rand.New(&seqSource{vals: []int64{1 << 62}})
	var mon *Monster
	for _, candidate := range w.monsters {
		mon = candidate
		break
	}
	mon.HP = 100
	mon.MaxHP = 100
	mon.Speed = 10000
	directHit, err := w.attackMonsterDirectDamageLocked(attacker, mon, 20)
	w.mu.Unlock()
	if err != nil {
		t.Fatalf("attackMonsterDirectDamageLocked() error = %v", err)
	}
	if directHit.Damage != 0 || mon.HP != 100 {
		t.Fatalf("secondary miss = damage %d hp %d, want damage 0 and hp 100", directHit.Damage, mon.HP)
	}
}

func TestSecondaryMeleeDamageTriggersAnimalStruckSideEffects(t *testing.T) {
	w, attacker := prepareHitDamageTestWorld(t, nil)
	w.mu.Lock()
	w.rand = rand.New(zeroSource{})
	var mon *Monster
	for _, candidate := range w.monsters {
		mon = candidate
		break
	}
	mon.HP = 100
	mon.MaxHP = 100
	mon.TargetCharacterID = ""
	result, err := w.attackMonsterDirectDamageLocked(attacker, mon, 20)
	w.mu.Unlock()
	if err != nil {
		t.Fatalf("attackMonsterDirectDamageLocked() error = %v", err)
	}
	if result.Damage <= 0 {
		t.Fatalf("secondary damage = %d, want positive damage", result.Damage)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if mon.TargetCharacterID != attacker.ID {
		t.Fatalf("animal target = %q, want attacker %q", mon.TargetCharacterID, attacker.ID)
	}
	if !mon.LastAttackAt.After(time.Now()) {
		t.Fatalf("LastAttackAt = %v, want delayed after secondary Struck", mon.LastAttackAt)
	}
}

func TestSecondaryMeleeDamageDefersDeathSettlement(t *testing.T) {
	w, attacker := prepareHitDamageTestWorld(t, nil)
	w.mu.Lock()
	w.rand = rand.New(zeroSource{})
	var mon *Monster
	for _, candidate := range w.monsters {
		mon = candidate
		break
	}
	mon.HP = 5
	mon.MaxHP = 100
	result, err := w.attackMonsterDirectDamageLocked(attacker, mon, 20)
	w.mu.Unlock()
	if err != nil {
		t.Fatalf("attackMonsterDirectDamageLocked() error = %v", err)
	}
	if mon.HP != 0 || !mon.Alive || !mon.PendingDeath || mon.DeathHitterID != attacker.ID || mon.LastHitterID != attacker.ID || result.Dead || result.Experience != 0 || len(result.Drops) != 0 {
		t.Fatalf("secondary death settlement = hp %d alive %t pending %t hitter %q last %q dead %t exp %d drops %d, want deferred", mon.HP, mon.Alive, mon.PendingDeath, mon.DeathHitterID, mon.LastHitterID, result.Dead, result.Experience, len(result.Drops))
	}
}

func TestPrimaryMeleeDamageDefersDeathSettlement(t *testing.T) {
	w, attacker := prepareHitDamageTestWorld(t, nil)
	w.mu.Lock()
	w.rand = rand.New(zeroSource{})
	var mon *Monster
	for _, candidate := range w.monsters {
		mon = candidate
		break
	}
	mon.HP = 5
	mon.MaxHP = 100
	result, err := w.attackMonsterWithDamageLocked(attacker, mon, 20)
	w.mu.Unlock()
	if err != nil {
		t.Fatalf("attackMonsterWithDamageLocked() error = %v", err)
	}
	if mon.HP != 0 || !mon.Alive || !mon.PendingDeath || mon.DeathHitterID != attacker.ID || mon.LastHitterID != attacker.ID || result.Dead || result.Experience != 0 || len(result.Drops) != 0 {
		t.Fatalf("primary death settlement = hp %d alive %t pending %t hitter %q last %q dead %t exp %d drops %d, want deferred", mon.HP, mon.Alive, mon.PendingDeath, mon.DeathHitterID, mon.LastHitterID, result.Dead, result.Experience, len(result.Drops))
	}
	tick, err := w.Tick([]PlayerSnapshot{{Character: attacker}}, time.Now())
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if len(tick.MonsterDeaths) != 1 || tick.MonsterDeaths[0].MonsterID != mon.ID || tick.MonsterDeaths[0].Experience != mon.Experience {
		t.Fatalf("primary deferred death tick = %+v, want one death with experience", tick.MonsterDeaths)
	}
}

func TestCharacterStruckDamageCarriesReferenceDurabilityChange(t *testing.T) {
	w, attacker := newTestWorldCharacter(t)
	target, err := w.CreateCharacterWithAppearance("durability-target", "durability-target", "warrior", 0, 0, attacker.MapID, attacker.X+1, attacker.Y)
	if err != nil {
		t.Fatalf("CreateCharacterWithAppearance() error = %v", err)
	}
	target.HP = 100
	target.MaxHP = 100
	target.EquippedItems = map[int]storage.UserItem{
		SlotDress: {ItemID: testArmorID, Dura: 10000, DuraMax: 10000},
	}
	w.mu.Lock()
	w.rand = rand.New(zeroSource{})
	_, hit, err := w.attackCharacterDirectDamageLocked(attacker, target, 20)
	w.mu.Unlock()
	if err != nil {
		t.Fatalf("attackCharacterDirectDamageLocked() error = %v", err)
	}
	if len(hit.Durability) != 1 {
		t.Fatalf("durability changes = %+v, want one dress change", hit.Durability)
	}
	change := hit.Durability[0]
	if change.Slot != SlotDress || change.Dura != 9995 || change.DuraMax != 10000 {
		t.Fatalf("durability change = %+v, want dress 9995/10000", change)
	}
	if hit.Character.EquippedItems[SlotDress].Dura != 9990 {
		t.Fatalf("target dress durability = %d, want 9990", hit.Character.EquippedItems[SlotDress].Dura)
	}
}

func TestCharacterStruckDamagePersistsAttackerPKFlag(t *testing.T) {
	w, attacker := newTestWorldCharacter(t)
	attacker.FreePKArea = true
	target := storage.Character{ID: "target", MapID: attacker.MapID, X: attacker.X + 1, Y: attacker.Y, HP: 100, MaxHP: 100, FreePKArea: true}
	w.mu.Lock()
	w.rand = rand.New(zeroSource{})
	_, hit, err := w.attackCharacterDirectDamageLocked(attacker, target, 20)
	w.mu.Unlock()
	if err != nil {
		t.Fatalf("attackCharacterDirectDamageLocked() error = %v", err)
	}
	if hit.Damage <= 0 {
		t.Fatalf("hit damage = %d, want positive damage", hit.Damage)
	}
	updated, ok := w.store.Character(attacker.ID)
	if !ok || !updated.PKFlag || updated.PKFlagUntil <= time.Now().UnixNano() {
		t.Fatalf("stored attacker PK state = %+v, want active marker", updated)
	}
	updatedTarget, ok := w.store.Character(target.ID)
	if !ok || updatedTarget.LastHitterID != attacker.ID || updatedTarget.LastHitterAt == 0 {
		t.Fatalf("stored target last hitter = %+v, want attacker %q", updatedTarget, attacker.ID)
	}
}

func TestCharacterStruckDamageResetsNaturalSpellTick(t *testing.T) {
	w, attacker := newTestWorldCharacter(t)
	attacker.FreePKArea = true
	target := storage.Character{ID: "target", MapID: attacker.MapID, X: attacker.X + 1, Y: attacker.Y, HP: 100, MaxHP: 100, PerHealth: 5, PerSpell: 5, SpellTick: 700, FreePKArea: true}
	w.mu.Lock()
	w.rand = rand.New(zeroSource{})
	updated, hit, err := w.attackCharacterDirectDamageLocked(attacker, target, 20)
	w.mu.Unlock()
	if err != nil {
		t.Fatalf("attackCharacterDirectDamageLocked() error = %v", err)
	}
	if hit.Damage <= 0 || updated.SpellTick != 0 {
		t.Fatalf("physical damage result = hit:%+v character:%+v, want positive damage and zero SpellTick", hit, updated)
	}
	if updated.PerHealth != 4 || updated.PerSpell != 4 {
		t.Fatalf("physical recovery counters = %d/%d, want 4/4", updated.PerHealth, updated.PerSpell)
	}
}

func TestChargeCharacterDamagePersistsAttackerPKFlag(t *testing.T) {
	w, attacker := newTestWorldCharacter(t)
	attacker.FreePKArea = true
	target := storage.Character{ID: "target", MapID: attacker.MapID, X: attacker.X + 1, Y: attacker.Y, HP: 100, MaxHP: 100, FreePKArea: true}
	w.mu.Lock()
	w.rand = rand.New(zeroSource{})
	_, hit, err := w.chargeCharacterWithDamageLocked(attacker, target, 20)
	w.mu.Unlock()
	if err != nil {
		t.Fatalf("chargeCharacterWithDamageLocked() error = %v", err)
	}
	if hit.Damage <= 0 || !hit.AttackerNameColorChanged {
		t.Fatalf("hit = %+v, want positive hit with name color change", hit)
	}
	updated, ok := w.store.Character(attacker.ID)
	if !ok || !updated.PKFlag {
		t.Fatalf("stored attacker PK state = %+v, want active marker", updated)
	}
}

func TestCharacterStruckDamageRefreshesFeatureWhenDressBreaks(t *testing.T) {
	w, attacker := newTestWorldCharacter(t)
	target, err := w.CreateCharacterWithAppearance("broken-dress-target", "broken-dress-target", "warrior", 0, 0, attacker.MapID, attacker.X+1, attacker.Y)
	if err != nil {
		t.Fatalf("CreateCharacterWithAppearance() error = %v", err)
	}
	target.HP = 100
	target.MaxHP = 100
	target.EquippedItems = map[int]storage.UserItem{
		SlotDress: {ItemID: testArmorID, Dura: 5, DuraMax: 10000},
	}
	w.mu.Lock()
	w.rand = rand.New(zeroSource{})
	_, hit, err := w.attackCharacterDirectDamageLocked(attacker, target, 20)
	w.mu.Unlock()
	if err != nil {
		t.Fatalf("attackCharacterDirectDamageLocked() error = %v", err)
	}
	if !hit.FeatureChanged {
		t.Fatal("FeatureChanged = false, want true after dress breaks")
	}
	if len(hit.Durability) != 0 {
		t.Fatalf("durability changes = %+v, want none when displayed durability stays zero", hit.Durability)
	}
	if len(hit.DeletedItems) != 1 || hit.DeletedItems[0].ItemID != testArmorID {
		t.Fatalf("deleted items = %+v, want broken dress %q", hit.DeletedItems, testArmorID)
	}
	if item := hit.Character.EquippedItems[SlotDress]; item.ItemID != "" || item.Dura != 0 {
		t.Fatalf("broken dress = %+v, want empty item with zero durability", item)
	}
}

func TestMagicCharacterDamageCarriesReferenceDurabilityChange(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	target, err := w.CreateCharacterWithAppearance("magic-durability-target", "magic-durability-target", "warrior", 0, 0, caster.MapID, caster.X+1, caster.Y)
	if err != nil {
		t.Fatalf("CreateCharacterWithAppearance() error = %v", err)
	}
	target.HP = 100
	target.MaxHP = 100
	target.EquippedItems = map[int]storage.UserItem{
		SlotDress: {ItemID: testArmorID, Dura: 10000, DuraMax: 10000},
	}
	w.mu.Lock()
	w.rand = rand.New(zeroSource{})
	_, hit, err := w.spellCharacterDamageWithPowerLocked(caster, target, 20)
	w.mu.Unlock()
	if err != nil {
		t.Fatalf("spellCharacterDamageWithPowerLocked() error = %v", err)
	}
	if len(hit.Durability) != 1 || hit.Durability[0].Dura != 9995 {
		t.Fatalf("magic durability changes = %+v, want dress 9995", hit.Durability)
	}
	if hit.Character.EquippedItems[SlotDress].Dura != 9990 {
		t.Fatalf("magic target dress durability = %d, want 9990", hit.Character.EquippedItems[SlotDress].Dura)
	}
}

func TestHitWithIdentReturnsCharacterHitOnPlayerTarget(t *testing.T) {
	w, attacker := newTestWorldCharacter(t)
	w.mu.Lock()
	w.monsters = map[string]*Monster{}
	w.occupied = map[monsterPosition]string{}
	w.mu.Unlock()
	mapID := attacker.MapID
	target, err := w.CreateCharacterWithAppearance("test2", "target", "warrior", 0, 0, mapID, attacker.X+1, attacker.Y)
	if err != nil {
		t.Fatalf("CreateCharacterWithAppearance() error = %v", err)
	}
	target.BonusAbil.Speed = -100
	updated, err := w.HitWithIdent(attacker, attacker.X, attacker.Y, 2, mir176.CMHit, target)
	if err != nil {
		t.Fatalf("HitWithIdent() error = %v", err)
	}
	if len(updated.CharacterHits) != 1 {
		t.Fatalf("CharacterHits = %+v, want 1", updated.CharacterHits)
	}
	hit := updated.CharacterHits[0]
	if hit.Character.ID != target.ID {
		t.Fatalf("hit target = %s, want %s", hit.Character.ID, target.ID)
	}
	if hit.AttackerID != attacker.ID {
		t.Fatalf("hit attacker = %s, want %s", hit.AttackerID, attacker.ID)
	}
	if hit.Damage <= 0 {
		t.Fatalf("hit damage = %d, want positive", hit.Damage)
	}
}

func TestSpawnMonsterByNameAddsLiveMonsters(t *testing.T) {
	w, ch := newRealDataWorldCharacter(t)
	before, _ := w.SnapshotAround(ch.MapID, 0, 0, 99999)
	result, err := spawnMonsterForTest(t, w, ch.MapID, ch.X+1, ch.Y, "鹿", 2)
	if err != nil {
		t.Fatalf("SpawnMonsterByName() error = %v", err)
	}
	if len(result.Monsters) != 2 {
		t.Fatalf("spawn result = %d monsters, want 2", len(result.Monsters))
	}
	after, _ := w.SnapshotAround(ch.MapID, 0, 0, 99999)
	if len(after) != len(before)+2 {
		t.Fatalf("monster count = %d, want %d", len(after), len(before)+2)
	}
	for _, mon := range result.Monsters {
		if mon.TemplateID != "鹿" || mon.Name != "鹿" || mon.MapID != ch.MapID || !mon.Alive {
			t.Fatalf("spawned monster = %+v", mon)
		}
	}
	if result.Monsters[0].X != ch.X+1 || result.Monsters[0].Y != ch.Y {
		t.Fatalf("first spawned monster = (%d,%d), want (%d,%d)", result.Monsters[0].X, result.Monsters[0].Y, ch.X+1, ch.Y)
	}
	seen := map[[2]int]bool{}
	for _, mon := range result.Monsters {
		pos := [2]int{mon.X, mon.Y}
		if seen[pos] {
			t.Fatalf("spawned monsters overlapped at (%d,%d): %+v", mon.X, mon.Y, result.Monsters)
		}
		seen[pos] = true
	}
}

func TestSpawnMonsterByNameLoadsRedMoonEvilFromConfigs(t *testing.T) {
	w, ch := newRealDataWorldCharacter(t)
	result, err := spawnMonsterForTest(t, w, ch.MapID, ch.X+1, ch.Y, "赤月恶魔", 1)
	if err != nil {
		t.Fatalf("SpawnMonsterByName() error = %v", err)
	}
	if len(result.Monsters) != 1 || result.Monsters[0].TemplateID != "赤月恶魔" || result.Monsters[0].Name != "赤月恶魔" {
		t.Fatalf("spawn result = %+v", result.Monsters)
	}
}

func TestSpawnMonsterCarriesConfiguredCombatAttributes(t *testing.T) {
	bundle := loadTestBundle(t)
	addSpawnNearDefault(t, &bundle, "半兽人", 2, 0)
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	w := New(bundle, store)
	monsters, _ := w.SnapshotAround(testMapID, 0, 0, 99999)
	if len(monsters) != 1 {
		t.Fatalf("monster count = %d, want 1", len(monsters))
	}
	mon := monsters[0]
	if mon.Level != 15 || mon.HP != 30 || mon.Defense != 1 || mon.MinAttack != 4 || mon.MaxAttack != 9 || mon.Experience != 20 || mon.WalkSpeedMS != 1500 {
		t.Fatalf("runtime monster attributes = %+v", mon)
	}
}

func TestSpawnMonsterByNameReturnsErrorWhenNoSpaceIsAvailable(t *testing.T) {
	bundle := loadTestBundle(t)
	targetX, targetY := startCoordsForMap(t, bundle, testMapID)
	targetX++
	blockedMap := bundle.Maps[testMapID]
	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			blockedMap.Blocked = append(blockedMap.Blocked, data.StdPoint{X: targetX + dx, Y: targetY + dy})
		}
	}
	bundle.Maps[testMapID] = blockedMap
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	w := New(bundle, store)
	mapID, x, y := defaultSpawn(bundle)
	ch, err := w.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	if _, err := spawnMonsterForTest(t, w, ch.MapID, targetX, targetY, "鹿", 1); err == nil {
		t.Fatalf("SpawnMonsterByName() expected error when no spawn positions are available")
	}
}

func TestInitialSpawnDoesNotOverlapWhenCountExceedsRange(t *testing.T) {
	bundle := loadTestBundle(t)
	startX, startY := startCoordsForMap(t, bundle, testMapID)
	bundle.Spawns = []data.StdSpawn{{MapID: testMapID, MonsterID: "鹿", X: startX + 2, Y: startY, Range: 1, Count: 3, RespawnSeconds: 10}}
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	w := New(bundle, store)
	monsters, _ := w.SnapshotAround(testMapID, 0, 0, 99999)
	if len(monsters) != 3 {
		t.Fatalf("monster count = %d, want 3", len(monsters))
	}
	seen := map[[2]int]bool{}
	for _, mon := range monsters {
		pos := [2]int{mon.X, mon.Y}
		if seen[pos] {
			t.Fatalf("initial monsters overlapped at (%d,%d): %+v", mon.X, mon.Y, monsters)
		}
		seen[pos] = true
	}
}

func TestInitialSpawnScalesWithMapMonsterSpawnRate(t *testing.T) {
	bundle := loadTestBundle(t)
	mp := bundle.Maps[testMapID]
	startX, startY := startCoordsForMap(t, bundle, testMapID)
	bundle.Spawns = []data.StdSpawn{{MapID: testMapID, MonsterID: "鹿", X: startX + 2, Y: startY, Range: 1, Count: 4, RespawnSeconds: 10}}
	mp.MonsterSpawnRate = 20
	bundle.Maps[testMapID] = mp
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	w := New(bundle, store)
	monsters, _ := w.SnapshotAround(testMapID, 0, 0, 99999)
	if len(monsters) != 2 {
		t.Fatalf("monster count = %d, want 2 when map spawn rate doubles the divisor", len(monsters))
	}
}

func TestRespawnDoesNotOverlapLivingMonster(t *testing.T) {
	bundle := loadTestBundle(t)
	startX, startY := startCoordsForMap(t, bundle, testMapID)
	spawn := data.StdSpawn{MapID: testMapID, MonsterID: "鹿", X: startX + 2, Y: startY, Range: 1, Count: 1, RespawnSeconds: 10}
	bundle.Spawns = []data.StdSpawn{spawn}
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	w := New(bundle, store)
	monsters, _ := w.SnapshotAround(testMapID, 0, 0, 99999)
	if len(monsters) != 1 {
		t.Fatalf("monster count = %d, want 1", len(monsters))
	}
	mon := monsters[0]

	w.mu.Lock()
	w.vacateMonsterLocked(w.monsters[mon.ID])
	w.monsters[mon.ID].Alive = false
	w.monsters[mon.ID].HP = 0
	w.monsters[mon.ID].RespawnAt = time.Unix(1, 0)
	w.monsters["blocker"] = &Monster{
		ID:         "blocker",
		TemplateID: mon.TemplateID,
		Name:       "Blocker",
		MapID:      spawn.MapID,
		X:          spawn.X,
		Y:          spawn.Y,
		HP:         100,
		MaxHP:      100,
		Alive:      true,
	}
	w.occupyMonsterLocked(w.monsters["blocker"])
	w.mu.Unlock()

	after, _ := w.SnapshotAround("0", 0, 0, 99999)
	seen := map[[2]int]string{}
	for _, current := range after {
		pos := [2]int{current.X, current.Y}
		if other := seen[pos]; other != "" {
			t.Fatalf("monsters %s and %s overlapped at (%d,%d): %+v", other, current.ID, current.X, current.Y, after)
		}
		seen[pos] = current.ID
	}
	revived := false
	for _, current := range after {
		if current.ID == mon.ID && current.Alive && (current.X != spawn.X || current.Y != spawn.Y) {
			revived = true
		}
	}
	if !revived {
		t.Fatalf("dead monster did not respawn away from blocker: %+v", after)
	}
}

func TestSpawnMonsterCarriesConfiguredSpecialState(t *testing.T) {
	w, ch := newRealDataWorldCharacter(t)

	result, err := spawnMonsterForTest(t, w, ch.MapID, ch.X+1, ch.Y, "暗之触龙神", 1)
	if err != nil {
		t.Fatalf("SpawnMonsterByName() error = %v", err)
	}
	if len(result.Monsters) != 1 {
		t.Fatalf("spawn result = %+v, want one monster", result.Monsters)
	}
	dragon := result.Monsters[0]
	if !dragon.Hidden || !dragon.FixedHideMode || !dragon.StoneMode || dragon.Dir != 5 {
		t.Fatalf("spawned monster = %+v, want hidden stone monster facing south", dragon)
	}

	result, err = spawnMonsterForTest(t, w, ch.MapID, ch.X+2, ch.Y, "圣域弓箭手", 1)
	if err != nil {
		t.Fatalf("SpawnMonsterByName() error = %v", err)
	}
	if len(result.Monsters) != 1 {
		t.Fatalf("spawn result = %+v, want one monster", result.Monsters)
	}
	archer := result.Monsters[0]
	if archer.AttackMax != 6 {
		t.Fatalf("spawned monster = %+v, want configured attack_max 6", archer)
	}
}

func TestRespawnRestoresConfiguredSpecialState(t *testing.T) {
	bundle := loadTestBundle(t)
	addSpawnNearDefault(t, &bundle, "暗之触龙神", 2, 0)
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	w := New(bundle, store)
	w.mu.Lock()
	var mon *Monster
	for _, candidate := range w.monsters {
		if candidate.TemplateID == "暗之触龙神" {
			mon = candidate
			break
		}
	}
	if mon == nil {
		w.mu.Unlock()
		t.Fatalf("expected spawned 暗之触龙神")
	}
	w.vacateMonsterLocked(mon)
	mon.Alive = false
	mon.HP = 0
	mon.RespawnAt = time.Unix(10, 0)
	w.respawnLocked(time.Unix(20, 0))
	updated := w.monsters[mon.ID]
	w.mu.Unlock()
	if updated == nil || !updated.Alive || !updated.Hidden || !updated.FixedHideMode || !updated.StoneMode || updated.Dir != 5 {
		t.Fatalf("respawned monster = %+v, want configured special state restored", updated)
	}
}

func TestTurnUpdatesDirectionInPlace(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	x, y := ch.X, ch.Y
	ch, err := w.Turn(ch, x, y, 3)
	if err != nil {
		t.Fatalf("Turn() error = %v", err)
	}
	if ch.Dir != 3 {
		t.Fatalf("Dir = %d, want 3", ch.Dir)
	}
}

func TestTurnRejectsCoordinateMismatch(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	x, y := ch.X, ch.Y
	if _, err := w.Turn(ch, x+1, y, 3); err == nil {
		t.Fatalf("Turn() expected error for mismatched coordinates")
	}
}

func TestTurnRejectsInvalidDirection(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	x, y := ch.X, ch.Y
	if _, err := w.Turn(ch, x, y, 8); err == nil {
		t.Fatalf("Turn() expected error for invalid direction")
	}
}

func TestCombatStatsZeroWithNothingEquipped(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	stats := w.CombatStats(ch)
	if stats != (CombatStats{}) {
		t.Fatalf("CombatStats() = %+v, want zero value", stats)
	}
}

func TestUseItemSlotOrderMatchesReferenceLayout(t *testing.T) {
	got := []int{
		SlotDress,
		SlotWeapon,
		SlotRightHand,
		SlotNecklace,
		SlotHelmet,
		SlotArmRingL,
		SlotArmRingR,
		SlotRingL,
		SlotRingR,
		SlotBujuk,
		SlotBelt,
		SlotBoots,
		SlotCharm,
	}
	want := []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("slot order = %v, want %v", got, want)
	}
}

func TestEquipWeaponAddsDCAndMovesItOutOfBag(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	ch.BagItems = []storage.UserItem{{ItemID: testWeaponID, MakeIndex: 1}}
	updated, err := w.EquipItem(ch, SlotWeapon, testWeaponID)
	if err != nil {
		t.Fatalf("EquipItem() error = %v", err)
	}
	if updated.EquippedItems[SlotWeapon].ItemID != testWeaponID {
		t.Fatalf("EquippedItems[SlotWeapon].ItemID = %q, want %s", updated.EquippedItems[SlotWeapon].ItemID, testWeaponID)
	}
	for _, entry := range updated.BagItems {
		if entry.ItemID == testWeaponID {
			t.Fatalf("expected %s to be removed from bag once equipped, got %+v", testWeaponID, entry)
		}
	}
	stats := w.CombatStats(updated)
	if stats.DC != 2 || stats.DCMax != 5 {
		t.Fatalf("CombatStats() = %+v, want DC=2 DCMax=5", stats)
	}
}

func TestEquipItemByBagIndexUsesMakeIndex(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	ch.BagItems = []storage.UserItem{{ItemID: testWeaponID, MakeIndex: 7}}
	updated, err := w.EquipItemByBagIndex(ch, SlotWeapon, 7, testWeaponID)
	if err != nil {
		t.Fatalf("EquipItemByBagIndex() error = %v", err)
	}
	if updated.EquippedItems[SlotWeapon].ItemID != testWeaponID {
		t.Fatalf("EquippedItems[SlotWeapon].ItemID = %q, want %s", updated.EquippedItems[SlotWeapon].ItemID, testWeaponID)
	}
	if got := updated.EquippedItems[SlotWeapon].MakeIndex; got != 7 {
		t.Fatalf("EquippedItems[SlotWeapon].MakeIndex = %d, want 7", got)
	}
}

func TestEquipItemByBagIndexRejectsMismatchedMakeIndex(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	ch.BagItems = []storage.UserItem{{ItemID: testWeaponID, MakeIndex: 7}}
	if _, err := w.EquipItemByBagIndex(ch, SlotWeapon, 8, testWeaponID); err == nil {
		t.Fatal("EquipItemByBagIndex() accepted a mismatched MakeIndex")
	}
}

func TestEquipAndUnequipPreserveStdModeMapDesc(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	ch.Level = 16
	var desc [14]byte
	desc[8] = 1
	ch.BagItems = []storage.UserItem{{ItemID: "护身戒指", MakeIndex: 7, Desc: desc}}
	equipped, err := w.EquipItemByBagIndex(ch, SlotRingL, 7, "护身戒指")
	if err != nil {
		t.Fatalf("EquipItemByBagIndex() error = %v", err)
	}
	if got := equipped.EquippedItems[SlotRingL].Desc[8]; got != 0 {
		t.Fatalf("EquippedItems[SlotRingL].desc[8] = %d, want 0 after equip", got)
	}
	unequipped, err := w.UnequipItemByItemID(equipped, SlotRingL, "护身戒指")
	if err != nil {
		t.Fatalf("UnequipItemByItemID() error = %v", err)
	}
	found := false
	for _, entry := range unequipped.BagItems {
		if entry.ItemID == "护身戒指" {
			found = true
			if got := entry.Desc[8]; got != 0 {
				t.Fatalf("bag desc[8] = %d, want 0 after unequip", got)
			}
		}
	}
	if !found {
		t.Fatalf("expected 护身戒指 back in bag, got %+v", unequipped.BagItems)
	}
	for _, entry := range unequipped.BagItems {
		if entry.ItemID == "护身戒指" && entry.MakeIndex != 7 {
			t.Fatalf("unequipped MakeIndex = %d, want 7", entry.MakeIndex)
		}
	}
}

func TestCanWearInSlotLockedMatchesReferenceStdModes(t *testing.T) {
	w, _ := newTestWorldCharacter(t)
	if w.canWearInSlotLocked(data.StdItem{StdMode: 16}, SlotHelmet) {
		t.Fatalf("StdMode 16 should not be wearable in helmet slot")
	}
	if !w.canWearInSlotLocked(data.StdItem{StdMode: 15}, SlotHelmet) {
		t.Fatalf("StdMode 15 should be wearable in helmet slot")
	}
	if !w.canWearInSlotLocked(data.StdItem{StdMode: 25}, SlotArmRingL) {
		t.Fatalf("StdMode 25 should be wearable in left arm ring slot")
	}
	if w.canWearInSlotLocked(data.StdItem{StdMode: 25}, SlotArmRingR) {
		t.Fatalf("StdMode 25 should not be wearable in right arm ring slot")
	}
}

func TestEquipArmorAddsAC(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	ch.BagItems = []storage.UserItem{{ItemID: testArmorID}}
	updated, err := w.EquipItem(ch, SlotArmor, testArmorID)
	if err != nil {
		t.Fatalf("EquipItem() error = %v", err)
	}
	stats := w.CombatStats(updated)
	if stats.AC != 0 || stats.ACMax != 2 || stats.MAC != 0 || stats.MACMax != 1 {
		t.Fatalf("CombatStats() = %+v, want AC=0 ACMax=2 MAC=0 MACMax=1", stats)
	}
}

func TestEquipRejectsWrongKindForSlot(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	if _, err := w.EquipItem(ch, SlotArmor, testWeaponID); err == nil {
		t.Fatalf("EquipItem() expected error equipping a weapon into the armor slot")
	}
}

func TestEquipThenUnequipReturnsItemToBag(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	equipped, err := w.EquipItem(ch, SlotWeapon, testWeaponID)
	if err != nil {
		t.Fatalf("EquipItem() error = %v", err)
	}
	unequipped, err := w.UnequipItem(equipped, SlotWeapon)
	if err != nil {
		t.Fatalf("UnequipItem() error = %v", err)
	}
	if unequipped.EquippedItems[SlotWeapon].ItemID != "" {
		t.Fatalf("EquippedItems[SlotWeapon].ItemID = %q, want empty after UnequipItem()", unequipped.EquippedItems[SlotWeapon].ItemID)
	}
	found := false
	for _, entry := range unequipped.BagItems {
		if entry.ItemID == testWeaponID {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected %s back in bag, got %+v", testWeaponID, unequipped.BagItems)
	}
	if stats := w.CombatStats(unequipped); stats != (CombatStats{}) {
		t.Fatalf("CombatStats() = %+v, want zero value after unequip", stats)
	}
}

func TestEquipSwapsPreviouslyEquippedItemBackToBag(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	ch.BagItems = []storage.UserItem{{ItemID: testWeaponID}}
	setEquippedItem(&ch, SlotWeapon, storage.UserItem{ItemID: "铁剑"})
	equipped, err := w.EquipItem(ch, SlotWeapon, testWeaponID)
	if err != nil {
		t.Fatalf("EquipItem() error = %v", err)
	}
	if equipped.EquippedItems[SlotWeapon].ItemID != testWeaponID {
		t.Fatalf("EquippedItems[SlotWeapon].ItemID = %q, want %s", equipped.EquippedItems[SlotWeapon].ItemID, testWeaponID)
	}
	if equipped.BagItems[0].ItemID != "铁剑" {
		t.Fatalf("bag[0] = %+v, want previous weapon returned to same slot", equipped.BagItems[0])
	}
}

func TestDropItemMovesOneItemToGround(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	ch.BagItems = []storage.UserItem{
		{ItemID: testHPItemID, MakeIndex: 1},
		{ItemID: testHPItemID, MakeIndex: 2},
		{ItemID: testHPItemID, MakeIndex: 3},
	}
	updated, drop, err := w.DropItemCountByBagIndex(ch, 0, testHPItemID)
	if err != nil {
		t.Fatalf("DropItem() error = %v", err)
	}
	if drop.ItemID != testHPItemID || drop.Count != 1 {
		t.Fatalf("DropItem() = %+v, want %s x1", drop, testHPItemID)
	}
	if countBagItems(updated.BagItems) != 2 {
		t.Fatalf("bag = %+v, want two items left after dropping one", updated.BagItems)
	}
	found := 0
	for _, entry := range updated.BagItems {
		if entry.ItemID == testHPItemID {
			found++
		}
	}
	if found != 2 {
		t.Fatalf("bag = %+v, want two remaining potions", updated.BagItems)
	}
	if _, ok := w.drops[drop.ID]; !ok {
		t.Fatalf("drop %s was not recorded on the ground", drop.ID)
	}
}

func TestDropItemCountDropsPartialStack(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	ch.BagItems = []storage.UserItem{
		{ItemID: testHPItemID, MakeIndex: 1},
		{ItemID: testHPItemID, MakeIndex: 2},
		{ItemID: testHPItemID, MakeIndex: 3},
	}
	updated, drop, err := w.DropItemCountByBagIndex(ch, 0, testHPItemID)
	if err != nil {
		t.Fatalf("DropItemCount() error = %v", err)
	}
	if drop.ItemID != testHPItemID || drop.Count != 1 {
		t.Fatalf("DropItemCount() = %+v, want %s x1", drop, testHPItemID)
	}
	found := false
	for _, entry := range updated.BagItems {
		if entry.ItemID == testHPItemID {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected remaining entry in bag, got %+v", updated.BagItems)
	}
}

func TestDropItemCountByBagIndexUsesMakeIndex(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	ch.BagItems = []storage.UserItem{
		{ItemID: testHPItemID, MakeIndex: 11},
		{ItemID: testHPItemID, MakeIndex: 12},
		{ItemID: testHPItemID, MakeIndex: 13},
	}
	updated, drop, err := w.DropItemCountByBagIndex(ch, 11, testHPItemID)
	if err != nil {
		t.Fatalf("DropItemCountByBagIndex() error = %v", err)
	}
	if drop.ItemID != testHPItemID || drop.Count != 1 {
		t.Fatalf("DropItemCountByBagIndex() = %+v, want %s x1", drop, testHPItemID)
	}
	if drop.MakeIndex != 11 {
		t.Fatalf("DropItemCountByBagIndex() MakeIndex = %d, want 11", drop.MakeIndex)
	}
	found := false
	for _, entry := range updated.BagItems {
		if entry.ItemID == testHPItemID {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected remaining entry in bag, got %+v", updated.BagItems)
	}
}

func TestDropItemCountByBagIndexClearsStaleQuickSlotBinding(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	ch.BagItems = []storage.UserItem{
		{ItemID: testMPItemID, MakeIndex: 21},
	}
	setEquippedItem(&ch, SlotCharm, storage.UserItem{ItemID: testMPItemID, MakeIndex: 21, Dura: 8})

	updated, drop, err := w.DropItemCountByBagIndex(ch, 21, testMPItemID)
	if err != nil {
		t.Fatalf("DropItemCountByBagIndex() error = %v", err)
	}
	if drop.ItemID != testMPItemID {
		t.Fatalf("DropItemCountByBagIndex() drop = %+v, want %s", drop, testMPItemID)
	}
	if updated.EquippedItems[SlotCharm].ItemID != "" {
		t.Fatalf("EquippedItems[SlotCharm].ItemID = %q, want cleared", updated.EquippedItems[SlotCharm].ItemID)
	}
	if updated.EquippedItems[SlotCharm].MakeIndex != 0 {
		t.Fatalf("EquippedItems[SlotCharm].MakeIndex = %d, want 0", updated.EquippedItems[SlotCharm].MakeIndex)
	}
	if updated.EquippedItems[SlotCharm].Dura != 0 {
		t.Fatalf("EquippedItems[SlotCharm].Dura = %d, want 0", updated.EquippedItems[SlotCharm].Dura)
	}
}

func TestPickupAtPreservesGroundMakeIndex(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	ch.BagItems = []storage.UserItem{}
	w.drops["drop-1"] = GroundDrop{
		ID:        "drop-1",
		MapID:     ch.MapID,
		X:         ch.X,
		Y:         ch.Y,
		ItemID:    testHPItemID,
		Count:     1,
		MakeIndex: 88,
	}
	updated, _, err := w.PickupAt(ch, ch.X, ch.Y)
	if err != nil {
		t.Fatalf("PickupAt() error = %v", err)
	}
	found := false
	for _, entry := range updated.BagItems {
		if entry.ItemID == testHPItemID {
			found = true
			if entry.MakeIndex != 88 {
				t.Fatalf("picked up MakeIndex = %d, want 88", entry.MakeIndex)
			}
		}
	}
	if !found {
		t.Fatalf("expected %s in bag after pickup, got %+v", testHPItemID, updated.BagItems)
	}
}

func TestPickupHonorsOwnershipUntilCooldown(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	other := ch
	other.ID = "other-player"
	drop := GroundDrop{
		ID:        "drop-1",
		MapID:     ch.MapID,
		X:         ch.X,
		Y:         ch.Y,
		ItemID:    testHPItemID,
		Count:     1,
		OwnerID:   ch.ID,
		PickupAt:  time.Now().Add(2 * time.Minute),
		MakeIndex: 77,
	}
	w.drops[drop.ID] = drop
	if _, _, err := w.PickupAt(other, other.X, other.Y); err == nil {
		t.Fatalf("PickupAt() expected ownership rejection before cooldown")
	}
	updated, _, err := w.PickupAt(ch, ch.X, ch.Y)
	if err != nil {
		t.Fatalf("PickupAt(owner) error = %v", err)
	}
	found := false
	for _, entry := range updated.BagItems {
		if entry.ItemID == testHPItemID && entry.MakeIndex == 77 {
			found = true
		}
	}
	if !found {
		t.Fatalf("pickup bag = %+v, want preserved owner pickup", updated.BagItems)
	}
}

func TestPickupHonorsGroupMembersUntilCooldown(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	member := ch
	member.ID = "group-member"
	member.GroupOwnerID = ch.ID
	drop := GroundDrop{
		ID:        "drop-2",
		MapID:     ch.MapID,
		X:         ch.X,
		Y:         ch.Y,
		ItemID:    testHPItemID,
		Count:     1,
		OwnerID:   ch.ID,
		PickupAt:  time.Now().Add(2 * time.Minute),
		MakeIndex: 78,
	}
	w.drops[drop.ID] = drop
	updated, _, err := w.PickupAt(member, member.X, member.Y)
	if err != nil {
		t.Fatalf("PickupAt(group member) error = %v", err)
	}
	found := false
	for _, entry := range updated.BagItems {
		if entry.ItemID == testHPItemID && entry.MakeIndex == 78 {
			found = true
		}
	}
	if !found {
		t.Fatalf("pickup bag = %+v, want preserved group pickup", updated.BagItems)
	}
}

func TestPickupKeepsItemsSeparate(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	ch.BagItems = []storage.UserItem{}
	w.drops["drop-1"] = GroundDrop{
		ID:        "drop-1",
		MapID:     ch.MapID,
		X:         ch.X,
		Y:         ch.Y,
		ItemID:    testHPItemID,
		Count:     1,
		MakeIndex: 91,
		PickupAt:  time.Now().Add(-time.Second),
	}
	w.drops["drop-2"] = GroundDrop{
		ID:        "drop-2",
		MapID:     ch.MapID,
		X:         ch.X,
		Y:         ch.Y,
		ItemID:    testHPItemID,
		Count:     1,
		MakeIndex: 92,
		PickupAt:  time.Now().Add(-time.Second),
	}
	updated, _, err := w.PickupAt(ch, ch.X, ch.Y)
	if err != nil {
		t.Fatalf("first PickupAt() error = %v", err)
	}
	updated, _, err = w.PickupAt(updated, updated.X, updated.Y)
	if err != nil {
		t.Fatalf("second PickupAt() error = %v", err)
	}
	if countBagItems(updated.BagItems) != 2 {
		t.Fatalf("bag len = %d, want 2 separate entries: %+v", countBagItems(updated.BagItems), updated.BagItems)
	}
	for _, entry := range updated.BagItems {
		if entry.ItemID == "" {
			continue
		}
		if entry.ItemID != testHPItemID {
			t.Fatalf("entry = %+v, want %s", entry, testHPItemID)
		}
	}
}

func TestPlaceDropsAvoidsBlockedTiles(t *testing.T) {
	bundle := data.StdBundle{
		Items: map[string]data.StdItem{
			testHPItemID: {
				ID:   testHPItemID,
				Name: testHPItemID,
				Kind: "misc",
			},
		},
		Maps: map[string]data.StdMap{
			"tiny": {
				ID:     "tiny",
				Name:   "tiny",
				Width:  3,
				Height: 3,
			},
		},
		Monsters: map[string]data.StdMonster{},
	}
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	w := New(bundle, store)
	w.mu.Lock()
	defer w.mu.Unlock()
	placed := w.placeDropsLocked("tiny", 1, 1, 1, []GroundDrop{{ID: "drop-2", MapID: "tiny", ItemID: testHPItemID, Count: 1}}, storage.Character{MapID: "tiny", X: 1, Y: 1})
	if len(placed) != 1 {
		t.Fatalf("placeDropsLocked() placed %d drops, want 1", len(placed))
	}
	if placed[0].X == 1 && placed[0].Y == 1 {
		t.Fatalf("placeDropsLocked() placed drop on blocked tile %+v", placed[0])
	}
}

func TestDropCandidatesStartAtUpperLeftAndSweepClockwiseByRing(t *testing.T) {
	bundle := data.StdBundle{
		Maps: map[string]data.StdMap{
			"tiny": {
				ID:     "tiny",
				Name:   "tiny",
				Width:  5,
				Height: 5,
			},
		},
	}
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	w := New(bundle, store)
	w.mu.Lock()
	defer w.mu.Unlock()

	candidates := w.dropCandidatesLocked("tiny", 2, 2, 1, map[monsterPosition]struct{}{
		{MapID: "tiny", X: 2, Y: 2}: {},
	})
	want := []monsterPosition{
		{MapID: "tiny", X: 1, Y: 1},
		{MapID: "tiny", X: 2, Y: 1},
		{MapID: "tiny", X: 3, Y: 1},
		{MapID: "tiny", X: 3, Y: 2},
		{MapID: "tiny", X: 3, Y: 3},
		{MapID: "tiny", X: 2, Y: 3},
		{MapID: "tiny", X: 1, Y: 3},
		{MapID: "tiny", X: 1, Y: 2},
	}
	if !reflect.DeepEqual(candidates, want) {
		t.Fatalf("dropCandidatesLocked() = %+v, want %+v", candidates, want)
	}
}

func TestPlaceDropsUsesUpperLeftCandidateFirst(t *testing.T) {
	bundle := data.StdBundle{
		Items: map[string]data.StdItem{
			testHPItemID: {
				ID:   testHPItemID,
				Name: testHPItemID,
				Kind: "misc",
			},
		},
		Maps: map[string]data.StdMap{
			"tiny": {
				ID:     "tiny",
				Name:   "tiny",
				Width:  5,
				Height: 5,
			},
		},
		Monsters: map[string]data.StdMonster{},
	}
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	w := New(bundle, store)
	w.mu.Lock()
	placed := w.placeDropsLocked("tiny", 2, 2, 1, []GroundDrop{{ID: "drop-1", MapID: "tiny", ItemID: testHPItemID, Count: 1}})
	w.mu.Unlock()
	if len(placed) != 1 {
		t.Fatalf("placeDropsLocked() placed %d drops, want 1", len(placed))
	}
	if placed[0].X != 1 || placed[0].Y != 1 {
		t.Fatalf("placeDropsLocked() = %+v, want first drop at upper-left candidate (1,1)", placed[0])
	}
}

func TestPlaceDropsAbandonsWhenAllCandidatesAreAtLimit(t *testing.T) {
	bundle := data.StdBundle{
		Items: map[string]data.StdItem{
			testHPItemID: {
				ID:   testHPItemID,
				Name: testHPItemID,
				Kind: "misc",
			},
		},
		Maps: map[string]data.StdMap{
			"tiny": {
				ID:     "tiny",
				Name:   "tiny",
				Width:  3,
				Height: 3,
			},
		},
		Monsters: map[string]data.StdMonster{},
	}
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	gameplay := config.DefaultGameplay()
	gameplay.Item.FloorDropMaxStackPerTile = 2
	w := New(bundle, store, gameplay)
	w.mu.Lock()
	for _, cell := range []struct{ x, y int }{
		{0, 0}, {1, 0}, {2, 0},
		{0, 1}, {2, 1},
		{0, 2}, {1, 2}, {2, 2},
	} {
		for i := 0; i < 2; i++ {
			id := fmt.Sprintf("drop-%d-%d-%d", cell.x, cell.y, i)
			w.drops[id] = GroundDrop{
				ID:     id,
				MapID:  "tiny",
				X:      cell.x,
				Y:      cell.y,
				ItemID: testHPItemID,
				Count:  1,
			}
		}
	}
	placed := w.placeDropsLocked("tiny", 1, 1, 1, []GroundDrop{{ID: "drop-new", MapID: "tiny", ItemID: testHPItemID, Count: 1}})
	w.mu.Unlock()
	if len(placed) != 0 {
		t.Fatalf("placeDropsLocked() placed %d drops, want 0 when every candidate is already at limit", len(placed))
	}
}

func TestPlaceDropsUsesConfiguredLimitWhenStacking(t *testing.T) {
	bundle := data.StdBundle{
		Items: map[string]data.StdItem{
			testHPItemID: {
				ID:   testHPItemID,
				Name: testHPItemID,
				Kind: "misc",
			},
		},
		Maps: map[string]data.StdMap{
			"tiny": {
				ID:     "tiny",
				Name:   "tiny",
				Width:  3,
				Height: 3,
			},
		},
		Monsters: map[string]data.StdMonster{},
	}
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	gameplay := config.DefaultGameplay()
	gameplay.Item.FloorDropMaxStackPerTile = 2
	w := New(bundle, store, gameplay)
	w.mu.Lock()
	w.drops["drop-1"] = GroundDrop{ID: "drop-1", MapID: "tiny", X: 0, Y: 0, ItemID: testHPItemID, Count: 1}
	w.drops["drop-2"] = GroundDrop{ID: "drop-2", MapID: "tiny", X: 1, Y: 0, ItemID: testHPItemID, Count: 1}
	w.drops["drop-3"] = GroundDrop{ID: "drop-3", MapID: "tiny", X: 1, Y: 0, ItemID: testHPItemID, Count: 1}
	w.drops["drop-4"] = GroundDrop{ID: "drop-4", MapID: "tiny", X: 2, Y: 0, ItemID: testHPItemID, Count: 1}
	w.drops["drop-5"] = GroundDrop{ID: "drop-5", MapID: "tiny", X: 2, Y: 0, ItemID: testHPItemID, Count: 1}
	w.drops["drop-6"] = GroundDrop{ID: "drop-6", MapID: "tiny", X: 0, Y: 1, ItemID: testHPItemID, Count: 1}
	w.drops["drop-7"] = GroundDrop{ID: "drop-7", MapID: "tiny", X: 0, Y: 1, ItemID: testHPItemID, Count: 1}
	w.drops["drop-8"] = GroundDrop{ID: "drop-8", MapID: "tiny", X: 2, Y: 1, ItemID: testHPItemID, Count: 1}
	w.drops["drop-9"] = GroundDrop{ID: "drop-9", MapID: "tiny", X: 2, Y: 1, ItemID: testHPItemID, Count: 1}
	w.drops["drop-10"] = GroundDrop{ID: "drop-10", MapID: "tiny", X: 0, Y: 2, ItemID: testHPItemID, Count: 1}
	w.drops["drop-11"] = GroundDrop{ID: "drop-11", MapID: "tiny", X: 0, Y: 2, ItemID: testHPItemID, Count: 1}
	w.drops["drop-12"] = GroundDrop{ID: "drop-12", MapID: "tiny", X: 1, Y: 2, ItemID: testHPItemID, Count: 1}
	w.drops["drop-13"] = GroundDrop{ID: "drop-13", MapID: "tiny", X: 1, Y: 2, ItemID: testHPItemID, Count: 1}
	w.drops["drop-14"] = GroundDrop{ID: "drop-14", MapID: "tiny", X: 2, Y: 2, ItemID: testHPItemID, Count: 1}
	w.drops["drop-15"] = GroundDrop{ID: "drop-15", MapID: "tiny", X: 2, Y: 2, ItemID: testHPItemID, Count: 1}
	placed := w.placeDropsLocked("tiny", 1, 1, 1, []GroundDrop{{ID: "drop-new", MapID: "tiny", ItemID: testHPItemID, Count: 1}})
	w.mu.Unlock()
	if len(placed) != 1 {
		t.Fatalf("placeDropsLocked() placed %d drops, want 1", len(placed))
	}
	if placed[0].X != 0 || placed[0].Y != 0 {
		t.Fatalf("placeDropsLocked() = %+v, want first candidate at (0,0)", placed[0])
	}
}

func TestPlaceDropsAbandonsWhenNoCandidateCellsExist(t *testing.T) {
	bundle := data.StdBundle{
		Items: map[string]data.StdItem{
			testHPItemID: {
				ID:   testHPItemID,
				Name: testHPItemID,
				Kind: "misc",
			},
		},
		Maps: map[string]data.StdMap{
			"tiny": {
				ID:     "tiny",
				Name:   "tiny",
				Width:  1,
				Height: 1,
			},
		},
		Monsters: map[string]data.StdMonster{},
	}
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	w := New(bundle, store)
	w.mu.Lock()
	w.drops["drop-1"] = GroundDrop{ID: "drop-1", MapID: "tiny", X: 0, Y: 0, ItemID: testHPItemID, Count: 1}
	placed := w.placeDropsLocked("tiny", 0, 0, 1, []GroundDrop{{ID: "drop-2", MapID: "tiny", ItemID: testHPItemID, Count: 1}})
	w.mu.Unlock()
	if len(placed) != 0 {
		t.Fatalf("placeDropsLocked() placed %d drops, want 0 when only the origin is available", len(placed))
	}
}

func TestPickupAtReturnsItemToBag(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	ch.BagItems = []storage.UserItem{
		{ItemID: testHPItemID, MakeIndex: 1},
		{ItemID: testHPItemID, MakeIndex: 2},
	}
	ch, drop, err := w.DropItemCountByBagIndex(ch, 0, testHPItemID)
	if err != nil {
		t.Fatalf("DropItem() error = %v", err)
	}
	ch.X, ch.Y = drop.X, drop.Y
	ch, picked, err := w.PickupAt(ch, ch.X, ch.Y)
	if err != nil {
		t.Fatalf("PickupAt() error = %v", err)
	}
	if picked.ID != drop.ID {
		t.Fatalf("PickupAt() = %+v, want drop %s", picked, drop.ID)
	}
	found := false
	for _, entry := range ch.BagItems {
		if entry.ItemID == testHPItemID {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected %s back in bag, got %+v", testHPItemID, ch.BagItems)
	}
	if _, ok := w.drops[drop.ID]; ok {
		t.Fatalf("drop %s was not removed from ground", drop.ID)
	}
}

func TestUseWeaponDoesNotAutoEquipItem(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	ch.BagItems = []storage.UserItem{{ItemID: testWeaponID}}
	if _, _, err := w.UseItemByBagIndex(ch, 0); err == nil {
		t.Fatalf("UseItem() expected error for wearable item")
	}
}

func TestUseSkillBookLearnsBasicSwordSkill(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	ch.Class = "warrior"
	ch.Level = 7
	ch.BagItems = []storage.UserItem{{ItemID: "基本剑术", MakeIndex: 1}}

	updated, result, err := w.UseItemByBagIndex(ch, 1)
	if err != nil {
		t.Fatalf("UseItemByBagIndex() error = %v", err)
	}
	if !result.SkillChanged {
		t.Fatalf("SkillChanged = false, want true")
	}
	if !updated.Skills.Has("基本剑术") {
		t.Fatalf("skills = %+v, want learned 基本剑术", updated.Skills)
	}
	if countBagItems(updated.BagItems) != 0 {
		t.Fatalf("bag = %+v, want consumed skill book", updated.BagItems)
	}
}

func TestUseItemConsumesSlowPotionAndQueuesRecovery(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	ch.HP = 10
	ch.MaxHP = 100
	ch.MP = 5
	ch.MaxMP = 40
	ch.BagItems = []storage.UserItem{
		{ItemID: testHPItemID, MakeIndex: 1},
		{ItemID: testHPItemID, MakeIndex: 2},
	}
	updated, _, err := w.UseItemByBagIndex(ch, 1)
	if err != nil {
		t.Fatalf("UseItem() error = %v", err)
	}
	if updated.HP != 10 {
		t.Fatalf("HP = %d, want 10 before tick recovery", updated.HP)
	}
	if updated.IncHealth != 20 || updated.IncSpell != 0 {
		t.Fatalf("queued recovery = %d/%d, want 20/0", updated.IncHealth, updated.IncSpell)
	}
	if countBagItems(updated.BagItems) != 1 {
		t.Fatalf("bag = %+v, want one remaining potion", updated.BagItems)
	}
	if updated.BagItems[0].ItemID != testHPItemID {
		t.Fatalf("bag = %+v, want remaining potion to stay in order", updated.BagItems)
	}
}

func TestUseItemByBagIndexConsumesPotionAndRestoresHealth(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	ch.HP = 10
	ch.MaxHP = 100
	ch.MP = 5
	ch.MaxMP = 40
	ch.BagItems = []storage.UserItem{{ItemID: testInstantHPItemID, MakeIndex: 200}}
	updated, _, err := w.UseItemByBagIndex(ch, 200)
	if err != nil {
		t.Fatalf("UseItemByBagIndex() error = %v", err)
	}
	if updated.HP != 40 {
		t.Fatalf("HP = %d, want 40", updated.HP)
	}
	if updated.MP != 40 {
		t.Fatalf("MP = %d, want 40", updated.MP)
	}
	if countBagItems(updated.BagItems) != 0 {
		t.Fatalf("bag = %+v, want empty after potion use", updated.BagItems)
	}
}

func TestTickAppliesQueuedPotionRecoveryAndClearsAtMax(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	ch.HP = 10
	ch.MaxHP = 100
	ch.MP = 5
	ch.MaxMP = 40
	ch.IncHealth = 20
	ch.IncSpell = 30
	ch.IncHealthSpellAt = time.Unix(9, 0).UnixMilli()
	result, err := w.Tick([]PlayerSnapshot{{Character: ch}}, time.Unix(10, 0))
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if len(result.Characters) != 1 {
		t.Fatalf("Characters = %+v, want one updated character", result.Characters)
	}
	updated := result.Characters[0]
	if updated.HP != 15 || updated.MP != 10 {
		t.Fatalf("HP/MP = %d/%d, want 15/10 after one recovery tick", updated.HP, updated.MP)
	}
	if updated.IncHealth != 15 || updated.IncSpell != 25 {
		t.Fatalf("queued recovery = %d/%d, want 15/25 after one tick", updated.IncHealth, updated.IncSpell)
	}
	if updated.IncHealthSpellAt == 0 {
		t.Fatalf("expected recovery tick timestamp to be set")
	}
	updated.HP = 99
	updated.MaxHP = 100
	updated.MP = 39
	updated.MaxMP = 40
	updated.IncHealth = 20
	updated.IncSpell = 20
	result, err = w.Tick([]PlayerSnapshot{{Character: updated}}, time.Unix(11, 0))
	if err != nil {
		t.Fatalf("second Tick() error = %v", err)
	}
	if len(result.Characters) != 1 {
		t.Fatalf("Characters = %+v, want one updated character on second tick", result.Characters)
	}
	updated = result.Characters[0]
	if updated.HP != 100 || updated.MP != 40 {
		t.Fatalf("HP/MP = %d/%d, want capped at full on second tick", updated.HP, updated.MP)
	}
	if updated.IncHealth != 0 || updated.IncSpell != 0 {
		t.Fatalf("queued recovery = %d/%d, want cleared at max HP/MP", updated.IncHealth, updated.IncSpell)
	}
}

func TestUseItemShape3UsesPercentBasedRecovery(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	w.data.Items["percent-drug"] = data.StdItem{
		ID:      "percent-drug",
		Name:    "percent-drug",
		Kind:    "consumable",
		StdMode: 0,
		Shape:   3,
		Stats: data.StdItemStats{
			AcMin:  20,
			MacMin: 30,
		},
	}
	ch.HP = 10
	ch.MaxHP = 100
	ch.MP = 5
	ch.MaxMP = 40
	ch.BagItems = []storage.UserItem{{ItemID: "percent-drug", MakeIndex: 400}}
	updated, _, err := w.UseItemByBagIndex(ch, 400)
	if err != nil {
		t.Fatalf("UseItemByBagIndex() error = %v", err)
	}
	if updated.HP != 30 {
		t.Fatalf("HP = %d, want 30", updated.HP)
	}
	if updated.MP != 17 {
		t.Fatalf("MP = %d, want 17", updated.MP)
	}
}

func TestUseItemShape12AppliesTemporaryBonuses(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	w.data.Items["apple-like"] = data.StdItem{
		ID:      "apple-like",
		Name:    "apple-like",
		Kind:    "consumable",
		StdMode: 3,
		Shape:   12,
		Stats: data.StdItemStats{
			DcMin:  10,
			DcMax:  1,
			McMin:  20,
			ScMin:  30,
			AcMin:  200,
			AcMax:  2,
			MacMin: 200,
			MacMax: 240,
		},
	}
	ch.HP = 10
	ch.MaxHP = 100
	ch.MP = 5
	ch.MaxMP = 40
	ch.BagItems = []storage.UserItem{{ItemID: "apple-like", MakeIndex: 401}}
	updated, result, err := w.UseItemByBagIndex(ch, 401)
	if err != nil {
		t.Fatalf("UseItemByBagIndex() error = %v", err)
	}
	if !result.AbilityChanged {
		t.Fatal("AbilityChanged = false, want true")
	}
	if updated.HP != 10 || updated.MP != 5 {
		t.Fatalf("HP/MP = %d/%d, want unchanged current values", updated.HP, updated.MP)
	}
	if got := updated.ExtraAbil[0]; got != 10 {
		t.Fatalf("ExtraAbil[0] = %d, want 10", got)
	}
	if got := updated.ExtraAbil[1]; got != 20 {
		t.Fatalf("ExtraAbil[1] = %d, want 20", got)
	}
	if got := updated.ExtraAbil[2]; got != 30 {
		t.Fatalf("ExtraAbil[2] = %d, want 30", got)
	}
	if got := updated.ExtraAbil[3]; got != 2 {
		t.Fatalf("ExtraAbil[3] = %d, want 2", got)
	}
	if got := updated.ExtraAbil[4]; got != 200 {
		t.Fatalf("ExtraAbil[4] = %d, want 200", got)
	}
	if got := updated.ExtraAbil[5]; got != 200 {
		t.Fatalf("ExtraAbil[5] = %d, want 200", got)
	}
	if countBagItems(updated.BagItems) != 0 {
		t.Fatalf("bag = %+v, want empty after shape12 use", updated.BagItems)
	}
	abilities := w.Abilities(updated)
	base := Base(updated.Class, updated.Level)
	if abilities.MaxHP != base.MaxHP+200 || abilities.MaxMP != base.MaxMP+200 {
		t.Fatalf("abilities max hp/mp = %d/%d, want %d/%d", abilities.MaxHP, abilities.MaxMP, base.MaxHP+200, base.MaxMP+200)
	}
}

func TestUseItemShape13AddsExperience(t *testing.T) {
	bundle := loadTestBundle(t)
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	gameplay := config.DefaultGameplay()
	gameplay.Progression.RequiredExperiencePerLevel = 1000
	w := New(bundle, store, gameplay)
	mapID, x, y := defaultSpawn(bundle)
	ch, err := w.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	w.data.Items["exp-drug"] = data.StdItem{
		ID:      "exp-drug",
		Name:    "exp-drug",
		Kind:    "consumable",
		StdMode: 3,
		Shape:   13,
		DuraMax: 75,
	}
	ch.BagItems = []storage.UserItem{{ItemID: "exp-drug", MakeIndex: 402}}
	updated, result, err := w.UseItemByBagIndex(ch, 402)
	if err != nil {
		t.Fatalf("UseItemByBagIndex() error = %v", err)
	}
	if result.Experience != 75 {
		t.Fatalf("result.Experience = %d, want 75", result.Experience)
	}
	if updated.Experience != 75 {
		t.Fatalf("Experience = %d, want 75", updated.Experience)
	}
	if result.LevelUp {
		t.Fatal("LevelUp = true, want false with high threshold")
	}
}

func TestCastSkillLevelsUpWhenTrainThresholdReached(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	w.mu.Lock()
	w.monsters = map[string]*Monster{}
	w.occupied = map[monsterPosition]string{}
	w.mu.Unlock()
	mapID := caster.MapID
	targetX, targetY := caster.X+1, caster.Y
	if !w.data.Maps[mapID].Walkable(targetX, targetY) {
		t.Fatal("expected walkable tile for skill level-up test")
	}
	result, err := w.SpawnMonsterByNameAt(mapID, targetX, targetY, "鸡", 1)
	if err != nil {
		t.Fatalf("SpawnMonsterByNameAt() error = %v", err)
	}
	if len(result.Monsters) != 1 {
		t.Fatalf("SpawnMonsterByNameAt() monsters = %d, want 1", len(result.Monsters))
	}
	caster.Skills = storage.SkillStates{{ID: "火球术", Level: 0, Train: 99}}
	caster.Level = 7
	caster.MP = 100
	updated, err := w.CastSkillWithPlayers(caster, "火球术", targetX, targetY, MonsterActorID(result.Monsters[0]), nil)
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	if len(updated.Character.Skills) != 1 {
		t.Fatalf("Skills = %+v, want 1 entry", updated.Character.Skills)
	}
	if updated.Character.Skills[0].Level != 1 {
		t.Fatalf("skill level = %d, want 1 after threshold", updated.Character.Skills[0].Level)
	}
	if got := updated.Character.Skills[0].Train; got < 0 || got > 2 {
		t.Fatalf("skill train = %d, want 0..2 after level up", got)
	}
	var skillExpEvent *SpellEvent
	for i := range updated.Events {
		if updated.Events[i].Kind == SpellEventSkillExp {
			skillExpEvent = &updated.Events[i]
			break
		}
	}
	if skillExpEvent == nil {
		t.Fatal("skill exp event = nil, want delayed message after level-up")
	}
	if skillExpEvent.SkillExpDelay != 800*time.Millisecond {
		t.Fatalf("skill exp delay = %s, want 800ms after level-up", skillExpEvent.SkillExpDelay)
	}
}

func TestCastSkillGroupHealingHealsGroupMembers(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	mapID, x, y := caster.MapID, caster.X, caster.Y
	friend, err := w.CreateCharacterWithAppearance("test", "friend", "wizard", 0, 0, mapID, x+1, y)
	if err != nil {
		t.Fatalf("CreateCharacter() friend error = %v", err)
	}
	caster.AllowGroup = true
	friend.AllowGroup = true
	caster, friend, err = w.CreateGroup(caster, friend, 2)
	if err != nil {
		t.Fatalf("CreateGroup() error = %v", err)
	}
	caster.Skills = storage.SkillStates{{ID: "群体治疗术", Level: 0, Train: 0}}
	caster.HP = 20
	caster.MaxHP = 100
	cost := w.SpellCost(w.data.Skills["群体治疗术"], caster.Skills[0])
	caster.MP = cost + 50
	friend.HP = 30
	friend.MaxHP = 100
	players := []storage.Character{friend}
	result, err := w.CastSkillWithPlayers(caster, "群体治疗术", x, y, 0, players)
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	if result.SkillID != "群体治疗术" {
		t.Fatalf("SkillID = %q, want 群体治疗术", result.SkillID)
	}
	if len(w.pendingSpells) != 2 {
		t.Fatalf("pending spells = %d, want 2", len(w.pendingSpells))
	}
	if result.Character.HP != 20 {
		t.Fatalf("caster HP = %d, want unchanged before recovery tick", result.Character.HP)
	}
	if result.Character.MP != 50 {
		t.Fatalf("caster MP = %d, want 50 after paying spell cost", result.Character.MP)
	}
	tick, err := w.Tick([]PlayerSnapshot{{Character: caster}, {Character: friend}}, time.Now().Add(time.Second))
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	for _, ch := range tick.Characters {
		if ch.ID == caster.ID && (ch.HP <= caster.HP || ch.IncHealing != 0) {
			t.Fatalf("caster recovery result = %+v, want same-tick recovery and empty queue", ch)
		}
		if ch.ID == friend.ID && (ch.HP <= friend.HP || ch.IncHealing != 0) {
			t.Fatalf("friend recovery result = %+v, want same-tick recovery and empty queue", ch)
		}
	}
}

func TestCastSkillHealingKeepsInvalidCharacterTargetForMagicFire(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	target, err := w.CreateCharacterWithAppearance("test", "target", "wizard", 0, 0, caster.MapID, caster.X+1, caster.Y)
	if err != nil {
		t.Fatalf("CreateCharacter() target error = %v", err)
	}
	caster.Skills = storage.SkillStates{{ID: "治愈术", Level: 0, Train: 0}}
	caster.AttackMode = 2
	caster.MP = 50
	players := []storage.Character{target}
	result, err := w.DoSpell(caster, "治愈术", target.X, target.Y, CharacterActorID(target), players)
	if err != nil {
		t.Fatalf("DoSpell() error = %v", err)
	}
	if len(w.pendingSpells) != 0 {
		t.Fatalf("pending spells = %d, want 0 for invalid friend target", len(w.pendingSpells))
	}
	if result.MagicTargetID != CharacterActorID(target) {
		t.Fatalf("MagicTargetID = %d, want %d", result.MagicTargetID, CharacterActorID(target))
	}
}

func TestCastSkillHealingDoesNotRollInvalidTarget(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	target, err := w.CreateCharacterWithAppearance("test", "target", "wizard", 0, 0, caster.MapID, caster.X+1, caster.Y)
	if err != nil {
		t.Fatalf("CreateCharacter() target error = %v", err)
	}
	caster.Skills = storage.SkillStates{{ID: "治愈术", Level: 0, Train: 0}}
	caster.AttackMode = 2
	caster.MP = 50
	rolls := &seqSource{vals: []int64{1}}
	w.rand = rand.New(rolls)
	if _, err := w.DoSpell(caster, "治愈术", target.X, target.Y, CharacterActorID(target), []storage.Character{target}); err != nil {
		t.Fatalf("DoSpell() error = %v", err)
	}
	if rolls.idx != 0 {
		t.Fatalf("random calls = %d, want none for invalid friend target", rolls.idx)
	}
}

func TestCastSkillHealingFallsBackFromDeadExplicitFriendTarget(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	target, err := w.CreateCharacterWithAppearance("test", "dead-target", "wizard", 0, 0, caster.MapID, caster.X+1, caster.Y)
	if err != nil {
		t.Fatalf("CreateCharacter() target error = %v", err)
	}
	target.HP = 0
	target.MaxHP = 100
	caster.Skills = storage.SkillStates{{ID: "治愈术", Level: 0, Train: 0}}
	caster.MP = 50
	result, err := w.DoSpell(caster, "治愈术", target.X, target.Y, CharacterActorID(target), []storage.Character{target})
	if err != nil {
		t.Fatalf("DoSpell() error = %v", err)
	}
	if result.MagicTargetID != CharacterActorID(caster) {
		t.Fatalf("MagicTargetID = %d, want caster %d", result.MagicTargetID, CharacterActorID(caster))
	}
	if len(w.pendingSpells) != 0 {
		t.Fatalf("pending spells = %+v, want no healing for full-health caster", w.pendingSpells)
	}
}

func TestCastSkillHealingFallsBackFromDeadExplicitFriendlySummon(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	caster.Skills = storage.SkillStates{{ID: "治愈术", Level: 0, Train: 0}}
	caster.MP = 50
	mon := &Monster{ID: "dead-summon", MapID: caster.MapID, X: caster.X + 1, Y: caster.Y, HP: 0, MaxHP: 100, Alive: false, MasterID: caster.ID, Race: 50}
	w.mu.Lock()
	w.monsters[mon.ID] = mon
	w.occupied[monsterPosition{MapID: mon.MapID, X: mon.X, Y: mon.Y}] = mon.ID
	w.mu.Unlock()
	result, err := w.DoSpell(caster, "治愈术", mon.X, mon.Y, MonsterActorID(*mon), nil)
	if err != nil {
		t.Fatalf("DoSpell() error = %v", err)
	}
	if result.MagicTargetID != CharacterActorID(caster) {
		t.Fatalf("MagicTargetID = %d, want caster %d", result.MagicTargetID, CharacterActorID(caster))
	}
	if len(w.pendingSpells) != 0 {
		t.Fatalf("pending spells = %+v, want no healing for full-health caster", w.pendingSpells)
	}
}

func TestCastSkillGroupHealingHealsFriendlySummon(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	caster.EquippedItems = map[int]storage.UserItem{SlotBujuk: {ItemID: "护身符", Dura: 10000}}
	caster.Dir = 2
	caster.HP = 20
	caster.MaxHP = 100
	caster.Skills = storage.SkillStates{{ID: "召唤骷髅", Level: 0, Train: 0}, {ID: "群体治疗术", Level: 0, Train: 0}}
	cost := w.SpellCost(w.data.Skills["群体治疗术"], caster.Skills[1])
	caster.MP = cost + 100
	caster.EquippedItems = map[int]storage.UserItem{SlotBujuk: {ItemID: "护身符", Dura: 10000}}
	summonedResult, err := w.CastSkillWithPlayers(caster, "召唤骷髅", caster.X, caster.Y, 0, nil)
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() summon error = %v", err)
	}
	summoned := summonedResult.SummonedMonsters[0]
	w.mu.Lock()
	if mon, ok := w.monsters[summoned.ID]; ok {
		mon.HP = 5
	}
	w.mu.Unlock()
	updated, err := w.CastSkillWithPlayers(summonedResult.Character, "群体治疗术", caster.X, caster.Y, 0, []storage.Character{summonedResult.Character})
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() group heal error = %v", err)
	}
	if updated.Character.HP != 20 {
		t.Fatalf("caster HP = %d, want unchanged before recovery tick", updated.Character.HP)
	}
	if len(w.pendingSpells) < 1 {
		t.Fatalf("pending spells = %d, want summon healing pending", len(w.pendingSpells))
	}
	_, err = w.Tick([]PlayerSnapshot{{Character: updated.Character}}, time.Now().Add(time.Second))
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	w.mu.Lock()
	gotHP := 0
	if mon, ok := w.monsters[summoned.ID]; ok {
		gotHP = mon.HP
	}
	w.mu.Unlock()
	if gotHP <= 5 {
		t.Fatalf("summoned HP = %d, want healed after recovery tick", gotHP)
	}
}

func TestCastSkillGroupHealingSucceedsOnFullHealthFriendlyTargets(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	friend, err := w.CreateCharacterWithAppearance("test", "friend", "wizard", 0, 0, caster.MapID, caster.X+1, caster.Y)
	if err != nil {
		t.Fatalf("CreateCharacter() friend error = %v", err)
	}
	caster.AllowGroup = true
	friend.AllowGroup = true
	caster, friend, err = w.CreateGroup(caster, friend, 2)
	if err != nil {
		t.Fatalf("CreateGroup() error = %v", err)
	}
	caster.HP = caster.MaxHP
	friend.HP = friend.MaxHP
	caster.Skills = storage.SkillStates{{ID: "群体治疗术", Level: 0, Train: 0}}
	cost := w.SpellCost(w.data.Skills["群体治疗术"], caster.Skills[0])
	caster.MP = cost + 100
	updated, err := w.CastSkillWithPlayers(caster, "群体治疗术", caster.X, caster.Y, 0, []storage.Character{caster, friend})
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	if updated.SkillID != "群体治疗术" {
		t.Fatalf("SkillID = %q, want 群体治疗术", updated.SkillID)
	}
	if len(updated.AffectedCharacters) != 0 || len(updated.AffectedMonsters) != 0 {
		t.Fatalf("affected targets = chars:%d mons:%d, want none on full health", len(updated.AffectedCharacters), len(updated.AffectedMonsters))
	}
	if updated.Character.MP != 100 {
		t.Fatalf("caster MP = %d, want 100 after paying spell cost", updated.Character.MP)
	}
}

func TestCastSkillGroupHealingReturnsFriendlySummonInAffectedMonsters(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	caster.EquippedItems = map[int]storage.UserItem{SlotBujuk: {ItemID: "护身符", Dura: 10000}}
	caster.Dir = 2
	caster.MP = 100
	caster.HP = 20
	caster.MaxHP = 100
	caster.Skills = storage.SkillStates{{ID: "召唤骷髅", Level: 0, Train: 0}, {ID: "群体治疗术", Level: 0, Train: 0}}
	caster.EquippedItems = map[int]storage.UserItem{SlotBujuk: {ItemID: "护身符", Dura: 10000}}
	summonedResult, err := w.CastSkillWithPlayers(caster, "召唤骷髅", caster.X, caster.Y, 0, nil)
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() summon error = %v", err)
	}
	summoned := summonedResult.SummonedMonsters[0]
	w.mu.Lock()
	if mon, ok := w.monsters[summoned.ID]; ok {
		mon.HP = 5
	}
	w.mu.Unlock()
	updated, err := w.CastSkillWithPlayers(summonedResult.Character, "群体治疗术", caster.X, caster.Y, 0, []storage.Character{summonedResult.Character})
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() group heal error = %v", err)
	}
	tick, err := w.Tick([]PlayerSnapshot{{Character: updated.Character}}, time.Now().Add(time.Second))
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if len(tick.AffectedMonsters) != 1 {
		t.Fatalf("AffectedMonsters = %d, want 1 after delay", len(tick.AffectedMonsters))
	}
	if tick.AffectedMonsters[0].ID != summoned.ID {
		t.Fatalf("AffectedMonsters[0].ID = %q, want %q", tick.AffectedMonsters[0].ID, summoned.ID)
	}
}

func TestCastSkillGroupHealingSkipsFullFriendlySummon(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	caster.MP = 100
	caster.Skills = storage.SkillStates{{ID: "群体治疗术", Level: 0, Train: 0}}
	spawn, err := w.SpawnMonsterByNameAt(caster.MapID, caster.X+1, caster.Y, "骷髅", 1)
	if err != nil {
		t.Fatalf("SpawnMonsterByNameAt() error = %v", err)
	}
	monID := spawn.Monsters[0].ID
	w.mu.Lock()
	mon := w.monsters[monID]
	mon.MasterID = caster.ID
	mon.HP = mon.MaxHP
	w.mu.Unlock()
	updated, err := w.CastSkillWithPlayers(caster, "群体治疗术", caster.X, caster.Y, 0, []storage.Character{caster})
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	for _, pending := range w.pendingSpells {
		if pending.TargetMonsterID == monID {
			t.Fatalf("full summon has pending healing: %+v", pending)
		}
	}
	if updated.SkillID != "群体治疗术" {
		t.Fatalf("SkillID = %q, want 群体治疗术", updated.SkillID)
	}
}

func TestCastSkillGroupHealingOrdersFriendlySummonsByRadius(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	caster.MP = 100
	caster.HP = 20
	caster.MaxHP = 100
	caster.Skills = storage.SkillStates{{ID: "群体治疗术", Level: 0, Train: 0}}
	firstSpawn, err := w.SpawnMonsterByNameAt(caster.MapID, caster.X+1, caster.Y, "骷髅", 1)
	if err != nil {
		t.Fatalf("SpawnMonsterByNameAt() first summon error = %v", err)
	}
	secondSpawn, err := w.SpawnMonsterByNameAt(caster.MapID, caster.X, caster.Y+1, "神兽", 1)
	if err != nil {
		t.Fatalf("SpawnMonsterByNameAt() second summon error = %v", err)
	}
	first := firstSpawn.Monsters[0]
	second := secondSpawn.Monsters[0]
	w.mu.Lock()
	if mon := w.monsters[first.ID]; mon != nil {
		mon.MasterID = caster.ID
		mon.MasterExpiresAt = time.Now().Add(time.Hour)
		mon.HP = 5
	}
	if mon := w.monsters[second.ID]; mon != nil {
		mon.MasterID = caster.ID
		mon.MasterExpiresAt = time.Now().Add(time.Hour)
		mon.HP = 5
	}
	w.mu.Unlock()
	updated, err := w.CastSkillWithPlayers(caster, "群体治疗术", caster.X, caster.Y, 0, []storage.Character{caster})
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() group heal error = %v", err)
	}
	tick, err := w.Tick([]PlayerSnapshot{{Character: updated.Character}}, time.Now().Add(time.Second))
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if len(tick.AffectedMonsters) != 2 {
		t.Fatalf("AffectedMonsters = %d, want 2 after delay", len(tick.AffectedMonsters))
	}
	if tick.AffectedMonsters[0].ID != second.ID || tick.AffectedMonsters[1].ID != first.ID {
		t.Fatalf("affected monster order = [%s %s], want [%s %s]", tick.AffectedMonsters[0].ID, tick.AffectedMonsters[1].ID, second.ID, first.ID)
	}
}

func TestQueuedHealingUsesMonsterRecoveryAccumulator(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	caster.MP = 100
	caster.Skills = storage.SkillStates{{ID: "群体治疗术", Level: 0, Train: 0}}
	spawn, err := w.SpawnMonsterByNameAt(caster.MapID, caster.X+1, caster.Y, "骷髅", 1)
	if err != nil {
		t.Fatalf("SpawnMonsterByNameAt() error = %v", err)
	}
	monID := spawn.Monsters[0].ID
	w.mu.Lock()
	w.monsters[monID].MasterID = caster.ID
	w.monsters[monID].HP = 1
	w.mu.Unlock()
	updated, err := w.CastSkillWithPlayers(caster, "群体治疗术", caster.X, caster.Y, 0, []storage.Character{caster})
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	w.mu.Lock()
	queued := *w.monsters[monID]
	w.mu.Unlock()
	if queued.HP != 1 || queued.IncHealing != 0 {
		t.Fatalf("queued monster = HP %d, IncHealing %d, want 1/0 before RM_MAGHEALING", queued.HP, queued.IncHealing)
	}
	heal := w.pendingSpells[0].Healing
	result, err := w.Tick([]PlayerSnapshot{{Character: updated.Character}}, time.Now().Add(time.Second))
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	wantHP := 1 + heal
	if wantHP > 6 {
		wantHP = 6
	}
	if len(result.AffectedMonsters) != 1 || result.AffectedMonsters[0].HP != wantHP || result.AffectedMonsters[0].IncHealing != 0 {
		t.Fatalf("healed monster = %+v, want HP %d and empty healing queue", result.AffectedMonsters, wantHP)
	}
}

func TestMonsterHealingClearsQueuedHealingWhenAlreadyFull(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	caster.MP = 100
	caster.Skills = storage.SkillStates{{ID: "群体治疗术", Level: 0, Train: 0}}
	spawn, err := w.SpawnMonsterByNameAt(caster.MapID, caster.X+1, caster.Y, "骷髅", 1)
	if err != nil {
		t.Fatalf("SpawnMonsterByNameAt() error = %v", err)
	}
	monID := spawn.Monsters[0].ID
	w.mu.Lock()
	mon := w.monsters[monID]
	mon.MasterID = caster.ID
	mon.HP = 1
	mon.IncHealthSpellAt = time.Now().Add(-time.Second).UnixMilli()
	w.mu.Unlock()
	updated, err := w.CastSkillWithPlayers(caster, "群体治疗术", caster.X, caster.Y, 0, []storage.Character{caster})
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	w.mu.Lock()
	w.monsters[monID].HP = w.monsters[monID].MaxHP
	w.mu.Unlock()
	if _, err := w.Tick([]PlayerSnapshot{{Character: updated.Character}}, time.Now().Add(time.Second)); err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if got := w.monsters[monID].IncHealing; got != 0 {
		t.Fatalf("full monster healing queue = %d, want 0", got)
	}
}

func TestQueuedHealingConsumesAfterMonsterDiesLikeReference(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	caster.MP = 100
	caster.Skills = storage.SkillStates{{ID: "群体治疗术", Level: 0, Train: 0}}
	spawn, err := w.SpawnMonsterByNameAt(caster.MapID, caster.X+1, caster.Y, "骷髅", 1)
	if err != nil {
		t.Fatalf("SpawnMonsterByNameAt() error = %v", err)
	}
	monID := spawn.Monsters[0].ID
	w.mu.Lock()
	w.monsters[monID].MasterID = caster.ID
	w.monsters[monID].HP = 1
	w.mu.Unlock()
	updated, err := w.CastSkillWithPlayers(caster, "群体治疗术", caster.X, caster.Y, 0, []storage.Character{caster})
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	w.mu.Lock()
	w.monsters[monID].Alive = false
	w.monsters[monID].PerHealing = 1
	w.mu.Unlock()
	w.mu.Lock()
	players := map[string]storage.Character{updated.Character.ID: updated.Character}
	result := TickResult{}
	w.applyPendingSpellTicksLocked(&result, players, map[string]storage.Character{}, time.Now().Add(time.Second))
	queued := w.monsters[monID].IncHealing
	perHealing := w.monsters[monID].PerHealing
	w.mu.Unlock()
	if queued <= 0 {
		t.Fatalf("dead monster healing queue = %d, want delayed RM_MAGHEALING consumption", queued)
	}
	if perHealing != 5 {
		t.Fatalf("dead monster per-healing = %d, want RM_MAGHEALING reset to 5", perHealing)
	}
}

func TestGroupHealingSkipsDeadFriendlyMonsterLikeReference(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	caster.MP = 100
	caster.Skills = storage.SkillStates{{ID: "群体治疗术", Level: 0, Train: 0}}
	spawn, err := w.SpawnMonsterByNameAt(caster.MapID, caster.X+1, caster.Y, "骷髅", 1)
	if err != nil {
		t.Fatalf("SpawnMonsterByNameAt() error = %v", err)
	}
	monID := spawn.Monsters[0].ID
	w.mu.Lock()
	w.monsters[monID].MasterID = caster.ID
	w.monsters[monID].HP = 0
	w.monsters[monID].Alive = false
	w.mu.Unlock()
	if _, err := w.CastSkillWithPlayers(caster, "群体治疗术", caster.X, caster.Y, 0, []storage.Character{caster}); err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.pendingSpells) != 0 {
		t.Fatalf("pending healing = %+v, want no entry for dead friendly monster", w.pendingSpells)
	}
}

func TestCharacterNameColorMatchesReferencePKPriority(t *testing.T) {
	w := &World{}
	tests := []struct {
		name  string
		ch    storage.Character
		color uint16
	}{
		{name: "default", color: 255},
		{name: "pk flag", ch: storage.Character{PKFlag: true}, color: 0x2F},
		{name: "pk level one", ch: storage.Character{PKPoint: 100, PKFlag: true}, color: 0xFB},
		{name: "pk level two", ch: storage.Character{PKPoint: 200, PKFlag: true}, color: 0xF9},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := w.CharacterNameColor(tt.ch); got != tt.color {
				t.Fatalf("CharacterNameColor() = %#x, want %#x", got, tt.color)
			}
		})
	}
}

func TestCharacterNameColorForPreservesReferenceRelationOverrides(t *testing.T) {
	w := &World{}
	target := storage.Character{PKFlag: true, GuildID: "guild"}
	if got := w.CharacterNameColorFor(storage.Character{GuildID: "guild"}, target); got != 0xB4 {
		t.Fatalf("same-guild color = %#x, want %#x", got, 0xB4)
	}
	if got := w.CharacterNameColorFor(storage.Character{GuildWarArea: true, GuildAllianceID: "alliance"}, storage.Character{GuildWarArea: true, GuildAllianceID: "alliance"}); got != 0xB4 {
		t.Fatalf("allied war color = %#x, want %#x", got, 0xB4)
	}
	if got := w.CharacterNameColorFor(storage.Character{GuildID: "observer", GuildWarArea: true}, storage.Character{GuildID: "target", GuildWarArea: true}); got != 0x45 {
		t.Fatalf("enemy war color = %#x, want %#x", got, 0x45)
	}
	if got := w.CharacterNameColorFor(storage.Character{GuildWarArea: true, FreePKArea: true}, storage.Character{GuildWarArea: true, FreePKArea: true}); got != 0xDD {
		t.Fatalf("free PK war color = %#x, want %#x", got, 0xDD)
	}
	if got := w.CharacterNameColorFor(storage.Character{GuildID: "guild"}, storage.Character{GuildID: "guild", PKPoint: 200}); got != 0xF9 {
		t.Fatalf("red PK color = %#x, want %#x", got, 0xF9)
	}
}

func TestWorldTickExpiresPKFlagAndReportsNameColorChange(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	ch.PKFlag = true
	ch.PKFlagUntil = time.Now().Add(-time.Second).UnixNano()
	tick, err := w.Tick([]PlayerSnapshot{{Character: ch}}, time.Now())
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if len(tick.NameColorCharacters) != 1 || tick.NameColorCharacters[0].ID != ch.ID {
		t.Fatalf("NameColorCharacters = %+v, want expired character %q", tick.NameColorCharacters, ch.ID)
	}
	if tick.NameColorCharacters[0].PKFlag || tick.NameColorCharacters[0].PKFlagUntil != 0 {
		t.Fatalf("expired character = %+v, want cleared PK flag", tick.NameColorCharacters[0])
	}
}

func TestWorldTickExpiresCharacterLastHitter(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	now := time.Now()
	ch.LastHitterID = "attacker"
	ch.LastHitterAt = now.Add(-31 * time.Second).UnixNano()
	tick, err := w.Tick([]PlayerSnapshot{{Character: ch}}, now)
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if len(tick.Characters) != 1 || tick.Characters[0].LastHitterID != "" || tick.Characters[0].LastHitterAt != 0 {
		t.Fatalf("expired character = %+v, want cleared last hitter", tick.Characters)
	}
}

func TestWorldTickClearsCharacterLastHitterWhenAttackerDies(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	now := time.Now()
	ch.LastHitterID = "attacker"
	ch.LastHitterAt = now.UnixNano()
	attacker := storage.Character{ID: "attacker", MapID: ch.MapID, HP: 0}
	tick, err := w.Tick([]PlayerSnapshot{{Character: ch}, {Character: attacker}}, now)
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	found := false
	for _, updated := range tick.Characters {
		if updated.ID == ch.ID && (updated.LastHitterID != "" || updated.LastHitterAt != 0) {
			t.Fatalf("character last hitter = %+v, want cleared after attacker death", updated)
		}
		if updated.ID == ch.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("cleared character %q missing from tick updates", ch.ID)
	}
}

func TestWorldTickClearsCharacterLastHitterWhenMonsterDies(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	spawn, err := w.SpawnMonsterByNameAt(ch.MapID, ch.X+1, ch.Y, "骷髅", 1)
	if err != nil || len(spawn.Monsters) != 1 {
		t.Fatalf("SpawnMonsterByNameAt() = %+v, error = %v", spawn, err)
	}
	monsterID := spawn.Monsters[0].ID
	now := time.Now()
	ch.LastHitterID = monsterID
	ch.LastHitterAt = now.UnixNano()
	w.mu.Lock()
	w.monsters[monsterID].HP = 0
	w.monsters[monsterID].Alive = false
	w.mu.Unlock()
	tick, err := w.Tick([]PlayerSnapshot{{Character: ch}}, now)
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	for _, updated := range tick.Characters {
		if updated.ID == ch.ID {
			if updated.LastHitterID != "" || updated.LastHitterAt != 0 {
				t.Fatalf("character last hitter = %+v, want cleared after monster death", updated)
			}
			return
		}
	}
	t.Fatalf("cleared character %q missing from tick updates", ch.ID)
}

func TestWorldTickExpiresMonsterHitterLifetimes(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	spawn, err := w.SpawnMonsterByNameAt(ch.MapID, ch.X+1, ch.Y, "骷髅", 1)
	if err != nil || len(spawn.Monsters) != 1 {
		t.Fatalf("SpawnMonsterByNameAt() = %+v, error = %v", spawn, err)
	}
	now := time.Now()
	w.mu.Lock()
	mon := w.monsters[spawn.Monsters[0].ID]
	mon.LastHitterID = ch.ID
	mon.LastHitterAt = now.Add(-31 * time.Second)
	mon.ExpHitterID = ch.ID
	mon.ExpHitterAt = now.Add(-7 * time.Second)
	w.mu.Unlock()
	if _, err := w.Tick([]PlayerSnapshot{{Character: ch}}, now); err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	mon = w.monsters[spawn.Monsters[0].ID]
	if mon.LastHitterID != "" || !mon.LastHitterAt.IsZero() || mon.ExpHitterID != "" || !mon.ExpHitterAt.IsZero() {
		t.Fatalf("monster hitter lifetimes = %+v, want expired", mon)
	}
}

func TestMonsterHitterRefreshesExperienceWindowForSameAttacker(t *testing.T) {
	w := &World{}
	mon := &Monster{ExpHitterID: "attacker", ExpHitterAt: time.Now().Add(-time.Second)}
	w.setMonsterLastHitterLocked(mon, "attacker")
	if mon.ExpHitterAt.Before(mon.LastHitterAt) || mon.ExpHitterAt.IsZero() {
		t.Fatalf("experience hitter time = %s, want refreshed at last hitter time %s", mon.ExpHitterAt, mon.LastHitterAt)
	}
}

func TestWorldTickDoesNotUseExpiredDeathHitterForExperience(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	spawn, err := w.SpawnMonsterByNameAt(ch.MapID, ch.X+1, ch.Y, "骷髅", 1)
	if err != nil || len(spawn.Monsters) != 1 {
		t.Fatalf("SpawnMonsterByNameAt() = %+v, error = %v", spawn, err)
	}
	now := time.Now()
	w.mu.Lock()
	mon := w.monsters[spawn.Monsters[0].ID]
	mon.HP = 0
	mon.PendingDeath = true
	mon.DeathHitterID = ch.ID
	mon.LastHitterID = ""
	mon.LastHitterAt = time.Time{}
	mon.ExpHitterID = ""
	mon.ExpHitterAt = time.Time{}
	w.mu.Unlock()
	tick, err := w.Tick([]PlayerSnapshot{{Character: ch}}, now)
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if len(tick.MonsterDeaths) != 1 || tick.MonsterDeaths[0].Experience != 0 {
		t.Fatalf("expired death hitter result = %+v, want one death without experience", tick.MonsterDeaths)
	}
}

func TestWorldTickReportsExpiredMonsterNameColor(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	spawn, err := w.SpawnMonsterByNameAt(caster.MapID, caster.X+1, caster.Y, "骷髅", 1)
	if err != nil {
		t.Fatalf("SpawnMonsterByNameAt() error = %v", err)
	}
	monID := spawn.Monsters[0].ID
	w.mu.Lock()
	w.monsters[monID].HolySeizeUntil = time.Now().Add(-time.Second)
	mon := *w.monsters[monID]
	w.mu.Unlock()

	result, err := w.Tick([]PlayerSnapshot{{Character: caster}}, time.Now())
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if len(result.NameColorMonsters) != 1 || result.NameColorMonsters[0].ID != mon.ID {
		t.Fatalf("name color monsters = %+v, want %q", result.NameColorMonsters, mon.ID)
	}
	if got := MonsterNameColor(result.NameColorMonsters[0]); got != 255 {
		t.Fatalf("expired monster name color = %d, want 255", got)
	}
}

func TestWorldTickReleasesExpiredSummon(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	spawn, err := w.SpawnMonsterByNameAt(caster.MapID, caster.X+1, caster.Y, "骷髅", 1)
	if err != nil {
		t.Fatalf("SpawnMonsterByNameAt() error = %v", err)
	}
	monID := spawn.Monsters[0].ID
	w.mu.Lock()
	mon := w.monsters[monID]
	mon.MasterID = caster.ID
	mon.MasterName = caster.Name
	mon.MasterExpiresAt = time.Now().Add(-time.Second)
	mon.NoTame = true
	mon.HP = 100
	w.mu.Unlock()

	result, err := w.Tick([]PlayerSnapshot{{Character: caster}}, time.Now())
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	w.mu.Lock()
	released := *w.monsters[monID]
	w.mu.Unlock()
	if !released.Alive || released.MasterID != "" || released.MasterName != "" || !released.MasterExpiresAt.IsZero() || released.NoTame {
		t.Fatalf("released summon = %+v, want alive and independent", released)
	}
	if released.HP != 10 {
		t.Fatalf("released summon HP = %d, want 10", released.HP)
	}
	if len(result.NameMonsters) != 1 || result.NameMonsters[0].ID != monID {
		t.Fatalf("released summon name refreshes = %+v, want %q", result.NameMonsters, monID)
	}
}

func TestCastSkillHealQueuesRecoveryForCaster(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	caster.Skills = storage.SkillStates{{ID: "治愈术", Level: 0, Train: 0}}
	caster.HP = 20
	caster.MaxHP = 100
	updated, err := w.CastSkillWithPlayers(caster, "治愈术", caster.X, caster.Y, 0, nil)
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	if updated.Character.HP != 20 {
		t.Fatalf("caster HP = %d, want unchanged before recovery tick", updated.Character.HP)
	}
	if len(w.pendingSpells) != 1 {
		t.Fatalf("pending spells = %d, want 1", len(w.pendingSpells))
	}
	if pending := w.pendingSpells[0]; pending.TargetX != caster.X || pending.TargetY != caster.Y {
		t.Fatalf("pending target = (%d,%d), want caster position (%d,%d)", pending.TargetX, pending.TargetY, caster.X, caster.Y)
	}
	var start, magicFire *SpellEvent
	for i := range updated.Events {
		switch updated.Events[i].Kind {
		case SpellEventStart:
			start = &updated.Events[i]
		case SpellEventMagicFire:
			magicFire = &updated.Events[i]
		}
	}
	if start == nil || start.TargetX != caster.X || start.TargetY != caster.Y {
		t.Fatalf("heal start target = %+v, want caster position (%d,%d)", start, caster.X, caster.Y)
	}
	if magicFire == nil || magicFire.TargetX != caster.X || magicFire.TargetY != caster.Y {
		t.Fatalf("heal magic fire target = %+v, want caster position (%d,%d)", magicFire, caster.X, caster.Y)
	}
}

func TestTickAppliesQueuedHealingRecoveryInMessageTick(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	caster.Skills = storage.SkillStates{{ID: "治愈术", Level: 0, Train: 0}}
	caster.HP = 20
	caster.MaxHP = 100
	updated, err := w.CastSkillWithPlayers(caster, "治愈术", caster.X, caster.Y, 0, nil)
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	result, err := w.Tick([]PlayerSnapshot{{Character: updated.Character}}, time.Now().Add(time.Second))
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if len(result.Characters) != 1 {
		t.Fatalf("Characters = %+v, want one updated character", result.Characters)
	}
	next := result.Characters[0]
	if next.HP <= 20 || next.IncHealing != 0 {
		t.Fatalf("first recovery = %+v, want same-tick healing with empty queue", next)
	}
	result, err = w.Tick([]PlayerSnapshot{{Character: next}}, time.Now().Add(2*time.Second))
	if err != nil {
		t.Fatalf("second Tick() error = %v", err)
	}
	if len(result.Characters) != 0 {
		t.Fatalf("second recovery = %+v, want no additional recovery after queue was consumed", result.Characters)
	}
}

func TestQueuedHealingStillAppliesAfterTargetMoves(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	target, err := w.CreateCharacterWithAppearance("target", "target", "warrior", 0, 0, caster.MapID, caster.X+1, caster.Y)
	if err != nil {
		t.Fatalf("CreateCharacterWithAppearance() error = %v", err)
	}
	caster.Skills = storage.SkillStates{{ID: "治愈术", Level: 0, Train: 0}}
	target.HP = 20
	target.MaxHP = 100
	caster.MP = 100
	cast, err := w.CastSkillWithPlayers(caster, "治愈术", target.X, target.Y, CharacterActorID(target), []storage.Character{target})
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	target.X++
	deadCaster := cast.Character
	deadCaster.HP = 0
	result, err := w.Tick([]PlayerSnapshot{{Character: deadCaster}, {Character: target}}, time.Now().Add(time.Second))
	if err != nil {
		t.Fatalf("World.Tick() error = %v", err)
	}
	if len(result.HealingCharacters) != 1 || result.HealingCharacters[0] != target.ID {
		t.Fatalf("HealingCharacters = %v, want moved target %q", result.HealingCharacters, target.ID)
	}
}

func TestDelayedHealingIsClearedWhenTargetIsFullAtRecoveryTick(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	target, err := w.CreateCharacterWithAppearance("target", "target", "taoist", 0, 0, caster.MapID, caster.X+1, caster.Y)
	if err != nil {
		t.Fatalf("CreateCharacterWithAppearance() error = %v", err)
	}
	caster.Skills = storage.SkillStates{{ID: "治愈术", Level: 0, Train: 0}}
	target.HP = 20
	target.MaxHP = 100
	caster.MP = 100
	cast, err := w.CastSkillWithPlayers(caster, "治愈术", target.X, target.Y, CharacterActorID(target), []storage.Character{target})
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	if len(w.pendingSpells) != 1 {
		t.Fatalf("pending spells = %d, want 1", len(w.pendingSpells))
	}
	target.HP = target.MaxHP
	result, err := w.Tick([]PlayerSnapshot{{Character: cast.Character}, {Character: target}}, time.Now().Add(time.Second))
	if err != nil {
		t.Fatalf("World.Tick() error = %v", err)
	}
	var got storage.Character
	for _, ch := range result.Characters {
		if ch.ID == target.ID {
			got = ch
			break
		}
	}
	if got.ID == "" {
		t.Fatalf("target missing from tick result: %+v", result.Characters)
	}
	if got.IncHealing != 0 {
		t.Fatalf("target IncHealing = %d, want recovery queue cleared after target became full", got.IncHealing)
	}
}

func TestCastSkillHealIgnoresNoTargetCoordinates(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	caster.Skills = storage.SkillStates{{ID: "治愈术", Level: 0, Train: 0}}
	caster.HP = 20
	caster.MaxHP = 100
	updated, err := w.CastSkillWithPlayers(caster, "治愈术", caster.X+8, caster.Y, 0, nil)
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	if updated.Character.HP != 20 {
		t.Fatalf("caster HP = %d, want unchanged before recovery tick", updated.Character.HP)
	}
	if len(w.pendingSpells) != 1 {
		t.Fatalf("pending spells = %d, want 1", len(w.pendingSpells))
	}
	if pending := w.pendingSpells[0]; pending.TargetX != caster.X || pending.TargetY != caster.Y {
		t.Fatalf("pending target = (%d,%d), want caster position (%d,%d)", pending.TargetX, pending.TargetY, caster.X, caster.Y)
	}
	startFound, fireFound := false, false
	for _, event := range updated.Events {
		switch event.Kind {
		case SpellEventStart:
			startFound = true
			if event.TargetX != caster.X || event.TargetY != caster.Y {
				t.Fatalf("heal start event = %+v, want caster coordinates", event)
			}
		case SpellEventMagicFire:
			fireFound = true
			if event.TargetX != caster.X || event.TargetY != caster.Y {
				t.Fatalf("heal magic fire event = %+v, want caster coordinates", event)
			}
		}
	}
	if !startFound || !fireFound {
		t.Fatalf("heal events = %+v, want start and magic fire events", updated.Events)
	}
}

func TestCastSkillHealHealsFriendlySummon(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	caster.EquippedItems = map[int]storage.UserItem{SlotBujuk: {ItemID: "护身符", Dura: 10000}}
	caster.Dir = 2
	caster.MP = 100
	caster.HP = 20
	caster.MaxHP = 100
	caster.Skills = storage.SkillStates{{ID: "召唤骷髅", Level: 0, Train: 0}, {ID: "治愈术", Level: 0, Train: 0}}
	caster.EquippedItems = map[int]storage.UserItem{SlotBujuk: {ItemID: "护身符", Dura: 10000}}
	summonedResult, err := w.CastSkillWithPlayers(caster, "召唤骷髅", caster.X, caster.Y, 0, nil)
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() summon error = %v", err)
	}
	summoned := summonedResult.SummonedMonsters[0]
	w.mu.Lock()
	if mon, ok := w.monsters[summoned.ID]; ok {
		mon.HP = 5
	}
	w.mu.Unlock()
	updated, err := w.CastSkillWithPlayers(summonedResult.Character, "治愈术", summoned.X, summoned.Y, MonsterActorID(summoned), nil)
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() heal error = %v", err)
	}
	if updated.Character.HP != 20 {
		t.Fatalf("caster HP = %d, want self HP unchanged when healing summon", updated.Character.HP)
	}
	_, err = w.Tick([]PlayerSnapshot{{Character: updated.Character}}, time.Now().Add(time.Second))
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	w.mu.Lock()
	gotHP := 0
	if mon, ok := w.monsters[summoned.ID]; ok {
		gotHP = mon.HP
	}
	w.mu.Unlock()
	if gotHP <= 5 {
		t.Fatalf("summoned HP = %d, want healed above 5", gotHP)
	}
}

func TestCastSkillHealCanTargetGroupMember(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	mapID, x, y := caster.MapID, caster.X, caster.Y
	friend, err := w.CreateCharacterWithAppearance("test", "friend", "wizard", 0, 0, mapID, x+1, y)
	if err != nil {
		t.Fatalf("CreateCharacter() friend error = %v", err)
	}
	caster.AllowGroup = true
	friend.AllowGroup = true
	caster, friend, err = w.CreateGroup(caster, friend, 2)
	if err != nil {
		t.Fatalf("CreateGroup() error = %v", err)
	}
	caster.Skills = storage.SkillStates{{ID: "治愈术", Level: 0, Train: 0}}
	friend.HP = 30
	friend.MaxHP = 100
	players := []storage.Character{caster, friend}
	updated, err := w.CastSkillWithPlayers(caster, "治愈术", friend.X, friend.Y, CharacterActorID(friend), players)
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	if updated.Character.HP != caster.HP {
		t.Fatalf("caster HP = %d, want unchanged", updated.Character.HP)
	}
	result, err := w.Tick([]PlayerSnapshot{{Character: updated.Character}, {Character: friend}}, time.Now().Add(time.Second))
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if len(result.Characters) == 0 || result.Characters[0].ID != friend.ID || result.Characters[0].HP <= friend.HP || result.Characters[0].IncHealing != 0 {
		t.Fatalf("recovery result = %+v, want same-tick friend recovery", result.Characters)
	}
}

func TestCastSkillHealCanTargetFriendlyNonGroupInPeaceMode(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	mapID, x, y := caster.MapID, caster.X, caster.Y
	friend, err := w.CreateCharacterWithAppearance("test", "friend", "wizard", 0, 0, mapID, x+1, y)
	if err != nil {
		t.Fatalf("CreateCharacter() friend error = %v", err)
	}
	caster.AttackMode = 1
	caster.Skills = storage.SkillStates{{ID: "治愈术", Level: 0, Train: 0}}
	friend.HP = 30
	friend.MaxHP = 100
	players := []storage.Character{caster, friend}
	updated, err := w.CastSkillWithPlayers(caster, "治愈术", friend.X, friend.Y, CharacterActorID(friend), players)
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	result, err := w.Tick([]PlayerSnapshot{{Character: updated.Character}, {Character: friend}}, time.Now().Add(time.Second))
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if len(result.Characters) == 0 || result.Characters[0].ID != friend.ID || result.Characters[0].HP <= friend.HP || result.Characters[0].IncHealing != 0 {
		t.Fatalf("recovery result = %+v, want same-tick friend recovery", result.Characters)
	}
}

func TestCastSkillHealPrefersExactTargetID(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	mapID, x, y := caster.MapID, caster.X, caster.Y
	first, err := w.CreateCharacterWithAppearance("test", "first", "wizard", 0, 0, mapID, x+1, y)
	if err != nil {
		t.Fatalf("CreateCharacter() first error = %v", err)
	}
	second, err := w.CreateCharacterWithAppearance("test", "second", "wizard", 0, 0, mapID, x, y+1)
	if err != nil {
		t.Fatalf("CreateCharacter() second error = %v", err)
	}
	caster.AttackMode = 1
	caster.Skills = storage.SkillStates{{ID: "治愈术", Level: 0, Train: 0}}
	first.HP = 30
	second.HP = 20
	first.MaxHP = 100
	second.MaxHP = 100
	players := []storage.Character{caster, first, second}
	updated, err := w.CastSkillWithPlayers(caster, "治愈术", second.X, second.Y, CharacterActorID(second), players)
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	result, err := w.Tick([]PlayerSnapshot{{Character: updated.Character}, {Character: first}, {Character: second}}, time.Now().Add(time.Second))
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if len(result.Characters) == 0 || result.Characters[0].ID != second.ID || result.Characters[0].HP <= second.HP || result.Characters[0].IncHealing != 0 {
		t.Fatalf("recovery result = %+v, want same-tick exact-target recovery", result.Characters)
	}
}

func TestCastSkillHealIgnoresHostileTarget(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	mapID, x, y := caster.MapID, caster.X, caster.Y
	enemy, err := w.CreateCharacterWithAppearance("test", "enemy", "warrior", 0, 0, mapID, x+1, y)
	if err != nil {
		t.Fatalf("CreateCharacter() enemy error = %v", err)
	}
	caster.AttackMode = 2
	caster.Skills = storage.SkillStates{{ID: "治愈术", Level: 0, Train: 0}}
	caster.HP = 20
	caster.MaxHP = 100
	players := []storage.Character{caster, enemy}
	updated, err := w.CastSkillWithPlayers(caster, "治愈术", enemy.X, enemy.Y, CharacterActorID(enemy), players)
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v, want hostile target ignored", err)
	}
	if updated.Character.ID != caster.ID {
		t.Fatalf("updated character = %+v, want caster", updated.Character)
	}
	if updated.Character.HP != caster.HP {
		t.Fatalf("caster HP = %d, want unchanged", updated.Character.HP)
	}
	if updated.Character.IncHealing != 0 {
		t.Fatalf("caster IncHealing = %d, want unchanged", updated.Character.IncHealing)
	}
}

func TestCastSkillProtectionBuffsApplyToGroupMembers(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	mapID, x, y := caster.MapID, caster.X, caster.Y
	friend, err := w.CreateCharacterWithAppearance("test", "friend", "wizard", 0, 0, mapID, x+1, y)
	if err != nil {
		t.Fatalf("CreateCharacter() friend error = %v", err)
	}
	caster.AllowGroup = true
	friend.AllowGroup = true
	caster, friend, err = w.CreateGroup(caster, friend, 2)
	if err != nil {
		t.Fatalf("CreateGroup() error = %v", err)
	}
	caster.Skills = storage.SkillStates{
		{ID: "神圣战甲术", Level: 0, Train: 0},
		{ID: "幽灵盾", Level: 0, Train: 0},
	}
	caster.EquippedItems = map[int]storage.UserItem{SlotBujuk: {ItemID: "护身符(大)", Dura: 20000}}
	players := []storage.Character{caster, friend}
	result, err := w.CastSkillWithPlayers(caster, "神圣战甲术", x, y, 0, players)
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() armour error = %v", err)
	}
	if len(result.AffectedCharacters) != 2 {
		t.Fatalf("armour affected = %d, want 2", len(result.AffectedCharacters))
	}
	if result.Character.DefenceUpUntil == 0 {
		t.Fatal("armour buff did not set expiry")
	}
	result, err = w.CastSkillWithPlayers(result.Character, "幽灵盾", x, y, 0, players)
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() shield error = %v", err)
	}
	if len(result.AffectedCharacters) != 2 {
		t.Fatalf("shield affected = %d, want 2", len(result.AffectedCharacters))
	}
	if result.Character.MagDefenceUpUntil == 0 {
		t.Fatal("shield buff did not set expiry")
	}
}

func TestGroupDefenceIncludesDeadFriendsLikeReference(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	friend, err := w.CreateCharacterWithAppearance("test", "dead-friend", "wizard", 0, 0, caster.MapID, caster.X+1, caster.Y)
	if err != nil {
		t.Fatalf("CreateCharacterWithAppearance() error = %v", err)
	}
	caster.AllowGroup = true
	friend.AllowGroup = true
	caster, friend, err = w.CreateGroup(caster, friend, 2)
	if err != nil {
		t.Fatalf("CreateGroup() error = %v", err)
	}
	friend.HP = 0
	now := time.Now()
	w.mu.Lock()
	affected, _, valid, err := w.groupDefenceCharactersLocked(caster, w.data.Skills["神圣战甲术"], storage.SkillState{}, []storage.Character{caster, friend}, caster.X, caster.Y, false, time.Second, now)
	w.mu.Unlock()
	if err != nil {
		t.Fatalf("groupDefenceCharactersLocked() error = %v", err)
	}
	if !valid || len(affected) != 2 {
		t.Fatalf("affected dead friend = %d, valid=%t, want 2,true", len(affected), valid)
	}
}

func TestProtectionBuffDoesNotShortenExistingDuration(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	friend, err := w.CreateCharacterWithAppearance("test", "friend", "wizard", 0, 0, caster.MapID, caster.X+1, caster.Y)
	if err != nil {
		t.Fatalf("CreateCharacterWithAppearance() friend error = %v", err)
	}
	caster.AllowGroup = true
	friend.AllowGroup = true
	caster, friend, err = w.CreateGroup(caster, friend, 2)
	if err != nil {
		t.Fatalf("CreateGroup() error = %v", err)
	}
	now := time.Unix(1000, 0)
	players := []storage.Character{caster, friend}
	firstExpires := now.Add(20*time.Second + 500*time.Millisecond).UnixNano()
	players[1].DefenceUpUntil = firstExpires
	w.mu.Lock()
	affected, _, _, err := w.groupDefenceCharactersLocked(caster, w.data.Skills["神圣战甲术"], storage.SkillState{Level: 0}, players, caster.X, caster.Y, false, 10*time.Second, now)
	w.mu.Unlock()
	if err != nil {
		t.Fatalf("groupDefenceCharactersLocked() error = %v", err)
	}
	if len(affected) != 2 {
		t.Fatalf("affected characters = %d, want 2", len(affected))
	}
	for _, target := range affected {
		if target.ID == friend.ID && target.DefenceUpUntil != now.Add(21*time.Second).UnixNano() {
			t.Fatalf("existing defence expiry = %d, want reset to 21 rounded seconds from recast", target.DefenceUpUntil)
		}
	}
}

func TestCastSkillProtectionBuffsApplyToFriendlySummon(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	caster.EquippedItems = map[int]storage.UserItem{SlotBujuk: {ItemID: "护身符", Dura: 10000}}
	caster.Dir = 2
	caster.MP = 100
	caster.Skills = storage.SkillStates{
		{ID: "召唤骷髅", Level: 0, Train: 0},
		{ID: "神圣战甲术", Level: 0, Train: 0},
	}
	caster.EquippedItems = map[int]storage.UserItem{SlotBujuk: {ItemID: "护身符", Dura: 10000}}
	summonedResult, err := w.CastSkillWithPlayers(caster, "召唤骷髅", caster.X, caster.Y, 0, nil)
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() summon error = %v", err)
	}
	updated, err := w.CastSkillWithPlayers(summonedResult.Character, "神圣战甲术", summonedResult.Character.X, summonedResult.Character.Y, 0, nil)
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() armour error = %v", err)
	}
	if len(updated.AffectedMonsters) != 1 {
		t.Fatalf("AffectedMonsters = %d, want 1", len(updated.AffectedMonsters))
	}
	if updated.AffectedMonsters[0].DefenceUpUntil == 0 {
		t.Fatal("summon armour buff did not set expiry")
	}
}

func TestTickClearsExpiredProtectionFromFriendlySummon(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	caster.EquippedItems = map[int]storage.UserItem{SlotBujuk: {ItemID: "护身符", Dura: 10000}}
	caster.Dir = 2
	caster.MP = 100
	caster.Skills = storage.SkillStates{
		{ID: "召唤骷髅", Level: 0, Train: 0},
		{ID: "神圣战甲术", Level: 0, Train: 0},
	}
	caster.EquippedItems = map[int]storage.UserItem{SlotBujuk: {ItemID: "护身符", Dura: 10000}}
	summonedResult, err := w.CastSkillWithPlayers(caster, "召唤骷髅", caster.X, caster.Y, 0, nil)
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() summon error = %v", err)
	}
	updated, err := w.CastSkillWithPlayers(summonedResult.Character, "神圣战甲术", summonedResult.Character.X, summonedResult.Character.Y, 0, nil)
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() armour error = %v", err)
	}
	if len(updated.AffectedMonsters) != 1 {
		t.Fatalf("AffectedMonsters = %d, want 1", len(updated.AffectedMonsters))
	}
	monID := updated.AffectedMonsters[0].ID
	w.mu.Lock()
	if mon := w.monsters[monID]; mon != nil {
		mon.DefenceUpUntil = time.Now().Add(-time.Second).UnixNano()
		mon.MagDefenceUpUntil = time.Now().Add(-time.Second).UnixNano()
	}
	w.mu.Unlock()
	if _, err := w.Tick([]PlayerSnapshot{{Character: updated.Character}}, time.Now()); err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	w.mu.Lock()
	mon := w.monsters[monID]
	w.mu.Unlock()
	if mon == nil {
		t.Fatalf("monster %s missing after tick", monID)
	}
	if mon.DefenceUpUntil != 0 || mon.MagDefenceUpUntil != 0 {
		t.Fatalf("monster protection not cleared: defence=%d magic=%d", mon.DefenceUpUntil, mon.MagDefenceUpUntil)
	}
}

func TestCastSkillProtectionBuffDurationGrowsWithSpirit(t *testing.T) {
	plainW, plainCaster := newTestWorldCharacter(t)
	plainW.mu.Lock()
	plainW.rand = rand.New(rand.NewSource(1))
	plainW.mu.Unlock()
	plainCaster.Skills = storage.SkillStates{{ID: "神圣战甲术", Level: 0, Train: 0}}
	plainCaster.EquippedItems = map[int]storage.UserItem{SlotBujuk: {ItemID: "护身符", Dura: 10000}}
	plainPlayers := []storage.Character{plainCaster}
	plainResult, err := plainW.CastSkillWithPlayers(plainCaster, "神圣战甲术", plainCaster.X, plainCaster.Y, 0, plainPlayers)
	if err != nil {
		t.Fatalf("plain CastSkillWithPlayers() error = %v", err)
	}
	plainRemaining := time.Until(time.Unix(0, plainResult.Character.DefenceUpUntil))

	buffedW, buffedCaster := newTestWorldCharacter(t)
	buffedW.mu.Lock()
	buffedW.rand = rand.New(rand.NewSource(1))
	buffedW.mu.Unlock()
	buffedCaster.Skills = storage.SkillStates{{ID: "神圣战甲术", Level: 0, Train: 0}}
	buffedCaster.EquippedItems = map[int]storage.UserItem{
		SlotBujuk:    {ItemID: "护身符", Dura: 10000},
		SlotWeapon:   {ItemID: "无极棍"},
		SlotDress:    {ItemID: "天尊道袍"},
		SlotNecklace: {ItemID: "天尊项链"},
		SlotRingL:    {ItemID: "泰坦戒指"},
	}
	buffedPlayers := []storage.Character{buffedCaster}
	buffedResult, err := buffedW.CastSkillWithPlayers(buffedCaster, "神圣战甲术", buffedCaster.X, buffedCaster.Y, 0, buffedPlayers)
	if err != nil {
		t.Fatalf("buffed CastSkillWithPlayers() error = %v", err)
	}
	buffedRemaining := time.Until(time.Unix(0, buffedResult.Character.DefenceUpUntil))
	if buffedRemaining <= plainRemaining+10*time.Second {
		t.Fatalf("buffed duration = %v, plain duration = %v, want spirit gear to extend it", buffedRemaining, plainRemaining)
	}
}

func TestCastSkillStealthMarksCasterTransparent(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	caster.EquippedItems = map[int]storage.UserItem{SlotBujuk: {ItemID: "护身符", Dura: 10000}}
	caster.Skills = storage.SkillStates{{ID: "隐身术", Level: 0, Train: 0}}
	updated, err := w.CastSkillWithPlayers(caster, "隐身术", caster.X, caster.Y, 0, nil)
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	if updated.Character.TransparentUntil == 0 {
		t.Fatal("TransparentUntil = 0, want active stealth")
	}
}

func TestCastSkillStealthTreatsExpiredStateAsPresentUntilTick(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	caster.EquippedItems = map[int]storage.UserItem{SlotBujuk: {ItemID: "护身符", Dura: 10000}}
	caster.Skills = storage.SkillStates{{ID: "隐身术", Level: 0, Train: 0}}
	caster.TransparentUntil = time.Now().Add(-time.Second).UnixNano()
	updated, err := w.CastSkillWithPlayers(caster, "隐身术", caster.X, caster.Y, 0, nil)
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	if updated.Character.TransparentUntil != caster.TransparentUntil {
		t.Fatalf("TransparentUntil = %d, want unchanged expired state %d", updated.Character.TransparentUntil, caster.TransparentUntil)
	}
}

func TestCastSkillStealthBreaksMonsterTarget(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	caster.EquippedItems = map[int]storage.UserItem{SlotBujuk: {ItemID: "护身符", Dura: 10000}}
	caster.Skills = storage.SkillStates{{ID: "隐身术", Level: 0, Train: 0}}
	mapID := caster.MapID
	x, y := caster.X+3, caster.Y
	created, err := spawnMonsterForTest(t, w, mapID, x, y, testMonsterID, 1)
	if err != nil {
		t.Fatalf("spawnMonsterForTest() error = %v", err)
	}
	mon := w.monsters[created.Monsters[0].ID]
	mon.TargetCharacterID = caster.ID
	mon.TargetFocusAt = time.Now()
	updated, err := w.CastSkillWithPlayers(caster, "隐身术", caster.X, caster.Y, 0, nil)
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	if updated.Character.TransparentUntil == 0 {
		t.Fatal("TransparentUntil = 0, want active stealth")
	}
	if mon.TargetCharacterID != "" {
		t.Fatalf("monster TargetCharacterID = %q, want cleared", mon.TargetCharacterID)
	}
	if !mon.TargetFocusAt.IsZero() {
		t.Fatalf("monster TargetFocusAt = %v, want cleared", mon.TargetFocusAt)
	}
}

func TestStealthTargetBreakUsesReferenceObjectOrder(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	first := &Monster{ID: "first", ObjectOrder: 1, Alive: true, Race: 50, MapID: caster.MapID, X: caster.X + 1, Y: caster.Y, TargetCharacterID: caster.ID}
	second := &Monster{ID: "second", ObjectOrder: 2, Alive: true, Race: 50, MapID: caster.MapID, X: caster.X + 1, Y: caster.Y, TargetCharacterID: caster.ID}
	w.mu.Lock()
	w.rand = rand.New(&seqSource{vals: []int64{0, (1 << 63) - 1}})
	w.monsters = map[string]*Monster{first.ID: first, second.ID: second}
	w.breakNearbyMonsterTargetsForStealthLocked(caster)
	w.mu.Unlock()
	if first.TargetCharacterID != "" || second.TargetCharacterID != caster.ID {
		t.Fatalf("target locks = (%q, %q), want first cleared and second retained", first.TargetCharacterID, second.TargetCharacterID)
	}
}

func TestCastSkillStealthDoesNotBreakNonAnimalMonsterTarget(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	caster.EquippedItems = map[int]storage.UserItem{SlotBujuk: {ItemID: "护身符", Dura: 10000}}
	caster.Skills = storage.SkillStates{{ID: "隐身术", Level: 0, Train: 0}}
	created, err := spawnMonsterForTest(t, w, caster.MapID, caster.X+3, caster.Y, testMonsterID, 1)
	if err != nil {
		t.Fatalf("spawnMonsterForTest() error = %v", err)
	}
	mon := w.monsters[created.Monsters[0].ID]
	mon.Race = 1
	mon.TargetCharacterID = caster.ID
	_, err = w.CastSkillWithPlayers(caster, "隐身术", caster.X, caster.Y, 0, nil)
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	if mon.TargetCharacterID != caster.ID {
		t.Fatalf("non-animal monster TargetCharacterID = %q, want %q", mon.TargetCharacterID, caster.ID)
	}
}

func TestMonsterTargetKeepsExpiredStealthUntilTick(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	caster.TransparentUntil = time.Now().Add(-time.Second).UnixNano()
	mon := &Monster{ID: "monster", MapID: caster.MapID, X: caster.X + 1, Y: caster.Y, ViewRange: 10}

	w.mu.Lock()
	_, ok := w.findClosestMonsterTargetExceptLocked(mon, map[string]storage.Character{caster.ID: caster}, 10, "", time.Now())
	w.mu.Unlock()
	if ok {
		t.Fatal("monster selected character with uncleared stealth state")
	}
}

func TestCastSkillGroupStealthMarksGroupMembersTransparent(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	caster.EquippedItems = map[int]storage.UserItem{SlotBujuk: {ItemID: "护身符(大)", Dura: 20000}}
	mapID, x, y := caster.MapID, caster.X, caster.Y
	friend, err := w.CreateCharacterWithAppearance("test", "friend", "wizard", 0, 0, mapID, x+1, y)
	if err != nil {
		t.Fatalf("CreateCharacter() friend error = %v", err)
	}
	caster.AllowGroup = true
	friend.AllowGroup = true
	caster, friend, err = w.CreateGroup(caster, friend, 2)
	if err != nil {
		t.Fatalf("CreateGroup() error = %v", err)
	}
	caster.Skills = storage.SkillStates{{ID: "集体隐身术", Level: 0, Train: 0}}
	players := []storage.Character{caster, friend}
	updated, err := w.CastSkillWithPlayers(caster, "集体隐身术", x, y, 0, players)
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	if len(updated.AffectedCharacters) != 0 {
		t.Fatalf("immediate affected characters = %d, want delayed", len(updated.AffectedCharacters))
	}
	if len(w.pendingSpells) != 2 {
		t.Fatalf("pending spells = %d, want caster and friend", len(w.pendingSpells))
	}
	for _, ch := range []storage.Character{caster, friend} {
		if ch.TransparentUntil != 0 {
			t.Fatalf("character %q became transparent before delay", ch.ID)
		}
	}
	delivered, err := w.Tick([]PlayerSnapshot{{Character: caster}, {Character: friend}}, time.Now().Add(900*time.Millisecond))
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if len(delivered.StatusRefreshCharacters) != 2 {
		t.Fatalf("delivered status refreshes = %d, want 2", len(delivered.StatusRefreshCharacters))
	}
	for _, ch := range delivered.StatusRefreshCharacters {
		if ch.TransparentUntil <= time.Now().Add(500*time.Millisecond).UnixNano() {
			t.Fatalf("character %q transparent until = %d, want duration starting at delayed delivery", ch.ID, ch.TransparentUntil)
		}
	}
}

func TestCastSkillGroupStealthSkipsDeadFriendsLikeReference(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	caster.EquippedItems = map[int]storage.UserItem{SlotBujuk: {ItemID: "护身符(大)", Dura: 20000}}
	mapID, x, y := caster.MapID, caster.X, caster.Y
	friend, err := w.CreateCharacterWithAppearance("test", "dead-friend", "wizard", 0, 0, mapID, x+1, y)
	if err != nil {
		t.Fatalf("CreateCharacter() friend error = %v", err)
	}
	caster.AllowGroup = true
	friend.AllowGroup = true
	caster, friend, err = w.CreateGroup(caster, friend, 2)
	if err != nil {
		t.Fatalf("CreateGroup() error = %v", err)
	}
	friend.HP = 0
	caster.Skills = storage.SkillStates{{ID: "集体隐身术", Level: 0, Train: 0}}
	_, err = w.CastSkillWithPlayers(caster, "集体隐身术", x, y, 0, []storage.Character{caster, friend})
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	if len(w.pendingSpells) != 1 {
		t.Fatalf("pending spells = %d, want living friendly target only", len(w.pendingSpells))
	}
}

func TestCastSkillGroupStealthDoesNotTrainWithoutNewTarget(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	caster.EquippedItems = map[int]storage.UserItem{SlotBujuk: {ItemID: "护身符(大)", Dura: 20000}}
	caster.Level = 50
	caster.TransparentUntil = time.Now().Add(time.Minute).UnixNano()
	caster.Skills = storage.SkillStates{{ID: "集体隐身术", Level: 0, Train: 0}}

	result, err := w.CastSkillWithPlayers(caster, "集体隐身术", caster.X, caster.Y, 0, []storage.Character{caster})
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	if len(w.pendingSpells) != 0 {
		t.Fatalf("pending spells = %d, want no duplicate transparency", len(w.pendingSpells))
	}
	if result.SkillTraining {
		t.Fatal("SkillTraining = true, want false when no new target is queued")
	}
}

func TestCastSkillGroupStealthMarksFriendlySummonTransparent(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	caster.EquippedItems = map[int]storage.UserItem{SlotBujuk: {ItemID: "护身符(大)", Dura: 20000}}
	spawned, err := w.SpawnMonsterByNameAt(caster.MapID, caster.X+1, caster.Y, "骷髅", 1)
	if err != nil {
		t.Fatalf("SpawnMonsterByNameAt() error = %v", err)
	}
	if len(spawned.Monsters) != 1 {
		t.Fatalf("spawned monsters = %d, want 1", len(spawned.Monsters))
	}
	w.mu.Lock()
	mon := w.monsters[spawned.Monsters[0].ID]
	if mon == nil {
		w.mu.Unlock()
		t.Fatal("spawned summon is missing from world")
	}
	mon.MasterID = caster.ID
	stealthTarget := &Monster{ID: "stealth-target", MapID: caster.MapID, X: mon.X + 2, Y: mon.Y, Race: 50, Alive: true, TargetCharacterID: mon.ID}
	w.monsters[stealthTarget.ID] = stealthTarget
	w.mu.Unlock()
	caster.Skills = storage.SkillStates{{ID: "集体隐身术", Level: 0, Train: 0}}
	result, err := w.CastSkillWithPlayers(caster, "集体隐身术", caster.X, caster.Y, 0, []storage.Character{caster})
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	if len(result.Events) == 0 || len(w.pendingSpells) != 2 {
		t.Fatalf("events = %d pending spells = %d, want events and 2", len(result.Events), len(w.pendingSpells))
	}
	delivered, err := w.Tick([]PlayerSnapshot{{Character: caster}}, time.Now().Add(900*time.Millisecond))
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if len(delivered.StatusRefreshMonsters) != 1 || delivered.StatusRefreshMonsters[0].ID != mon.ID {
		t.Fatalf("monster status refreshes = %+v, want summon refresh", delivered.StatusRefreshMonsters)
	}
	if delivered.StatusRefreshMonsters[0].TransparentUntil.IsZero() {
		t.Fatal("summon TransparentUntil is zero, want active transparency")
	}
	w.mu.Lock()
	remainingTarget := w.monsters[stealthTarget.ID].TargetCharacterID
	w.mu.Unlock()
	if remainingTarget != "" {
		t.Fatalf("stealth target = %q, want cleared target", remainingTarget)
	}
}

func TestQueuedGroupStealthUsesTargetMessageState(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	now := time.Now()
	friend, err := w.CreateCharacterWithAppearance("test", "friend", "wizard", 0, 0, caster.MapID, caster.X+1, caster.Y)
	if err != nil {
		t.Fatalf("CreateCharacter() friend error = %v", err)
	}
	w.pendingSpells = []pendingSpell{{
		DueAt: now.Add(-time.Millisecond), CasterID: caster.ID, TargetCharacterID: friend.ID,
		TransparentDuration: time.Minute,
	}}
	deadCaster := caster
	deadCaster.HP = 0
	movedFriend := friend
	movedFriend.MapID = "other-map"
	delivered, err := w.Tick([]PlayerSnapshot{{Character: deadCaster}, {Character: movedFriend}}, now)
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if len(delivered.StatusRefreshCharacters) != 1 || delivered.StatusRefreshCharacters[0].ID != friend.ID {
		t.Fatalf("status refreshes = %+v, want friend target refresh", delivered.StatusRefreshCharacters)
	}
	if delivered.StatusRefreshCharacters[0].TransparentUntil <= now.UnixNano() {
		t.Fatalf("transparent until = %d, want future expiry", delivered.StatusRefreshCharacters[0].TransparentUntil)
	}
}

func TestQueuedGroupStealthDoesNotExtendActiveTransparency(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	now := time.Now()
	friend, err := w.CreateCharacterWithAppearance("test", "friend", "wizard", 0, 0, caster.MapID, caster.X+1, caster.Y)
	if err != nil {
		t.Fatalf("CreateCharacter() friend error = %v", err)
	}
	originalUntil := now.Add(time.Minute).UnixNano()
	friend.TransparentUntil = originalUntil
	w.pendingSpells = []pendingSpell{{
		DueAt: now.Add(-time.Millisecond), CasterID: caster.ID, TargetCharacterID: friend.ID,
		TransparentDuration: 2 * time.Minute,
	}}
	delivered, err := w.Tick([]PlayerSnapshot{{Character: caster}, {Character: friend}}, now)
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if len(delivered.StatusRefreshCharacters) != 0 {
		t.Fatalf("status refreshes = %+v, want no refresh for active transparency", delivered.StatusRefreshCharacters)
	}
}

func TestCastSkillIceStormHitsMultipleMonsters(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	mapID, x, y := caster.MapID, caster.X, caster.Y
	mp := w.data.Maps[mapID]
	targetX, targetY := -1, -1
	for dx := 6; dx < 24 && targetX < 0; dx++ {
		for dy := -4; dy <= 4; dy++ {
			tx := x + dx
			ty := y + dy
			if !mp.Walkable(tx, ty) {
				continue
			}
			clear := true
			w.mu.Lock()
			for _, mon := range w.monsters {
				if mon != nil && mon.Alive && mon.MapID == mapID && abs(mon.X-tx) <= 1 && abs(mon.Y-ty) <= 1 {
					clear = false
					break
				}
			}
			w.mu.Unlock()
			if clear {
				targetX, targetY = tx, ty
				break
			}
		}
	}
	if targetX < 0 {
		t.Fatal("could not find clear tile for ice storm test")
	}
	result, err := w.SpawnMonsterByNameAt(mapID, targetX, targetY, "鸡", 2)
	if err != nil {
		t.Fatalf("SpawnMonsterByNameAt() error = %v", err)
	}
	if len(result.Monsters) != 2 {
		t.Fatalf("SpawnMonsterByNameAt() monsters = %d, want 2", len(result.Monsters))
	}
	caster.Skills = storage.SkillStates{{ID: "冰咆哮", Level: 0, Train: 0}}
	caster.MP = 50
	updated, err := w.CastSkillWithPlayers(caster, "冰咆哮", targetX, targetY, 0, nil)
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	if len(updated.MonsterHits) != 0 || len(updated.Impacts) != 2 || len(w.pendingSpells) != 0 {
		t.Fatalf("ice storm cast result = hits:%d impacts:%d pending:%d, want 0, 2 and 0", len(updated.MonsterHits), len(updated.Impacts), len(w.pendingSpells))
	}
	w.mu.Lock()
	for _, spawned := range result.Monsters {
		if mon := w.monsters[spawned.ID]; mon != nil && mon.Race >= 50 && mon.Level < 50 && !mon.LastWalkAt.After(time.Now()) {
			t.Fatalf("monster %s LastWalkAt = %v, want delayed after RM_MAGSTRUCK", mon.ID, mon.LastWalkAt)
		}
	}
	w.mu.Unlock()
	for _, impact := range updated.Impacts {
		if impact.MonsterHit == nil || impact.MonsterHit.Damage <= 0 || impact.MonsterHit.MonsterHP >= impact.MonsterHit.MonsterMaxHP {
			t.Fatalf("impact = %+v, want positive damage and reduced hp", impact)
		}
	}
}

func TestCastSkillLightningHitsCharacterTarget(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	w.mu.Lock()
	w.monsters = map[string]*Monster{}
	w.occupied = map[monsterPosition]string{}
	w.mu.Unlock()
	mapID := caster.MapID
	mp := w.data.Maps[mapID]
	targetX, targetY := -1, -1
	for dx := 2; dx < 10 && targetX < 0; dx++ {
		for dy := -2; dy <= 2; dy++ {
			tx := caster.X + dx
			ty := caster.Y + dy
			if !mp.Walkable(tx, ty) {
				continue
			}
			targetX, targetY = tx, ty
			break
		}
	}
	if targetX < 0 {
		t.Fatal("could not find clear tile for lightning test")
	}
	target, err := w.CreateCharacterWithAppearance("test2", "target", "warrior", 0, 0, mapID, targetX, targetY)
	if err != nil {
		t.Fatalf("CreateCharacterWithAppearance() error = %v", err)
	}
	caster.Skills = storage.SkillStates{{ID: "雷电术", Level: 0, Train: 0}}
	caster.MP = 100
	updated, err := w.CastSkillWithPlayers(caster, "雷电术", targetX, targetY, CharacterActorID(target), []storage.Character{target})
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	tick, err := w.Tick([]PlayerSnapshot{{Character: updated.Character}, {Character: target}}, time.Now().Add(2*time.Second))
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if len(tick.CharacterHits) != 1 {
		t.Fatalf("CharacterHits = %d, want 1", len(tick.CharacterHits))
	}
	hit := tick.CharacterHits[0]
	if hit.Character.ID != target.ID {
		t.Fatalf("hit.Character.ID = %q, want %q", hit.Character.ID, target.ID)
	}
	if hit.Damage <= 0 {
		t.Fatalf("hit = %+v, want positive damage", hit)
	}
	foundTarget := false
	for _, character := range tick.Characters {
		if character.ID == caster.ID && character.TargetID == target.ID {
			foundTarget = true
			break
		}
	}
	if !foundTarget {
		t.Fatalf("lightning caster target was not updated to %q", target.ID)
	}
}

func TestCastSkillIceStormHitsCharacterTarget(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	w.mu.Lock()
	w.monsters = map[string]*Monster{}
	w.occupied = map[monsterPosition]string{}
	w.mu.Unlock()
	targetX, targetY := caster.X+1, caster.Y
	if !w.data.Maps[caster.MapID].Walkable(targetX, targetY) {
		t.Fatal("expected walkable tile for ice storm test")
	}
	target, err := w.CreateCharacterWithAppearance("test2", "target", "warrior", 0, 0, caster.MapID, targetX, targetY)
	if err != nil {
		t.Fatalf("CreateCharacterWithAppearance() error = %v", err)
	}
	target.HP = 100
	target.MaxHP = 100
	caster.Skills = storage.SkillStates{{ID: "冰咆哮", Level: 0, Train: 0}}
	caster.MP = 100
	updated, err := w.CastSkillWithPlayers(caster, "冰咆哮", targetX, targetY, 0, []storage.Character{target})
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	if len(updated.CharacterHits) != 0 || len(updated.Impacts) != 1 || len(w.pendingSpells) != 0 {
		t.Fatalf("cast result = hits:%d impacts:%d pending:%d, want 0, 1 and 0", len(updated.CharacterHits), len(updated.Impacts), len(w.pendingSpells))
	}
	if updated.Impacts[0].CharacterHit == nil || updated.Impacts[0].CharacterHit.Character.ID != target.ID || updated.Impacts[0].CharacterHit.Damage <= 0 {
		t.Fatalf("impact = %+v, want one positive hit on %q", updated.Impacts[0], target.ID)
	}
}

func TestTickPoisonDeathClearsRecoveryAndEmitsDeathResult(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	now := time.Now()
	ch.HP = 1
	ch.PoisonHealthLevel = 1
	ch.PoisonHealthStartAt = now.Add(-time.Second).UnixNano()
	ch.PoisonHealthUntil = now.Add(time.Second).UnixNano()
	ch.PoisonHealthTickAt = now.Add(-poisonHealthTickInterval - time.Nanosecond).UnixNano()
	ch.IncHealth = 10
	ch.IncSpell = 10
	ch.IncHealing = 10

	result, err := w.Tick([]PlayerSnapshot{{Character: ch}}, now)
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if len(result.CharacterDeaths) != 0 {
		t.Fatalf("first CharacterDeaths = %+v, want no death until next object tick", result.CharacterDeaths)
	}
	dead := result.Characters[0]
	if dead.IncHealth != 0 || dead.IncSpell != 0 || dead.IncHealing != 0 {
		t.Fatalf("dead recovery queues = %d/%d/%d, want all cleared", dead.IncHealth, dead.IncSpell, dead.IncHealing)
	}
	if len(result.CharacterHits) != 0 {
		t.Fatalf("CharacterHits = %+v, want no synthetic struck event", result.CharacterHits)
	}
	next, err := w.Tick([]PlayerSnapshot{{Character: dead}}, now.Add(time.Second))
	if err != nil {
		t.Fatalf("second Tick() error = %v", err)
	}
	if len(next.CharacterDeaths) != 1 || next.CharacterDeaths[0].ID != ch.ID {
		t.Fatalf("second CharacterDeaths = %+v, want one death for %q", next.CharacterDeaths, ch.ID)
	}
}

func TestCastSkillSpiritFireHitsMonsterTarget(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	w.mu.Lock()
	w.monsters = map[string]*Monster{}
	w.occupied = map[monsterPosition]string{}
	w.mu.Unlock()
	mapID := caster.MapID
	targetX, targetY := caster.X+1, caster.Y
	if !w.data.Maps[mapID].Walkable(targetX, targetY) {
		t.Fatal("expected walkable tile for spirit fire test")
	}
	result, err := w.SpawnMonsterByNameAt(mapID, targetX, targetY, "鸡", 1)
	if err != nil {
		t.Fatalf("SpawnMonsterByNameAt() error = %v", err)
	}
	if len(result.Monsters) != 1 {
		t.Fatalf("SpawnMonsterByNameAt() monsters = %d, want 1", len(result.Monsters))
	}
	caster.Skills = storage.SkillStates{{ID: "灵魂火符", Level: 0, Train: 0}}
	caster.EquippedItems = map[int]storage.UserItem{SlotBujuk: {ItemID: "护身符", Dura: 10000}}
	caster.MP = 100
	updated, err := w.CastSkillWithPlayers(caster, "灵魂火符", targetX, targetY, MonsterActorID(result.Monsters[0]), nil)
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	tick, err := w.Tick([]PlayerSnapshot{{Character: updated.Character}}, time.Now().Add(2*time.Second))
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if len(tick.MonsterHits) != 1 || tick.MonsterHits[0].Damage <= 0 {
		t.Fatalf("MonsterHits = %+v, want one positive delayed hit", tick.MonsterHits)
	}
}

func TestCastSkillSpiritFireCanBeResistedByAntiMagic(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	w.mu.Lock()
	w.monsters = map[string]*Monster{}
	w.occupied = map[monsterPosition]string{}
	w.mu.Unlock()
	mapID := caster.MapID
	targetX, targetY := caster.X+1, caster.Y
	if !w.data.Maps[mapID].Walkable(targetX, targetY) {
		t.Fatal("expected walkable tile for spirit fire resistance test")
	}
	result, err := w.SpawnMonsterByNameAt(mapID, targetX, targetY, "鸡", 1)
	if err != nil {
		t.Fatalf("SpawnMonsterByNameAt() error = %v", err)
	}
	if len(result.Monsters) != 1 {
		t.Fatalf("SpawnMonsterByNameAt() monsters = %d, want 1", len(result.Monsters))
	}
	monsterID := result.Monsters[0].ID
	w.mu.Lock()
	if mon := w.monsters[monsterID]; mon != nil {
		mon.AntiMagic = 10
	}
	w.mu.Unlock()
	caster.Skills = storage.SkillStates{{ID: "灵魂火符", Level: 0, Train: 0}}
	caster.EquippedItems = map[int]storage.UserItem{SlotBujuk: {ItemID: "护身符", Dura: 10000}}
	caster.MP = 100
	updated, err := w.CastSkillWithPlayers(caster, "灵魂火符", targetX, targetY, 0, nil)
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	if updated.MonsterHit != nil {
		t.Fatalf("MonsterHit = %+v, want nil when magic defense resists", updated.MonsterHit)
	}
}

func TestCastSkillSpiritFireCanMissWithoutTarget(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	w.mu.Lock()
	w.monsters = map[string]*Monster{}
	w.occupied = map[monsterPosition]string{}
	w.mu.Unlock()
	mapID := caster.MapID
	mp := w.data.Maps[mapID]
	targetX, targetY := -1, -1
	for dx := 1; dx < 8 && targetX < 0; dx++ {
		for dy := -2; dy <= 2; dy++ {
			tx := caster.X + dx
			ty := caster.Y + dy
			if !mp.Walkable(tx, ty) {
				continue
			}
			targetX, targetY = tx, ty
			break
		}
	}
	if targetX < 0 {
		t.Fatal("could not find clear tile for spirit fire miss test")
	}
	caster.Skills = storage.SkillStates{{ID: "灵魂火符", Level: 0, Train: 0}}
	caster.EquippedItems = map[int]storage.UserItem{SlotBujuk: {ItemID: "护身符", Dura: 10000}}
	caster.MP = 100
	updated, err := w.CastSkillWithPlayers(caster, "灵魂火符", targetX, targetY, 0, nil)
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	if updated.MonsterHit != nil {
		t.Fatalf("MonsterHit = %+v, want nil on empty target", updated.MonsterHit)
	}
}

func TestCastSkillSpiritFireCanMissFarWithoutTarget(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	w.mu.Lock()
	w.monsters = map[string]*Monster{}
	w.occupied = map[monsterPosition]string{}
	w.mu.Unlock()
	caster.Skills = storage.SkillStates{{ID: "灵魂火符", Level: 0, Train: 0}}
	caster.EquippedItems = map[int]storage.UserItem{SlotBujuk: {ItemID: "护身符", Dura: 10000}}
	caster.MP = 100
	updated, err := w.CastSkillWithPlayers(caster, "灵魂火符", caster.X+8, caster.Y, 0, nil)
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	if updated.MonsterHit != nil || len(updated.CharacterHits) > 0 {
		t.Fatalf("updated result = %+v, want a miss with no hit", updated)
	}
}

func TestCastSkillLightningUsesMonsterMagicDefense(t *testing.T) {
	prepare := func(magicDefense int) (*World, storage.Character, string) {
		w, caster := newTestWorldCharacter(t)
		w.mu.Lock()
		w.monsters = map[string]*Monster{}
		w.occupied = map[monsterPosition]string{}
		w.mu.Unlock()
		w.rand = rand.New(rand.NewSource(1))
		mapID := caster.MapID
		targetX, targetY := caster.X+1, caster.Y
		if !w.data.Maps[mapID].Walkable(targetX, targetY) {
			t.Fatal("expected walkable tile for lightning magic defense test")
		}
		result, err := w.SpawnMonsterByNameAt(mapID, targetX, targetY, "鸡", 1)
		if err != nil {
			t.Fatalf("SpawnMonsterByNameAt() error = %v", err)
		}
		if len(result.Monsters) != 1 {
			t.Fatalf("SpawnMonsterByNameAt() monsters = %d, want 1", len(result.Monsters))
		}
		monID := result.Monsters[0].ID
		w.mu.Lock()
		if mon := w.monsters[monID]; mon != nil {
			mon.MagicDefense = magicDefense
			mon.Defense = 255
		}
		w.mu.Unlock()
		caster.Skills = storage.SkillStates{{ID: "雷电术", Level: 0, Train: 0}}
		caster.MP = 100
		return w, caster, monID
	}

	baseW, baseCaster, baseMonID := prepare(0)
	baseResult, err := baseW.CastSkillWithPlayers(baseCaster, "雷电术", baseCaster.X+1, baseCaster.Y, MonsterActorID(Monster{ID: baseMonID}), nil)
	if err != nil {
		t.Fatalf("base CastSkillWithPlayers() error = %v", err)
	}
	baseTick, err := baseW.Tick([]PlayerSnapshot{{Character: baseResult.Character}}, time.Now().Add(2*time.Second))
	if err != nil {
		t.Fatalf("base Tick() error = %v", err)
	}
	if len(baseTick.MonsterHits) != 1 || baseTick.MonsterHits[0].Damage <= 0 {
		t.Fatalf("base lightning hits = %+v, want one positive hit", baseTick.MonsterHits)
	}

	highW, highCaster, highMonID := prepare(10)
	highResult, err := highW.CastSkillWithPlayers(highCaster, "雷电术", highCaster.X+1, highCaster.Y, MonsterActorID(Monster{ID: highMonID}), nil)
	if err != nil {
		t.Fatalf("high CastSkillWithPlayers() error = %v", err)
	}
	highTick, err := highW.Tick([]PlayerSnapshot{{Character: highResult.Character}}, time.Now().Add(2*time.Second))
	if err != nil {
		t.Fatalf("high Tick() error = %v", err)
	}
	if len(highTick.MonsterHits) != 0 {
		t.Fatalf("high lightning hits = %d, want none after full magic defense", len(highTick.MonsterHits))
	}
}

func TestCastSkillLightningAddsUndeadMultiplier(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	w.mu.Lock()
	w.monsters = map[string]*Monster{}
	w.occupied = map[monsterPosition]string{}
	w.rand = rand.New(zeroSource{})
	w.mu.Unlock()
	caster.Skills = storage.SkillStates{{ID: "雷电术", Level: 0, Train: 0}}
	caster.MP = 100
	targetX, targetY := caster.X+1, caster.Y
	spawned, err := w.SpawnMonsterByNameAt(caster.MapID, targetX, targetY, "鸡", 1)
	if err != nil || len(spawned.Monsters) != 1 {
		t.Fatalf("SpawnMonsterByNameAt() = %+v, %v", spawned.Monsters, err)
	}
	monID := spawned.Monsters[0].ID
	w.mu.Lock()
	mon := w.monsters[monID]
	mon.Undead = 1
	mon.MagicDefense = 0
	w.mu.Unlock()
	baseDamage := w.spellMonsterDamageLocked(caster, w.data.Skills["雷电术"], caster.Skills[0])
	cast, err := w.DoSpell(caster, "雷电术", targetX, targetY, MonsterActorID(*mon), nil)
	if err != nil {
		t.Fatalf("DoSpell() error = %v", err)
	}
	wantDamage := int(math.Round(float64(baseDamage) * 1.5))
	if len(w.pendingSpells) != 1 || w.pendingSpells[0].Damage != wantDamage {
		t.Fatalf("pending lightning damage = %+v, want %d with undead multiplier", w.pendingSpells, wantDamage)
	}
	if cast.MagicTargetID != MonsterActorID(*mon) {
		t.Fatalf("magic target = %d, want %d", cast.MagicTargetID, MonsterActorID(*mon))
	}
}

func TestCastSkillSpiritFireHitsCharacterTarget(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	w.mu.Lock()
	w.monsters = map[string]*Monster{}
	w.occupied = map[monsterPosition]string{}
	w.mu.Unlock()
	mapID := caster.MapID
	mp := w.data.Maps[mapID]
	targetX, targetY := -1, -1
	for dx := 2; dx < 10 && targetX < 0; dx++ {
		for dy := -2; dy <= 2; dy++ {
			tx := caster.X + dx
			ty := caster.Y + dy
			if !mp.Walkable(tx, ty) {
				continue
			}
			targetX, targetY = tx, ty
			break
		}
	}
	if targetX < 0 {
		t.Fatal("could not find clear tile for spirit fire test")
	}
	target, err := w.CreateCharacterWithAppearance("test2", "target", "warrior", 0, 0, mapID, targetX, targetY)
	if err != nil {
		t.Fatalf("CreateCharacterWithAppearance() error = %v", err)
	}
	caster.Skills = storage.SkillStates{{ID: "灵魂火符", Level: 0, Train: 0}}
	caster.EquippedItems = map[int]storage.UserItem{SlotBujuk: {ItemID: "护身符", Dura: 10000}}
	caster.MP = 100
	updated, err := w.CastSkillWithPlayers(caster, "灵魂火符", targetX-1, targetY, CharacterActorID(target), []storage.Character{target})
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	w.mu.Lock()
	if len(w.pendingSpells) != 1 || w.pendingSpells[0].TargetX != target.X || w.pendingSpells[0].TargetY != target.Y {
		w.mu.Unlock()
		t.Fatalf("pending spell target = %+v, want target coordinates (%d,%d)", w.pendingSpells, target.X, target.Y)
	}
	w.mu.Unlock()
	tick, err := w.Tick([]PlayerSnapshot{{Character: updated.Character}, {Character: target}}, time.Now().Add(2*time.Second))
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if len(tick.CharacterHits) != 1 {
		t.Fatalf("CharacterHits = %d, want 1", len(tick.CharacterHits))
	}
	hit := tick.CharacterHits[0]
	if hit.Character.ID != target.ID {
		t.Fatalf("hit.Character.ID = %q, want %q", hit.Character.ID, target.ID)
	}
	if hit.Damage <= 0 {
		t.Fatalf("hit = %+v, want positive damage", hit)
	}
}

func TestCastSkillMindRevelationSelectsTarget(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	mapID, x, y := caster.MapID, caster.X, caster.Y
	target, err := w.CreateCharacterWithAppearance("test2", "target", "warrior", 0, 0, mapID, x+1, y)
	if err != nil {
		t.Fatalf("CreateCharacterWithAppearance() error = %v", err)
	}
	caster.Skills = storage.SkillStates{{ID: "心灵启示", Level: 0, Train: 0}}
	updated, err := w.CastSkillWithPlayers(caster, "心灵启示", x+1, y, CharacterActorID(target), []storage.Character{target})
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	if len(updated.AffectedCharacters) != 0 {
		t.Fatalf("AffectedCharacters = %d, want 0 before delayed open", len(updated.AffectedCharacters))
	}
	w.mu.Lock()
	if len(w.pendingSpells) != 1 {
		t.Fatalf("pending spells = %d, want 1", len(w.pendingSpells))
	}
	pending := w.pendingSpells[0]
	w.mu.Unlock()
	if pending.TargetCharacterID != target.ID || pending.ShowHealthDuration <= 0 {
		t.Fatalf("pending show health = %+v, want target %q and positive duration", pending, target.ID)
	}
	if pending.DueAt.Before(pending.ShowHealthStartedAt.Add(1500 * time.Millisecond)) {
		t.Fatalf("pending due time = %v, want at least 1.5s after start %v", pending.DueAt, pending.ShowHealthStartedAt)
	}
	if target.ShowHPUntil != 0 || target.ShowHPOpenAt != 0 {
		t.Fatalf("target show HP state = %+v, want unchanged before delayed open", target)
	}
	tick, err := w.Tick([]PlayerSnapshot{{Character: target}}, pending.DueAt.Add(time.Millisecond))
	if err != nil {
		t.Fatalf("World.Tick() error = %v", err)
	}
	if len(tick.ShowHPOpenedCharacters) != 1 {
		t.Fatalf("ShowHPOpenedCharacters = %+v, want 1", tick.ShowHPOpenedCharacters)
	}
	opened := tick.ShowHPOpenedCharacters[0]
	wantUntil := pending.ShowHealthStartedAt.Add(pending.ShowHealthDuration).UnixNano()
	if opened.ShowHPUntil != wantUntil {
		t.Fatalf("ShowHPUntil = %d, want cast start plus duration %d", opened.ShowHPUntil, wantUntil)
	}
}

func TestCastSkillMindRevelationKeepsExpiredStateUntilTick(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	target, err := w.CreateCharacterWithAppearance("test2", "target", "warrior", 0, 0, caster.MapID, caster.X+1, caster.Y)
	if err != nil {
		t.Fatalf("CreateCharacterWithAppearance() error = %v", err)
	}
	target.ShowHPUntil = time.Now().Add(-time.Second).UnixNano()
	caster.Skills = storage.SkillStates{{ID: "心灵启示", Level: 3}}
	caster.MP = 100

	result, err := w.CastSkillWithPlayers(caster, "心灵启示", target.X, target.Y, CharacterActorID(target), []storage.Character{target})
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	if len(result.AffectedCharacters) != 0 || len(result.AffectedMonsters) != 0 {
		t.Fatalf("affected targets = characters:%d monsters:%d, want none before Tick clears state", len(result.AffectedCharacters), len(result.AffectedMonsters))
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.pendingSpells) != 0 {
		t.Fatalf("pending show health spells = %d, want none while state slot is present", len(w.pendingSpells))
	}
}

func TestCastSkillMindRevelationSelectsMonsterTarget(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	mapID := caster.MapID
	monsters, err := w.SpawnMonsterByNameAt(mapID, caster.X+1, caster.Y, "黑色恶蛆1", 1)
	if err != nil {
		t.Fatalf("SpawnMonsterByNameAt() error = %v", err)
	}
	if len(monsters.Monsters) != 1 {
		t.Fatalf("SpawnMonsterByNameAt() monsters = %d, want 1", len(monsters.Monsters))
	}
	caster.Level = 100
	caster.Skills = storage.SkillStates{{ID: "心灵启示", Level: 3}}
	caster.MP = 100
	updated, err := w.CastSkillWithPlayers(caster, "心灵启示", caster.X+1, caster.Y, MonsterActorID(monsters.Monsters[0]), nil)
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	if updated.MagicTargetID != MonsterActorID(monsters.Monsters[0]) {
		t.Fatalf("MagicTargetID = %d, want %d", updated.MagicTargetID, MonsterActorID(monsters.Monsters[0]))
	}
	w.mu.Lock()
	mon := *w.monsters[monsters.Monsters[0].ID]
	if len(w.pendingSpells) != 1 {
		w.mu.Unlock()
		t.Fatalf("pending spells = %d, want 1", len(w.pendingSpells))
	}
	pending := w.pendingSpells[0]
	w.mu.Unlock()
	if pending.TargetMonsterID != mon.ID || pending.ShowHealthDuration <= 0 {
		t.Fatalf("pending show health = %+v, want monster %q and positive duration", pending, mon.ID)
	}
	if mon.ShowHPOpenAt != 0 || mon.ShowHPDuration != 0 || mon.ShowHPUntil != 0 {
		t.Fatalf("monster show HP state = %+v, want unchanged before delayed open", mon)
	}
	tick, err := w.Tick(nil, pending.DueAt.Add(time.Millisecond))
	if err != nil {
		t.Fatalf("World.Tick() error = %v", err)
	}
	if len(tick.ShowHPOpenedMonsters) != 1 || tick.ShowHPOpenedMonsters[0].ID != mon.ID {
		t.Fatalf("ShowHPOpenedMonsters = %+v, want %s", tick.ShowHPOpenedMonsters, mon.ID)
	}
	opened := tick.ShowHPOpenedMonsters[0]
	expiresAt := time.Unix(0, opened.ShowHPUntil)
	tick, err = w.Tick(nil, expiresAt.Add(time.Millisecond))
	if err != nil {
		t.Fatalf("World.Tick() expiration error = %v", err)
	}
	if len(tick.ShowHPExpiredMonsters) != 1 || tick.ShowHPExpiredMonsters[0].ID != mon.ID {
		t.Fatalf("ShowHPExpiredMonsters = %+v, want %s", tick.ShowHPExpiredMonsters, mon.ID)
	}
}

func TestCastSkillMindRevelationQueuesRepeatedDelayedOpens(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	mapID, x, y := caster.MapID, caster.X, caster.Y
	target, err := w.CreateCharacterWithAppearance("test2", "target", "warrior", 0, 0, mapID, x+1, y)
	if err != nil {
		t.Fatalf("CreateCharacterWithAppearance() error = %v", err)
	}
	caster.Level = 100
	caster.MP = 1000
	caster.Skills = storage.SkillStates{{ID: "心灵启示", Level: 10}}
	for i := 0; i < 2; i++ {
		if _, err := w.CastSkillWithPlayers(caster, "心灵启示", target.X, target.Y, CharacterActorID(target), []storage.Character{target}); err != nil {
			t.Fatalf("CastSkillWithPlayers() #%d error = %v", i+1, err)
		}
	}
	w.mu.Lock()
	var pending []pendingSpell
	for _, spell := range w.pendingSpells {
		if spell.TargetCharacterID == target.ID && spell.ShowHealthDuration > 0 {
			pending = append(pending, spell)
		}
	}
	w.mu.Unlock()
	if len(pending) != 2 {
		t.Fatalf("pending show health spells = %d, want 2", len(pending))
	}
	if !pending[1].DueAt.After(pending[0].ShowHealthStartedAt) {
		t.Fatalf("second pending spell = %+v, want delayed after its cast", pending[1])
	}
	firstTick, err := w.Tick([]PlayerSnapshot{{Character: target}}, pending[0].DueAt)
	if err != nil {
		t.Fatalf("World.Tick() first open error = %v", err)
	}
	if len(firstTick.ShowHPOpenedCharacters) != 1 {
		t.Fatalf("first ShowHPOpenedCharacters = %d, want 1", len(firstTick.ShowHPOpenedCharacters))
	}
	secondTick, err := w.Tick([]PlayerSnapshot{{Character: firstTick.ShowHPOpenedCharacters[0]}}, pending[1].DueAt.Add(time.Millisecond))
	if err != nil {
		t.Fatalf("World.Tick() second open error = %v", err)
	}
	if len(secondTick.ShowHPOpenedCharacters) != 1 {
		t.Fatalf("second ShowHPOpenedCharacters = %d, want 1", len(secondTick.ShowHPOpenedCharacters))
	}
}

func TestCastSkillHellfireHitsLineTargets(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	w.mu.Lock()
	w.monsters = map[string]*Monster{}
	w.occupied = map[monsterPosition]string{}
	w.mu.Unlock()
	mapID := caster.MapID
	mp := w.data.Maps[mapID]
	targetX, targetY := -1, -1
	for dx := 5; dx < 14 && targetX < 0; dx++ {
		tx := caster.X + dx
		ty := caster.Y
		clear := true
		if !mp.Walkable(tx, ty) {
			continue
		}
		for step := 1; step <= 5; step++ {
			if !mp.Walkable(caster.X+step, caster.Y) {
				clear = false
				break
			}
		}
		if clear {
			targetX, targetY = tx, ty
			break
		}
	}
	if targetX < 0 {
		t.Fatal("could not find clear line for hellfire test")
	}
	result, err := w.SpawnMonsterByNameAt(mapID, caster.X+1, caster.Y, "鸡", 1)
	if err != nil {
		t.Fatalf("SpawnMonsterByNameAt() error = %v", err)
	}
	if len(result.Monsters) != 1 {
		t.Fatalf("SpawnMonsterByNameAt() monsters = %d, want 1", len(result.Monsters))
	}
	second, err := w.SpawnMonsterByNameAt(mapID, caster.X+4, caster.Y, "鸡", 1)
	if err != nil {
		t.Fatalf("SpawnMonsterByNameAt() second error = %v", err)
	}
	if len(second.Monsters) != 1 {
		t.Fatalf("SpawnMonsterByNameAt() second monsters = %d, want 1", len(second.Monsters))
	}
	caster.Skills = storage.SkillStates{{ID: "地狱火", Level: 0, Train: 0}}
	caster.MP = 100
	updated, err := w.CastSkillWithPlayers(caster, "地狱火", targetX, targetY, 0, nil)
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	if len(updated.MonsterHits) != 0 {
		t.Fatalf("immediate MonsterHits = %d, want deferred hits", len(updated.MonsterHits))
	}
	magicFireFound := false
	wantDir := direction(caster.X, caster.Y, targetX, targetY)
	wantX := caster.X + dirOffsets[wantDir][0]*5
	wantY := caster.Y + dirOffsets[wantDir][1]*5
	for _, event := range updated.Events {
		if event.Kind == SpellEventMagicFire {
			magicFireFound = true
			if event.TargetX != wantX || event.TargetY != wantY {
				t.Fatalf("magic fire target = (%d,%d), want line endpoint (%d,%d)", event.TargetX, event.TargetY, wantX, wantY)
			}
		}
	}
	if !magicFireFound {
		t.Fatal("missing line magic fire event")
	}
	tick, err := w.Tick([]PlayerSnapshot{{Character: updated.Character}}, time.Now().Add(2*time.Second))
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if len(tick.MonsterHits) != 2 {
		t.Fatalf("deferred MonsterHits = %d, want 2", len(tick.MonsterHits))
	}
	for _, hit := range tick.MonsterHits {
		if hit.Damage <= 0 {
			t.Fatalf("hit = %+v, want positive damage", hit)
		}
	}
}

func TestCastSkillExplosionHitsMultipleMonsters(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	w.mu.Lock()
	w.monsters = map[string]*Monster{}
	w.occupied = map[monsterPosition]string{}
	w.mu.Unlock()
	mapID := caster.MapID
	mp := w.data.Maps[mapID]
	targetX, targetY := -1, -1
	for dx := 4; dx < 12 && targetX < 0; dx++ {
		tx := caster.X + dx
		ty := caster.Y
		if !mp.Walkable(tx, ty) || !mp.Walkable(tx-1, ty) || !mp.Walkable(tx, ty+1) {
			continue
		}
		targetX, targetY = tx, ty
	}
	if targetX < 0 {
		t.Fatal("could not find clear tile for explosion test")
	}
	left, err := w.SpawnMonsterByNameAt(mapID, targetX-1, targetY, "鸡", 1)
	if err != nil {
		t.Fatalf("SpawnMonsterByNameAt() left error = %v", err)
	}
	if len(left.Monsters) != 1 {
		t.Fatalf("SpawnMonsterByNameAt() left monsters = %d, want 1", len(left.Monsters))
	}
	bottom, err := w.SpawnMonsterByNameAt(mapID, targetX, targetY+1, "鸡", 1)
	if err != nil {
		t.Fatalf("SpawnMonsterByNameAt() bottom error = %v", err)
	}
	if len(bottom.Monsters) != 1 {
		t.Fatalf("SpawnMonsterByNameAt() bottom monsters = %d, want 1", len(bottom.Monsters))
	}
	caster.Skills = storage.SkillStates{{ID: "爆裂火焰", Level: 0, Train: 0}}
	caster.MP = 100
	updated, err := w.CastSkillWithPlayers(caster, "爆裂火焰", targetX, targetY, 0, nil)
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	if len(updated.MonsterHits) != 0 || len(updated.Impacts) != 2 || len(w.pendingSpells) != 0 {
		t.Fatalf("explosion cast result = hits:%d impacts:%d pending:%d, want 0, 2 and 0", len(updated.MonsterHits), len(updated.Impacts), len(w.pendingSpells))
	}
	for _, impact := range updated.Impacts {
		if impact.MonsterHit == nil || impact.MonsterHit.Damage <= 0 {
			t.Fatalf("impact = %+v, want positive monster damage", impact)
		}
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, id := range []string{left.Monsters[0].ID, bottom.Monsters[0].ID} {
		if mon := w.monsters[id]; mon != nil && mon.TargetCharacterID != "" {
			t.Fatalf("explosion magic changed monster target = %q, want empty", mon.TargetCharacterID)
		}
	}
}

func TestCastSkillSingleTargetMonsterSpellsUseReferenceMagicResistance(t *testing.T) {
	for _, skillID := range []string{"火球术", "灵魂火符", "雷电术"} {
		t.Run(skillID, func(t *testing.T) {
			w, caster := newTestWorldCharacter(t)
			spawned, err := w.SpawnMonsterByNameAt(caster.MapID, caster.X+1, caster.Y, "鸡", 1)
			if err != nil {
				t.Fatalf("SpawnMonsterByNameAt() error = %v", err)
			}
			mon := spawned.Monsters[0]
			w.mu.Lock()
			w.monsters[mon.ID].AntiMagic = 10
			w.rand = rand.New(zeroSource{})
			w.mu.Unlock()
			caster.Skills = storage.SkillStates{{ID: skillID, Level: 0, Train: 0}}
			if skillID == "灵魂火符" {
				caster.EquippedItems = map[int]storage.UserItem{SlotBujuk: {ItemID: "护身符", Dura: 10000}}
			}
			caster.MP = 100
			result, err := w.DoSpell(caster, skillID, mon.X, mon.Y, MonsterActorID(mon), nil)
			if err != nil {
				t.Fatalf("DoSpell() error = %v", err)
			}
			if result.MagicTargetID != 0 || len(w.pendingSpells) != 0 {
				t.Fatalf("resisted %s result = %+v, pending=%d; want no hit", skillID, result, len(w.pendingSpells))
			}
		})
	}
}

func TestCastSkillLightningLineHitsMonstersAndCharacter(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	w.mu.Lock()
	w.monsters = map[string]*Monster{}
	w.occupied = map[monsterPosition]string{}
	w.mu.Unlock()
	mapID := caster.MapID
	mp := w.data.Maps[mapID]
	targetX, targetY := -1, -1
	for dx := 8; dx < 16 && targetX < 0; dx++ {
		tx := caster.X + dx
		ty := caster.Y
		clear := true
		for step := 1; step <= skillLightningRange; step++ {
			if !mp.Walkable(caster.X+step, caster.Y) {
				clear = false
				break
			}
		}
		if !clear || !mp.Walkable(tx, ty) {
			continue
		}
		targetX, targetY = tx, ty
	}
	if targetX < 0 {
		t.Fatal("could not find clear line for lightning test")
	}
	first, err := w.SpawnMonsterByNameAt(mapID, caster.X+1, caster.Y, "鸡", 1)
	if err != nil {
		t.Fatalf("SpawnMonsterByNameAt() first error = %v", err)
	}
	if len(first.Monsters) != 1 {
		t.Fatalf("SpawnMonsterByNameAt() first monsters = %d, want 1", len(first.Monsters))
	}
	second, err := w.SpawnMonsterByNameAt(mapID, caster.X+4, caster.Y, "鸡", 1)
	if err != nil {
		t.Fatalf("SpawnMonsterByNameAt() second error = %v", err)
	}
	if len(second.Monsters) != 1 {
		t.Fatalf("SpawnMonsterByNameAt() second monsters = %d, want 1", len(second.Monsters))
	}
	target, err := w.CreateCharacterWithAppearance("test2", "target", "warrior", 0, 0, mapID, caster.X+6, caster.Y)
	if err != nil {
		t.Fatalf("CreateCharacterWithAppearance() error = %v", err)
	}
	caster.Skills = storage.SkillStates{{ID: "疾光电影", Level: 0, Train: 0}}
	caster.MP = 100
	w.rand = rand.New(&seqSource{vals: []int64{1 << 62}})
	updated, err := w.CastSkillWithPlayers(caster, "疾光电影", targetX, targetY, CharacterActorID(target), []storage.Character{target})
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	if len(updated.MonsterHits) != 0 || len(updated.CharacterHits) != 0 {
		t.Fatalf("immediate hits = monsters:%d characters:%d, want deferred hits", len(updated.MonsterHits), len(updated.CharacterHits))
	}
	w.mu.Lock()
	lineDamages := make([]int, 0, 2)
	for _, pending := range w.pendingSpells {
		if pending.TargetMonsterID != "" {
			lineDamages = append(lineDamages, pending.Damage)
		}
	}
	w.mu.Unlock()
	if len(lineDamages) < 2 || lineDamages[1] <= lineDamages[0] {
		t.Fatalf("line monster damages = %v, want cumulative undead amplification", lineDamages)
	}
	tick, err := w.Tick([]PlayerSnapshot{{Character: updated.Character}, {Character: target}}, time.Now().Add(2*time.Second))
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if len(tick.MonsterHits) != 2 {
		t.Fatalf("deferred MonsterHits = %d, want 2", len(tick.MonsterHits))
	}
	spellCharacterHits := 0
	for _, hit := range tick.CharacterHits {
		if hit.AttackerID == caster.ID {
			spellCharacterHits++
		}
	}
	if spellCharacterHits != 1 {
		t.Fatalf("deferred spell CharacterHits = %d, want 1 (all hits: %+v)", spellCharacterHits, tick.CharacterHits)
	}
	for _, hit := range tick.MonsterHits {
		if hit.Damage <= 0 {
			t.Fatalf("monster hit = %+v, want positive damage", hit)
		}
	}
	if tick.CharacterHits[0].Damage <= 0 {
		t.Fatalf("character hit = %+v, want positive damage", tick.CharacterHits[0])
	}
}

func TestCastSkillLightningLineUsesMonsterMagicDefense(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	w.mu.Lock()
	w.monsters = map[string]*Monster{}
	w.occupied = map[monsterPosition]string{}
	w.mu.Unlock()
	targetX, targetY := caster.X+1, caster.Y
	spawned, err := w.SpawnMonsterByNameAt(caster.MapID, targetX, targetY, "鸡", 1)
	if err != nil || len(spawned.Monsters) != 1 {
		t.Fatalf("SpawnMonsterByNameAt() = %+v, %v", spawned.Monsters, err)
	}
	monID := spawned.Monsters[0].ID
	w.mu.Lock()
	w.monsters[monID].MagicDefense = 100000
	w.monsters[monID].MagicDefenseMax = 100000
	w.rand = rand.New(zeroSource{})
	w.mu.Unlock()
	caster.Skills = storage.SkillStates{{ID: "疾光电影", Level: 0, Train: 0}}
	caster.MP = 100
	cast, err := w.DoSpell(caster, "疾光电影", targetX, targetY, MonsterActorID(Monster{ID: monID}), nil)
	if err != nil {
		t.Fatalf("DoSpell() error = %v", err)
	}
	tick, err := w.Tick([]PlayerSnapshot{{Character: cast.Character}}, time.Now().Add(time.Second))
	if err != nil {
		t.Fatalf("World.Tick() error = %v", err)
	}
	if len(tick.MonsterHits) != 0 {
		t.Fatalf("linear lightning hits = %d, want none after magic defense", len(tick.MonsterHits))
	}
}

func TestCastSkillLightningLinePreservesResolvedMagicTarget(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	spawned, err := w.SpawnMonsterByNameAt(caster.MapID, caster.X+1, caster.Y, "鸡", 1)
	if err != nil || len(spawned.Monsters) != 1 {
		t.Fatalf("SpawnMonsterByNameAt() = %+v, %v", spawned.Monsters, err)
	}
	monster := spawned.Monsters[0]
	caster.Skills = storage.SkillStates{{ID: "疾光电影", Level: 0, Train: 0}}
	caster.MP = 100
	result, err := w.DoSpell(caster, "疾光电影", monster.X, monster.Y, MonsterActorID(monster), nil)
	if err != nil {
		t.Fatalf("DoSpell() error = %v", err)
	}
	if result.MagicTargetID != MonsterActorID(monster) {
		t.Fatalf("MagicTargetID = %d, want %d", result.MagicTargetID, MonsterActorID(monster))
	}
}

func TestCastSkillElectricBlizzardHitsCenterArea(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	w.mu.Lock()
	w.monsters = map[string]*Monster{}
	w.occupied = map[monsterPosition]string{}
	w.mu.Unlock()
	mapID := caster.MapID
	mp := w.data.Maps[mapID]
	if !mp.Walkable(caster.X+1, caster.Y) || !mp.Walkable(caster.X-1, caster.Y) || !mp.Walkable(caster.X, caster.Y+1) {
		t.Fatal("expected walkable tiles around caster for blizzard test")
	}
	undead, err := w.SpawnMonsterByNameAt(mapID, caster.X+1, caster.Y, "僵尸", 1)
	if err != nil {
		t.Fatalf("SpawnMonsterByNameAt() undead error = %v", err)
	}
	if len(undead.Monsters) != 1 {
		t.Fatalf("SpawnMonsterByNameAt() undead monsters = %d, want 1", len(undead.Monsters))
	}
	normal, err := w.SpawnMonsterByNameAt(mapID, caster.X-1, caster.Y, "鸡", 1)
	if err != nil {
		t.Fatalf("SpawnMonsterByNameAt() normal error = %v", err)
	}
	if len(normal.Monsters) != 1 {
		t.Fatalf("SpawnMonsterByNameAt() normal monsters = %d, want 1", len(normal.Monsters))
	}
	target, err := w.CreateCharacterWithAppearance("test2", "target", "warrior", 0, 0, mapID, caster.X, caster.Y+1)
	if err != nil {
		t.Fatalf("CreateCharacterWithAppearance() error = %v", err)
	}
	caster.Level = 100
	caster.Skills = storage.SkillStates{{ID: "地狱雷光", Level: 10, Train: 0}}
	caster.MP = 500
	updated, err := w.CastSkillWithPlayers(caster, "地狱雷光", caster.X, caster.Y, CharacterActorID(target), []storage.Character{caster, target})
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	if len(updated.MonsterHits) != 0 || len(updated.Impacts) != 3 || len(w.pendingSpells) != 0 {
		t.Fatalf("blizzard cast result = hits:%d impacts:%d pending:%d, want 0, 3 and 0", len(updated.MonsterHits), len(updated.Impacts), len(w.pendingSpells))
	}
	undeadHit := false
	for _, impact := range updated.Impacts {
		if impact.MonsterHit != nil && impact.MonsterHit.MonsterID == undead.Monsters[0].ID && impact.MonsterHit.Damage > 0 {
			undeadHit = true
		}
	}
	if !undeadHit {
		t.Fatalf("undead monster hit missing from impacts = %+v", updated.Impacts)
	}
}

func TestCastSkillElectricBlizzardUsesMonsterMagicDefense(t *testing.T) {
	prepare := func(magicDefense int) (*World, storage.Character, string) {
		w, caster := newTestWorldCharacter(t)
		w.mu.Lock()
		w.monsters = map[string]*Monster{}
		w.occupied = map[monsterPosition]string{}
		w.mu.Unlock()
		w.rand = rand.New(rand.NewSource(1))
		mapID := caster.MapID
		targetX, targetY := caster.X+1, caster.Y
		if !w.data.Maps[mapID].Walkable(targetX, targetY) {
			t.Fatal("expected walkable tile for blizzard magic defense test")
		}
		result, err := w.SpawnMonsterByNameAt(mapID, targetX, targetY, "僵尸", 1)
		if err != nil {
			t.Fatalf("SpawnMonsterByNameAt() error = %v", err)
		}
		if len(result.Monsters) != 1 {
			t.Fatalf("SpawnMonsterByNameAt() monsters = %d, want 1", len(result.Monsters))
		}
		monID := result.Monsters[0].ID
		w.mu.Lock()
		if mon := w.monsters[monID]; mon != nil {
			mon.MagicDefense = magicDefense
		}
		w.mu.Unlock()
		caster.Level = 100
		caster.Skills = storage.SkillStates{{ID: "地狱雷光", Level: 10, Train: 0}}
		caster.MP = 500
		return w, caster, monID
	}

	baseW, baseCaster, baseMonsterID := prepare(0)
	baseResult, err := baseW.CastSkillWithPlayers(baseCaster, "地狱雷光", baseCaster.X, baseCaster.Y, 0, nil)
	if err != nil {
		t.Fatalf("base CastSkillWithPlayers() error = %v", err)
	}
	baseHit := 0
	for _, impact := range baseResult.Impacts {
		if impact.MonsterHit != nil && impact.MonsterHit.MonsterID == baseMonsterID {
			baseHit = impact.MonsterHit.Damage
			break
		}
	}
	if baseHit <= 0 {
		t.Fatalf("base monster hit = %d, want positive", baseHit)
	}
	magicFireIndex := -1
	for index, event := range baseResult.Events {
		if event.Kind == SpellEventMagicFire && magicFireIndex < 0 {
			magicFireIndex = index
		}
	}
	if magicFireIndex < 0 {
		t.Fatalf("electric blizzard events = %+v, want MagicFire", baseResult.Events)
	}

	highW, highCaster, highMonsterID := prepare(10000)
	highCaster.Skills[0].Level = 0
	highResult, err := highW.CastSkillWithPlayers(highCaster, "地狱雷光", highCaster.X, highCaster.Y, 0, nil)
	if err != nil {
		t.Fatalf("high CastSkillWithPlayers() error = %v", err)
	}
	highHit := 0
	for _, impact := range highResult.Impacts {
		if impact.MonsterHit != nil && impact.MonsterHit.MonsterID == highMonsterID {
			highHit = impact.MonsterHit.Damage
			break
		}
	}
	if highHit > baseHit {
		t.Fatalf("high magic defense hit = %d, base = %d, want reduced or equal", highHit, baseHit)
	}
	if highResult.SkillTraining == false {
		t.Fatalf("high magic defense SkillTraining = false, want valid target to train despite zero damage")
	}
}

func TestCastSkillAreaDamagePrecedesMagicFire(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	w.mu.Lock()
	w.monsters = map[string]*Monster{}
	w.occupied = map[monsterPosition]string{}
	w.mu.Unlock()
	if !w.data.Maps[caster.MapID].Walkable(caster.X+1, caster.Y) {
		t.Fatal("expected walkable tile for area damage order test")
	}
	spawned, err := w.SpawnMonsterByNameAt(caster.MapID, caster.X+1, caster.Y, "僵尸", 1)
	if err != nil || len(spawned.Monsters) != 1 {
		t.Fatalf("SpawnMonsterByNameAt() = %+v, %v", spawned, err)
	}
	caster.Skills = storage.SkillStates{{ID: "爆裂火焰", Level: 0, Train: 0}}
	caster.MP = 100
	result, err := w.CastSkillWithPlayers(caster, "爆裂火焰", caster.X+1, caster.Y, 0, nil)
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	impactIndex, fireIndex := -1, -1
	for i, event := range result.Events {
		if event.Kind == SpellEventMonsterHit && impactIndex < 0 {
			impactIndex = i
		}
		if event.Kind == SpellEventMagicFire && fireIndex < 0 {
			fireIndex = i
		}
	}
	if impactIndex < 0 || fireIndex < 0 || impactIndex >= fireIndex {
		t.Fatalf("event order = %+v, want MonsterHit before MagicFire", result.Events)
	}
}

func TestCastSkillExplosionSkipsZeroDamageCharacterImpact(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	mapID := caster.MapID
	target, err := w.CreateCharacterWithAppearance("test2", "target", "wizard", 0, 0, mapID, caster.X+1, caster.Y)
	if err != nil {
		t.Fatalf("CreateCharacterWithAppearance() error = %v", err)
	}
	target.Level = 100
	target.EquippedItems = map[int]storage.UserItem{
		SlotDress:  {ItemID: "天师长袍"},
		SlotHelmet: {ItemID: "道士头盔"},
	}
	target.BubbleDefenceLevel = 255
	target.BubbleDefenceUntil = time.Now().Add(time.Minute).UnixNano()
	caster.Skills = storage.SkillStates{{ID: "爆裂火焰", Level: 0, Train: 0}}
	caster.MP = 100
	result, err := w.CastSkillWithPlayers(caster, "爆裂火焰", target.X, target.Y, 0, []storage.Character{target})
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	for _, impact := range result.Impacts {
		if impact.CharacterHit != nil && impact.CharacterHit.Character.ID == target.ID {
			t.Fatalf("zero-damage character impact = %+v, want no impact", impact)
		}
	}
}

func TestCastSkillGroupHealingKeepsEmptyMagicFireTarget(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	caster.Skills = storage.SkillStates{{ID: "群体治疗术", Level: 0, Train: 0}}
	caster.HP = 1
	caster.MP = 100
	result, err := w.CastSkillWithPlayers(caster, "群体治疗术", caster.X, caster.Y, 0, nil)
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	for _, event := range result.Events {
		if event.Kind == SpellEventMagicFire && event.TargetID != 0 {
			t.Fatalf("group healing MagicFire target = %d, want empty target", event.TargetID)
		}
	}
}

func TestCastFireWallDoesNotReplaceUncollectedExpiredEvent(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	caster.Skills = storage.SkillStates{{ID: "火墙", Level: 0, Train: 0}}
	caster.MP = 100
	now := time.Now()
	w.mu.Lock()
	skill := w.data.Skills["火墙"]
	state := caster.Skills[0]
	key := fireFieldKey{MapID: caster.MapID, X: caster.X, Y: caster.Y}
	w.fireFields[key] = fireField{EventID: 77, MapID: caster.MapID, X: caster.X, Y: caster.Y, OwnerID: caster.ID, ExpiresAt: now.Add(-time.Second)}
	w.groundEvents[77] = SpellGroundEvent{ID: 77, MapID: caster.MapID, X: caster.X, Y: caster.Y, Type: 5, StartAt: now.Add(-time.Second), Duration: time.Second}
	if got := w.castFireWallLocked(caster, skill, state, caster.X, caster.Y, now); got != 1 {
		w.mu.Unlock()
		t.Fatalf("castFireWallLocked() = %d, want reference success value 1", got)
	}
	field := w.fireFields[key]
	w.mu.Unlock()
	if field.EventID != 77 {
		t.Fatalf("expired uncollected event ID = %d, want 77", field.EventID)
	}
}

func TestCastFireWallDoesNotReplaceOtherGroundEvent(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	caster.Skills = storage.SkillStates{{ID: "火墙", Level: 0, Train: 0}}
	now := time.Now()
	w.mu.Lock()
	skill := w.data.Skills["火墙"]
	state := caster.Skills[0]
	centerID := w.nextGroundEventIDLocked()
	w.groundEvents[centerID] = SpellGroundEvent{
		ID: centerID, MapID: caster.MapID, X: caster.X, Y: caster.Y,
		Type: 99, StartAt: now, Duration: time.Minute,
	}
	if got := w.castFireWallLocked(caster, skill, state, caster.X, caster.Y, now); got != 1 {
		w.mu.Unlock()
		t.Fatalf("castFireWallLocked() = %d, want reference success value 1", got)
	}
	_, centerFire := w.fireFields[fireFieldKey{MapID: caster.MapID, X: caster.X, Y: caster.Y}]
	created := 0
	for _, event := range w.groundEvents {
		if event.Type == 5 && event.ID != centerID {
			created++
		}
	}
	w.mu.Unlock()
	if centerFire {
		t.Fatal("fire wall replaced an occupied ground event")
	}
	if created != 4 {
		t.Fatalf("new fire wall events = %d, want 4 around occupied center", created)
	}
}

func TestCastSkillFireWallCreatesRingAndTicksDamage(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	w.mu.Lock()
	w.monsters = map[string]*Monster{}
	w.occupied = map[monsterPosition]string{}
	w.mu.Unlock()
	mapID := caster.MapID
	mp := w.data.Maps[mapID]
	targetX, targetY := -1, -1
	offsets := [][2]int{
		{0, -1}, {-1, 0}, {0, 0}, {1, 0}, {0, 1},
	}
	for dx := 4; dx < 12 && targetX < 0; dx++ {
		tx := caster.X + dx
		ty := caster.Y
		ok := true
		for _, off := range offsets {
			if !mp.Walkable(tx+off[0], ty+off[1]) {
				ok = false
				break
			}
		}
		if ok {
			targetX, targetY = tx, ty
		}
	}
	if targetX < 0 {
		t.Fatal("could not find clear tile for fire wall test")
	}
	monsterResult, err := w.SpawnMonsterByNameAt(mapID, targetX, targetY, "半兽人", 1)
	if err != nil {
		t.Fatalf("SpawnMonsterByNameAt() monster error = %v", err)
	}
	if len(monsterResult.Monsters) != 1 {
		t.Fatalf("SpawnMonsterByNameAt() monsters = %d, want 1", len(monsterResult.Monsters))
	}
	monster := monsterResult.Monsters[0]
	target, err := w.CreateCharacterWithAppearance("test2", "target", "warrior", 0, 0, mapID, targetX, targetY+1)
	if err != nil {
		t.Fatalf("CreateCharacterWithAppearance() error = %v", err)
	}
	caster.Level = 100
	caster.Skills = storage.SkillStates{{ID: "火墙", Level: 10, Train: 0}}
	caster.MP = 500
	updated, err := w.CastSkillWithPlayers(caster, "火墙", targetX, targetY, CharacterActorID(target), []storage.Character{target})
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	if len(w.fireFields) != 5 {
		t.Fatalf("fireFields = %d, want 5", len(w.fireFields))
	}
	if len(w.groundEvents) != 5 {
		t.Fatalf("groundEvents = %d, want 5", len(w.groundEvents))
	}
	if len(updated.GroundEvents) != 5 {
		t.Fatalf("cast GroundEvents = %d, want 5 immediate events", len(updated.GroundEvents))
	}
	for _, event := range w.groundEvents {
		if event.Type != 5 || event.Duration <= 0 || event.StartAt.IsZero() {
			t.Fatalf("fire wall ground event = %+v, want visible fire event", event)
		}
	}
	w.mu.Lock()
	fireSkill, ok := w.data.Skills["火墙"]
	if !ok {
		w.mu.Unlock()
		t.Fatal("火墙 skill not found")
	}
	fireState, _, ok := caster.Skills.Get("火墙")
	if !ok {
		w.mu.Unlock()
		t.Fatal("火墙 skill state not found")
	}
	castAt := time.Now()
	for key, field := range w.fireFields {
		field.ExpiresAt = castAt.Add(time.Minute)
		w.fireFields[key] = field
	}
	if got := w.castFireWallLocked(caster, fireSkill, fireState, target.X, target.Y, castAt); got != 1 {
		w.mu.Unlock()
		t.Fatalf("repeated cast result = %d, want reference success value 1", got)
	}
	w.mu.Unlock()
	if got := len(w.monstersInRadiusLocked(mapID, monster.X, monster.Y, 0)); got != 1 {
		t.Fatalf("monster coverage = %d, want 1 at fire wall cross", got)
	}
	if got := len(w.charactersInRadiusLocked([]storage.Character{updated.Character, target}, mapID, target.X, target.Y, 0)); got != 1 {
		t.Fatalf("character coverage = %d, want 1 at fire wall cross", got)
	}
	fireTickAt := time.Now().Add(fireWallTickInterval + 100*time.Millisecond)
	result, err := w.Tick([]PlayerSnapshot{{Character: updated.Character}, {Character: target}}, fireTickAt)
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if len(result.MonsterHits) == 0 {
		t.Fatal("MonsterHits = 0, want fire wall to damage monster")
	}
	if len(result.CharacterHits) == 0 {
		t.Fatal("CharacterHits = 0, want fire wall to damage character")
	}
	monster.HP = monster.MaxHP
	monster.MasterID = updated.Character.ID
	w.mu.Lock()
	if current := w.monsters[monster.ID]; current != nil {
		current.HP = monster.HP
		current.MasterID = monster.MasterID
	}
	w.mu.Unlock()
	result, err = w.Tick([]PlayerSnapshot{{Character: updated.Character}, {Character: target}}, fireTickAt.Add(100*time.Millisecond))
	if err != nil {
		t.Fatalf("World.Tick() before fire interval error = %v", err)
	}
	if len(result.MonsterHits) != 0 || len(result.CharacterHits) != 0 {
		t.Fatalf("fire wall hits before 3-second interval = monsters %d characters %d, want none", len(result.MonsterHits), len(result.CharacterHits))
	}
	result, err = w.Tick([]PlayerSnapshot{{Character: updated.Character}, {Character: target}}, fireTickAt.Add(3*time.Second))
	if err != nil {
		t.Fatalf("World.Tick() protected monster error = %v", err)
	}
	if len(result.MonsterHits) != 0 {
		t.Fatalf("fire wall hit caster summon = %d, want none", len(result.MonsterHits))
	}
	if len(w.fireFields) != 5 {
		t.Fatalf("fireFields after tick = %d, want 5 while still active", len(w.fireFields))
	}
	result, err = w.Tick([]PlayerSnapshot{{Character: updated.Character}, {Character: target}}, time.Now().Add(2*time.Minute))
	if err != nil {
		t.Fatalf("final Tick() error = %v", err)
	}
	if len(w.fireFields) != 0 {
		t.Fatalf("fireFields after expiry = %d, want 0", len(w.fireFields))
	}
	if len(result.MonsterHits) != 0 && len(result.CharacterHits) != 0 {
		t.Fatalf("unexpected hits after expiry: monsters=%d characters=%d", len(result.MonsterHits), len(result.CharacterHits))
	}
}

func TestFireWallRetainsOwnerQualificationWhenOwnerLeavesSnapshot(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	caster.AttackMode = 1
	now := time.Now()
	spawned, err := w.SpawnMonsterByNameAt(caster.MapID, caster.X, caster.Y, "半兽人", 1)
	if err != nil || len(spawned.Monsters) != 1 {
		t.Fatalf("SpawnMonsterByNameAt() = %+v, %v", spawned.Monsters, err)
	}
	mon := spawned.Monsters[0]
	w.mu.Lock()
	mon.MasterID = caster.ID
	w.monsters[mon.ID].MasterID = caster.ID
	skill := w.data.Skills["火墙"]
	state := storage.SkillState{Level: 0}
	if got := w.castFireWallLocked(caster, skill, state, caster.X, caster.Y, now); got != 1 {
		w.mu.Unlock()
		t.Fatalf("castFireWallLocked() = %d, want 1", got)
	}
	w.mu.Unlock()
	result, err := w.Tick(nil, now.Add(fireWallTickInterval+time.Nanosecond))
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if len(result.MonsterHits) != 0 {
		t.Fatalf("MonsterHits = %d, want none for attack-mode-1 owner's summon", len(result.MonsterHits))
	}
}

func TestFireWallClearsDeadOwnerAfterCurrentTick(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	caster.Level = 100
	now := time.Now()
	spawned, err := w.SpawnMonsterByNameAt(caster.MapID, caster.X, caster.Y+1, "半兽人", 1)
	if err != nil || len(spawned.Monsters) != 1 {
		t.Fatalf("SpawnMonsterByNameAt() = %+v, %v", spawned.Monsters, err)
	}
	mon := spawned.Monsters[0]
	w.mu.Lock()
	w.monsters[mon.ID].MagicDefense = 0
	w.monsters[mon.ID].MagicDefenseMax = 0
	skill := w.data.Skills["火墙"]
	state := storage.SkillState{Level: 10}
	if got := w.castFireWallLocked(caster, skill, state, caster.X, caster.Y, now); got != 1 {
		w.mu.Unlock()
		t.Fatalf("castFireWallLocked() = %d, want 1", got)
	}
	w.mu.Unlock()

	dead := caster
	dead.HP = 0
	result, err := w.Tick([]PlayerSnapshot{{Character: dead}}, now.Add(fireWallTickInterval+time.Nanosecond))
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if len(result.MonsterHits) != 1 {
		t.Fatalf("MonsterHits = %d, want one final hit before owner cleanup", len(result.MonsterHits))
	}
	firstHP := w.monsters[mon.ID].HP
	if firstHP >= mon.HP {
		t.Fatalf("monster HP = %d, want damage from final owner tick", firstHP)
	}
	result, err = w.Tick([]PlayerSnapshot{{Character: dead}}, now.Add(2*fireWallTickInterval+time.Nanosecond))
	if err != nil {
		t.Fatalf("World.Tick() after owner cleanup error = %v", err)
	}
	if len(result.MonsterHits) != 0 {
		t.Fatalf("MonsterHits = %d, want none after owner cleanup", len(result.MonsterHits))
	}
	if monAfter := w.monsters[mon.ID]; monAfter.HP != firstHP {
		t.Fatalf("monster HP = %d, want unchanged %d after owner cleanup", monAfter.HP, firstHP)
	}
}

func TestFireWallTickUsesCreationOrder(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	mapID := caster.MapID
	m1 := &Monster{ID: "fire-m1", Alive: true, MapID: mapID, X: caster.X + 1, Y: caster.Y, HP: 100, MaxHP: 100, Level: 1}
	m2 := &Monster{ID: "fire-m2", Alive: true, MapID: mapID, X: caster.X + 2, Y: caster.Y, HP: 100, MaxHP: 100, Level: 1}
	w.mu.Lock()
	w.monsters = map[string]*Monster{}
	w.occupied = map[monsterPosition]string{}
	w.monsters[m1.ID] = m1
	w.monsters[m2.ID] = m2
	w.fireFields[fireFieldKey{MapID: mapID, X: m1.X, Y: m1.Y}] = fireField{Order: 2, MapID: mapID, X: m1.X, Y: m1.Y, OwnerID: caster.ID, Damage: 3, ExpiresAt: time.Now().Add(time.Minute), NextTick: time.Time{}}
	w.fireFields[fireFieldKey{MapID: mapID, X: m2.X, Y: m2.Y}] = fireField{Order: 1, MapID: mapID, X: m2.X, Y: m2.Y, OwnerID: caster.ID, Damage: 7, ExpiresAt: time.Now().Add(time.Minute), NextTick: time.Time{}}
	monHits, _, _ := w.applyFireWallTickLocked(map[string]storage.Character{caster.ID: caster}, time.Now())
	w.mu.Unlock()
	if len(monHits) != 2 {
		t.Fatalf("fire wall hit count = %d, want 2", len(monHits))
	}
	if monHits[0].MonsterID != m2.ID || monHits[1].MonsterID != m1.ID {
		t.Fatalf("fire wall hit order = %q, %q; want %q, %q", monHits[0].MonsterID, monHits[1].MonsterID, m2.ID, m1.ID)
	}
}

func TestFireWallTickWaitsPastExactInterval(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	mon := &Monster{ID: "fire-exact", Alive: true, MapID: caster.MapID, X: caster.X, Y: caster.Y, HP: 100, MaxHP: 100, Level: 1}
	now := time.Now()
	w.mu.Lock()
	w.monsters = map[string]*Monster{mon.ID: mon}
	w.occupied = map[monsterPosition]string{}
	w.fireFields[fireFieldKey{MapID: mon.MapID, X: mon.X, Y: mon.Y}] = fireField{
		MapID: mon.MapID, X: mon.X, Y: mon.Y, OwnerID: caster.ID, Damage: 3,
		ExpiresAt: now.Add(time.Minute), NextTick: now,
	}
	first, _, _ := w.applyFireWallTickLocked(map[string]storage.Character{caster.ID: caster}, now)
	second, _, _ := w.applyFireWallTickLocked(map[string]storage.Character{caster.ID: caster}, now.Add(time.Nanosecond))
	w.mu.Unlock()
	if len(first) != 0 || len(second) != 1 {
		t.Fatalf("fire wall exact interval hits = %d then %d, want 0 then 1", len(first), len(second))
	}
}

func TestMonsterTargetResolvesPastOutOfRangeEntries(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	first := &Monster{ID: "target-first", Alive: true, MapID: caster.MapID, X: caster.X + 5, Y: caster.Y}
	second := &Monster{ID: "target-second", Alive: true, MapID: caster.MapID, X: caster.X + 1, Y: caster.Y}
	w.mu.Lock()
	w.monsters = map[string]*Monster{first.ID: first, second.ID: second}
	var got *Monster
	for i := 0; i < 50; i++ {
		got = w.monsterTargetLocked(caster.MapID, caster.X, caster.Y, MonsterActorID(*second), 1)
		if got != second {
			break
		}
	}
	w.mu.Unlock()
	if got != second {
		t.Fatalf("monster target = %+v, want %s", got, second.ID)
	}
}

func TestMonsterTargetWithoutActorIDRequiresExactCoordinate(t *testing.T) {
	w := &World{monsters: map[string]*Monster{
		"adjacent": {ID: "adjacent", Alive: true, MapID: "map", X: 11, Y: 10},
		"exact":    {ID: "exact", Alive: true, MapID: "map", X: 10, Y: 10},
	}}
	if got := w.monsterTargetLocked("map", 10, 10, 0, 1); got == nil || got.ID != "exact" {
		t.Fatalf("exact target = %+v, want exact-coordinate monster", got)
	}
	delete(w.monsters, "exact")
	if got := w.monsterTargetLocked("map", 10, 10, 0, 1); got != nil {
		t.Fatalf("adjacent target = %+v, want nil without actor ID", got)
	}
}

func TestCastSkillRepelPushesNearbyTargets(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	w.mu.Lock()
	w.monsters = map[string]*Monster{}
	w.occupied = map[monsterPosition]string{}
	w.mu.Unlock()
	mapID := caster.MapID
	mp := w.data.Maps[mapID]
	if !mp.Walkable(caster.X+1, caster.Y) || !mp.Walkable(caster.X, caster.Y+1) || !mp.Walkable(caster.X+2, caster.Y) || !mp.Walkable(caster.X, caster.Y+2) {
		t.Fatal("expected walkable tiles around caster for repel test")
	}
	target, err := w.CreateCharacterWithAppearance("test2", "target", "warrior", 0, 0, mapID, caster.X+1, caster.Y)
	if err != nil {
		t.Fatalf("CreateCharacterWithAppearance() error = %v", err)
	}
	monsterResult, err := w.SpawnMonsterByNameAt(mapID, caster.X, caster.Y+1, "鸡", 1)
	if err != nil {
		t.Fatalf("SpawnMonsterByNameAt() error = %v", err)
	}
	if len(monsterResult.Monsters) != 1 {
		t.Fatalf("SpawnMonsterByNameAt() monsters = %d, want 1", len(monsterResult.Monsters))
	}
	caster.Level = 100
	caster.Skills = storage.SkillStates{{ID: "抗拒火环", Level: 10, Train: 0}}
	caster.MP = 500
	updated, err := w.CastSkillWithPlayers(caster, "抗拒火环", caster.X, caster.Y, CharacterActorID(target), []storage.Character{target})
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	if len(updated.AffectedCharacters) != 1 {
		t.Fatalf("AffectedCharacters = %d, want 1", len(updated.AffectedCharacters))
	}
	if updated.AffectedCharacters[0].X <= target.X {
		t.Fatalf("affected character = %+v, want pushed away to the east", updated.AffectedCharacters[0])
	}
	if len(updated.CharacterPushes) == 0 {
		t.Fatalf("character push messages = %+v, want at least one", updated.CharacterPushes)
	}
	lastPush := updated.CharacterPushes[len(updated.CharacterPushes)-1].Character
	finalTarget := updated.AffectedCharacters[0]
	if finalTarget.ID != lastPush.ID || finalTarget.X != lastPush.X || finalTarget.Y != lastPush.Y || finalTarget.Dir != lastPush.Dir {
		t.Fatalf("character push final state = %+v, messages = %+v", updated.AffectedCharacters[0], updated.CharacterPushes)
	}
	for i := 1; i < len(updated.CharacterPushes); i++ {
		if updated.CharacterPushes[i].Character.X != updated.CharacterPushes[i-1].Character.X+1 {
			t.Fatalf("character push step %d = %+v, previous = %+v", i, updated.CharacterPushes[i], updated.CharacterPushes[i-1])
		}
	}
	if len(updated.MonsterActions) != 1 {
		t.Fatalf("MonsterActions = %d, want 1", len(updated.MonsterActions))
	}
	if len(updated.MonsterPushes) == 0 {
		t.Fatalf("MonsterPushes = %+v, want at least one", updated.MonsterPushes)
	}
	lastMonsterPush := updated.MonsterPushes[len(updated.MonsterPushes)-1]
	if lastMonsterPush.MonsterID != updated.MonsterActions[0].MonsterID || lastMonsterPush.X != updated.MonsterActions[0].X || lastMonsterPush.Y != updated.MonsterActions[0].Y {
		t.Fatalf("monster push final state = %+v, actions = %+v", lastMonsterPush, updated.MonsterActions)
	}
	for i := 1; i < len(updated.MonsterPushes); i++ {
		dx := abs(updated.MonsterPushes[i].X - updated.MonsterPushes[i-1].X)
		dy := abs(updated.MonsterPushes[i].Y - updated.MonsterPushes[i-1].Y)
		if dx > 1 || dy > 1 || dx+dy == 0 {
			t.Fatalf("monster push step %d = %+v, previous = %+v", i, updated.MonsterPushes[i], updated.MonsterPushes[i-1])
		}
	}
	if updated.MonsterActions[0].X != caster.X || updated.MonsterActions[0].Y <= caster.Y+1 {
		t.Fatalf("monster action = %+v, want pushed farther south", updated.MonsterActions[0])
	}
	if want := (direction(caster.X, caster.Y, updated.MonsterActions[0].X, updated.MonsterActions[0].Y) + 4) % 8; updated.MonsterActions[0].Dir != want {
		t.Fatalf("monster push direction = %d, want %d", updated.MonsterActions[0].Dir, want)
	}
}

func TestCastSkillRepelUsesReferenceSuccessRate(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	w.mu.Lock()
	w.monsters = map[string]*Monster{}
	w.occupied = map[monsterPosition]string{}
	w.mu.Unlock()
	mapID := caster.MapID
	mp := w.data.Maps[mapID]
	if !mp.Walkable(caster.X+1, caster.Y) {
		t.Fatal("expected walkable tile east of caster for repel test")
	}
	target, err := w.CreateCharacterWithAppearance("test2", "target", "warrior", 0, 0, mapID, caster.X+1, caster.Y)
	if err != nil {
		t.Fatalf("CreateCharacterWithAppearance() error = %v", err)
	}
	caster.Level = 11
	caster.Skills = storage.SkillStates{{ID: "抗拒火环", Level: 1, Train: 0}}
	caster.MP = 500
	w.rand = rand.New(&seqSource{vals: []int64{0, 10}})
	updated, err := w.CastSkillWithPlayers(caster, "抗拒火环", caster.X, caster.Y, CharacterActorID(target), []storage.Character{target})
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	if len(updated.AffectedCharacters) != 1 {
		t.Fatalf("AffectedCharacters = %d, want 1", len(updated.AffectedCharacters))
	}
	if updated.AffectedCharacters[0].X != target.X+1 {
		t.Fatalf("affected character = %+v, want pushed east when roll is 10", updated.AffectedCharacters[0])
	}
}

func TestCastSkillRepelConsumesRollBeforeInvalidTargetCheck(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	target, err := w.CreateCharacterWithAppearance("test2", "target", "warrior", 0, 0, caster.MapID, caster.X+1, caster.Y)
	if err != nil {
		t.Fatalf("CreateCharacterWithAppearance() error = %v", err)
	}
	caster.Level = 100
	caster.AttackMode = 1
	caster.Skills = storage.SkillStates{{ID: "抗拒火环", Level: 1, Train: 0}}
	caster.MP = 500
	source := &seqSource{vals: []int64{0}}
	w.rand = rand.New(source)
	result, err := w.CastSkillWithPlayers(caster, "抗拒火环", caster.X, caster.Y, 0, []storage.Character{target})
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	if len(result.CharacterPushes) != 0 {
		t.Fatalf("character pushes = %+v, want none for invalid target", result.CharacterPushes)
	}
	if source.idx != 1 {
		t.Fatalf("random calls = %d, want one roll before target check", source.idx)
	}
}

func TestCastSkillRepelConsumesRollBeforeInvalidMonsterCheck(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	source := &seqSource{vals: []int64{0}}
	w.mu.Lock()
	w.monsters = map[string]*Monster{}
	w.occupied = map[monsterPosition]string{}
	w.rand = rand.New(source)
	w.mu.Unlock()
	spawned, err := w.SpawnMonsterByNameAt(caster.MapID, caster.X+1, caster.Y, "鸡", 1)
	if err != nil {
		t.Fatalf("SpawnMonsterByNameAt() error = %v", err)
	}
	if len(spawned.Monsters) != 1 {
		t.Fatalf("SpawnMonsterByNameAt() monsters = %d, want 1", len(spawned.Monsters))
	}
	w.mu.Lock()
	mon := w.monsters[spawned.Monsters[0].ID]
	mon.AdminMode = true
	w.mu.Unlock()
	caster.Level = 100
	caster.Skills = storage.SkillStates{{ID: "抗拒火环", Level: 1, Train: 0}}
	caster.MP = 500
	result, err := w.CastSkillWithPlayers(caster, "抗拒火环", caster.X, caster.Y, 0, nil)
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	if len(result.MonsterActions) != 0 || result.PushTargets != 0 {
		t.Fatalf("invalid monster push result = %+v, want no push", result)
	}
	if source.idx != 1 {
		t.Fatalf("random calls = %d, want one roll before target check", source.idx)
	}
}

func TestCastSkillRepelTrainsWhenPushIsBlocked(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	w.mu.Lock()
	w.monsters = map[string]*Monster{}
	w.occupied = map[monsterPosition]string{}
	w.mu.Unlock()
	mapID := caster.MapID
	if !w.data.Maps[mapID].Walkable(caster.X+1, caster.Y) || !w.data.Maps[mapID].Walkable(caster.X+2, caster.Y) {
		t.Fatal("expected walkable tiles east of caster for blocked repel test")
	}
	target, err := w.CreateCharacterWithAppearance("test2", "target", "warrior", 0, 0, mapID, caster.X+1, caster.Y)
	if err != nil {
		t.Fatalf("CreateCharacterWithAppearance() error = %v", err)
	}
	if _, err := w.SpawnMonsterByNameAt(mapID, caster.X+2, caster.Y, "鸡", 100); err != nil {
		t.Fatalf("SpawnMonsterByNameAt() error = %v", err)
	}
	caster.Level = 10
	caster.Skills = storage.SkillStates{{ID: "抗拒火环", Level: 1, Train: 0}}
	caster.MP = 500
	w.rand = rand.New(&seqSource{vals: []int64{0, 0}})
	result, err := w.CastSkillWithPlayers(caster, "抗拒火环", caster.X, caster.Y, CharacterActorID(target), []storage.Character{target})
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	if result.PushTargets != 1 {
		t.Fatalf("PushTargets = %d, want one valid blocked target", result.PushTargets)
	}
	if len(result.CharacterPushes) != 0 || len(result.MonsterActions) != 0 {
		t.Fatalf("push events = characters %d, monsters %d; want none when blocked", len(result.CharacterPushes), len(result.MonsterActions))
	}
}

func TestCastSkillRepelUsesVisibleObjectOrderForMixedTargets(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	w.mu.Lock()
	w.monsters = map[string]*Monster{}
	w.occupied = map[monsterPosition]string{}
	w.mu.Unlock()
	mapID := caster.MapID
	if !w.data.Maps[mapID].Walkable(caster.X+1, caster.Y) || !w.data.Maps[mapID].Walkable(caster.X, caster.Y+1) {
		t.Fatal("expected walkable adjacent tiles")
	}
	target, err := w.CreateCharacterWithAppearance("test2", "target", "warrior", 0, 0, mapID, caster.X+1, caster.Y)
	if err != nil {
		t.Fatalf("CreateCharacterWithAppearance() error = %v", err)
	}
	target.ObjectOrder = 10
	spawned, err := w.SpawnMonsterByNameAt(mapID, caster.X, caster.Y+1, "鸡", 1)
	if err != nil {
		t.Fatalf("SpawnMonsterByNameAt() error = %v", err)
	}
	monID := spawned.Monsters[0].ID
	w.mu.Lock()
	w.monsters[monID].ObjectOrder = 20
	w.monsters[monID].Level = 2
	w.rand = rand.New(&seqSource{vals: []int64{0, 0}})
	w.mu.Unlock()
	caster.Level = 2
	caster.Skills = storage.SkillStates{{ID: "抗拒火环", Level: 0, Train: 0}}
	caster.MP = 500
	w.mu.Lock()
	visible := w.visibleSpellAreaTargetsLocked([]storage.Character{target}, mapID, caster.X, caster.Y, 1)
	visibleIDs := make([]string, 0, len(visible))
	for _, areaTarget := range visible {
		_, _, id := areaTarget.CharacterOrMonster()
		visibleIDs = append(visibleIDs, id)
	}
	w.mu.Unlock()
	if len(visibleIDs) != 2 || visibleIDs[0] != target.ID || visibleIDs[1] != monID {
		t.Fatalf("visible target order = %v, want target, monster", visibleIDs)
	}
	result, err := w.CastSkillWithPlayers(caster, "抗拒火环", caster.X, caster.Y, 0, []storage.Character{target})
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	if len(result.AffectedCharacters) != 1 || result.AffectedCharacters[0].ID != target.ID {
		t.Fatalf("affected characters = %+v, want target after visible-order roll", result.AffectedCharacters)
	}
	if len(result.MonsterActions) != 0 {
		t.Fatalf("monster actions = %+v, want none after coordinate-order alternative is rejected", result.MonsterActions)
	}
}

func TestMotaeboConsumesReferenceRollBeforeTargetCheck(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	target, err := w.CreateCharacterWithAppearance("test2", "target", "warrior", 0, 0, caster.MapID, caster.X+1, caster.Y)
	if err != nil {
		t.Fatalf("CreateCharacterWithAppearance() error = %v", err)
	}
	caster.Level = 100
	target.Level = 1
	high := &seqSource{vals: []int64{0}}
	w.rand = rand.New(high)
	if !w.canMotaeboCharacterLocked(caster, target, 10) {
		t.Fatal("canMotaeboCharacterLocked() = false, want success")
	}
	if high.idx != 1 {
		t.Fatalf("high-threshold random calls = %d, want 1", high.idx)
	}

	low := &seqSource{vals: []int64{0}}
	w.rand = rand.New(low)
	if w.canMotaeboCharacterLocked(caster, target, -30) {
		t.Fatal("canMotaeboCharacterLocked() = true, want failure")
	}
	if low.idx != 1 {
		t.Fatalf("low-threshold random calls = %d, want 1", low.idx)
	}
}

func TestCastSkillChargeMovesCasterAndPushesTarget(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	w.mu.Lock()
	w.monsters = map[string]*Monster{}
	w.occupied = map[monsterPosition]string{}
	w.mu.Unlock()
	mapID := caster.MapID
	mp := w.data.Maps[mapID]
	if !mp.Walkable(caster.X+1, caster.Y) || !mp.Walkable(caster.X+2, caster.Y) || !mp.Walkable(caster.X+3, caster.Y) {
		t.Fatal("expected walkable tiles east of caster for charge test")
	}
	target, err := w.CreateCharacterWithAppearance("test2", "target", "warrior", 0, 0, mapID, caster.X+1, caster.Y)
	if err != nil {
		t.Fatalf("CreateCharacterWithAppearance() error = %v", err)
	}
	target.HP = 1000
	caster.Level = 100
	caster.Skills = storage.SkillStates{{ID: "野蛮冲撞", Level: 0, Train: 0}}
	caster.MP = 500
	updated, err := w.CastSkillWithPlayers(caster, "野蛮冲撞", caster.X+2, caster.Y, 0, []storage.Character{target})
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	if updated.Character.X != caster.X+3 || updated.Character.Y != caster.Y {
		t.Fatalf("caster = %+v, want moved three tiles east", updated.Character)
	}
	if len(updated.CharacterHits) != 1 {
		t.Fatalf("CharacterHits = %d, want 1", len(updated.CharacterHits))
	}
	if updated.CharacterHits[0].Character.X != caster.X+4 {
		t.Fatalf("hit character = %+v, want pushed through three collisions", updated.CharacterHits[0].Character)
	}
	if updated.CharacterHits[0].Damage <= 0 {
		t.Fatalf("CharacterHits[0].Damage = %d, want positive charge damage", updated.CharacterHits[0].Damage)
	}
	if updated.CharacterHits[0].Magic {
		t.Fatal("charge CharacterHit.Magic = true, want physical hit")
	}
	if updated.CharacterHits[0].Character.HP >= 1000 {
		t.Fatalf("hit character HP = %d, want reduced by charge", updated.CharacterHits[0].Character.HP)
	}
	orderedKinds := make([]SpellEventKind, 0, len(updated.OrderedEvents))
	for _, event := range updated.OrderedEvents {
		orderedKinds = append(orderedKinds, event.Kind)
	}
	wantKinds := []SpellEventKind{SpellEventCharacterPush, SpellEventRush, SpellEventCharacterPush, SpellEventRush, SpellEventCharacterPush, SpellEventRush, SpellEventCharacterHit}
	if len(orderedKinds) < len(wantKinds) {
		t.Fatalf("OrderedEvents = %v, want prefix %v", orderedKinds, wantKinds)
	}
	for i, want := range wantKinds {
		if orderedKinds[i] != want {
			t.Fatalf("OrderedEvents[%d] = %v, want %v; all = %v", i, orderedKinds[i], want, orderedKinds)
		}
	}
	var skillExp bool
	for _, event := range updated.Events {
		if event.Kind == SpellEventSkillExp {
			skillExp = true
			if event.SkillLevel != updated.SkillLevel || event.SkillTrain != updated.SkillTrain {
				t.Fatalf("skill exp event = level %d train %d, want level %d train %d", event.SkillLevel, event.SkillTrain, updated.SkillLevel, updated.SkillTrain)
			}
			if event.SkillExpDelay != time.Second {
				t.Fatalf("skill exp delay = %s, want 1s for charge training", event.SkillExpDelay)
			}
		}
	}
	if !skillExp {
		t.Fatalf("Events = %+v, want skill experience event", updated.Events)
	}
	if len(updated.Events) == 0 || updated.Events[0].Kind != SpellEventCasterState {
		t.Fatalf("charge events = %+v, want resource state first", updated.Events)
	}
	for _, event := range updated.Events[1:] {
		if event.Kind == SpellEventCharacter {
			t.Fatalf("charge events = %+v, want no redundant final character event", updated.Events)
		}
	}
}

func TestCastSkillChargePrePushesSecondTileAtHighLevel(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	w.mu.Lock()
	w.monsters = map[string]*Monster{}
	w.occupied = map[monsterPosition]string{}
	w.rand = rand.New(zeroSource{})
	w.mu.Unlock()
	mapID := caster.MapID
	frontX := caster.X + 1
	secondX := caster.X + 2
	if !w.data.Maps[mapID].Walkable(caster.X+7, caster.Y) {
		t.Skip("test map does not provide enough walkable tiles")
	}
	front, err := w.CreateCharacterWithAppearance("test2", "front", "warrior", 0, 0, mapID, frontX, caster.Y)
	if err != nil {
		t.Fatalf("CreateCharacterWithAppearance(front) error = %v", err)
	}
	second, err := w.CreateCharacterWithAppearance("test3", "second", "warrior", 0, 0, mapID, secondX, caster.Y)
	if err != nil {
		t.Fatalf("CreateCharacterWithAppearance(second) error = %v", err)
	}
	caster.Level = 30
	caster.MP = 500
	caster.Skills = storage.SkillStates{{ID: "野蛮冲撞", Level: 3, Train: 0}}
	updated, err := w.CastSkillWithPlayers(caster, "野蛮冲撞", caster.X+3, caster.Y, 0, []storage.Character{front, second})
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	if len(updated.OrderedEvents) < 2 || updated.OrderedEvents[0].Kind != SpellEventCharacterPush || updated.OrderedEvents[1].Kind != SpellEventCharacterPush {
		t.Fatalf("OrderedEvents = %+v, want two pushes before the first rush", updated.OrderedEvents)
	}
	if updated.OrderedEvents[0].CharacterPush.Character.ID != second.ID {
		t.Fatalf("first pushed character = %q, want second-tile target %q", updated.OrderedEvents[0].CharacterPush.Character.ID, second.ID)
	}
	if updated.OrderedEvents[1].CharacterPush.Character.ID != front.ID {
		t.Fatalf("second pushed character = %q, want front target %q", updated.OrderedEvents[1].CharacterPush.Character.ID, front.ID)
	}
}

func TestCastSkillChargeStopsAgainstHigherLevelTarget(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	w.mu.Lock()
	w.monsters = map[string]*Monster{}
	w.occupied = map[monsterPosition]string{}
	w.mu.Unlock()
	mapID := caster.MapID
	mp := w.data.Maps[mapID]
	if !mp.Walkable(caster.X+1, caster.Y) || !mp.Walkable(caster.X+2, caster.Y) {
		t.Fatal("expected walkable tiles east of caster for charge test")
	}
	target, err := w.CreateCharacterWithAppearance("test2", "target", "warrior", 0, 0, mapID, caster.X+1, caster.Y)
	if err != nil {
		t.Fatalf("CreateCharacterWithAppearance() error = %v", err)
	}
	caster.Level = 10
	target.Level = 11
	caster.Skills = storage.SkillStates{{ID: "野蛮冲撞", Level: 0, Train: 0}}
	caster.MP = 500
	updated, err := w.CastSkillWithPlayers(caster, "野蛮冲撞", caster.X+2, caster.Y, 0, []storage.Character{target})
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	if updated.Character.X != caster.X || updated.Character.Y != caster.Y {
		t.Fatalf("caster = %+v, want unchanged when target is higher level", updated.Character)
	}
	if len(updated.AffectedCharacters) != 0 {
		t.Fatalf("AffectedCharacters = %+v, want none when charge is blocked", updated.AffectedCharacters)
	}
	if len(updated.CharacterHits) != 1 {
		t.Fatalf("CharacterHits = %+v, want one self-hit when an occupied target blocks charge", updated.CharacterHits)
	}
	if updated.CharacterHits[0].Character.ID != caster.ID || updated.CharacterHits[0].Damage <= 0 {
		t.Fatalf("blocked charge self-hit = %+v, want positive damage to caster", updated.CharacterHits[0])
	}
}

func TestCastSkillChargeOmitsZeroDamageHitEvent(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	w.mu.Lock()
	w.monsters = map[string]*Monster{}
	w.occupied = map[monsterPosition]string{}
	w.mu.Unlock()
	mon, err := w.SpawnMonsterByNameAt(caster.MapID, caster.X+1, caster.Y, "骷髅", 1)
	if err != nil {
		t.Fatalf("SpawnMonsterByNameAt() error = %v", err)
	}
	if len(mon.Monsters) != 1 {
		t.Fatalf("spawned monsters = %d, want 1", len(mon.Monsters))
	}
	caster.Level = 20
	caster.MP = 500
	caster.Skills = storage.SkillStates{{ID: "野蛮冲撞", Level: 0, Train: 0}}
	w.mu.Lock()
	target := w.monsters[mon.Monsters[0].ID]
	target.Defense = 10000
	w.mu.Unlock()
	updated, err := w.CastSkillWithPlayers(caster, "野蛮冲撞", caster.X+2, caster.Y, 0, nil)
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	if len(updated.MonsterHits) != 0 {
		t.Fatalf("MonsterHits = %+v, want none for zero damage", updated.MonsterHits)
	}
	for _, event := range updated.OrderedEvents {
		if event.Kind == SpellEventMonsterHit {
			t.Fatalf("OrderedEvents = %+v, want no zero-damage monster hit", updated.OrderedEvents)
		}
	}
}

func TestCastSkillChargeAppliesDefenseToSelfDamage(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	caster.Skills = storage.SkillStates{{ID: "野蛮冲撞", Level: 0, Train: 0}}
	caster.MP = 500
	caster.EquippedItems = map[int]storage.UserItem{
		SlotArmor: {ItemID: testArmorID},
	}
	w.mu.Lock()
	mp := w.data.Maps[caster.MapID]
	mp.Blocked = append(mp.Blocked, data.StdPoint{X: caster.X + 1, Y: caster.Y})
	w.data.Maps[caster.MapID] = mp
	armor := w.data.Items[testArmorID]
	armor.Stats.AcMin = 100
	armor.Stats.AcMax = 100
	w.data.Items[testArmorID] = armor
	w.rand = rand.New(zeroSource{})
	w.mu.Unlock()

	result, err := w.DoCharge(caster, 2, nil)
	if err != nil {
		t.Fatalf("DoCharge() error = %v", err)
	}
	if len(result.CharacterHits) != 0 {
		t.Fatalf("CharacterHits = %+v, want no zero-damage self-hit event", result.CharacterHits)
	}
	if result.Character.HP != caster.HP {
		t.Fatalf("self HP = %d, want unchanged %d after defense-reduced zero damage", result.Character.HP, caster.HP)
	}
}

func TestCastSkillMagicShieldReducesMonsterDamageAndConsumesShieldTime(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	caster.Skills = storage.SkillStates{{ID: "魔法盾", Level: 25, Train: 0}}
	caster.MP = 1000
	updated, err := w.CastSkillWithPlayers(caster, "魔法盾", caster.X, caster.Y, 0, nil)
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	if updated.Character.BubbleDefenceUntil == 0 {
		t.Fatal("BubbleDefenceUntil = 0, want active shield")
	}
	mon := &Monster{
		ID:        "mon-shield",
		MapID:     caster.MapID,
		X:         caster.X + 1,
		Y:         caster.Y,
		Alive:     true,
		HP:        999,
		MaxHP:     999,
		MinAttack: 1,
		MaxAttack: 1,
	}
	next, hit, err := w.monsterAttackCharacterWithDamageLocked(mon, updated.Character, 100)
	if err != nil {
		t.Fatalf("monsterAttackCharacterWithDamageLocked() error = %v", err)
	}
	if hit.Damage != 216 {
		t.Fatalf("hit.Damage = %d, want 216 after magic shield reduction", hit.Damage)
	}
	if next.BubbleDefenceUntil >= updated.Character.BubbleDefenceUntil {
		t.Fatalf("BubbleDefenceUntil = %d, want reduced below %d", next.BubbleDefenceUntil, updated.Character.BubbleDefenceUntil)
	}
}

func TestCastSkillMagicShieldRejectsRecastWhileActive(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	caster.Skills = storage.SkillStates{{ID: "魔法盾", Level: 0, Train: 0}}
	updated, err := w.CastSkillWithPlayers(caster, "魔法盾", caster.X, caster.Y, 0, nil)
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() first cast error = %v", err)
	}
	if recast, err := w.CastSkillWithPlayers(updated.Character, "魔法盾", updated.Character.X, updated.Character.Y, 0, nil); err != nil {
		t.Fatalf("CastSkillWithPlayers() recast error = %v, want no-effect completion", err)
	} else {
		if recast.Character.BubbleDefenceUntil != updated.Character.BubbleDefenceUntil {
			t.Fatalf("BubbleDefenceUntil = %d, want unchanged %d", recast.Character.BubbleDefenceUntil, updated.Character.BubbleDefenceUntil)
		}
		magicFire := 0
		for _, event := range recast.Events {
			if event.Kind == SpellEventMagicFire {
				magicFire++
			}
		}
		if magicFire != 1 {
			t.Fatalf("recast magic fire events = %d, want 1", magicFire)
		}
	}
}

func TestCastSkillMagicShieldTreatsExpiredStateAsPresentUntilTick(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	caster.Skills = storage.SkillStates{{ID: "魔法盾", Level: 0, Train: 0}}
	caster.BubbleDefenceUntil = time.Now().Add(-time.Second).UnixNano()
	caster.BubbleDefenceLevel = 1

	result, err := w.DoSpell(caster, "魔法盾", caster.X, caster.Y, 0, nil)
	if err != nil {
		t.Fatalf("DoSpell() error = %v", err)
	}
	if result.Character.BubbleDefenceUntil != caster.BubbleDefenceUntil || result.Character.BubbleDefenceLevel != caster.BubbleDefenceLevel {
		t.Fatalf("expired shield state = (%d, %d), want unchanged (%d, %d)", result.Character.BubbleDefenceUntil, result.Character.BubbleDefenceLevel, caster.BubbleDefenceUntil, caster.BubbleDefenceLevel)
	}
}

func TestCastSkillTurnUndeadKillsUndeadMonster(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	mapID, x, y := caster.MapID, caster.X, caster.Y
	mp := w.data.Maps[mapID]
	targetX, targetY := -1, -1
	for dx := 6; dx < 24 && targetX < 0; dx++ {
		for dy := -4; dy <= 4; dy++ {
			tx := x + dx
			ty := y + dy
			if !mp.Walkable(tx, ty) {
				continue
			}
			clear := true
			w.mu.Lock()
			for _, mon := range w.monsters {
				if mon != nil && mon.Alive && mon.MapID == mapID && abs(mon.X-tx) <= 1 && abs(mon.Y-ty) <= 1 {
					clear = false
					break
				}
			}
			w.mu.Unlock()
			if clear {
				targetX, targetY = tx, ty
				break
			}
		}
	}
	if targetX < 0 {
		t.Fatal("could not find clear tile for turn undead test")
	}
	result, err := w.SpawnMonsterByNameAt(mapID, targetX, targetY, "僵尸", 1)
	if err != nil {
		t.Fatalf("SpawnMonsterByNameAt() error = %v", err)
	}
	if len(result.Monsters) != 1 {
		t.Fatalf("SpawnMonsterByNameAt() monsters = %d, want 1", len(result.Monsters))
	}
	monsterID := result.Monsters[0].ID
	caster.Level = 100
	caster.MP = 500
	caster.Skills = storage.SkillStates{{ID: "圣言术", Level: 0, Train: 0}}
	updated, err := w.CastSkillWithPlayers(caster, "圣言术", targetX, targetY, MonsterActorID(result.Monsters[0]), nil)
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	if updated.MonsterHit != nil {
		t.Fatalf("MonsterHit = %+v, want no synthetic hit event", updated.MonsterHit)
	}
	w.mu.Lock()
	if w.monsters[monsterID].LastHitterID != updated.Character.ID {
		t.Fatalf("turn-undead last hitter = %q, want %q", w.monsters[monsterID].LastHitterID, updated.Character.ID)
	}
	w.mu.Unlock()
	tick, err := w.Tick([]PlayerSnapshot{{Character: updated.Character}}, time.Now())
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if len(tick.MonsterDeaths) != 1 {
		t.Fatalf("MonsterDeaths = %+v, want turn-undead target", tick.MonsterDeaths)
	}
	tick, err = w.Tick([]PlayerSnapshot{{Character: tick.MonsterDeaths[0].Character}}, time.Now())
	if err != nil {
		t.Fatalf("second Tick() error = %v", err)
	}
	if len(tick.MonsterDeaths) != 0 {
		t.Fatalf("second MonsterDeaths = %+v, want no duplicate death", tick.MonsterDeaths)
	}
}

func TestCastSkillTurnUndeadRejectsAdminMonster(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	targetX, targetY := caster.X+1, caster.Y
	mon := &Monster{ID: "admin-undead-1", MapID: caster.MapID, X: targetX, Y: targetY, HP: 100, MaxHP: 100, Alive: true, Undead: 1, AdminMode: true}
	w.mu.Lock()
	w.monsters = map[string]*Monster{mon.ID: mon}
	w.occupied = map[monsterPosition]string{{MapID: mon.MapID, X: mon.X, Y: mon.Y}: mon.ID}
	w.mu.Unlock()
	caster.Level = 100
	caster.MP = 500
	caster.Skills = storage.SkillStates{{ID: "圣言术", Level: 3}}
	result, err := w.CastSkillWithPlayers(caster, "圣言术", targetX, targetY, MonsterActorID(*mon), nil)
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	if result.MonsterHit != nil {
		t.Fatalf("MonsterHit = %+v, want nil for admin monster", result.MonsterHit)
	}
	w.mu.Lock()
	got := *w.monsters[mon.ID]
	w.mu.Unlock()
	if got.HP != mon.HP || got.TargetCharacterID != "" || got.RunAwayMode {
		t.Fatalf("admin monster state = %+v, want unchanged", got)
	}
}

func TestCastSkillTurnUndeadRejectsImproperMonsterTarget(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	targetX, targetY := caster.X+1, caster.Y
	mon := &Monster{ID: "owned-undead-1", MapID: caster.MapID, X: targetX, Y: targetY, HP: 100, MaxHP: 100, Alive: true, Undead: 1, MasterID: caster.ID}
	w.mu.Lock()
	w.monsters = map[string]*Monster{mon.ID: mon}
	w.occupied = map[monsterPosition]string{{MapID: mon.MapID, X: mon.X, Y: mon.Y}: mon.ID}
	w.mu.Unlock()
	caster.AttackMode = 1
	caster.Level = 100
	caster.MP = 500
	caster.Skills = storage.SkillStates{{ID: "圣言术", Level: 3}}
	result, err := w.CastSkillWithPlayers(caster, "圣言术", targetX, targetY, MonsterActorID(*mon), nil)
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	if result.MonsterHit != nil {
		t.Fatalf("MonsterHit = %+v, want nil for improper target", result.MonsterHit)
	}
	w.mu.Lock()
	got := *w.monsters[mon.ID]
	w.mu.Unlock()
	if got.HP != mon.HP || got.TargetCharacterID != "" || got.RunAwayMode {
		t.Fatalf("improper target state = %+v, want unchanged", got)
	}
}

func TestCastSkillTurnUndeadRejectsHighLevelUndeadMonster(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	mapID, x, y := caster.MapID, caster.X, caster.Y
	mp := w.data.Maps[mapID]
	targetX, targetY := -1, -1
	for dx := 6; dx < 24 && targetX < 0; dx++ {
		for dy := -4; dy <= 4; dy++ {
			tx := x + dx
			ty := y + dy
			if !mp.Walkable(tx, ty) {
				continue
			}
			clear := true
			w.mu.Lock()
			for _, mon := range w.monsters {
				if mon != nil && mon.Alive && mon.MapID == mapID && abs(mon.X-tx) <= 1 && abs(mon.Y-ty) <= 1 {
					clear = false
					break
				}
			}
			w.mu.Unlock()
			if clear {
				targetX, targetY = tx, ty
				break
			}
		}
	}
	if targetX < 0 {
		t.Fatal("could not find clear tile for turn undead test")
	}
	result, err := w.SpawnMonsterByNameAt(mapID, targetX, targetY, "僵尸", 1)
	if err != nil {
		t.Fatalf("SpawnMonsterByNameAt() error = %v", err)
	}
	if len(result.Monsters) != 1 {
		t.Fatalf("SpawnMonsterByNameAt() monsters = %d, want 1", len(result.Monsters))
	}
	monID := result.Monsters[0].ID
	w.mu.Lock()
	if mon := w.monsters[monID]; mon != nil {
		mon.Level = caster.Level + 20
	}
	w.mu.Unlock()
	caster.Level = 1
	caster.MP = 500
	caster.Skills = storage.SkillStates{{ID: "圣言术", Level: 0, Train: 0}}
	updated, err := w.CastSkillWithPlayers(caster, "圣言术", targetX, targetY, MonsterActorID(Monster{ID: monID}), nil)
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	if updated.MonsterHit != nil {
		t.Fatalf("MonsterHit = %+v, want nil for high-level undead", updated.MonsterHit)
	}
	w.mu.Lock()
	mon := w.monsters[monID]
	w.mu.Unlock()
	if mon == nil {
		t.Fatal("monster missing after failed turn undead")
	}
	if mon.RunAwayMode {
		t.Fatal("monster RunAwayMode = true, want no flee state after Struck sets target")
	}
	if mon.TargetCharacterID != caster.ID {
		t.Fatalf("monster TargetCharacterID = %q, want %q", mon.TargetCharacterID, caster.ID)
	}
	if !mon.LastAttackAt.After(time.Now()) {
		t.Fatalf("monster LastAttackAt = %v, want delayed after Struck", mon.LastAttackAt)
	}
}

func TestCastSkillSummonSkeletonCreatesOwnedMonsterAndExpires(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	mapID := caster.MapID
	mp := w.data.Maps[mapID]
	targetX, targetY := -1, -1
	for dx := 1; dx < 8 && targetX < 0; dx++ {
		for dy := -2; dy <= 2; dy++ {
			tx := caster.X + dx
			ty := caster.Y + dy
			if !mp.Walkable(tx, ty) {
				continue
			}
			clear := true
			w.mu.Lock()
			for _, mon := range w.monsters {
				if mon != nil && mon.Alive && mon.MapID == mapID && abs(mon.X-tx) <= 1 && abs(mon.Y-ty) <= 1 {
					clear = false
					break
				}
			}
			w.mu.Unlock()
			if clear {
				targetX, targetY = tx, ty
				break
			}
		}
	}
	if targetX < 0 {
		t.Fatal("could not find clear tile for summon test")
	}
	caster.X = targetX
	caster.Y = targetY
	caster.Dir = 2
	caster.MP = 100
	caster.Skills = storage.SkillStates{{ID: "召唤骷髅", Level: 0, Train: 0}}
	caster.EquippedItems = map[int]storage.UserItem{SlotBujuk: {ItemID: "护身符", Dura: 10000}}
	result, err := w.CastSkillWithPlayers(caster, "召唤骷髅", caster.X, caster.Y, 0, nil)
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	if len(result.SummonedMonsters) != 1 {
		t.Fatalf("SummonedMonsters = %d, want 1", len(result.SummonedMonsters))
	}
	summoned := result.SummonedMonsters[0]
	if summoned.X != caster.X+1 || summoned.Y != caster.Y {
		t.Fatalf("summoned position = (%d,%d), want front tile (%d,%d)", summoned.X, summoned.Y, caster.X+1, caster.Y)
	}
	if summoned.MasterID != caster.ID {
		t.Fatalf("summoned.MasterID = %q, want %q", summoned.MasterID, caster.ID)
	}
	if summoned.MasterName != caster.Name {
		t.Fatalf("summoned.MasterName = %q, want %q", summoned.MasterName, caster.Name)
	}
	if summoned.TemplateID != "骷髅" {
		t.Fatalf("summoned.TemplateID = %q, want 骷髅", summoned.TemplateID)
	}
	if summoned.Alive != true {
		t.Fatalf("summoned.Alive = false, want true")
	}
	if summoned.MasterExpiresAt.IsZero() {
		t.Fatal("summoned.MasterExpiresAt = zero, want active expiry")
	}
	if summoned.SlaveMakeLevel != caster.Skills[0].Level {
		t.Fatalf("summoned.SlaveMakeLevel = %d, want %d", summoned.SlaveMakeLevel, caster.Skills[0].Level)
	}
	recast := result.Character
	recast.Skills[0].LastCastAt = time.Now().Add(-time.Second).UnixMilli()
	dup, err := w.CastSkillWithPlayers(recast, "召唤骷髅", recast.X, recast.Y, 0, nil)
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() duplicate summon error = %v", err)
	}
	if len(dup.AffectedMonsters) != 0 {
		t.Fatalf("duplicate summon affected monsters = %d, want 0", len(dup.AffectedMonsters))
	}
	if mon, ok := w.monsters[summoned.ID]; !ok || mon.X != summoned.X || mon.Y != summoned.Y {
		t.Fatalf("duplicate skeleton summon moved monster = %+v, want unchanged position (%d,%d)", mon, summoned.X, summoned.Y)
	}
	w.mu.Lock()
	activeSkeletons := 0
	for _, mon := range w.monsters {
		if mon == nil || !mon.Alive || mon.MasterID != caster.ID || mon.TemplateID != "骷髅" {
			continue
		}
		activeSkeletons++
	}
	w.mu.Unlock()
	if activeSkeletons != 1 {
		t.Fatalf("active skeletons = %d, want 1 after duplicate summon", activeSkeletons)
	}
	w.mu.Lock()
	if mon, ok := w.monsters[summoned.ID]; ok {
		mon.MasterExpiresAt = time.Now().Add(-time.Second)
	}
	w.mu.Unlock()
	recast = result.Character
	recast.Skills[0].LastCastAt = time.Now().Add(-time.Second).UnixMilli()
	beforeTick, err := w.CastSkillWithPlayers(recast, "召唤骷髅", recast.X, recast.Y, 0, nil)
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() expired summon error = %v", err)
	}
	if len(beforeTick.SummonedMonsters) != 0 {
		t.Fatalf("expired summon before Tick created %d monsters, want 0", len(beforeTick.SummonedMonsters))
	}
	if _, err := w.Tick([]PlayerSnapshot{{Character: result.Character}}, time.Now()); err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	monsters, _ := w.SnapshotAround(result.Character.MapID, result.Character.X, result.Character.Y, 10)
	found := false
	for _, mon := range monsters {
		if mon.ID == summoned.ID {
			found = true
			if mon.MasterID != "" || mon.MasterName != "" || mon.NoTame || !mon.MasterExpiresAt.IsZero() {
				t.Fatalf("expired skeleton relation = %+v, want released", mon)
			}
		}
	}
	if !found {
		t.Fatalf("summoned monster %s missing after expiry", summoned.ID)
	}
}

func TestCastSkillSummonSkeletonConsumesAmulet(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	mapID := caster.MapID
	mp := w.data.Maps[mapID]
	targetX, targetY := -1, -1
	for dx := 1; dx < 8 && targetX < 0; dx++ {
		for dy := -2; dy <= 2; dy++ {
			tx := caster.X + dx
			ty := caster.Y + dy
			if !mp.Walkable(tx, ty) {
				continue
			}
			clear := true
			w.mu.Lock()
			for _, mon := range w.monsters {
				if mon != nil && mon.Alive && mon.MapID == mapID && abs(mon.X-tx) <= 1 && abs(mon.Y-ty) <= 1 {
					clear = false
					break
				}
			}
			w.mu.Unlock()
			if clear {
				targetX, targetY = tx, ty
				break
			}
		}
	}
	if targetX < 0 {
		t.Fatal("could not find clear tile for summon test")
	}
	caster.X = targetX
	caster.Y = targetY
	caster.Dir = 2
	caster.MP = 100
	caster.Skills = storage.SkillStates{{ID: "召唤骷髅", Level: 0, Train: 0}}
	caster.EquippedItems = map[int]storage.UserItem{SlotBujuk: {ItemID: "护身符", Dura: 10000, DuraMax: 10000}}
	before := caster.EquippedItems[SlotBujuk]
	result, err := w.CastSkillWithPlayers(caster, "召唤骷髅", caster.X, caster.Y, 0, nil)
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	if len(result.SummonedMonsters) != 1 {
		t.Fatalf("SummonedMonsters = %d, want 1", len(result.SummonedMonsters))
	}
	if got := result.Character.EquippedItems[SlotBujuk]; got.Dura != before.Dura-100 {
		t.Fatalf("skeleton amulet dura = %d, want %d", got.Dura, before.Dura-100)
	}
}

func TestWorldTickDelaysSummonDeathAfterMasterDeath(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	spawn, err := w.SpawnMonsterByNameAt(caster.MapID, caster.X+1, caster.Y, "骷髅", 1)
	if err != nil {
		t.Fatalf("SpawnMonsterByNameAt() error = %v", err)
	}
	monID := spawn.Monsters[0].ID
	w.mu.Lock()
	mon := w.monsters[monID]
	mon.MasterID = caster.ID
	mon.MasterName = caster.Name
	mon.MasterExpiresAt = time.Now().Add(time.Hour)
	w.mu.Unlock()
	dead := caster
	dead.HP = 0
	start := time.Now()
	if _, err := w.Tick([]PlayerSnapshot{{Character: dead}}, start); err != nil {
		t.Fatalf("first Tick() error = %v", err)
	}
	w.mu.Lock()
	if !w.monsters[monID].Alive {
		w.mu.Unlock()
		t.Fatal("summon died on first master-death observation")
	}
	w.mu.Unlock()
	result, err := w.Tick([]PlayerSnapshot{{Character: dead}}, start.Add(time.Second))
	if err != nil {
		t.Fatalf("second Tick() error = %v", err)
	}
	w.mu.Lock()
	stillAlive := w.monsters[monID].Alive
	w.mu.Unlock()
	if stillAlive {
		t.Fatal("summon remained alive after one second of master death")
	}
	if len(result.MonsterDeaths) != 1 || result.MonsterDeaths[0].MonsterID != monID || result.MonsterDeaths[0].MonsterMapID != caster.MapID {
		t.Fatalf("monster deaths = %+v, want death for %q on map %q", result.MonsterDeaths, monID, caster.MapID)
	}
}

func TestBoostSummonedMonsterLockedRestoresHalfMissingHP(t *testing.T) {
	mon := &Monster{HP: 30, MaxHP: 100}
	boostSummonedMonsterLocked(mon)
	if mon.HP != 65 {
		t.Fatalf("HP = %d, want 65 after half-missing restore", mon.HP)
	}
}

func TestCastSkillSummonSkeletonFailsWhenFrontTileBlocked(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	mapID, x, y := caster.MapID, caster.X, caster.Y
	caster.Dir = 2
	blocker, err := w.CreateCharacterWithAppearance("test", "blocker", "wizard", 0, 0, mapID, x+1, y)
	if err != nil {
		t.Fatalf("CreateCharacter() blocker error = %v", err)
	}
	caster.Level = 100
	caster.AttackMode = 1
	caster.MP = 100
	caster.Skills = storage.SkillStates{{ID: "召唤骷髅", Level: 0, Train: 0}}
	caster.EquippedItems = map[int]storage.UserItem{SlotBujuk: {ItemID: "护身符", Dura: 10000, DuraMax: 10000}}
	players := []storage.Character{blocker}
	result, err := w.CastSkillWithPlayers(caster, "召唤骷髅", caster.X, caster.Y, 0, players)
	if err != nil || result.SpellFailed {
		t.Fatalf("CastSkillWithPlayers() result = %+v, error = %v, want ordinary blocked summon result", result, err)
	}
	if len(result.SummonedMonsters) != 0 {
		t.Fatalf("SummonedMonsters = %d, want 0 for blocked front tile", len(result.SummonedMonsters))
	}
	if got := result.Character.EquippedItems[SlotBujuk].Dura; got != 9900 {
		t.Fatalf("blocked summon amulet dura = %d, want 9900 after reference pre-consumption", got)
	}
}

func TestSummonedMonsterIgnoresMastersFriendlyGroupMembers(t *testing.T) {
	w, master := newTestWorldCharacter(t)
	mapID := master.MapID
	friend, err := w.CreateCharacterWithAppearance("test", "friend", "wizard", 0, 0, mapID, master.X+2, master.Y)
	if err != nil {
		t.Fatalf("CreateCharacter() friend error = %v", err)
	}
	master.AllowGroup = true
	friend.AllowGroup = true
	master, friend, err = w.CreateGroup(master, friend, 2)
	if err != nil {
		t.Fatalf("CreateGroup() error = %v", err)
	}
	master.Dir = 2
	master.MP = 100
	master.Skills = storage.SkillStates{{ID: "召唤骷髅", Level: 0, Train: 0}}
	master.EquippedItems = map[int]storage.UserItem{SlotBujuk: {ItemID: "护身符", Dura: 10000}}
	summonedResult, err := w.CastSkillWithPlayers(master, "召唤骷髅", master.X, master.Y, 0, []storage.Character{master, friend})
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() summon error = %v", err)
	}
	summoned := summonedResult.SummonedMonsters[0]
	w.mu.Lock()
	mon := w.monsters[summoned.ID]
	mon.TargetCharacterID = ""
	mon.TargetFocusAt = time.Time{}
	mon.NextSearchAt = time.Now().Add(-time.Second)
	w.mu.Unlock()
	players := map[string]storage.Character{
		master.ID: master,
		friend.ID: friend,
	}
	actions, _, _, err := w.tickSummonedMonsterLocked(mon, players, time.Now())
	if err != nil {
		t.Fatalf("tickSummonedMonsterLocked() error = %v", err)
	}
	if mon.TargetCharacterID == friend.ID {
		t.Fatal("summoned monster targeted master friend, want friend ignored")
	}
	if len(actions) == 0 && mon.TargetCharacterID != "" {
		t.Fatalf("summoned monster target = %q, want empty or a non-friend target", mon.TargetCharacterID)
	}
}

func TestCastSkillSummonBeastCreatesOwnedMonsterAndExpires(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	mapID := caster.MapID
	mp := w.data.Maps[mapID]
	targetX, targetY := -1, -1
	for dx := 1; dx < 8 && targetX < 0; dx++ {
		for dy := -2; dy <= 2; dy++ {
			tx := caster.X + dx
			ty := caster.Y + dy
			if !mp.Walkable(tx, ty) {
				continue
			}
			clear := true
			w.mu.Lock()
			for _, mon := range w.monsters {
				if mon != nil && mon.Alive && mon.MapID == mapID && abs(mon.X-tx) <= 1 && abs(mon.Y-ty) <= 1 {
					clear = false
					break
				}
			}
			w.mu.Unlock()
			if clear {
				targetX, targetY = tx, ty
				break
			}
		}
	}
	if targetX < 0 {
		t.Fatal("could not find clear tile for summon beast test")
	}
	caster.X = targetX
	caster.Y = targetY
	caster.Dir = 2
	caster.MP = 100
	caster.Skills = storage.SkillStates{{ID: "召唤神兽", Level: 0, Train: 0}}
	caster.EquippedItems = map[int]storage.UserItem{SlotBujuk: {ItemID: "护身符", Dura: 10000}}
	result, err := w.CastSkillWithPlayers(caster, "召唤神兽", caster.X, caster.Y, 0, nil)
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	if len(result.SummonedMonsters) != 1 {
		t.Fatalf("SummonedMonsters = %d, want 1", len(result.SummonedMonsters))
	}
	summoned := result.SummonedMonsters[0]
	if summoned.MasterID != caster.ID {
		t.Fatalf("summoned.MasterID = %q, want %q", summoned.MasterID, caster.ID)
	}
	if summoned.TemplateID != "神兽" {
		t.Fatalf("summoned.TemplateID = %q, want 神兽", summoned.TemplateID)
	}
	if summoned.Alive != true {
		t.Fatalf("summoned.Alive = false, want true")
	}
	if summoned.MasterExpiresAt.IsZero() {
		t.Fatal("summoned.MasterExpiresAt = zero, want active expiry")
	}
	recast := result.Character
	recast.Skills[0].LastCastAt = time.Now().Add(-time.Second).UnixMilli()
	dup, err := w.CastSkillWithPlayers(recast, "召唤神兽", recast.X, recast.Y, 0, nil)
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() duplicate summon error = %v", err)
	}
	if len(dup.AffectedMonsters) != 1 {
		t.Fatalf("duplicate summon affected monsters = %d, want 1", len(dup.AffectedMonsters))
	}
	if dup.AffectedMonsters[0].ID != summoned.ID {
		t.Fatalf("duplicate summon affected monster = %q, want %q", dup.AffectedMonsters[0].ID, summoned.ID)
	}
	w.mu.Lock()
	if mon, ok := w.monsters[summoned.ID]; ok {
		mon.MasterExpiresAt = time.Now().Add(-time.Second)
	}
	w.mu.Unlock()
	recast = result.Character
	recast.Skills[0].LastCastAt = time.Now().Add(-time.Second).UnixMilli()
	beforeTick, err := w.CastSkillWithPlayers(recast, "召唤神兽", recast.X, recast.Y, 0, nil)
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() expired beast error = %v", err)
	}
	if len(beforeTick.AffectedMonsters) != 1 || beforeTick.AffectedMonsters[0].ID != summoned.ID {
		t.Fatalf("expired beast before Tick result = %+v, want existing beast recall", beforeTick.AffectedMonsters)
	}
	if _, err := w.Tick([]PlayerSnapshot{{Character: result.Character}}, time.Now()); err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	monsters, _ := w.SnapshotAround(result.Character.MapID, result.Character.X, result.Character.Y, 10)
	found := false
	for _, mon := range monsters {
		if mon.ID == summoned.ID {
			found = true
			if mon.MasterID != "" || mon.MasterName != "" || mon.NoTame || !mon.MasterExpiresAt.IsZero() {
				t.Fatalf("expired beast relation = %+v, want released", mon)
			}
		}
	}
	if !found {
		t.Fatalf("summoned beast %s missing after expiry", summoned.ID)
	}
}

func TestCastSkillSummonBeastIgnoresDirectionRecog(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	mapID := caster.MapID
	mp := w.data.Maps[mapID]
	targetX, targetY := -1, -1
	for dx := 1; dx < 8 && targetX < 0; dx++ {
		for dy := -2; dy <= 2; dy++ {
			tx := caster.X + dx
			ty := caster.Y + dy
			if !mp.Walkable(tx, ty) {
				continue
			}
			clear := true
			w.mu.Lock()
			for _, mon := range w.monsters {
				if mon != nil && mon.Alive && mon.MapID == mapID && abs(mon.X-tx) <= 1 && abs(mon.Y-ty) <= 1 {
					clear = false
					break
				}
			}
			w.mu.Unlock()
			if clear {
				targetX, targetY = tx, ty
				break
			}
		}
	}
	if targetX < 0 {
		t.Fatal("could not find clear tile for summon beast recog test")
	}
	caster.X = targetX
	caster.Y = targetY
	caster.Dir = 2
	caster.MP = 100
	caster.Skills = storage.SkillStates{{ID: "召唤神兽", Level: 0, Train: 0}}
	caster.EquippedItems = map[int]storage.UserItem{SlotBujuk: {ItemID: "护身符(大)", Dura: 20000}}
	result, err := w.CastSkillWithPlayers(caster, "召唤神兽", caster.X, caster.Y, 0, nil)
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	if len(result.SummonedMonsters) != 1 {
		t.Fatalf("SummonedMonsters = %d, want 1", len(result.SummonedMonsters))
	}
	if result.SummonedMonsters[0].TemplateID != "神兽" {
		t.Fatalf("summoned.TemplateID = %q, want 神兽", result.SummonedMonsters[0].TemplateID)
	}
}

func TestCastSkillSummonSkeletonBlocksBeastWhileActive(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	mapID := caster.MapID
	mp := w.data.Maps[mapID]
	targetX, targetY := -1, -1
	for dx := 1; dx < 8 && targetX < 0; dx++ {
		for dy := -2; dy <= 2; dy++ {
			tx := caster.X + dx
			ty := caster.Y + dy
			if !mp.Walkable(tx, ty) {
				continue
			}
			clear := true
			w.mu.Lock()
			for _, mon := range w.monsters {
				if mon != nil && mon.Alive && mon.MapID == mapID && abs(mon.X-tx) <= 1 && abs(mon.Y-ty) <= 1 {
					clear = false
					break
				}
			}
			w.mu.Unlock()
			if clear {
				targetX, targetY = tx, ty
				break
			}
		}
	}
	if targetX < 0 {
		t.Fatal("could not find clear tile for coexist summon test")
	}
	caster.X = targetX
	caster.Y = targetY
	caster.Dir = 2
	caster.MP = 100
	caster.Skills = storage.SkillStates{{ID: "召唤骷髅", Level: 0, Train: 0}, {ID: "召唤神兽", Level: 0, Train: 0}}
	caster.EquippedItems = map[int]storage.UserItem{SlotBujuk: {ItemID: "护身符(大)", Dura: 20000}}
	skeletonResult, err := w.CastSkillWithPlayers(caster, "召唤骷髅", caster.X, caster.Y, 0, nil)
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() skeleton error = %v", err)
	}
	beastCaster := skeletonResult.Character
	beastCaster.Dir = 0
	beastCaster.Skills[1].LastCastAt = time.Now().Add(-time.Second).UnixMilli()
	dup, err := w.CastSkillWithPlayers(beastCaster, "召唤神兽", beastCaster.X, beastCaster.Y, 0, nil)
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() beast summon while skeleton active error = %v", err)
	}
	if len(dup.SummonedMonsters) != 1 {
		t.Fatalf("duplicate beast summon = %+v, want one new beast summon while skeleton active", dup.SummonedMonsters)
	}
	if dup.SummonedMonsters[0].TemplateID != "神兽" {
		t.Fatalf("duplicate beast summon template = %q, want 神兽", dup.SummonedMonsters[0].TemplateID)
	}
}

func TestCastSkillSummonUsesIndependentTemplateCaps(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	mapID := caster.MapID
	mp := w.data.Maps[mapID]
	targetX, targetY := -1, -1
	for dx := 1; dx < 10 && targetX < 0; dx++ {
		for dy := -2; dy <= 2; dy++ {
			tx := caster.X + dx
			ty := caster.Y + dy
			if !mp.Walkable(tx, ty) {
				continue
			}
			clear := true
			w.mu.Lock()
			for _, mon := range w.monsters {
				if mon != nil && mon.Alive && mon.MapID == mapID && abs(mon.X-tx) <= 1 && abs(mon.Y-ty) <= 1 {
					clear = false
					break
				}
			}
			w.mu.Unlock()
			if clear {
				targetX, targetY = tx, ty
				break
			}
		}
	}
	if targetX < 0 {
		t.Fatal("could not find clear tile for independent summon cap test")
	}
	caster.X = targetX
	caster.Y = targetY
	caster.Dir = 2
	caster.MP = 100
	caster.Skills = storage.SkillStates{{ID: "召唤骷髅", Level: 0, Train: 0}, {ID: "召唤神兽", Level: 0, Train: 0}}
	caster.EquippedItems = map[int]storage.UserItem{SlotBujuk: {ItemID: "护身符(大)", Dura: 20000}}
	w.mu.Lock()
	tpl, ok := w.data.Monsters["鸡"]
	if !ok {
		w.mu.Unlock()
		t.Fatal("monster 鸡 missing from configs")
	}
	for i := 0; i < defaultTamingCount; i++ {
		id := fmt.Sprintf("independent-cap-%d", i)
		mon := newMonster(w, id, tpl, mapID, caster.X+i+2, caster.Y, data.StdSpawn{MapID: mapID, MonsterID: tpl.ID, X: caster.X + i + 2, Y: caster.Y})
		mon.MasterID = caster.ID
		mon.MasterExpiresAt = time.Now().Add(time.Hour)
		w.monsters[id] = mon
	}
	w.mu.Unlock()
	skeletonResult, err := w.CastSkillWithPlayers(caster, "召唤骷髅", caster.X, caster.Y, 0, nil)
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() skeleton error = %v", err)
	}
	if len(skeletonResult.SummonedMonsters) != 1 {
		t.Fatalf("SummonedMonsters = %d, want 1", len(skeletonResult.SummonedMonsters))
	}
	beastCaster := skeletonResult.Character
	beastCaster.Dir = 0
	beastCaster.Skills[1].LastCastAt = time.Now().Add(-time.Second).UnixMilli()
	blocked, err := w.CastSkillWithPlayers(beastCaster, "召唤神兽", beastCaster.X, beastCaster.Y, 0, nil)
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() beast summon at shared cap error = %v", err)
	}
	if len(blocked.SummonedMonsters) != 1 {
		t.Fatalf("beast summon = %+v, want one new summon despite skeleton cap", blocked.SummonedMonsters)
	}
}

func TestCastSkillPoisonAppliesMonsterStatusAndTickDamage(t *testing.T) {
	cases := []struct {
		name         string
		powderItemID string
		powderSlot   int
		wantHealth   bool
		wantArmor    bool
	}{
		{
			name:         "gray",
			powderItemID: "灰色药粉(少量)",
			powderSlot:   SlotBujuk,
			wantHealth:   true,
		},
		{
			name:         "yellow",
			powderItemID: "黄色药粉(少量)",
			powderSlot:   SlotArmRingL,
			wantArmor:    true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w, caster := newTestWorldCharacter(t)
			w.mu.Lock()
			w.monsters = map[string]*Monster{}
			w.occupied = map[monsterPosition]string{}
			w.mu.Unlock()
			mapID := caster.MapID
			mp := w.data.Maps[mapID]
			baseX, baseY := -1, -1
			for dx := 6; dx < 18 && baseX < 0; dx++ {
				for dy := -2; dy <= 2; dy++ {
					tx := caster.X + dx
					ty := caster.Y + dy
					if !mp.Walkable(tx, ty) || !mp.Walkable(tx+1, ty) {
						continue
					}
					baseX, baseY = tx, ty
					break
				}
			}
			if baseX < 0 {
				t.Fatal("could not find clear tile for poison test")
			}
			caster.X = baseX
			caster.Y = baseY
			caster.Dir = 2
			skill, ok := w.Skill("施毒术")
			if !ok {
				t.Fatalf("skill 施毒术 missing from config")
			}
			cost := w.SpellCost(skill, storage.SkillState{ID: "施毒术", Level: 3, Train: 0})
			caster.Level = 14
			caster.MP = cost + 20
			caster.Skills = storage.SkillStates{{ID: "施毒术", Level: 3, Train: 0}}
			caster.EquippedItems = map[int]storage.UserItem{
				tc.powderSlot: {ItemID: tc.powderItemID, Dura: 5000},
			}
			result, err := w.SpawnMonsterByNameAt(mapID, baseX+1, baseY, "鸡", 1)
			if err != nil {
				t.Fatalf("SpawnMonsterByNameAt() error = %v", err)
			}
			if len(result.Monsters) != 1 {
				t.Fatalf("SpawnMonsterByNameAt() monsters = %d, want 1", len(result.Monsters))
			}
			monsterID := result.Monsters[0].ID
			w.mu.Lock()
			if mon := w.monsters[monsterID]; mon != nil {
				mon.HP = 1000
				mon.MaxHP = 1000
			}
			w.mu.Unlock()
			beforeDura := caster.EquippedItems[tc.powderSlot].Dura
			updated, err := w.CastSkillWithPlayers(caster, "施毒术", baseX+1, baseY, MonsterActorID(Monster{ID: monsterID}), nil)
			if err != nil {
				t.Fatalf("CastSkillWithPlayers() error = %v", err)
			}
			if got := updated.Character.MP; got != caster.MP-cost {
				t.Fatalf("MP = %d, want %d after poison", got, caster.MP-cost)
			}
			if got := updated.Character.EquippedItems[tc.powderSlot].Dura; got != beforeDura-100 {
				t.Fatalf("poison powder dura = %d, want %d", got, beforeDura-100)
			}
			delivered, err := w.Tick([]PlayerSnapshot{{Character: updated.Character}}, time.Now().Add(1100*time.Millisecond))
			if err != nil {
				t.Fatalf("Tick() poison delivery error = %v", err)
			}
			if len(delivered.AffectedMonsters) != 0 {
				t.Fatalf("poison affected monsters = %d, want no ordinary feature refresh", len(delivered.AffectedMonsters))
			}
			if len(delivered.StatusRefreshMonsters) != 1 {
				t.Fatalf("poison status refreshes = %+v, want one status refresh", delivered.StatusRefreshMonsters)
			}
			w.mu.Lock()
			mon := w.monsters[monsterID]
			w.mu.Unlock()
			if mon == nil {
				t.Fatalf("monster %s missing after poison cast", monsterID)
			}
			if tc.wantHealth {
				if mon.PoisonHealthUntil.IsZero() || mon.PoisonHealthStartAt.IsZero() {
					t.Fatalf("monster poison health = %+v, want active health poison", mon)
				}
				if !mon.PoisonHealthStartAt.After(time.Now()) {
					t.Fatalf("monster poison health start = %s, want future start", mon.PoisonHealthStartAt)
				}
			}
			if tc.wantArmor {
				if mon.PoisonArmorLevel == 0 || mon.PoisonArmorUntil.IsZero() || mon.PoisonArmorStartAt.IsZero() {
					t.Fatalf("monster poison armor = %+v, want active armor poison", mon)
				}
				if !mon.PoisonArmorStartAt.After(time.Now()) {
					t.Fatalf("monster poison armor start = %s, want future start", mon.PoisonArmorStartAt)
				}
			}
			beforeHP := mon.HP
			tickAt := time.Now().Add(5 * time.Second)
			if _, err := w.Tick([]PlayerSnapshot{{Character: updated.Character}}, tickAt); err != nil {
				t.Fatalf("Tick() error = %v", err)
			}
			w.mu.Lock()
			mon = w.monsters[monsterID]
			w.mu.Unlock()
			if tc.wantHealth && mon.HP >= beforeHP {
				t.Fatalf("monster HP = %d, want reduced from %d after poison tick", mon.HP, beforeHP)
			}
		})
	}
}

func TestCastSkillPoisonRejectsSelfBeforeConsumingPowder(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	skill, ok := w.Skill("施毒术")
	if !ok {
		t.Fatalf("skill 施毒术 missing from config")
	}
	cost := w.SpellCost(skill, storage.SkillState{ID: "施毒术", Level: 0, Train: 0})
	caster.Level = 14
	caster.MP = cost + 20
	caster.Skills = storage.SkillStates{{ID: "施毒术", Level: 0, Train: 0}}
	caster.EquippedItems = map[int]storage.UserItem{
		SlotBujuk: {ItemID: "灰色药粉(少量)", Dura: 5000},
	}

	if _, err := w.CastSkillWithPlayers(caster, "施毒术", caster.X, caster.Y, CharacterActorID(caster), []storage.Character{caster}); err == nil {
		t.Fatal("CastSkillWithPlayers() error = nil, want self target rejection")
	}
	if got := caster.EquippedItems[SlotBujuk].Dura; got != 5000 {
		t.Fatalf("poison powder dura = %d, want unchanged 5000", got)
	}
}

func TestCastSkillPoisonDoesNotRetargetInvalidMonster(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	target, err := w.CreateCharacterWithAppearance("test2", "target", "warrior", 0, 0, caster.MapID, caster.X+1, caster.Y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	caster.Level = 14
	caster.MP = 100
	caster.Skills = storage.SkillStates{{ID: "施毒术", Level: 0, Train: 0}}
	caster.EquippedItems = map[int]storage.UserItem{SlotBujuk: {ItemID: "灰色药粉(少量)", Dura: 5000}}
	mon := &Monster{ID: "invalid-poison-monster", Name: "invalid", MapID: caster.MapID, X: target.X, Y: target.Y, HP: 100, MaxHP: 100, Alive: true, AdminMode: true}
	w.mu.Lock()
	w.monsters[mon.ID] = mon
	w.mu.Unlock()
	_, err = w.DoSpell(caster, "施毒术", target.X, target.Y, MonsterActorID(*mon), []storage.Character{target})
	if err == nil {
		t.Fatal("DoSpell() error = nil, want invalid monster target failure")
	}
	if got := caster.EquippedItems[SlotBujuk].Dura; got != 5000 {
		t.Fatalf("poison powder dura = %d, want unchanged 5000", got)
	}
	if len(w.pendingSpells) != 0 {
		t.Fatalf("pending poison spells = %d, want 0", len(w.pendingSpells))
	}
}

func TestDoSpellPreservesStartedFailureContextWithoutAmulet(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	skill, ok := w.Skill("灵魂火符")
	if !ok {
		t.Fatal("skill 灵魂火符 missing from config")
	}
	cost := w.SpellCost(skill, storage.SkillState{ID: "灵魂火符", Level: 0})
	caster.MP = cost + 20
	caster.Skills = storage.SkillStates{{ID: "灵魂火符", Level: 0}}

	result, err := w.DoSpell(caster, "灵魂火符", caster.X+1, caster.Y, 0, []storage.Character{caster})
	if err == nil {
		t.Fatal("DoSpell() error = nil, want started failure")
	}
	if !result.SpellStarted || result.Character.ID != caster.ID {
		t.Fatalf("result = %+v, want started failure for caster", result)
	}
	if result.Character.MP != caster.MP-cost {
		t.Fatalf("result MP = %d, want %d after resource deduction", result.Character.MP, caster.MP-cost)
	}
	if len(result.Events) != 2 || result.Events[0].Kind != SpellEventCasterState || result.Events[1].Kind != SpellEventStart {
		t.Fatalf("failure events = %+v, want caster state then spell start", result.Events)
	}
}

func TestConsumePoisonPowderUsesPartialDurabilityLikeReference(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	caster.EquippedItems = map[int]storage.UserItem{
		SlotBujuk: {ItemID: "灰色药粉(少量)", MakeIndex: 7, Dura: 150, DuraMax: 200},
	}

	w.mu.Lock()
	item, ok := w.consumePoisonPowderLocked(&caster)
	w.mu.Unlock()
	if !ok {
		t.Fatal("consumePoisonPowderLocked() = false, want partial durability to be consumable")
	}
	if item.ItemID != "灰色药粉(少量)" || item.Dura != 50 {
		t.Fatalf("consumed item = %+v, want 50 durability powder", item)
	}
	if caster.EquippedItems[SlotBujuk].Dura != 50 {
		t.Fatalf("powder durability = %d, want 50", caster.EquippedItems[SlotBujuk].Dura)
	}
}

func TestConsumePoisonPowderRejectsEmptyDurability(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	caster.EquippedItems = map[int]storage.UserItem{
		SlotArmRingL: {ItemID: "灰色药粉(少量)", Dura: 0, DuraMax: 100},
	}

	w.mu.Lock()
	_, ok := w.consumePoisonPowderLocked(&caster)
	w.mu.Unlock()
	if ok {
		t.Fatal("consumePoisonPowderLocked() = true, want false for empty durability")
	}
}

func TestConsumeMagicAmuletUsesRoundedDurabilityLikeReference(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	caster.EquippedItems = map[int]storage.UserItem{
		SlotBujuk: {ItemID: "护身符", MakeIndex: 8, Dura: 150, DuraMax: 200},
	}

	w.mu.Lock()
	ok := w.consumeMagicAmuletLocked(&caster, 1)
	w.mu.Unlock()
	if !ok {
		t.Fatal("consumeMagicAmuletLocked() = false, want rounded partial durability to be consumable")
	}
	if caster.EquippedItems[SlotBujuk].Dura != 50 {
		t.Fatalf("amulet durability = %d, want 50", caster.EquippedItems[SlotBujuk].Dura)
	}
}

func TestConsumeMagicAmuletUsesReferenceBankersRounding(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	caster.EquippedItems = map[int]storage.UserItem{
		SlotBujuk: {ItemID: "护身符", Dura: 250, DuraMax: 300},
	}

	w.mu.Lock()
	ok := w.consumeMagicAmuletLocked(&caster, 3)
	w.mu.Unlock()
	if ok {
		t.Fatal("consumeMagicAmuletLocked() = true, want false because ROUND(2.5) is 2")
	}
}

func TestCastSkillPoisonAppliesCharacterStatusAndDamageReduction(t *testing.T) {
	cases := []struct {
		name         string
		powderItemID string
		powderSlot   int
		wantHealth   bool
		wantArmor    bool
	}{
		{
			name:         "gray",
			powderItemID: "灰色药粉(少量)",
			powderSlot:   SlotBujuk,
			wantHealth:   true,
		},
		{
			name:         "yellow",
			powderItemID: "黄色药粉(少量)",
			powderSlot:   SlotArmRingL,
			wantArmor:    true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w, caster := newTestWorldCharacter(t)
			w.mu.Lock()
			w.monsters = map[string]*Monster{}
			w.occupied = map[monsterPosition]string{}
			w.mu.Unlock()
			mapID := caster.MapID
			mp := w.data.Maps[mapID]
			baseX, baseY := -1, -1
			for dx := 6; dx < 18 && baseX < 0; dx++ {
				for dy := -2; dy <= 2; dy++ {
					tx := caster.X + dx
					ty := caster.Y + dy
					if !mp.Walkable(tx, ty) {
						continue
					}
					baseX, baseY = tx, ty
					break
				}
			}
			if baseX < 0 {
				t.Fatal("could not find clear tile for poison character test")
			}
			caster.X = baseX
			caster.Y = baseY
			caster.Dir = 2
			skill, ok := w.Skill("施毒术")
			if !ok {
				t.Fatalf("skill 施毒术 missing from config")
			}
			cost := w.SpellCost(skill, storage.SkillState{ID: "施毒术", Level: 3, Train: 0})
			caster.Level = 14
			caster.MP = cost + 20
			caster.Skills = storage.SkillStates{{ID: "施毒术", Level: 3, Train: 0}}
			caster.EquippedItems = map[int]storage.UserItem{
				tc.powderSlot: {ItemID: tc.powderItemID, Dura: 5000},
			}
			target, err := w.CreateCharacterWithAppearance("test2", "target", "wizard", 0, 0, mapID, baseX+1, baseY)
			if err != nil {
				t.Fatalf("CreateCharacter() target error = %v", err)
			}
			target.HP = 1000
			target.MaxHP = 1000
			beforeDura := caster.EquippedItems[tc.powderSlot].Dura
			updated, err := w.CastSkillWithPlayers(caster, "施毒术", baseX+1, baseY, CharacterActorID(target), []storage.Character{target})
			if err != nil {
				t.Fatalf("CastSkillWithPlayers() error = %v", err)
			}
			if got := updated.Character.MP; got != caster.MP-cost {
				t.Fatalf("MP = %d, want %d after poison", got, caster.MP-cost)
			}
			if got := updated.Character.EquippedItems[tc.powderSlot].Dura; got != beforeDura-100 {
				t.Fatalf("poison powder dura = %d, want %d", got, beforeDura-100)
			}
			delayedCaster := updated.Character
			delayedCaster.HP = 0
			delayedCaster.MapID = "other-map"
			movedTarget := target
			movedTarget.X += 5
			delivery, err := w.Tick([]PlayerSnapshot{{Character: delayedCaster}, {Character: movedTarget}}, time.Now().Add(1100*time.Millisecond))
			if err != nil {
				t.Fatalf("Tick() poison delivery error = %v", err)
			}
			if len(delivery.AffectedCharacters) != 1 {
				t.Fatalf("AffectedCharacters = %d, want 1", len(delivery.AffectedCharacters))
			}
			if len(delivery.StatusRefreshCharacters) != 1 || delivery.StatusRefreshCharacters[0].ID != target.ID {
				t.Fatalf("StatusRefreshCharacters = %+v, want poisoned target", delivery.StatusRefreshCharacters)
			}
			if len(delivery.PoisonNotifications) != 1 || delivery.PoisonNotifications[0].Character.ID != target.ID || delivery.PoisonNotifications[0].Seconds < 1 || delivery.PoisonNotifications[0].Points < 1 {
				t.Fatalf("PoisonNotifications = %+v, want one notification for poisoned target", delivery.PoisonNotifications)
			}
			poisoned := delivery.AffectedCharacters[0]
			stored, ok := w.store.Character(target.ID)
			if !ok || stored.PoisonHealthLevel != poisoned.PoisonHealthLevel || stored.PoisonArmorLevel != poisoned.PoisonArmorLevel || stored.PoisonHealthUntil != poisoned.PoisonHealthUntil || stored.PoisonArmorUntil != poisoned.PoisonArmorUntil {
				t.Fatalf("stored poisoned target = %+v, want delayed poison state %+v", stored, poisoned)
			}
			if poisoned.TargetID != "" {
				t.Fatalf("poisoned target = %q, want no target after caster leaves map", poisoned.TargetID)
			}
			if tc.wantHealth {
				if poisoned.PoisonHealthUntil == 0 || poisoned.PoisonHealthTickAt == 0 || poisoned.PoisonHealthStartAt == 0 {
					t.Fatalf("character poison health = %+v, want active health poison", poisoned)
				}
				if !time.Unix(0, poisoned.PoisonHealthStartAt).After(time.Now().Add(-time.Second)) {
					t.Fatalf("character poison health start = %d, want delivery-time start", poisoned.PoisonHealthStartAt)
				}
				beforeHP := poisoned.HP
				tickResult, err := w.Tick([]PlayerSnapshot{{Character: poisoned}}, time.Now().Add(5*time.Second))
				if err != nil {
					t.Fatalf("Tick() error = %v", err)
				}
				if len(tickResult.Characters) != 1 {
					t.Fatalf("Tick() characters = %d, want 1", len(tickResult.Characters))
				}
				updatedTarget := tickResult.Characters[0]
				if updatedTarget.HP >= beforeHP {
					t.Fatalf("character HP = %d, want reduced from %d after poison tick", updatedTarget.HP, beforeHP)
				}
			}
			if tc.wantArmor {
				if poisoned.PoisonArmorLevel == 0 || poisoned.PoisonArmorUntil == 0 || poisoned.PoisonArmorStartAt == 0 {
					t.Fatalf("character poison armor = %+v, want active armor poison", poisoned)
				}
				if !time.Unix(0, poisoned.PoisonArmorStartAt).After(time.Now().Add(-time.Second)) {
					t.Fatalf("character poison armor start = %d, want delivery-time start", poisoned.PoisonArmorStartAt)
				}
				mon := &Monster{ID: "毒测试怪", MinAttack: 10, MaxAttack: 10}
				_, baseHit, err := w.monsterAttackCharacterLocked(mon, target)
				if err != nil {
					t.Fatalf("monsterAttackCharacterLocked() baseline error = %v", err)
				}
				active := poisoned
				active.PoisonArmorStartAt = time.Now().Add(-time.Second).UnixNano()
				_, poisonHit, err := w.monsterAttackCharacterLocked(mon, active)
				if err != nil {
					t.Fatalf("monsterAttackCharacterLocked() poison error = %v", err)
				}
				if poisonHit.Damage <= baseHit.Damage {
					t.Fatalf("poison damage = %d, want greater than baseline %d", poisonHit.Damage, baseHit.Damage)
				}
			}
		})
	}
}

func TestPoisonRefreshKeepsStatusBroadcastStable(t *testing.T) {
	now := time.Now()
	target := storage.Character{PoisonHealthLevel: 4, PoisonHealthStartAt: now.Add(-time.Second).UnixNano(), PoisonHealthUntil: now.Add(5 * time.Second).UnixNano()}
	previous := characterStatus(target, now, false)
	setCharacterHealthPoisonLocked(&target, 4, now.Add(20*time.Second), now)
	if got := characterStatus(target, now, false); got != previous {
		t.Fatalf("poison status = %#x, want unchanged %#x after duration refresh", got, previous)
	}
}

func TestCharacterStatusKeepsExpiredStateUntilTick(t *testing.T) {
	now := time.Now()
	ch := storage.Character{ParalyzedUntil: now.Add(-time.Second).UnixNano()}
	if got := characterStatus(ch, now, false); got&0x04000000 != 0 {
		t.Fatal("time-filtered status unexpectedly reports paralysis")
	}
	if got := (&World{}).CharacterStatus(ch); got&0x04000000 == 0 {
		t.Fatal("CharacterStatus() cleared paralysis before Tick")
	}
}

func TestPoisonRefreshUsesLatestPointAndResetsTick(t *testing.T) {
	now := time.Now()
	target := storage.Character{
		PoisonHealthLevel:   8,
		PoisonHealthStartAt: now.Add(-time.Second).UnixNano(),
		PoisonHealthUntil:   now.Add(20 * time.Second).UnixNano(),
		PoisonHealthTickAt:  now.Add(-time.Second).UnixNano(),
	}
	setCharacterHealthPoisonLocked(&target, 3, now.Add(5*time.Second), now)
	if target.PoisonHealthLevel != 3 {
		t.Fatalf("poison point = %d, want latest point 3", target.PoisonHealthLevel)
	}
	if target.PoisonHealthUntil != now.Add(20*time.Second).UnixNano() {
		t.Fatalf("poison expiry = %d, want existing later expiry", target.PoisonHealthUntil)
	}
	if target.PoisonHealthTickAt != now.UnixNano() {
		t.Fatalf("poison tick = %d, want reset at refresh", target.PoisonHealthTickAt)
	}
}

func TestMonsterPoisonRefreshUsesLatestPointAndResetsTick(t *testing.T) {
	now := time.Now()
	mon := &Monster{
		PoisonHealthLevel:   8,
		PoisonHealthStartAt: now.Add(-time.Second),
		PoisonHealthUntil:   now.Add(20 * time.Second),
		PoisonHealthTickAt:  now.Add(-time.Second),
	}
	setMonsterHealthPoisonLocked(mon, 3, now.Add(5*time.Second), "caster", now)
	if mon.PoisonHealthLevel != 3 {
		t.Fatalf("monster poison point = %d, want latest point 3", mon.PoisonHealthLevel)
	}
	if !mon.PoisonHealthUntil.Equal(now.Add(20 * time.Second)) {
		t.Fatalf("monster poison expiry = %s, want existing later expiry", mon.PoisonHealthUntil)
	}
	if !mon.PoisonHealthTickAt.Equal(now) {
		t.Fatalf("monster poison tick = %s, want reset at refresh", mon.PoisonHealthTickAt)
	}
}

func TestMonsterNameColorMatchesReferenceStatePriority(t *testing.T) {
	now := time.Now()
	if got := MonsterNameColor(Monster{CrazyUntil: now.Add(time.Minute)}); got != 0xF9 {
		t.Fatalf("crazy name color = %#x, want %#x", got, uint16(0xF9))
	}
	if got := MonsterNameColor(Monster{CrazyUntil: now.Add(time.Minute), HolySeizeUntil: now.Add(time.Minute)}); got != 0x7D {
		t.Fatalf("holy seize name color = %#x, want %#x to override crazy", got, uint16(0x7D))
	}
}

func TestCastSkillTamingMonsterCreatesTimedMasterRelation(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	w.mu.Lock()
	w.monsters = map[string]*Monster{}
	w.occupied = map[monsterPosition]string{}
	w.mu.Unlock()
	mapID := caster.MapID
	mp := w.data.Maps[mapID]
	targetX, targetY := -1, -1
	for dx := 1; dx < 8 && targetX < 0; dx++ {
		for dy := -2; dy <= 2; dy++ {
			tx := caster.X + dx
			ty := caster.Y + dy
			if !mp.Walkable(tx, ty) {
				continue
			}
			clear := true
			w.mu.Lock()
			for _, mon := range w.monsters {
				if mon != nil && mon.Alive && mon.MapID == mapID && abs(mon.X-tx) <= 1 && abs(mon.Y-ty) <= 1 {
					clear = false
					break
				}
			}
			w.mu.Unlock()
			if clear {
				targetX, targetY = tx, ty
				break
			}
		}
	}
	if targetX < 0 {
		t.Fatal("could not find clear tile for taming test")
	}
	result, err := w.SpawnMonsterByNameAt(mapID, targetX, targetY, "鸡", 1)
	if err != nil {
		t.Fatalf("SpawnMonsterByNameAt() error = %v", err)
	}
	if len(result.Monsters) != 1 {
		t.Fatalf("SpawnMonsterByNameAt() monsters = %d, want 1", len(result.Monsters))
	}
	spawned := result.Monsters[0]
	targetX, targetY = spawned.X, spawned.Y
	caster.Level = 100
	caster.MP = 100
	caster.Skills = storage.SkillStates{{ID: "诱惑之光", Level: 10, Train: 0}}
	w.mu.Lock()
	src := &seqSource{vals: []int64{0, 0, 0}}
	w.rand = rand.New(src)
	w.mu.Unlock()
	updated, err := w.CastSkillWithPlayers(caster, "诱惑之光", targetX, targetY, MonsterActorID(spawned), nil)
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	if len(updated.AffectedMonsters) != 1 {
		t.Fatalf("AffectedMonsters = %d, want 1", len(updated.AffectedMonsters))
	}
	controlled := updated.AffectedMonsters[0]
	if controlled.MasterID != caster.ID {
		t.Fatalf("controlled.MasterID = %q, want %q", controlled.MasterID, caster.ID)
	}
	if controlled.MasterExpiresAt.IsZero() {
		t.Fatal("controlled.MasterExpiresAt = zero, want active expiry")
	}
	if !controlled.NoTame {
		t.Fatal("controlled.NoTame = false, want true")
	}
	if controlled.MasterName != caster.Name {
		t.Fatalf("controlled.MasterName = %q, want %q", controlled.MasterName, caster.Name)
	}
	if controlled.SlaveMakeLevel != caster.Skills[0].Level {
		t.Fatalf("controlled.SlaveMakeLevel = %d, want %d", controlled.SlaveMakeLevel, caster.Skills[0].Level)
	}
	if controlled.MasterTick.IsZero() {
		t.Fatal("controlled.MasterTick is zero, want relation start time")
	}
	if !hasSpellEventForMonster(updated.Events, SpellEventMonsterUsername, controlled.ID) {
		t.Fatalf("Events = %+v, want successful tame username refresh", updated.Events)
	}
	if hasSpellEventForMonster(updated.Events, SpellEventMonsterNameColor, controlled.ID) {
		t.Fatalf("Events = %+v, want no name/color refresh for existing tamed monster", updated.Events)
	}
	w.mu.Lock()
	if mon, ok := w.monsters[controlled.ID]; ok {
		mon.MasterExpiresAt = time.Now().Add(-time.Second)
	}
	w.mu.Unlock()
	if _, err := w.Tick([]PlayerSnapshot{{Character: updated.Character}}, time.Now()); err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	monsters, _ := w.SnapshotAround(updated.Character.MapID, updated.Character.X, updated.Character.Y, 10)
	found := false
	for _, mon := range monsters {
		if mon.ID == controlled.ID {
			found = true
			if mon.MasterID != "" || mon.MasterName != "" || mon.NoTame || !mon.MasterExpiresAt.IsZero() {
				t.Fatalf("expired controlled relation = %+v, want released", mon)
			}
		}
	}
	if !found {
		t.Fatalf("controlled monster %s missing after expiry", controlled.ID)
	}
}

func TestCastSkillTamingFailureOpensHolySeizeMode(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	w.mu.Lock()
	w.monsters = map[string]*Monster{}
	w.occupied = map[monsterPosition]string{}
	w.mu.Unlock()
	targetResult, err := w.SpawnMonsterByNameAt(caster.MapID, caster.X+1, caster.Y, "鸡", 1)
	if err != nil || len(targetResult.Monsters) != 1 {
		t.Fatalf("SpawnMonsterByNameAt() = %+v, %v", targetResult.Monsters, err)
	}
	target := targetResult.Monsters[0]
	caster.Level = 20
	caster.MP = 100
	caster.Skills = storage.SkillStates{{ID: "诱惑之光", Level: 10, Train: 0}}
	w.mu.Lock()
	w.monsters[target.ID].Level = caster.Level + 3
	w.rand = rand.New(zeroSource{})
	w.mu.Unlock()
	result, err := w.CastSkillWithPlayers(caster, "诱惑之光", target.X, target.Y, MonsterActorID(target), nil)
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	if len(result.NameColorMonsters) != 1 || result.NameColorMonsters[0].HolySeizeUntil.IsZero() {
		w.mu.Lock()
		live := w.monsters[target.ID]
		w.mu.Unlock()
		t.Fatalf("NameColorMonsters = %+v, live=%+v, magic target=%d, training=%v, skill level=%d, want active holy seize failure state", result.NameColorMonsters, live, result.MagicTargetID, result.SkillTraining, result.SkillLevel)
	}
	if len(result.NameColorMonsters) != 1 {
		t.Fatalf("NameColorMonsters = %d, want 1", len(result.NameColorMonsters))
	}
	if !hasSpellEventForMonster(result.Events, SpellEventMonsterNameColor, target.ID) {
		t.Fatalf("Events = %+v, want failed target name/color refresh", result.Events)
	}
}

func hasSpellEventForMonster(events []SpellEvent, kind SpellEventKind, monsterID string) bool {
	for _, event := range events {
		if event.Kind == kind && event.Monster.ID == monsterID {
			return true
		}
	}
	return false
}

func TestCastSkillTamingFailureOpensCrazyMode(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	w.mu.Lock()
	w.monsters = map[string]*Monster{}
	w.occupied = map[monsterPosition]string{}
	w.mu.Unlock()
	targetResult, err := w.SpawnMonsterByNameAt(caster.MapID, caster.X+1, caster.Y, "鸡", 1)
	if err != nil || len(targetResult.Monsters) != 1 {
		t.Fatalf("SpawnMonsterByNameAt() = %+v, %v", targetResult.Monsters, err)
	}
	target := targetResult.Monsters[0]
	caster.Level = 20
	w.mu.Lock()
	w.monsters[target.ID].Level = caster.Level + 1
	w.mu.Unlock()
	caster.MP = 100
	caster.Skills = storage.SkillStates{{ID: "诱惑之光", Level: 0, Train: 0}}
	w.rand = rand.New(&seqSource{vals: []int64{0, 0, 0, 0, 0}})
	result, err := w.CastSkillWithPlayers(caster, "诱惑之光", target.X, target.Y, MonsterActorID(target), nil)
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	if len(result.NameColorMonsters) != 1 || result.NameColorMonsters[0].CrazyUntil.IsZero() {
		t.Fatalf("NameColorMonsters = %+v, want active crazy failure state", result.NameColorMonsters)
	}
	if len(result.NameColorMonsters) != 1 {
		t.Fatalf("NameColorMonsters = %d, want 1", len(result.NameColorMonsters))
	}
	if !hasSpellEventForMonster(result.Events, SpellEventMonsterNameColor, target.ID) {
		t.Fatalf("Events = %+v, want failed target name/color refresh", result.Events)
	}
}

func TestCastSkillTamingTransfersWithoutDamagingOldMaster(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	oldMaster, err := w.CreateCharacterWithAppearance("old", "old-master", "warrior", 0, 0, caster.MapID, caster.X+4, caster.Y)
	if err != nil {
		t.Fatalf("CreateCharacterWithAppearance() error = %v", err)
	}
	oldMaster.HP = 100
	w.mu.Lock()
	w.monsters = map[string]*Monster{}
	w.occupied = map[monsterPosition]string{}
	w.mu.Unlock()
	targetResult, err := w.SpawnMonsterByNameAt(caster.MapID, caster.X+1, caster.Y, "鸡", 1)
	if err != nil || len(targetResult.Monsters) != 1 {
		t.Fatalf("SpawnMonsterByNameAt() = %+v, %v", targetResult.Monsters, err)
	}
	target := targetResult.Monsters[0]
	w.mu.Lock()
	w.monsters[target.ID].MasterID = oldMaster.ID
	w.monsters[target.ID].NoTame = false
	w.mu.Unlock()
	caster.Level = 100
	caster.MP = 100
	caster.Skills = storage.SkillStates{{ID: "诱惑之光", Level: 10, Train: 0}}
	w.rand = rand.New(zeroSource{})
	result, err := w.CastSkillWithPlayers(caster, "诱惑之光", target.X, target.Y, MonsterActorID(target), []storage.Character{oldMaster})
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	if len(result.AffectedMonsters) != 1 || result.AffectedMonsters[0].MasterID != caster.ID {
		t.Fatalf("transferred monster = %+v, want caster master", result.AffectedMonsters)
	}
	if len(result.AffectedCharacters) != 0 {
		t.Fatalf("old master effects = %+v, want no character update", result.AffectedCharacters)
	}
	if oldMaster.HP != 100 {
		t.Fatalf("old master HP = %d, want unchanged 100", oldMaster.HP)
	}
}

func TestSpellEventsTamingEmitsUsernameOnly(t *testing.T) {
	w := &World{}
	mon := Monster{ID: "monster-1", Name: "鸡"}
	result := SkillCastResult{
		Character:        storage.Character{ID: "caster-1", MP: 90},
		SkillID:          "诱惑之光",
		AffectedMonsters: []Monster{mon},
		NameMonsters:     []Monster{mon},
	}
	events := w.spellEvents(storage.Character{ID: "caster-1", MP: 100}, result, data.StdSkill{}, 10, 10, 0)
	stateEvents := make([]SpellEventKind, 0, 2)
	for _, event := range events {
		if event.Monster.ID != mon.ID {
			continue
		}
		if event.Kind == SpellEventMonsterNameColor || event.Kind == SpellEventMonsterUsername {
			stateEvents = append(stateEvents, event.Kind)
		}
	}
	want := []SpellEventKind{SpellEventMonsterUsername}
	if !reflect.DeepEqual(stateEvents, want) {
		t.Fatalf("taming monster state events = %v, want %v", stateEvents, want)
	}
}

func TestSpellEventsEmitCharacterNameColorAfterMagicHit(t *testing.T) {
	w := &World{}
	caster := storage.Character{ID: "caster-1"}
	marked := storage.Character{ID: "caster-1", PKFlag: true}
	events := w.spellEvents(caster, SkillCastResult{
		Character:           marked,
		SkillID:             "火球术",
		CharacterHits:       []CharacterHit{{Character: storage.Character{ID: "target-1"}, Magic: true, Damage: 1}},
		NameColorCharacters: []storage.Character{marked},
	}, data.StdSkill{}, 10, 10, 0)
	hitIndex, colorIndex := -1, -1
	for i, event := range events {
		if event.Kind == SpellEventCharacterHit {
			hitIndex = i
		}
		if event.Kind == SpellEventCharacterNameColor {
			colorIndex = i
			if event.Character.ID != caster.ID {
				t.Fatalf("name color event character = %q, want %q", event.Character.ID, caster.ID)
			}
		}
	}
	if hitIndex < 0 || colorIndex != hitIndex+1 {
		t.Fatalf("spell event order = %v, want character hit followed by name color", events)
	}
}

func TestCastSkillTamingMonsterLimitsControlledCountToFive(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	mapID, x, y := caster.MapID, caster.X, caster.Y
	caster.Level = 100
	caster.MP = 100
	caster.Skills = storage.SkillStates{{ID: "诱惑之光", Level: 10, Train: 0}}
	w.mu.Lock()
	tpl, ok := w.data.Monsters["鸡"]
	if !ok {
		w.mu.Unlock()
		t.Fatal("monster 鸡 missing from configs")
	}
	for i := 0; i < defaultTamingCount; i++ {
		id := fmt.Sprintf("tamed-%d", i)
		mon := newMonster(w, id, tpl, mapID, x+i+2, y, data.StdSpawn{MapID: mapID, MonsterID: tpl.ID, X: x + i + 2, Y: y})
		mon.MasterID = caster.ID
		mon.MasterExpiresAt = time.Now().Add(time.Hour)
		w.monsters[id] = mon
	}
	w.mu.Unlock()
	targetX, targetY := x+8, y
	result, err := w.SpawnMonsterByNameAt(mapID, targetX, targetY, "鸡", 1)
	if err != nil {
		t.Fatalf("SpawnMonsterByNameAt() error = %v", err)
	}
	if len(result.Monsters) != 1 {
		t.Fatalf("SpawnMonsterByNameAt() monsters = %d, want 1", len(result.Monsters))
	}
	cast, err := w.CastSkillWithPlayers(caster, "诱惑之光", targetX, targetY, MonsterActorID(result.Monsters[0]), nil)
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v, want no-effect completion", err)
	}
	if len(cast.AffectedMonsters) != 0 {
		t.Fatalf("AffectedMonsters = %d, want no controlled monster", len(cast.AffectedMonsters))
	}
}

func TestCastSkillTamingMonsterUsesCasterLevelInSuccessCheck(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	w.mu.Lock()
	w.monsters = map[string]*Monster{}
	w.occupied = map[monsterPosition]string{}
	w.mu.Unlock()
	mapID, x, y := caster.MapID, caster.X, caster.Y
	caster.Level = 80
	caster.MP = 100
	caster.Skills = storage.SkillStates{{ID: "诱惑之光", Level: 0, Train: 0}}
	targetX, targetY := x+8, y
	result, err := w.SpawnMonsterByNameAt(mapID, targetX, targetY, "鸡", 1)
	if err != nil {
		t.Fatalf("SpawnMonsterByNameAt() error = %v", err)
	}
	if len(result.Monsters) != 1 {
		t.Fatalf("SpawnMonsterByNameAt() monsters = %d, want 1", len(result.Monsters))
	}
	spawned := result.Monsters[0]
	targetX, targetY = spawned.X, spawned.Y
	w.mu.Lock()
	mon := w.monsters[spawned.ID]
	mon.Level = 40
	src := &seqSource{vals: []int64{0, 0, 0}}
	w.rand = rand.New(src)
	w.mu.Unlock()
	updated, err := w.CastSkillWithPlayers(caster, "诱惑之光", targetX, targetY, MonsterActorID(spawned), nil)
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	if len(updated.AffectedMonsters) != 1 {
		t.Fatalf("AffectedMonsters = %d, want 1", len(updated.AffectedMonsters))
	}
	if updated.AffectedMonsters[0].MasterID != caster.ID {
		t.Fatalf("controlled.MasterID = %q, want %q", updated.AffectedMonsters[0].MasterID, caster.ID)
	}
}

func TestCastSkillTamingMonsterRejectsTargetsAboveCasterPlusTwo(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	w.mu.Lock()
	w.monsters = map[string]*Monster{}
	w.occupied = map[monsterPosition]string{}
	w.mu.Unlock()
	mapID, x, y := caster.MapID, caster.X, caster.Y
	caster.Level = 10
	caster.MP = 100
	caster.Skills = storage.SkillStates{{ID: "诱惑之光", Level: 0, Train: 0}}
	targetX, targetY := x+8, y
	result, err := w.SpawnMonsterByNameAt(mapID, targetX, targetY, "鸡", 1)
	if err != nil {
		t.Fatalf("SpawnMonsterByNameAt() error = %v", err)
	}
	if len(result.Monsters) != 1 {
		t.Fatalf("SpawnMonsterByNameAt() monsters = %d, want 1", len(result.Monsters))
	}
	w.mu.Lock()
	if mon := w.monsters[result.Monsters[0].ID]; mon != nil {
		mon.Level = caster.Level + 3
	}
	w.rand = rand.New(zeroSource{})
	w.mu.Unlock()
	cast, err := w.CastSkillWithPlayers(caster, "诱惑之光", targetX, targetY, MonsterActorID(result.Monsters[0]), nil)
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v, want no-effect completion", err)
	}
	if len(cast.AffectedMonsters) != 0 {
		t.Fatalf("AffectedMonsters = %d, want no controlled monster", len(cast.AffectedMonsters))
	}
	if len(cast.NameColorMonsters) != 1 || cast.NameColorMonsters[0].HolySeizeUntil.IsZero() {
		t.Fatalf("NameColorMonsters = %+v, want holy-seize state for over-level target", cast.NameColorMonsters)
	}
}

func TestCastSkillTamingMonsterRejectsAboveLevel50(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	w.mu.Lock()
	w.monsters = map[string]*Monster{}
	w.occupied = map[monsterPosition]string{}
	w.mu.Unlock()
	mapID, x, y := caster.MapID, caster.X, caster.Y
	caster.Level = 100
	caster.MP = 100
	caster.Skills = storage.SkillStates{{ID: "诱惑之光", Level: 0, Train: 0}}
	targetX, targetY := x+8, y
	result, err := w.SpawnMonsterByNameAt(mapID, targetX, targetY, "鸡", 1)
	if err != nil {
		t.Fatalf("SpawnMonsterByNameAt() error = %v", err)
	}
	if len(result.Monsters) != 1 {
		t.Fatalf("SpawnMonsterByNameAt() monsters = %d, want 1", len(result.Monsters))
	}
	w.mu.Lock()
	if mon := w.monsters[result.Monsters[0].ID]; mon != nil {
		mon.Level = 51
	}
	w.mu.Unlock()
	cast, err := w.CastSkillWithPlayers(caster, "诱惑之光", targetX, targetY, MonsterActorID(result.Monsters[0]), nil)
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v, want no-effect completion", err)
	}
	if len(cast.AffectedMonsters) != 0 {
		t.Fatalf("AffectedMonsters = %d, want no controlled monster", len(cast.AffectedMonsters))
	}
}

func TestCastSkillInsightPrefersExplicitTargetID(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	mapID, x, y := caster.MapID, caster.X, caster.Y
	targetA, err := w.CreateCharacterWithAppearance("test", "target-a", "wizard", 0, 0, mapID, x+1, y)
	if err != nil {
		t.Fatalf("CreateCharacter() targetA error = %v", err)
	}
	targetB, err := w.CreateCharacterWithAppearance("test", "target-b", "warrior", 0, 0, mapID, x+1, y+1)
	if err != nil {
		t.Fatalf("CreateCharacter() targetB error = %v", err)
	}
	caster.Skills = storage.SkillStates{{ID: "心灵启示", Level: 0, Train: 0}}
	w.mu.Lock()
	w.rand = rand.New(&seqSource{vals: []int64{0}})
	w.mu.Unlock()
	updated, err := w.CastSkillWithPlayers(caster, "心灵启示", x+1, y, CharacterActorID(targetB), []storage.Character{targetA, targetB})
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	if updated.MagicTargetID != CharacterActorID(targetB) {
		t.Fatalf("MagicTargetID = %d, want explicit targetID %d", updated.MagicTargetID, CharacterActorID(targetB))
	}
	w.mu.Lock()
	_, found := w.characterAtPointLocked([]storage.Character{targetA, targetB}, mapID, x+1, y, 999999)
	w.mu.Unlock()
	if found {
		t.Fatal("characterAtPointLocked() found fallback target for an unknown explicit target ID")
	}
	w.mu.Lock()
	if len(w.pendingSpells) != 1 || w.pendingSpells[0].TargetCharacterID != targetB.ID {
		t.Fatalf("pending show health = %+v, want explicit target %q", w.pendingSpells, targetB.ID)
	}
	w.mu.Unlock()
}

func TestCastSkillInsightDoesNotUseCoordinateFallback(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	mapID, x, y := caster.MapID, caster.X, caster.Y
	targetA, err := w.CreateCharacterWithAppearance("test", "target-a", "wizard", 0, 0, mapID, x+1, y)
	if err != nil {
		t.Fatalf("CreateCharacter() targetA error = %v", err)
	}
	targetB, err := w.CreateCharacterWithAppearance("test", "target-b", "warrior", 0, 0, mapID, x+1, y+1)
	if err != nil {
		t.Fatalf("CreateCharacter() targetB error = %v", err)
	}
	caster.Skills = storage.SkillStates{{ID: "心灵启示", Level: 0, Train: 0}}
	w.mu.Lock()
	w.rand = rand.New(&seqSource{vals: []int64{0}})
	w.mu.Unlock()
	updated, err := w.CastSkillWithPlayers(caster, "心灵启示", x+1, y, 0, []storage.Character{targetA, targetB})
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	if updated.MagicTargetID != 0 {
		t.Fatalf("MagicTargetID = %d, want no target without explicit ActorID", updated.MagicTargetID)
	}
	if len(w.pendingSpells) != 0 {
		t.Fatalf("pending show health = %+v, want no coordinate fallback", w.pendingSpells)
	}
}

func TestDoSpellAtMaxSkillLevelDoesNotConsumeTrainingRandom(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	caster.Skills = storage.SkillStates{{ID: "魔法盾", Level: 3, Train: 0}}
	caster.MP = 100
	w.mu.Lock()
	source := &seqSource{vals: []int64{1}}
	w.rand = rand.New(source)
	w.mu.Unlock()
	if _, err := w.DoSpell(caster, "魔法盾", caster.X, caster.Y, 0, nil); err != nil {
		t.Fatalf("DoSpell() error = %v", err)
	}
	if source.idx != 0 {
		t.Fatalf("training random calls = %d, want 0 at max skill level", source.idx)
	}
}

func TestDoSpellLegacyClientStopsHighMagicAfterStart(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	caster.SoftVersionDate = 0
	caster.ClientTick = 0
	caster.Skills = storage.SkillStates{{ID: "困魔咒", Level: 1, Train: 0}}
	skill, ok := w.Skill("困魔咒")
	if !ok {
		t.Fatal("困魔咒 skill missing")
	}
	caster.MP = w.SpellCost(skill, caster.Skills[0]) + 1
	result, err := w.DoSpell(caster, "困魔咒", caster.X, caster.Y, 0, nil)
	if err == nil {
		t.Fatal("DoSpell() error = nil, want legacy-client failure after start")
	}
	if !result.SpellStarted || result.Character.MP != caster.MP-w.SpellCost(skill, caster.Skills[0]) {
		t.Fatalf("legacy result = %+v, want started with consumed mana", result)
	}
	if result.SkillTraining || len(w.pendingSpells) != 0 {
		t.Fatalf("legacy branch mutated spell state: training=%t pending=%d", result.SkillTraining, len(w.pendingSpells))
	}
	magicFire := 0
	start := 0
	for _, event := range result.Events {
		switch event.Kind {
		case SpellEventStart:
			start++
		case SpellEventMagicFire:
			magicFire++
		}
	}
	if start != 1 || magicFire != 0 || len(result.Events) == 0 {
		t.Fatalf("legacy events = start:%d magic-fire:%d events=%+v, want started failure without magic fire", start, magicFire, result.Events)
	}
}

func TestDoChargeAtTrainingLevelDoesNotConsumeTrainingRandom(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	skill, ok := w.Skill("野蛮冲撞")
	if !ok {
		t.Fatal("野蛮冲撞 skill missing")
	}
	caster.Level = skill.NeedLevel1
	caster.MP = 100
	caster.Skills = storage.SkillStates{{ID: "野蛮冲撞", Level: 0, Train: 0}}
	w.mu.Lock()
	source := &seqSource{vals: []int64{1}}
	w.rand = rand.New(source)
	w.mu.Unlock()
	if _, err := w.DoCharge(caster, caster.Dir, nil); err != nil {
		t.Fatalf("DoCharge() error = %v", err)
	}
	if source.idx != 0 {
		t.Fatalf("training random calls = %d, want 0 at training level boundary", source.idx)
	}
}

func TestDoChargeWithoutTargetDoesNotTrainAfterMovement(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	skill, ok := w.Skill("野蛮冲撞")
	if !ok {
		t.Fatal("野蛮冲撞 skill missing")
	}
	caster.Level = skill.NeedLevel1 + 1
	caster.MP = 100
	caster.Skills = storage.SkillStates{{ID: "野蛮冲撞", Level: 0, Train: 0}}
	result, err := w.DoCharge(caster, caster.Dir, nil)
	if err != nil {
		t.Fatalf("DoCharge() error = %v", err)
	}
	if result.SkillTraining || result.Character.Skills[0].Train != 0 {
		t.Fatalf("charge without target trained skill: result=%+v", result)
	}
}

func TestDoChargeUpdatesDirectionBeforeManaCheck(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	caster.Skills = storage.SkillStates{{ID: "野蛮冲撞", Level: 3, Train: 0}}
	caster.Dir = 0
	caster.MP = 0
	result, err := w.DoCharge(caster, 2, nil)
	if err != nil {
		t.Fatalf("DoCharge() error = %v", err)
	}
	if result.Character.Dir != 2 {
		t.Fatalf("direction = %d, want 2 after insufficient MP", result.Character.Dir)
	}
	if result.SpellStarted {
		t.Fatalf("SpellStarted = true, want false with insufficient MP")
	}
}

func TestCastSkillTamingDeathDefersToWorldTick(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	caster.Level = 20
	caster.MP = 100
	caster.Skills = storage.SkillStates{{ID: "诱惑之光", Level: 3, Train: 0}}
	spawned, err := w.SpawnMonsterByNameAt(caster.MapID, caster.X+1, caster.Y, "鸡", 1)
	if err != nil || len(spawned.Monsters) != 1 {
		t.Fatalf("SpawnMonsterByNameAt() = %+v, %v", spawned.Monsters, err)
	}
	monsterID := spawned.Monsters[0].ID
	w.mu.Lock()
	mon := w.monsters[monsterID]
	mon.Level = caster.Level
	mon.MaxHP = 100
	mon.HP = 100
	mon.MasterID = caster.ID
	w.rand = rand.New(zeroSource{})
	w.mu.Unlock()

	cast, err := w.CastSkillWithPlayers(caster, "诱惑之光", mon.X, mon.Y, MonsterActorID(*mon), nil)
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	if len(cast.AffectedMonsters) != 0 {
		t.Fatalf("cast affected monsters = %+v, want none before Tick", cast.AffectedMonsters)
	}
	w.mu.Lock()
	pending := w.monsters[monsterID]
	w.mu.Unlock()
	if pending == nil || pending.HP != 0 || !pending.Alive || !pending.PendingDeath {
		t.Fatalf("pending tame death = %+v, want live HP-zero pending object", pending)
	}

	tick, err := w.Tick([]PlayerSnapshot{{Character: caster}}, time.Now())
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if len(tick.MonsterDeaths) != 1 || tick.MonsterDeaths[0].MonsterID != monsterID {
		t.Fatalf("monster deaths = %+v, want one deferred death", tick.MonsterDeaths)
	}
	if tick.MonsterDeaths[0].Experience != 0 || len(tick.MonsterDeaths[0].Drops) != 0 {
		t.Fatalf("deferred tame death = %+v, want no experience or drops", tick.MonsterDeaths[0])
	}
}

func TestWorldTickSettlesPendingDeathsInObjectOrder(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	w.mu.Lock()
	w.monsters = map[string]*Monster{
		"death-late":  {ID: "death-late", MapID: caster.MapID, X: caster.X + 2, Y: caster.Y, HP: 0, MaxHP: 100, Alive: true, PendingDeath: true},
		"death-early": {ID: "death-early", MapID: caster.MapID, X: caster.X + 1, Y: caster.Y, HP: 0, MaxHP: 100, Alive: true, PendingDeath: true},
	}
	w.occupied = map[monsterPosition]string{}
	w.mu.Unlock()

	tick, err := w.Tick([]PlayerSnapshot{{Character: caster}}, time.Now())
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if len(tick.MonsterDeaths) != 2 || tick.MonsterDeaths[0].MonsterID != "death-early" || tick.MonsterDeaths[1].MonsterID != "death-late" {
		t.Fatalf("monster deaths = %+v, want coordinate order", tick.MonsterDeaths)
	}
}

func TestCastSkillTamingMonsterRespectsMonsterHpGate(t *testing.T) {
	prepare := func(maxHP int, seed int64) (*World, storage.Character, int, int, int32) {
		w, caster := newTestWorldCharacter(t)
		w.mu.Lock()
		w.monsters = map[string]*Monster{}
		w.occupied = map[monsterPosition]string{}
		w.mu.Unlock()
		mapID, x, y := caster.MapID, caster.X, caster.Y
		caster.Level = 100
		caster.MP = 100
		caster.Skills = storage.SkillStates{{ID: "诱惑之光", Level: 3, Train: 0}}
		targetX, targetY := x+8, y
		result, err := w.SpawnMonsterByNameAt(mapID, targetX, targetY, "鸡", 1)
		if err != nil {
			t.Fatalf("SpawnMonsterByNameAt() error = %v", err)
		}
		if len(result.Monsters) != 1 {
			t.Fatalf("SpawnMonsterByNameAt() monsters = %d, want 1", len(result.Monsters))
		}
		spawned := result.Monsters[0]
		targetX, targetY = spawned.X, spawned.Y
		w.mu.Lock()
		mon := w.monsters[spawned.ID]
		mon.MaxHP = maxHP
		mon.HP = maxHP
		w.rand = rand.New(rand.NewSource(seed))
		w.mu.Unlock()
		return w, caster, targetX, targetY, MonsterActorID(spawned)
	}

	seed := int64(-1)
	lowOK := false
	highFail := false
	var lowW *World
	var lowCaster storage.Character
	var lowX, lowY int
	var lowTargetID int32
	for s := int64(0); s < 1000; s++ {
		lowW, lowCaster, lowX, lowY, lowTargetID = prepare(100, s)
		lowUpdated, err := lowW.CastSkillWithPlayers(lowCaster, "诱惑之光", lowX, lowY, lowTargetID, nil)
		if err != nil {
			continue
		}
		if len(lowUpdated.AffectedMonsters) != 1 {
			continue
		}
		highW, highCaster, highX, highY, highTargetID := prepare(500, s)
		highUpdated, err := highW.CastSkillWithPlayers(highCaster, "诱惑之光", highX, highY, highTargetID, nil)
		if err != nil {
			continue
		}
		if len(highUpdated.AffectedMonsters) == 0 {
			seed = s
			lowOK = true
			highFail = true
			break
		}
	}
	if !lowOK || !highFail {
		t.Fatal("could not find a seed that distinguishes low and high HP taming")
	}
	lowW, lowCaster, lowX, lowY, lowTargetID = prepare(100, seed)
	lowUpdated, err := lowW.CastSkillWithPlayers(lowCaster, "诱惑之光", lowX, lowY, lowTargetID, nil)
	if err != nil {
		t.Fatalf("low HP CastSkillWithPlayers() error = %v", err)
	}
	if len(lowUpdated.AffectedMonsters) != 1 {
		t.Fatalf("low HP affected monsters = %d, want 1", len(lowUpdated.AffectedMonsters))
	}

	highW, highCaster, highX, highY, highTargetID := prepare(500, seed)
	highUpdated, err := highW.CastSkillWithPlayers(highCaster, "诱惑之光", highX, highY, highTargetID, nil)
	if err != nil {
		t.Fatalf("high HP CastSkillWithPlayers() error = %v", err)
	}
	if len(highUpdated.AffectedMonsters) != 0 {
		t.Fatalf("high HP affected monsters = %d, want 0", len(highUpdated.AffectedMonsters))
	}
}

func TestCastSkillParalysisQueuesDelayedMagic(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	w.mu.Lock()
	w.monsters = map[string]*Monster{}
	w.occupied = map[monsterPosition]string{}
	w.mu.Unlock()
	mapID := caster.MapID
	mp := w.data.Maps[mapID]
	targetX, targetY := -1, -1
	for dx := 2; dx < 8 && targetX < 0; dx++ {
		for dy := -2; dy <= 2; dy++ {
			tx := caster.X + dx
			ty := caster.Y + dy
			if !mp.Walkable(tx, ty) {
				continue
			}
			clear := true
			w.mu.Lock()
			for _, mon := range w.monsters {
				if mon != nil && mon.Alive && mon.MapID == mapID && abs(mon.X-tx) <= 1 && abs(mon.Y-ty) <= 1 {
					clear = false
					break
				}
			}
			w.mu.Unlock()
			if clear {
				targetX, targetY = tx, ty
				break
			}
		}
	}
	if targetX < 0 {
		t.Fatal("could not find clear tile for trap test")
	}
	result, err := w.SpawnMonsterByNameAt(mapID, targetX, targetY, "鸡", 2)
	if err != nil {
		t.Fatalf("SpawnMonsterByNameAt() error = %v", err)
	}
	if len(result.Monsters) != 2 {
		t.Fatalf("SpawnMonsterByNameAt() monsters = %d, want 2", len(result.Monsters))
	}
	w.mu.Lock()
	for _, mon := range w.monsters {
		mon.MagicDefense = 0
		mon.MagicDefenseMax = 0
	}
	w.mu.Unlock()
	caster.Level = 100
	caster.MP = 100
	caster.Skills = storage.SkillStates{{ID: "困魔咒", Level: 5, Train: 0}}
	caster.EquippedItems = map[int]storage.UserItem{SlotBujuk: {ItemID: "护身符", Dura: 10000}}
	mabe := w.data.Skills["困魔咒"]
	mabe.Power = 100
	mabe.MaxPower = 100
	mabe.TrainLevel1 = 0
	w.data.Skills["困魔咒"] = mabe
	w.rand = rand.New(zeroSource{})
	updated, err := w.CastSkillWithPlayers(caster, "困魔咒", targetX, targetY, MonsterActorID(result.Monsters[0]), nil)
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	if len(updated.AffectedMonsters) != 0 || len(updated.GroundEvents) != 0 {
		t.Fatalf("immediate effects = monsters %d ground %d, want none", len(updated.AffectedMonsters), len(updated.GroundEvents))
	}
	if len(w.pendingSpells) != 3 {
		t.Fatalf("pending spells = %d, want first damage, second damage, and control", len(w.pendingSpells))
	}
	if w.monsters[result.Monsters[0].ID].LastHitterID != caster.ID {
		t.Fatalf("monster last hitter = %+v, want immediate caster marker", w.monsters[result.Monsters[0].ID])
	}
	if got := w.pendingSpells[1].DueAt.Sub(w.pendingSpells[0].DueAt); got != 0 {
		t.Fatalf("second damage delay = %s, want same delivery time", got)
	}
	if got := w.pendingSpells[2].DueAt.Sub(w.pendingSpells[0].DueAt); got != 50*time.Millisecond {
		t.Fatalf("control delay = %s, want 50ms after damage", got)
	}
	damageAt := w.pendingSpells[0].DueAt
	if _, err := w.Tick([]PlayerSnapshot{{Character: updated.Character}}, damageAt); err != nil {
		t.Fatalf("Tick(damage) error = %v", err)
	}
	controlAt := damageAt.Add(50 * time.Millisecond)
	delivered, err := w.Tick([]PlayerSnapshot{{Character: updated.Character}}, controlAt)
	if err != nil {
		t.Fatalf("Tick(control) error = %v", err)
	}
	if len(delivered.StatusRefreshMonsters) != 1 || delivered.StatusRefreshMonsters[0].ID == "" {
		t.Fatalf("paralysis status refreshes = %+v, want one monster status refresh", delivered.StatusRefreshMonsters)
	}
	if len(delivered.AffectedMonsters) != 0 {
		t.Fatalf("paralysis affected monsters = %d, want no ordinary monster refresh", len(delivered.AffectedMonsters))
	}
	w.mu.Lock()
	paralyzed := false
	for _, mon := range w.monsters {
		if mon.ParalyzedUntil.After(controlAt) {
			paralyzed = true
			break
		}
	}
	w.mu.Unlock()
	if !paralyzed {
		t.Fatal("no monster paralysis after delayed control delivery")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, mon := range w.monsters {
		if mon.ParalyzedUntil.After(controlAt) && mon.LastHitterID != caster.ID {
			t.Fatalf("monster last hitter = %q, want %q", mon.LastHitterID, caster.ID)
		}
	}
}

func TestUseBundleItemUnpacksSixItems(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	ch.BagItems = []storage.UserItem{{ItemID: "回城卷包", MakeIndex: 300}}
	updated, _, err := w.UseItemByBagIndex(ch, 300)
	if err != nil {
		t.Fatalf("UseItemByBagIndex() error = %v", err)
	}
	if countBagItems(updated.BagItems) != 6 {
		t.Fatalf("bag len = %d, want 6 separate items", countBagItems(updated.BagItems))
	}
	seen := map[int32]struct{}{}
	for _, entry := range updated.BagItems {
		if entry.ItemID == "" {
			continue
		}
		if entry.ItemID != "回城卷" {
			t.Fatalf("entry = %+v, want 回城卷 x1", entry)
		}
		if entry.MakeIndex == 0 {
			t.Fatalf("entry = %+v, want non-zero MakeIndex", entry)
		}
		if _, ok := seen[entry.MakeIndex]; ok {
			t.Fatalf("duplicate MakeIndex = %d in %+v", entry.MakeIndex, updated.BagItems)
		}
		seen[entry.MakeIndex] = struct{}{}
	}
}

func TestNormalizeCharacterBagItemsRemovesInvalidAndDuplicates(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	ch.BagItems = []storage.UserItem{
		{ItemID: testHPItemID, MakeIndex: 10},
		{ItemID: "不存在的物品", MakeIndex: 11},
		{ItemID: testHPItemID, MakeIndex: 10},
	}

	normalized := ch
	changed := w.normalizeBagItemMakeIndexesLocked(&normalized)
	if !changed {
		t.Fatal("normalizeBagItemMakeIndexesLocked() changed = false, want true")
	}
	if countBagItems(normalized.BagItems) != 2 {
		t.Fatalf("normalized bag len = %d, want 2", countBagItems(normalized.BagItems))
	}
	if normalized.BagItems[0].ItemID != testHPItemID || normalized.BagItems[0].MakeIndex != 10 {
		t.Fatalf("entry = %+v, want first potion with MakeIndex 10", normalized.BagItems[0])
	}
	if normalized.BagItems[1].ItemID != testHPItemID || normalized.BagItems[1].MakeIndex == 10 {
		t.Fatalf("entry = %+v, want duplicate potion kept at another slot with new identity", normalized.BagItems[1])
	}
}

func TestNormalizeCharacterStateSyncsEquipmentIdentity(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	setEquippedItem(&ch, SlotWeapon, storage.UserItem{ItemID: testWeaponID})
	updated, changed := w.NormalizeCharacterState(ch)
	if !changed {
		t.Fatal("NormalizeCharacterState() changed = false, want true")
	}
	if updated.EquippedItems[SlotWeapon].ItemID != testWeaponID {
		t.Fatalf("EquippedItems[SlotWeapon].ItemID = %q, want %s", updated.EquippedItems[SlotWeapon].ItemID, testWeaponID)
	}
	if updated.EquippedItems[SlotWeapon].MakeIndex == 0 {
		t.Fatal("EquippedItems[SlotWeapon].MakeIndex = 0, want non-zero identity")
	}
}

func TestNormalizeCharacterStateDropsStaleQuickSlotBinding(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	ch.BagItems = []storage.UserItem{{ItemID: testHPItemID, MakeIndex: 10}}
	setEquippedItem(&ch, SlotNecklace, storage.UserItem{ItemID: testHPItemID, MakeIndex: 11, Dura: 7})

	updated, changed := w.NormalizeCharacterState(ch)
	if !changed {
		t.Fatal("NormalizeCharacterState() changed = false, want true")
	}
	if updated.EquippedItems[SlotNecklace].ItemID != "" {
		t.Fatalf("EquippedItems[SlotNecklace].ItemID = %q, want cleared", updated.EquippedItems[SlotNecklace].ItemID)
	}
	if updated.EquippedItems[SlotNecklace].MakeIndex != 0 {
		t.Fatalf("EquippedItems[SlotNecklace].MakeIndex = %d, want 0", updated.EquippedItems[SlotNecklace].MakeIndex)
	}
	if updated.EquippedItems[SlotNecklace].Dura != 0 {
		t.Fatalf("EquippedItems[SlotNecklace].Dura = %d, want 0", updated.EquippedItems[SlotNecklace].Dura)
	}
}

func TestUseTeleportRandomScrollMovesWithinSameMap(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	ch.BagItems = []storage.UserItem{{ItemID: "随机传送卷", MakeIndex: 301}}
	updated, _, err := w.UseItemByBagIndex(ch, 301)
	if err != nil {
		t.Fatalf("UseItemByBagIndex() error = %v", err)
	}
	if updated.MapID != ch.MapID {
		t.Fatalf("MapID = %q, want %q", updated.MapID, ch.MapID)
	}
	if updated.X == ch.X && updated.Y == ch.Y {
		t.Fatalf("teleport did not move character")
	}
}

func TestUseDungeonEscapeScrollUsesHomeMapRandomPosition(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	ch.BagItems = []storage.UserItem{{ItemID: "地牢逃脱卷", MakeIndex: 302}}
	updated, _, err := w.UseItemByBagIndex(ch, 302)
	if err != nil {
		t.Fatalf("UseItemByBagIndex() error = %v", err)
	}
	if updated.MapID != ch.HomeMap {
		t.Fatalf("MapID = %q, want home map %q", updated.MapID, ch.HomeMap)
	}
	if updated.X == ch.X && updated.Y == ch.Y {
		t.Fatalf("teleport did not move character")
	}
}

func TestUseBlessingOilUpdatesEquippedWeaponLuck(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	ch.BagItems = []storage.UserItem{
		{ItemID: testWeaponID, MakeIndex: 10},
		{ItemID: "祝福油", MakeIndex: 11},
	}
	equipped, err := w.EquipItemByBagIndex(ch, SlotWeapon, 10, testWeaponID)
	if err != nil {
		t.Fatalf("EquipItemByBagIndex() error = %v", err)
	}
	updated, _, err := w.UseItemByBagIndex(equipped, 11)
	if err != nil {
		t.Fatalf("UseItemByBagIndex() error = %v", err)
	}
	if got := updated.EquippedItems[SlotWeapon].Desc[3]; got != 1 {
		t.Fatalf("weapon luck byte[3] = %d, want 1", got)
	}
	if got := updated.EquippedItems[SlotWeapon].Desc[4]; got != 0 {
		t.Fatalf("weapon curse byte[4] = %d, want 0", got)
	}
	for _, entry := range updated.BagItems {
		if entry.ItemID == "祝福油" {
			t.Fatalf("祝福油 should be consumed, bag = %+v", updated.BagItems)
		}
	}
}

func TestUseRepairOilImprovesWeaponDurability(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	ch.BagItems = []storage.UserItem{
		{ItemID: testWeaponID, MakeIndex: 20},
		{ItemID: "修复油", MakeIndex: 21},
	}
	equipped, err := w.EquipItemByBagIndex(ch, SlotWeapon, 20, testWeaponID)
	if err != nil {
		t.Fatalf("EquipItemByBagIndex() error = %v", err)
	}
	setEquippedItem(&equipped, SlotWeapon, storage.UserItem{ItemID: testWeaponID, MakeIndex: 20, Dura: 200})
	updated, _, err := w.UseItemByBagIndex(equipped, 21)
	if err != nil {
		t.Fatalf("UseItemByBagIndex() error = %v", err)
	}
	if updated.EquippedItems[SlotWeapon].Dura <= 200 {
		t.Fatalf("weapon durability = %d, want improved durability", updated.EquippedItems[SlotWeapon].Dura)
	}
}

func TestUseSuperRepairOilRestoresWeaponDurability(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	ch.BagItems = []storage.UserItem{
		{ItemID: testWeaponID, MakeIndex: 30},
		{ItemID: "战神油", MakeIndex: 31},
	}
	equipped, err := w.EquipItemByBagIndex(ch, SlotWeapon, 30, testWeaponID)
	if err != nil {
		t.Fatalf("EquipItemByBagIndex() error = %v", err)
	}
	setEquippedItem(&equipped, SlotWeapon, storage.UserItem{ItemID: testWeaponID, MakeIndex: 30, Dura: 200})
	updated, _, err := w.UseItemByBagIndex(equipped, 31)
	if err != nil {
		t.Fatalf("UseItemByBagIndex() error = %v", err)
	}
	if updated.EquippedItems[SlotWeapon].Dura != itemDuraMax(w.data.Items[testWeaponID]) {
		t.Fatalf("weapon durability = %d, want full durability", updated.EquippedItems[SlotWeapon].Dura)
	}
}

func TestEquipRejectsWrongSexForDress(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	ch.Sex = 1
	ch.BagItems = []storage.UserItem{{ItemID: testArmorID}}
	if _, err := w.EquipItem(ch, SlotArmor, testArmorID); err == nil {
		t.Fatalf("EquipItem() expected error for wrong-sex dress")
	}
}

func TestEquipRejectsNeedLevel(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	w.data.Items["need-level-item"] = data.StdItem{
		ID:        "need-level-item",
		Name:      "need-level-item",
		StdMode:   5,
		Weight:    1,
		Need:      0,
		NeedLevel: 99,
	}
	ch.BagItems = []storage.UserItem{{ItemID: "need-level-item"}}
	if _, err := w.EquipItem(ch, SlotWeapon, "need-level-item"); err == nil {
		t.Fatalf("EquipItem() expected error for unmet level requirement")
	}
}

func TestPickupGoldAddsToBalance(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	ch.BagItems = []storage.UserItem{}
	w.drops["drop-1"] = GroundDrop{ID: "drop-1", MapID: ch.MapID, X: ch.X, Y: ch.Y, ItemID: "金币", Count: 123}
	updated, drop, err := w.PickupAt(ch, ch.X, ch.Y)
	if err != nil {
		t.Fatalf("PickupAt() error = %v", err)
	}
	if drop.ItemID != "金币" || drop.Count != 123 {
		t.Fatalf("pickup drop = %+v, want 金币 x123", drop)
	}
	if updated.Gold != 123 {
		t.Fatalf("Gold = %d, want 123", updated.Gold)
	}
	if countBagItems(updated.BagItems) != 0 {
		t.Fatalf("bag = %+v, want empty after gold pickup", updated.BagItems)
	}
}

func TestPickupAtWithResultDistinguishesGoldAndItem(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	ch.BagItems = []storage.UserItem{}
	w.drops["drop-gold"] = GroundDrop{ID: "drop-gold", MapID: ch.MapID, X: ch.X, Y: ch.Y, ItemID: "金币", Count: 7}
	_, goldResult, err := w.PickupAtWithResult(ch, ch.X, ch.Y)
	if err != nil {
		t.Fatalf("PickupAtWithResult() gold error = %v", err)
	}
	if !goldResult.GoldChanged || goldResult.Gold != 7 {
		t.Fatalf("gold result = %+v, want gold change to 7", goldResult)
	}

	w.drops["drop-item"] = GroundDrop{ID: "drop-item", MapID: ch.MapID, X: ch.X, Y: ch.Y, ItemID: testHPItemID, Count: 1}
	_, itemResult, err := w.PickupAtWithResult(ch, ch.X, ch.Y)
	if err != nil {
		t.Fatalf("PickupAtWithResult() item error = %v", err)
	}
	if itemResult.GoldChanged {
		t.Fatalf("item result unexpectedly marked gold changed: %+v", itemResult)
	}
	if len(itemResult.AddedItems) != 1 || itemResult.AddedItems[0].ItemID != testHPItemID {
		t.Fatalf("item result = %+v, want one %s added", itemResult, testHPItemID)
	}
}

func TestSetGroupModeWithResultReturnsResponseParam(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	updated, result, err := w.SetGroupModeWithResult(ch, true)
	if err != nil {
		t.Fatalf("SetGroupModeWithResult() error = %v", err)
	}
	if result.ResponseParam != 1 {
		t.Fatalf("response param = %d, want 1", result.ResponseParam)
	}
	if !updated.AllowGroup {
		t.Fatalf("updated.AllowGroup = false, want true")
	}
	if len(result.Sync.Updated) != 1 || result.Sync.Updated[0].ID != ch.ID {
		t.Fatalf("sync updated = %+v, want current character", result.Sync.Updated)
	}
}

func TestSetSkillHotkeyPersistsChange(t *testing.T) {
	bundle := loadTestBundle(t)
	storePath := filepath.Join(t.TempDir(), "state.json")
	store, err := storage.Open(storePath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	w := New(bundle, store)
	mapID, x, y := defaultSpawn(bundle)
	ch, err := w.CreateCharacterWithAppearance("test", "tester", "taoist", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacterWithAppearance() error = %v", err)
	}
	ch.Skills = storage.SkillStates{{ID: "精神力战法"}}
	if err := w.store.SaveCharacter(ch); err != nil {
		t.Fatalf("SaveCharacter() error = %v", err)
	}
	updated, changed, err := w.SetSkillHotkey(ch, "精神力战法", '3')
	if err != nil {
		t.Fatalf("SetSkillHotkey() error = %v", err)
	}
	if !changed {
		t.Fatalf("SetSkillHotkey() changed = false, want true")
	}
	if updated.Skills[0].Hotkey != '3' {
		t.Fatalf("updated hotkey = %d, want '3'", updated.Skills[0].Hotkey)
	}
	reopened, err := storage.Open(storePath)
	if err != nil {
		t.Fatalf("reopen Open() error = %v", err)
	}
	saved, ok := reopened.Character(ch.ID)
	if !ok {
		t.Fatalf("saved character not found")
	}
	if saved.Skills[0].Hotkey != '3' {
		t.Fatalf("saved hotkey = %d, want '3'", saved.Skills[0].Hotkey)
	}
}

func TestTickClearsExpiredStealthAndQueuesStatusRefresh(t *testing.T) {
	bundle := loadTestBundle(t)
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	w := New(bundle, store)
	mapID, x, y := defaultSpawn(bundle)
	ch, err := w.CreateCharacterWithAppearance("test", "tester", "taoist", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacterWithAppearance() error = %v", err)
	}
	ch.TransparentUntil = time.Now().Add(-time.Second).UnixNano()
	if err := w.store.SaveCharacter(ch); err != nil {
		t.Fatalf("SaveCharacter() error = %v", err)
	}
	result, err := w.Tick([]PlayerSnapshot{{Character: ch}}, time.Now())
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if len(result.StatusRefreshCharacters) != 1 {
		t.Fatalf("StatusRefreshCharacters = %+v, want 1", result.StatusRefreshCharacters)
	}
	if result.StatusRefreshCharacters[0].ID != ch.ID {
		t.Fatalf("StatusRefreshCharacters[0].ID = %q, want %q", result.StatusRefreshCharacters[0].ID, ch.ID)
	}
	if len(result.Characters) != 1 {
		t.Fatalf("Characters = %+v, want 1", result.Characters)
	}
	if result.Characters[0].TransparentUntil != 0 {
		t.Fatalf("TransparentUntil = %d, want 0", result.Characters[0].TransparentUntil)
	}
}

func TestTickClearsExpiredSpellStatesForDeadCharacter(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	now := time.Now()
	ch.HP = 0
	ch.TransparentUntil = now.Add(-time.Second).UnixNano()
	ch.DefenceUpUntil = now.Add(-time.Second).UnixNano()
	ch.PoisonHealthLevel = 1
	ch.PoisonHealthUntil = now.Add(-time.Second).UnixNano()
	ch.ParalyzedUntil = now.Add(-time.Second).UnixNano()
	result, err := w.Tick([]PlayerSnapshot{{Character: ch}}, now)
	if err != nil {
		t.Fatalf("World.Tick() error = %v", err)
	}
	if len(result.StatusRefreshCharacters) != 1 {
		t.Fatalf("StatusRefreshCharacters = %+v, want 1", result.StatusRefreshCharacters)
	}
	updated := result.StatusRefreshCharacters[0]
	if updated.TransparentUntil != 0 || updated.DefenceUpUntil != 0 || updated.PoisonHealthUntil != 0 || updated.PoisonHealthLevel != 0 || updated.ParalyzedUntil != 0 {
		t.Fatalf("expired states = %+v, want cleared for dead character", updated)
	}
}

func TestMonsterStatusIncludesDefenceStates(t *testing.T) {
	now := time.Now()
	mon := Monster{
		DefenceUpUntil:    now.Add(time.Minute).UnixNano(),
		MagDefenceUpUntil: now.Add(time.Minute).UnixNano(),
		ShowHPUntil:       now.Add(time.Minute).UnixNano(),
	}
	status := MonsterStatus(mon, now)
	if status&0x00400000 == 0 || status&0x00200000 == 0 || status&0x20000000 == 0 {
		t.Fatalf("MonsterStatus() = %#x, want defence and magic-defence bits", uint32(status))
	}
}

func TestMonsterStatusKeepsExpiredStateUntilTick(t *testing.T) {
	now := time.Now()
	mon := Monster{
		PoisonHealthUntil: now.Add(-time.Second),
		DefenceUpUntil:    now.Add(-time.Second).UnixNano(),
		TransparentUntil:  now.Add(-time.Second),
		ParalyzedUntil:    now.Add(-time.Second),
	}
	status := MonsterStatus(mon, now)
	bits := uint32(status)
	if bits&0x80000000 == 0 || bits&0x00400000 == 0 || bits&0x00800000 == 0 || bits&0x04000000 == 0 {
		t.Fatalf("MonsterStatus() = %#x, want uncleaned states preserved", uint32(status))
	}
}

func TestTickExpiresMonsterDefenceStates(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	spawned, err := w.SpawnMonsterByNameAt(caster.MapID, caster.X+1, caster.Y, "鸡", 1)
	if err != nil || len(spawned.Monsters) != 1 {
		t.Fatalf("SpawnMonsterByNameAt() = %+v, error = %v", spawned.Monsters, err)
	}
	mon := spawned.Monsters[0]
	now := time.Now()
	mon.DefenceUpUntil = now.Add(-time.Second).UnixNano()
	mon.MagDefenceUpUntil = now.Add(-time.Second).UnixNano()
	w.mu.Lock()
	w.monsters[mon.ID] = &mon
	w.mu.Unlock()
	result, err := w.Tick(nil, now)
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if len(result.StatusRefreshMonsters) != 1 || result.StatusRefreshMonsters[0].ID != mon.ID {
		t.Fatalf("StatusRefreshMonsters = %+v, want one refresh for %s", result.StatusRefreshMonsters, mon.ID)
	}
	if result.StatusRefreshMonsters[0].DefenceUpUntil != 0 || result.StatusRefreshMonsters[0].MagDefenceUpUntil != 0 {
		t.Fatalf("expired monster states = %+v, want cleared", result.StatusRefreshMonsters[0])
	}
}

func TestCastSkillParalysisUsesCasterLevelInSuccessChance(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	w.mu.Lock()
	w.monsters = map[string]*Monster{}
	w.occupied = map[monsterPosition]string{}
	w.mu.Unlock()
	spawned, err := w.SpawnMonsterByNameAt(caster.MapID, caster.X+2, caster.Y, "鸡", 2)
	if err != nil {
		t.Fatalf("SpawnMonsterByNameAt() error = %v", err)
	}
	caster.Level = 100
	caster.MP = 100
	caster.Skills = storage.SkillStates{{ID: "困魔咒", Level: 5, Train: 0}}
	caster.EquippedItems = map[int]storage.UserItem{SlotBujuk: {ItemID: "护身符", Dura: 10000}}
	mabe := w.data.Skills["困魔咒"]
	mabe.Power = 100
	mabe.MaxPower = 100
	mabe.DefPower = 0
	mabe.DefMaxPower = 0
	mabe.TrainLevel1 = 0
	w.data.Skills["困魔咒"] = mabe
	w.rand = rand.New(&seqSource{vals: []int64{0, 0, 60, 0}})
	result, err := w.CastSkillWithPlayers(caster, "困魔咒", spawned.Monsters[0].X, spawned.Monsters[0].Y, MonsterActorID(spawned.Monsters[0]), nil)
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	if len(w.pendingSpells) != 3 {
		t.Fatalf("pending spells = %d, want first damage, second damage, and control; result=%+v", len(w.pendingSpells), result)
	}
}

func TestCharacterParalysisImmunityComesFromEquipment(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	target, err := w.CreateCharacterWithAppearance("test2", "target", "warrior", 0, 0, caster.MapID, caster.X+1, caster.Y)
	if err != nil {
		t.Fatalf("CreateCharacter() target error = %v", err)
	}
	w.data.Items["paralysis-immunity"] = data.StdItem{ID: "paralysis-immunity", Shape: 139}
	target.EquippedItems = map[int]storage.UserItem{SlotArmRingL: {ItemID: "paralysis-immunity"}}
	w.mu.Lock()
	if !w.characterCannotParalyzeLocked(target) {
		w.mu.Unlock()
		t.Fatal("characterCannotParalyzeLocked() = false, want true")
	}
	w.mu.Unlock()
}

func TestTickParalysisStatusRefreshesOnlyAtExpiry(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	now := time.Now().Truncate(time.Millisecond)
	ch.ParalyzedUntil = now.Add(time.Second).UnixNano()

	active, err := w.Tick([]PlayerSnapshot{{Character: ch}}, now.Add(500*time.Millisecond))
	if err != nil {
		t.Fatalf("active World.Tick() error = %v", err)
	}
	if len(active.StatusRefreshCharacters) != 0 {
		t.Fatalf("active status refreshes = %+v, want none", active.StatusRefreshCharacters)
	}

	expired, err := w.Tick([]PlayerSnapshot{{Character: ch}}, now.Add(time.Second+time.Nanosecond))
	if err != nil {
		t.Fatalf("expiry World.Tick() error = %v", err)
	}
	if len(expired.StatusRefreshCharacters) != 1 || expired.StatusRefreshCharacters[0].ID != ch.ID {
		t.Fatalf("expiry status refreshes = %+v, want one for %q", expired.StatusRefreshCharacters, ch.ID)
	}
	if len(expired.Characters) != 1 || expired.Characters[0].ParalyzedUntil != 0 {
		t.Fatalf("expired character = %+v, want cleared paralysis", expired.Characters)
	}

	repeated, err := w.Tick([]PlayerSnapshot{{Character: expired.Characters[0]}}, now.Add(2*time.Second))
	if err != nil {
		t.Fatalf("repeated World.Tick() error = %v", err)
	}
	if len(repeated.StatusRefreshCharacters) != 0 {
		t.Fatalf("repeated status refreshes = %+v, want none", repeated.StatusRefreshCharacters)
	}
}

func TestTickMonsterParalysisRemainsActiveAtExactExpiry(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	now := time.Now().Truncate(time.Millisecond)
	spawned, err := w.SpawnMonsterByNameAt(caster.MapID, caster.X+1, caster.Y, "鸡", 1)
	if err != nil || len(spawned.Monsters) != 1 {
		t.Fatalf("SpawnMonsterByNameAt() = %+v, %v", spawned.Monsters, err)
	}
	monID := spawned.Monsters[0].ID
	w.mu.Lock()
	w.monsters[monID].ParalyzedUntil = now
	w.mu.Unlock()
	result, err := w.Tick([]PlayerSnapshot{{Character: caster}}, now)
	if err != nil {
		t.Fatalf("World.Tick() error = %v", err)
	}
	w.mu.Lock()
	paralyzedUntil := w.monsters[monID].ParalyzedUntil
	w.mu.Unlock()
	if !paralyzedUntil.Equal(now) {
		t.Fatalf("monster paralysis = %v, want exact boundary %v to remain active", paralyzedUntil, now)
	}
	for _, mon := range result.StatusRefreshMonsters {
		if mon.ID == monID {
			t.Fatalf("status refreshes = %+v, want no expiry at exact boundary", result.StatusRefreshMonsters)
		}
	}
}

func TestPendingParalysisDoesNotShortenOrRepeatStatusRefresh(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	now := time.Now().Truncate(time.Millisecond)
	ch.ParalyzedUntil = now.Add(time.Minute).UnixNano()
	w.mu.Lock()
	w.pendingSpells = []pendingSpell{{
		DueAt: now, CasterID: ch.ID, TargetCharacterID: ch.ID,
		ParalysisDuration: time.Second,
	}}
	w.mu.Unlock()

	result, err := w.Tick([]PlayerSnapshot{{Character: ch}}, now)
	if err != nil {
		t.Fatalf("World.Tick() error = %v", err)
	}
	if len(result.StatusRefreshCharacters) != 0 {
		t.Fatalf("status refreshes = %+v, want none for unchanged active status", result.StatusRefreshCharacters)
	}
	if len(result.Characters) != 1 || result.Characters[0].ParalyzedUntil != ch.ParalyzedUntil {
		t.Fatalf("paralysis expiry = %+v, want unchanged %d", result.Characters, ch.ParalyzedUntil)
	}
}

func TestTickClearsExpiredProtectionAndQueuesAbilityRefresh(t *testing.T) {
	bundle := loadTestBundle(t)
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	w := New(bundle, store)
	mapID, x, y := defaultSpawn(bundle)
	ch, err := w.CreateCharacterWithAppearance("test", "tester", "wizard", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacterWithAppearance() error = %v", err)
	}
	ch.DefenceUpUntil = time.Now().Add(-time.Second).UnixNano()
	ch.MagDefenceUpUntil = time.Now().Add(-time.Second).UnixNano()
	ch.BubbleDefenceUntil = time.Now().Add(-time.Second).UnixNano()
	ch.BubbleDefenceLevel = 3
	if err := w.store.SaveCharacter(ch); err != nil {
		t.Fatalf("SaveCharacter() error = %v", err)
	}
	result, err := w.Tick([]PlayerSnapshot{{Character: ch}}, time.Now())
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if len(result.AbilityRefreshCharacters) != 1 {
		t.Fatalf("AbilityRefreshCharacters = %+v, want 1", result.AbilityRefreshCharacters)
	}
	if result.AbilityRefreshCharacters[0].ID != ch.ID {
		t.Fatalf("AbilityRefreshCharacters[0].ID = %q, want %q", result.AbilityRefreshCharacters[0].ID, ch.ID)
	}
	if len(result.Characters) != 1 {
		t.Fatalf("Characters = %+v, want 1", result.Characters)
	}
	got := result.Characters[0]
	if got.DefenceUpUntil != 0 || got.MagDefenceUpUntil != 0 || got.BubbleDefenceUntil != 0 || got.BubbleDefenceLevel != 0 {
		t.Fatalf("expired protection not cleared: %+v", got)
	}
}

func TestTickClearsExpiredBubbleWithoutAbilityRefresh(t *testing.T) {
	bundle := loadTestBundle(t)
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	w := New(bundle, store)
	mapID, x, y := defaultSpawn(bundle)
	ch, err := w.CreateCharacterWithAppearance("test", "tester", "wizard", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacterWithAppearance() error = %v", err)
	}
	ch.BubbleDefenceUntil = time.Now().Add(-time.Second).UnixNano()
	ch.BubbleDefenceLevel = 3
	result, err := w.Tick([]PlayerSnapshot{{Character: ch}}, time.Now())
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if len(result.AbilityRefreshCharacters) != 0 {
		t.Fatalf("AbilityRefreshCharacters = %+v, want none", result.AbilityRefreshCharacters)
	}
	if len(result.StatusRefreshCharacters) != 1 || result.StatusRefreshCharacters[0].ID != ch.ID {
		t.Fatalf("StatusRefreshCharacters = %+v, want %q", result.StatusRefreshCharacters, ch.ID)
	}
	if len(result.Characters) != 1 || result.Characters[0].BubbleDefenceUntil != 0 || result.Characters[0].BubbleDefenceLevel != 0 {
		t.Fatalf("expired bubble = %+v, want cleared", result.Characters)
	}
}

func TestTickOpensAndClosesShowHPAtScheduledTimes(t *testing.T) {
	bundle := loadTestBundle(t)
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	w := New(bundle, store)
	mapID, x, y := defaultSpawn(bundle)
	ch, err := w.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacterWithAppearance() error = %v", err)
	}
	now := time.Now()
	ch.ShowHPOpenAt = now.Add(-time.Second).UnixNano()
	ch.ShowHPUntil = now.Add(time.Second).UnixNano()
	if err := w.store.SaveCharacter(ch); err != nil {
		t.Fatalf("SaveCharacter() error = %v", err)
	}
	result, err := w.Tick([]PlayerSnapshot{{Character: ch}}, now)
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if len(result.ShowHPOpenedCharacters) != 1 {
		t.Fatalf("ShowHPOpenedCharacters = %+v, want 1", result.ShowHPOpenedCharacters)
	}
	if result.ShowHPOpenedCharacters[0].ID != ch.ID {
		t.Fatalf("ShowHPOpenedCharacters[0].ID = %q, want %q", result.ShowHPOpenedCharacters[0].ID, ch.ID)
	}
	if result.ShowHPOpenedCharacters[0].ShowHPOpenAt != 0 {
		t.Fatalf("opened ShowHPOpenAt = %d, want 0", result.ShowHPOpenedCharacters[0].ShowHPOpenAt)
	}
	result, err = w.Tick([]PlayerSnapshot{{Character: result.ShowHPOpenedCharacters[0]}}, now.Add(2*time.Second))
	if err != nil {
		t.Fatalf("second Tick() error = %v", err)
	}
	if len(result.ShowHPExpiredCharacters) != 1 {
		t.Fatalf("ShowHPExpiredCharacters = %+v, want 1", result.ShowHPExpiredCharacters)
	}
	if result.ShowHPExpiredCharacters[0].ID != ch.ID {
		t.Fatalf("ShowHPExpiredCharacters[0].ID = %q, want %q", result.ShowHPExpiredCharacters[0].ID, ch.ID)
	}
	if result.ShowHPExpiredCharacters[0].ShowHPUntil != 0 {
		t.Fatalf("expired ShowHPUntil = %d, want 0", result.ShowHPExpiredCharacters[0].ShowHPUntil)
	}
}

func TestTickOpensShowHPForDeadDelayedTarget(t *testing.T) {
	bundle := loadTestBundle(t)
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	w := New(bundle, store)
	mapID, x, y := defaultSpawn(bundle)
	target, err := w.CreateCharacterWithAppearance("test", "target", "warrior", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacterWithAppearance() error = %v", err)
	}
	now := time.Now()
	target.HP = 0
	target.ShowHPOpenAt = now.Add(-time.Millisecond).UnixNano()
	target.ShowHPDuration = time.Minute.Nanoseconds()
	result, err := w.Tick([]PlayerSnapshot{{Character: target}}, now)
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if len(result.ShowHPOpenedCharacters) != 1 || result.ShowHPOpenedCharacters[0].ID != target.ID {
		t.Fatalf("ShowHPOpenedCharacters = %+v, want dead target", result.ShowHPOpenedCharacters)
	}
}

func TestTickClearsExpiredTemporaryAbilitiesAndQueuesAbilityRefresh(t *testing.T) {
	bundle := loadTestBundle(t)
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	w := New(bundle, store)
	mapID, x, y := defaultSpawn(bundle)
	ch, err := w.CreateCharacterWithAppearance("test", "tester", "wizard", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacterWithAppearance() error = %v", err)
	}
	ch.ExtraAbil[0] = 3
	ch.ExtraAbil[4] = 7
	ch.ExtraAbilTimes[0] = time.Now().Add(-time.Second).UnixNano()
	ch.ExtraAbilTimes[4] = time.Now().Add(-time.Second).UnixNano()
	if err := w.store.SaveCharacter(ch); err != nil {
		t.Fatalf("SaveCharacter() error = %v", err)
	}
	result, err := w.Tick([]PlayerSnapshot{{Character: ch}}, time.Now())
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if len(result.AbilityRefreshCharacters) != 1 {
		t.Fatalf("AbilityRefreshCharacters = %+v, want 1", result.AbilityRefreshCharacters)
	}
	if len(result.Characters) != 1 {
		t.Fatalf("Characters = %+v, want 1", result.Characters)
	}
	got := result.Characters[0]
	if got.ExtraAbil[0] != 0 || got.ExtraAbil[4] != 0 || got.ExtraAbilTimes[0] != 0 || got.ExtraAbilTimes[4] != 0 {
		t.Fatalf("expired temporary abilities not cleared: %+v", got)
	}
}

func TestAbilitiesMatchesBaseWithNothingEquipped(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	base := Base(ch.Class, ch.Level)
	got := w.Abilities(ch)
	if got.MaxHP != base.MaxHP || got.MaxMP != base.MaxMP {
		t.Fatalf("Abilities() MaxHP/MaxMP = %d/%d, want %d/%d", got.MaxHP, got.MaxMP, base.MaxHP, base.MaxMP)
	}
	if got.DC != PackWord(base.DC, base.DCMax, 0, 0) {
		t.Fatalf("Abilities().DC = %d, want base-only packed value", got.DC)
	}
}

func TestAbilitiesAddsEquippedItemBonusOnTopOfBase(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	equipped, err := w.EquipItem(ch, SlotWeapon, testWeaponID)
	if err != nil {
		t.Fatalf("EquipItem() error = %v", err)
	}
	base := Base(equipped.Class, equipped.Level)
	got := w.Abilities(equipped)
	want := PackWord(base.DC, base.DCMax, 2, 5)
	if got.DC != want {
		t.Fatalf("Abilities().DC = %d, want %d (base DC plus %s's 2~5)", got.DC, want, testWeaponID)
	}
}

func TestAbilitiesSumsCarriedItemWeightIntoBagWeight(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	got := w.Abilities(ch)
	if got.Weight != 7 {
		t.Fatalf("Abilities().Weight = %d, want 7 (the starting %s's weight)", got.Weight, testWeaponID)
	}
}

func TestLevelUpRecomputesMaxHPFromLevelAndHealsToFull(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	ch.Experience = w.RequiredExperience(ch.Level) - 1
	monsters, _ := w.SnapshotAround(ch.MapID, 0, 0, 99999)
	if len(monsters) == 0 {
		t.Fatalf("expected monsters")
	}
	ch.X, ch.Y = monsters[0].X-1, monsters[0].Y
	var result AttackResult
	var err error
	for i := 0; i < 10; i++ {
		result, err = w.Hit(ch, ch.X, ch.Y, 2)
		if err != nil {
			t.Fatalf("Hit() error = %v", err)
		}
		ch = result.Character
		if result.LevelUp {
			break
		}
	}
	if !result.LevelUp {
		tick, err := w.Tick([]PlayerSnapshot{{Character: ch}}, time.Now())
		if err != nil {
			t.Fatalf("Tick() error = %v", err)
		}
		if len(tick.SpellExperience) > 0 {
			result.Character = tick.SpellExperience[0].Character
			result.LevelUp = tick.SpellExperience[0].LevelUp
		}
	}
	if !result.LevelUp {
		t.Fatalf("expected LevelUp = true")
	}
	want := Base(result.Character.Class, result.Character.Level)
	if result.Character.MaxHP != want.MaxHP || result.Character.HP != want.MaxHP {
		t.Fatalf("after level-up HP/MaxHP = %d/%d, want both = %d (fully healed at the new level's MaxHP)",
			result.Character.HP, result.Character.MaxHP, want.MaxHP)
	}
	if result.Character.Experience >= w.RequiredExperience(result.Character.Level) {
		t.Fatalf("after level-up Experience = %d, want remainder below next threshold %d", result.Character.Experience, w.RequiredExperience(result.Character.Level))
	}
}

func TestGameplaySettingsControlRequiredExperience(t *testing.T) {
	bundle := loadTestBundle(t)
	addSpawnNearDefault(t, &bundle, testMonsterID, 1, 0)
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	settings := config.DefaultGameplay()
	settings.Progression.RequiredExperiencePerLevel = 5
	w := New(bundle, store, settings)
	mapID, x, y := defaultSpawn(bundle)
	ch, err := w.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch.BonusAbil.Hit = 100
	monsters, _ := w.SnapshotAround(ch.MapID, 0, 0, 99999)
	if len(monsters) == 0 {
		t.Fatalf("expected monsters")
	}
	ch.X, ch.Y = monsters[0].X-1, monsters[0].Y
	var result AttackResult
	for i := 0; i < 2; i++ {
		result, err = w.Hit(ch, ch.X, ch.Y, 2)
		if err != nil {
			t.Fatalf("Hit() error = %v", err)
		}
		ch = result.Character
	}
	if !result.LevelUp {
		tick, err := w.Tick([]PlayerSnapshot{{Character: ch}}, time.Now())
		if err != nil {
			t.Fatalf("Tick() error = %v", err)
		}
		if len(tick.SpellExperience) > 0 {
			result.LevelUp = tick.SpellExperience[0].LevelUp
		}
	}
	if !result.LevelUp {
		t.Fatalf("expected LevelUp = true with configured required experience")
	}
}

func TestHandleUserCommandMakeAddsItems(t *testing.T) {
	w, ch := newRealDataWorldCharacter(t)
	result, ok := w.HandleUserCommand(ch, "@Make 回城卷 2")
	if !ok {
		t.Fatal("HandleUserCommand() returned ok=false")
	}
	if got := result.Message; got != "made 2 回城卷" {
		t.Fatalf("message = %q, want %q", got, "made 2 回城卷")
	}
	if len(result.AddedItems) != 2 {
		t.Fatalf("AddedItems len = %d, want 2", len(result.AddedItems))
	}
	if len(result.Character.BagItems) != len(ch.BagItems)+2 {
		t.Fatalf("BagItems len = %d, want %d", len(result.Character.BagItems), len(ch.BagItems)+2)
	}
	seen := map[int32]struct{}{}
	for i, entry := range result.AddedItems {
		if entry.ItemID != "回城卷" {
			t.Fatalf("added item %d id = %q, want 回城卷", i, entry.ItemID)
		}
		if entry.MakeIndex == 0 {
			t.Fatalf("added item %d makeindex = 0", i)
		}
		if _, ok := seen[entry.MakeIndex]; ok {
			t.Fatalf("duplicate makeindex = %d", entry.MakeIndex)
		}
		seen[entry.MakeIndex] = struct{}{}
		if entry.Dura != entry.DuraMax {
			t.Fatalf("added item %d dura = %d, duraMax = %d, want equal", i, entry.Dura, entry.DuraMax)
		}
	}
}

func TestHandleUserCommandMoveRandomCurrentMap(t *testing.T) {
	w, ch := newRealDataWorldCharacter(t)
	result, ok := w.HandleUserCommand(ch, "@Move")
	if !ok {
		t.Fatal("HandleUserCommand() returned ok=false")
	}
	if result.Teleport == nil {
		t.Fatal("HandleUserCommand() did not provide teleport event")
	}
	if result.Character.MapID != ch.MapID {
		t.Fatalf("map = %q, want %q", result.Character.MapID, ch.MapID)
	}
}

func TestHandleUserCommandMoveCurrentMapCoords(t *testing.T) {
	w, ch := newRealDataWorldCharacter(t)
	targetX, targetY := firstWalkableAround(t, w, ch.MapID, ch.X+1, ch.Y, 4)
	result, ok := w.HandleUserCommand(ch, fmt.Sprintf("@Move %d %d", targetX, targetY))
	if !ok {
		t.Fatal("HandleUserCommand() returned ok=false")
	}
	if result.Teleport == nil {
		t.Fatal("HandleUserCommand() did not provide teleport event")
	}
	if result.Character.MapID != ch.MapID || result.Character.X != targetX || result.Character.Y != targetY {
		t.Fatalf("move = %s (%d,%d), want %s (%d,%d)", result.Character.MapID, result.Character.X, result.Character.Y, ch.MapID, targetX, targetY)
	}
}

func TestHandleUserCommandMoveTargetMapRandom(t *testing.T) {
	w, ch := newRealDataWorldCharacter(t)
	targetMap := otherMapID(t, w, ch.MapID)
	result, ok := w.HandleUserCommand(ch, "@Move "+targetMap)
	if !ok {
		t.Fatal("HandleUserCommand() returned ok=false")
	}
	if result.Teleport == nil {
		t.Fatal("HandleUserCommand() did not provide teleport event")
	}
	if result.Character.MapID != targetMap {
		t.Fatalf("map = %q, want %q", result.Character.MapID, targetMap)
	}
}

func TestHandleUserCommandMoveTargetMapCoords(t *testing.T) {
	w, ch := newRealDataWorldCharacter(t)
	targetMap := otherMapID(t, w, ch.MapID)
	targetX, targetY := startCoordsForMap(t, w.data, targetMap)
	targetX, targetY = firstWalkableAround(t, w, targetMap, targetX, targetY, 4)
	result, ok := w.HandleUserCommand(ch, fmt.Sprintf("@Move %s %d %d", targetMap, targetX, targetY))
	if !ok {
		t.Fatal("HandleUserCommand() returned ok=false")
	}
	if result.Teleport == nil {
		t.Fatal("HandleUserCommand() did not provide teleport event")
	}
	if result.Character.MapID != targetMap || result.Character.X != targetX || result.Character.Y != targetY {
		t.Fatalf("move = %s (%d,%d), want %s (%d,%d)", result.Character.MapID, result.Character.X, result.Character.Y, targetMap, targetX, targetY)
	}
}

func TestHandleChatFormatsLocalMessage(t *testing.T) {
	w, ch := newRealDataWorldCharacter(t)
	result, ok := w.HandleChat(ch, " hello world ")
	if !ok {
		t.Fatal("HandleChat() returned ok=false")
	}
	if result.Global {
		t.Fatal("HandleChat() marked local message as global")
	}
	if got, want := result.Message, ch.Name+":hello world"; got != want {
		t.Fatalf("message = %q, want %q", got, want)
	}
}

func TestHandleChatFormatsGlobalMessage(t *testing.T) {
	w, ch := newRealDataWorldCharacter(t)
	result, ok := w.HandleChat(ch, "! hello world ")
	if !ok {
		t.Fatal("HandleChat() returned ok=false")
	}
	if !result.Global {
		t.Fatal("HandleChat() did not mark global message")
	}
	if got, want := result.Message, "(!)"+ch.Name+":  hello world"; got != want {
		t.Fatalf("message = %q, want %q", got, want)
	}
}

func TestHandleSayRoutesCommandAndChat(t *testing.T) {
	w, ch := newRealDataWorldCharacter(t)
	commandResult, ok := w.HandleSay(ch, "@Move")
	if !ok {
		t.Fatal("HandleSay() returned ok=false for command")
	}
	if commandResult.Command == nil || commandResult.Chat != nil {
		t.Fatalf("HandleSay() command routing = %+v", commandResult)
	}
	chatResult, ok := w.HandleSay(ch, "hello")
	if !ok {
		t.Fatal("HandleSay() returned ok=false for chat")
	}
	if chatResult.Chat == nil || chatResult.Command != nil {
		t.Fatalf("HandleSay() chat routing = %+v", chatResult)
	}
}

func TestTeleportRandomInMapPersistsCharacterPosition(t *testing.T) {
	bundle := loadTestBundle(t)
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	w := New(bundle, store)
	mapID, x, y := defaultSpawn(bundle)
	ch, err := w.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	targetMap := otherMapID(t, w, ch.MapID)
	updated, err := w.TeleportRandomInMap(ch, targetMap)
	if err != nil {
		t.Fatalf("TeleportRandomInMap() error = %v", err)
	}
	saved, ok := store.Character(updated.ID)
	if !ok {
		t.Fatalf("store.Character(%q) missing", updated.ID)
	}
	if saved.MapID != targetMap || saved.X != updated.X || saved.Y != updated.Y {
		t.Fatalf("saved character = %s (%d,%d), want %s (%d,%d)", saved.MapID, saved.X, saved.Y, targetMap, updated.X, updated.Y)
	}
}

func TestInstantTeleportKeepsMagicFireWhenMoveFails(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	ch.Skills = storage.SkillStates{{ID: "瞬息移动", Level: 0, Train: 0}}
	skill, ok := w.Skill("瞬息移动")
	if !ok {
		t.Fatal("skill 瞬息移动 missing from config")
	}
	ch.MP = w.SpellCost(skill, ch.Skills[0]) + 1
	w.mu.Lock()
	w.rand = rand.New(&seqSource{vals: []int64{10 << 32}})
	w.mu.Unlock()
	result, err := w.DoSpell(ch, "瞬息移动", ch.X, ch.Y, 0, nil)
	if err != nil {
		t.Fatalf("DoSpell() error = %v", err)
	}
	magicFire, spaceMoveFire := 0, 0
	for _, event := range result.Events {
		switch event.Kind {
		case SpellEventMagicFire:
			magicFire++
		case SpellEventSpaceMoveFire:
			spaceMoveFire++
		}
	}
	if magicFire != 1 || spaceMoveFire != 0 {
		t.Fatalf("teleport failure events = magic fire:%d space move fire:%d, want 1 and 0", magicFire, spaceMoveFire)
	}
	if result.Character.X != ch.X || result.Character.Y != ch.Y {
		t.Fatalf("failed teleport position = (%d,%d), want unchanged (%d,%d)", result.Character.X, result.Character.Y, ch.X, ch.Y)
	}
}

func TestCastSkillInstantTeleportMovesCasterWithinHomeMap(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	ch.Skills = storage.SkillStates{{ID: "瞬息移动", Level: 5, Train: 0}}
	skill, ok := w.Skill("瞬息移动")
	if !ok {
		t.Fatalf("skill 瞬息移动 missing from config")
	}
	cost := w.SpellCost(skill, ch.Skills[0])
	ch.MP = cost + 10
	updated, err := w.CastSkillWithPlayers(ch, "瞬息移动", ch.X, ch.Y, 0, nil)
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	if updated.Character.MapID != ch.MapID {
		t.Fatalf("MapID = %q, want %q", updated.Character.MapID, ch.MapID)
	}
	if updated.Character.X == ch.X && updated.Character.Y == ch.Y {
		t.Fatalf("teleport did not move character")
	}
	if updated.Character.MP != ch.MP-cost {
		t.Fatalf("MP = %d, want %d after teleport", updated.Character.MP, ch.MP-cost)
	}
}
