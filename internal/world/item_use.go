package world

import (
	"fmt"

	"openmir2/internal/storage"
)

func (w *World) UseItem(ch storage.Character, itemID string) (storage.Character, ItemUseResult, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.normalizeBagItemMakeIndexesLocked(&ch) {
		if err := w.store.SaveCharacter(ch); err != nil {
			return ch, ItemUseResult{}, err
		}
	}
	w.normalizeEquippedItemsLocked(&ch)
	idx := w.findBagItemSlotLocked(ch, itemID, 0)
	if idx < 0 {
		return ch, ItemUseResult{}, fmt.Errorf("item %s not in bag", itemID)
	}
	return w.useBagItemEntryLocked(ch, idx)
}

func (w *World) UseItemByBagIndex(ch storage.Character, bagIndex int) (storage.Character, ItemUseResult, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.normalizeBagItemMakeIndexesLocked(&ch) {
		if err := w.store.SaveCharacter(ch); err != nil {
			return ch, ItemUseResult{}, err
		}
	}
	w.normalizeEquippedItemsLocked(&ch)
	idx := w.findBagItemSlotLocked(ch, "", int32(bagIndex))
	if idx < 0 {
		return ch, ItemUseResult{}, fmt.Errorf("item not in bag")
	}
	return w.useBagItemEntryLocked(ch, idx)
}

func (w *World) useBagItemEntryLocked(ch storage.Character, idx int) (storage.Character, ItemUseResult, error) {
	itemID := ch.BagItems[idx].ItemID
	item, ok := w.data.Items[itemID]
	if !ok {
		return ch, ItemUseResult{}, fmt.Errorf("item %s not found", itemID)
	}
	result := ItemUseResult{Character: ch}
	prev := ch
	prevLevel := ch.Level
	switch {
	case itemID == "金币":
		ch.Gold++
		w.clearBagItemLocked(&ch, idx)
	case itemID == "祝福油":
		if err := w.useBlessingOilLocked(&ch); err != nil {
			return ch, ItemUseResult{}, err
		}
		w.clearBagItemLocked(&ch, idx)
	case itemID == "修复油":
		if err := w.repairWeaponLocked(&ch, false); err != nil {
			return ch, ItemUseResult{}, err
		}
		w.clearBagItemLocked(&ch, idx)
	case itemID == "战神油":
		if err := w.repairWeaponLocked(&ch, true); err != nil {
			return ch, ItemUseResult{}, err
		}
		w.clearBagItemLocked(&ch, idx)
	case item.StdMode == 0:
		next, err := applyStdMode0Use(ch, ch.BagItems[idx], item)
		if err != nil {
			return ch, ItemUseResult{}, err
		}
		ch = next
		w.clearBagItemLocked(&ch, idx)
	case item.StdMode == 1:
		return ch, ItemUseResult{}, fmt.Errorf("item %s cannot be used", itemID)
	case item.StdMode == 2:
		w.clearBagItemLocked(&ch, idx)
	case item.StdMode == 3:
		next, extra, exp, abilityChanged, err := applyStdMode3Use(w, ch, ch.BagItems[idx], item)
		if err != nil {
			return ch, ItemUseResult{}, err
		}
		ch = next
		result.Experience = exp
		result.AbilityChanged = abilityChanged && item.Shape == 12
		if exp > 0 {
			result.CurrentExp = ch.Experience
		}
		if ch.Level > prevLevel {
			result.LevelUp = true
		}
		if extra != "" {
			extraItem, ok := w.data.Items[extra]
			if !ok {
				return ch, ItemUseResult{}, fmt.Errorf("item %s not found", extra)
			}
			if !w.canCarryBagItemsLocked(ch, 6-1) {
				return ch, ItemUseResult{}, fmt.Errorf("bag is full")
			}
			for i := 0; i < 6; i++ {
				if len(ch.BagItems) >= w.gameplay.Item.MaxBagItem {
					return ch, ItemUseResult{}, fmt.Errorf("bag is full")
				}
				ch.BagItems = append(ch.BagItems, w.createUserItemFromStd(extraItem, 0, [14]byte{}))
			}
		}
		w.clearBagItemLocked(&ch, idx)
	case item.StdMode == 31:
		extra, err := unpackBundleItem(item)
		if err != nil {
			return ch, ItemUseResult{}, err
		}
		extraItem, ok := w.data.Items[extra]
		if !ok {
			return ch, ItemUseResult{}, fmt.Errorf("item %s not found", extra)
		}
		if !w.canCarryBagItemsLocked(ch, 6-1) {
			return ch, ItemUseResult{}, fmt.Errorf("bag is full")
		}
		for i := 0; i < 6; i++ {
			if len(ch.BagItems) >= w.gameplay.Item.MaxBagItem {
				return ch, ItemUseResult{}, fmt.Errorf("bag is full")
			}
			entry := w.createUserItemFromStd(extraItem, 0, [14]byte{})
			ch.BagItems = append(ch.BagItems, entry)
			result.AddedItems = append(result.AddedItems, entry)
		}
		w.clearBagItemLocked(&ch, idx)
	case item.Kind == "book" || item.StdMode == 4:
		if skill, ok := w.data.Skills[item.Name]; ok {
			if canLearnSkill(ch, skill) && !hasSkill(ch, skill.ID) {
				ch.Skills = append(ch.Skills, skill.ID)
				w.clearBagItemLocked(&ch, idx)
			} else {
				return ch, ItemUseResult{}, fmt.Errorf("item %s cannot be used", itemID)
			}
		} else {
			return ch, ItemUseResult{}, fmt.Errorf("item %s cannot be used", itemID)
		}
	default:
		return ch, ItemUseResult{}, fmt.Errorf("item %s cannot be used", itemID)
	}
	result.Character = ch
	result.Consumed = true
	if prev.MapID != ch.MapID || prev.X != ch.X || prev.Y != ch.Y {
		result.Teleport = newTeleportEvent(prev, ch)
	}
	if prev.HP != ch.HP || prev.MP != ch.MP || result.LevelUp {
		result.HealthChanged = true
	}
	w.pruneStaleEquippedItemsLocked(&ch)
	return ch, result, w.store.SaveCharacter(ch)
}
