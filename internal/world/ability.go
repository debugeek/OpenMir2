package world

import (
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
		a.MaxWearWeight = 15 + iround(l/50*l)
		a.MaxHandWeight = 12 + iround(l/42*l)
		n := level / 7
		a.DC, a.DCMax = clampByte(maxInt(n-1, 0)), clampByte(maxInt(1, n))
		a.SC, a.SCMax = a.DC, a.DCMax
		n6 := iround(l / 6)
		a.MAC, a.MACMax = clampByte(n6/2), clampByte(n6+1)
	case "wizard":
		a.MaxHP = 14 + iround((l/levelValueOfWizardHP+levelValueOfWizardHPRate)*l)
		a.MaxMP = 13 + iround((l/5+2)*2.2*l)
		a.MaxWeight = 50 + iround(l/5*l)
		a.MaxWearWeight = 15 + iround(l/100*l)
		a.MaxHandWeight = 12 + iround(l/90*l)
		n := level / 7
		a.DC, a.DCMax = clampByte(maxInt(n-1, 0)), clampByte(maxInt(1, n))
		a.MC, a.MCMax = a.DC, a.DCMax
	default:
		a.MaxHP = 14 + iround((l/levelValueOfWarrHP+levelValueOfWarrHPRate+l/20)*l)
		a.MaxMP = 11 + iround(l*3.5)
		a.MaxWeight = 50 + iround(l/3*l)
		a.MaxWearWeight = 15 + iround(l/20*l)
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
	return referenceRound(v)
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

type SubAbilityStats struct {
	AntiMagic     int
	HitPoint      int
	SpeedPoint    int
	AntiPoison    int
	PoisonRecover int
	HealthRecover int
	SpellRecover  int
}

func (w *World) SubAbilityStats(ch storage.Character) SubAbilityStats {
	w.mu.Lock()
	defer w.mu.Unlock()
	combat := w.combatStatsLocked(ch)
	bonusHit, bonusSpeed := w.bonusHitSpeedLocked(ch)
	hitPoint := 5 + combat.Hit + bonusHit
	if ch.ExtraAbil[3] > 0 {
		hitPoint += int(ch.ExtraAbil[3])
	}
	speedPoint := int(SubAbilitySpeed(ch.Class)) + combat.Speed + bonusSpeed
	if ch.ExtraAbil[3] > 0 {
		speedPoint += int(ch.ExtraAbil[3])
	}
	if speedPoint < 1 {
		speedPoint = 1
	}
	antiMagic := 1
	antiPoison := ch.AntiPoison
	poisonRecover := 0
	healthRecover := 0
	spellRecover := 0
	for slot := 0; slot < useSlotCount; slot++ {
		entry, ok := w.activeEquippedItemLocked(ch, slot)
		if !ok {
			continue
		}
		if item, ok := w.data.Items[entry.ItemID]; ok {
			display := UpgradeClientItemForDisplay(item, entry, false)
			if display.StdMode == 25 {
				continue
			}
			antiMagic += display.MgAvoid
			antiPoison += display.ToxAvoid
			switch display.StdMode {
			case 19:
				antiMagic += display.Stats.AcMax
			case 20, 24:
				hitPoint += display.Stats.AcMax
				speedPoint += display.Stats.MacMax
			case 21:
				healthRecover += display.Stats.AcMax
				spellRecover += display.Stats.MacMax
			case 23:
				antiPoison += display.Stats.AcMax
				poisonRecover += display.Stats.MacMax
			}
		}
	}
	return SubAbilityStats{
		AntiMagic:     antiMagic,
		HitPoint:      hitPoint,
		SpeedPoint:    speedPoint,
		AntiPoison:    antiPoison,
		PoisonRecover: poisonRecover,
		HealthRecover: healthRecover,
		SpellRecover:  spellRecover,
	}
}

func (w *World) bonusHitSpeedLocked(ch storage.Character) (int, int) {
	settings := w.gameplay.Bonus.Warrior
	switch NormalizeClass(ch.Class) {
	case "wizard":
		settings = w.gameplay.Bonus.Wizard
	case "taoist":
		settings = w.gameplay.Bonus.Taoist
	}
	hitTick, speedTick := settings.Bonus.Hit, settings.Bonus.Speed
	if hitTick <= 0 {
		hitTick = 1
	}
	if speedTick <= 0 {
		speedTick = 1
	}
	return ch.BonusAbil.Hit / hitTick, ch.BonusAbil.Speed / speedTick
}

func addHighByte(word, add int) int {
	low := word & 0xFF
	high := (word >> 8) & 0xFF
	high = minInt(255, high+add)
	return low | (high << 8)
}

func temporaryCombatBonus(value uint16) int {
	if value == 0 {
		return 0
	}
	return 2 + int(value)
}

func (w *World) Abilities(ch storage.Character) Abilities {
	w.mu.Lock()
	defer w.mu.Unlock()
	level := ch.Level
	if level <= 0 {
		level = 1
	}
	base := Base(ch.Class, level)
	if w.characterHasMuscleRingLocked(ch) {
		base.MaxWeight *= 2
		base.MaxWearWeight *= 2
		base.MaxHandWeight *= 2
	}
	_, _, bonusHP, bonusMP := w.bonusCombatValuesLocked(ch)
	base.MaxHP = minInt(65535, base.MaxHP+bonusHP)
	base.MaxMP = minInt(65535, base.MaxMP+bonusMP)
	item := w.combatStatsLocked(ch)
	extra := activeTemporaryAbilities(ch)
	ac := PackWord(base.AC, base.ACMax, item.AC, item.ACMax)
	mac := PackWord(base.MAC, base.MACMax, item.MAC, item.MACMax)
	dc := PackWord(base.DC, base.DCMax, item.DC, item.DCMax)
	mc := PackWord(base.MC, base.MCMax, item.MC, item.MCMax)
	sc := PackWord(base.SC, base.SCMax, item.SC, item.SCMax)
	defenceBonus, magicDefenceBonus, _, _ := activeProtectionBuffs(ch, time.Now())
	if defenceBonus > 0 {
		ac = addHighByte(ac, defenceBonus)
	}
	if magicDefenceBonus > 0 {
		mac = addHighByte(mac, magicDefenceBonus)
	}
	if extra[4] > 0 {
		base.MaxHP = minInt(65535, base.MaxHP+int(extra[4]))
	}
	if extra[5] > 0 {
		base.MaxMP = minInt(65535, base.MaxMP+int(extra[5]))
	}
	base.MaxHP, base.MaxMP = w.applyMagicBloodSuiteLocked(ch, base.MaxHP, base.MaxMP)
	wear, hand := w.equippedWeightTotalsLocked(ch)
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
	Hit         int
	HitSpeed    int
	Speed       int
	Undead      int
	Luck        int
	UnLuck      int
}

func (w *World) equippedWeightTotalsLocked(ch storage.Character) (wear, hand int) {
	for slot := 0; slot < useSlotCount; slot++ {
		entry, ok := w.activeEquippedItemLocked(ch, slot)
		if !ok {
			continue
		}
		item, ok := w.data.Items[entry.ItemID]
		if !ok {
			continue
		}
		if slot == SlotWeapon || slot == SlotRightHand {
			hand += item.Weight
		} else {
			wear += item.Weight
		}
	}
	return wear, hand
}

func (w *World) CombatStats(ch storage.Character) CombatStats {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.combatStatsLocked(ch)
}

func (w *World) AbilityStats(ch storage.Character) AbilityStats {
	w.mu.Lock()
	defer w.mu.Unlock()
	level := ch.Level
	if level <= 0 {
		level = 1
	}
	base := Base(ch.Class, level)
	if w.characterHasMuscleRingLocked(ch) {
		base.MaxWeight *= 2
		base.MaxWearWeight *= 2
		base.MaxHandWeight *= 2
	}
	_, _, bonusHP, bonusMP := w.bonusCombatValuesLocked(ch)
	base.MaxHP = minInt(65535, base.MaxHP+bonusHP)
	base.MaxMP = minInt(65535, base.MaxMP+bonusMP)
	item := w.combatStatsLocked(ch)
	extra := activeTemporaryAbilities(ch)
	ac := PackWord(base.AC, base.ACMax, item.AC, item.ACMax)
	mac := PackWord(base.MAC, base.MACMax, item.MAC, item.MACMax)
	dc := PackWord(base.DC, base.DCMax, item.DC, item.DCMax)
	mc := PackWord(base.MC, base.MCMax, item.MC, item.MCMax)
	sc := PackWord(base.SC, base.SCMax, item.SC, item.SCMax)
	defenceBonus, magicDefenceBonus, _, _ := activeProtectionBuffs(ch, time.Now())
	if defenceBonus > 0 {
		ac = addHighByte(ac, defenceBonus)
	}
	if magicDefenceBonus > 0 {
		mac = addHighByte(mac, magicDefenceBonus)
	}
	if extra[4] > 0 {
		base.MaxHP = minInt(65535, base.MaxHP+int(extra[4]))
	}
	if extra[5] > 0 {
		base.MaxMP = minInt(65535, base.MaxMP+int(extra[5]))
	}
	base.MaxHP, base.MaxMP = w.applyMagicBloodSuiteLocked(ch, base.MaxHP, base.MaxMP)
	wear, hand := w.equippedWeightTotalsLocked(ch)
	bagWeight := 0
	for _, entry := range ch.BagItems {
		if it, ok := w.data.Items[entry.ItemID]; ok {
			bagWeight += it.Weight
		}
	}
	hp := ch.HP
	if hp < 0 || hp > base.MaxHP {
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

func (w *World) characterHasEquipmentShapeLocked(ch storage.Character, shape int) bool {
	for slot := 0; slot < useSlotCount; slot++ {
		entry, ok := w.activeEquippedItemLocked(ch, slot)
		if !ok {
			continue
		}
		item, ok := w.data.Items[entry.ItemID]
		if !ok {
			continue
		}
		if item.Shape == shape && slot == SlotNecklace {
			return true
		}
		if item.AniCount == shape && (slot == SlotDress || slot == SlotWeapon || slot == SlotRightHand) {
			return true
		}
	}
	return false
}

func (w *World) characterHasMuscleRingLocked(ch storage.Character) bool {
	for slot := 0; slot < useSlotCount; slot++ {
		entry, ok := w.activeEquippedItemLocked(ch, slot)
		if !ok {
			continue
		}
		item, ok := w.data.Items[entry.ItemID]
		if !ok {
			continue
		}
		if (item.Shape == 119 && (slot == SlotRingL || slot == SlotRingR)) ||
			(item.AniCount == 119 && (slot == SlotDress || slot == SlotWeapon || slot == SlotRightHand)) ||
			(item.Shape == 153 || item.Shape == 156) && (slot != SlotDress && slot != SlotWeapon && slot != SlotRightHand) ||
			(slot == SlotDress || slot == SlotWeapon || slot == SlotRightHand) && (item.AniCount == 153 || item.AniCount == 156) {
			return true
		}
	}
	return false
}

func (w *World) magicBloodSuiteValueLocked(ch storage.Character) int {
	value := 0
	haveNecklace, haveRing, haveArmRing := false, false, false
	for slot, shape := range map[int]int{
		SlotNecklace: 135,
		SlotRingL:    133,
		SlotRingR:    133,
		SlotArmRingL: 134,
		SlotArmRingR: 134,
	} {
		entry, ok := w.activeEquippedItemLocked(ch, slot)
		if !ok {
			continue
		}
		item, ok := w.data.Items[entry.ItemID]
		if !ok || item.Shape != shape {
			continue
		}
		value += item.AniCount
		switch shape {
		case 135:
			haveNecklace = true
		case 133:
			haveRing = true
		case 134:
			haveArmRing = true
		}
	}
	if haveNecklace && haveRing && haveArmRing {
		value += 50
	}
	return value
}

func (w *World) applyMagicBloodSuiteLocked(ch storage.Character, maxHP, maxMP int) (int, int) {
	value := w.magicBloodSuiteValueLocked(ch)
	if value <= 0 {
		return maxHP, maxMP
	}
	if maxMP <= value {
		value = maxMP - 1
		if value < 0 {
			value = 0
		}
	}
	maxMP -= value
	maxHP = minInt(65535, maxHP+value)
	return maxHP, maxMP
}

func (w *World) hasRainbowSuiteLocked(ch storage.Character) bool {
	if w.data.Items == nil || ch.EquippedItems == nil {
		return false
	}
	hasShape := func(slot int, shape int) bool {
		entry, ok := w.activeEquippedItemLocked(ch, slot)
		if !ok {
			return false
		}
		item, ok := w.data.Items[entry.ItemID]
		return ok && item.Shape == shape
	}
	return hasShape(SlotNecklace, 138) &&
		(hasShape(SlotRingL, 136) || hasShape(SlotRingR, 136)) &&
		(hasShape(SlotArmRingL, 137) || hasShape(SlotArmRingR, 137))
}

func (w *World) HasProbeNecklace(ch storage.Character) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	for slot := 0; slot < useSlotCount; slot++ {
		entry, ok := w.activeEquippedItemLocked(ch, slot)
		if !ok {
			continue
		}
		item, ok := w.data.Items[entry.ItemID]
		if !ok {
			continue
		}
		if (item.AniCount == 121 || item.AniCount == 159) &&
			(slot == SlotDress || slot == SlotWeapon || slot == SlotRightHand) {
			return true
		}
		if slot == SlotNecklace && item.Shape == 121 {
			return true
		}
		if slot != SlotDress && slot != SlotWeapon && slot != SlotRightHand && item.Shape == 159 {
			return true
		}
	}
	return false
}

func (w *World) skillTrainingMultiplierLocked(ch storage.Character) int {
	if w.characterHasEquipmentShapeLocked(ch, 120) {
		return 3
	}
	return 1
}

func (w *World) combatStatsLocked(ch storage.Character) CombatStats {
	var stats CombatStats
	for slot := 0; slot < useSlotCount; slot++ {
		itemEntry, ok := w.activeEquippedItemLocked(ch, slot)
		if !ok {
			continue
		}
		item, ok := w.data.Items[itemEntry.ItemID]
		if !ok {
			continue
		}
		item = UpgradeClientItemForDisplay(item, itemEntry, false)
		if item.StdMode == 25 || item.StdMode == 30 {
			continue
		}
		if item.StdMode != 5 && item.StdMode != 6 && item.StdMode != 15 && item.StdMode != 19 && item.StdMode != 20 && item.StdMode != 21 && item.StdMode != 22 && item.StdMode != 23 && item.StdMode != 24 && item.StdMode != 26 {
			stats.AC += int(byte(item.Stats.AcMin))
			stats.ACMax += int(byte(item.Stats.AcMin >> 8))
			stats.MAC += int(byte(item.Stats.MacMin))
			stats.MACMax += int(byte(item.Stats.MacMin >> 8))
		}
		stats.DC += int(byte(item.Stats.DcMin))
		stats.DCMax += int(byte(item.Stats.DcMin >> 8))
		stats.MC += int(byte(item.Stats.McMin))
		stats.MCMax += int(byte(item.Stats.McMin >> 8))
		stats.SC += int(byte(item.Stats.ScMin))
		stats.SCMax += int(byte(item.Stats.ScMin >> 8))
		stats.Hit += item.Accurate
		stats.HitSpeed += item.AtkSpd
		stats.Speed += item.Agility
		stats.Undead += item.Undead
		if item.StdMode == 5 || item.StdMode == 6 {
			stats.Hit += int(byte(item.Stats.AcMin >> 8))
			weaponSpeed := int(byte(item.Stats.MacMin >> 8))
			if weaponSpeed > 10 {
				stats.HitSpeed += weaponSpeed - 10
			} else {
				stats.HitSpeed -= weaponSpeed
			}
		}
		if item.StdMode == 5 || item.StdMode == 6 {
			stats.Luck += int(byte(item.Stats.AcMin))
			stats.UnLuck += int(byte(item.Stats.MacMin))
		}
		if item.StdMode == 19 {
			stats.Luck += int(byte(item.Stats.MacMin >> 8))
			stats.UnLuck += int(byte(item.Stats.MacMin))
		}
		if item.StdMode == 21 || item.StdMode == 23 {
			stats.HitSpeed += int(byte(item.Stats.AcMin))
			stats.HitSpeed -= int(byte(item.Stats.MacMin))
		}
	}
	stats.Luck -= stats.UnLuck
	bonusLow, bonusHigh, _, _ := w.bonusCombatValuesLocked(ch)
	stats.DC += bonusLow[0]
	stats.DCMax += bonusHigh[0]
	stats.MC += bonusLow[1]
	stats.MCMax += bonusHigh[1]
	stats.SC += bonusLow[2]
	stats.SCMax += bonusHigh[2]
	stats.AC += bonusLow[3]
	stats.ACMax += bonusHigh[3]
	stats.MAC += bonusLow[4]
	stats.MACMax += bonusHigh[4]
	if w.hasSpiritSuiteLocked(ch) {
		stats.DC += 2
		stats.DCMax += 7
		stats.HitSpeed += 2
	}
	if w.hasRainbowSuiteLocked(ch) {
		stats.Hit += 2
	}
	extra := activeTemporaryAbilities(ch)
	stats.DCMax += temporaryCombatBonus(extra[0])
	stats.MCMax += temporaryCombatBonus(extra[1])
	stats.SCMax += temporaryCombatBonus(extra[2])
	if state, _, ok := ch.Skills.Get("基本剑术"); ok {
		bonus := int(state.Level) * 3
		stats.Hit += bonus
	}
	if state, _, ok := ch.Skills.Get("精神力战法"); ok {
		stats.Hit += (int(state.Level)*8 + 1) / 3
	}
	return stats
}

func (w *World) bonusCombatValuesLocked(ch storage.Character) ([5]int, [5]int, int, int) {
	settings := w.gameplay.Bonus.Warrior
	switch NormalizeClass(ch.Class) {
	case "wizard":
		settings = w.gameplay.Bonus.Wizard
	case "taoist":
		settings = w.gameplay.Bonus.Taoist
	}
	allocated := [5]int{ch.BonusAbil.DC, ch.BonusAbil.MC, ch.BonusAbil.SC, ch.BonusAbil.AC, ch.BonusAbil.MAC}
	ticks := [5]int{settings.Bonus.DC, settings.Bonus.MC, settings.Bonus.SC, settings.Bonus.AC, settings.Bonus.MAC}
	naked := [5]int{settings.Naked.DC, settings.Naked.MC, settings.Naked.SC, settings.Naked.AC, settings.Naked.MAC}
	low, high := [5]int{}, [5]int{}
	for index := range allocated {
		if ticks[index] <= 0 {
			ticks[index] = 1
		}
		value := allocated[index] / ticks[index]
		lo, hi := naked[index]&0xFF, (naked[index]>>8)&0xFF
		for step := 0; step < value; step++ {
			if lo+1 < hi {
				lo++
				low[index]++
			} else {
				hi++
				high[index]++
			}
		}
	}
	hpTick, mpTick := settings.Bonus.HP, settings.Bonus.MP
	if hpTick <= 0 {
		hpTick = 1
	}
	if mpTick <= 0 {
		mpTick = 1
	}
	return low, high, ch.BonusAbil.HP / hpTick, ch.BonusAbil.MP / mpTick
}

type tempAbility struct {
	DC       uint16
	MC       uint16
	SC       uint16
	HitSpeed uint16
	HP       uint16
	MP       uint16
	Duration time.Duration
	Now      time.Time
}

const tempAbilityCount = 6

func applyTemporaryAbilityLocked(ch *storage.Character, bonus tempAbility) bool {
	if bonus.Now.IsZero() {
		bonus.Now = time.Now()
	}
	expires := bonus.Now.Add(bonus.Duration).UnixNano()
	changed := false
	apply := func(slot int, value uint16) {
		if value == 0 {
			return
		}
		ch.ExtraAbil[slot] = value
		ch.ExtraAbilTimes[slot] = expires
		changed = true
	}
	apply(0, bonus.DC)
	apply(1, bonus.MC)
	apply(2, bonus.SC)
	apply(3, bonus.HitSpeed)
	apply(4, bonus.HP)
	apply(5, bonus.MP)
	return changed
}

func activeTemporaryAbilities(ch storage.Character) [tempAbilityCount]uint16 {
	var out [tempAbilityCount]uint16
	for i := 0; i < tempAbilityCount && i < len(ch.ExtraAbil); i++ {
		if ch.ExtraAbil[i] == 0 {
			continue
		}
		out[i] = ch.ExtraAbil[i]
	}
	return out
}

func activeProtectionBuffs(ch storage.Character, now time.Time) (defenceBonus, magicDefenceBonus int, bubbleLevel byte, bubbleActive bool) {
	if ch.DefenceUpUntil > 0 {
		defenceBonus = 2 + maxInt(ch.Level, 1)/7
	}
	if ch.MagDefenceUpUntil > 0 {
		magicDefenceBonus = 2 + maxInt(ch.Level, 1)/7
	}
	if ch.BubbleDefenceUntil > 0 && (ch.BubbleDefenceActive == nil || *ch.BubbleDefenceActive) {
		bubbleLevel = ch.BubbleDefenceLevel
		bubbleActive = true
	}
	return
}

func activeMonsterProtectionBuffs(mon *Monster, now time.Time) (defenceBonus, magicDefenceBonus int) {
	if mon == nil {
		return 0, 0
	}
	if mon.DefenceUpUntil > 0 {
		defenceBonus = 2 + maxInt(mon.Level, 1)/7
	}
	if mon.MagDefenceUpUntil > 0 {
		magicDefenceBonus = 2 + maxInt(mon.Level, 1)/7
	}
	return
}
