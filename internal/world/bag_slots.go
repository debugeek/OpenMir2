package world

import (
	"errors"
	"fmt"

	"openmir2/internal/data"
	"openmir2/internal/storage"
)

type equipFailureMessageError struct {
	err     error
	message string
}

func (e *equipFailureMessageError) Error() string { return e.err.Error() }

func (e *equipFailureMessageError) Unwrap() error { return e.err }

func EquipFailureMessage(err error) (string, bool) {
	var messageErr *equipFailureMessageError
	if !errors.As(err, &messageErr) {
		return "", false
	}
	return messageErr.message, true
}

func equipFailure(err error, message string) error {
	return &equipFailureMessageError{err: err, message: message}
}

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
	default:
		return false
	}
}

func (w *World) canEquipItemLocked(ch storage.Character, item data.StdItem, slot int) error {
	if slot == SlotDress {
		switch item.StdMode {
		case 10:
			if ch.Sex != 0 {
				return equipFailure(fmt.Errorf("item %s cannot be used", item.ID), "男装不能穿戴物品.")
			}
		case 11:
			if ch.Sex == 0 {
				return equipFailure(fmt.Errorf("item %s cannot be used", item.ID), "女装不能穿戴物品.")
			}
		}
	}
	level := ch.Level
	if level <= 0 {
		level = 1
	}
	base := Base(ch.Class, level)
	if w.characterHasMuscleRingLocked(ch) {
		base.MaxWearWeight *= 2
		base.MaxHandWeight *= 2
	}
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
	if slot == SlotWeapon || slot == SlotRightHand {
		if item.Weight > base.MaxHandWeight {
			return equipFailure(fmt.Errorf("item %s is too heavy", item.ID), "手上重量不够.")
		}
	} else {
		currentWeight := 0
		for i, equipped := range ch.EquippedItems {
			if i == slot || i == SlotWeapon || i == SlotRightHand || equipped.ItemID == "" {
				continue
			}
			currentWeight += w.itemWeight(equipped.ItemID)
		}
		if currentWeight+item.Weight > base.MaxWearWeight {
			return equipFailure(fmt.Errorf("item %s is too heavy", item.ID), "负重不够.")
		}
	}
	switch item.Need {
	case 0:
		if level < item.NeedLevel {
			return equipFailure(fmt.Errorf("item %s cannot be used", item.ID), "等级不够.")
		}
	case 1:
		if int(byte(combinedDC>>8)) < item.NeedLevel {
			return equipFailure(fmt.Errorf("item %s cannot be used", item.ID), "攻击力不够.")
		}
	case 2:
		if int(byte(combinedMC>>8)) < item.NeedLevel {
			return equipFailure(fmt.Errorf("item %s cannot be used", item.ID), "魔法力不够.")
		}
	case 3:
		if int(byte(combinedSC>>8)) < item.NeedLevel {
			return equipFailure(fmt.Errorf("item %s cannot be used", item.ID), "道术力不够.")
		}
	case 5:
		if ch.CreditPoint < item.NeedLevel {
			return equipFailure(fmt.Errorf("item %s cannot be used", item.ID), "声望点不够.")
		}
	case 10:
		if job != item.NeedLevel&0xFF || level < item.NeedLevel>>8 {
			return equipFailure(fmt.Errorf("item %s cannot be used", item.ID), "职业不符或等级不够.")
		}
	case 11:
		if job != item.NeedLevel&0xFF || int(byte(combinedDC>>8)) < item.NeedLevel>>8 {
			return equipFailure(fmt.Errorf("item %s cannot be used", item.ID), "职业不符或攻击力不够.")
		}
	case 12:
		if job != item.NeedLevel&0xFF || int(byte(combinedMC>>8)) < item.NeedLevel>>8 {
			return equipFailure(fmt.Errorf("item %s cannot be used", item.ID), "职业不符或魔法力不够.")
		}
	case 13:
		if job != item.NeedLevel&0xFF || int(byte(combinedSC>>8)) < item.NeedLevel>>8 {
			return equipFailure(fmt.Errorf("item %s cannot be used", item.ID), "职业不符或道术力不够.")
		}
	default:
		if item.Need != 0 || item.NeedLevel != 0 {
			return fmt.Errorf("item %s cannot be used", item.ID)
		}
	}
	return nil
}
