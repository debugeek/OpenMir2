package world

import (
	"math"
	"strings"
	"time"

	"openmir2/internal/storage"
)

const (
	levelValueOfWarrHP       = 4
	levelValueOfWarrHPRate   = 4.5
	levelValueOfWizardHP     = 15
	levelValueOfWizardHPRate = 1.8
	levelValueOfTaosHP       = 6
	levelValueOfTaosHPRate   = 2.5
	levelValueOfTaosMP       = 8
)

type LevelAbilities struct {
	MaxHP, MaxMP                            int
	AC, ACMax                               int
	MAC, MACMax                             int
	DC, DCMax                               int
	MC, MCMax                               int
	SC, SCMax                               int
	MaxWeight, MaxWearWeight, MaxHandWeight int
}

func Base(class string, level int) LevelAbilities {
	if level <= 0 {
		level = 1
	}
	l := float64(level)
	var a LevelAbilities
	switch NormalizeClass(class) {
	case "taoist":
		a.MaxHP = 14 + iround((l/levelValueOfTaosHP+levelValueOfTaosHPRate)*l)
		a.MaxMP = 13 + iround(l/levelValueOfTaosMP*2.2*l)
		a.MaxWeight = 50 + iround(l/4*l)
		a.MaxWearWeight = minInt(255, 15+iround(l/50*l))
		if uncapped := 12 + iround(l/13*l); uncapped > 255 {
			a.MaxHandWeight = 255
		} else {
			a.MaxHandWeight = 12 + iround(l/42*l)
		}
		n := level / 7
		a.DC, a.DCMax = clampByte(maxInt(n-1, 0)), clampByte(maxInt(1, n))
		a.SC, a.SCMax = a.DC, a.DCMax
		n6 := iround(l / 6)
		a.MAC, a.MACMax = clampByte(n6/2), clampByte(n6+1)
	case "wizard":
		a.MaxHP = 14 + iround((l/levelValueOfWizardHP+levelValueOfWizardHPRate)*l)
		a.MaxMP = 13 + iround((l/5+2)*2.2*l)
		a.MaxWeight = 50 + iround(l/5*l)
		a.MaxWearWeight = minInt(255, 15+iround(l/100*l))
		a.MaxHandWeight = 12 + iround(l/90*l)
		n := level / 7
		a.DC, a.DCMax = clampByte(maxInt(n-1, 0)), clampByte(maxInt(1, n))
		a.MC, a.MCMax = a.DC, a.DCMax
	default:
		a.MaxHP = 14 + iround((l/levelValueOfWarrHP+levelValueOfWarrHPRate+l/20)*l)
		a.MaxMP = 11 + iround(l*3.5)
		a.MaxWeight = 50 + iround(l/3*l)
		a.MaxWearWeight = minInt(255, 15+iround(l/20*l))
		a.MaxHandWeight = 12 + iround(l/13*l)
		n5 := level / 5
		a.DC, a.DCMax = clampByte(maxInt(n5-1, 1)), clampByte(maxInt(1, n5))
		a.AC, a.ACMax = 0, clampByte(level/7)
	}
	a.MaxHP = minInt(65535, a.MaxHP)
	a.MaxMP = minInt(65535, a.MaxMP)
	return a
}

func NormalizeClass(class string) string {
	switch strings.ToLower(class) {
	case "wizard", "mage":
		return "wizard"
	case "taoist", "tao":
		return "taoist"
	default:
		return "warrior"
	}
}

func iround(v float64) int {
	return int(math.Round(v))
}

func clampByte(v int) int {
	return minInt(255, maxInt(v, 0))
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func PackWord(baseLow, baseHigh, itemLow, itemHigh int) int {
	low := clampByte(baseLow + itemLow)
	high := clampByte(baseHigh + itemHigh)
	return low | high<<8
}

type Abilities struct {
	MaxHP, MaxMP              int
	AC, MAC, DC, MC, SC       int
	Weight, MaxWeight         int
	WearWeight, MaxWearWeight int
	HandWeight, MaxHandWeight int
}

type AbilityStats struct {
	Level         int
	AC, MAC       int
	DC, MC, SC    int
	HP, MP        int
	MaxHP, MaxMP  int
	Exp, MaxExp   int
	Weight        int
	MaxWeight     int
	WearWeight    int
	MaxWearWeight int
	HandWeight    int
	MaxHandWeight int
}

func addHighByte(word, add int) int {
	low := word & 0xFF
	high := (word >> 8) & 0xFF
	high = minInt(255, high+add)
	return low | (high << 8)
}

func (w *World) Abilities(ch storage.Character) Abilities {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.normalizeEquippedItemsLocked(&ch)
	level := ch.Level
	if level <= 0 {
		level = 1
	}
	base := Base(ch.Class, level)
	item := w.combatStatsLocked(ch)
	extra := activeTemporaryAbilities(ch, time.Now())
	ac := PackWord(base.AC, base.ACMax, item.AC, item.ACMax)
	mac := PackWord(base.MAC, base.MACMax, item.MAC, item.MACMax)
	dc := PackWord(base.DC, base.DCMax, item.DC, item.DCMax)
	mc := PackWord(base.MC, base.MCMax, item.MC, item.MCMax)
	sc := PackWord(base.SC, base.SCMax, item.SC, item.SCMax)
	dc = addHighByte(dc, int(extra[0]))
	mc = addHighByte(mc, int(extra[1]))
	sc = addHighByte(sc, int(extra[2]))
	if extra[4] > 0 {
		base.MaxHP = minInt(65535, base.MaxHP+int(extra[4]))
	}
	if extra[5] > 0 {
		base.MaxMP = minInt(65535, base.MaxMP+int(extra[5]))
	}
	var wear, hand int
	for slot, itemID := range w.equippedSlotMapLocked(ch) {
		if it, ok := w.data.Items[itemID]; ok {
			switch slot {
			case SlotDress:
				wear = it.Weight
			case SlotWeapon:
				hand = it.Weight
			}
		}
	}
	bagWeight := 0
	for _, entry := range ch.BagItems {
		if it, ok := w.data.Items[entry.ItemID]; ok {
			bagWeight += it.Weight
		}
	}
	return Abilities{
		MaxHP:         base.MaxHP,
		MaxMP:         base.MaxMP,
		AC:            ac,
		MAC:           mac,
		DC:            dc,
		MC:            mc,
		SC:            sc,
		Weight:        bagWeight,
		MaxWeight:     base.MaxWeight,
		MaxWearWeight: base.MaxWearWeight,
		MaxHandWeight: base.MaxHandWeight,
		WearWeight:    wear,
		HandWeight:    hand,
	}
}

type CombatStats struct {
	AC, ACMax   int
	MAC, MACMax int
	DC, DCMax   int
	MC, MCMax   int
	SC, SCMax   int
}

func (w *World) CombatStats(ch storage.Character) CombatStats {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.normalizeEquippedItemsLocked(&ch)
	return w.combatStatsLocked(ch)
}

func (w *World) AbilityStats(ch storage.Character) AbilityStats {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.normalizeEquippedItemsLocked(&ch)
	level := ch.Level
	if level <= 0 {
		level = 1
	}
	base := Base(ch.Class, level)
	item := w.combatStatsLocked(ch)
	extra := activeTemporaryAbilities(ch, time.Now())
	ac := PackWord(base.AC, base.ACMax, item.AC, item.ACMax)
	mac := PackWord(base.MAC, base.MACMax, item.MAC, item.MACMax)
	dc := PackWord(base.DC, base.DCMax, item.DC, item.DCMax)
	mc := PackWord(base.MC, base.MCMax, item.MC, item.MCMax)
	sc := PackWord(base.SC, base.SCMax, item.SC, item.SCMax)
	dc = addHighByte(dc, int(extra[0]))
	mc = addHighByte(mc, int(extra[1]))
	sc = addHighByte(sc, int(extra[2]))
	if extra[4] > 0 {
		base.MaxHP = minInt(65535, base.MaxHP+int(extra[4]))
	}
	if extra[5] > 0 {
		base.MaxMP = minInt(65535, base.MaxMP+int(extra[5]))
	}
	var wear, hand int
	for slot, itemID := range w.equippedSlotMapLocked(ch) {
		if it, ok := w.data.Items[itemID]; ok {
			switch slot {
			case SlotDress:
				wear = it.Weight
			case SlotWeapon:
				hand = it.Weight
			}
		}
	}
	bagWeight := 0
	for _, entry := range ch.BagItems {
		if it, ok := w.data.Items[entry.ItemID]; ok {
			bagWeight += it.Weight
		}
	}
	hp := ch.HP
	if hp <= 0 || hp > base.MaxHP {
		hp = base.MaxHP
	}
	mp := ch.MP
	if mp < 0 || mp > base.MaxMP {
		mp = base.MaxMP
	}
	return AbilityStats{
		Level:         level,
		AC:            ac,
		MAC:           mac,
		DC:            dc,
		MC:            mc,
		SC:            sc,
		HP:            hp,
		MP:            mp,
		MaxHP:         base.MaxHP,
		MaxMP:         base.MaxMP,
		Exp:           ch.Experience,
		MaxExp:        w.RequiredExperience(level),
		Weight:        bagWeight,
		MaxWeight:     base.MaxWeight,
		WearWeight:    wear,
		MaxWearWeight: base.MaxWearWeight,
		HandWeight:    hand,
		MaxHandWeight: base.MaxHandWeight,
	}
}

func (w *World) combatStatsLocked(ch storage.Character) CombatStats {
	var stats CombatStats
	for slot := 0; slot < useSlotCount; slot++ {
		itemEntry, ok := w.equippedItemLocked(ch, slot)
		if !ok {
			continue
		}
		item, ok := w.data.Items[itemEntry.ItemID]
		if !ok {
			continue
		}
		item = UpgradeClientItemForDisplay(item, itemEntry, false)
		stats.AC += int(byte(item.Stats.AcMin))
		stats.ACMax += int(byte(item.Stats.AcMin >> 8))
		stats.MAC += int(byte(item.Stats.MacMin))
		stats.MACMax += int(byte(item.Stats.MacMin >> 8))
		stats.DC += int(byte(item.Stats.DcMin))
		stats.DCMax += int(byte(item.Stats.DcMin >> 8))
		stats.MC += int(byte(item.Stats.McMin))
		stats.MCMax += int(byte(item.Stats.McMin >> 8))
		stats.SC += int(byte(item.Stats.ScMin))
		stats.SCMax += int(byte(item.Stats.ScMin >> 8))
	}
	return stats
}

type tempAbility struct {
	DC       byte
	MC       byte
	SC       byte
	HitSpeed byte
	HP       byte
	MP       byte
	Duration time.Duration
	Now      time.Time
}

const tempAbilityCount = 7

func applyTemporaryAbilityLocked(ch *storage.Character, bonus tempAbility) bool {
	if bonus.Now.IsZero() {
		bonus.Now = time.Now()
	}
	expires := bonus.Now.Add(bonus.Duration).UnixNano()
	changed := false
	apply := func(slot int, value byte) {
		if value == 0 {
			return
		}
		if ch.ExtraAbil[slot] < uint16(value) {
			ch.ExtraAbil[slot] = uint16(value)
			changed = true
		}
		if ch.ExtraAbilTimes[slot] < expires {
			ch.ExtraAbilTimes[slot] = expires
			changed = true
		}
	}
	apply(0, bonus.DC)
	apply(1, bonus.MC)
	apply(2, bonus.SC)
	apply(3, bonus.HitSpeed)
	apply(4, bonus.HP)
	apply(5, bonus.MP)
	return changed
}

func activeTemporaryAbilities(ch storage.Character, now time.Time) [tempAbilityCount]uint16 {
	var out [tempAbilityCount]uint16
	if now.IsZero() {
		now = time.Now()
	}
	expires := now.UnixNano()
	for i := 0; i < tempAbilityCount && i < len(ch.ExtraAbil); i++ {
		if ch.ExtraAbil[i] == 0 {
			continue
		}
		if ch.ExtraAbilTimes[i] > 0 && ch.ExtraAbilTimes[i] < expires {
			continue
		}
		out[i] = ch.ExtraAbil[i]
	}
	return out
}
