package world

import "sort"

// DefaultSpawn returns the map with the lexicographically smallest ID and
// its configured spawn point. Iterating w.data.Maps directly would pick a
// different map on every call — Go map iteration order is randomized — so
// this is deterministic on purpose: newly created characters must always
// land on the same map.
func (w *World) DefaultSpawn() (string, int, int) {
	if len(w.data.Maps) == 0 {
		return "0", 0, 0
	}
	ids := make([]string, 0, len(w.data.Maps))
	for id := range w.data.Maps {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	mp := w.data.Maps[ids[0]]
	return mp.ID, mp.SpawnX, mp.SpawnY
}

func (w *World) MapName(mapID string) string {
	w.mu.Lock()
	defer w.mu.Unlock()
	if mp, ok := w.data.Maps[mapID]; ok {
		return mp.Name
	}
	return mapID
}

func (w *World) MapLight(mapID string) int {
	w.mu.Lock()
	defer w.mu.Unlock()
	if mp, ok := w.data.Maps[mapID]; ok {
		return mp.Light
	}
	return 0
}

func (w *World) SetMapLight(mapID string, light int) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	mp, ok := w.data.Maps[mapID]
	if !ok {
		return false
	}
	mp.Light = light
	w.data.Maps[mapID] = mp
	return true
}
