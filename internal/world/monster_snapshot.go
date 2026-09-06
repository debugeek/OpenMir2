package world

import (
	"sort"
	"time"
)

func (w *World) MonsterSnapshot(id string) (Monster, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	mon, ok := w.monsters[id]
	if !ok || mon == nil || !mon.Alive {
		return Monster{}, false
	}
	return *mon, true
}

func (w *World) MonsterTracked(id string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	_, ok := w.monsters[id]
	return ok
}

func (w *World) SnapshotAround(mapID string, x, y, viewRange int) ([]Monster, []GroundDrop) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.respawnLocked(time.Now())
	monsters := []Monster{}
	for _, mon := range w.monsters {
		if mon.MapID == mapID && mon.Alive && !mon.Hidden && abs(mon.X-x) <= viewRange && abs(mon.Y-y) <= viewRange {
			monsters = append(monsters, *mon)
		}
	}
	drops := []GroundDrop{}
	for _, drop := range w.drops {
		if drop.MapID == mapID && abs(drop.X-x) <= viewRange && abs(drop.Y-y) <= viewRange {
			drops = append(drops, drop)
		}
	}
	sort.Slice(monsters, func(i, j int) bool { return idSeq(monsters[i].ID) < idSeq(monsters[j].ID) })
	sort.Slice(drops, func(i, j int) bool { return idSeq(drops[i].ID) < idSeq(drops[j].ID) })
	return monsters, drops
}
