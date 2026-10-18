package world

import (
	"fmt"
	"strings"
	"time"

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
	w.mu.Lock()
	defer w.mu.Unlock()
	ch = cloneCharacterForMutation(ch)
	updated, _, err := w.equipItemLocked(ch, slot, 0, itemID, false)
	return updated, err
}

// EquipItemByBagIndex moves a bag item identified by its MakeIndex and
// item ID into the given equip slot.
func (w *World) EquipItemByBagIndex(ch storage.Character, slot int, bagIndex int, itemID string) (storage.Character, error) {
	updated, _, err := w.EquipItemByBagIndexWithResult(ch, slot, bagIndex, itemID)
	return updated, err
}

func (w *World) EquipItemByBagIndexWithResult(ch storage.Character, slot int, bagIndex int, itemID string) (storage.Character, EquipResult, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	ch = cloneCharacterForMutation(ch)
	updated, result, err := w.equipItemLocked(ch, slot, bagIndex, itemID, true)
	if err != nil {
		return ch, EquipResult{}, err
	}
	return updated, result, nil
}

func (w *World) resolveItemIDLocked(itemName string) (string, bool) {
	for id, item := range w.data.Items {
		if strings.EqualFold(item.Name, itemName) {
			return id, true
		}
	}
	return "", false
}

func (w *World) itemNameMatchesLocked(itemID, itemName string) bool {
	item, ok := w.data.Items[itemID]
	if !ok {
		return false
	}
	return strings.EqualFold(item.Name, itemName)
}

func (w *World) equipItemLocked(ch storage.Character, slot int, bagIndex int, itemID string, matchMakeIndex bool) (storage.Character, EquipResult, error) {
	before := ch
	if slot < 0 || slot >= useSlotCount {
		return ch, EquipResult{}, fmt.Errorf("unsupported equip slot %d", slot)
	}
	requestedItemName := itemID
	idx := -1
	if matchMakeIndex {
		idx = w.findBagItemByMakeIndexLocked(ch, int32(bagIndex))
	} else {
		resolvedItemID, ok := w.resolveItemIDLocked(itemID)
		if ok {
			idx = w.findBagItemSlotLocked(ch, resolvedItemID, 0)
		}
	}
	if idx < 0 {
		return ch, EquipResult{}, fmt.Errorf("item %s not in bag", itemID)
	}
	if !w.itemNameMatchesLocked(ch.BagItems[idx].ItemID, requestedItemName) {
		return ch, EquipResult{}, fmt.Errorf("item %s not in bag", itemID)
	}
	itemID = ch.BagItems[idx].ItemID
	item, ok := w.data.Items[itemID]
	if !ok {
		return ch, EquipResult{}, fmt.Errorf("item %s not in bag", itemID)
	}
	effectiveItem := UpgradeClientItemForDisplay(item, ch.BagItems[idx], false)
	if !w.canWearInSlotLocked(effectiveItem, slot) {
		return ch, EquipResult{}, fmt.Errorf("item %s is not wearable", itemID)
	}
	if err := w.canEquipItemLocked(ch, effectiveItem, slot); err != nil {
		return ch, EquipResult{}, err
	}
	previous, hasPrevious := w.equippedItemLocked(ch, slot)
	if hasPrevious {
		previousItem, ok := w.data.Items[previous.ItemID]
		if ok && mysteryEquipStdMode(previousItem.StdMode) && previous.Desc[7] != 0 && !ch.DrugUnlocked {
			return ch, EquipResult{}, fmt.Errorf("item %s cannot be taken off", previous.ItemID)
		}
	}
	entry := ch.BagItems[idx]
	entryDesc := entry.Desc
	currentDura := entry.Dura
	if item.StdMode == 15 || item.StdMode == 19 || item.StdMode == 20 || item.StdMode == 21 || item.StdMode == 22 || item.StdMode == 23 || item.StdMode == 24 || item.StdMode == 26 {
		if entryDesc[8] != 0 {
			entryDesc[8] = 0
		}
	}
	updated := storage.UserItem{
		ItemID:    itemID,
		MakeIndex: entry.MakeIndex,
		Desc:      entryDesc,
		Dura:      currentDura,
		DuraMax:   entry.DuraMax,
	}
	w.setEquippedItemLocked(&ch, slot, updated)
	w.applyStealthRingStateLocked(&ch, time.Now())
	w.clearBagItemLocked(&ch, idx)
	if hasPrevious {
		ch.BagItems = append(ch.BagItems, previous)
	}
	addedSkills, removedSkills := w.itemSkillChangesLocked(before, ch)
	for _, skillID := range removedSkills {
		delete(ch.ItemSkillHotkeys, skillID)
	}
	result := EquipResult{Character: ch, AddedSkills: addedSkills, RemovedSkills: removedSkills}
	if hasPrevious {
		result.SwappedOut = previous
		result.HasSwappedOut = true
	}
	return ch, result, nil
}

// UnequipItem returns the item worn in the given slot to the character's bag.
// Mirrors CM_TAKEOFFITEM (ClientTakeOffItems).
func (w *World) UnequipItem(ch storage.Character, slot int) (storage.Character, error) {
	updated, _, err := w.UnequipItemByItemIDWithResult(ch, slot, "")
	return updated, err
}

// UnequipItemByItemID returns the item worn in the given slot after confirming the
// equipped item ID when provided.
func (w *World) UnequipItemByItemID(ch storage.Character, slot int, itemID string) (storage.Character, error) {
	updated, _, err := w.UnequipItemByItemIDWithResult(ch, slot, itemID)
	return updated, err
}

func (w *World) UnequipItemByItemIDWithResult(ch storage.Character, slot int, itemID string) (storage.Character, UnequipResult, error) {
	return w.unequipByIdentityWithResult(ch, slot, 0, itemID, false)
}

func (w *World) UnequipItemByMakeIndexWithResult(ch storage.Character, slot, makeIndex int, itemID string) (storage.Character, UnequipResult, error) {
	return w.unequipByIdentityWithResult(ch, slot, makeIndex, itemID, true)
}

func (w *World) unequipByIdentityWithResult(ch storage.Character, slot, makeIndex int, itemID string, matchMakeIndex bool) (storage.Character, UnequipResult, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	ch = cloneCharacterForMutation(ch)
	updated, result, err := w.unequipItemLocked(ch, slot, makeIndex, itemID, matchMakeIndex)
	if err != nil {
		return ch, UnequipResult{}, err
	}
	return updated, result, nil
}

func (w *World) unequipItemLocked(ch storage.Character, slot, requestedIndex int, itemID string, matchMakeIndex bool) (storage.Character, UnequipResult, error) {
	before := ch
	if slot < 0 || slot >= useSlotCount {
		return ch, UnequipResult{}, fmt.Errorf("unsupported equip slot %d", slot)
	}
	wornItem, ok := w.equippedItemLocked(ch, slot)
	if !ok {
		return ch, UnequipResult{}, fmt.Errorf("slot %d is empty", slot)
	}
	if matchMakeIndex && wornItem.MakeIndex != int32(requestedIndex) {
		return ch, UnequipResult{}, fmt.Errorf("item identity mismatch in slot %d", slot)
	}
	if itemID != "" && !w.itemNameMatchesLocked(wornItem.ItemID, itemID) {
		return ch, UnequipResult{}, fmt.Errorf("item %s not in slot %d", itemID, slot)
	}
	_, ok = w.data.Items[wornItem.ItemID]
	if !ok {
		return ch, UnequipResult{}, fmt.Errorf("item %s not found", wornItem.ItemID)
	}
	item := w.data.Items[wornItem.ItemID]
	if mysteryEquipStdMode(item.StdMode) && wornItem.Desc[7] != 0 && !ch.DrugUnlocked {
		return ch, UnequipResult{}, fmt.Errorf("item %s cannot be taken off", wornItem.ItemID)
	}
	if !w.canCarryBagItemsLocked(ch, 1) {
		return ch, UnequipResult{}, fmt.Errorf("bag is full")
	}
	bagItem := wornItem
	w.deleteEquippedItemLocked(&ch, slot)
	w.applyStealthRingStateLocked(&ch, time.Now())
	ch.BagItems = append(ch.BagItems, bagItem)
	addedSkills, removedSkills := w.itemSkillChangesLocked(before, ch)
	for _, skillID := range removedSkills {
		delete(ch.ItemSkillHotkeys, skillID)
	}
	return ch, UnequipResult{Character: ch, RemovedItem: bagItem, HasRemovedItem: true, AddedSkills: addedSkills, RemovedSkills: removedSkills}, nil
}

func mysteryEquipStdMode(stdMode int) bool {
	switch stdMode {
	case 15, 19, 20, 21, 22, 23, 24, 26:
		return true
	default:
		return false
	}
}
