package world

import (
	"time"

	"openmir2/internal/data"
	"openmir2/internal/storage"
)

const doorOpenDuration = 5 * time.Second

type DoorChange struct {
	MapID string
	X     int
	Y     int
}

func (w *World) doorGroupAtLocked(mapID string, x, y int) (int, bool) {
	mp, ok := w.data.Maps[mapID]
	if !ok {
		return 0, false
	}
	for _, door := range mp.Doors {
		if door.X == x && door.Y == y {
			return door.Group, true
		}
	}
	return 0, false
}

func (w *World) doorStateKeyAtLocked(mapID string, x, y int) (doorStateKey, bool) {
	mp, ok := w.data.Maps[mapID]
	if !ok {
		return doorStateKey{}, false
	}
	group, ok := w.doorGroupAtLocked(mapID, x, y)
	if !ok {
		return doorStateKey{}, false
	}
	component := []data.StdMapDoor{{X: x, Y: y, Group: group}}
	for changed := true; changed; {
		changed = false
		for _, door := range mp.Doors {
			if door.Group != group {
				continue
			}
			for _, member := range component {
				if abs(door.X-member.X) <= 10 && abs(door.Y-member.Y) <= 10 {
					found := false
					for _, existing := range component {
						if existing.X == door.X && existing.Y == door.Y {
							found = true
							break
						}
					}
					if !found {
						component = append(component, door)
						changed = true
					}
					break
				}
			}
		}
	}
	anchor := component[0]
	for _, door := range component[1:] {
		if door.X < anchor.X || door.X == anchor.X && door.Y < anchor.Y {
			anchor = door
		}
	}
	return doorStateKey{MapID: mapID, Group: group, X: anchor.X, Y: anchor.Y}, true
}

func (w *World) doorOpenLocked(mapID string, x, y int, now time.Time) bool {
	key, ok := w.doorStateKeyAtLocked(mapID, x, y)
	if !ok {
		return false
	}
	state, ok := w.doorStates[key]
	if !ok || now.Sub(state.OpenedAt) > doorOpenDuration {
		if ok {
			delete(w.doorStates, key)
		}
		return false
	}
	return true
}

func (w *World) DoorsOpenAround(mapID string, x, y int, now time.Time) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.doorsOpenAroundLocked(mapID, x, y, now)
}

func (w *World) doorsOpenAroundLocked(mapID string, x, y int, now time.Time) bool {
	mp, ok := w.data.Maps[mapID]
	if !ok {
		return true
	}
	for _, door := range mp.Doors {
		if abs(door.X-x) <= 1 && abs(door.Y-y) <= 1 && !w.doorOpenLocked(mapID, door.X, door.Y, now) {
			return false
		}
	}
	return true
}

func (w *World) OpenDoor(mapID string, x, y int, now time.Time) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.openDoorLocked(mapID, x, y, now, nil)
}

func (w *World) OpenDoorForCharacter(ch storage.Character, x, y int, now time.Time) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.openDoorLocked(ch.MapID, x, y, now, &ch)
}

func (w *World) openDoorLocked(mapID string, x, y int, now time.Time, ch *storage.Character) bool {
	key, ok := w.doorStateKeyAtLocked(mapID, x, y)
	if !ok {
		return false
	}
	if ch != nil && w.castlePalaceDoorBlockedLocked(mapID, key.Group, *ch) {
		return false
	}
	if w.doorOpenLocked(mapID, x, y, now) {
		return false
	}
	w.doorStates[key] = doorState{OpenedAt: now}
	return true
}

func (w *World) castlePalaceDoorBlockedLocked(mapID string, group int, ch storage.Character) bool {
	if mapID != w.data.Castle.CastleMap || (w.data.Castle.PalaceDoorX == 0 && w.data.Castle.PalaceDoorY == 0) {
		return false
	}
	palaceGroup, ok := w.castlePalaceDoorGroupLocked(mapID)
	if !ok || group != palaceGroup {
		return false
	}
	castle, ok := w.castleSnapshotLocked(w.data.Castle.ID)
	if !ok {
		return false
	}
	if ch.GuildID == castle.OwnerGuildID && ch.GuildRank == 1 {
		return false
	}
	for i, wall := range w.data.Castle.Walls {
		if i >= len(castle.WallHP) {
			break
		}
		if castle.WallHP[i] > 0 {
			continue
		}
		if ch.X == wall.X && ch.Y == wall.Y {
			return false
		}
	}
	return true
}

func (w *World) castlePalaceDoorGroupLocked(mapID string) (int, bool) {
	mp, ok := w.data.Maps[mapID]
	if !ok {
		return 0, false
	}
	for _, door := range mp.Doors {
		if abs(door.X-w.data.Castle.PalaceDoorX) <= 3 && abs(door.Y-w.data.Castle.PalaceDoorY) <= 3 {
			return door.Group, true
		}
	}
	return 0, false
}

func (w *World) ExpireDoors(now time.Time) []DoorChange {
	w.mu.Lock()
	defer w.mu.Unlock()
	changes := make([]DoorChange, 0)
	for key, state := range w.doorStates {
		if !now.After(state.OpenedAt.Add(doorOpenDuration)) {
			continue
		}
		delete(w.doorStates, key)
		if _, ok := w.data.Maps[key.MapID]; ok {
			changes = append(changes, DoorChange{MapID: key.MapID, X: key.X, Y: key.Y})
		}
	}
	return changes
}
