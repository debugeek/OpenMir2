package world

import "openmir2/internal/data"

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

func (w *World) DropLooks(drop GroundDrop) int32 {
	if drop.ItemID == "金币" {
		looks := int32(112)
		if drop.Count >= 30 {
			looks = 113
		}
		if drop.Count >= 70 {
			looks = 114
		}
		if drop.Count >= 300 {
			looks = 115
		}
		if drop.Count >= 1000 {
			looks = 116
		}
		return looks
	}
	item, ok := w.Item(drop.ItemID)
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
