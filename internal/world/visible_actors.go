package world

import (
	"sort"
	"time"

	"openmir2/internal/storage"
)

func characterVisibleActorKey(id string) string {
	return "character:" + id
}

func monsterVisibleActorKey(id string) string {
	return "monster:" + id
}

type visibleActorEntry struct {
	key         string
	mapID       string
	X           int
	Y           int
	ObjectOrder uint64
}

func (w *World) ensureMonsterCellRegistryLocked() {
	if len(w.monsterCellByID) == len(w.monsters) {
		return
	}
	w.rebuildMonsterCellRegistryLocked()
}

func (w *World) rebuildMonsterCellRegistryLocked() {
	w.monsterCells = map[monsterPosition]map[string]struct{}{}
	w.monsterCellByID = map[string]monsterPosition{}
	for _, mon := range w.monsters {
		w.indexMonsterCellLocked(mon)
	}
}

func (w *World) visibleActorKeysLocked(observerID, mapID string, x, y, viewRange int, players map[string]storage.Character) []string {
	if observer, ok := w.monsters[observerID]; ok && observer != nil && !observer.NextVisibleSearchAt.IsZero() {
		if cached, cachedOK := w.visibleActors[observerID]; cachedOK {
			return append([]string(nil), cached...)
		}
		return nil
	}
	w.ensureMonsterCellRegistryLocked()
	if observer, ok := w.monsters[observerID]; ok && observer != nil {
		cell := monsterPosition{MapID: observer.MapID, X: observer.X, Y: observer.Y}
		if indexed, indexedOK := w.monsterCellByID[observerID]; !indexedOK || indexed != cell {
			w.monsterCells = map[monsterPosition]map[string]struct{}{}
			w.monsterCellByID = map[string]monsterPosition{}
			for _, mon := range w.monsters {
				w.indexMonsterCellLocked(mon)
			}
		}
	}
	scanRange := viewRange
	allowUnownedMonsters := true
	if observer, ok := w.monsters[observerID]; ok && observer != nil && observer.ViewRange > 0 {
		scanRange = observer.ViewRange
	}
	if observer, ok := w.monsters[observerID]; ok && observer != nil {
		allowUnownedMonsters = observer.Race < 50 || observer.MasterID != "" || !observer.CrazyUntil.IsZero() || observer.WantRefMsg
	}
	current := make(map[string]visibleActorEntry)
	for _, ch := range players {
		if ch.ID == "" || ch.MapID != mapID || ch.Ghost || ch.ObserverMode {
			continue
		}
		if abs(ch.X-x) > scanRange || abs(ch.Y-y) > scanRange {
			continue
		}
		entry := visibleActorEntry{key: characterVisibleActorKey(ch.ID), mapID: ch.MapID, X: ch.X, Y: ch.Y, ObjectOrder: ch.ObjectOrder}
		current[entry.key] = entry
	}
	for xx := x - scanRange; xx <= x+scanRange; xx++ {
		for yy := y - scanRange; yy <= y+scanRange; yy++ {
			cell := monsterPosition{MapID: mapID, X: xx, Y: yy}
			for id := range w.monsterCells[cell] {
				mon := w.monsters[id]
				if mon == nil || mon.ID == "" || mon.MapID != mapID || mon.Ghost || mon.FixedHideMode {
					continue
				}
				if mon.ID != observerID && mon.MasterID == "" && !allowUnownedMonsters {
					continue
				}
				if mon.ID != observerID && mon.MasterID != "" && !allowUnownedMonsters && (abs(mon.X-x) > 3 || abs(mon.Y-y) > 3) {
					continue
				}
				entry := visibleActorEntry{key: monsterVisibleActorKey(mon.ID), mapID: mon.MapID, X: mon.X, Y: mon.Y, ObjectOrder: mon.ObjectOrder}
				current[entry.key] = entry
			}
		}
	}

	byCell := make(map[monsterPosition][]visibleActorEntry)
	for _, entry := range current {
		cell := monsterPosition{MapID: entry.mapID, X: entry.X, Y: entry.Y}
		byCell[cell] = append(byCell[cell], entry)
	}
	for cell := range byCell {
		entries := byCell[cell]
		sort.SliceStable(entries, func(i, j int) bool {
			if entries[i].ObjectOrder != 0 || entries[j].ObjectOrder != 0 {
				if entries[i].ObjectOrder == 0 {
					return false
				}
				if entries[j].ObjectOrder == 0 {
					return true
				}
				if entries[i].ObjectOrder != entries[j].ObjectOrder {
					return entries[i].ObjectOrder < entries[j].ObjectOrder
				}
			}
			return entries[i].key < entries[j].key
		})
		byCell[cell] = entries
	}

	scanned := make([]string, 0, len(current))
	for xx := x - scanRange; xx <= x+scanRange; xx++ {
		for yy := y - scanRange; yy <= y+scanRange; yy++ {
			cell := monsterPosition{MapID: mapID, X: xx, Y: yy}
			for _, entry := range byCell[cell] {
				scanned = append(scanned, entry.key)
			}
		}
	}

	previous := w.visibleActors[observerID]
	seen := make(map[string]struct{}, len(scanned))
	ordered := make([]string, 0, len(scanned))
	for _, key := range previous {
		if _, ok := current[key]; !ok {
			continue
		}
		ordered = append(ordered, key)
		seen[key] = struct{}{}
	}
	for _, key := range scanned {
		if _, ok := seen[key]; ok {
			continue
		}
		ordered = append(ordered, key)
		seen[key] = struct{}{}
	}
	w.visibleActors[observerID] = ordered
	return ordered
}

func (w *World) monsterVisibleSearchDelayMSLocked(mon *Monster) int {
	base := 3000
	span := 2000
	switch mon.Race {
	case 110, 111, 112, 115, 120:
		base = 2000
	}
	switch mon.Race {
	case 53, 81, 82, 83, 84, 86, 88, 89, 90, 91, 92, 94, 97, 100, 101, 102, 105, 106, 118, 119, 130, 131, 133, 134, 200, 201, 206, 208, 209, 210, 214, 215:
		base = 1500
		span = 1500
	case 85, 95, 96, 103, 107, 116, 117, 132:
		base = 2500
		span = 1500
	}
	if mon.Race == 87 || mon.Race == 93 || mon.Race == 104 {
		return 3000
	}
	if mon.Race == 92 {
		base = 500
	}
	return base + w.rand.Intn(span)
}

func monsterVisibleSearchStartsImmediately(mon *Monster) bool {
	return mon != nil && (mon.Race == 116 || mon.Race == 117)
}

func (w *World) refreshMonsterVisibleActorsLocked(mon *Monster, players map[string]storage.Character) {
	if mon == nil {
		return
	}
	now := w.actionNow
	if now.IsZero() {
		now = time.Now()
	}
	mon.NextVisibleSearchAt = time.Time{}
	delete(w.visibleActors, mon.ID)
	w.visibleActorKeysLocked(mon.ID, mon.MapID, mon.X, mon.Y, mon.ViewRange, players)
	if mon.VisibleSearchIntervalMS <= 0 {
		mon.VisibleSearchIntervalMS = 1500
	}
	mon.NextVisibleSearchAt = now.Add(time.Duration(mon.VisibleSearchIntervalMS) * time.Millisecond)
}
