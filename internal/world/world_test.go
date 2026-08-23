package world

import (
	"fmt"
	"math/rand"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"

	"openmir2/internal/config"
	"openmir2/internal/data"
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
	bundle, err := data.Load(testConfigsDir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
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
	mapID, x, y := w.DefaultSpawn()
	ch, err := w.CreateCharacter("test", "tester", "warrior", mapID, x, y)
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
	mapID, x, y := w.DefaultSpawn()
	ch, err := w.CreateCharacter("test", "tester", "warrior", mapID, x, y)
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
	mapID, x, y := w.DefaultSpawn()
	ch, err := w.CreateCharacter("test", "tester", "warrior", mapID, x, y)
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
	monsters, _ := w.Snapshot(ch.MapID)
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
	monsters, _ := w.Snapshot(testMapID)
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
	monsters, _ := w.Snapshot(ch.MapID)
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
	monsters, _ := w.Snapshot(ch.MapID)
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
	monsters, _ := w.Snapshot(ch.MapID)
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
	mapID, x, y := w.DefaultSpawn()
	ch, err := w.CreateCharacter("test", "tester", "warrior", mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch, err = w.Move(ch, x+1, y)
	if err != nil {
		t.Fatalf("Move() error = %v", err)
	}
	monsters, _ := w.Snapshot(ch.MapID)
	if len(monsters) == 0 {
		t.Fatalf("expected monsters")
	}
	var result AttackResult
	for i := 0; i < 10; i++ {
		result, err = w.Attack(ch, monsters[0].ID)
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
	ch, _, err = w.Pickup(ch, result.Drops[0].ID)
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

func TestDefaultSpawnIsDeterministic(t *testing.T) {
	bundle := loadTestBundle(t)
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	w := New(bundle, store)
	mapID, x, y := w.DefaultSpawn()
	if mapID == "" {
		t.Fatal("DefaultSpawn() returned empty map id")
	}
	found := false
	for _, sp := range allStartPoints(bundle) {
		if sp.MapID == mapID && sp.X == x && sp.Y == y {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("DefaultSpawn() = (%s,%d,%d), want one of configured start points", mapID, x, y)
	}
	for i := 0; i < 20; i++ {
		nextMapID, nextX, nextY := w.DefaultSpawn()
		if nextMapID != mapID || nextX != x || nextY != y {
			t.Fatalf("DefaultSpawn() call %d = (%s,%d,%d), want (%s,%d,%d)", i, nextMapID, nextX, nextY, mapID, x, y)
		}
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
	ch, err := w.CreateCharacter("test", "tester3", "warrior", testMapID, startX, startY)
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
	monsters, _ := w.Snapshot(ch.MapID)
	if len(monsters) == 0 {
		t.Fatalf("expected monsters on config map")
	}
	if _, err := w.Attack(ch, monsters[0].ID); err != nil {
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
	mapID, x, y := w.DefaultSpawn()
	ch, err := w.CreateCharacter("test", "tester", "warrior", mapID, x, y)
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
	ch, err := w.CreateCharacter("test", "tester1", "warrior", testMapID, startX, startY)
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
	monsters, _ := w.Snapshot(ch.MapID)
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

func TestSpawnMonsterByNameAddsLiveMonsters(t *testing.T) {
	w, ch := newRealDataWorldCharacter(t)
	before, _ := w.Snapshot(ch.MapID)
	result, err := w.SpawnMonsterByName(ch.MapID, ch.X+1, ch.Y, "鹿", 2)
	if err != nil {
		t.Fatalf("SpawnMonsterByName() error = %v", err)
	}
	if len(result.Monsters) != 2 {
		t.Fatalf("spawn result = %d monsters, want 2", len(result.Monsters))
	}
	after, _ := w.Snapshot(ch.MapID)
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
	result, err := w.SpawnMonsterByName(ch.MapID, ch.X+1, ch.Y, "赤月恶魔", 1)
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
	monsters, _ := w.Snapshot(testMapID)
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
	mapID, x, y := w.DefaultSpawn()
	ch, err := w.CreateCharacter("test", "tester", "warrior", mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	if _, err := w.SpawnMonsterByName(ch.MapID, targetX, targetY, "鹿", 1); err == nil {
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
	monsters, _ := w.Snapshot(testMapID)
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
	monsters, _ := w.Snapshot(testMapID)
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
	monsters, _ := w.Snapshot(testMapID)
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

	after, _ := w.Snapshot("0")
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

	result, err := w.SpawnMonsterByName(ch.MapID, ch.X+1, ch.Y, "暗之触龙神", 1)
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

	result, err = w.SpawnMonsterByName(ch.MapID, ch.X+2, ch.Y, "圣域弓箭手", 1)
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

func TestMoveBagItemMovesIntoEmptySlot(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	ch.BagItems = []storage.UserItem{{ItemID: testHPItemID, MakeIndex: 11}}

	updated, err := w.MoveBagItem(ch, 0, 5)
	if err != nil {
		t.Fatalf("MoveBagItem() error = %v", err)
	}
	if len(updated.BagItems) != 1 {
		t.Fatalf("bag len = %d, want 1", len(updated.BagItems))
	}
	if updated.BagItems[0].ItemID != testHPItemID {
		t.Fatalf("bag[0] = %+v, want moved item", updated.BagItems[0])
	}
	if updated.BagItems[0].MakeIndex != 11 {
		t.Fatalf("bag[0].MakeIndex = %d, want 11", updated.BagItems[0].MakeIndex)
	}
}

func TestMoveBagItemSwapsOccupiedSlots(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	ch.BagItems = []storage.UserItem{
		{ItemID: testHPItemID, MakeIndex: 11},
		{ItemID: testWeaponID, MakeIndex: 22},
	}

	updated, err := w.MoveBagItem(ch, 0, 5)
	if err != nil {
		t.Fatalf("MoveBagItem() error = %v", err)
	}
	if updated.BagItems[0].ItemID != testWeaponID {
		t.Fatalf("bag[0] = %+v, want remaining item first", updated.BagItems[0])
	}
	if updated.BagItems[1].ItemID != testHPItemID {
		t.Fatalf("bag[1] = %+v, want moved item at the end", updated.BagItems[1])
	}
	if updated.BagItems[0].MakeIndex != 22 || updated.BagItems[1].MakeIndex != 11 {
		t.Fatalf("bag makeindexes = [%d %d], want [22 11]", updated.BagItems[0].MakeIndex, updated.BagItems[1].MakeIndex)
	}
}

func TestDropItemMovesOneItemToGround(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	ch.BagItems = []storage.UserItem{
		{ItemID: testHPItemID, MakeIndex: 1},
		{ItemID: testHPItemID, MakeIndex: 2},
		{ItemID: testHPItemID, MakeIndex: 3},
	}
	updated, drop, err := w.DropItem(ch, testHPItemID)
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
	updated, drop, err := w.DropItemCount(ch, testHPItemID, 2)
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
	updated, drop, err := w.DropItemCountByBagIndex(ch, 11, testHPItemID, 2)
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

	updated, drop, err := w.DropItemCountByBagIndex(ch, 21, testMPItemID, 1)
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
	ch, drop, err := w.DropItem(ch, testHPItemID)
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
	if _, _, err := w.UseItem(ch, testWeaponID); err == nil {
		t.Fatalf("UseItem() expected error for wearable item")
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
	updated, _, err := w.UseItem(ch, testHPItemID)
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
	mapID, x, y := w.DefaultSpawn()
	ch, err := w.CreateCharacter("test", "tester", "warrior", mapID, x, y)
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

	normalized, changed := w.NormalizeCharacterBagItems(ch)
	if !changed {
		t.Fatal("NormalizeCharacterBagItems() changed = false, want true")
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
	monsters, _ := w.Snapshot(ch.MapID)
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
	mapID, x, y := w.DefaultSpawn()
	ch, err := w.CreateCharacter("test", "tester", "warrior", mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	monsters, _ := w.Snapshot(ch.MapID)
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
	mapID, x, y := w.DefaultSpawn()
	ch, err := w.CreateCharacter("test", "tester", "warrior", mapID, x, y)
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
