package world

import (
	"fmt"
	"math/rand"
	"time"

	"openmir2/internal/data"
	"openmir2/internal/storage"
	"openmir2/internal/world/core"
)

func (w *World) Teleport(ch storage.Character, mapID string, x, y int) (storage.Character, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.teleportLocked(ch, mapID, x, y)
}

func (w *World) TeleportRandomInMap(ch storage.Character, mapID string) (storage.Character, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.teleportRandomInMapLocked(ch, mapID)
}

func (w *World) TeleportRandomInCurrentMap(ch storage.Character) (storage.Character, error) {
	return w.TeleportRandomInMap(ch, ch.MapID)
}

func (w *World) teleportLocked(ch storage.Character, mapID string, x, y int) (storage.Character, error) {
	mp, ok := w.data.Maps[mapID]
	if !ok {
		return ch, fmt.Errorf("map %s not found", mapID)
	}
	next, err := core.TeleportTo(ch, mp, x, y, w.rand)
	if err != nil {
		return ch, err
	}
	w.syncCharacterHomeFromStartPointLocked(&next)
	w.refreshCharacterObjectOrderLocked(&next)
	next.MapMoveAt = time.Now().UnixNano()
	return next, w.store.SaveCharacter(next)
}

func (w *World) teleportRandomInMapLocked(ch storage.Character, mapID string) (storage.Character, error) {
	mp, ok := w.data.Maps[mapID]
	if !ok {
		return ch, fmt.Errorf("map %s not found", mapID)
	}
	next, err := core.TeleportRandomInMap(ch, mp, w.rand)
	if err != nil {
		return ch, err
	}
	w.syncCharacterHomeFromStartPointLocked(&next)
	w.refreshCharacterObjectOrderLocked(&next)
	next.MapMoveAt = time.Now().UnixNano()
	return next, w.store.SaveCharacter(next)
}

func (w *World) itemWeight(itemID string) int {
	item, ok := w.data.Items[itemID]
	if !ok {
		return 0
	}
	return item.Weight
}

func (w *World) homeTeleportCharacterLocked(ch storage.Character) (storage.Character, error) {
	mapID := ch.HomeMap
	if mapID == "" {
		mapID = ch.MapID
	}
	x, y := ch.HomeX, ch.HomeY
	mp, ok := w.data.Maps[mapID]
	if !ok {
		return ch, fmt.Errorf("map %s not found", mapID)
	}
	next, err := core.TeleportTo(ch, mp, x, y, w.rand)
	if err != nil {
		return ch, err
	}
	w.refreshCharacterObjectOrderLocked(&next)
	next.MapMoveAt = time.Now().UnixNano()
	return next, nil
}

func (w *World) homeTeleportRandomCharacterLocked(ch storage.Character) (storage.Character, error) {
	mapID := ch.HomeMap
	if mapID == "" {
		mapID = ch.MapID
	}
	if mapID == "" {
		return ch, fmt.Errorf("character has no home map")
	}
	mp, ok := w.data.Maps[mapID]
	if !ok {
		return ch, fmt.Errorf("map %s not found", mapID)
	}
	next, err := randomMoveToHomeMap(ch, mp, w.rand)
	if err != nil {
		return ch, err
	}
	w.refreshCharacterObjectOrderLocked(&next)
	next.MapMoveAt = time.Now().UnixNano()
	return next, nil
}

func randomMoveToHomeMap(ch storage.Character, mp data.StdMap, rng *rand.Rand) (storage.Character, error) {
	if rng == nil {
		return ch, fmt.Errorf("teleport rng is nil")
	}
	if mp.Width <= 0 || mp.Height <= 0 {
		return ch, fmt.Errorf("map %s is invalid", mp.ID)
	}
	edge := 50
	if mp.Height < 150 {
		edge = 20
		if mp.Height < 30 {
			edge = 2
		}
	}
	maxX := mp.Width - edge - 1
	maxY := mp.Height - edge - 1
	if maxX <= edge || maxY <= edge {
		return core.TeleportRandomInMap(ch, mp, rng)
	}
	x := rng.Intn(maxX) + edge
	y := rng.Intn(maxY) + edge
	for attempt := 0; attempt < 201; attempt++ {
		if mp.Walkable(x, y) {
			ch.MapID = mp.ID
			ch.X = x
			ch.Y = y
			return ch, nil
		}
		if x < maxX {
			x++
		} else {
			x = rng.Intn(mp.Width)
			if y < maxY {
				y++
			} else {
				y = rng.Intn(mp.Height)
			}
		}
	}
	return ch, fmt.Errorf("no available teleport position")
}

func (w *World) ReviveCharacterAtHome(ch storage.Character) (storage.Character, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	next, err := w.homeTeleportCharacterLocked(ch)
	if err != nil {
		return ch, err
	}
	if next.MaxHP > 0 || next.MaxMP > 0 {
		next = core.SetVitals(next, next.MaxHP, next.MaxMP).Character
	}
	if err := w.store.SaveCharacter(next); err != nil {
		return ch, err
	}
	return next, nil
}
