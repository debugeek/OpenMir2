package world

import (
	"fmt"

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
	if mapID == "" {
		mapID, x, y = w.DefaultSpawn()
	}
	mp, ok := w.data.Maps[mapID]
	if !ok {
		return ch, fmt.Errorf("map %s not found", mapID)
	}
	return core.TeleportTo(ch, mp, x, y, w.rand)
}

func (w *World) homeTeleportRandomCharacterLocked(ch storage.Character) (storage.Character, error) {
	mapID := ch.HomeMap
	if mapID == "" {
		mapID = ch.MapID
	}
	if mapID == "" {
		mapID, _, _ = w.DefaultSpawn()
	}
	mp, ok := w.data.Maps[mapID]
	if !ok {
		return ch, fmt.Errorf("map %s not found", mapID)
	}
	return core.TeleportRandomInMap(ch, mp, w.rand)
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
