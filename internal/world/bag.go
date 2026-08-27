package world

import "openmir2/internal/storage"

func (w *World) bagItemCountLocked(ch storage.Character) int {
	return len(ch.BagItems)
}

func (w *World) findBagItemSlotLocked(ch storage.Character, itemID string, makeIndex int32) int {
	for i, entry := range ch.BagItems {
		if entry.ItemID == "" {
			continue
		}
		if itemID != "" && entry.ItemID != itemID {
			continue
		}
		if makeIndex > 0 && entry.MakeIndex != makeIndex {
			continue
		}
		return i
	}
	return -1
}

func (w *World) clearBagItemLocked(ch *storage.Character, slot int) {
	if slot < 0 || slot >= len(ch.BagItems) {
		return
	}
	ch.BagItems = append(ch.BagItems[:slot], ch.BagItems[slot+1:]...)
}

func (w *World) normalizeBagItemMakeIndexesLocked(ch *storage.Character) bool {
	changed := false
	seen := map[int32]struct{}{}
	normalized := make([]storage.UserItem, 0, len(ch.BagItems))
	for _, entry := range ch.BagItems {
		if entry.ItemID == "" {
			changed = true
			continue
		}
		if _, ok := w.data.Items[entry.ItemID]; !ok {
			changed = true
			continue
		}
		if entry.MakeIndex > 0 {
			if _, ok := seen[entry.MakeIndex]; !ok {
				seen[entry.MakeIndex] = struct{}{}
				if next := int(entry.MakeIndex) + 1; next > w.nextID {
					w.nextID = next
				}
				normalized = append(normalized, entry)
				continue
			}
		}
		entry.MakeIndex = int32(w.nextID)
		w.nextID++
		seen[entry.MakeIndex] = struct{}{}
		normalized = append(normalized, entry)
		changed = true
	}
	if len(normalized) != len(ch.BagItems) {
		changed = true
	}
	ch.BagItems = normalized
	return changed
}

func (w *World) normalizeEquippedItemsLocked(ch *storage.Character) bool {
	if ch.EquippedItems == nil {
		return false
	}
	changed := false
	for slot, item := range ch.EquippedItems {
		if slot < 0 || slot >= useSlotCount || item.ItemID == "" {
			delete(ch.EquippedItems, slot)
			changed = true
			continue
		}
		if item.MakeIndex != 0 {
			continue
		}
		item.MakeIndex = int32(w.nextID)
		w.nextID++
		ch.EquippedItems[slot] = item
		changed = true
	}
	return changed
}

func (w *World) pruneStaleEquippedItemsLocked(ch *storage.Character) bool {
	if ch.EquippedItems == nil {
		return false
	}
	changed := false
	for slot, item := range ch.EquippedItems {
		if item.ItemID == "" || slot == SlotDress || slot == SlotWeapon {
			continue
		}
		if w.bagHasItemLocked(*ch, item.ItemID, item.MakeIndex) {
			continue
		}
		delete(ch.EquippedItems, slot)
		changed = true
	}
	return changed
}

func (w *World) bagHasItemLocked(ch storage.Character, itemID string, makeIndex int32) bool {
	for _, entry := range ch.BagItems {
		if entry.ItemID != itemID {
			continue
		}
		if makeIndex > 0 && entry.MakeIndex != makeIndex {
			continue
		}
		return true
	}
	return false
}

func (w *World) equippedItemLocked(ch storage.Character, slot int) (storage.UserItem, bool) {
	if slot < 0 || slot >= useSlotCount {
		return storage.UserItem{}, false
	}
	if ch.EquippedItems == nil {
		return storage.UserItem{}, false
	}
	item, ok := ch.EquippedItems[slot]
	if !ok || item.ItemID == "" {
		return storage.UserItem{}, false
	}
	return item, true
}

func (w *World) setEquippedItemLocked(ch *storage.Character, slot int, item storage.UserItem) {
	if slot < 0 || slot >= useSlotCount {
		return
	}
	if ch.EquippedItems == nil {
		ch.EquippedItems = map[int]storage.UserItem{}
	}
	if item.ItemID == "" {
		delete(ch.EquippedItems, slot)
		return
	}
	ch.EquippedItems[slot] = item
}

func (w *World) deleteEquippedItemLocked(ch *storage.Character, slot int) {
	if ch.EquippedItems == nil {
		return
	}
	delete(ch.EquippedItems, slot)
}

func (w *World) equippedSlotMapLocked(ch storage.Character) map[int]string {
	w.normalizeEquippedItemsLocked(&ch)
	out := map[int]string{}
	for i := 0; i < useSlotCount; i++ {
		if item, ok := w.equippedItemLocked(ch, i); ok {
			out[i] = item.ItemID
		}
	}
	return out
}
