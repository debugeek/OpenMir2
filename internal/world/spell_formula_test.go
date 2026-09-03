package world

import (
	"math/rand"
	"reflect"
	"testing"
	"time"

	"openmir2/internal/config"
	"openmir2/internal/data"
	"openmir2/internal/protocol/mir176"
	"openmir2/internal/storage"
)

func TestReferenceRoundUsesBankersRounding(t *testing.T) {
	for _, test := range []struct {
		value float64
		want  int
	}{
		{value: 1.5, want: 2},
		{value: 2.5, want: 2},
		{value: 3.5, want: 4},
		{value: -2.5, want: -2},
	} {
		if got := referenceRound(test.value); got != test.want {
			t.Fatalf("referenceRound(%v) = %d, want %d", test.value, got, test.want)
		}
	}
}

func TestSpellPowerUsesReferenceExclusiveRandomUpperBounds(t *testing.T) {
	w := &World{rand: rand.New(rand.NewSource(1))}
	skill := data.StdSkill{Power: 10, MaxPower: 13, TrainLevel1: 0}
	state := storage.SkillState{Level: 0}
	for i := 0; i < 1000; i++ {
		if got := w.spellScaledPowerLocked(skill, state); got >= 13 {
			t.Fatalf("spellScaledPowerLocked() = %d, want less than exclusive max 13", got)
		}
	}

	bonusSkill := data.StdSkill{DefPower: 4, DefMaxPower: 7}
	for i := 0; i < 1000; i++ {
		if got := w.spellDefenseBonusLocked(bonusSkill); got >= 7 {
			t.Fatalf("spellDefenseBonusLocked() = %d, want less than exclusive max 7", got)
		}
	}
}

func TestCharacterNaturalSpellTickRecoversMana(t *testing.T) {
	w := &World{}
	now := time.UnixMilli(1400)
	ch := storage.Character{HP: 1, MP: 1, MaxMP: 100, SpellTick: 780, SpellTickAt: 1000}
	if !w.applyCharacterNaturalSpellTickLocked(&ch, now) {
		t.Fatal("natural spell tick did not recover mana")
	}
	if ch.MP != 7 || ch.SpellTick != 0 || ch.SpellTickAt != 1400 {
		t.Fatalf("character natural recovery = %+v, want mp=7 tick=0 at=1400", ch)
	}
}

func TestCharacterNaturalSpellTickResetsAtFullMana(t *testing.T) {
	w := &World{}
	now := time.UnixMilli(1800)
	ch := storage.Character{HP: 1, MP: 100, MaxMP: 100, SpellTick: 780, SpellTickAt: 1400}
	if w.applyCharacterNaturalSpellTickLocked(&ch, now) {
		t.Fatal("full-mana natural spell tick changed mana")
	}
	if ch.SpellTick != 0 {
		t.Fatalf("SpellTick = %d, want reset to 0 at full mana", ch.SpellTick)
	}
}

func TestCharacterMagicDamageResetsNaturalSpellTick(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	target := storage.Character{ID: "target", MapID: caster.MapID, HP: 100, MaxHP: 100, SpellTick: 700}
	w.mu.Lock()
	updated, hit, err := w.spellCharacterDamageWithPowerLocked(caster, target, 1)
	w.mu.Unlock()
	if err != nil {
		t.Fatalf("spellCharacterDamageWithPowerLocked() error = %v", err)
	}
	if hit.Damage <= 0 || updated.SpellTick != 0 {
		t.Fatalf("magic damage result = hit:%+v character:%+v, want positive damage and zero SpellTick", hit, updated)
	}
}

func TestPendingMagicDamagePrecedesNaturalSpellRecovery(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	now := time.Now()
	target := storage.Character{
		ID: "target", MapID: caster.MapID, X: caster.X + 1, Y: caster.Y,
		HP: 100, MaxHP: 100, MP: 1, MaxMP: 100,
		SpellTick: 780, SpellTickAt: now.Add(-400 * time.Millisecond).UnixMilli(),
	}
	w.mu.Lock()
	w.pendingSpells = []pendingSpell{{
		DueAt: now.Add(-time.Second), CasterID: caster.ID, TargetCharacterID: target.ID,
		CharacterDamage: true, TargetX: target.X, TargetY: target.Y, Damage: 1,
	}}
	w.mu.Unlock()
	result, err := w.Tick([]PlayerSnapshot{{Character: caster}, {Character: target}}, now)
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	for _, updated := range result.Characters {
		if updated.ID == target.ID {
			if updated.MP != 1 {
				t.Fatalf("target MP = %d, want 1 after pending magic damage precedes recovery", updated.MP)
			}
			if updated.HP >= target.HP {
				t.Fatalf("target HP = %d, want damage before recovery", updated.HP)
			}
			return
		}
	}
	t.Fatal("updated target missing")
}

func TestCharacterPhysicalDamageUsesReferenceMagicBubbleAfterDefense(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	w.rand = rand.New(&seqSource{vals: []int64{0, 0, 0, 0}})
	target := storage.Character{
		ID: "target", MapID: caster.MapID, HP: 100, MaxHP: 100,
		BubbleDefenceLevel: 0, BubbleDefenceUntil: time.Now().Add(time.Minute).UnixNano(),
	}
	w.mu.Lock()
	updated, hit, err := w.attackCharacterWithDamageLocked(caster, target, 20)
	w.mu.Unlock()
	if err != nil {
		t.Fatalf("attackCharacterWithDamageLocked() error = %v", err)
	}
	if hit.Damage != 3 || updated.HP != 97 {
		t.Fatalf("physical bubble result = damage:%d hp:%d, want damage:3 hp:97", hit.Damage, updated.HP)
	}
}

func TestPrimaryCharacterAttackUsesReferencePhysicalDefense(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	w.rand = rand.New(&seqSource{vals: []int64{0, 0, 0, 0}})
	target := storage.Character{
		ID: "target", MapID: caster.MapID, X: caster.X, Y: caster.Y - 1,
		HP: 100, MaxHP: 100,
		EquippedItems: map[int]storage.UserItem{SlotDress: {ItemID: "target-armor"}},
	}
	w.data.Items["target-armor"] = data.StdItem{ID: "target-armor", StdMode: 5, Stats: data.StdItemStats{AcMin: 20 | (20 << 8)}}
	players := []storage.Character{target}
	result, err := w.HitWithIdent(caster, caster.X, caster.Y, 0, mir176.CMHit, players...)
	if err != nil {
		t.Fatalf("HitWithIdent() error = %v", err)
	}
	if len(result.CharacterHits) != 1 || result.CharacterHits[0].Damage != 0 || result.CharacterHits[0].Character.HP != 100 {
		t.Fatalf("primary character hit = %+v, want zero damage after physical defense", result.CharacterHits)
	}
}

func TestChargeDamageUsesReferencePhysicalDefense(t *testing.T) {
	w := &World{
		data: data.StdBundle{Items: map[string]data.StdItem{
			"charge-armor": {ID: "charge-armor", StdMode: 5, Stats: data.StdItemStats{AcMin: 10 | (10 << 8)}},
		}},
		rand: rand.New(&seqSource{vals: []int64{0}}),
	}
	target := storage.Character{Class: "warrior", Level: 1, EquippedItems: map[int]storage.UserItem{SlotDress: {ItemID: "charge-armor"}}}
	if got := w.characterPhysicalDamageAfterDefenseLocked(&target, 20); got != 10 {
		t.Fatalf("charge damage after physical defense = %d, want 10", got)
	}
}

func TestChargeDamageAddsUndeadBonusAfterMonsterDefense(t *testing.T) {
	w := &World{}
	caster := storage.Character{Class: "taoist", EquippedItems: map[int]storage.UserItem{
		SlotArmRingL: {ItemID: "undead-ring"},
	}}
	w.data.Items = map[string]data.StdItem{
		"undead-ring": {ID: "undead-ring", StdMode: 19, Undead: 7},
	}
	mon := &Monster{Defense: 3, Undead: 1}
	damage := w.monsterPhysicalDamageAfterDefenseLocked(mon, 10)
	if damage != 7 {
		t.Fatalf("charge damage after monster defense = %d, want 7", damage)
	}
	if got := damage + w.combatStatsLocked(caster).Undead; got != 14 {
		t.Fatalf("charge damage with undead bonus = %d, want 14", got)
	}
}

func TestCharacterPoisonDamageResetsNaturalSpellTick(t *testing.T) {
	w := &World{}
	now := time.Unix(10, 0)
	ch := storage.Character{ID: "target", HP: 100, MaxHP: 100, SpellTick: 700, PoisonHealthLevel: 1, PoisonHealthStartAt: now.Add(-time.Second).UnixNano(), PoisonHealthUntil: now.Add(time.Second).UnixNano(), PoisonHealthTickAt: now.Add(-poisonHealthTickInterval).UnixNano()}
	updated, changed := w.applyCharacterPoisonTickLocked(ch, now)
	if !changed || updated.HP >= ch.HP || updated.SpellTick != 0 {
		t.Fatalf("poison result = changed:%v character:%+v, want damage and zero SpellTick", changed, updated)
	}
}

func TestMagicShieldDurationIncludesReferenceDefensePower(t *testing.T) {
	w := &World{rand: rand.New(rand.NewSource(1))}
	ch := storage.Character{}
	skill := data.StdSkill{TrainLevel1: 1, DefPower: 4, DefMaxPower: 7}
	state := storage.SkillState{Level: 0}
	if got := w.magicShieldDurationLocked(ch, skill, state); got < 8*time.Second || got >= 11*time.Second {
		t.Fatalf("magicShieldDurationLocked() = %s, want 8-10 seconds", got)
	}
}

func TestFireWallDurationUsesReferenceDefensePower(t *testing.T) {
	w := &World{rand: rand.New(rand.NewSource(1))}
	skill := data.StdSkill{TrainLevel1: 1, DefPower: 4, DefMaxPower: 7}
	state := storage.SkillState{Level: 0}
	if got := w.fireWallDurationLocked(storage.Character{}, skill, state); got < 8*time.Second || got >= 11*time.Second {
		t.Fatalf("fireWallDurationLocked() = %s, want 8-10 seconds", got)
	}
}

func TestGroupDefenceDurationUsesReferenceSpiritPowerFormula(t *testing.T) {
	w := &World{rand: rand.New(rand.NewSource(1))}
	skill := data.StdSkill{TrainLevel1: 1, DefPower: 4, DefMaxPower: 7}
	state := storage.SkillState{Level: 0}
	ch := storage.Character{}
	if got := w.groupDefenceDurationLocked(ch, skill, state); got < 34*time.Second || got >= 37*time.Second {
		t.Fatalf("groupDefenceDurationLocked() = %s, want 34-36 seconds from direct GetPower13 formula", got)
	}
}

func TestSpellCostUsesReferenceFixedTrainLevel(t *testing.T) {
	w := &World{}
	skill := data.StdSkill{Spell: 400, DefSpell: 5, TrainLevel1: 100}
	if got := w.SpellCost(skill, storage.SkillState{Level: 0}); got != 105 {
		t.Fatalf("SpellCost() = %d, want 105 with fixed btTrainLv=3", got)
	}
}

func TestGroupHealingUsesReferenceSpiritPowerRange(t *testing.T) {
	w := &World{
		rand: rand.New(rand.NewSource(1)),
		data: data.StdBundle{Items: map[string]data.StdItem{
			"test-talisman": {ID: "test-talisman", Stats: data.StdItemStats{ScMin: 10, ScMax: 20}},
		}},
	}
	ch := storage.Character{EquippedItems: map[int]storage.UserItem{
		0: {ItemID: "test-talisman"},
	}}
	skill := data.StdSkill{Power: 1, MaxPower: 1, TrainLevel1: 0}
	state := storage.SkillState{Level: 0}
	for i := 0; i < 1000; i++ {
		if got := w.spellHealAmountLocked(ch, skill, state); got < 21 || got > 42 {
			t.Fatalf("spellHealAmountLocked() = %d, want 21-42", got)
		}
	}
}

func TestSpellDamageUsesReferenceAttackPowerUpperBound(t *testing.T) {
	w := &World{
		rand: rand.New(rand.NewSource(1)),
		data: data.StdBundle{Items: map[string]data.StdItem{
			"test-staff": {ID: "test-staff", Stats: data.StdItemStats{McMin: 10 | (20 << 8)}},
		}},
	}
	ch := storage.Character{EquippedItems: map[int]storage.UserItem{0: {ItemID: "test-staff"}}}
	skill := data.StdSkill{Power: 1, MaxPower: 1, TrainLevel1: 0}
	state := storage.SkillState{Level: 0}
	for i := 0; i < 1000; i++ {
		if got := w.spellDamageLocked(ch, skill, state); got < 11 || got > 22 {
			t.Fatalf("spellDamageLocked() = %d, want 11-22", got)
		}
	}
}

func TestSpellDamageUsesWeaponLuck(t *testing.T) {
	w := &World{
		rand: rand.New(rand.NewSource(1)),
		data: data.StdBundle{Items: map[string]data.StdItem{
			"test-weapon": {ID: "test-weapon", StdMode: 5, Stats: data.StdItemStats{McMin: 1, McMax: 3}},
		}},
	}
	ch := storage.Character{EquippedItems: map[int]storage.UserItem{SlotWeapon: {ItemID: "test-weapon", Desc: [14]byte{3: 10}}}}
	skill := data.StdSkill{Power: 1, MaxPower: 1, TrainLevel1: 0}
	state := storage.SkillState{Level: 0}
	for i := 0; i < 100; i++ {
		if got := w.spellDamageLocked(ch, skill, state); got != 5 {
			t.Fatalf("spellDamageLocked() = %d, want blessed weapon maximum 5", got)
		}
	}
}

func TestAttackPowerNegativeLuckUsesReferenceRandomOrder(t *testing.T) {
	w := &World{rand: rand.New(&seqSource{vals: []int64{1 << 32, 5 << 32}})}
	if got := w.attackPowerLocked(CombatStats{Luck: -1}, 10, 10); got != 11 {
		t.Fatalf("attackPowerLocked() = %d, want 11", got)
	}
}

func TestAttackPowerNegativeLuckUsesReferenceUnclampedBoundary(t *testing.T) {
	w := &World{rand: rand.New(rand.NewSource(1))}
	if got := w.attackPowerLocked(CombatStats{Luck: -10}, 10, 10); got != 10 {
		t.Fatalf("attackPowerLocked() = %d, want 10 at Random(0) boundary", got)
	}
}

func TestAttackPowerPositiveLuckUsesReferenceCriticalOrder(t *testing.T) {
	w := &World{rand: rand.New(&seqSource{vals: []int64{0, 5 << 32}})}
	if got := w.attackPowerLocked(CombatStats{Luck: 1}, 10, 10); got != 20 {
		t.Fatalf("attackPowerLocked() = %d, want 20", got)
	}
}

func TestMonsterMagicResistanceConsumesRandomAtBothBoundaries(t *testing.T) {
	source := &seqSource{vals: []int64{0, 0}}
	w := &World{rand: rand.New(source)}
	if !w.monsterMagicHitAllowedLocked(&Monster{AntiMagic: 0}) {
		t.Fatal("anti-magic 0 resisted a zero roll")
	}
	if w.monsterMagicHitAllowedLocked(&Monster{AntiMagic: 10}) {
		t.Fatal("anti-magic 10 accepted a zero roll")
	}
	if source.idx != 2 {
		t.Fatalf("random calls = %d, want 2", source.idx)
	}
}

func TestImmediateMonsterMagicDamageDoesNotUsePoisonArmorMultiplier(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	mon := &Monster{ID: "magic-target", HP: 100, MaxHP: 100, Alive: true, PoisonArmorLevel: poisonDamageArmorRate, PoisonArmorUntil: time.Now().Add(time.Minute)}
	w.monsters[mon.ID] = mon

	hit, err := w.attackMonsterWithImmediateMagicDamageLocked(caster, mon, 10)
	if err != nil {
		t.Fatalf("attackMonsterWithImmediateMagicDamageLocked() error = %v", err)
	}
	if hit.Damage != 10 {
		t.Fatalf("immediate magic damage = %d, want 10 without poison armor multiplier", hit.Damage)
	}
}

func TestImmediateMonsterMagicDamageDefersDeathSettlement(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	mon := &Monster{ID: "magic-death-target", MapID: caster.MapID, X: caster.X + 1, Y: caster.Y, HP: 5, MaxHP: 100, Alive: true, Experience: 25}
	w.mu.Lock()
	w.monsters[mon.ID] = mon
	w.occupyMonsterLocked(mon)
	hit, err := w.attackMonsterWithImmediateMagicDamageLocked(caster, mon, 10)
	w.mu.Unlock()
	if err != nil {
		t.Fatalf("attackMonsterWithImmediateMagicDamageLocked() error = %v", err)
	}
	if hit.Dead || hit.Experience != 0 || len(hit.Drops) != 0 || !mon.Alive || !mon.PendingDeath || mon.HP != 0 {
		t.Fatalf("magic hit settlement = dead:%t exp:%d drops:%d alive:%t pending:%t hp:%d, want deferred death", hit.Dead, hit.Experience, len(hit.Drops), mon.Alive, mon.PendingDeath, mon.HP)
	}
	tick, err := w.Tick([]PlayerSnapshot{{Character: caster}}, time.Now())
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if len(tick.MonsterDeaths) != 1 || tick.MonsterDeaths[0].MonsterID != mon.ID {
		t.Fatalf("Tick() deaths = %+v, want %q", tick.MonsterDeaths, mon.ID)
	}
	if len(tick.SpellExperience) != 1 || tick.SpellExperience[0].Experience != mon.Experience {
		t.Fatalf("Tick() experience = %+v, want %d", tick.SpellExperience, mon.Experience)
	}
}

func TestCharacterMagicResistanceConsumesRandomAtBothBoundaries(t *testing.T) {
	source := &seqSource{vals: []int64{0, 0}}
	w := &World{
		data: data.StdBundle{Items: map[string]data.StdItem{
			"anti": {ID: "anti", MgAvoid: 9},
		}},
		rand: rand.New(source),
	}
	target := storage.Character{EquippedItems: map[int]storage.UserItem{SlotWeapon: {ItemID: "anti"}}}
	if w.characterMagicHitAllowedLocked(target) {
		t.Fatal("anti-magic 10 accepted a zero roll")
	}
	target.EquippedItems[SlotWeapon] = storage.UserItem{ItemID: "anti-zero"}
	w.data.Items["anti-zero"] = data.StdItem{ID: "anti-zero", MgAvoid: -1}
	if !w.characterMagicHitAllowedLocked(target) {
		t.Fatal("anti-magic 0 resisted a zero roll")
	}
	if source.idx != 2 {
		t.Fatalf("random calls = %d, want 2", source.idx)
	}
}

func TestCharacterMagicResistanceIncludesEquippedItemBonus(t *testing.T) {
	source := &seqSource{vals: []int64{9, 0}}
	w := &World{
		data: data.StdBundle{Items: map[string]data.StdItem{
			"anti-base": {ID: "anti-base", StdMode: 10, MgAvoid: 8},
		}},
		rand: rand.New(source),
	}
	target := storage.Character{EquippedItems: map[int]storage.UserItem{
		SlotWeapon: {ItemID: "anti-base", Desc: [14]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}},
	}}
	if w.characterMagicHitAllowedLocked(target) {
		t.Fatal("anti-magic 10 accepted a roll of 9")
	}
	if source.idx != 1 {
		t.Fatalf("random calls = %d, want 1", source.idx)
	}
}

func TestMonsterTargetUsesExplicitActorID(t *testing.T) {
	w := &World{monsters: map[string]*Monster{}}
	first := &Monster{ID: "mon-1", TemplateID: "鸡", MapID: "map", X: 10, Y: 10, Alive: true}
	second := &Monster{ID: "mon-2", TemplateID: "鸡", MapID: "map", X: 10, Y: 10, Alive: true}
	w.monsters[first.ID] = first
	w.monsters[second.ID] = second

	if got := w.monsterTargetLocked("map", 10, 10, MonsterActorID(*second), 1); got != second {
		t.Fatalf("monsterTargetLocked() = %+v, want explicit target %q", got, second.ID)
	}
	if got := w.monsterTargetLocked("map", 10, 10, MonsterActorID(Monster{ID: "mon-99"}), 1); got != nil {
		t.Fatalf("monsterTargetLocked() = %+v, want nil for unknown explicit target", got)
	}
}

func TestSpellAreaTargetsUseReferenceCoordinateOrder(t *testing.T) {
	w := &World{monsters: map[string]*Monster{
		"mon-b": {ID: "mon-b", MapID: "map", X: 11, Y: 9, Alive: true},
		"mon-a": {ID: "mon-a", MapID: "map", X: 9, Y: 11, Alive: true},
	}}
	players := []storage.Character{
		{ID: "char-b", MapID: "map", X: 11, Y: 8, HP: 1},
		{ID: "char-a", MapID: "map", X: 9, Y: 10, HP: 1},
	}
	targets := w.spellAreaTargetsLocked(players, "map", 10, 10, 2)
	got := make([]string, 0, len(targets))
	for _, target := range targets {
		_, _, id := target.CharacterOrMonster()
		got = append(got, id)
	}
	want := []string{"char-a", "mon-a", "char-b", "mon-b"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("spellAreaTargetsLocked() = %v, want %v", got, want)
	}
}

func TestSpellAreaTargetsUseSharedObjectOrderWithinCell(t *testing.T) {
	w := &World{monsters: map[string]*Monster{
		"mon-first": {ID: "mon-first", ObjectOrder: 2, MapID: "map", X: 10, Y: 10, Alive: true},
		"mon-last":  {ID: "mon-last", ObjectOrder: 4, MapID: "map", X: 10, Y: 10, Alive: true},
	}}
	players := []storage.Character{
		{ID: "char-middle", ObjectOrder: 3, MapID: "map", X: 10, Y: 10, HP: 1},
		{ID: "char-first", ObjectOrder: 1, MapID: "map", X: 10, Y: 10, HP: 1},
	}
	targets := w.spellAreaTargetsLocked(players, "map", 10, 10, 0)
	got := make([]string, 0, len(targets))
	for _, target := range targets {
		_, _, id := target.CharacterOrMonster()
		got = append(got, id)
	}
	want := []string{"char-first", "mon-first", "char-middle", "mon-last"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("spellAreaTargetsLocked() = %v, want %v", got, want)
	}
}

func TestHealingGaugeTargetsUseSpellObjectOrder(t *testing.T) {
	w := &World{monsters: map[string]*Monster{
		"mon": {ID: "mon", MasterID: "caster", ObjectOrder: 2, MapID: "map", X: 10, Y: 10, HP: 1, MaxHP: 10, Alive: true},
	}}
	caster := storage.Character{ID: "caster", MapID: "map", X: 10, Y: 10, HP: 10, MaxHP: 10, ObjectOrder: 3}
	players := []storage.Character{{ID: "ally", MapID: "map", X: 10, Y: 10, HP: 1, MaxHP: 10, ObjectOrder: 1}}
	targets := w.healingGaugeTargetsLocked(caster, players, 10, 10)
	if len(targets) != 3 {
		t.Fatalf("healingGaugeTargetsLocked() returned %d targets, want 3", len(targets))
	}
	if targets[0].Character == nil || targets[0].Character.ID != "ally" {
		t.Fatalf("first healing gauge target = %+v, want ally", targets[0])
	}
	if targets[1].Monster == nil || targets[1].Monster.ID != "mon" {
		t.Fatalf("second healing gauge target = %+v, want mon", targets[1])
	}
	if targets[2].Character == nil || targets[2].Character.ID != "caster" {
		t.Fatalf("third healing gauge target = %+v, want caster", targets[2])
	}
}

func TestCharacterSpellTargetUsesReferencePKProtection(t *testing.T) {
	gameplay := config.DefaultGameplay()
	w := &World{
		data:     data.StdBundle{Maps: map[string]data.StdMap{"map": {ID: "map"}}},
		gameplay: gameplay,
	}
	caster := storage.Character{ID: "caster", MapID: "map", Level: 1, HP: 1}
	target := storage.Character{ID: "target", MapID: "map", Level: 20, HP: 1, PKPoint: 200}
	if w.isProperCharacterTargetLocked(caster, target) {
		t.Fatal("clean low-level caster should not attack a high-level red target")
	}

	caster.Level = 20
	caster.PKPoint = 200
	target.Level = 1
	target.PKPoint = 0
	if w.isProperCharacterTargetLocked(caster, target) {
		t.Fatal("high-level red caster should not attack a low-level clean target")
	}
}

func TestCharacterSpellTargetRejectsSafeZone(t *testing.T) {
	gameplay := config.DefaultGameplay()
	w := &World{
		data: data.StdBundle{Maps: map[string]data.StdMap{
			"safe": {ID: "safe", Safe: true},
		}},
		gameplay: gameplay,
	}
	caster := storage.Character{ID: "caster", MapID: "safe", Level: 20, HP: 1}
	target := storage.Character{ID: "target", MapID: "safe", Level: 20, HP: 1}
	if w.isProperCharacterTargetLocked(caster, target) {
		t.Fatal("spell target in a safe map should be rejected")
	}
}

func TestCharacterSpellTargetUsesReferenceGuildAttackMode(t *testing.T) {
	gameplay := config.DefaultGameplay()
	w := &World{
		data:     data.StdBundle{Maps: map[string]data.StdMap{"map": {ID: "map"}}},
		gameplay: gameplay,
	}
	caster := storage.Character{ID: "caster", MapID: "map", Level: 20, HP: 1, AttackMode: 3, GuildID: "guild"}
	target := storage.Character{ID: "target", MapID: "map", Level: 20, HP: 1, GuildID: "guild"}
	if w.isProperCharacterTargetLocked(caster, target) {
		t.Fatal("guild attack mode should reject a member of the same guild")
	}
	target.GuildID = "other"
	if !w.isProperCharacterTargetLocked(caster, target) {
		t.Fatal("guild attack mode should allow a different guild outside other protections")
	}
	caster.GuildWarArea = true
	target.GuildWarArea = true
	caster.GuildAllianceID = "alliance"
	target.GuildAllianceID = "alliance"
	if w.isProperCharacterTargetLocked(caster, target) {
		t.Fatal("guild attack mode should reject an allied guild in a guild war area")
	}
}

func TestSpellTargetRejectsAdminAndStoneObjects(t *testing.T) {
	gameplay := config.DefaultGameplay()
	w := &World{
		data:     data.StdBundle{Maps: map[string]data.StdMap{"map": {ID: "map"}}},
		gameplay: gameplay,
	}
	caster := storage.Character{ID: "caster", MapID: "map", Level: 20, HP: 1}
	target := storage.Character{ID: "target", MapID: "map", Level: 20, HP: 1, AdminMode: true}
	if w.isProperCharacterTargetLocked(caster, target) {
		t.Fatal("admin target should be rejected")
	}
	target.AdminMode = false
	target.StoneMode = true
	if w.isProperCharacterTargetLocked(caster, target) {
		t.Fatal("stone target should be rejected")
	}
	mon := &Monster{ID: "monster", MapID: "map", Alive: true, AdminMode: true}
	if w.isProperMonsterTargetLocked(caster, nil, mon) {
		t.Fatal("admin monster should be rejected")
	}
	mon.AdminMode = false
	mon.StoneMode = true
	if w.isProperMonsterTargetLocked(caster, nil, mon) {
		t.Fatal("stone monster should be rejected")
	}
}

func TestMonsterSpellTargetUsesReferenceOwnerAttackRules(t *testing.T) {
	w := &World{
		data:     data.StdBundle{Maps: map[string]data.StdMap{"map": {ID: "map"}}},
		gameplay: config.DefaultGameplay(),
	}
	caster := storage.Character{ID: "caster", MapID: "map", Level: 20, HP: 1, AttackMode: 0}
	owned := &Monster{ID: "owned", MapID: "map", Alive: true, MasterID: caster.ID}
	if !w.isProperMonsterTargetLocked(caster, nil, owned) {
		t.Fatal("all attack mode should allow own summon, matching IsProperTarget")
	}
	caster.AttackMode = 1
	if w.isProperMonsterTargetLocked(caster, nil, owned) {
		t.Fatal("peace attack mode should reject own summon")
	}

	master := storage.Character{ID: "master", MapID: "map", Level: 20, HP: 1}
	other := &Monster{ID: "other", MapID: "map", Alive: true, MasterID: master.ID}
	caster.AttackMode = 0
	if !w.isProperMonsterTargetLocked(caster, []storage.Character{master}, other) {
		t.Fatal("all attack mode should allow another owner's summon")
	}
	caster.AttackMode = 2
	caster.GroupOwnerID = "group"
	master.GroupOwnerID = "group"
	if w.isProperMonsterTargetLocked(caster, []storage.Character{master}, other) {
		t.Fatal("group attack mode should reject a group member's summon")
	}
	w.gameplay.Combat.NonPKServer = true
	if !w.isProperMonsterTargetLocked(caster, []storage.Character{master}, other) {
		t.Fatal("non-PK server should allow a group member's summon")
	}
}

func TestMonsterStruckByCharacterRetargetsAdjacentAttacker(t *testing.T) {
	w := &World{gameplay: config.DefaultGameplay(), rand: rand.New(rand.NewSource(1))}
	caster := storage.Character{ID: "caster", MapID: "map", X: 11, Y: 10, HP: 1}
	oldTarget := storage.Character{ID: "old-target", MapID: "map", X: 10, Y: 11, HP: 1}
	mon := &Monster{ID: "monster", MapID: "map", X: 10, Y: 10, Alive: true, TargetCharacterID: oldTarget.ID}
	now := time.Unix(10, 0)

	w.monsterStruckByCharacterLocked(mon, caster, []storage.Character{oldTarget}, now)

	if mon.TargetCharacterID != caster.ID {
		t.Fatalf("TargetCharacterID = %q, want %q after adjacent Struck", mon.TargetCharacterID, caster.ID)
	}
	if !mon.LastAttackAt.After(now) {
		t.Fatalf("LastAttackAt = %v, want delayed after Struck", mon.LastAttackAt)
	}
}

func TestStealthAffectedTargetsUseReferenceObjectOrder(t *testing.T) {
	w := &World{monsters: map[string]*Monster{
		"summon": {ID: "summon", Race: 50, MasterID: "caster", ObjectOrder: 1, MapID: "map", X: 10, Y: 10, HP: 1, Alive: true},
	}}
	caster := storage.Character{ID: "caster", MapID: "map", X: 10, Y: 10, HP: 10, ObjectOrder: 3, AttackMode: 0}
	friend := storage.Character{ID: "friend", MapID: "map", X: 10, Y: 10, HP: 1, ObjectOrder: 2}

	targets := w.stealthAffectedTargetsLocked(caster, []storage.Character{friend, caster}, 10, 10)
	if len(targets) != 3 {
		t.Fatalf("stealthAffectedTargetsLocked() returned %d targets, want 3", len(targets))
	}
	if targets[0].Monster == nil || targets[0].Monster.ID != "summon" {
		t.Fatalf("first stealth target = %+v, want summon", targets[0])
	}
	if targets[1].Character == nil || targets[1].Character.ID != friend.ID {
		t.Fatalf("second stealth target = %+v, want friend", targets[1])
	}
	if targets[2].Character == nil || targets[2].Character.ID != caster.ID {
		t.Fatalf("third stealth target = %+v, want caster", targets[2])
	}
}

func TestMovingObjectAtPointUsesObjectOrder(t *testing.T) {
	w := &World{monsters: map[string]*Monster{
		"monster": {ID: "monster", MapID: "map", X: 3, Y: 4, Alive: true, ObjectOrder: 1},
	}}
	players := []storage.Character{{ID: "character", MapID: "map", X: 3, Y: 4, HP: 1, ObjectOrder: 2}}
	selected := w.movingObjectAtPointLocked(players, "map", 3, 4)
	if selected.Monster == nil || selected.Monster.ID != "monster" {
		t.Fatalf("selected object = %+v, want earliest monster", selected)
	}
	w.monsters["monster"].ObjectOrder = 3
	selected = w.movingObjectAtPointLocked(players, "map", 3, 4)
	if selected.Character == nil || selected.Character.ID != "character" {
		t.Fatalf("selected object = %+v, want earliest character", selected)
	}
}

func TestMovingObjectAtPointKeepsUnorderedInputBeforeOrderedObject(t *testing.T) {
	w := &World{monsters: map[string]*Monster{
		"monster": {ID: "monster", MapID: "map", X: 3, Y: 4, Alive: true, ObjectOrder: 1},
	}}
	players := []storage.Character{{ID: "character", MapID: "map", X: 3, Y: 4, HP: 1}}
	selected := w.movingObjectAtPointLocked(players, "map", 3, 4)
	if selected.Character == nil || selected.Character.ID != "character" {
		t.Fatalf("selected object = %+v, want unordered input character", selected)
	}
}

func TestSpellTeleportRefreshesMapObjectOrder(t *testing.T) {
	w := &World{
		data: data.StdBundle{Maps: map[string]data.StdMap{
			"map": {ID: "map", Width: 3, Height: 3},
		}},
		rand:            rand.New(rand.NewSource(1)),
		nextObjectOrder: 1,
	}
	ch := storage.Character{ID: "caster", MapID: "map", X: 0, Y: 0}
	next, err := w.homeTeleportRandomCharacterLocked(ch)
	if err != nil {
		t.Fatalf("homeTeleportRandomCharacterLocked() error = %v", err)
	}
	if next.ObjectOrder != 1 {
		t.Fatalf("teleported ObjectOrder = %d, want 1", next.ObjectOrder)
	}
	if next.MapMoveAt == 0 {
		t.Fatal("teleported MapMoveAt is zero")
	}
}

func TestPoisonEffectDurationUsesReferencePower(t *testing.T) {
	tests := []struct {
		name  string
		power int
		want  time.Duration
	}{
		{name: "power 40", power: 40, want: 40 * time.Second},
		{name: "power 30", power: 30, want: 30 * time.Second},
		{name: "power 1", power: 1, want: time.Second},
		{name: "zero", power: 0, want: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := poisonEffectDuration(test.power); got != test.want {
				t.Fatalf("poisonEffectDuration(%d) = %s, want %s", test.power, got, test.want)
			}
		})
	}
}
