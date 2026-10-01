package world

import (
	"testing"
	"time"

	"openmir2/internal/data"
)

func lifecycleWorld() *World {
	return New(data.StdBundle{
		Maps:     map[string]data.StdMap{"m": {ID: "m", Width: 20, Height: 20, MonsterSpawnRate: 10}},
		Monsters: map[string]data.StdMonster{"rat": {ID: "rat", Name: "rat", HP: 10, MP: 1, MaxAttack: 1, ViewRange: 6}},
		Spawns:   []data.StdSpawn{{ID: "s", MapID: "m", MonsterID: "rat", X: 5, Y: 5, Count: 1, Range: 0, RespawnSeconds: 1}},
	}, nil)
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
	w.mu.Unlock()
	if _, err := w.Tick(nil, now); err != nil {
		t.Fatal(err)
	}
	if !old.Ghost {
		t.Fatal("expected dead monster to become ghost")
	}
	if _, err := w.Tick(nil, now.Add(5*time.Minute+time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, ok := w.monsters[old.ID]; ok {
		t.Fatal("expected ghost monster to be removed")
	}
}
