package world

import "time"

func (w *World) MapName(mapID string) string {
	w.mu.Lock()
	defer w.mu.Unlock()
	if mp, ok := w.data.Maps[mapID]; ok {
		return mp.Name
	}
	return mapID
}

func (w *World) MapDimensions(mapID string) (int, int, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	mp, ok := w.data.Maps[mapID]
	if !ok {
		return 0, 0, false
	}
	return mp.Width, mp.Height, true
}

func (w *World) MapLight(mapID string) int {
	w.mu.Lock()
	defer w.mu.Unlock()
	if mp, ok := w.data.Maps[mapID]; ok {
		return mp.Light
	}
	return 0
}

func (w *World) MapMinMap(mapID string) int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.data.MiniMaps[mapID]
}

func (w *World) MapDayBright(mapID string, gameTime byte) int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.mapDayBrightLocked(mapID, gameTime)
}

func (w *World) mapDayBrightLocked(mapID string, gameTime byte) int {
	mp, ok := w.data.Maps[mapID]
	if !ok || mp.LightFixed {
		if ok {
			return mp.Light
		}
		return 0
	}
	switch gameTime {
	case 1:
		return 0
	case 3:
		return 1
	default:
		return 2
	}
}

func GameTime(now time.Time) byte {
	switch now.Hour() {
	case 4, 15:
		return 0
	case 5, 6, 7, 8, 9, 10, 16, 17, 18, 19, 20, 21, 22:
		return 1
	case 11, 23:
		return 2
	default:
		return 3
	}
}

func (w *World) MapRunHuman(mapID string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if mp, ok := w.data.Maps[mapID]; ok {
		return mp.RunHuman
	}
	return false
}
