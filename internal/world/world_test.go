package world

import (
	"fmt"
	"math"
	"math/rand"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
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
		if result.Dead {
			break
		}
	}
	if !result.Dead {
		t.Fatalf("monster did not die")
	}
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
	return w, ch
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

func TestCastSkillRespectsCooldown(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	ch.Skills = storage.SkillStates{{ID: "火球术", Level: 0, Train: 0}}
	ch.MP = 100

	first, err := w.CastSkillWithPlayers(ch, "火球术", ch.X+1, ch.Y, 0, nil)
	if err != nil {
		t.Fatalf("first CastSkillWithPlayers() error = %v", err)
	}
	if first.Character.Skills[0].LastCastAt == 0 {
		t.Fatal("first cast LastCastAt = 0, want set")
	}

	second, err := w.CastSkillWithPlayers(first.Character, "火球术", ch.X+1, ch.Y, 0, nil)
	if err == nil {
		t.Fatalf("second CastSkillWithPlayers() error = nil, want cooldown rejection")
	}
	if !strings.Contains(err.Error(), "cooling down") {
		t.Fatalf("second CastSkillWithPlayers() error = %v, want cooldown rejection", err)
	}
	if second.SkillID != "" {
		t.Fatalf("second result = %+v, want zero-value result on cooldown rejection", second)
	}
}

func TestCastSkillRejectsTargetsOutOfRange(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	caster.Skills = storage.SkillStates{{ID: "火球术", Level: 0, Train: 0}}
	caster.MP = 100
	if _, err := w.CastSkillWithPlayers(caster, "火球术", caster.X+9, caster.Y, 0, nil); err == nil {
		t.Fatal("CastSkillWithPlayers() expected range rejection")
	}
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
	skillInfo, ok := skillW.Skill("刺杀剑术")
	if !ok {
		t.Fatal("skill 刺杀剑术 missing from config")
	}
	want := baseDamage + int(math.Round(float64(baseDamage)/float64(skillInfo.TrainLevel1+2)*float64(2)))
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
	skillInfo, ok := skillW.Skill("半月弯刀")
	if !ok {
		t.Fatal("skill 半月弯刀 missing from config")
	}
	want := baseDamage + int(math.Round(float64(baseDamage)/float64(skillInfo.TrainLevel1+10)*float64(2)))
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
	updated, err := w.CastSkillWithPlayers(caster, "火球术", targetX, targetY, 0, nil)
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
	caster.MP = 50
	friend.HP = 30
	friend.MaxHP = 100
	players := []storage.Character{caster, friend}
	result, err := w.CastSkillWithPlayers(caster, "群体治疗术", x, y, 0, players)
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	if result.SkillID != "群体治疗术" {
		t.Fatalf("SkillID = %q, want 群体治疗术", result.SkillID)
	}
	if len(result.AffectedCharacters) != 2 {
		t.Fatalf("AffectedCharacters = %d, want 2", len(result.AffectedCharacters))
	}
	if result.Character.HP <= 20 {
		t.Fatalf("caster HP = %d, want healed above 20", result.Character.HP)
	}
	if result.Character.MP >= 50 {
		t.Fatalf("caster MP = %d, want spent mana", result.Character.MP)
	}
	seenCaster := false
	seenFriend := false
	for _, ch := range result.AffectedCharacters {
		switch ch.ID {
		case caster.ID:
			seenCaster = true
			if ch.HP <= 20 {
				t.Fatalf("caster affected HP = %d, want healed", ch.HP)
			}
		case friend.ID:
			seenFriend = true
			if ch.HP <= 30 {
				t.Fatalf("friend affected HP = %d, want healed", ch.HP)
			}
		}
	}
	if !seenCaster || !seenFriend {
		t.Fatalf("affected set missing caster or friend: caster=%v friend=%v", seenCaster, seenFriend)
	}
}

func TestCastSkillGroupHealingHealsFriendlySummon(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	caster.Dir = 2
	caster.MP = 100
	caster.HP = 20
	caster.MaxHP = 100
	caster.Skills = storage.SkillStates{{ID: "召唤骷髅", Level: 0, Train: 0}, {ID: "群体治疗术", Level: 0, Train: 0}}
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
	if updated.Character.HP <= 20 {
		t.Fatalf("caster HP = %d, want healed above 20", updated.Character.HP)
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
	caster.MP = 100
	updated, err := w.CastSkillWithPlayers(caster, "群体治疗术", caster.X, caster.Y, 0, []storage.Character{caster, friend})
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	if updated.SkillID != "群体治疗术" {
		t.Fatalf("SkillID = %q, want 群体治疗术", updated.SkillID)
	}
	if len(updated.AffectedCharacters) != 1 || len(updated.AffectedMonsters) != 0 {
		t.Fatalf("affected targets = chars:%d mons:%d, want one valid friendly cast target on full health", len(updated.AffectedCharacters), len(updated.AffectedMonsters))
	}
	if updated.AffectedCharacters[0].ID != caster.ID {
		t.Fatalf("affected character = %q, want caster queued first", updated.AffectedCharacters[0].ID)
	}
	if updated.Character.MP >= 100 {
		t.Fatalf("caster MP = %d, want mana spent even on full health targets", updated.Character.MP)
	}
}

func TestCastSkillGroupHealingReturnsFriendlySummonInAffectedMonsters(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	caster.Dir = 2
	caster.MP = 100
	caster.HP = 20
	caster.MaxHP = 100
	caster.Skills = storage.SkillStates{{ID: "召唤骷髅", Level: 0, Train: 0}, {ID: "群体治疗术", Level: 0, Train: 0}}
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
	if len(updated.AffectedMonsters) != 1 {
		t.Fatalf("AffectedMonsters = %d, want 1", len(updated.AffectedMonsters))
	}
	if updated.AffectedMonsters[0].ID != summoned.ID {
		t.Fatalf("AffectedMonsters[0].ID = %q, want %q", updated.AffectedMonsters[0].ID, summoned.ID)
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
	if len(updated.AffectedMonsters) != 2 {
		t.Fatalf("AffectedMonsters = %d, want 2", len(updated.AffectedMonsters))
	}
	if updated.AffectedMonsters[0].ID != first.ID || updated.AffectedMonsters[1].ID != second.ID {
		t.Fatalf("affected monster order = [%s %s], want [%s %s]", updated.AffectedMonsters[0].ID, updated.AffectedMonsters[1].ID, first.ID, second.ID)
	}
}

func TestCastSkillHealRestoresCasterHP(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	caster.Skills = storage.SkillStates{{ID: "治愈术", Level: 0, Train: 0}}
	caster.HP = 20
	caster.MaxHP = 100
	updated, err := w.CastSkillWithPlayers(caster, "治愈术", caster.X, caster.Y, 0, nil)
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	if updated.Character.HP <= 20 {
		t.Fatalf("caster HP = %d, want healed above 20", updated.Character.HP)
	}
	if updated.Character.HP > updated.Character.MaxHP {
		t.Fatalf("caster HP = %d, want not above max %d", updated.Character.HP, updated.Character.MaxHP)
	}
}

func TestCastSkillHealIgnoresNoTargetCoordinates(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	caster.Skills = storage.SkillStates{{ID: "治愈术", Level: 0, Train: 0}}
	caster.HP = 20
	caster.MaxHP = 100
	updated, err := w.CastSkillWithPlayers(caster, "治愈术", 3, 0, 0, nil)
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	if updated.Character.HP <= 20 {
		t.Fatalf("caster HP = %d, want healed above 20", updated.Character.HP)
	}
}

func TestCastSkillHealHealsFriendlySummon(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	caster.Dir = 2
	caster.MP = 100
	caster.HP = 20
	caster.MaxHP = 100
	caster.Skills = storage.SkillStates{{ID: "召唤骷髅", Level: 0, Train: 0}, {ID: "治愈术", Level: 0, Train: 0}}
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
	updated, err := w.CastSkillWithPlayers(summonedResult.Character, "治愈术", summoned.X, summoned.Y, 0, nil)
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() heal error = %v", err)
	}
	if updated.Character.HP != 20 {
		t.Fatalf("caster HP = %d, want self HP unchanged when healing summon", updated.Character.HP)
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
	if len(updated.AffectedCharacters) != 1 {
		t.Fatalf("AffectedCharacters = %d, want 1", len(updated.AffectedCharacters))
	}
	if updated.AffectedCharacters[0].ID != friend.ID {
		t.Fatalf("affected target = %s, want %s", updated.AffectedCharacters[0].ID, friend.ID)
	}
	if updated.AffectedCharacters[0].HP <= 30 {
		t.Fatalf("friend HP = %d, want healed", updated.AffectedCharacters[0].HP)
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
	if len(updated.AffectedCharacters) != 1 {
		t.Fatalf("AffectedCharacters = %d, want 1", len(updated.AffectedCharacters))
	}
	if updated.AffectedCharacters[0].ID != friend.ID {
		t.Fatalf("affected target = %s, want %s", updated.AffectedCharacters[0].ID, friend.ID)
	}
	if updated.AffectedCharacters[0].HP <= 30 {
		t.Fatalf("friend HP = %d, want healed", updated.AffectedCharacters[0].HP)
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
	if len(updated.AffectedCharacters) != 1 {
		t.Fatalf("AffectedCharacters = %d, want 1", len(updated.AffectedCharacters))
	}
	if updated.AffectedCharacters[0].ID != second.ID {
		t.Fatalf("affected target = %s, want %s", updated.AffectedCharacters[0].ID, second.ID)
	}
	if updated.AffectedCharacters[0].HP <= 20 {
		t.Fatalf("second HP = %d, want healed", updated.AffectedCharacters[0].HP)
	}
}

func TestCastSkillHealRejectsHostileTarget(t *testing.T) {
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
	if err == nil {
		t.Fatal("CastSkillWithPlayers() error = nil, want hostile target rejected")
	}
	if updated.Character.ID != "" {
		t.Fatalf("updated character = %+v, want zero value on rejection", updated.Character)
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

func TestCastSkillProtectionBuffsApplyToFriendlySummon(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	caster.Dir = 2
	caster.MP = 100
	caster.Skills = storage.SkillStates{
		{ID: "召唤骷髅", Level: 0, Train: 0},
		{ID: "神圣战甲术", Level: 0, Train: 0},
	}
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
	caster.Dir = 2
	caster.MP = 100
	caster.Skills = storage.SkillStates{
		{ID: "召唤骷髅", Level: 0, Train: 0},
		{ID: "神圣战甲术", Level: 0, Train: 0},
	}
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
	caster.Skills = storage.SkillStates{{ID: "隐身术", Level: 0, Train: 0}}
	updated, err := w.CastSkillWithPlayers(caster, "隐身术", caster.X, caster.Y, 0, nil)
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	if updated.Character.TransparentUntil == 0 {
		t.Fatal("TransparentUntil = 0, want active stealth")
	}
}

func TestCastSkillStealthBreaksMonsterTarget(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
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
}

func TestCastSkillGroupStealthMarksGroupMembersTransparent(t *testing.T) {
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
	caster.Skills = storage.SkillStates{{ID: "集体隐身术", Level: 0, Train: 0}}
	players := []storage.Character{caster, friend}
	updated, err := w.CastSkillWithPlayers(caster, "集体隐身术", x, y, 0, players)
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	if len(updated.AffectedCharacters) != 2 {
		t.Fatalf("AffectedCharacters = %d, want 2", len(updated.AffectedCharacters))
	}
	seenCaster := false
	seenFriend := false
	for _, ch := range updated.AffectedCharacters {
		if ch.TransparentUntil == 0 {
			t.Fatalf("affected character = %+v, want transparent", ch)
		}
		switch ch.ID {
		case caster.ID:
			seenCaster = true
		case friend.ID:
			seenFriend = true
		}
	}
	if !seenCaster || !seenFriend {
		t.Fatalf("affected set missing caster or friend: caster=%v friend=%v", seenCaster, seenFriend)
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
	if len(updated.MonsterHits) != 2 {
		t.Fatalf("MonsterHits = %d, want 2", len(updated.MonsterHits))
	}
	for _, hit := range updated.MonsterHits {
		if hit.Damage <= 0 {
			t.Fatalf("hit = %+v, want positive damage", hit)
		}
		if hit.MonsterHP >= hit.MonsterMaxHP {
			t.Fatalf("hit = %+v, want reduced monster hp", hit)
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
	if len(updated.CharacterHits) != 1 {
		t.Fatalf("CharacterHits = %d, want 1", len(updated.CharacterHits))
	}
	hit := updated.CharacterHits[0]
	if hit.Character.ID != target.ID {
		t.Fatalf("hit.Character.ID = %q, want %q", hit.Character.ID, target.ID)
	}
	if hit.Damage <= 0 {
		t.Fatalf("hit = %+v, want positive damage", hit)
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
	caster.MP = 100
	updated, err := w.CastSkillWithPlayers(caster, "灵魂火符", targetX, targetY, 0, nil)
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	if updated.MonsterHit == nil || updated.MonsterHit.Damage <= 0 {
		t.Fatalf("MonsterHit = %+v, want positive damage", updated.MonsterHit)
	}
}

func TestCastSkillSpiritFireCanBeResistedByMagicDefense(t *testing.T) {
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
		mon.MagicDefense = 10
	}
	w.mu.Unlock()
	caster.Skills = storage.SkillStates{{ID: "灵魂火符", Level: 0, Train: 0}}
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
	caster.MP = 100
	updated, err := w.CastSkillWithPlayers(caster, "灵魂火符", 3, 0, 0, nil)
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

	baseW, baseCaster, _ := prepare(0)
	baseResult, err := baseW.CastSkillWithPlayers(baseCaster, "雷电术", baseCaster.X+1, baseCaster.Y, 0, nil)
	if err != nil {
		t.Fatalf("base CastSkillWithPlayers() error = %v", err)
	}
	if baseResult.MonsterHit == nil || baseResult.MonsterHit.Damage <= 0 {
		t.Fatalf("base lightning hit = %+v, want positive", baseResult.MonsterHit)
	}

	highW, highCaster, _ := prepare(10)
	highResult, err := highW.CastSkillWithPlayers(highCaster, "雷电术", highCaster.X+1, highCaster.Y, 0, nil)
	if err != nil {
		t.Fatalf("high CastSkillWithPlayers() error = %v", err)
	}
	if highResult.MonsterHit == nil {
		t.Fatalf("high lightning hit = nil, want hit")
	}
	if highResult.MonsterHit.Damage > baseResult.MonsterHit.Damage {
		t.Fatalf("high magic defense lightning hit = %d, base = %d, want reduced or equal", highResult.MonsterHit.Damage, baseResult.MonsterHit.Damage)
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
	caster.MP = 100
	updated, err := w.CastSkillWithPlayers(caster, "灵魂火符", targetX, targetY, CharacterActorID(target), []storage.Character{target})
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	if len(updated.CharacterHits) != 1 {
		t.Fatalf("CharacterHits = %d, want 1", len(updated.CharacterHits))
	}
	hit := updated.CharacterHits[0]
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
	if len(updated.AffectedCharacters) != 1 {
		t.Fatalf("AffectedCharacters = %d, want 1", len(updated.AffectedCharacters))
	}
	if updated.AffectedCharacters[0].ID != target.ID {
		t.Fatalf("AffectedCharacters[0].ID = %q, want %q", updated.AffectedCharacters[0].ID, target.ID)
	}
	if updated.AffectedCharacters[0].ShowHPUntil <= 0 {
		t.Fatal("ShowHPUntil = 0, want future expiration")
	}
	if updated.AffectedCharacters[0].ShowHPOpenAt <= 0 {
		t.Fatal("ShowHPOpenAt = 0, want delayed open time")
	}
	if updated.AffectedCharacters[0].ShowHPOpenAt <= time.Now().Add(time.Second).UnixNano() {
		t.Fatalf("ShowHPOpenAt = %d, want roughly 1.5s delay", updated.AffectedCharacters[0].ShowHPOpenAt)
	}
	if updated.AffectedCharacters[0].ShowHPUntil <= updated.AffectedCharacters[0].ShowHPOpenAt {
		t.Fatalf("ShowHPUntil = %d, want after ShowHPOpenAt = %d", updated.AffectedCharacters[0].ShowHPUntil, updated.AffectedCharacters[0].ShowHPOpenAt)
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
	if len(updated.MonsterHits) != 2 {
		t.Fatalf("MonsterHits = %d, want 2", len(updated.MonsterHits))
	}
	for _, hit := range updated.MonsterHits {
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
	if len(updated.MonsterHits) != 2 {
		t.Fatalf("MonsterHits = %d, want 2", len(updated.MonsterHits))
	}
	for _, hit := range updated.MonsterHits {
		if hit.Damage <= 0 {
			t.Fatalf("hit = %+v, want positive damage", hit)
		}
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
	updated, err := w.CastSkillWithPlayers(caster, "疾光电影", targetX, targetY, CharacterActorID(target), []storage.Character{target})
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	if len(updated.MonsterHits) != 2 {
		t.Fatalf("MonsterHits = %d, want 2", len(updated.MonsterHits))
	}
	if len(updated.CharacterHits) != 1 {
		t.Fatalf("CharacterHits = %d, want 1", len(updated.CharacterHits))
	}
	for _, hit := range updated.MonsterHits {
		if hit.Damage <= 0 {
			t.Fatalf("monster hit = %+v, want positive damage", hit)
		}
	}
	if updated.CharacterHits[0].Damage <= 0 {
		t.Fatalf("character hit = %+v, want positive damage", updated.CharacterHits[0])
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
	updated, err := w.CastSkillWithPlayers(caster, "地狱雷光", caster.X, caster.Y, CharacterActorID(target), []storage.Character{target})
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	if len(updated.MonsterHits) != 2 {
		t.Fatalf("MonsterHits = %d, want 2", len(updated.MonsterHits))
	}
	if len(updated.CharacterHits) != 1 {
		t.Fatalf("CharacterHits = %d, want 1", len(updated.CharacterHits))
	}
	var undeadHit, normalHit *AttackResult
	for i := range updated.MonsterHits {
		hit := &updated.MonsterHits[i]
		if hit.MonsterMaxHP > 100 {
			undeadHit = hit
		} else {
			normalHit = hit
		}
	}
	if undeadHit == nil || normalHit == nil {
		t.Fatalf("monster hits = %+v, want both undead and normal targets", updated.MonsterHits)
	}
	if undeadHit.Damage <= normalHit.Damage {
		t.Fatalf("monster hits = %+v, want undead target to take more damage than normal target", updated.MonsterHits)
	}
	if updated.CharacterHits[0].Damage <= 0 {
		t.Fatalf("character hit = %+v, want positive damage", updated.CharacterHits[0])
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
	for _, hit := range baseResult.MonsterHits {
		if hit.MonsterID == baseMonsterID {
			baseHit = hit.Damage
			break
		}
	}
	if baseHit <= 0 {
		t.Fatalf("base monster hit = %d, want positive", baseHit)
	}

	highW, highCaster, highMonsterID := prepare(10)
	highResult, err := highW.CastSkillWithPlayers(highCaster, "地狱雷光", highCaster.X, highCaster.Y, 0, nil)
	if err != nil {
		t.Fatalf("high CastSkillWithPlayers() error = %v", err)
	}
	highHit := 0
	for _, hit := range highResult.MonsterHits {
		if hit.MonsterID == highMonsterID {
			highHit = hit.Damage
			break
		}
	}
	if highHit > baseHit {
		t.Fatalf("high magic defense hit = %d, base = %d, want reduced or equal", highHit, baseHit)
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
	if got := len(w.monstersInRadiusLocked(mapID, monster.X, monster.Y, 0)); got != 1 {
		t.Fatalf("monster coverage = %d, want 1 at fire wall cross", got)
	}
	if got := len(w.charactersInRadiusLocked([]storage.Character{updated.Character, target}, mapID, target.X, target.Y, 0)); got != 1 {
		t.Fatalf("character coverage = %d, want 1 at fire wall cross", got)
	}
	fireTickAt := time.Now().Add(250 * time.Millisecond)
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
	if len(updated.MonsterActions) != 1 {
		t.Fatalf("MonsterActions = %d, want 1", len(updated.MonsterActions))
	}
	if updated.MonsterActions[0].X != caster.X || updated.MonsterActions[0].Y <= caster.Y+1 {
		t.Fatalf("monster action = %+v, want pushed farther south", updated.MonsterActions[0])
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
	if updated.Character.X != caster.X+2 || updated.Character.Y != caster.Y {
		t.Fatalf("caster = %+v, want moved two tiles east", updated.Character)
	}
	if len(updated.AffectedCharacters) != 1 {
		t.Fatalf("AffectedCharacters = %d, want 1", len(updated.AffectedCharacters))
	}
	if updated.AffectedCharacters[0].X != caster.X+3 {
		t.Fatalf("affected character = %+v, want pushed farther east", updated.AffectedCharacters[0])
	}
	if len(updated.CharacterHits) != 1 {
		t.Fatalf("CharacterHits = %d, want 1", len(updated.CharacterHits))
	}
	if updated.CharacterHits[0].Damage <= 0 {
		t.Fatalf("CharacterHits[0].Damage = %d, want positive charge damage", updated.CharacterHits[0].Damage)
	}
	if updated.AffectedCharacters[0].HP >= 1000 {
		t.Fatalf("affected character HP = %d, want reduced by charge", updated.AffectedCharacters[0].HP)
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
}

func TestCastSkillMagicShieldReducesMonsterDamageAndConsumesShieldTime(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	caster.Skills = storage.SkillStates{{ID: "魔法盾", Level: 25, Train: 0}}
	caster.MP = 100
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
	if _, err := w.CastSkillWithPlayers(updated.Character, "魔法盾", updated.Character.X, updated.Character.Y, 0, nil); err == nil {
		t.Fatal("CastSkillWithPlayers() expected active shield recast to fail")
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
	caster.Level = 100
	caster.MP = 500
	caster.Skills = storage.SkillStates{{ID: "圣言术", Level: 0, Train: 0}}
	updated, err := w.CastSkillWithPlayers(caster, "圣言术", targetX, targetY, 0, nil)
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	if updated.MonsterHit == nil {
		t.Fatal("MonsterHit = nil, want undead monster hit")
	}
	if !updated.MonsterHit.Dead {
		t.Fatalf("MonsterHit = %+v, want dead monster", updated.MonsterHit)
	}
	if updated.MonsterHit.MonsterID == "" {
		t.Fatal("MonsterHit.MonsterID = empty, want target monster id")
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
	updated, err := w.CastSkillWithPlayers(caster, "圣言术", targetX, targetY, 0, nil)
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
	if !mon.RunAwayMode {
		t.Fatal("monster RunAwayMode = false, want flee state after failed turn undead")
	}
	if mon.TargetCharacterID != caster.ID {
		t.Fatalf("monster TargetCharacterID = %q, want %q", mon.TargetCharacterID, caster.ID)
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
	if summoned.TemplateID != "骷髅" {
		t.Fatalf("summoned.TemplateID = %q, want 骷髅", summoned.TemplateID)
	}
	if summoned.Alive != true {
		t.Fatalf("summoned.Alive = false, want true")
	}
	if summoned.MasterExpiresAt.IsZero() {
		t.Fatal("summoned.MasterExpiresAt = zero, want active expiry")
	}
	recast := result.Character
	recast.Skills[0].LastCastAt = time.Now().Add(-time.Second).UnixMilli()
	dup, err := w.CastSkillWithPlayers(recast, "召唤骷髅", recast.X, recast.Y, 0, nil)
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
	if _, err := w.Tick([]PlayerSnapshot{{Character: result.Character}}, time.Now()); err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	monsters, _ := w.SnapshotAround(result.Character.MapID, result.Character.X, result.Character.Y, 10)
	for _, mon := range monsters {
		if mon.ID == summoned.ID {
			t.Fatalf("summoned monster %s still visible after expiry", summoned.ID)
		}
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
	caster.MP = 100
	caster.Skills = storage.SkillStates{{ID: "召唤骷髅", Level: 0, Train: 0}}
	players := []storage.Character{blocker}
	_, err = w.CastSkillWithPlayers(caster, "召唤骷髅", caster.X, caster.Y, 0, players)
	if err == nil {
		t.Fatal("CastSkillWithPlayers() error = nil, want front-blocked summon to fail")
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
	if _, err := w.Tick([]PlayerSnapshot{{Character: result.Character}}, time.Now()); err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	monsters, _ := w.SnapshotAround(result.Character.MapID, result.Character.X, result.Character.Y, 10)
	for _, mon := range monsters {
		if mon.ID == summoned.ID {
			t.Fatalf("summoned beast %s still visible after expiry", summoned.ID)
		}
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
	result, err := w.CastSkillWithPlayers(caster, "召唤神兽", caster.Dir, 0, 0, nil)
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

func TestCastSkillSummonRespectsSharedCapAcrossTemplates(t *testing.T) {
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
		t.Fatal("could not find clear tile for shared summon cap test")
	}
	caster.X = targetX
	caster.Y = targetY
	caster.Dir = 2
	caster.MP = 100
	caster.Skills = storage.SkillStates{{ID: "召唤骷髅", Level: 0, Train: 0}, {ID: "召唤神兽", Level: 0, Train: 0}}
	w.mu.Lock()
	tpl, ok := w.data.Monsters["鸡"]
	if !ok {
		w.mu.Unlock()
		t.Fatal("monster 鸡 missing from configs")
	}
	for i := 0; i < defaultTamingCount-1; i++ {
		id := fmt.Sprintf("shared-cap-%d", i)
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
	if len(blocked.SummonedMonsters) != 0 {
		t.Fatalf("blocked beast summon = %+v, want no new summon at shared cap", blocked.SummonedMonsters)
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
			cost := w.SpellCost(skill, storage.SkillState{ID: "施毒术", Level: 0, Train: 0})
			caster.Level = 14
			caster.MP = cost + 20
			caster.Skills = storage.SkillStates{{ID: "施毒术", Level: 0, Train: 0}}
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
			updated, err := w.CastSkillWithPlayers(caster, "施毒术", baseX+1, baseY, 0, nil)
			if err != nil {
				t.Fatalf("CastSkillWithPlayers() error = %v", err)
			}
			if got := updated.Character.MP; got != caster.MP-cost {
				t.Fatalf("MP = %d, want %d after poison", got, caster.MP-cost)
			}
			if got := updated.Character.EquippedItems[tc.powderSlot].Dura; got != beforeDura-100 {
				t.Fatalf("poison powder dura = %d, want %d", got, beforeDura-100)
			}
			w.mu.Lock()
			mon := w.monsters[monsterID]
			w.mu.Unlock()
			if mon == nil {
				t.Fatalf("monster %s missing after poison cast", monsterID)
			}
			if tc.wantHealth {
				if mon.PoisonHealthLevel == 0 || mon.PoisonHealthUntil.IsZero() || mon.PoisonHealthStartAt.IsZero() {
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
			cost := w.SpellCost(skill, storage.SkillState{ID: "施毒术", Level: 0, Train: 0})
			caster.Level = 14
			caster.MP = cost + 20
			caster.Skills = storage.SkillStates{{ID: "施毒术", Level: 0, Train: 0}}
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
			updated, err := w.CastSkillWithPlayers(caster, "施毒术", baseX+1, baseY, 0, []storage.Character{target})
			if err != nil {
				t.Fatalf("CastSkillWithPlayers() error = %v", err)
			}
			if got := updated.Character.MP; got != caster.MP-cost {
				t.Fatalf("MP = %d, want %d after poison", got, caster.MP-cost)
			}
			if got := updated.Character.EquippedItems[tc.powderSlot].Dura; got != beforeDura-100 {
				t.Fatalf("poison powder dura = %d, want %d", got, beforeDura-100)
			}
			if len(updated.AffectedCharacters) != 1 {
				t.Fatalf("AffectedCharacters = %d, want 1", len(updated.AffectedCharacters))
			}
			poisoned := updated.AffectedCharacters[0]
			if tc.wantHealth {
				if poisoned.PoisonHealthLevel == 0 || poisoned.PoisonHealthUntil == 0 || poisoned.PoisonHealthTickAt == 0 || poisoned.PoisonHealthStartAt == 0 {
					t.Fatalf("character poison health = %+v, want active health poison", poisoned)
				}
				if !time.Unix(0, poisoned.PoisonHealthStartAt).After(time.Now()) {
					t.Fatalf("character poison health start = %d, want future start", poisoned.PoisonHealthStartAt)
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
				if !time.Unix(0, poisoned.PoisonArmorStartAt).After(time.Now()) {
					t.Fatalf("character poison armor start = %d, want future start", poisoned.PoisonArmorStartAt)
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
	updated, err := w.CastSkillWithPlayers(caster, "诱惑之光", targetX, targetY, 0, nil)
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
	w.mu.Lock()
	if mon, ok := w.monsters[controlled.ID]; ok {
		mon.MasterExpiresAt = time.Now().Add(-time.Second)
	}
	w.mu.Unlock()
	if _, err := w.Tick([]PlayerSnapshot{{Character: updated.Character}}, time.Now()); err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	monsters, _ := w.SnapshotAround(updated.Character.MapID, updated.Character.X, updated.Character.Y, 10)
	for _, mon := range monsters {
		if mon.ID == controlled.ID {
			t.Fatalf("controlled monster %s still visible after expiry", controlled.ID)
		}
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
	_, err = w.CastSkillWithPlayers(caster, "诱惑之光", targetX, targetY, 0, nil)
	if err == nil {
		t.Fatal("CastSkillWithPlayers() error = nil, want controlled monster limit rejection")
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
	updated, err := w.CastSkillWithPlayers(caster, "诱惑之光", targetX, targetY, 0, nil)
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
	w.mu.Unlock()
	_, err = w.CastSkillWithPlayers(caster, "诱惑之光", targetX, targetY, 0, nil)
	if err == nil {
		t.Fatal("CastSkillWithPlayers() error = nil, want caster-plus-two level rejection")
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
	_, err = w.CastSkillWithPlayers(caster, "诱惑之光", targetX, targetY, 0, nil)
	if err == nil {
		t.Fatal("CastSkillWithPlayers() error = nil, want level cap rejection")
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
	if len(updated.AffectedCharacters) != 1 {
		t.Fatalf("AffectedCharacters = %d, want 1", len(updated.AffectedCharacters))
	}
	if updated.AffectedCharacters[0].ID != targetB.ID {
		t.Fatalf("affected target = %q, want explicit targetID %q", updated.AffectedCharacters[0].ID, targetB.ID)
	}
}

func TestCastSkillInsightFallsBackToFirstCandidate(t *testing.T) {
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
	if len(updated.AffectedCharacters) != 1 {
		t.Fatalf("AffectedCharacters = %d, want 1", len(updated.AffectedCharacters))
	}
	if updated.AffectedCharacters[0].ID != targetA.ID {
		t.Fatalf("affected target = %q, want first candidate %q", updated.AffectedCharacters[0].ID, targetA.ID)
	}
}

func TestCastSkillTamingMonsterRespectsMonsterHpGate(t *testing.T) {
	prepare := func(maxHP int, seed int64) (*World, storage.Character, int, int) {
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
		return w, caster, targetX, targetY
	}

	seed := int64(-1)
	lowOK := false
	highFail := false
	var lowW *World
	var lowCaster storage.Character
	var lowX, lowY int
	for s := int64(0); s < 1000; s++ {
		lowW, lowCaster, lowX, lowY = prepare(100, s)
		lowUpdated, err := lowW.CastSkillWithPlayers(lowCaster, "诱惑之光", lowX, lowY, 0, nil)
		if err != nil {
			continue
		}
		if len(lowUpdated.AffectedMonsters) != 1 {
			continue
		}
		highW, highCaster, highX, highY := prepare(500, s)
		highUpdated, err := highW.CastSkillWithPlayers(highCaster, "诱惑之光", highX, highY, 0, nil)
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
	lowW, lowCaster, lowX, lowY = prepare(100, seed)
	lowUpdated, err := lowW.CastSkillWithPlayers(lowCaster, "诱惑之光", lowX, lowY, 0, nil)
	if err != nil {
		t.Fatalf("low HP CastSkillWithPlayers() error = %v", err)
	}
	if len(lowUpdated.AffectedMonsters) != 1 {
		t.Fatalf("low HP affected monsters = %d, want 1", len(lowUpdated.AffectedMonsters))
	}

	highW, highCaster, highX, highY := prepare(500, seed)
	highUpdated, err := highW.CastSkillWithPlayers(highCaster, "诱惑之光", highX, highY, 0, nil)
	if err != nil {
		t.Fatalf("high HP CastSkillWithPlayers() error = %v", err)
	}
	if len(highUpdated.AffectedMonsters) != 0 {
		t.Fatalf("high HP affected monsters = %d, want 0", len(highUpdated.AffectedMonsters))
	}
}

func TestCastSkillTrapDamagesMultipleMonsters(t *testing.T) {
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
	caster.Level = 100
	caster.MP = 100
	caster.Skills = storage.SkillStates{{ID: "困魔咒", Level: 5, Train: 0}}
	updated, err := w.CastSkillWithPlayers(caster, "困魔咒", targetX, targetY, 0, nil)
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() error = %v", err)
	}
	if len(updated.MonsterHits) != 2 {
		t.Fatalf("MonsterHits = %d, want 2", len(updated.MonsterHits))
	}
	for _, hit := range updated.MonsterHits {
		if hit.Damage <= 0 {
			t.Fatalf("hit = %+v, want positive damage", hit)
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

func TestTickClearsExpiredStealthAndQueuesStateRefresh(t *testing.T) {
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
	if len(result.StateRefreshCharacters) != 1 {
		t.Fatalf("StateRefreshCharacters = %+v, want 1", result.StateRefreshCharacters)
	}
	if result.StateRefreshCharacters[0].ID != ch.ID {
		t.Fatalf("StateRefreshCharacters[0].ID = %q, want %q", result.StateRefreshCharacters[0].ID, ch.ID)
	}
	if len(result.Characters) != 1 {
		t.Fatalf("Characters = %+v, want 1", result.Characters)
	}
	if result.Characters[0].TransparentUntil != 0 {
		t.Fatalf("TransparentUntil = %d, want 0", result.Characters[0].TransparentUntil)
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
