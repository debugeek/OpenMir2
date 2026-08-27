package world

import (
	"sort"
	"strconv"

	"openmir2/internal/data"
	"openmir2/internal/storage"
)

func (w *World) RandomNewCharacterSpawn() (string, int, int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	sp, ok := w.randomNewCharacterStartPointLocked()
	if !ok {
		return "0", 0, 0
	}
	return sp.MapID, sp.X, sp.Y
}

func (w *World) SyncCharacterHomeFromStartPoint(ch storage.Character) (storage.Character, bool, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.syncCharacterHomeFromStartPointLocked(&ch) {
		return ch, false, nil
	}
	if err := w.store.SaveCharacter(ch); err != nil {
		return ch, false, err
	}
	return ch, true, nil
}

func (w *World) randomNewCharacterStartPointLocked() (data.StdStartPoint, bool) {
	all := w.allStartPointsLocked()
	if len(all) == 0 {
		return data.StdStartPoint{}, false
	}
	limit := 2
	if len(all) < limit {
		limit = len(all)
	}
	return all[w.rand.Intn(limit)], true
}

func (w *World) syncCharacterHomeFromStartPointLocked(ch *storage.Character) bool {
	for _, sp := range w.allStartPointsLocked() {
		if sp.MapID != ch.MapID {
			continue
		}
		if abs(ch.X-sp.X) > 50 || abs(ch.Y-sp.Y) > 50 {
			continue
		}
		if ch.HomeMap == sp.MapID && ch.HomeX == sp.X && ch.HomeY == sp.Y {
			return false
		}
		ch.HomeMap = sp.MapID
		ch.HomeX = sp.X
		ch.HomeY = sp.Y
		return true
	}
	return false
}

func (w *World) allStartPointsLocked() []data.StdStartPoint {
	mapIDs := make([]string, 0, len(w.data.Maps))
	for mapID := range w.data.Maps {
		mapIDs = append(mapIDs, mapID)
	}
	sort.Slice(mapIDs, func(i, j int) bool {
		return compareMapIDs(mapIDs[i], mapIDs[j]) < 0
	})
	points := make([]data.StdStartPoint, 0)
	for _, mapID := range mapIDs {
		mp := w.data.Maps[mapID]
		for _, sp := range mp.StartPoints {
			sp.MapID = mapID
			points = append(points, sp)
		}
	}
	return points
}

func compareMapIDs(a, b string) int {
	ai, aErr := strconv.Atoi(a)
	bi, bErr := strconv.Atoi(b)
	switch {
	case aErr == nil && bErr == nil:
		switch {
		case ai < bi:
			return -1
		case ai > bi:
			return 1
		default:
			return 0
		}
	case aErr == nil:
		return -1
	case bErr == nil:
		return 1
	default:
		switch {
		case a < b:
			return -1
		case a > b:
			return 1
		default:
			return 0
		}
	}
}
