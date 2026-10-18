package world

import (
	"sort"
	"time"
)

func (w *World) MonsterSnapshot(id string) (Monster, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	mon, ok := w.monsters[id]
	if !ok || mon == nil || !mon.Alive || mon.Ghost {
		return Monster{}, false
	}
	return *mon, true
}

func (w *World) MonsterSnapshotForDelayedMessage(id string) (Monster, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	mon, ok := w.monsters[id]
	if !ok || mon == nil || mon.Ghost {
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

func (w *World) MonsterActorIDByID(id string) int32 {
	w.mu.Lock()
	defer w.mu.Unlock()
	if mon, ok := w.monsters[id]; ok && mon != nil {
		return MonsterActorID(*mon)
	}
	return MonsterActorID(Monster{ID: id})
}

func (w *World) MonsterSnapshotByActorID(actorID int32) (Monster, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	var found Monster
	foundOK := false
	for _, mon := range w.monsters {
		if mon == nil || !mon.Alive || mon.Ghost || MonsterActorID(*mon) != actorID {
			continue
		}
		if !foundOK || monsterSnapshotBefore(*mon, found) {
			found = *mon
			foundOK = true
		}
	}
	return found, foundOK
}

func (w *World) MonsterQuerySnapshotByActorID(actorID int32) (Monster, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	var found Monster
	foundOK := false
	for _, mon := range w.monsters {
		if mon == nil || MonsterActorID(*mon) != actorID {
			continue
		}
		if !foundOK || monsterSnapshotBefore(*mon, found) {
			found = *mon
			foundOK = true
		}
	}
	return found, foundOK
}

func monsterSnapshotBefore(candidate, current Monster) bool {
	if candidate.ObjectOrder != 0 || current.ObjectOrder != 0 {
		if candidate.ObjectOrder == 0 {
			return false
		}
		if current.ObjectOrder == 0 {
			return true
		}
		if candidate.ObjectOrder != current.ObjectOrder {
			return candidate.ObjectOrder < current.ObjectOrder
		}
	}
	return candidate.ID < current.ID
}

func (w *World) SnapshotAround(mapID string, x, y, viewRange int) ([]Monster, []GroundDrop) {
	return w.snapshotAround(mapID, x, y, viewRange, false, false)
}

func (w *World) SnapshotAroundForVisibility(mapID string, x, y, viewRange int) ([]Monster, []GroundDrop) {
	return w.snapshotAround(mapID, x, y, viewRange, true, true)
}

func (w *World) snapshotAround(mapID string, x, y, viewRange int, includeDead, includeHidden bool) ([]Monster, []GroundDrop) {
	w.mu.Lock()
	defer w.mu.Unlock()
	monsters := []Monster{}
	for _, mon := range w.monsters {
		if mon.MapID == mapID && (includeDead || mon.Alive) && !mon.Ghost && (includeHidden || !mon.Hidden) && !mon.FixedHideMode && abs(mon.X-x) <= viewRange && abs(mon.Y-y) <= viewRange {
			monsters = append(monsters, *mon)
		}
	}
	drops := []GroundDrop{}
	now := time.Now()
	for id, drop := range w.drops {
		if drop.MapID != mapID || abs(drop.X-x) > viewRange || abs(drop.Y-y) > viewRange {
			continue
		}
		if !drop.CreatedAt.IsZero() && now.After(drop.CreatedAt.Add(time.Hour)) {
			delete(w.drops, id)
			continue
		}
		if !drop.PickupAt.IsZero() && now.After(drop.PickupAt) && (drop.OwnerID != "" || drop.OwnerGroupID != "") {
			drop.OwnerID = ""
			drop.OwnerGroupID = ""
			w.drops[id] = drop
		} else {
			changed := false
			if drop.OwnerID != "" {
				if owner, ok := w.disconnectedCharacters[drop.OwnerID]; ok && owner.Character.Ghost {
					drop.OwnerID = ""
					changed = true
				}
			}
			if drop.OwnerGroupID != "" {
				if owner, ok := w.disconnectedCharacters[drop.OwnerGroupID]; ok && owner.Character.Ghost {
					drop.OwnerGroupID = ""
					changed = true
				}
			}
			if changed {
				w.drops[id] = drop
			}
		}
		drops = append(drops, drop)
	}
	sort.SliceStable(monsters, func(i, j int) bool {
		if monsters[i].X != monsters[j].X {
			return monsters[i].X < monsters[j].X
		}
		if monsters[i].Y != monsters[j].Y {
			return monsters[i].Y < monsters[j].Y
		}
		return monsterSnapshotBefore(monsters[i], monsters[j])
	})
	sort.SliceStable(drops, func(i, j int) bool {
		if drops[i].X != drops[j].X {
			return drops[i].X < drops[j].X
		}
		if drops[i].Y != drops[j].Y {
			return drops[i].Y < drops[j].Y
		}
		return groundDropBefore(drops[i], drops[j])
	})
	return monsters, drops
}
