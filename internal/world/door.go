package world

import "time"

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

func (w *World) doorOpenLocked(mapID string, group int, now time.Time) bool {
	state, ok := w.doorStates[doorStateKey{MapID: mapID, Group: group}]
	if !ok || now.Sub(state.OpenedAt) >= doorOpenDuration {
		if ok {
			delete(w.doorStates, doorStateKey{MapID: mapID, Group: group})
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
		if abs(door.X-x) <= 1 && abs(door.Y-y) <= 1 && !w.doorOpenLocked(mapID, door.Group, now) {
			return false
		}
	}
	return true
}

func (w *World) OpenDoor(mapID string, x, y int, now time.Time) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	group, ok := w.doorGroupAtLocked(mapID, x, y)
	if !ok {
		return false
	}
	if w.doorOpenLocked(mapID, group, now) {
		return false
	}
	w.doorStates[doorStateKey{MapID: mapID, Group: group}] = doorState{OpenedAt: now}
	return true
}

func (w *World) ExpireDoors(now time.Time) []DoorChange {
	w.mu.Lock()
	defer w.mu.Unlock()
	changes := make([]DoorChange, 0)
	for key, state := range w.doorStates {
		if now.Sub(state.OpenedAt) < doorOpenDuration {
			continue
		}
		delete(w.doorStates, key)
		if mp, ok := w.data.Maps[key.MapID]; ok {
			for _, door := range mp.Doors {
				if door.Group == key.Group {
					changes = append(changes, DoorChange{MapID: key.MapID, X: door.X, Y: door.Y})
					break
				}
			}
		}
	}
	return changes
}
