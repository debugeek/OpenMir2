package world

import (
	"fmt"

	"openmir2/internal/data"
	"openmir2/internal/storage"
)

type itemUseMessageError struct {
	err     error
	message string
}

func (e *itemUseMessageError) Error() string { return e.err.Error() }

func (e *itemUseMessageError) Unwrap() error { return e.err }

func (w *World) UseItemByBagIndex(ch storage.Character, bagIndex int) (storage.Character, ItemUseResult, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	ch = cloneCharacterForMutation(ch)
	idx := w.findBagItemByMakeIndexLocked(ch, int32(bagIndex))
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
	if itemID == "金币" {
		return ch, ItemUseResult{}, fmt.Errorf("item %s cannot be used", itemID)
	}
	if mp, ok := w.data.Maps[ch.MapID]; ok && mp.NoDrug && item.StdMode >= 0 && item.StdMode <= 3 {
		return ch, ItemUseResult{Character: ch, SystemMessageEvents: []ItemUseSystemMessage{{Text: "此地图不允许使用任何药品.", Foreground: 0xFF, Background: 0x38}}, Consumed: false}, fmt.Errorf("items are not usable on this map")
	}
	result := ItemUseResult{Character: ch, SuppressRemovedSync: true}
	prev := ch
	prevLevel := ch.Level
	consumedItem := ch.BagItems[idx]
	switch {
	case item.StdMode == 3 && item.Shape == 4:
		changed, message, messages, err := w.useBlessingOilLocked(&ch)
		if err != nil {
			return ch, ItemUseResult{}, err
		}
		result.AbilityChanged = changed
		result.SubAbilityChanged = changed
		result.SystemMessage = message
		result.SystemMessages = messages
		result.SystemMessageBeforeAbility = true
		w.clearBagItemLocked(&ch, idx)
		result.RemovedItems = append(result.RemovedItems, consumedItem)
	case item.StdMode == 3 && item.Shape == 9:
		if err := w.repairWeaponLocked(&ch, false); err != nil {
			return ch, ItemUseResult{}, err
		}
		result.Durability = w.equippedItemDurabilityLocked(ch, SlotWeapon)
		result.SystemMessage = "武器修复成功..."
		w.clearBagItemLocked(&ch, idx)
		result.RemovedItems = append(result.RemovedItems, consumedItem)
	case item.StdMode == 3 && item.Shape == 10:
		if err := w.repairWeaponLocked(&ch, true); err != nil {
			return ch, ItemUseResult{}, err
		}
		result.Durability = w.equippedItemDurabilityLocked(ch, SlotWeapon)
		result.SystemMessage = "武器修复成功..."
		w.clearBagItemLocked(&ch, idx)
		result.RemovedItems = append(result.RemovedItems, consumedItem)
	case item.StdMode == 0:
		next, err := applyStdMode0Use(ch, ch.BagItems[idx], item)
		if err != nil {
			return ch, ItemUseResult{}, err
		}
		ch = next
		if item.Shape == 1 {
			result.HealthChanged = true
		}
		w.clearBagItemLocked(&ch, idx)
		result.RemovedItems = append(result.RemovedItems, consumedItem)
	case item.StdMode == 2:
		w.clearBagItemLocked(&ch, idx)
		result.RemovedItems = append(result.RemovedItems, consumedItem)
	case item.StdMode == 3:
		next, systemMessages, abilityChanged, used, err := applyStdMode3Use(w, ch, ch.BagItems[idx], item)
		if err != nil {
			if messageErr, ok := err.(*itemUseMessageError); ok {
				result.Character = ch
				result.SystemMessageEvents = []ItemUseSystemMessage{{Text: messageErr.message, Foreground: 0xFF, Background: 0x38}}
				return ch, result, err
			}
			return ch, ItemUseResult{}, err
		}
		ch = next
		result.AbilityChanged = abilityChanged && item.Shape == 12
		result.SystemMessages = systemMessages
		if !used {
			result.Character = ch
			return ch, result, fmt.Errorf("item %s cannot be used", item.ID)
		}
		if ch.Level > prevLevel {
			result.LevelUp = true
		}
		w.clearBagItemLocked(&ch, idx)
		result.RemovedItems = append(result.RemovedItems, consumedItem)
	case item.StdMode == 31:
		if item.AniCount != 0 {
			if (item.ID != "金条" && item.ID != "金砖") || (item.AniCount != 36 && item.AniCount != 37) {
				return ch, ItemUseResult{}, fmt.Errorf("item %s cannot be used", item.ID)
			}
			w.clearBagItemLocked(&ch, idx)
			result.RemovedItems = append(result.RemovedItems, consumedItem)
			result.SuppressRemovedSync = true
			break
		}
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
		w.clearBagItemLocked(&ch, idx)
		for i := 0; i < 6; i++ {
			entry := w.createUserItemFromStd(extraItem, 0, [14]byte{})
			ch.BagItems = append(ch.BagItems, entry)
			result.AddedItems = append(result.AddedItems, entry)
		}
		result.RemovedItems = append(result.RemovedItems, consumedItem)
		result.SuppressRemovedSync = true
	case item.StdMode == 4:
		if skill, ok := skillForBook(w.data.Skills, item.Name); ok {
			if canLearnSkill(ch, skill) && !skillAlreadyAvailableLocked(w, ch, skill.ID) {
				if !learnSkill(&ch, skill.ID) {
					return ch, ItemUseResult{}, fmt.Errorf("item %s cannot be used", itemID)
				}
				result.SkillChanged = true
				result.AddedSkills = append(result.AddedSkills, storage.SkillState{ID: skill.ID})
				switch skill.ID {
				case "刺杀剑术":
					ch.ThrustingDisabled = false
					result.EnableThrusting = true
				case "半月弯刀":
					ch.HalfMoonDisabled = false
					result.EnableHalfMoon = true
				}
				if _, _, ok := ch.Skills.Get("刺杀剑术"); ok && ch.ThrustingDisabled {
					ch.ThrustingDisabled = false
					result.EnableThrusting = true
				}
				if _, _, ok := ch.Skills.Get("半月弯刀"); ok && ch.HalfMoonDisabled {
					ch.HalfMoonDisabled = false
					result.EnableHalfMoon = true
				}
				w.clearBagItemLocked(&ch, idx)
				result.RemovedItems = append(result.RemovedItems, consumedItem)
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
	if item.StdMode == 3 && (item.Shape == 1 || item.Shape == 2 || item.Shape == 3) {
		if result.Teleport == nil {
			result.Teleport = newTeleportEvent(prev, ch)
		}
		result.Teleport.SpaceMoveFire = true
	}
	if prev.HP != ch.HP || prev.MP != ch.MP || result.LevelUp {
		result.HealthChanged = true
	}
	return ch, result, nil
}

func skillAlreadyAvailableLocked(w *World, ch storage.Character, skillID string) bool {
	_, _, ok := ch.Skills.Get(skillID)
	return ok
}

func skillForBook(skills map[string]data.StdSkill, itemName string) (data.StdSkill, bool) {
	for _, skill := range skills {
		if skill.Name == itemName {
			return skill, true
		}
	}
	return data.StdSkill{}, false
}
