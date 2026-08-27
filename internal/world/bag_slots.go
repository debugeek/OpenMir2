package world

import (
	"fmt"

	"openmir2/internal/data"
	"openmir2/internal/storage"
)

func (w *World) canWearInSlotLocked(item data.StdItem, slot int) bool {
	switch slot {
	case SlotWeapon:
		return item.StdMode == 5 || item.StdMode == 6
	case SlotDress:
		return item.StdMode == 10 || item.StdMode == 11
	case SlotHelmet:
		return item.StdMode == 15
	case SlotNecklace:
		return item.StdMode == 19 || item.StdMode == 20 || item.StdMode == 21
	case SlotRingL, SlotRingR:
		return item.StdMode == 22 || item.StdMode == 23
	case SlotArmRingL:
		return item.StdMode == 24 || item.StdMode == 25 || item.StdMode == 26
	case SlotArmRingR:
		return item.StdMode == 24 || item.StdMode == 26
	case SlotRightHand:
		return item.StdMode == 28 || item.StdMode == 29 || item.StdMode == 30
	case SlotBujuk:
		return item.StdMode == 25 || item.StdMode == 51
	case SlotBoots:
		return item.StdMode == 52 || item.StdMode == 62
	case SlotCharm:
		return item.StdMode == 53 || item.StdMode == 63
	case SlotBelt:
		return item.StdMode == 54 || item.StdMode == 64
	default:
		return false
	}
}

func (w *World) canEquipItemLocked(ch storage.Character, item data.StdItem, slot int) error {
	if slot == SlotDress {
		switch item.StdMode {
		case 10:
			if ch.Sex != 0 {
				return fmt.Errorf("item %s cannot be used", item.ID)
			}
		case 11:
			if ch.Sex == 0 {
				return fmt.Errorf("item %s cannot be used", item.ID)
			}
		}
	}
	level := ch.Level
	if level <= 0 {
		level = 1
	}
	base := Base(ch.Class, level)
	stats := w.combatStatsLocked(ch)
	combinedDC := PackWord(base.DC, base.DCMax, stats.DC, stats.DCMax)
	combinedMC := PackWord(base.MC, base.MCMax, stats.MC, stats.MCMax)
	combinedSC := PackWord(base.SC, base.SCMax, stats.SC, stats.SCMax)
	job := 0
	switch NormalizeClass(ch.Class) {
	case "wizard":
		job = 1
	case "taoist":
		job = 2
	}
	switch item.Need {
	case 0:
		if level < item.NeedLevel {
			return fmt.Errorf("item %s cannot be used", item.ID)
		}
	case 1:
		if int(byte(combinedDC>>8)) < item.NeedLevel {
			return fmt.Errorf("item %s cannot be used", item.ID)
		}
	case 2:
		if int(byte(combinedMC>>8)) < item.NeedLevel {
			return fmt.Errorf("item %s cannot be used", item.ID)
		}
	case 3:
		if int(byte(combinedSC>>8)) < item.NeedLevel {
			return fmt.Errorf("item %s cannot be used", item.ID)
		}
	case 10:
		if job != item.NeedLevel&0xFF || level < item.NeedLevel>>8 {
			return fmt.Errorf("item %s cannot be used", item.ID)
		}
	case 11:
		if job != item.NeedLevel&0xFF || int(byte(combinedDC>>8)) < item.NeedLevel>>8 {
			return fmt.Errorf("item %s cannot be used", item.ID)
		}
	case 12:
		if job != item.NeedLevel&0xFF || int(byte(combinedMC>>8)) < item.NeedLevel>>8 {
			return fmt.Errorf("item %s cannot be used", item.ID)
		}
	case 13:
		if job != item.NeedLevel&0xFF || int(byte(combinedSC>>8)) < item.NeedLevel>>8 {
			return fmt.Errorf("item %s cannot be used", item.ID)
		}
	default:
		if item.Need != 0 || item.NeedLevel != 0 {
			return fmt.Errorf("item %s cannot be used", item.ID)
		}
	}
	if slot == SlotWeapon || slot == SlotRightHand {
		if item.Weight > base.MaxHandWeight {
			return fmt.Errorf("item %s is too heavy", item.ID)
		}
		return nil
	}
	currentWeight := 0
	for i, item := range ch.EquippedItems {
		if i == slot || i == SlotWeapon || i == SlotRightHand || item.ItemID == "" {
			continue
		}
		currentWeight += w.itemWeight(item.ItemID)
	}
	if currentWeight+item.Weight > base.MaxWearWeight {
		return fmt.Errorf("item %s is too heavy", item.ID)
	}
	return nil
}
