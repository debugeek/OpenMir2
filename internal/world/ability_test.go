package world

import (
	"testing"
	"time"

	"openmir2/internal/config"
	"openmir2/internal/data"
	"openmir2/internal/storage"
)

func TestBaseWarriorLevel1(t *testing.T) {
	got := Base("warrior", 1)
	want := LevelAbilities{
		MaxHP: 19, MaxMP: 15,
		DC: 1, DCMax: 1,
		MaxWeight: 50, MaxWearWeight: 15, MaxHandWeight: 12,
	}
	if got != want {
		t.Fatalf("Base(warrior, 1) = %+v, want %+v", got, want)
	}
}

func TestBaseWizardLevel1(t *testing.T) {
	got := Base("wizard", 1)
	want := LevelAbilities{
		MaxHP: 16, MaxMP: 18,
		DC: 0, DCMax: 1,
		MC: 0, MCMax: 1,
		MaxWeight: 50, MaxWearWeight: 15, MaxHandWeight: 12,
	}
	if got != want {
		t.Fatalf("Base(wizard, 1) = %+v, want %+v", got, want)
	}
}

func TestBaseTaoistLevel1(t *testing.T) {
	got := Base("taoist", 1)
	want := LevelAbilities{
		MaxHP: 17, MaxMP: 13,
		DC: 0, DCMax: 1,
		SC: 0, SCMax: 1,
		MAC: 0, MACMax: 1,
		MaxWeight: 50, MaxWearWeight: 15, MaxHandWeight: 12,
	}
	if got != want {
		t.Fatalf("Base(taoist, 1) = %+v, want %+v", got, want)
	}
}

func TestBaseWeightLimitsKeepReferenceInternalValues(t *testing.T) {
	warrior := Base("warrior", 100)
	if warrior.MaxWearWeight != 515 || warrior.MaxHandWeight != 781 {
		t.Fatalf("warrior weight limits = %d/%d, want 515/781", warrior.MaxWearWeight, warrior.MaxHandWeight)
	}
	taoist := Base("taoist", 100)
	if taoist.MaxWearWeight != 215 || taoist.MaxHandWeight != 250 {
		t.Fatalf("taoist weight limits = %d/%d, want 215/250", taoist.MaxWearWeight, taoist.MaxHandWeight)
	}
}

func TestSkillTrainingMultiplierUsesTechniqueNecklace(t *testing.T) {
	w := &World{data: data.StdBundle{Items: map[string]data.StdItem{
		"technique-necklace": {ID: "technique-necklace", Shape: 120},
	}}}
	without := storage.Character{}
	with := storage.Character{EquippedItems: map[int]storage.UserItem{
		SlotNecklace: {ItemID: "technique-necklace"},
	}}
	if got := w.skillTrainingMultiplierLocked(without); got != 1 {
		t.Fatalf("training multiplier without necklace = %d, want 1", got)
	}
	if got := w.skillTrainingMultiplierLocked(with); got != 3 {
		t.Fatalf("training multiplier with necklace = %d, want 3", got)
	}
}

func TestSkillTrainingMultiplierUsesReferenceEquipmentFieldBySlot(t *testing.T) {
	w := &World{data: data.StdBundle{Items: map[string]data.StdItem{
		"wrong-ring-field":  {ID: "wrong-ring-field", AniCount: 120},
		"wrong-ring-shape":  {ID: "wrong-ring-shape", Shape: 120},
		"wrong-dress-field": {ID: "wrong-dress-field", Shape: 120},
		"technique-weapon":  {ID: "technique-weapon", AniCount: 120},
	}}}
	if got := w.skillTrainingMultiplierLocked(storage.Character{EquippedItems: map[int]storage.UserItem{
		SlotRingL: {ItemID: "wrong-ring-field"},
	}}); got != 1 {
		t.Fatalf("training multiplier for ring AniCount = %d, want 1", got)
	}
	if got := w.skillTrainingMultiplierLocked(storage.Character{EquippedItems: map[int]storage.UserItem{
		SlotRingL: {ItemID: "wrong-ring-shape"},
	}}); got != 1 {
		t.Fatalf("training multiplier for ring Shape = %d, want 1", got)
	}
	if got := w.skillTrainingMultiplierLocked(storage.Character{EquippedItems: map[int]storage.UserItem{
		SlotDress: {ItemID: "wrong-dress-field"},
	}}); got != 1 {
		t.Fatalf("training multiplier for dress Shape = %d, want 1", got)
	}
	if got := w.skillTrainingMultiplierLocked(storage.Character{EquippedItems: map[int]storage.UserItem{
		SlotWeapon: {ItemID: "technique-weapon"},
	}}); got != 3 {
		t.Fatalf("training multiplier for weapon AniCount = %d, want 3", got)
	}
}

func TestItemGrantedSkillUsesReferenceEquipmentFieldBySlot(t *testing.T) {
	if got := itemGrantedSkillIDAtSlot(data.StdItem{Shape: 154}, SlotDress); got != "火球术" {
		t.Fatalf("dress Shape=154 granted skill %q, want 火球术", got)
	}
	if got := itemGrantedSkillIDAtSlot(data.StdItem{AniCount: 154}, SlotRingL); got != "" {
		t.Fatalf("ring AniCount=154 granted skill %q, want none", got)
	}
	if got := itemGrantedSkillIDAtSlot(data.StdItem{Shape: 154}, SlotRingL); got != "火球术" {
		t.Fatalf("ring Shape=154 granted skill %q, want 火球术", got)
	}
}

func TestWeaponAccuracyAndHitSpeedUseReferenceWeaponFields(t *testing.T) {
	w := &World{data: data.StdBundle{Items: map[string]data.StdItem{
		"weapon": {ID: "weapon", StdMode: 5, Stats: data.StdItemStats{AcMax: 3, MacMax: 12}},
	}}}
	ch := storage.Character{EquippedItems: map[int]storage.UserItem{
		SlotWeapon: {ItemID: "weapon", Dura: 1},
	}}

	stats := w.CombatStats(ch)
	if stats.Hit != 3 {
		t.Fatalf("weapon hit = %d, want 3", stats.Hit)
	}
	if stats.HitSpeed != 2 {
		t.Fatalf("weapon hit speed = %d, want 2", stats.HitSpeed)
	}
	if stats.AC != 0 || stats.MAC != 0 {
		t.Fatalf("weapon defense stats = %d/%d, want 0/0", stats.AC, stats.MAC)
	}
	if got := w.SubAbilityStats(ch).HitPoint; got != 8 {
		t.Fatalf("weapon hit point = %d, want 8", got)
	}
}

func TestWeaponInstanceFieldsUseReferenceLuckHitAndCurseBytes(t *testing.T) {
	w := &World{data: data.StdBundle{Items: map[string]data.StdItem{
		"weapon": {ID: "weapon", StdMode: 5, Stats: data.StdItemStats{
			AcMin: 2, AcMax: 3, MacMin: 4, MacMax: 5,
		}},
	}}}
	ch := storage.Character{EquippedItems: map[int]storage.UserItem{
		SlotWeapon: {ItemID: "weapon", Dura: 1, Desc: [14]byte{3: 7, 4: 8, 5: 9, 6: 10}},
	}}
	got := w.CombatStats(ch)
	if got.Hit != 12 || got.Luck != -3 || got.UnLuck != 12 || got.HitSpeed != 5 {
		t.Fatalf("weapon instance fields = %+v, want hit 12 net luck -3 unluck 12 hit speed 5", got)
	}
}

func TestAccessoryModesUseReferenceSecondaryFields(t *testing.T) {
	w := &World{data: data.StdBundle{Items: map[string]data.StdItem{
		"amulet":            {ID: "amulet", StdMode: 19, Stats: data.StdItemStats{AcMax: 2}},
		"life-necklace":     {ID: "life-necklace", StdMode: 20, Stats: data.StdItemStats{AcMax: 2, MacMax: 3}},
		"recovery-necklace": {ID: "recovery-necklace", StdMode: 21, Stats: data.StdItemStats{AcMin: 4, MacMin: 1}},
	}}}
	ch := storage.Character{Class: "warrior", Level: 1, EquippedItems: map[int]storage.UserItem{
		SlotNecklace: {ItemID: "amulet"},
		SlotRingL:    {ItemID: "life-necklace"},
		SlotRingR:    {ItemID: "recovery-necklace"},
	}}

	sub := w.SubAbilityStats(ch)
	wantSpeed := int(SubAbilitySpeed("warrior")) + 3
	if sub.AntiMagic != 3 || sub.HitPoint != 7 || sub.SpeedPoint != wantSpeed {
		t.Fatalf("accessory secondary fields = %+v, want anti-magic 3, hit point 7, speed point %d", sub, wantSpeed)
	}
	combat := w.CombatStats(ch)
	if combat.AC != 0 || combat.ACMax != 0 || combat.MAC != 0 || combat.MACMax != 0 {
		t.Fatalf("special accessory defense stats = %+v, want zero defense contribution", combat)
	}
	if got := combat.HitSpeed; got != 3 {
		t.Fatalf("accessory hit speed = %d, want 3", got)
	}
}

func TestReferenceNonDefenseEquipmentModesDoNotAddDefense(t *testing.T) {
	tests := []struct {
		name    string
		slot    int
		mode    int
		zeroAll bool
	}{
		{name: "helmet", slot: SlotHelmet, mode: 15},
		{name: "ring", slot: SlotRingL, mode: 22},
		{name: "armring", slot: SlotArmRingL, mode: 26},
		{name: "bujuk", slot: SlotArmRingL, mode: 25, zeroAll: true},
		{name: "honor-medal", slot: SlotRightHand, mode: 30, zeroAll: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := &World{data: data.StdBundle{Items: map[string]data.StdItem{
				"item": {ID: "item", StdMode: tt.mode, Accurate: 3, AtkSpd: 4, Stats: data.StdItemStats{AcMin: 7, AcMax: 11, MacMin: 5, MacMax: 9, DcMin: 2, DcMax: 6, McMin: 1, McMax: 4, ScMin: 3, ScMax: 5}},
			}}}
			ch := storage.Character{EquippedItems: map[int]storage.UserItem{
				tt.slot: {ItemID: "item", Dura: 1},
			}}
			got := w.CombatStats(ch)
			if got.AC != 0 || got.ACMax != 0 || got.MAC != 0 || got.MACMax != 0 {
				t.Fatalf("mode %d defense stats = %+v, want zero defense contribution", tt.mode, got)
			}
			if tt.zeroAll && got != (CombatStats{}) {
				t.Fatalf("mode %d combat stats = %+v, want zero contribution", tt.mode, got)
			}
		})
	}
}

func TestReferenceEtcEquipmentDisplayStatsAreZero(t *testing.T) {
	item := data.StdItem{StdMode: 30, Stats: data.StdItemStats{
		AcMin: 1, AcMax: 2, MacMin: 3, MacMax: 4, DcMin: 5, DcMax: 6,
		McMin: 7, McMax: 8, ScMin: 9, ScMax: 10,
	}}
	display := UpgradeClientItemForDisplay(item, storage.UserItem{}, false)
	if display.Stats != (data.StdItemStats{}) {
		t.Fatalf("mode 30 display stats = %+v, want zero ETC stats", display.Stats)
	}
}

func TestLuckAndCurseUseReferenceMode19Fields(t *testing.T) {
	w := &World{data: data.StdBundle{Items: map[string]data.StdItem{
		"amulet": {ID: "amulet", StdMode: 19, Stats: data.StdItemStats{MacMin: 2, MacMax: 4}},
	}}}
	ch := storage.Character{EquippedItems: map[int]storage.UserItem{
		SlotNecklace: {ItemID: "amulet"},
	}}

	if got := w.CombatStats(ch); got.Luck != 2 {
		t.Fatalf("mode 19 net luck = %d, want 2", got.Luck)
	}
}

func TestBaseUnknownClassFallsBackToWarrior(t *testing.T) {
	if got, want := Base("nonsense", 5), Base("warrior", 5); got != want {
		t.Fatalf("Base(nonsense, 5) = %+v, want fallback to warrior = %+v", got, want)
	}
}

func TestBaseLevelHigherThanOneIncreasesMaxHP(t *testing.T) {
	l1 := Base("warrior", 1)
	l10 := Base("warrior", 10)
	if l10.MaxHP <= l1.MaxHP {
		t.Fatalf("Base(warrior, 10).MaxHP = %d, want > Base(warrior, 1).MaxHP = %d", l10.MaxHP, l1.MaxHP)
	}
}

func TestBaseUsesReferenceBankersRounding(t *testing.T) {
	if got := Base("warrior", 3).MaxMP; got != 21 {
		t.Fatalf("Base(warrior, 3).MaxMP = %d, want reference bankers-rounded value 21", got)
	}
}

func TestTemporaryCombatBonusesUseReferenceOffset(t *testing.T) {
	world := &World{}
	ch := storage.Character{
		Class:     "warrior",
		Level:     1,
		ExtraAbil: [6]uint16{3, 4, 5},
	}

	stats := world.AbilityStats(ch)
	base := Base(ch.Class, ch.Level)
	if got, want := highByte(stats.DC), highByte(PackWord(base.DC, base.DCMax, 0, 0))+5; got != want {
		t.Fatalf("temporary DC = %d, want %d", got, want)
	}
	if got, want := highByte(stats.MC), highByte(PackWord(base.MC, base.MCMax, 0, 0))+6; got != want {
		t.Fatalf("temporary MC = %d, want %d", got, want)
	}
	if got, want := highByte(stats.SC), highByte(PackWord(base.SC, base.SCMax, 0, 0))+7; got != want {
		t.Fatalf("temporary SC = %d, want %d", got, want)
	}
	combat := world.CombatStats(ch)
	if combat.DCMax != 5 || combat.MCMax != 6 || combat.SCMax != 7 {
		t.Fatalf("temporary combat stats = %+v, want DC/MC/SC max 5/6/7", combat)
	}
}

func TestTemporaryCombatBonusesRemainUntilCharacterTickClearsThem(t *testing.T) {
	now := time.Unix(100, 0)
	world := &World{}
	ch := storage.Character{ExtraAbil: [6]uint16{3}, ExtraAbilTimes: [6]int64{now.Add(-time.Second).UnixNano()}}

	stats := world.CombatStats(ch)
	if stats.DCMax != temporaryCombatBonus(3) {
		t.Fatalf("expired temporary DC = %d, want %d before tick cleanup", stats.DCMax, temporaryCombatBonus(3))
	}
	updated, changed := world.applyCharacterTemporaryAbilityTickLocked(ch, now)
	if !changed || updated.ExtraAbil[0] != 0 {
		t.Fatalf("temporary ability cleanup = changed:%t value:%d, want cleared by tick", changed, updated.ExtraAbil[0])
	}
	if got := world.CombatStats(updated).DCMax; got != 0 {
		t.Fatalf("cleared temporary DC = %d, want 0 after tick cleanup", got)
	}
}

func TestAbilityStatsIncludesAllocatedBonusHealthAndMana(t *testing.T) {
	w := &World{gameplay: config.DefaultGameplay()}
	ch := storage.Character{Class: "warrior", Level: 1, BonusAbil: storage.BonusAbility{HP: 7, MP: 3}}
	got := w.AbilityStats(ch)
	base := Base(ch.Class, ch.Level)
	if got.MaxHP != base.MaxHP+7 || got.MaxMP != base.MaxMP+1 {
		t.Fatalf("bonus ability limits = hp:%d mp:%d, want hp:%d mp:%d", got.MaxHP, got.MaxMP, base.MaxHP+7, base.MaxMP+1)
	}
}

func TestAbilityStatsPreservesZeroCurrentHealthAndMana(t *testing.T) {
	w := &World{gameplay: config.DefaultGameplay()}
	ch := storage.Character{Class: "warrior", Level: 1, HP: 0, MP: 0}

	got := w.AbilityStats(ch)
	if got.HP != 0 || got.MP != 0 {
		t.Fatalf("current health and mana = %d/%d, want 0/0", got.HP, got.MP)
	}
}

func TestMagicBloodSuiteUsesReferenceHealthManaAdjustment(t *testing.T) {
	w := &World{gameplay: config.DefaultGameplay(), data: data.StdBundle{Items: map[string]data.StdItem{
		"magic-blood-necklace": {ID: "magic-blood-necklace", Shape: 135, AniCount: 25, DuraMax: 4000},
		"magic-blood-ring":     {ID: "magic-blood-ring", Shape: 133, AniCount: 25, DuraMax: 4000},
		"magic-blood-bracelet": {ID: "magic-blood-bracelet", Shape: 134, AniCount: 25, DuraMax: 4000},
	}}}
	ch := storage.Character{Class: "warrior", Level: 1, EquippedItems: map[int]storage.UserItem{
		SlotNecklace: {ItemID: "magic-blood-necklace", Dura: 1},
		SlotRingL:    {ItemID: "magic-blood-ring", Dura: 1},
		SlotArmRingL: {ItemID: "magic-blood-bracelet", Dura: 1},
	}}

	got := w.AbilityStats(ch)
	if got.MaxHP != 33 || got.MaxMP != 1 {
		t.Fatalf("magic blood suite limits = %d/%d, want 33/1", got.MaxHP, got.MaxMP)
	}
}

func TestRainbowSuiteAddsReferenceAccuracy(t *testing.T) {
	w := &World{data: data.StdBundle{Items: map[string]data.StdItem{
		"rainbow-necklace": {ID: "rainbow-necklace", Shape: 138, AniCount: 5, DuraMax: 4000},
		"rainbow-ring":     {ID: "rainbow-ring", Shape: 136, AniCount: 5, DuraMax: 4000},
		"rainbow-bracelet": {ID: "rainbow-bracelet", Shape: 137, AniCount: 5, DuraMax: 4000},
	}}}
	ch := storage.Character{Class: "warrior", Level: 1, EquippedItems: map[int]storage.UserItem{
		SlotNecklace: {ItemID: "rainbow-necklace", Dura: 1},
		SlotRingL:    {ItemID: "rainbow-ring", Dura: 1},
		SlotArmRingL: {ItemID: "rainbow-bracelet", Dura: 1},
	}}

	combat := w.CombatStats(ch)
	if combat.Hit != 2 || w.SubAbilityStats(ch).HitPoint != 7 {
		t.Fatalf("rainbow suite accuracy = %d/%d, want combat 2 and sub-ability 7", combat.Hit, w.SubAbilityStats(ch).HitPoint)
	}
}

func TestSubAbilityStatsUsesReferenceCharacterDefaults(t *testing.T) {
	w := &World{}
	ch := storage.Character{Class: "taoist", Level: 1, AntiPoison: 4, BonusAbil: storage.BonusAbility{Hit: 2, Speed: 3}}
	got := w.SubAbilityStats(ch)
	if got.AntiMagic != 1 || got.HitPoint != 7 || got.SpeedPoint != 21 || got.AntiPoison != 4 || got.PoisonRecover != 0 || got.HealthRecover != 0 || got.SpellRecover != 0 {
		t.Fatalf("SubAbilityStats = %+v, want reference defaults and derived hit/speed values", got)
	}
}

func TestSubAbilityStatsUsesConfiguredBonusThresholds(t *testing.T) {
	w := &World{gameplay: config.DefaultGameplay()}
	base := storage.Character{Class: "warrior", Level: 1}
	partial := base
	partial.BonusAbil.Hit = 19
	partial.BonusAbil.Speed = 34
	complete := base
	complete.BonusAbil.Hit = 20
	complete.BonusAbil.Speed = 35
	baseStats := w.SubAbilityStats(base)
	partialStats := w.SubAbilityStats(partial)
	completeStats := w.SubAbilityStats(complete)
	if partialStats.HitPoint != baseStats.HitPoint || partialStats.SpeedPoint != baseStats.SpeedPoint {
		t.Fatalf("partial bonus thresholds = %+v, base = %+v; want no derived increase", partialStats, baseStats)
	}
	if completeStats.HitPoint != baseStats.HitPoint+1 || completeStats.SpeedPoint != baseStats.SpeedPoint+1 {
		t.Fatalf("complete bonus thresholds = %+v, base = %+v; want one derived increase", completeStats, baseStats)
	}
}

func TestAbilitiesUsesConfiguredCombatBonusThresholds(t *testing.T) {
	w := &World{gameplay: config.DefaultGameplay()}
	base := storage.Character{Class: "warrior", Level: 1}
	partial := base
	partial.BonusAbil.MAC = 19
	complete := base
	complete.BonusAbil.MAC = 20
	complete.BonusAbil.HP = 1
	complete.BonusAbil.MP = 3
	baseStats := w.Abilities(base)
	partialStats := w.Abilities(partial)
	completeStats := w.Abilities(complete)
	if partialStats != baseStats {
		t.Fatalf("partial combat bonus stats = %+v, base = %+v; want no derived change", partialStats, baseStats)
	}
	if completeStats.MAC != baseStats.MAC+1 || completeStats.MaxHP != baseStats.MaxHP+1 || completeStats.MaxMP != baseStats.MaxMP+1 {
		t.Fatalf("complete combat bonus stats = %+v, base = %+v; want one MAC/HP/MP increase", completeStats, baseStats)
	}
}

func TestAbilitiesDistributesBonusCombatValuesLikeReference(t *testing.T) {
	w := &World{gameplay: config.DefaultGameplay()}
	base := storage.Character{Class: "warrior", Level: 1}
	one := base
	one.BonusAbil.DC = 17
	two := base
	two.BonusAbil.DC = 34
	baseStats := w.CombatStats(base)
	oneStats := w.CombatStats(one)
	twoStats := w.CombatStats(two)
	if oneStats.DC != baseStats.DC+1 || oneStats.DCMax != baseStats.DCMax {
		t.Fatalf("first distributed DC bonus = %d/%d, base = %d/%d; want low-byte increase", oneStats.DC, oneStats.DCMax, baseStats.DC, baseStats.DCMax)
	}
	if twoStats.DC != baseStats.DC+1 || twoStats.DCMax != baseStats.DCMax+1 {
		t.Fatalf("second distributed DC bonus = %d/%d, base = %d/%d; want high-byte increase", twoStats.DC, twoStats.DCMax, baseStats.DC, baseStats.DCMax)
	}
}

func TestAbilitiesUsesClassSpecificBonusThresholds(t *testing.T) {
	w := &World{gameplay: config.DefaultGameplay()}
	base := storage.Character{Class: "wizard", Level: 1}
	partial := base
	partial.BonusAbil.MAC = 14
	complete := base
	complete.BonusAbil.MAC = 15
	baseStats := w.Abilities(base)
	partialStats := w.Abilities(partial)
	completeStats := w.Abilities(complete)
	if partialStats != baseStats {
		t.Fatalf("partial wizard MAC bonus stats = %+v, base = %+v; want no derived change", partialStats, baseStats)
	}
	if completeStats.MAC != baseStats.MAC+1 {
		t.Fatalf("complete wizard MAC bonus = %d, base = %d; want one derived increase", completeStats.MAC, baseStats.MAC)
	}
}

func TestSubAbilityStatsIncludesEquippedResistances(t *testing.T) {
	w := &World{data: data.StdBundle{Items: map[string]data.StdItem{
		"amulet": {ID: "amulet", StdMode: 10, MgAvoid: 2, ToxAvoid: 3},
	}}}
	entry := storage.UserItem{ItemID: "amulet"}
	ch := storage.Character{Class: "warrior", Level: 1, AntiPoison: 4, EquippedItems: map[int]storage.UserItem{SlotNecklace: entry}}
	got := w.SubAbilityStats(ch)
	if got.AntiMagic != 3 || got.AntiPoison != 7 {
		t.Fatalf("SubAbilityStats resistances = %+v, want standard anti-magic/poison 3/7", got)
	}
}

func TestSubAbilityStatsIncludesDurableEquippedResistances(t *testing.T) {
	w := &World{data: data.StdBundle{Items: map[string]data.StdItem{
		"durable-amulet": {ID: "durable-amulet", StdMode: 10, DuraMax: 5000, MgAvoid: 2, ToxAvoid: 3},
	}}}
	ch := storage.Character{Class: "warrior", Level: 1, EquippedItems: map[int]storage.UserItem{
		SlotNecklace: {ItemID: "durable-amulet", Dura: 1},
	}}
	got := w.SubAbilityStats(ch)
	if got.AntiMagic != 3 || got.AntiPoison != 3 {
		t.Fatalf("durable equipment resistances = %+v, want anti-magic/poison 3/3", got)
	}
}

func TestBrokenEquipmentDoesNotContributeResistances(t *testing.T) {
	w := &World{data: data.StdBundle{Items: map[string]data.StdItem{
		"broken-amulet": {ID: "broken-amulet", StdMode: 10, DuraMax: 5000, MgAvoid: 2, ToxAvoid: 3},
	}}}
	ch := storage.Character{Class: "warrior", Level: 1, EquippedItems: map[int]storage.UserItem{
		SlotNecklace: {ItemID: "broken-amulet", Dura: 0},
	}}
	got := w.SubAbilityStats(ch)
	if got.AntiMagic != 1 || got.AntiPoison != 0 {
		t.Fatalf("broken equipment resistances = anti-magic:%d anti-poison:%d, want 1/0", got.AntiMagic, got.AntiPoison)
	}
}

func TestBrokenEquipmentDoesNotContributeLiveAntiPoison(t *testing.T) {
	w := &World{data: data.StdBundle{Items: map[string]data.StdItem{
		"broken-poison-ring": {ID: "broken-poison-ring", StdMode: 23, DuraMax: 5000, ToxAvoid: 4, Stats: data.StdItemStats{AcMax: 3}},
	}}}
	ch := storage.Character{EquippedItems: map[int]storage.UserItem{
		SlotRingL: {ItemID: "broken-poison-ring", Dura: 0},
	}}
	if got := w.characterAntiPoisonLocked(ch); got != 0 {
		t.Fatalf("broken equipment live anti-poison = %d, want 0", got)
	}
}

func TestDurableEquipmentContributesLiveAntiPoison(t *testing.T) {
	w := &World{data: data.StdBundle{Items: map[string]data.StdItem{
		"poison-ring": {ID: "poison-ring", StdMode: 23, DuraMax: 5000, ToxAvoid: 4, Stats: data.StdItemStats{AcMax: 3}},
	}}}
	ch := storage.Character{EquippedItems: map[int]storage.UserItem{
		SlotRingL: {ItemID: "poison-ring", Dura: 1},
	}}
	if got := w.characterAntiPoisonLocked(ch); got != 7 {
		t.Fatalf("durable equipment live anti-poison = %d, want 7", got)
	}
}

func TestBrokenProbeNecklaceIsNotAvailable(t *testing.T) {
	w := &World{data: data.StdBundle{Items: map[string]data.StdItem{
		"broken-probe-necklace": {ID: "broken-probe-necklace", Shape: 121, DuraMax: 5000},
	}}}
	ch := storage.Character{EquippedItems: map[int]storage.UserItem{
		SlotNecklace: {ItemID: "broken-probe-necklace", Dura: 0},
	}}
	if w.HasProbeNecklace(ch) {
		t.Fatal("broken probe necklace remained available")
	}
}

func TestDurableProbeNecklaceIsAvailable(t *testing.T) {
	w := &World{data: data.StdBundle{Items: map[string]data.StdItem{
		"probe-necklace": {ID: "probe-necklace", Shape: 121, DuraMax: 5000},
	}}}
	ch := storage.Character{EquippedItems: map[int]storage.UserItem{
		SlotNecklace: {ItemID: "probe-necklace", Dura: 1},
	}}
	if !w.HasProbeNecklace(ch) {
		t.Fatal("durable probe necklace was not available")
	}
}

func TestProbeNecklaceShapeUsesReferenceSlotRules(t *testing.T) {
	w := &World{data: data.StdBundle{Items: map[string]data.StdItem{
		"shape-probe":       {ID: "shape-probe", Shape: 121},
		"combined-probe":    {ID: "combined-probe", Shape: 159},
		"wrong-field-probe": {ID: "wrong-field-probe", AniCount: 121},
	}}}
	if w.HasProbeNecklace(storage.Character{EquippedItems: map[int]storage.UserItem{
		SlotRingL: {ItemID: "shape-probe", Dura: 1},
	}}) {
		t.Fatal("ring Shape=121 incorrectly enabled probe necklace")
	}
	if !w.HasProbeNecklace(storage.Character{EquippedItems: map[int]storage.UserItem{
		SlotRingL: {ItemID: "combined-probe", Dura: 1},
	}}) {
		t.Fatal("ring Shape=159 did not enable the reference combined probe ability")
	}
	if !w.HasProbeNecklace(storage.Character{EquippedItems: map[int]storage.UserItem{
		SlotWeapon: {ItemID: "wrong-field-probe", Dura: 1},
	}}) {
		t.Fatal("weapon AniCount=121 did not enable probe necklace")
	}
}

func TestReferenceCombinedEquipmentAbilitiesUseSlotSpecificFields(t *testing.T) {
	w := &World{data: data.StdBundle{Items: map[string]data.StdItem{
		"paralysis-shield":      {ID: "paralysis-shield", AniCount: 150, DuraMax: 5000},
		"shape-paralysis":       {ID: "shape-paralysis", Shape: 150, DuraMax: 5000},
		"teleport-revival":      {ID: "teleport-revival", Shape: 160, DuraMax: 5000},
		"shape-teleport-weapon": {ID: "shape-teleport-weapon", Shape: 157, DuraMax: 5000},
		"dress-shape":           {ID: "dress-shape", Shape: 150, DuraMax: 5000},
	}}}
	ch := storage.Character{EquippedItems: map[int]storage.UserItem{
		SlotWeapon: {ItemID: "paralysis-shield", Dura: 1},
		SlotRingL:  {ItemID: "shape-paralysis", Dura: 1},
		SlotRingR:  {ItemID: "teleport-revival", Dura: 1},
	}}
	if !w.characterHasParalysisRingLocked(ch) || !w.characterHasMagicShieldLocked(ch) || !w.hasTeleportRingLocked(ch) || !revivalItem(data.StdItem{Shape: 160}, SlotRingL) {
		t.Fatal("combined equipment abilities were not recognized")
	}
	if revivalAbilityItem(data.StdItem{Shape: 160}, SlotDress) || !revivalAbilityItem(data.StdItem{AniCount: 160}, SlotDress) ||
		!revivalDamageItem(data.StdItem{Shape: 160}, SlotDress) || revivalDamageItem(data.StdItem{AniCount: 160}, SlotDress) {
		t.Fatal("revival ability and durability slot rules diverged from reference")
	}
	shapeOnWeapon := storage.Character{EquippedItems: map[int]storage.UserItem{
		SlotWeapon: {ItemID: "shape-teleport-weapon", Dura: 1},
	}}
	if w.hasTeleportRingLocked(shapeOnWeapon) {
		t.Fatal("weapon Shape=157 incorrectly enabled teleport")
	}
	dressOnly := storage.Character{EquippedItems: map[int]storage.UserItem{
		SlotDress: {ItemID: "dress-shape", Dura: 1},
	}}
	if w.characterHasParalysisRingLocked(dressOnly) || w.characterHasMagicShieldLocked(dressOnly) {
		t.Fatal("shape-only combined equipment incorrectly applied on dress slot")
	}
}

func TestActiveEquippedItemUsesInstanceDurability(t *testing.T) {
	w := &World{data: data.StdBundle{Items: map[string]data.StdItem{
		"durable-item": {ID: "durable-item", DuraMax: 5000},
	}}}
	ch := storage.Character{EquippedItems: map[int]storage.UserItem{
		SlotWeapon: {ItemID: "durable-item", Dura: 1},
	}}
	if _, ok := w.activeEquippedItemLocked(ch, SlotWeapon); !ok {
		t.Fatal("positive-durability equipment was treated as inactive")
	}
	ch.EquippedItems[SlotWeapon] = storage.UserItem{ItemID: "durable-item", Dura: 0}
	if _, ok := w.activeEquippedItemLocked(ch, SlotWeapon); ok {
		t.Fatal("zero-durability equipment was treated as active")
	}
}

func TestBrokenEquipmentDoesNotContributeCombatStats(t *testing.T) {
	w := &World{data: data.StdBundle{Items: map[string]data.StdItem{
		"broken-blade": {ID: "broken-blade", DuraMax: 1000, Stats: data.StdItemStats{DcMin: 2, DcMax: 5}},
	}}}
	ch := storage.Character{EquippedItems: map[int]storage.UserItem{
		SlotWeapon: {ItemID: "broken-blade", Dura: 0, DuraMax: 1000},
	}}
	if got := w.CombatStats(ch); got != (CombatStats{}) {
		t.Fatalf("CombatStats() for broken equipment = %+v, want zero", got)
	}
}

func TestSubAbilityStatsIncludesReferenceRecoveryModes(t *testing.T) {
	w := &World{data: data.StdBundle{Items: map[string]data.StdItem{
		"recovery-necklace": {ID: "recovery-necklace", StdMode: 21, Stats: data.StdItemStats{AcMax: 7, MacMax: 4}},
		"poison-ring":       {ID: "poison-ring", StdMode: 23, Stats: data.StdItemStats{AcMax: 6, MacMax: 3}},
	}}}
	ch := storage.Character{Class: "warrior", Level: 1, EquippedItems: map[int]storage.UserItem{
		SlotNecklace: {ItemID: "recovery-necklace"},
		SlotRingL:    {ItemID: "poison-ring"},
	}}
	got := w.SubAbilityStats(ch)
	if got.AntiPoison != 6 || got.PoisonRecover != 3 || got.HealthRecover != 7 || got.SpellRecover != 4 {
		t.Fatalf("SubAbilityStats recovery modes = %+v, want reference accessory recovery values", got)
	}
}

func TestAbilitiesDoubleWeightLimitsForReferenceMuscleRing(t *testing.T) {
	w := &World{data: data.StdBundle{Items: map[string]data.StdItem{
		"muscle-ring": {ID: "muscle-ring", Shape: 119},
	}}}
	ch := storage.Character{Class: "warrior", Level: 1, EquippedItems: map[int]storage.UserItem{
		SlotRingL: {ItemID: "muscle-ring"},
	}}
	base := Base(ch.Class, ch.Level)
	abilities := w.Abilities(ch)
	stats := w.AbilityStats(ch)
	if abilities.MaxWeight != base.MaxWeight*2 || abilities.MaxWearWeight != base.MaxWearWeight*2 || abilities.MaxHandWeight != base.MaxHandWeight*2 {
		t.Fatalf("abilities weight limits = %d/%d/%d, want %d/%d/%d", abilities.MaxWeight, abilities.MaxWearWeight, abilities.MaxHandWeight, base.MaxWeight*2, base.MaxWearWeight*2, base.MaxHandWeight*2)
	}
	if stats.MaxWeight != abilities.MaxWeight || stats.MaxWearWeight != abilities.MaxWearWeight || stats.MaxHandWeight != abilities.MaxHandWeight {
		t.Fatalf("ability stats weight limits = %d/%d/%d, want abilities values", stats.MaxWeight, stats.MaxWearWeight, stats.MaxHandWeight)
	}
	w.data.Items["wrong-slot-muscle"] = data.StdItem{ID: "wrong-slot-muscle", Shape: 153}
	wrongSlot := ch
	wrongSlot.EquippedItems = map[int]storage.UserItem{SlotWeapon: {ItemID: "wrong-slot-muscle"}}
	if got := w.Abilities(wrongSlot); got.MaxWeight != base.MaxWeight || got.MaxWearWeight != base.MaxWearWeight || got.MaxHandWeight != base.MaxHandWeight {
		t.Fatalf("wrong-slot shape muscle item changed weight limits = %d/%d/%d", got.MaxWeight, got.MaxWearWeight, got.MaxHandWeight)
	}
}

func TestBrokenMuscleRingDoesNotDoubleWeightLimits(t *testing.T) {
	w := &World{data: data.StdBundle{Items: map[string]data.StdItem{
		"muscle-ring": {ID: "muscle-ring", Shape: 119, DuraMax: 5000},
	}}}
	ch := storage.Character{Class: "warrior", Level: 1, EquippedItems: map[int]storage.UserItem{
		SlotRingL: {ItemID: "muscle-ring", Dura: 0},
	}}
	base := Base(ch.Class, ch.Level)
	got := w.Abilities(ch)
	if got.MaxWeight != base.MaxWeight || got.MaxWearWeight != base.MaxWearWeight || got.MaxHandWeight != base.MaxHandWeight {
		t.Fatalf("broken muscle ring weight limits = %d/%d/%d, want base %d/%d/%d", got.MaxWeight, got.MaxWearWeight, got.MaxHandWeight, base.MaxWeight, base.MaxWearWeight, base.MaxHandWeight)
	}
}

func TestDurableMuscleRingDoublesWeightLimits(t *testing.T) {
	w := &World{data: data.StdBundle{Items: map[string]data.StdItem{
		"muscle-ring": {ID: "muscle-ring", Shape: 119, DuraMax: 5000},
	}}}
	ch := storage.Character{Class: "warrior", Level: 1, EquippedItems: map[int]storage.UserItem{
		SlotRingL: {ItemID: "muscle-ring", Dura: 1},
	}}
	base := Base(ch.Class, ch.Level)
	got := w.Abilities(ch)
	if got.MaxWeight != base.MaxWeight*2 || got.MaxWearWeight != base.MaxWearWeight*2 || got.MaxHandWeight != base.MaxHandWeight*2 {
		t.Fatalf("durable muscle ring weight limits = %d/%d/%d, want %d/%d/%d", got.MaxWeight, got.MaxWearWeight, got.MaxHandWeight, base.MaxWeight*2, base.MaxWearWeight*2, base.MaxHandWeight*2)
	}
}

func TestAbilitiesRefreshesAllEquippedWeightBuckets(t *testing.T) {
	w := &World{data: data.StdBundle{Items: map[string]data.StdItem{
		"armor":    {ID: "armor", Weight: 4},
		"weapon":   {ID: "weapon", Weight: 6},
		"shield":   {ID: "shield", Weight: 3},
		"necklace": {ID: "necklace", Weight: 2},
	}}}
	ch := storage.Character{Class: "warrior", Level: 1, EquippedItems: map[int]storage.UserItem{
		SlotDress:     {ItemID: "armor"},
		SlotWeapon:    {ItemID: "weapon"},
		SlotRightHand: {ItemID: "shield"},
		SlotNecklace:  {ItemID: "necklace"},
	}}
	got := w.Abilities(ch)
	if got.WearWeight != 6 || got.HandWeight != 9 {
		t.Fatalf("weight buckets = wear:%d hand:%d, want wear:6 hand:9", got.WearWeight, got.HandWeight)
	}
}

func highByte(word int) int {
	return (word >> 8) & 0xFF
}

func TestBaseNonPositiveLevelTreatedAsOne(t *testing.T) {
	if got, want := Base("warrior", 0), Base("warrior", 1); got != want {
		t.Fatalf("Base(warrior, 0) = %+v, want same as level 1 = %+v", got, want)
	}
}

func TestPackWordCombinesAndClampsToByte(t *testing.T) {
	if got, want := PackWord(1, 1, 1, 2), 2|3<<8; got != want {
		t.Fatalf("PackWord(1,1,1,2) = %d, want %d", got, want)
	}
	if got, want := PackWord(200, 200, 100, 100), 255|255<<8; got != want {
		t.Fatalf("PackWord(200,200,100,100) = %d, want %d (clamped to byte)", got, want)
	}
	if got, want := PackWord(1, 1, -5, -5), 0|0<<8; got != want {
		t.Fatalf("PackWord(1,1,-5,-5) = %d, want %d (clamped to 0)", got, want)
	}
}

func TestCharacterObjectLightUsesReferenceRightHandDurability(t *testing.T) {
	ch := storage.Character{EquippedItems: map[int]storage.UserItem{
		SlotRightHand: {ItemID: "torch", Dura: 1},
	}}
	if got := CharacterObjectLight(ch); got != 3 {
		t.Fatalf("character light with durable right-hand item = %d, want 3", got)
	}
	ch.EquippedItems[SlotRightHand] = storage.UserItem{ItemID: "torch", Dura: 0}
	if got := CharacterObjectLight(ch); got != 0 {
		t.Fatalf("character light with depleted right-hand item = %d, want 0", got)
	}
}
