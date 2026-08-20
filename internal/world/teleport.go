package world

import (
	"fmt"

	"openmir2/internal/storage"
)

func (w *World) Teleport(ch storage.Character, mapID string, x, y int) (storage.Character, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	next, err := w.teleportCharacterLocked(ch, mapID, x, y)
	if err != nil {
		return ch, err
	}
	return next, w.store.SaveCharacter(next)
}

func (w *World) TeleportRandomInMap(ch storage.Character, mapID string) (storage.Character, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	next, err := w.teleportRandomInMapLocked(ch, mapID)
	if err != nil {
		return ch, err
	}
	return next, w.store.SaveCharacter(next)
}

func (w *World) TeleportRandomInCurrentMap(ch storage.Character) (storage.Character, error) {
	return w.TeleportRandomInMap(ch, ch.MapID)
}

func (w *World) itemWeight(itemID string) int {
	item, ok := w.data.Items[itemID]
	if !ok {
		return 0
	}
	return item.Weight
}

func (w *World) teleportCharacterLocked(ch storage.Character, mapID string, x, y int) (storage.Character, error) {
	mp, ok := w.data.Maps[mapID]
	if !ok {
		return ch, fmt.Errorf("map %s not found", mapID)
	}
	if !mp.Walkable(x, y) {
		if px, py, ok := w.findRandomSpawnPositionLocked(mapID, mp.SpawnX, mp.SpawnY, 4, ""); ok {
			x, y = px, py
		} else {
			return ch, fmt.Errorf("target coordinate is blocked")
		}
	}
	ch.MapID = mapID
	ch.X = x
	ch.Y = y
	return ch, nil
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
	return w.teleportCharacterLocked(ch, mapID, x, y)
}

func (w *World) homeTeleportRandomCharacterLocked(ch storage.Character) (storage.Character, error) {
	mapID := ch.HomeMap
	if mapID == "" {
		mapID = ch.MapID
	}
	if mapID == "" {
		mapID, _, _ = w.DefaultSpawn()
	}
	return w.teleportRandomInMapLocked(ch, mapID)
}

func (w *World) teleportRandomInMapLocked(ch storage.Character, mapID string) (storage.Character, error) {
	mp, ok := w.data.Maps[mapID]
	if !ok {
		return ch, fmt.Errorf("map %s not found", mapID)
	}
	positions := make([][2]int, 0, mp.Width*mp.Height)
	for y := 0; y < mp.Height; y++ {
		for x := 0; x < mp.Width; x++ {
			if mp.Walkable(x, y) {
				positions = append(positions, [2]int{x, y})
			}
		}
	}
	if len(positions) == 0 {
		return ch, fmt.Errorf("no available teleport position")
	}
	pick := positions[w.rand.Intn(len(positions))]
	if len(positions) > 1 && pick[0] == ch.X && pick[1] == ch.Y {
		pick = positions[(w.rand.Intn(len(positions)-1)+1)%len(positions)]
	}
	ch.MapID = mapID
	ch.X = pick[0]
	ch.Y = pick[1]
	return ch, nil
}
