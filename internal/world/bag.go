package world

import "openmir2/internal/storage"

func cloneCharacterForMutation(ch storage.Character) storage.Character {
	if ch.EquippedItems != nil {
		cloned := make(map[int]storage.UserItem, len(ch.EquippedItems))
		for slot, item := range ch.EquippedItems {
			cloned[slot] = item
		}
		ch.EquippedItems = cloned
	}
	if ch.BagItems != nil {
		ch.BagItems = append([]storage.UserItem(nil), ch.BagItems...)
	}
	if ch.StorageItems != nil {
		ch.StorageItems = append([]storage.UserItem(nil), ch.StorageItems...)
	}
	if ch.ItemSkillHotkeys != nil {
		cloned := make(map[string]byte, len(ch.ItemSkillHotkeys))
		for skillID, hotkey := range ch.ItemSkillHotkeys {
			cloned[skillID] = hotkey
		}
		ch.ItemSkillHotkeys = cloned
	}
	return ch
}

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

func (w *World) findBagItemByMakeIndexLocked(ch storage.Character, makeIndex int32) int {
	for i, entry := range ch.BagItems {
		if entry.ItemID != "" && entry.MakeIndex == makeIndex {
			return i
		}
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
				if entry.MakeIndex <= itemMakeIndexMax && entry.MakeIndex+1 > w.nextItemID {
					w.nextItemID = entry.MakeIndex + 1
				}
				normalized = append(normalized, entry)
				continue
			}
		}
		entry.MakeIndex = w.nextItemMakeIndexLocked()
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
		standard, ok := w.data.Items[item.ItemID]
		if !ok {
			delete(ch.EquippedItems, slot)
			changed = true
			continue
		}
		if !w.canWearInSlotLocked(standard, slot) {
			if item.MakeIndex == 0 {
				item.MakeIndex = w.nextItemMakeIndexLocked()
			}
			ch.BagItems = append([]storage.UserItem{item}, ch.BagItems...)
			delete(ch.EquippedItems, slot)
			changed = true
			continue
		}
		if item.MakeIndex != 0 {
			if item.MakeIndex <= itemMakeIndexMax && item.MakeIndex+1 > w.nextItemID {
				w.nextItemID = item.MakeIndex + 1
			}
			continue
		}
		item.MakeIndex = w.nextItemMakeIndexLocked()
		ch.EquippedItems[slot] = item
		changed = true
	}
	return changed
}

func (w *World) normalizeStorageItemMakeIndexesLocked(ch *storage.Character) bool {
	changed := false
	seen := map[int32]struct{}{}
	normalized := make([]storage.UserItem, 0, len(ch.StorageItems))
	for _, entry := range ch.StorageItems {
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
				if entry.MakeIndex <= itemMakeIndexMax && entry.MakeIndex+1 > w.nextItemID {
					w.nextItemID = entry.MakeIndex + 1
				}
				normalized = append(normalized, entry)
				continue
			}
		}
		entry.MakeIndex = w.nextItemMakeIndexLocked()
		seen[entry.MakeIndex] = struct{}{}
		normalized = append(normalized, entry)
		changed = true
	}
	if len(normalized) != len(ch.StorageItems) {
		changed = true
	}
	ch.StorageItems = normalized
	return changed
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

func (w *World) activeEquippedItemLocked(ch storage.Character, slot int) (storage.UserItem, bool) {
	item, ok := w.equippedItemLocked(ch, slot)
	if !ok {
		return storage.UserItem{}, false
	}
	standard, ok := w.data.Items[item.ItemID]
	if !ok || (item.Dura == 0 && standard.DuraMax > 0) {
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
