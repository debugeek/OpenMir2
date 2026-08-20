package world

import (
	"openmir2/internal/data"
)

func (w *World) ItemName(itemID string) (string, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	item, ok := w.data.Items[itemID]
	if !ok {
		return "", false
	}
	return item.Name, true
}

func (w *World) Item(itemID string) (data.StdItem, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	item, ok := w.data.Items[itemID]
	if !ok {
		return data.StdItem{}, false
	}
	return item, true
}

func (w *World) ItemKind(itemID string) (string, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	item, ok := w.data.Items[itemID]
	if !ok {
		return "", false
	}
	return item.Kind, true
}

func (w *World) DropLooks(itemID string) int32 {
	item, ok := w.Item(itemID)
	if !ok {
		return 2
	}
	return int32(item.Looks)
}

func (w *World) DropDisplayName(drop GroundDrop) string {
	if name, ok := w.ItemName(drop.ItemID); ok {
		return name
	}
	return drop.ItemID
}
