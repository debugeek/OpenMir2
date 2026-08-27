package world

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
