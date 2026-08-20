package world

import (
	"fmt"

	"openmir2/internal/storage"
)

func (w *World) bagItemCountLocked(ch storage.Character) int {
	return len(ch.BagItems)
}

func (w *World) firstEmptyBagSlotLocked(ch storage.Character) int {
	if len(ch.BagItems) >= w.gameplay.Item.MaxBagItem {
		return -1
	}
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

func (w *World) addBagItemLocked(ch *storage.Character, itemID string, desc [14]byte, makeIndex int32) (int, bool) {
	if len(ch.BagItems) >= w.gameplay.Item.MaxBagItem {
		return -1, false
	}
	currentMakeIndex := makeIndex
	if currentMakeIndex <= 0 {
		currentMakeIndex = int32(w.nextID)
		w.nextID++
	} else if next := int(currentMakeIndex) + 1; next > w.nextID {
		w.nextID = next
	}
	ch.BagItems = append(ch.BagItems, storage.UserItem{ItemID: itemID, MakeIndex: currentMakeIndex, Desc: desc})
	return len(ch.BagItems) - 1, true
}

func (w *World) addToBagItemsLocked(ch *storage.Character, itemID string, count int, desc [14]byte) bool {
	return w.addToBagItemsWithMakeIndexLocked(ch, itemID, count, desc, 0)
}

func (w *World) addToBagItemsWithMakeIndexLocked(ch *storage.Character, itemID string, count int, desc [14]byte, makeIndex int32) bool {
	if count <= 0 {
		return true
	}
	if !w.canCarryBagItemsLocked(*ch, count) {
		return false
	}
	for i := 0; i < count; i++ {
		currentMakeIndex := makeIndex
		if currentMakeIndex > 0 {
			currentMakeIndex += int32(i)
		}
		if _, ok := w.addBagItemLocked(ch, itemID, desc, currentMakeIndex); !ok {
			return false
		}
	}
	return true
}

func (w *World) clearBagItemLocked(ch *storage.Character, slot int) {
	if slot < 0 || slot >= len(ch.BagItems) {
		return
	}
	ch.BagItems = append(ch.BagItems[:slot], ch.BagItems[slot+1:]...)
}

func (w *World) MoveBagItem(ch storage.Character, fromSlot, toSlot int) (storage.Character, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.moveBagItemLocked(&ch, fromSlot, toSlot); err != nil {
		return ch, err
	}
	return ch, w.store.SaveCharacter(ch)
}

func (w *World) moveBagItemLocked(ch *storage.Character, fromSlot, toSlot int) error {
	if fromSlot < 0 || fromSlot >= len(ch.BagItems) {
		return fmt.Errorf("source bag slot %d out of range", fromSlot)
	}
	if toSlot < 0 {
		return fmt.Errorf("target bag slot %d out of range", toSlot)
	}
	if fromSlot == toSlot {
		return nil
	}
	source := ch.BagItems[fromSlot]
	items := append([]storage.UserItem(nil), ch.BagItems...)
	items = append(items[:fromSlot], items[fromSlot+1:]...)
	if toSlot > len(items) {
		toSlot = len(items)
	}
	items = append(items[:toSlot], append([]storage.UserItem{source}, items[toSlot:]...)...)
	ch.BagItems = items
	return nil
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

func (w *World) getEquippedItemLocked(ch storage.Character, slot int) string {
	item, ok := w.equippedItemLocked(ch, slot)
	if !ok {
		return ""
	}
	return item.ItemID
}

func (w *World) getEquippedItemIndexLocked(ch storage.Character, slot int) int32 {
	item, ok := w.equippedItemLocked(ch, slot)
	if !ok {
		return 0
	}
	return item.MakeIndex
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

func (w *World) equippedItemIDsLocked(ch storage.Character) []string {
	w.normalizeEquippedItemsLocked(&ch)
	out := make([]string, 0, useSlotCount)
	for i := 0; i < useSlotCount; i++ {
		if item, ok := w.equippedItemLocked(ch, i); ok {
			out = append(out, item.ItemID)
		}
	}
	return out
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
