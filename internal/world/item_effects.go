package world

import (
	"fmt"
	"math"
	"time"

	"openmir2/internal/data"
	"openmir2/internal/storage"
	"openmir2/internal/world/core"
)

func applyStdMode0Use(ch storage.Character, entry storage.UserItem, item data.StdItem) (storage.Character, error) {
	switch item.Shape {
	case 1:
		ch = core.ApplyVitalDelta(ch, item.Stats.AcMin, item.Stats.MacMin).Character
	case 2:
		return ch, nil
	case 3:
		hp := int(math.Round(float64(ch.MaxHP) * float64(item.Stats.AcMin) / 100.0))
		mp := int(math.Round(float64(ch.MaxMP) * float64(item.Stats.MacMin) / 100.0))
		ch = core.ApplyVitalDelta(ch, hp, mp).Character
	default:
		ch = core.QueueRecovery(ch, item.Stats.AcMin, item.Stats.MacMin)
	}
	_ = entry
	return ch, nil
}

func applyStdMode3Use(w *World, ch storage.Character, entry storage.UserItem, item data.StdItem) (storage.Character, string, int, bool, error) {
	_ = entry
	switch item.Shape {
	case 1:
		next, err := w.homeTeleportRandomCharacterLocked(ch)
		return next, "", 0, false, err
	case 3:
		next, err := w.homeTeleportCharacterLocked(ch)
		return next, "", 0, false, err
	case 5:
		next, err := w.homeTeleportCharacterLocked(ch)
		return next, "", 0, false, err
	case 2:
		next, err := w.teleportRandomInMapLocked(ch, ch.MapID)
		return next, "", 0, false, err
	case 12:
		now := time.Now()
		duration := time.Duration(byte(item.Stats.DcMax))*time.Minute + time.Duration(byte(item.Stats.MacMax))*time.Second
		if duration < 0 {
			duration = 0
		}
		abilityChanged := applyTemporaryAbilityLocked(&ch, tempAbility{
			DC:       byte(item.Stats.DcMin),
			MC:       byte(item.Stats.McMin),
			SC:       byte(item.Stats.ScMin),
			HitSpeed: byte(item.Stats.AcMax),
			HP:       byte(item.Stats.AcMin),
			MP:       byte(item.Stats.MacMin),
			Duration: duration,
			Now:      now,
		})
		return ch, "", 0, abilityChanged, nil
	case 4, 9, 10:
		return ch, "", 0, false, fmt.Errorf("item %s cannot be used", item.ID)
	case 13:
		return gainExperienceLocked(w, ch, item.DuraMax)
	default:
		return ch, "", 0, false, fmt.Errorf("item %s cannot be used", item.ID)
	}
}

func (w *World) useBlessingOilLocked(ch *storage.Character) (bool, error) {
	weapon, ok := w.equippedItemLocked(*ch, SlotWeapon)
	if !ok {
		return false, fmt.Errorf("item 祝福油 cannot be used")
	}
	desc := weapon.Desc
	item, ok := w.data.Items[weapon.ItemID]
	if !ok {
		return false, fmt.Errorf("item 祝福油 cannot be used")
	}
	nRand := abs(int(item.Stats.DcMax)-int(item.Stats.DcMin)) / 5
	if w.rand.Intn(20) == 1 {
		if desc[3] > 0 {
			desc[3]--
		} else if desc[4] < 10 {
			desc[4]++
		}
	} else if desc[4] > 0 {
		desc[4]--
	} else if desc[3] < 1 {
		desc[3]++
	} else if desc[3] < 3 && w.rand.Intn(nRand+6) == 1 {
		desc[3]++
	} else if desc[3] < 7 && w.rand.Intn(nRand*10+30) == 1 {
		desc[3]++
	} else {
		return true, nil
	}
	weapon.Desc = desc
	w.setEquippedItemLocked(ch, SlotWeapon, weapon)
	return true, nil
}

func (w *World) repairWeaponLocked(ch *storage.Character, super bool) error {
	weapon, ok := w.equippedItemLocked(*ch, SlotWeapon)
	if !ok {
		return fmt.Errorf("item %s cannot be used", map[bool]string{true: "战神油", false: "修复油"}[super])
	}
	if weapon.DuraMax == 0 {
		item, ok := w.data.Items[weapon.ItemID]
		if !ok {
			return fmt.Errorf("item %s cannot be used", map[bool]string{true: "战神油", false: "修复油"}[super])
		}
		weapon.DuraMax = itemDuraMax(item)
	}
	if super {
		weapon.Dura = weapon.DuraMax
	} else {
		if weapon.DuraMax <= weapon.Dura {
			return fmt.Errorf("item %s cannot be used", "修复油")
		}
		weapon.DuraMax -= (weapon.DuraMax - weapon.Dura) / 30
		delta := minInt(5000, int(weapon.DuraMax-weapon.Dura))
		if delta <= 0 {
			return fmt.Errorf("item %s cannot be used", "修复油")
		}
		weapon.Dura += uint16(delta)
	}
	w.setEquippedItemLocked(ch, SlotWeapon, weapon)
	return nil
}

func itemDuraMax(item data.StdItem) uint16 {
	if item.DuraMax <= 0 {
		return 0
	}
	if item.DuraMax > 0xFFFF {
		return 0xFFFF
	}
	return uint16(item.DuraMax)
}
