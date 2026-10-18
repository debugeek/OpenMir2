package world

import (
	"fmt"
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
		ch.DrugUnlocked = true
	default:
		ch = core.QueueRecovery(ch, item.Stats.AcMin, item.Stats.MacMin)
	}
	_ = entry
	return ch, nil
}

func applyStdMode3Use(w *World, ch storage.Character, entry storage.UserItem, item data.StdItem) (storage.Character, []ItemUseSystemMessage, bool, bool, error) {
	_ = entry
	switch item.Shape {
	case 1:
		next, err := w.homeTeleportRandomHomeMapLocked(ch)
		return next, nil, false, true, err
	case 3:
		next, err := w.homeTeleportCharacterLocked(ch)
		return next, nil, false, true, err
	case 5:
		return ch, nil, false, false, nil
	case 2:
		if mp, ok := w.data.Maps[ch.MapID]; ok && mp.NoRandomMove {
			return ch, nil, false, false, fmt.Errorf("random teleport is not allowed on this map")
		}
		next, err := w.prepareRandomTeleportInMapLocked(ch, ch.MapID)
		return next, nil, false, true, err
	case 12:
		now := time.Now()
		duration := time.Duration(item.Stats.MacMax) * time.Second
		abilityChanged := applyTemporaryAbilityLocked(&ch, tempAbility{
			DC:       uint16(item.Stats.DcMin),
			MC:       uint16(item.Stats.McMin),
			SC:       uint16(item.Stats.ScMin),
			HitSpeed: uint16(item.Stats.AcMax),
			HP:       uint16(item.Stats.AcMin),
			MP:       uint16(item.Stats.MacMin),
			Duration: duration,
			Now:      now,
		})
		messages := make([]ItemUseSystemMessage, 0, 6)
		seconds := item.Stats.MacMax
		if item.Stats.DcMin > 0 {
			messages = append(messages, ItemUseSystemMessage{Text: fmt.Sprintf("Temporarily destructive power increased during %dsec.", seconds), Foreground: 0xDB, Background: 0xFF})
		}
		if item.Stats.McMin > 0 {
			messages = append(messages, ItemUseSystemMessage{Text: fmt.Sprintf("Temporarily magic power increased during %dsec.", seconds), Foreground: 0xDB, Background: 0xFF})
		}
		if item.Stats.ScMin > 0 {
			messages = append(messages, ItemUseSystemMessage{Text: fmt.Sprintf("Temporarily zen power increased during %dsec.", seconds), Foreground: 0xDB, Background: 0xFF})
		}
		if item.Stats.AcMax > 0 {
			messages = append(messages, ItemUseSystemMessage{Text: fmt.Sprintf("Temporarily hitting speed increased during %dsec.", seconds), Foreground: 0xDB, Background: 0xFF})
		}
		if item.Stats.AcMin > 0 {
			messages = append(messages, ItemUseSystemMessage{Text: fmt.Sprintf("Temporarily HP increased during %dsec.", seconds), Foreground: 0xDB, Background: 0xFF})
		}
		if item.Stats.MacMin > 0 {
			messages = append(messages, ItemUseSystemMessage{Text: fmt.Sprintf("Temporarily MP increased during %dsec.", seconds), Foreground: 0xDB, Background: 0xFF})
		}
		return ch, messages, abilityChanged, abilityChanged, nil
	case 4, 9, 10:
		return ch, nil, false, false, fmt.Errorf("item %s cannot be used", item.ID)
	default:
		return ch, nil, false, false, fmt.Errorf("item %s cannot be used", item.ID)
	}
}

func (w *World) useBlessingOilLocked(ch *storage.Character) (bool, string, []ItemUseSystemMessage, error) {
	weapon, ok := w.equippedItemLocked(*ch, SlotWeapon)
	if !ok {
		return false, "", nil, fmt.Errorf("item 祝福油 cannot be used")
	}
	desc := weapon.Desc
	item, ok := w.data.Items[weapon.ItemID]
	if !ok {
		return false, "", nil, fmt.Errorf("item 祝福油 cannot be used")
	}
	nRand := abs(int(item.Stats.DcMax)-int(item.Stats.DcMin)) / 5
	if w.rand.Intn(20) == 1 {
		if desc[3] > 0 {
			desc[3]--
			weapon.Desc = desc
			w.setEquippedItemLocked(ch, SlotWeapon, weapon)
			return true, "", []ItemUseSystemMessage{{Text: "你的武器被诅咒了.", Foreground: 0xFF, Background: 0x38}}, nil
		} else if desc[4] < 10 {
			desc[4]++
			weapon.Desc = desc
			w.setEquippedItemLocked(ch, SlotWeapon, weapon)
			return true, "", []ItemUseSystemMessage{{Text: "你的武器被诅咒了.", Foreground: 0xFF, Background: 0x38}}, nil
		}
		return true, "", nil, nil
	} else if desc[4] > 0 {
		desc[4]--
	} else if desc[3] < 1 {
		desc[3]++
	} else if desc[3] < 3 && w.rand.Intn(nRand+6) == 1 {
		desc[3]++
	} else if desc[3] < 7 && w.rand.Intn(nRand*10+30) == 1 {
		desc[3]++
	} else {
		return true, "无效.", nil, nil
	}
	weapon.Desc = desc
	w.setEquippedItemLocked(ch, SlotWeapon, weapon)
	return true, "武器被加幸运了...", nil, nil
}

func (w *World) repairWeaponLocked(ch *storage.Character, super bool) error {
	weapon, ok := w.equippedItemLocked(*ch, SlotWeapon)
	if !ok {
		return fmt.Errorf("item %s cannot be used", map[bool]string{true: "战神油", false: "修复油"}[super])
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

func (w *World) equippedItemDurabilityLocked(ch storage.Character, slot int) *SpellDurability {
	item, ok := w.equippedItemLocked(ch, slot)
	if !ok {
		return nil
	}
	return &SpellDurability{Slot: slot, Dura: item.Dura, DuraMax: item.DuraMax}
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
