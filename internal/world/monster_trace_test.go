package world

import (
	"math/rand"
	"path/filepath"
	"testing"
	"time"

	"openmir2/internal/config"
	"openmir2/internal/storage"
)

func newMonsterTraceWorld(t *testing.T, seed int64) (*World, *Monster) {
	t.Helper()
	bundle := loadTestBundle(t)
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	w := New(bundle, store, config.DefaultGameplay())
	w.mu.Lock()
	defer w.mu.Unlock()
	w.monsters = map[string]*Monster{}
	w.occupied = map[monsterPosition]string{}
	mapData := bundle.Maps[testMapID]
	x, y := startCoordsForMap(t, bundle, testMapID)
	for dx := 0; dx < 20; dx++ {
		if mapData.Walkable(x+dx, y) && mapData.Walkable(x+dx+1, y) && mapData.Walkable(x+dx+2, y) {
			x += dx
			break
		}
	}
	mon := &Monster{
		ID: "trace-monster", Name: "鹿", TemplateID: "鹿", MapID: testMapID,
		X: x, Y: y, Dir: 2, ViewRange: 10, LeashRange: 20,
		SearchNoTargetMS: 1, SearchHasTargetMS: 1, HP: 100, MaxHP: 100,
		Alive: true, WalkSpeedMS: 1, WalkStep: 1, NextSearchAt: time.Unix(9, 0),
	}
	w.monsters[mon.ID] = mon
	w.occupyMonsterLocked(mon)
	w.rand = rand.New(rand.NewSource(seed))
	w.monsterTraceEnabled = true
	return w, mon
}

func TestMonsterTraceCapturesTickStateAndRandomCalls(t *testing.T) {
	w, mon := newMonsterTraceWorld(t, 11)
	result, err := w.Tick(nil, time.Unix(10, 0))
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if len(result.MonsterTraces) != 1 {
		t.Fatalf("MonsterTraces = %d, want 1", len(result.MonsterTraces))
	}
	trace := result.MonsterTraces[0]
	if trace.Tick != 1 || trace.NowMS != 10000 || trace.MonsterID != mon.ID {
		t.Fatalf("trace identity = %+v", trace)
	}
	if trace.StateBefore.X != mon.X-1 || trace.StateAfter.X != mon.X {
		t.Fatalf("trace coordinates = before %+v after %+v", trace.StateBefore, trace.StateAfter)
	}
	if trace.Decision != "wander.walk" || len(trace.Actions) != 1 || trace.Actions[0].Kind != MonsterActionWalk {
		t.Fatalf("trace decision = %q actions = %+v", trace.Decision, trace.Actions)
	}
	if len(trace.Random) == 0 || trace.Random[0].Label != "wander.roll" {
		t.Fatalf("trace random calls = %+v", trace.Random)
	}
	if trace.Actions[0].Status != MonsterStatus(*mon, time.Unix(10, 0)) {
		t.Fatalf("action status = %d, want tick-time status", trace.Actions[0].Status)
	}
}

func TestMonsterTraceChaseIsDeterministicAcrossWorlds(t *testing.T) {
	makeRun := func() MonsterTickTrace {
		w, mon := newMonsterTraceWorld(t, 23)
		target := storage.Character{ID: "trace-target", MapID: testMapID, X: mon.X - 2, Y: mon.Y, HP: 100, MaxHP: 100}
		result, err := w.Tick([]PlayerSnapshot{{Character: target}}, time.Unix(10, 0))
		if err != nil {
			t.Fatalf("Tick() error = %v", err)
		}
		if len(result.MonsterTraces) != 1 {
			t.Fatalf("MonsterTraces = %d, want 1", len(result.MonsterTraces))
		}
		return result.MonsterTraces[0]
	}
	first, second := makeRun(), makeRun()
	if first.Decision != "chase" || second.Decision != "chase" {
		t.Fatalf("decisions = %q, %q; want chase", first.Decision, second.Decision)
	}
	if len(first.Random) != len(second.Random) || len(first.Actions) != len(second.Actions) || first.StateAfter != second.StateAfter {
		t.Fatalf("non-deterministic traces: first=%+v second=%+v", first, second)
	}
	for i := range first.Random {
		if first.Random[i] != second.Random[i] {
			t.Fatalf("random trace %d differs: %+v vs %+v", i, first.Random[i], second.Random[i])
		}
	}
}
