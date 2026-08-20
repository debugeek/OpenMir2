package world

import (
	"fmt"

	"openmir2/internal/storage"
)

// ItemLocation slot indices, mirrored from the reference client's
// equipped item layout. SlotArmor remains as a compatibility alias for Dress.
const (
	SlotDress     = 0
	SlotArmor     = SlotDress
	SlotWeapon    = 1
	SlotRightHand = 2
	SlotNecklace  = 3
	SlotHelmet    = 4
	SlotArmRingL  = 5
	SlotArmRingR  = 6
	SlotRingL     = 7
	SlotRingR     = 8
	SlotBujuk     = 9
	SlotBelt      = 10
	SlotBoots     = 11
	SlotCharm     = 12
	useSlotCount  = 13
)

// EquipItem moves an item from the character's bag into the given equip slot,
// returning whatever was previously equipped there to the bag.
func (w *World) EquipItem(ch storage.Character, slot int, itemID string) (storage.Character, error) {
	return w.EquipItemByBagIndex(ch, slot, 0, itemID)
}

// EquipItemByBagIndex moves a bag item identified by its MakeIndex and
// item ID into the given equip slot.
func (w *World) EquipItemByBagIndex(ch storage.Character, slot int, bagIndex int, itemID string) (storage.Character, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.normalizeBagItemMakeIndexesLocked(&ch) {
		if err := w.store.SaveCharacter(ch); err != nil {
			return ch, err
		}
	}
	w.normalizeEquippedItemsLocked(&ch)
	updated, err := w.equipItemLocked(ch, slot, bagIndex, itemID)
	if err != nil {
		return ch, err
	}
	return updated, w.store.SaveCharacter(updated)
}

func (w *World) equipItemLocked(ch storage.Character, slot int, bagIndex int, itemID string) (storage.Character, error) {
	if slot < 0 || slot >= useSlotCount {
		return ch, fmt.Errorf("unsupported equip slot %d", slot)
	}
	item, ok := w.data.Items[itemID]
	if !ok {
		return ch, fmt.Errorf("item %s not found", itemID)
	}
	if !w.canWearInSlotLocked(item, slot) {
		return ch, fmt.Errorf("item %s is not wearable", itemID)
	}
	if err := w.canEquipItemLocked(ch, item, slot); err != nil {
		return ch, err
	}
	idx := w.findBagItemSlotLocked(ch, itemID, int32(bagIndex))
	if idx < 0 {
		return ch, fmt.Errorf("item %s not in bag", itemID)
	}
	previous, hasPrevious := w.equippedItemLocked(ch, slot)
	entry := ch.BagItems[idx]
	entryDesc := entry.Desc
	currentDuraMax := entry.DuraMax
	if currentDuraMax == 0 {
		currentDuraMax = itemDuraMax(item)
	}
	currentDura := entry.Dura
	if currentDura == 0 {
		currentDura = currentDuraMax
	}
	if item.StdMode == 15 || item.StdMode == 19 || item.StdMode == 20 || item.StdMode == 21 || item.StdMode == 22 || item.StdMode == 23 || item.StdMode == 24 || item.StdMode == 26 {
		if entryDesc[8] != 0 {
			entryDesc[8] = 0
		}
	}
	addWeight := -item.Weight
	if hasPrevious {
		if prevItem, ok := w.data.Items[previous.ItemID]; ok {
			addWeight += prevItem.Weight
		}
	}
	if !w.canCarryWeightLocked(ch, addWeight) {
		return ch, fmt.Errorf("item %s is too heavy", itemID)
	}
	updated := storage.UserItem{
		ItemID:    itemID,
		MakeIndex: entry.MakeIndex,
		Desc:      entryDesc,
		Dura:      currentDura,
		DuraMax:   currentDuraMax,
	}
	w.setEquippedItemLocked(&ch, slot, updated)
	w.clearBagItemLocked(&ch, idx)
	if hasPrevious {
		ch.BagItems = append(ch.BagItems, previous)
	}
	return ch, nil
}

// UnequipItem returns the item worn in the given slot to the character's bag.
// Mirrors CM_TAKEOFFITEM (ClientTakeOffItems).
func (w *World) UnequipItem(ch storage.Character, slot int) (storage.Character, error) {
	return w.UnequipItemByItemID(ch, slot, "")
}

// UnequipItemByItemID returns the item worn in the given slot after confirming the
// equipped item ID when provided.
func (w *World) UnequipItemByItemID(ch storage.Character, slot int, itemID string) (storage.Character, error) {
	return w.unequipByIdentity(ch, slot, 0, itemID)
}

func (w *World) UnequipItemByMakeIndex(ch storage.Character, slot int, makeIndex int, itemID string) (storage.Character, error) {
	return w.unequipByIdentity(ch, slot, makeIndex, itemID)
}

func (w *World) unequipByIdentity(ch storage.Character, slot, makeIndex int, itemID string) (storage.Character, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.normalizeEquippedItemsLocked(&ch)
	updated, err := w.unequipItemLocked(ch, slot, makeIndex, itemID)
	if err != nil {
		return ch, err
	}
	return updated, w.store.SaveCharacter(updated)
}

func (w *World) unequipItemLocked(ch storage.Character, slot, requestedIndex int, itemID string) (storage.Character, error) {
	if slot < 0 || slot >= useSlotCount {
		return ch, fmt.Errorf("unsupported equip slot %d", slot)
	}
	wornItem, ok := w.equippedItemLocked(ch, slot)
	if !ok {
		return ch, fmt.Errorf("slot %d is empty", slot)
	}
	if itemID != "" && wornItem.ItemID != itemID {
		return ch, fmt.Errorf("item %s not in slot %d", itemID, slot)
	}
	if requestedIndex > 0 && wornItem.MakeIndex != int32(requestedIndex) {
		return ch, fmt.Errorf("item identity mismatch in slot %d", slot)
	}
	item, ok := w.data.Items[wornItem.ItemID]
	if !ok {
		return ch, fmt.Errorf("item %s not found", wornItem.ItemID)
	}
	if !w.canCarryWeightLocked(ch, item.Weight) {
		return ch, fmt.Errorf("item %s is too heavy", wornItem.ItemID)
	}
	if !w.canCarryBagItemsLocked(ch, 1) {
		return ch, fmt.Errorf("bag is full")
	}
	bagItem := wornItem
	if bagItem.DuraMax == 0 {
		bagItem.DuraMax = itemDuraMax(item)
	}
	if bagItem.Dura == 0 {
		bagItem.Dura = bagItem.DuraMax
	}
	w.deleteEquippedItemLocked(&ch, slot)
	ch.BagItems = append(ch.BagItems, bagItem)
	return ch, nil
}
