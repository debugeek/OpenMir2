package world

import (
	"math/rand"
	"os"
	"path/filepath"
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

func TestSpellRangeAndLineOfSightUseReferenceBoundaries(t *testing.T) {
	w := &World{}
	w.data.Maps = map[string]data.StdMap{
		"test": {Width: 16, Height: 16},
	}
	caster := storage.Character{MapID: "test", X: 4, Y: 4}

	if !w.SpellTargetInRange(caster, 12, 12) {
		t.Fatal("SpellTargetInRange() rejected the inclusive 8-tile coordinate range")
	}
	if w.SpellTargetInRange(caster, 13, 12) {
		t.Fatal("SpellTargetInRange() accepted a coordinate beyond the reference range")
	}
	if !w.magCanHitTargetLocked("test", 4, 4, 7, 4) {
		t.Fatal("magCanHitTargetLocked() rejected an unobstructed target")
	}

	w.data.Maps["test"] = data.StdMap{
		Width:   16,
		Height:  16,
		Blocked: []data.StdPoint{{X: 5, Y: 4}},
	}
	if w.magCanHitTargetLocked("test", 4, 4, 7, 4) {
		t.Fatal("magCanHitTargetLocked() crossed a blocked intermediate cell")
	}

	w.data.Maps["test"] = data.StdMap{
		Width:   16,
		Height:  16,
		Blocked: []data.StdPoint{{X: 4, Y: 5}},
	}
	if w.magCanHitTargetLocked("test", 4, 4, 4, 4) {
		t.Fatal("magCanHitTargetLocked() accepted a same-coordinate target when the reference next cell is blocked")
	}
}

func TestSpellStatusAndShowHealthExpireOnlyAfterReferenceBoundary(t *testing.T) {
	now := time.Unix(20, 0)
	boundary := now.UnixNano()
	w := &World{}
	character := storage.Character{
		DefenceUpUntil:     boundary,
		MagDefenceUpUntil:  boundary,
		BubbleDefenceUntil: boundary,
		BubbleDefenceLevel: 2,
		TransparentUntil:   boundary,
		ParalyzedUntil:     boundary,
		ShowHPUntil:        boundary,
	}
	if _, changed := w.applyCharacterProtectionTickLocked(character, now); changed {
		t.Fatal("character protection expired at the exact reference boundary")
	}
	if _, changed := w.applyCharacterShowHPTickLocked(character, now); changed {
		t.Fatal("character show-health state expired at the exact reference boundary")
	}
	if _, changed := w.applyCharacterStealthTickLocked(character, now); changed {
		t.Fatal("character stealth expired at the exact reference boundary")
	}
	if characterStatus(character, now, false)&0x00800000 == 0 {
		t.Fatal("character status dropped stealth at the exact reference boundary")
	}
	status := uint32(characterStatus(character, now, false))
	if status&0x00400000 == 0 || status&0x00200000 == 0 || status&0x00100000 == 0 || status&0x00000002 == 0 || status&0x04000000 == 0 {
		t.Fatalf("character status dropped an active exact-boundary flag: %#x", status)
	}
	if _, changed := w.applyCharacterProtectionTickLocked(character, now.Add(1)); !changed {
		t.Fatal("character protection did not expire after the reference boundary")
	}
	if _, changed := w.applyCharacterShowHPTickLocked(character, now.Add(1)); !changed {
		t.Fatal("character show-health state did not expire after the reference boundary")
	}
	if _, changed := w.applyCharacterStealthTickLocked(character, now.Add(1)); !changed {
		t.Fatal("character stealth did not expire after the reference boundary")
	}

	monster := &Monster{DefenceUpUntil: boundary, MagDefenceUpUntil: boundary, ShowHPUntil: boundary, TransparentUntil: now, Alive: true}
	if w.applyMonsterProtectionTickLocked(monster, now) {
		t.Fatal("monster protection expired at the exact reference boundary")
	}
	if w.applyMonsterShowHPTickLocked(monster, now) {
		t.Fatal("monster show-health state expired at the exact reference boundary")
	}
	if !monster.TransparentUntil.Equal(now) {
		t.Fatal("monster stealth state changed at the exact reference boundary")
	}
	if !w.applyMonsterProtectionTickLocked(monster, now.Add(1)) {
		t.Fatal("monster protection did not expire after the reference boundary")
	}
	monster.ShowHPUntil = boundary
	if !w.applyMonsterShowHPTickLocked(monster, now.Add(1)) {
		t.Fatal("monster show-health state did not expire after the reference boundary")
	}
}

func TestMonsterRunBatchesExpiredStatusRefreshes(t *testing.T) {
	now := time.Unix(100, 0)
	monster := &Monster{
		ID:                "expired-status-monster",
		Alive:             true,
		ParalyzedUntil:    now.Add(-time.Second),
		TransparentUntil:  now.Add(-time.Second),
		DefenceUpUntil:    now.Add(-time.Second).UnixNano(),
		MagDefenceUpUntil: now.Add(-time.Second).UnixNano(),
	}
	result := TickResult{}
	w := &World{}
	w.applyMonsterInheritedStateTickLocked(monster, now, &result)

	if len(result.StatusRefreshMonsters) != 1 || result.StatusRefreshMonsters[0].ID != monster.ID {
		t.Fatalf("expired status refreshes = %+v, want one refresh", result.StatusRefreshMonsters)
	}
	statusEvents := 0
	for _, event := range result.OrderedMonsterEvents {
		if event.Kind == OrderedMonsterEventStatus {
			statusEvents++
		}
	}
	if statusEvents != 1 {
		t.Fatalf("expired status events = %d, want one event", statusEvents)
	}
}

func TestMonsterInheritedStateEndsHolySeizeBeforeCrazy(t *testing.T) {
	now := time.Unix(100, 0)
	monster := &Monster{ID: "ordered-control-expiry", Alive: true, HolySeizeUntil: now.Add(-time.Second), CrazyUntil: now.Add(-time.Second)}
	result := TickResult{}
	w := &World{}
	w.applyMonsterInheritedStateTickLocked(monster, now, &result)
	if len(result.OrderedMonsterEvents) < 2 || result.OrderedMonsterEvents[0].Kind != OrderedMonsterEventNameColor || result.OrderedMonsterEvents[1].Kind != OrderedMonsterEventNameColor {
		t.Fatalf("ordered inherited events = %+v, want two name-color events", result.OrderedMonsterEvents)
	}
	if len(result.NameColorMonsters) != 2 || result.NameColorMonsters[0].CrazyUntil.IsZero() || !result.NameColorMonsters[1].CrazyUntil.IsZero() {
		t.Fatalf("name-color refreshes = %+v, want two refreshes", result.NameColorMonsters)
	}
}

func TestSummonedMonsterExpiresAfterReferenceMasterTickWindow(t *testing.T) {
	now := time.Unix(100, 0)
	monster := &Monster{
		ID:         "master-window-monster",
		MasterID:   "owner",
		MasterTick: now.Add(-12*time.Hour - time.Nanosecond),
		HP:         100,
		Alive:      true,
	}
	w := &World{}
	if !w.finalizeSummonedOwnerStateAfterRunLocked(monster, map[string]storage.Character{
		"owner": {ID: "owner", HP: 100, MaxHP: 100},
	}, now) {
		t.Fatal("master tick expiry = false, want forced death")
	}
	if monster.HP != 0 || !monster.PendingDeath || monster.DeathHitterID != "" {
		t.Fatalf("expired master window state = hp:%d pending:%t hitter:%q, want deferred death", monster.HP, monster.PendingDeath, monster.DeathHitterID)
	}
}

func TestSummonedMonsterKeepsNoItemStateAfterMasterExpiry(t *testing.T) {
	now := time.Unix(100, 0)
	monster := &Monster{ID: "expired-no-item", MasterID: "owner", MasterExpiresAt: now.Add(-time.Second), CorpseGold: 100}
	w := &World{}
	monster.NoItem = monster.MasterID != ""
	if !w.releaseExpiredMonsterMasterLocked(monster, now) {
		t.Fatal("releaseExpiredMonsterMasterLocked() = false, want true")
	}
	if monster.MasterID != "" || !monster.NoItem {
		t.Fatalf("expired summon state = master:%q no_item:%t, want cleared master and retained no-item", monster.MasterID, monster.NoItem)
	}
	if drops := w.rollDropsLocked(monster, "owner", ""); len(drops) != 0 {
		t.Fatalf("expired summon drops = %+v, want none", drops)
	}
}

func TestPoisonActiveChecksHonorReferenceExpiry(t *testing.T) {
	now := time.Unix(30, 0)
	character := storage.Character{
		PoisonHealthLevel: 1, PoisonHealthUntil: now.UnixNano(), PoisonHealthStartAt: now.UnixNano(),
		PoisonArmorLevel: 1, PoisonArmorUntil: now.UnixNano(), PoisonArmorStartAt: now.UnixNano(),
	}
	if !characterPoisonHealthActive(character, now) || !characterPoisonArmorActive(character, now) {
		t.Fatal("character poison became inactive at the exact reference expiry boundary")
	}
	if characterPoisonHealthActive(character, now.Add(1)) || characterPoisonArmorActive(character, now.Add(1)) {
		t.Fatal("character poison remained active after the reference expiry boundary")
	}

	monster := &Monster{
		PoisonHealthLevel: 1, PoisonHealthUntil: now, PoisonHealthStartAt: now,
		PoisonArmorLevel: 1, PoisonArmorUntil: now, PoisonArmorStartAt: now,
	}
	if !monsterPoisonHealthActive(monster, now) || !monsterPoisonArmorActive(monster, now) {
		t.Fatal("monster poison became inactive at the exact reference expiry boundary")
	}
	if monsterPoisonHealthActive(monster, now.Add(1)) || monsterPoisonArmorActive(monster, now.Add(1)) {
		t.Fatal("monster poison remained active after the reference expiry boundary")
	}
}

func TestPoisonDamageTickWaitsPastReferenceInterval(t *testing.T) {
	now := time.Unix(40, 0)
	character := storage.Character{
		HP: 100, MaxHP: 100, PoisonHealthLevel: 1,
		PoisonHealthStartAt: now.Add(-time.Second).UnixNano(), PoisonHealthUntil: now.Add(time.Minute).UnixNano(),
		PoisonHealthTickAt: now.Add(-poisonHealthTickInterval).UnixNano(),
	}
	w := &World{}
	updated, changed := w.applyCharacterPoisonTickLocked(character, now)
	if changed || updated.HP != character.HP {
		t.Fatalf("character poison tick fired at exact interval: changed=%t hp=%d", changed, updated.HP)
	}
	updated, changed = w.applyCharacterPoisonTickLocked(character, now.Add(1))
	if !changed || updated.HP >= character.HP {
		t.Fatalf("character poison tick did not fire after interval: changed=%t hp=%d", changed, updated.HP)
	}

	monster := &Monster{
		ID: "poison-tick-monster", Alive: true, HP: 100, MaxHP: 100,
		PoisonHealthLevel: 1, PoisonHealthStartAt: now.Add(-time.Second), PoisonHealthUntil: now.Add(time.Minute),
		PoisonHealthTickAt: now.Add(-poisonHealthTickInterval),
	}
	hits, _, err := w.applyMonsterPoisonTickLocked(monster, nil, now)
	if err != nil {
		t.Fatalf("monster poison tick at exact interval error = %v", err)
	}
	if len(hits) != 0 || monster.HP != 100 {
		t.Fatalf("monster poison tick fired at exact interval: hits=%d hp=%d", len(hits), monster.HP)
	}
	hits, _, err = w.applyMonsterPoisonTickLocked(monster, nil, now.Add(1))
	if err != nil {
		t.Fatalf("monster poison tick after interval error = %v", err)
	}
	if len(hits) != 1 || monster.HP >= 100 {
		t.Fatalf("monster poison tick did not fire after interval: hits=%d hp=%d", len(hits), monster.HP)
	}
	if monster.PendingDeath {
		t.Fatalf("nonlethal monster poison tick marked pending death")
	}
}

func TestPoisonBaselineAdvancesWithoutActivePoison(t *testing.T) {
	now := time.Unix(50, 0)
	character := storage.Character{HP: 100, MaxHP: 100, PoisonHealthTickAt: now.Add(-poisonHealthTickInterval - time.Nanosecond).UnixNano()}
	updated, changed := (&World{}).applyCharacterPoisonTickLocked(character, now)
	if !changed || updated.HP != character.HP || updated.PoisonHealthTickAt != now.UnixNano() {
		t.Fatalf("character poison baseline = changed:%t hp:%d tick:%d, want refreshed baseline without damage", changed, updated.HP, updated.PoisonHealthTickAt)
	}
	setCharacterHealthPoisonLocked(&updated, 1, 1, now.Add(time.Minute), now)
	updated, changed = (&World{}).applyCharacterPoisonTickLocked(updated, now.Add(time.Nanosecond))
	if changed || updated.HP != character.HP {
		t.Fatalf("character poison after baseline refresh = changed:%t hp:%d, want no immediate damage", changed, updated.HP)
	}

	monster := &Monster{ID: "baseline-monster", Alive: true, HP: 100, MaxHP: 100, PoisonHealthTickAt: now.Add(-poisonHealthTickInterval - time.Nanosecond)}
	if hits, _, err := (&World{}).applyMonsterPoisonTickLocked(monster, nil, now); err != nil || len(hits) != 0 || !monster.PoisonHealthTickAt.Equal(now) {
		t.Fatalf("monster poison baseline = hits:%d err:%v tick:%s, want refreshed baseline without damage", len(hits), err, monster.PoisonHealthTickAt)
	}
}

func TestPoisonDamageUsesCharacterMagicShield(t *testing.T) {
	w := &World{data: data.StdBundle{Items: map[string]data.StdItem{
		"magic-shield-ring": {ID: "magic-shield-ring", Shape: 118, DuraMax: 5000},
	}}}
	now := time.Unix(60, 0)
	character := storage.Character{
		HP: 100, MaxHP: 100, MP: 100, MaxMP: 100,
		PoisonHealthLevel: 1, PoisonHealthPoint: 4,
		PoisonHealthStartAt: now.Add(-time.Second).UnixNano(), PoisonHealthUntil: now.Add(time.Minute).UnixNano(),
		PoisonHealthTickAt: now.Add(-poisonHealthTickInterval - time.Nanosecond).UnixNano(),
		EquippedItems:      map[int]storage.UserItem{SlotRingL: {ItemID: "magic-shield-ring", Dura: 1000}},
	}
	updated, changed := w.applyCharacterPoisonTickLocked(character, now)
	if !changed || updated.HP != character.HP || updated.MP != 92 {
		t.Fatalf("poison magic shield = changed:%t hp:%d mp:%d, want HP unchanged and MP 92", changed, updated.HP, updated.MP)
	}
}

func TestPoisonApplicationPreservesObjectTickBaseline(t *testing.T) {
	now := time.Unix(40, 0)
	character := storage.Character{HP: 100, MaxHP: 100, PoisonHealthTickAt: now.Add(-3 * time.Second).UnixNano()}
	setCharacterHealthPoisonLocked(&character, 1, 1, now.Add(time.Minute), now)
	updated, changed := (&World{}).applyCharacterPoisonTickLocked(character, now)
	if !changed || updated.HP >= character.HP {
		t.Fatalf("character poison after refresh = changed:%t hp:%d, want immediate damage from aged baseline", changed, updated.HP)
	}
	if updated.PoisonHealthTickAt != now.UnixNano() {
		t.Fatalf("character poison tick baseline = %d, want current damage tick %d", updated.PoisonHealthTickAt, now.UnixNano())
	}

	monster := &Monster{ID: "aged-poison-monster", Alive: true, HP: 100, MaxHP: 100, PoisonHealthTickAt: now.Add(-3 * time.Second)}
	setMonsterHealthPoisonLocked(monster, 1, 1, now.Add(time.Minute), "caster", now)
	hits, _, err := (&World{}).applyMonsterPoisonTickLocked(monster, nil, now)
	if err != nil {
		t.Fatalf("monster poison after aged object baseline error = %v", err)
	}
	if len(hits) != 1 || monster.HP >= 100 {
		t.Fatalf("monster poison after refresh = hits:%d hp:%d, want immediate damage from aged baseline", len(hits), monster.HP)
	}
	hits, _, err = (&World{}).applyMonsterPoisonTickLocked(monster, nil, now.Add(poisonHealthTickInterval+time.Nanosecond))
	if err != nil {
		t.Fatalf("monster poison after refreshed interval error = %v", err)
	}
	if len(hits) != 1 || monster.HP >= 100 {
		t.Fatalf("monster poison after refreshed interval = hits:%d hp:%d, want damage", len(hits), monster.HP)
	}
}

func TestArmorPoisonStoresReferenceGreenPoint(t *testing.T) {
	now := time.Unix(40, 0)
	character := storage.Character{}
	if !setCharacterArmorPoisonLocked(&character, 7, now.Add(time.Minute), now) || character.PoisonHealthPoint != 7 {
		t.Fatalf("character armor poison point = %d, want 7", character.PoisonHealthPoint)
	}

	monster := &Monster{}
	if !setMonsterArmorPoisonLocked(monster, 7, now.Add(time.Minute), now) || monster.PoisonHealthPoint != 7 {
		t.Fatalf("monster armor poison point = %d, want 7", monster.PoisonHealthPoint)
	}
}

func TestPoisonDamageTickUsesConfiguredInterval(t *testing.T) {
	now := time.Unix(40, 0)
	w := &World{}
	w.gameplay.Combat.PoisonTickIntervalMS = 1000
	character := storage.Character{HP: 100, MaxHP: 100, PoisonHealthLevel: 1, PoisonHealthStartAt: now.Add(-time.Second).UnixNano(), PoisonHealthUntil: now.Add(time.Minute).UnixNano(), PoisonHealthTickAt: now.Add(-time.Second).UnixNano()}
	updated, changed := w.applyCharacterPoisonTickLocked(character, now)
	if changed || updated.HP != character.HP {
		t.Fatalf("configured poison tick fired at exact interval: changed=%t hp=%d", changed, updated.HP)
	}
	updated, changed = w.applyCharacterPoisonTickLocked(character, now.Add(time.Nanosecond))
	if !changed || updated.HP >= character.HP {
		t.Fatalf("configured poison tick did not fire after interval: changed=%t hp=%d", changed, updated.HP)
	}
}

func TestPoisonDamageTickUsesAppliedPointNotStatusLevel(t *testing.T) {
	now := time.Unix(40, 0)
	character := storage.Character{
		HP: 100, MaxHP: 100, PoisonHealthLevel: 1, PoisonHealthPoint: 8,
		PoisonHealthStartAt: now.Add(-time.Second).UnixNano(), PoisonHealthUntil: now.Add(time.Minute).UnixNano(),
		PoisonHealthTickAt: now.Add(-poisonHealthTickInterval - time.Nanosecond).UnixNano(),
	}
	updated, changed := (&World{}).applyCharacterPoisonTickLocked(character, now)
	if !changed || updated.HP != 91 {
		t.Fatalf("character poison damage = %d, changed=%t, want 9 damage from applied point", 100-updated.HP, changed)
	}

	monster := &Monster{
		ID: "poison-point-monster", Alive: true, HP: 100, MaxHP: 100,
		HealthTick: 17, SpellTick: 23,
		PoisonHealthLevel: 1, PoisonHealthPoint: 8,
		PoisonHealthStartAt: now.Add(-time.Second), PoisonHealthUntil: now.Add(time.Minute),
		PoisonHealthTickAt: now.Add(-poisonHealthTickInterval - time.Nanosecond),
	}
	hits, _, err := (&World{}).applyMonsterPoisonTickLocked(monster, nil, now)
	if err != nil {
		t.Fatalf("monster poison point tick error = %v", err)
	}
	if len(hits) != 1 || monster.HP != 91 || hits[0].Damage != 9 || monster.HealthTick != 0 || monster.SpellTick != 0 {
		t.Fatalf("monster poison damage = %d, hp=%d, recovery ticks=%d/%d, want 9 damage and reset ticks", hits[0].Damage, monster.HP, monster.HealthTick, monster.SpellTick)
	}
}

func TestSourceLessMonsterPoisonDeathDefersSettlement(t *testing.T) {
	now := time.Unix(40, 0)
	monster := &Monster{
		ID: "poison-death-monster", Alive: true, HP: 1, MaxHP: 100,
		PoisonHealthLevel: 1, PoisonHealthStartAt: now.Add(-time.Second), PoisonHealthUntil: now.Add(time.Minute),
		PoisonHealthTickAt: now.Add(-poisonHealthTickInterval - time.Nanosecond),
	}
	w := &World{}
	hits, dead, err := w.applyMonsterPoisonTickLocked(monster, nil, now)
	if err != nil {
		t.Fatalf("monster poison death tick error = %v", err)
	}
	if !dead || len(hits) != 1 || !monster.PendingDeath || monster.Alive == false {
		t.Fatalf("source-less poison death = dead:%t hits:%d pending:%t alive:%t, want deferred pending death state", dead, len(hits), monster.PendingDeath, monster.Alive)
	}
}

func TestSourceLessMonsterPoisonDeathUsesLastHitterOnNextTick(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	now := time.Unix(40, 0)
	monster := &Monster{
		ID: "poison-last-hitter", MapID: caster.MapID, X: caster.X, Y: caster.Y, Alive: true, HP: 1, MaxHP: 100,
		Experience: 25, LastHitterID: caster.ID, LastHitterAt: now,
		PoisonHealthLevel: 1, PoisonHealthStartAt: now.Add(-time.Second), PoisonHealthUntil: now.Add(time.Minute),
		PoisonHealthTickAt: now.Add(-poisonHealthTickInterval - time.Nanosecond),
	}
	w.mu.Lock()
	w.monsters = map[string]*Monster{monster.ID: monster}
	w.occupied = map[monsterPosition]string{}
	w.mu.Unlock()

	first, err := w.Tick([]PlayerSnapshot{{Character: caster}}, now)
	if err != nil {
		t.Fatalf("first Tick() error = %v", err)
	}
	if len(first.MonsterDeaths) != 0 || !monster.PendingDeath {
		t.Fatalf("first tick = deaths:%d pending:%t, want deferred death", len(first.MonsterDeaths), monster.PendingDeath)
	}
	second, err := w.Tick([]PlayerSnapshot{{Character: caster}}, now.Add(time.Second))
	if err != nil {
		t.Fatalf("second Tick() error = %v", err)
	}
	if len(second.MonsterDeaths) != 1 || second.MonsterDeaths[0].Experience != 25 {
		t.Fatalf("second tick deaths = %+v, want last-hitter experience", second.MonsterDeaths)
	}
}

func TestMonsterPoisonDamageTickBypassesDefenseAndArmorMultiplier(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	now := time.Now()
	mon := &Monster{
		ID: "poison-damage-monster", MapID: caster.MapID, Alive: true, HP: 100, MaxHP: 100,
		HealthTick: 17, SpellTick: 23,
		Defense: 100, PoisonArmorLevel: poisonDamageArmorRate,
		PoisonArmorStartAt: now.Add(-time.Minute), PoisonArmorUntil: now.Add(time.Minute),
		PoisonHealthLevel: 9, PoisonHealthPoint: 9, PoisonHealthStartAt: now.Add(-time.Second), PoisonHealthUntil: now.Add(time.Minute),
		PoisonHealthTickAt: now.Add(-poisonHealthTickInterval - time.Nanosecond),
	}
	w.mu.Lock()
	w.monsters[mon.ID] = mon
	hits, _, err := w.applyMonsterPoisonTickLocked(mon, map[string]storage.Character{caster.ID: caster}, now)
	w.mu.Unlock()
	if err != nil {
		t.Fatalf("applyMonsterPoisonTickLocked() error = %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("poison hits = %d, want one", len(hits))
	}
	if got, want := mon.HP, 90; got != want {
		t.Fatalf("monster HP = %d, want %d direct poison damage without defense or armor multiplier", got, want)
	}
	if mon.HealthTick != 0 || mon.SpellTick != 0 {
		t.Fatalf("monster recovery ticks = %d/%d, want reset after sourced poison damage", mon.HealthTick, mon.SpellTick)
	}
}

func TestMonsterPoisonDeathDoesNotInferOfflineSource(t *testing.T) {
	w, _ := newTestWorldCharacter(t)
	now := time.Unix(100, 0)
	mon := &Monster{ID: "poison-dead", HP: 1, MaxHP: 10, Alive: true, PoisonHealthLevel: 0, PoisonHealthStartAt: now.Add(-time.Second), PoisonHealthUntil: now.Add(time.Minute), PoisonHealthTickAt: now.Add(-4 * time.Second), PoisonSourceID: "offline-caster"}
	hits, dead, err := w.applyMonsterPoisonTickLocked(mon, nil, now)
	if err != nil {
		t.Fatalf("applyMonsterPoisonTickLocked() error = %v", err)
	}
	if !dead || len(hits) != 1 || mon.DeathHitterID != "" {
		t.Fatalf("poison death = dead:%v hits:%d hitter:%q, want no inferred hitter", dead, len(hits), mon.DeathHitterID)
	}
}

func TestWorldTickDoesNotUsePoisonSourceAsDeathHitter(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	now := time.Unix(100, 0)
	mon := &Monster{ID: "poison-fallback", MapID: caster.MapID, X: caster.X, Y: caster.Y, Alive: true, HP: 0, MaxHP: 10, PendingDeath: true, DeathHitterID: caster.ID, PoisonSourceID: caster.ID, Experience: 25}
	w.mu.Lock()
	w.monsters[mon.ID] = mon
	w.occupied[monsterPosition{MapID: mon.MapID, X: mon.X, Y: mon.Y}] = mon.ID
	w.mu.Unlock()
	result, err := w.Tick([]PlayerSnapshot{{Character: caster}}, now)
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if len(result.MonsterDeaths) != 1 || result.MonsterDeaths[0].Experience != 0 {
		t.Fatalf("poison death fallback = %+v, want no experience", result.MonsterDeaths)
	}
}

func TestMonsterPhysicalDamageUsesOnePoisonArmorMultiplier(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	now := time.Now()
	mon := &Monster{
		ID: "poison-armor-physical-monster", MapID: caster.MapID, Alive: true, HP: 200, MaxHP: 200,
		PoisonArmorLevel:   poisonDamageArmorRate,
		PoisonArmorStartAt: now.Add(-time.Minute), PoisonArmorUntil: now.Add(time.Minute),
	}

	hit, err := w.attackMonsterWithDamageModeAndMeatLocked(caster, mon, 100, true, false, true, false, false)
	if err != nil {
		t.Fatalf("attackMonsterWithDamageModeLocked() error = %v", err)
	}
	if hit.Damage != 120 || mon.HP != 80 {
		t.Fatalf("physical poison-armor damage = %d, hp = %d, want damage 120 and hp 80", hit.Damage, mon.HP)
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

func TestDeadCharacterAdvancesNaturalSpellClockWithoutRecovery(t *testing.T) {
	w := &World{}
	now := time.UnixMilli(1400)
	ch := storage.Character{HP: 0, MaxHP: 100, MP: 50, MaxMP: 100, HealthTickAt: 1000, SpellTickAt: 1000}

	if !w.advanceCharacterNaturalSpellClockLocked(&ch, now) {
		t.Fatal("dead natural spell clock did not advance")
	}
	if ch.HP != 0 || ch.MP != 50 || ch.HealthTick != 20 || ch.SpellTick != 20 {
		t.Fatalf("dead natural spell state = hp:%d mp:%d health:%d spell:%d, want no recovery with elapsed clocks", ch.HP, ch.MP, ch.HealthTick, ch.SpellTick)
	}
	if ch.HealthTickAt != now.UnixMilli() || ch.SpellTickAt != now.UnixMilli() {
		t.Fatalf("dead natural spell timestamps = %d/%d, want %d", ch.HealthTickAt, ch.SpellTickAt, now.UnixMilli())
	}
}

func TestMonsterNaturalSpellTickKeepsHealthFrameOrder(t *testing.T) {
	w := &World{}
	now := time.UnixMilli(1400)
	mon := &Monster{
		HP: 1, MaxHP: 100, MP: 1, MaxMP: 100,
		HealthTick: 300, SpellTick: 800, HealthTickAt: 1400, SpellTickAt: 1400,
		Alive: true,
	}
	changes := w.applyMonsterNaturalSpellTickLocked(mon, now)
	if len(changes) != 2 {
		t.Fatalf("natural monster recovery frames = %d, want 2", len(changes))
	}
	if changes[0].HP != 3 || changes[0].MP != 1 || changes[1].HP != 3 || changes[1].MP != 7 {
		t.Fatalf("natural monster health frames = %+v, want hp-only then hp/mp 3/7", changes)
	}
}

func TestMonsterNaturalSpellTickSettlesNegativeHealthTick(t *testing.T) {
	w := &World{}
	mon := &Monster{HP: 10, MaxHP: 100, HealthTick: -321, Alive: true}
	if !w.applyMonsterNegativeHealthTickLocked(mon) {
		t.Fatal("negative monster health tick did not settle")
	}
	if mon.HP != 9 || mon.HealthTick != -21 {
		t.Fatalf("negative monster health tick = hp:%d tick:%d, want hp:9 tick:-21", mon.HP, mon.HealthTick)
	}
}

func TestCharacterNaturalSpellTickKeepsHealthFrameOrder(t *testing.T) {
	w := &World{}
	now := time.UnixMilli(1400)
	ch := storage.Character{
		HP: 1, MaxHP: 100, MP: 1, MaxMP: 100,
		HealthTick: 300, SpellTick: 800, HealthTickAt: 1400, SpellTickAt: 1400,
	}
	changed, frames := w.applyCharacterNaturalSpellTickTraceLocked(&ch, now)
	if !changed || len(frames) != 2 {
		t.Fatalf("natural recovery = changed:%t frames:%d, want true/2", changed, len(frames))
	}
	if frames[0].HP != 3 || frames[0].MP != 1 || frames[1].HP != 3 || frames[1].MP != 7 {
		t.Fatalf("health frames = %+v, want hp-only then hp/mp 3/7", frames)
	}
}

func TestCharacterNaturalSpellTickRecoversHealth(t *testing.T) {
	w := &World{}
	now := time.UnixMilli(1400)
	ch := storage.Character{HP: 1, MaxHP: 100, MP: 100, MaxMP: 100, HealthTick: 280, HealthTickAt: 1000}
	if !w.applyCharacterNaturalSpellTickLocked(&ch, now) {
		t.Fatal("natural health tick did not recover health")
	}
	if ch.HP != 3 || ch.HealthTick != 0 || ch.HealthTickAt != 1400 {
		t.Fatalf("character natural recovery = %+v, want hp=3 tick=0 at=1400", ch)
	}
}

func TestCharacterNegativeHealthTickSettlesAfterQueuedRecovery(t *testing.T) {
	w := &World{}
	ch := storage.Character{HP: 10, MaxHP: 100, HealthTick: -321}
	if !w.applyCharacterNegativeHealthTickLocked(&ch) {
		t.Fatal("negative health tick did not settle")
	}
	if ch.HP != 9 || ch.HealthTick != -21 {
		t.Fatalf("negative health tick = %+v, want hp=9 tick=-21", ch)
	}
}

func TestCharacterNegativeHealthTickDoesNotKillFromOneHealth(t *testing.T) {
	w := &World{}
	ch := storage.Character{HP: 1, MaxHP: 100, HealthTick: -321}
	if w.applyCharacterNegativeHealthTickLocked(&ch) {
		t.Fatal("negative health tick changed one-health character")
	}
	if ch.HP != 1 || ch.HealthTick != -321 {
		t.Fatalf("negative health tick = %+v, want hp=1 tick=-321", ch)
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
	target := storage.Character{ID: "target", MapID: caster.MapID, HP: 100, MaxHP: 100, HealthTick: 700, SpellTick: 700}
	w.mu.Lock()
	updated, hit, err := w.spellCharacterDamageWithPowerLocked(caster, target, 1)
	w.mu.Unlock()
	if err != nil {
		t.Fatalf("spellCharacterDamageWithPowerLocked() error = %v", err)
	}
	if hit.Damage <= 0 || updated.HealthTick != 0 || updated.SpellTick != 0 {
		t.Fatalf("magic damage result = hit:%+v character:%+v, want positive damage and zero natural recovery ticks", hit, updated)
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
			if updated.MP != 7 {
				t.Fatalf("target MP = %d, want 7 after pending magic damage and natural recovery", updated.MP)
			}
			if updated.HP >= target.HP {
				t.Fatalf("target HP = %d, want damage before recovery", updated.HP)
			}
			return
		}
	}
	t.Fatal("updated target missing")
}

func TestPendingMagicDamageDoesNotRequireCharacterPersistence(t *testing.T) {
	bundle := loadTestBundle(t)
	storePath := filepath.Join(t.TempDir(), "state.json")
	store, err := storage.Open(storePath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	w := New(bundle, store)
	mapID, x, y := defaultSpawn(bundle)
	caster, err := w.CreateCharacterWithAppearance("test", "pending-error-caster", "wizard", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	target := storage.Character{ID: "pending-error-target", MapID: mapID, X: x + 1, Y: y, HP: 100, MaxHP: 100}
	now := time.Now()
	w.mu.Lock()
	w.pendingSpells = []pendingSpell{{
		DueAt: now.Add(-time.Second), CasterID: caster.ID, TargetCharacterID: target.ID,
		CharacterDamage: true, TargetX: target.X, TargetY: target.Y, Damage: 1,
	}}
	w.mu.Unlock()
	if err := os.Remove(storePath); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if err := os.Mkdir(storePath, 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	result, err := w.Tick([]PlayerSnapshot{{Character: caster}, {Character: target}}, now)
	if err != nil {
		t.Fatalf("Tick() error = %v, want no persistence-dependent failure", err)
	}
	if len(result.CharacterHits) != 1 {
		t.Fatalf("CharacterHits = %d, want delayed damage result", len(result.CharacterHits))
	}
}

func TestPendingSpellDropsGhostTargetsLikeReferenceSendMsg(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	now := time.Now()
	character := storage.Character{ID: "ghost-character", MapID: caster.MapID, X: caster.X + 1, Y: caster.Y, HP: 100, MaxHP: 100, Ghost: true}
	monster := &Monster{ID: "ghost-monster", MapID: caster.MapID, X: caster.X + 1, Y: caster.Y, HP: 100, MaxHP: 100, Alive: true, Ghost: true}
	w.mu.Lock()
	w.monsters[monster.ID] = monster
	w.pendingSpells = []pendingSpell{
		{DueAt: now.Add(-time.Second), CasterID: caster.ID, TargetCharacterID: character.ID, CharacterDamage: true, TargetX: character.X, TargetY: character.Y, Damage: 10},
		{DueAt: now.Add(-time.Second), CasterID: caster.ID, TargetMonsterID: monster.ID, TargetX: monster.X, TargetY: monster.Y, Damage: 10},
	}
	w.mu.Unlock()

	result, err := w.Tick([]PlayerSnapshot{{Character: caster}, {Character: character}}, now)
	if err != nil {
		t.Fatalf("Tick() = %v", err)
	}
	if len(result.CharacterHits) != 0 || len(result.MonsterHits) != 0 {
		t.Fatalf("ghost delayed hits = characters:%d monsters:%d, want none", len(result.CharacterHits), len(result.MonsterHits))
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if got := w.monsters[monster.ID].HP; got != 100 {
		t.Fatalf("ghost monster HP = %d, want unchanged", got)
	}
}

func TestPendingSpellPersistenceFailureDoesNotChangeQueueOrder(t *testing.T) {
	bundle := loadTestBundle(t)
	storePath := filepath.Join(t.TempDir(), "state.json")
	store, err := storage.Open(storePath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	w := New(bundle, store)
	mapID, x, y := defaultSpawn(bundle)
	caster, err := w.CreateCharacterWithAppearance("test", "pending-order-caster", "wizard", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	monster := &Monster{ID: "pending-order-monster", MapID: mapID, X: x + 1, Y: y, Alive: true, HP: 100, MaxHP: 100}
	target := storage.Character{ID: "pending-order-target", MapID: mapID, X: x + 1, Y: y, HP: 100, MaxHP: 100}
	now := time.Now()
	w.mu.Lock()
	w.monsters[monster.ID] = monster
	w.pendingSpells = []pendingSpell{
		{DueAt: now.Add(-time.Second), TargetMonsterID: monster.ID, Healing: 1},
		{DueAt: now.Add(time.Second), TargetMonsterID: "future-target", Healing: 2},
		{DueAt: now.Add(-time.Second), CasterID: caster.ID, TargetCharacterID: target.ID, CharacterDamage: true, TargetX: target.X, TargetY: target.Y, Damage: 1},
	}
	w.mu.Unlock()
	if err := os.Remove(storePath); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if err := os.Mkdir(storePath, 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	_, err = w.Tick([]PlayerSnapshot{{Character: caster}, {Character: target}}, now)
	if err != nil {
		t.Fatalf("Tick() error = %v, want no persistence-dependent failure", err)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.pendingSpells) != 1 {
		t.Fatalf("pending spells = %d, want only future event", len(w.pendingSpells))
	}
	if got := w.pendingSpells[0].TargetMonsterID; got != "future-target" {
		t.Fatalf("remaining pending target = %q, want future-target", got)
	}
}

func TestDoSpellPersistenceFailureDoesNotRollbackPendingEffects(t *testing.T) {
	bundle := loadTestBundle(t)
	storePath := filepath.Join(t.TempDir(), "state.json")
	store, err := storage.Open(storePath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	w := New(bundle, store)
	mapID, x, y := defaultSpawn(bundle)
	caster, err := w.CreateCharacterWithAppearance("test", "spell-error-caster", "wizard", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	caster.Skills = storage.SkillStates{{ID: "火球术", Level: 0}}
	caster.MP = 100
	target := Monster{ID: "spell-error-target", Name: "鹿", MapID: mapID, X: x + 1, Y: y, Alive: true, HP: 100, MaxHP: 100, Race: 50}
	w.mu.Lock()
	w.monsters[target.ID] = &target
	w.mu.Unlock()
	if err := os.Remove(storePath); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if err := os.Mkdir(storePath, 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	result, err := w.DoSpell(caster, "火球术", target.X, target.Y, MonsterActorID(target), []storage.Character{caster})
	if err != nil {
		t.Fatalf("DoSpell() error = %v, want no persistence-dependent failure", err)
	}
	if !result.SpellStarted {
		t.Fatalf("DoSpell() result = %+v, want spell started", result)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.pendingSpells) == 0 {
		t.Fatalf("pendingSpells = %d, want pending spell retained", len(w.pendingSpells))
	}
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

func TestMixedMonsterDamageAppliesMagicBubbleToBothHalves(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	now := time.Now()
	caster.HP = 100
	caster.MaxHP = 100
	caster.BubbleDefenceLevel = 0
	caster.BubbleDefenceUntil = now.Add(time.Minute).UnixNano()
	mon := &Monster{ID: "mixed-bubble", MinAttack: 20, MaxAttack: 20}
	w.mu.Lock()
	updated, hit, err := w.monsterMixedAttackCharacterWithPowerAtLocked(mon, caster, 20, now)
	w.mu.Unlock()
	if err != nil {
		t.Fatalf("monsterMixedAttackCharacterWithPowerAtLocked() error = %v", err)
	}
	if hit.Damage != 4 || updated.HP != 96 {
		t.Fatalf("mixed bubble damage = %d hp:%d, want damage:4 hp:96", hit.Damage, updated.HP)
	}
}

func TestCharacterMagicShieldRingConsumesMPBeforeHP(t *testing.T) {
	w := &World{data: data.StdBundle{Items: map[string]data.StdItem{
		"magic-shield-ring": {ID: "magic-shield-ring", Shape: 118, DuraMax: 5000},
	}}}
	target := storage.Character{MP: 10, MaxMP: 10, HP: 100, MaxHP: 100, EquippedItems: map[int]storage.UserItem{
		SlotRingL: {ItemID: "magic-shield-ring", Dura: 1000},
	}}
	w.mu.Lock()
	damage := w.applyCharacterMagicShieldLocked(&target, 10)
	w.mu.Unlock()
	if damage != 3 || target.MP != 0 {
		t.Fatalf("magic shield ring result = damage:%d mp:%d, want damage:3 mp:0", damage, target.MP)
	}
}

func TestCharacterMagicShieldIgnoresBrokenRing(t *testing.T) {
	w := &World{data: data.StdBundle{Items: map[string]data.StdItem{
		"magic-shield-ring": {ID: "magic-shield-ring", Shape: 118, DuraMax: 5000},
	}}}
	target := storage.Character{MP: 10, MaxMP: 10, HP: 100, MaxHP: 100, EquippedItems: map[int]storage.UserItem{
		SlotRingL: {ItemID: "magic-shield-ring", Dura: 0},
	}}
	w.mu.Lock()
	damage := w.applyCharacterMagicShieldLocked(&target, 10)
	w.mu.Unlock()
	if damage != 10 || target.MP != 10 {
		t.Fatalf("broken magic shield ring result = damage:%d mp:%d, want damage:10 mp:10", damage, target.MP)
	}
}

func TestCharacterMagicShieldBlockedByReferenceItem(t *testing.T) {
	w := &World{data: data.StdBundle{Items: map[string]data.StdItem{
		"magic-shield-ring": {ID: "magic-shield-ring", Shape: 118, DuraMax: 5000},
		"shield-break-ring": {ID: "shield-break-ring", Shape: 143, DuraMax: 5000},
	}}}
	target := storage.Character{MP: 10, MaxMP: 10, HP: 100, MaxHP: 100, EquippedItems: map[int]storage.UserItem{
		SlotRingL: {ItemID: "magic-shield-ring", Dura: 1000},
		SlotRingR: {ItemID: "shield-break-ring", Dura: 1000},
	}}
	w.mu.Lock()
	damage := w.applyCharacterMagicShieldLocked(&target, 10)
	w.mu.Unlock()
	if damage != 10 || target.MP != 10 {
		t.Fatalf("blocked magic shield result = damage:%d mp:%d, want damage:10 mp:10", damage, target.MP)
	}
}

func TestCharacterProtectionBlockersRespectReferenceEquipmentSlots(t *testing.T) {
	w := &World{data: data.StdBundle{Items: map[string]data.StdItem{
		"wrong-ring-immunity":   {ID: "wrong-ring-immunity", AniCount: 143},
		"wrong-dress-immunity":  {ID: "wrong-dress-immunity", Shape: 143},
		"wrong-ring-paralysis":  {ID: "wrong-ring-paralysis", AniCount: 139},
		"wrong-dress-paralysis": {ID: "wrong-dress-paralysis", Shape: 139},
	}}}
	for name, equipped := range map[string]map[int]storage.UserItem{
		"ring AniCount": {SlotRingL: {ItemID: "wrong-ring-immunity", Dura: 1}},
		"dress Shape":   {SlotDress: {ItemID: "wrong-dress-immunity", Dura: 1}},
	} {
		t.Run(name, func(t *testing.T) {
			if w.characterMagicShieldBlockedLocked(storage.Character{EquippedItems: equipped}) {
				t.Fatal("wrong-slot magic-shield blocker was active")
			}
		})
	}
	for name, equipped := range map[string]map[int]storage.UserItem{
		"ring AniCount": {SlotRingL: {ItemID: "wrong-ring-paralysis", Dura: 1}},
		"dress Shape":   {SlotDress: {ItemID: "wrong-dress-paralysis", Dura: 1}},
	} {
		t.Run(name, func(t *testing.T) {
			if w.characterCannotParalyzeLocked(storage.Character{EquippedItems: equipped}) {
				t.Fatal("wrong-slot paralysis blocker was active")
			}
		})
	}
}

func TestParalysisRingAppliesReferenceAttackStatus(t *testing.T) {
	w := &World{data: data.StdBundle{Items: map[string]data.StdItem{
		"paralysis-ring": {ID: "paralysis-ring", Shape: 113},
	}}}
	w.rand = rand.New(&seqSource{vals: []int64{0}})
	attacker := storage.Character{EquippedItems: map[int]storage.UserItem{SlotRingL: {ItemID: "paralysis-ring"}}}
	target := storage.Character{AntiPoison: 0}
	w.mu.Lock()
	changed := w.tryApplyParalysisRingToCharacterLocked(attacker, &target)
	w.mu.Unlock()
	if !changed || target.ParalyzedUntil <= time.Now().UnixNano() {
		t.Fatalf("paralysis ring status = changed:%v until:%d, want active status", changed, target.ParalyzedUntil)
	}
}

func TestBrokenParalysisRingDoesNotApplyAttackStatus(t *testing.T) {
	w := &World{data: data.StdBundle{Items: map[string]data.StdItem{
		"paralysis-ring": {ID: "paralysis-ring", Shape: 113, DuraMax: 5000},
	}}}
	w.rand = rand.New(&seqSource{vals: []int64{0}})
	attacker := storage.Character{EquippedItems: map[int]storage.UserItem{
		SlotRingL: {ItemID: "paralysis-ring", Dura: 0},
	}}
	target := storage.Character{}
	w.mu.Lock()
	changed := w.tryApplyParalysisRingToCharacterLocked(attacker, &target)
	w.mu.Unlock()
	if changed || target.ParalyzedUntil != 0 {
		t.Fatalf("broken paralysis ring status = changed:%v until:%d, want inactive", changed, target.ParalyzedUntil)
	}
}

func TestDurableParalysisRingAppliesAttackStatus(t *testing.T) {
	w := &World{data: data.StdBundle{Items: map[string]data.StdItem{
		"paralysis-ring": {ID: "paralysis-ring", Shape: 113, DuraMax: 5000},
	}}}
	w.rand = rand.New(&seqSource{vals: []int64{0}})
	attacker := storage.Character{EquippedItems: map[int]storage.UserItem{
		SlotRingL: {ItemID: "paralysis-ring", Dura: 1},
	}}
	target := storage.Character{}
	w.mu.Lock()
	changed := w.tryApplyParalysisRingToCharacterLocked(attacker, &target)
	w.mu.Unlock()
	if !changed || target.ParalyzedUntil <= time.Now().UnixNano() {
		t.Fatalf("durable paralysis ring status = changed:%v until:%d, want active status", changed, target.ParalyzedUntil)
	}
}

func TestSpecialRingProvidesReferenceSkillWithoutPersistingIt(t *testing.T) {
	w := &World{data: data.StdBundle{Items: map[string]data.StdItem{
		"flame-ring":    {ID: "flame-ring", Shape: 115},
		"recovery-ring": {ID: "recovery-ring", Shape: 116},
	}}}
	ch := storage.Character{EquippedItems: map[int]storage.UserItem{
		SlotRingL: {ItemID: "flame-ring"}, SlotRingR: {ItemID: "recovery-ring"},
	}}
	states := w.EffectiveSkillStates(ch)
	if len(states) != 2 || states[0].ID != "火球术" || states[1].ID != "治愈术" {
		t.Fatalf("effective ring skills = %+v, want fireball and heal", states)
	}
	if len(ch.Skills) != 0 {
		t.Fatalf("persisted skills = %+v, want unchanged", ch.Skills)
	}
}

func TestBrokenSpecialRingDoesNotProvideReferenceSkill(t *testing.T) {
	w := &World{data: data.StdBundle{Items: map[string]data.StdItem{
		"flame-ring": {ID: "flame-ring", Shape: 115, DuraMax: 5000},
	}}}
	ch := storage.Character{EquippedItems: map[int]storage.UserItem{
		SlotRingL: {ItemID: "flame-ring", Dura: 0},
	}}
	if states := w.EffectiveSkillStates(ch); len(states) != 0 {
		t.Fatalf("broken special ring skills = %+v, want none", states)
	}
}

func TestDurableSpecialRingProvidesReferenceSkill(t *testing.T) {
	w := &World{data: data.StdBundle{Items: map[string]data.StdItem{
		"flame-ring": {ID: "flame-ring", Shape: 115, DuraMax: 5000},
	}}}
	ch := storage.Character{EquippedItems: map[int]storage.UserItem{
		SlotRingL: {ItemID: "flame-ring", Dura: 1},
	}}
	states := w.EffectiveSkillStates(ch)
	if len(states) != 1 || states[0].ID != "火球术" || states[0].Level != 1 {
		t.Fatalf("durable special ring skills = %+v, want fireball", states)
	}
}

func TestSpecialRingEquipChangesExposeReferenceSkillEvents(t *testing.T) {
	w := &World{data: data.StdBundle{Items: map[string]data.StdItem{
		"flame-ring": {ID: "flame-ring", Shape: 115},
	}}}
	before := storage.Character{}
	after := storage.Character{EquippedItems: map[int]storage.UserItem{
		SlotRingL: {ItemID: "flame-ring"},
	}}
	added, removed := w.itemSkillChangesLocked(before, after)
	if len(added) != 1 || added[0].ID != "火球术" || added[0].Level != 1 {
		t.Fatalf("added ring skills = %+v, want level-one fireball", added)
	}
	if len(removed) != 0 {
		t.Fatalf("removed ring skills = %+v, want none", removed)
	}
	added, removed = w.itemSkillChangesLocked(after, before)
	if len(added) != 0 || len(removed) != 1 || removed[0] != "火球术" {
		t.Fatalf("removed ring skills = added:%+v removed:%+v, want fireball removal", added, removed)
	}
	after.Skills = storage.SkillStates{{ID: "火球术", Level: 3}}
	added, removed = w.itemSkillChangesLocked(after, before)
	if len(added) != 0 || len(removed) != 0 {
		t.Fatalf("persisted fireball changes = added:%+v removed:%+v, want none", added, removed)
	}
}

func TestBrokenSpecialRingDoesNotExposeReferenceSkillEvents(t *testing.T) {
	w := &World{data: data.StdBundle{Items: map[string]data.StdItem{
		"flame-ring": {ID: "flame-ring", Shape: 115, DuraMax: 5000},
	}}}
	before := storage.Character{}
	after := storage.Character{EquippedItems: map[int]storage.UserItem{
		SlotRingL: {ItemID: "flame-ring", Dura: 0},
	}}
	added, removed := w.itemSkillChangesLocked(before, after)
	if len(added) != 0 || len(removed) != 0 {
		t.Fatalf("broken ring skill changes = added:%+v removed:%+v, want none", added, removed)
	}
}

func TestStealthRingSetsReferenceTransparentDuration(t *testing.T) {
	w := &World{data: data.StdBundle{Items: map[string]data.StdItem{
		"stealth-ring": {ID: "stealth-ring", Shape: 111},
	}}}
	ch := storage.Character{EquippedItems: map[int]storage.UserItem{SlotRingL: {ItemID: "stealth-ring"}}}
	updated := w.RegisterCharacter(ch)
	if updated.TransparentUntil <= time.Now().Add(59*time.Second).UnixNano() {
		t.Fatalf("TransparentUntil = %d, want roughly 60 seconds", updated.TransparentUntil)
	}
}

func TestBrokenStealthRingDoesNotSetTransparentDuration(t *testing.T) {
	w := &World{data: data.StdBundle{Items: map[string]data.StdItem{
		"stealth-ring": {ID: "stealth-ring", Shape: 111, DuraMax: 5000},
	}}}
	ch := storage.Character{EquippedItems: map[int]storage.UserItem{
		SlotRingL: {ItemID: "stealth-ring", Dura: 0},
	}}
	updated := w.RegisterCharacter(ch)
	if updated.TransparentUntil != 0 {
		t.Fatalf("broken stealth ring TransparentUntil = %d, want 0", updated.TransparentUntil)
	}
}

func TestDurableStealthRingSetsTransparentDuration(t *testing.T) {
	w := &World{data: data.StdBundle{Items: map[string]data.StdItem{
		"stealth-ring": {ID: "stealth-ring", Shape: 111, DuraMax: 5000},
	}}}
	ch := storage.Character{EquippedItems: map[int]storage.UserItem{
		SlotRingL: {ItemID: "stealth-ring", Dura: 1},
	}}
	updated := w.RegisterCharacter(ch)
	if updated.TransparentUntil <= time.Now().Add(59*time.Second).UnixNano() {
		t.Fatalf("durable stealth ring TransparentUntil = %d, want roughly 60 seconds", updated.TransparentUntil)
	}
}

func TestStealthRingUsesReferenceEquipmentFieldBySlot(t *testing.T) {
	w := &World{data: data.StdBundle{Items: map[string]data.StdItem{
		"shape-stealth": {ID: "shape-stealth", Shape: 111},
		"ani-stealth":   {ID: "ani-stealth", AniCount: 111},
	}}}

	shapeOnWeapon := w.RegisterCharacter(storage.Character{EquippedItems: map[int]storage.UserItem{
		SlotWeapon: {ItemID: "shape-stealth"},
	}})
	if shapeOnWeapon.TransparentUntil != 0 {
		t.Fatalf("weapon Shape=111 enabled stealth, want no stealth")
	}
	shapeOnNecklace := w.RegisterCharacter(storage.Character{EquippedItems: map[int]storage.UserItem{
		SlotNecklace: {ItemID: "shape-stealth"},
	}})
	if shapeOnNecklace.TransparentUntil != 0 {
		t.Fatalf("necklace Shape=111 enabled stealth, want no stealth")
	}
	aniOnRing := w.RegisterCharacter(storage.Character{EquippedItems: map[int]storage.UserItem{
		SlotRingL: {ItemID: "ani-stealth"},
	}})
	if aniOnRing.TransparentUntil != 0 {
		t.Fatalf("ring AniCount=111 enabled stealth, want no stealth")
	}
}

func TestBrokenAntiMagicEquipmentDoesNotBlockMagicHit(t *testing.T) {
	w := &World{data: data.StdBundle{Items: map[string]data.StdItem{
		"broken-anti-magic": {ID: "broken-anti-magic", Shape: 112, DuraMax: 5000},
	}}, rand: rand.New(rand.NewSource(1))}
	target := storage.Character{EquippedItems: map[int]storage.UserItem{
		SlotRingL: {ItemID: "broken-anti-magic", Dura: 0},
	}}
	w.mu.Lock()
	allowed := w.characterMagicHitAllowedLocked(target)
	w.mu.Unlock()
	if !allowed {
		t.Fatal("broken anti-magic equipment blocked a magic hit")
	}
}

func TestBrokenParalysisImmunityDoesNotBlockParalysis(t *testing.T) {
	w := &World{data: data.StdBundle{Items: map[string]data.StdItem{
		"broken-immunity": {ID: "broken-immunity", Shape: 139, DuraMax: 5000},
	}}}
	target := storage.Character{EquippedItems: map[int]storage.UserItem{
		SlotRingL: {ItemID: "broken-immunity", Dura: 0},
	}}
	if w.characterCannotParalyzeLocked(target) {
		t.Fatal("broken paralysis-immunity equipment blocked paralysis")
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
	w.data.Items["target-armor"] = data.StdItem{ID: "target-armor", StdMode: 10, Stats: data.StdItemStats{AcMin: 20 | (20 << 8)}}
	players := []storage.Character{target}
	result, err := w.HitWithIdent(caster, caster.X, caster.Y, 0, mir176.CMHit, players...)
	if err != nil {
		t.Fatalf("HitWithIdent() error = %v", err)
	}
	if len(result.CharacterHits) != 0 {
		t.Fatalf("primary character hit = %+v, want no result after physical defense", result.CharacterHits)
	}
}

func TestChargeDamageUsesReferencePhysicalDefense(t *testing.T) {
	w := &World{
		data: data.StdBundle{Items: map[string]data.StdItem{
			"charge-armor": {ID: "charge-armor", StdMode: 10, Stats: data.StdItemStats{AcMin: 10 | (10 << 8)}},
		}},
		rand: rand.New(&seqSource{vals: []int64{0}}),
	}
	target := storage.Character{Class: "warrior", Level: 1, EquippedItems: map[int]storage.UserItem{SlotDress: {ItemID: "charge-armor"}}}
	if got := w.characterPhysicalDamageAfterDefenseLocked(&target, 20); got != 10 {
		t.Fatalf("charge damage after physical defense = %d, want 10", got)
	}
}

func TestCharacterPhysicalDefenseBuffIsAppliedOnce(t *testing.T) {
	now := time.Now()
	w := &World{
		data: data.StdBundle{Items: map[string]data.StdItem{
			"defence-armor": {ID: "defence-armor", StdMode: 10, Stats: data.StdItemStats{AcMin: 10 | (10 << 8)}},
		}},
		rand: rand.New(&seqSource{vals: []int64{3}}),
	}
	target := storage.Character{
		Class:          "warrior",
		Level:          1,
		DefenceUpUntil: now.Add(time.Minute).UnixNano(),
		EquippedItems:  map[int]storage.UserItem{SlotDress: {ItemID: "defence-armor"}},
	}
	if got := w.characterPhysicalDamageAfterDefenseAtLocked(&target, 20, now); got != 10 {
		t.Fatalf("physical damage with defence buff = %d, want 10 after one application", got)
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

func TestPrimaryMonsterAttackAddsUndeadBonusAfterDefense(t *testing.T) {
	w, caster := prepareHitDamageTestWorld(t, nil)
	w.data.Items["undead-ring"] = data.StdItem{ID: "undead-ring", StdMode: 19, Undead: 7}
	caster.EquippedItems = map[int]storage.UserItem{
		SlotArmRingL: {ItemID: "undead-ring"},
		SlotWeapon:   {ItemID: "木剑", Dura: 1, DuraMax: 4000},
	}
	w.mu.Lock()
	w.rand = rand.New(zeroSource{})
	var mon *Monster
	for _, candidate := range w.monsters {
		mon = candidate
		break
	}
	mon.Defense = 1
	mon.Undead = 1
	mon.Speed = 1
	stats := w.combatStatsLocked(caster)
	base := Base(caster.Class, caster.Level)
	want := base.DC + stats.DC - mon.Defense + stats.Undead
	w.mu.Unlock()

	result, err := w.HitWithIdent(caster, caster.X, caster.Y, 2, mir176.CMHit)
	if err != nil {
		t.Fatalf("HitWithIdent() error = %v", err)
	}
	if result.Damage != want {
		t.Fatalf("primary monster damage = %d, want defense-adjusted undead damage %d", result.Damage, want)
	}
}

func TestCharacterPoisonDamageResetsNaturalSpellTick(t *testing.T) {
	w := &World{}
	now := time.Unix(10, 0).Add(time.Nanosecond)
	ch := storage.Character{ID: "target", HP: 100, MaxHP: 100, HealthTick: 700, SpellTick: 700, PoisonHealthLevel: 1, PoisonHealthStartAt: now.Add(-time.Second).UnixNano(), PoisonHealthUntil: now.Add(time.Second).UnixNano(), PoisonHealthTickAt: now.Add(-poisonHealthTickInterval).Add(-time.Nanosecond).UnixNano()}
	updated, changed := w.applyCharacterPoisonTickLocked(ch, now)
	if !changed || updated.HP >= ch.HP || updated.HealthTick != 0 || updated.SpellTick != 0 {
		t.Fatalf("poison result = changed:%v character:%+v, want damage and zero natural recovery ticks", changed, updated)
	}
}

func TestCharacterStrikeRecordsHitterWhenMagicShieldAbsorbsDamage(t *testing.T) {
	w := &World{
		data: data.StdBundle{Items: map[string]data.StdItem{
			"magic-shield": {ID: "magic-shield", Shape: 118, DuraMax: 1},
		}},
		gameplay: config.DefaultGameplay(),
		rand:     rand.New(rand.NewSource(1)),
	}
	caster := storage.Character{ID: "caster", MapID: "map", Level: 20, HP: 100, MaxHP: 100, FreePKArea: true}
	target := storage.Character{
		ID: "target", MapID: "map", Level: 20, HP: 100, MaxHP: 100, MP: 100, FreePKArea: true,
		EquippedItems: map[int]storage.UserItem{SlotNecklace: {ItemID: "magic-shield", Dura: 1}},
	}

	updated, hit, err := w.attackCharacterWithDamageModeAtLocked(caster, target, 10, false, false, time.Unix(10, 0))
	if err != nil {
		t.Fatalf("attackCharacterWithDamageModeAtLocked() error = %v", err)
	}
	if hit.Damage != 10 || updated.HP != target.HP || updated.LastHitterID != caster.ID || !hit.AttackerNameColorChanged {
		t.Fatalf("shielded strike = damage:%d hp:%d hitter:%q name_color:%t, want raw hit, unchanged HP, hitter and PK event", hit.Damage, updated.HP, updated.LastHitterID, hit.AttackerNameColorChanged)
	}
}

func TestCharacterStrikeRecordsHitterWithoutPKFlagForRedTarget(t *testing.T) {
	w := &World{
		gameplay: config.DefaultGameplay(),
		rand:     rand.New(rand.NewSource(1)),
	}
	caster := storage.Character{ID: "caster", MapID: "map", Level: 20, HP: 100, MaxHP: 100, FreePKArea: true}
	target := storage.Character{ID: "target", MapID: "map", Level: 20, HP: 100, MaxHP: 100, PKPoint: 200, FreePKArea: true}

	updated, hit, err := w.attackCharacterWithDamageModeAtLocked(caster, target, 10, false, false, time.Unix(10, 0))
	if err != nil {
		t.Fatalf("attackCharacterWithDamageModeAtLocked() error = %v", err)
	}
	if updated.LastHitterID != caster.ID || hit.AttackerNameColorChanged {
		t.Fatalf("red-target strike = hitter:%q name_color:%t, want hitter and no PK event", updated.LastHitterID, hit.AttackerNameColorChanged)
	}
}

func TestCharacterStrikeRecordsHitterWithoutPKFlagInFightZone(t *testing.T) {
	w := &World{
		data:     data.StdBundle{Maps: map[string]data.StdMap{"map": {ID: "map", FightZone: true}}},
		gameplay: config.DefaultGameplay(),
		rand:     rand.New(rand.NewSource(1)),
	}
	caster := storage.Character{ID: "caster", MapID: "map", Level: 20, HP: 100, MaxHP: 100, FreePKArea: true}
	target := storage.Character{ID: "target", MapID: "map", Level: 20, HP: 100, MaxHP: 100, FreePKArea: true}

	updated, hit, err := w.attackCharacterWithDamageModeAtLocked(caster, target, 10, false, false, time.Unix(10, 0))
	if err != nil {
		t.Fatalf("attackCharacterWithDamageModeAtLocked() error = %v", err)
	}
	if updated.LastHitterID != caster.ID || hit.AttackerNameColorChanged {
		t.Fatalf("fight-zone strike = hitter:%q name_color:%t, want hitter and no PK event", updated.LastHitterID, hit.AttackerNameColorChanged)
	}
}

func TestMonsterStrikeRecordsHitterWhenMagicShieldAbsorbsDamage(t *testing.T) {
	w := &World{
		data: data.StdBundle{Items: map[string]data.StdItem{
			"magic-shield": {ID: "magic-shield", Shape: 118, DuraMax: 1},
		}},
		rand: rand.New(rand.NewSource(1)),
	}
	mon := &Monster{ID: "monster", MapID: "map", MinAttack: 10, MaxAttack: 10}
	target := storage.Character{
		ID: "target", MapID: "map", HP: 100, MaxHP: 100, MP: 100, HealthTick: 3, SpellTick: 4,
		EquippedItems: map[int]storage.UserItem{SlotNecklace: {ItemID: "magic-shield", Dura: 1}},
	}

	updated, hit, err := w.monsterAttackCharacterWithDamageAtLocked(mon, target, 10, time.Unix(10, 0))
	if err != nil {
		t.Fatalf("monsterAttackCharacterWithDamageAtLocked() error = %v", err)
	}
	if hit.Damage != 10 || updated.HP != target.HP || updated.LastHitterID != mon.ID || updated.HealthTick != 0 || updated.SpellTick != 0 {
		t.Fatalf("shielded monster strike = damage:%d hp:%d hitter:%q ticks:%d/%d, want raw hit, unchanged HP, hitter and reset ticks", hit.Damage, updated.HP, updated.LastHitterID, updated.HealthTick, updated.SpellTick)
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

func TestHolyCurtainPowerTriplesSpiritRange(t *testing.T) {
	w := &World{
		data: data.StdBundle{Items: map[string]data.StdItem{
			"spirit-range": {ID: "spirit-range", Stats: data.StdItemStats{ScMin: 2, ScMax: 2}},
		}},
		rand: rand.New(rand.NewSource(1)),
	}
	ch := storage.Character{EquippedItems: map[int]storage.UserItem{SlotWeapon: {ItemID: "spirit-range"}}}
	skill := data.StdSkill{TrainLevel1: 1, DefPower: 4, DefMaxPower: 4}
	state := storage.SkillState{Level: 0}
	base := w.spellPower13Locked(40, skill, state)
	power, ok := w.holyCurtainPowerLocked(ch, skill, state)
	if !ok || power != base+6 {
		t.Fatalf("holyCurtainPowerLocked() = %d,%t, want %d,true", power, ok, base+6)
	}
}

func TestFireWallDurationHalvesRolledMagicRange(t *testing.T) {
	w := &World{
		data: data.StdBundle{Items: map[string]data.StdItem{
			"magic-ring": {ID: "magic-ring", Stats: data.StdItemStats{McMin: 3, McMax: 5}},
		}},
		rand: rand.New(rand.NewSource(1)),
	}
	skill := data.StdSkill{TrainLevel1: 1, DefPower: 4, DefMaxPower: 4}
	state := storage.SkillState{Level: 0}
	ch := storage.Character{EquippedItems: map[int]storage.UserItem{
		SlotWeapon: {ItemID: "magic-ring"},
	}}
	if got := w.fireWallDurationLocked(ch, skill, state); got < 8*time.Second || got >= 10*time.Second {
		t.Fatalf("fireWallDurationLocked() = %s, want 8-9 seconds", got)
	}
}

func TestGroupDefenceDurationUsesReferenceSpiritPowerFormula(t *testing.T) {
	w := &World{rand: rand.New(rand.NewSource(1))}
	skill := data.StdSkill{TrainLevel1: 1, DefPower: 4, DefMaxPower: 7}
	state := storage.SkillState{Level: 0}
	ch := storage.Character{}
	if got := w.groupDefenceDurationLocked(ch, skill, state); got < 34*time.Second || got >= 38*time.Second {
		t.Fatalf("groupDefenceDurationLocked() = %s, want 34-37 seconds from GetPower13 plus GetAttackPower", got)
	}
}

func TestGroupDefenceDurationUsesSpiritPowerRange(t *testing.T) {
	w := &World{
		data: data.StdBundle{Items: map[string]data.StdItem{
			"spirit-ring": {ID: "spirit-ring", Stats: data.StdItemStats{ScMin: 2 | (4 << 8)}},
		}},
		rand: rand.New(rand.NewSource(1)),
	}
	skill := data.StdSkill{TrainLevel1: 1, DefPower: 4, DefMaxPower: 4}
	state := storage.SkillState{Level: 0}
	ch := storage.Character{Class: "taoist", Level: 35, EquippedItems: map[int]storage.UserItem{
		SlotArmRingL: {ItemID: "spirit-ring"},
	}}
	got := w.groupDefenceDurationLocked(ch, skill, state)
	if got < 54*time.Second || got >= 58*time.Second {
		t.Fatalf("groupDefenceDurationLocked() = %s, want 54-57 seconds including SC range", got)
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

func TestSpellDamageUsesWeaponLuckWithFixedMagicRange(t *testing.T) {
	w := &World{
		data: data.StdBundle{Items: map[string]data.StdItem{
			"lucky-staff": {ID: "lucky-staff", StdMode: 5, Stats: data.StdItemStats{McMin: 2, McMax: 2}},
		}},
		rand: rand.New(zeroSource{}),
	}
	ch := storage.Character{EquippedItems: map[int]storage.UserItem{
		SlotWeapon: {ItemID: "lucky-staff", Desc: [14]byte{3: 10}},
	}}
	skill := data.StdSkill{Power: 1, MaxPower: 1, TrainLevel1: 0}
	if got := w.spellDamageLocked(ch, skill, storage.SkillState{Level: 0}); got != 4 {
		t.Fatalf("spellDamageLocked() = %d, want 4 from fixed MC plus lucky GetAttackPower", got)
	}
}

func TestSpellHealUsesWeaponLuckWithFixedSpiritRange(t *testing.T) {
	w := &World{
		data: data.StdBundle{Items: map[string]data.StdItem{
			"lucky-talisman": {ID: "lucky-talisman", StdMode: 6, Stats: data.StdItemStats{ScMin: 2, ScMax: 2}},
		}},
		rand: rand.New(zeroSource{}),
	}
	ch := storage.Character{EquippedItems: map[int]storage.UserItem{
		SlotWeapon: {ItemID: "lucky-talisman", Desc: [14]byte{3: 10}},
	}}
	skill := data.StdSkill{Power: 1, MaxPower: 1, TrainLevel1: 0}
	if got := w.spellHealAmountLocked(ch, skill, storage.SkillState{Level: 0}); got != 6 {
		t.Fatalf("spellHealAmountLocked() = %d, want 6 from fixed SC plus lucky GetAttackPower", got)
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

func TestCharacterAttackPowerUsesWeaponLuck(t *testing.T) {
	w := &World{
		rand: rand.New(&seqSource{vals: []int64{0}}),
		data: data.StdBundle{Items: map[string]data.StdItem{
			"lucky-sword": {ID: "lucky-sword", StdMode: 5, Stats: data.StdItemStats{DcMin: 2, DcMax: 5}},
		}},
	}
	ch := storage.Character{Level: 1, EquippedItems: map[int]storage.UserItem{
		SlotWeapon: {ItemID: "lucky-sword", Desc: [14]byte{3: 10}},
	}}
	if got := w.characterAttackPowerLocked(ch, mir176.CMHit); got != 6 {
		t.Fatalf("characterAttackPowerLocked() = %d, want lucky maximum 6", got)
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

func TestImmediateMonsterMagicDamageUsesPoisonArmorMultiplier(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	mon := &Monster{ID: "magic-target", HP: 100, MaxHP: 100, Alive: true, PoisonArmorLevel: poisonDamageArmorRate, PoisonArmorUntil: time.Now().Add(time.Minute)}
	w.monsters[mon.ID] = mon

	hit, err := w.attackMonsterWithImmediateMagicDamageLocked(caster, mon, 10)
	if err != nil {
		t.Fatalf("attackMonsterWithImmediateMagicDamageLocked() error = %v", err)
	}
	if hit.Damage != 12 {
		t.Fatalf("immediate magic damage = %d, want 12 with poison armor multiplier", hit.Damage)
	}
}

func TestImmediateMonsterMagicDamageDefersDeathSettlement(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	caster.Experience = 10
	mon := &Monster{ID: "magic-death-target", MapID: caster.MapID, X: caster.X + 1, Y: caster.Y, HP: 5, MaxHP: 100, Alive: true, Experience: 5}
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
	if len(tick.SpellExperience) != 1 || tick.SpellExperience[0].Experience != mon.Experience || tick.SpellExperience[0].CurrentExp != 15 {
		t.Fatalf("Tick() experience = %+v, want %d", tick.SpellExperience, mon.Experience)
	}
	if len(tick.Characters) != 1 || tick.Characters[0].ID != caster.ID || tick.Characters[0].Experience != 15 {
		t.Fatalf("Tick() characters = %+v, want cumulative experience 15", tick.Characters)
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

func TestDurableAntiMagicEquipmentContributesToMagicResistance(t *testing.T) {
	w := &World{
		data: data.StdBundle{Items: map[string]data.StdItem{
			"anti-durable": {ID: "anti-durable", MgAvoid: 9, DuraMax: 5000},
		}},
		rand: rand.New(&seqSource{vals: []int64{0}}),
	}
	target := storage.Character{EquippedItems: map[int]storage.UserItem{
		SlotWeapon: {ItemID: "anti-durable", Dura: 1},
	}}
	if w.characterMagicHitAllowedLocked(target) {
		t.Fatal("durable anti-magic equipment accepted a zero roll")
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

func TestCharactersInRadiusDeduplicatesRepeatedCharacterSnapshots(t *testing.T) {
	w := &World{}
	players := []storage.Character{
		{ID: "target", MapID: "map", X: 10, Y: 10, HP: 1},
		{ID: "target", MapID: "map", X: 10, Y: 10, HP: 1},
	}
	got := w.charactersInRadiusLocked(players, "map", 10, 10, 1)
	if len(got) != 1 || got[0].ID != "target" {
		t.Fatalf("charactersInRadiusLocked() = %+v, want one target snapshot", got)
	}
}

func TestCharactersInRadiusSkipsDeadAndGhostSnapshots(t *testing.T) {
	w := &World{}
	players := []storage.Character{
		{ID: "dead", MapID: "map", X: 10, Y: 10},
		{ID: "ghost", MapID: "map", X: 10, Y: 10, HP: 1, Ghost: true},
		{ID: "alive", MapID: "map", X: 10, Y: 10, HP: 1},
	}
	got := w.charactersInRadiusLocked(players, "map", 10, 10, 1)
	if len(got) != 1 || got[0].ID != "alive" {
		t.Fatalf("charactersInRadiusLocked() = %+v, want alive target only", got)
	}
}

func TestCharactersInRadiusRejectsNegativeRadius(t *testing.T) {
	w := &World{}
	players := []storage.Character{{ID: "target", MapID: "map", X: 10, Y: 10, HP: 1}}
	if got := w.charactersInRadiusLocked(players, "map", 10, 10, -1); len(got) != 0 {
		t.Fatalf("negative-radius characters = %+v, want none", got)
	}
}

func TestSpellAreaTargetsUseCoordinateOrderBeforeObjectOrder(t *testing.T) {
	w := &World{monsters: map[string]*Monster{
		"far":  {ID: "far", MapID: "map", X: 11, Y: 10, ObjectOrder: 1, Alive: true},
		"near": {ID: "near", MapID: "map", X: 9, Y: 10, ObjectOrder: 2, Alive: true},
	}}

	targets := w.spellAreaTargetsLocked(nil, "map", 10, 10, 2)
	got := make([]string, 0, len(targets))
	for _, target := range targets {
		_, _, id := target.CharacterOrMonster()
		got = append(got, id)
	}
	want := []string{"near", "far"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("target order = %v, want %v", got, want)
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

func TestSpellAreaTargetsUseSharedObjectOrderAcrossActorTypes(t *testing.T) {
	w := &World{monsters: map[string]*Monster{
		"monster": {ID: "monster", MapID: "map", X: 10, Y: 10, ObjectOrder: 2, Alive: true},
	}}
	players := []storage.Character{{ID: "character", MapID: "map", X: 10, Y: 10, ObjectOrder: 1, HP: 1}}

	targets := w.spellAreaTargetsLocked(players, "map", 10, 10, 0)
	got := make([]string, 0, len(targets))
	for _, target := range targets {
		_, _, id := target.CharacterOrMonster()
		got = append(got, id)
	}
	want := []string{"character", "monster"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("target order = %v, want %v", got, want)
	}
}

func TestSpellAreaTargetsPlaceUnknownObjectOrderAfterKnownObject(t *testing.T) {
	w := &World{monsters: map[string]*Monster{
		"known": {ID: "known", ObjectOrder: 2, MapID: "map", X: 10, Y: 10, Alive: true},
	}}
	players := []storage.Character{
		{ID: "unknown", MapID: "map", X: 10, Y: 10, HP: 1},
	}
	targets := w.spellAreaTargetsLocked(players, "map", 10, 10, 0)
	if len(targets) != 2 {
		t.Fatalf("spellAreaTargetsLocked() returned %d targets, want 2", len(targets))
	}
	if _, _, id := targets[0].CharacterOrMonster(); id != "known" {
		t.Fatalf("first target = %q, want known object order first", id)
	}
}

func TestSpellAreaTargetsSortUnknownMonstersByStableID(t *testing.T) {
	w := &World{monsters: map[string]*Monster{
		"monster-z": {ID: "monster-z", MapID: "map", X: 10, Y: 10, Alive: true},
		"monster-a": {ID: "monster-a", MapID: "map", X: 10, Y: 10, Alive: true},
	}}
	targets := w.spellAreaTargetsLocked(nil, "map", 10, 10, 0)
	if len(targets) != 2 {
		t.Fatalf("spellAreaTargetsLocked() returned %d targets, want 2", len(targets))
	}
	if _, _, id := targets[0].CharacterOrMonster(); id != "monster-a" {
		t.Fatalf("first unknown-order monster = %q, want monster-a", id)
	}
}

func TestSpellAreaTargetsSkipMonsterWithoutID(t *testing.T) {
	w := &World{monsters: map[string]*Monster{
		"": {MapID: "map", X: 10, Y: 10, HP: 1, Alive: true},
	}}
	if got := w.spellAreaTargetsLocked(nil, "map", 10, 10, 0); len(got) != 0 {
		t.Fatalf("spellAreaTargetsLocked() = %+v, want no anonymous monster", got)
	}
}

func TestSpellAreaTargetsSkipGhostCharacters(t *testing.T) {
	w := &World{monsters: map[string]*Monster{}}
	players := []storage.Character{{ID: "ghost", MapID: "map", X: 10, Y: 10, HP: 1, Ghost: true}}
	if targets := w.spellAreaTargetsLocked(players, "map", 10, 10, 1); len(targets) != 0 {
		t.Fatalf("spell area targets = %+v, want no ghost target", targets)
	}
}

func TestSpellAreaTargetsRejectNegativeRadius(t *testing.T) {
	w := &World{monsters: map[string]*Monster{
		"target": {ID: "target", MapID: "map", X: 10, Y: 10, Alive: true},
	}}
	players := []storage.Character{{ID: "character", MapID: "map", X: 10, Y: 10, HP: 1}}
	if targets := w.spellAreaTargetsLocked(players, "map", 10, 10, -1); len(targets) != 0 {
		t.Fatalf("negative-radius spell area targets = %+v, want none", targets)
	}
}

func TestSpellAreaTargetsCanRetainDeadObjectsForDefenceArea(t *testing.T) {
	w := &World{monsters: map[string]*Monster{
		"dead":  {ID: "dead", MapID: "map", X: 10, Y: 10, Alive: false},
		"ghost": {ID: "ghost", MapID: "map", X: 10, Y: 10, Alive: true, Ghost: true},
	}}
	players := []storage.Character{
		{ID: "dead-character", MapID: "map", X: 10, Y: 10, HP: 0},
		{ID: "ghost-character", MapID: "map", X: 10, Y: 10, HP: 1, Ghost: true},
	}
	targets := w.spellAreaTargetsWithLifeFilterLocked(players, "map", 10, 10, 1, false)
	if len(targets) != 2 {
		t.Fatalf("defence-area targets = %+v, want dead character and monster", targets)
	}
}

func TestDefenceAreaTargetsRejectNegativeRadius(t *testing.T) {
	w := &World{monsters: map[string]*Monster{
		"dead": {ID: "dead", MapID: "map", X: 10, Y: 10, Alive: false},
	}}
	players := []storage.Character{{ID: "dead-character", MapID: "map", X: 10, Y: 10, HP: 0}}
	if got := w.spellAreaTargetsWithLifeFilterLocked(players, "map", 10, 10, -1, false); len(got) != 0 {
		t.Fatalf("negative-radius defence-area targets = %+v, want none", got)
	}
}

func TestSpellAreaTargetsSkipDeadCharactersAndMonsters(t *testing.T) {
	w := &World{monsters: map[string]*Monster{
		"dead-monster": {ID: "dead-monster", MapID: "map", X: 10, Y: 10, Alive: false},
	}}
	players := []storage.Character{{ID: "dead-character", MapID: "map", X: 10, Y: 10, HP: 0}}
	if got := w.spellAreaTargetsLocked(players, "map", 10, 10, 1); len(got) != 0 {
		t.Fatalf("alive-filtered spell area targets = %+v, want none", got)
	}
}

func TestSpellLineNextPositionUsesBoundsOnly(t *testing.T) {
	mapData := data.StdMap{Width: 10, Height: 8, Blocked: []data.StdPoint{{X: 5, Y: 2}}}
	if x, y, ok := spellLineNextPosition(mapData, 2, 2, 2, 3); !ok || x != 5 || y != 2 {
		t.Fatalf("spellLineNextPosition() = (%d,%d,%t), want (5,2,true) on blocked tile", x, y, ok)
	}
	if x, y, ok := spellLineNextPosition(mapData, 8, 2, 2, 3); ok || x != 8 || y != 2 {
		t.Fatalf("spellLineNextPosition() = (%d,%d,%t), want unchanged position and false at boundary", x, y, ok)
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

func TestHealingGaugeTargetsIncludeFullHealthFriendlySummon(t *testing.T) {
	w := &World{monsters: map[string]*Monster{
		"mon": {ID: "mon", MasterID: "caster", MapID: "map", X: 10, Y: 10, HP: 10, MaxHP: 10, Alive: true},
	}}
	caster := storage.Character{ID: "caster", MapID: "map", X: 10, Y: 10, HP: 10, MaxHP: 10}
	targets := w.healingGaugeTargetsLocked(caster, nil, 10, 10)
	if len(targets) != 2 {
		t.Fatalf("healingGaugeTargetsLocked() returned %d targets, want caster and full-health summon", len(targets))
	}
	foundMonster, foundCaster := false, false
	for _, target := range targets {
		if target.Monster != nil && target.Monster.ID == "mon" {
			foundMonster = true
		}
		if target.Character != nil && target.Character.ID == "caster" {
			foundCaster = true
		}
	}
	if !foundMonster || !foundCaster {
		t.Fatalf("healing gauge targets = %+v, want full-health summon and caster", targets)
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

func TestSpellFriendQualificationUsesReferenceAttackModes(t *testing.T) {
	w := &World{}
	caster := storage.Character{ID: "caster", AttackMode: 3, GuildID: "guild"}
	target := storage.Character{ID: "target", GuildID: "guild"}
	if !w.isProperFriendLocked(caster, target) {
		t.Fatal("guild mode should treat same-guild target as a friend")
	}
	target.GuildID = "other"
	caster.GuildWarArea = true
	target.GuildWarArea = true
	caster.GuildAllianceID = "alliance"
	target.GuildAllianceID = "alliance"
	if !w.isProperFriendLocked(caster, target) {
		t.Fatal("guild war mode should treat an allied target as a friend")
	}
	caster = storage.Character{ID: "caster", AttackMode: 4, PKPoint: 0}
	target = storage.Character{ID: "target", PKPoint: 200}
	if !w.isProperFriendLocked(caster, target) {
		t.Fatal("PK mode should treat the opposing PK tier as a friend")
	}
}

func TestPoisonRetargetQualificationUsesTargetPerspective(t *testing.T) {
	w := &World{
		data:     data.StdBundle{Maps: map[string]data.StdMap{"map": {ID: "map"}}},
		gameplay: config.DefaultGameplay(),
	}
	caster := storage.Character{ID: "caster", MapID: "map", HP: 1, AttackMode: 2, GroupOwnerID: "group"}
	target := storage.Character{ID: "target", MapID: "map", HP: 1, AttackMode: 0, GroupOwnerID: "group"}
	if w.isProperCharacterTargetLocked(caster, target) {
		t.Fatal("caster perspective should reject a group member")
	}
	if !w.isProperCharacterTargetLocked(target, caster) {
		t.Fatal("poison retarget should use target perspective")
	}
}

func TestDelayedMonsterPoisonRetargetUsesMonsterPerspective(t *testing.T) {
	w := &World{
		data:     data.StdBundle{Maps: map[string]data.StdMap{"map": {ID: "map"}}},
		gameplay: config.DefaultGameplay(),
	}
	caster := storage.Character{ID: "caster", MapID: "map", HP: 1, TargetID: "other"}
	master := storage.Character{ID: "master", MapID: "map", HP: 1}
	owned := &Monster{ID: "owned", MapID: "map", Alive: true, MasterID: master.ID}
	if w.isProperDelayedMonsterPoisonTargetLocked(caster, []storage.Character{master}, owned) {
		t.Fatal("monster perspective should reject an unrelated summoned monster")
	}
	caster.TargetID = ""
	master.TargetID = caster.ID
	if !w.isProperDelayedMonsterPoisonTargetLocked(caster, []storage.Character{master}, owned) {
		t.Fatal("monster perspective should accept the master's target relationship")
	}
}

func TestDelayedMonsterParalysisSkipsLastHitterForInvalidTarget(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	owner := storage.Character{ID: "owner", MapID: caster.MapID, HP: 1, GroupOwnerID: "group"}
	caster.GroupOwnerID = "group"
	caster.AttackMode = 2
	mon := &Monster{ID: "owned", MapID: caster.MapID, X: caster.X + 1, Y: caster.Y, Alive: true, MasterID: owner.ID}
	now := time.Now()
	w.mu.Lock()
	w.monsters = map[string]*Monster{mon.ID: mon}
	w.pendingSpells = []pendingSpell{{
		DueAt: now.Add(-time.Second), CasterID: caster.ID, TargetMonsterID: mon.ID,
		ParalysisDuration: time.Second,
	}}
	players := map[string]storage.Character{caster.ID: caster, owner.ID: owner}
	result := TickResult{}
	updated := map[string]storage.Character{}
	err := w.applyPendingSpellTicksLocked(&result, players, updated, now)
	w.mu.Unlock()
	if err != nil {
		t.Fatalf("applyPendingSpellTicksLocked() error = %v", err)
	}
	if mon.LastHitterID != "" {
		t.Fatalf("monster LastHitterID = %q, want empty for invalid target", mon.LastHitterID)
	}
	if !mon.ParalyzedUntil.After(now) {
		t.Fatalf("monster ParalyzedUntil = %s, want active paralysis", mon.ParalyzedUntil)
	}
}

func TestPendingSpellPhasesMatchReferenceObjectStages(t *testing.T) {
	now := time.Now()
	w := &World{
		data:     data.StdBundle{Maps: map[string]data.StdMap{"map": {ID: "map"}}},
		gameplay: config.DefaultGameplay(),
		monsters: map[string]*Monster{"monster": {ID: "monster", MapID: "map", Alive: true, HP: 100, MaxHP: 100}},
	}
	players := map[string]storage.Character{"character": {ID: "character", MapID: "map", HP: 100, MaxHP: 100}}
	w.pendingSpells = []pendingSpell{
		{DueAt: now.Add(-time.Second), TargetCharacterID: "character", TransparentDuration: time.Second},
		{DueAt: now.Add(-time.Second), TargetMonsterID: "monster", TransparentDuration: time.Second},
	}
	result := TickResult{}
	updated := map[string]storage.Character{}

	if err := w.applyPendingSpellTicksForPhaseLocked(&result, players, updated, now, pendingSpellPhaseCharacter); err != nil {
		t.Fatalf("character phase = %v", err)
	}
	if players["character"].TransparentUntil == 0 {
		t.Fatal("character phase did not consume character target")
	}
	if !w.monsters["monster"].TransparentUntil.IsZero() {
		t.Fatal("character phase consumed monster target")
	}
	if len(w.pendingSpells) != 1 || pendingSpellPhaseForTarget(w.pendingSpells[0]) != pendingSpellPhaseMonster {
		t.Fatalf("pending after character phase = %+v, want only monster target", w.pendingSpells)
	}

	if err := w.applyPendingSpellTicksForPhaseLocked(&result, players, updated, now, pendingSpellPhaseMonster); err != nil {
		t.Fatalf("monster phase = %v", err)
	}
	if w.monsters["monster"].TransparentUntil.IsZero() {
		t.Fatal("monster phase did not consume monster target")
	}
	if len(w.pendingSpells) != 0 {
		t.Fatalf("pending after monster phase = %d, want 0", len(w.pendingSpells))
	}
}

func TestPendingShowHealthOpensDeadMonsterLikeReference(t *testing.T) {
	now := time.Unix(100, 0)
	w := &World{
		gameplay: config.DefaultGameplay(),
		monsters: map[string]*Monster{"monster": {ID: "monster", MapID: "map", Alive: false, HP: 0, MaxHP: 100}},
		pendingSpells: []pendingSpell{{
			DueAt: now.Add(-time.Second), TargetMonsterID: "monster",
			ShowHealthDuration: time.Minute, ShowHealthStartedAt: now.Add(-2 * time.Second),
		}},
	}
	result := TickResult{}
	if err := w.applyPendingSpellTicksLocked(&result, map[string]storage.Character{}, map[string]storage.Character{}, now); err != nil {
		t.Fatalf("applyPendingSpellTicksLocked() error = %v", err)
	}
	if len(result.ShowHPOpenedMonsters) != 1 || result.ShowHPOpenedMonsters[0].ID != "monster" {
		t.Fatalf("ShowHPOpenedMonsters = %+v, want dead monster", result.ShowHPOpenedMonsters)
	}
	if w.monsters["monster"].ShowHPUntil != now.Add(58*time.Second).UnixNano() {
		t.Fatalf("ShowHPUntil = %d, want %d", w.monsters["monster"].ShowHPUntil, now.Add(58*time.Second).UnixNano())
	}
}

func TestPendingCharacterSpellsConsumeInCharacterOrder(t *testing.T) {
	now := time.Now()
	w := &World{gameplay: config.DefaultGameplay()}
	first := storage.Character{ID: "first", MapID: "map", HP: 100, MaxHP: 100}
	second := storage.Character{ID: "second", MapID: "map", HP: 100, MaxHP: 100}
	players := map[string]storage.Character{first.ID: first, second.ID: second}
	w.pendingSpells = []pendingSpell{
		{DueAt: now.Add(-time.Second), TargetCharacterID: first.ID, TransparentDuration: time.Second},
		{DueAt: now.Add(-time.Second), TargetCharacterID: second.ID, TransparentDuration: time.Second},
	}
	result := TickResult{}
	updated := map[string]storage.Character{}
	if err := w.applyPendingSpellTicksForCharacterLocked(&result, players, updated, now, first.ID); err != nil {
		t.Fatalf("first character phase = %v", err)
	}
	if players[first.ID].TransparentUntil == 0 || players[second.ID].TransparentUntil != 0 {
		t.Fatalf("after first character phase = first:%d second:%d, want first active and second unchanged", players[first.ID].TransparentUntil, players[second.ID].TransparentUntil)
	}
	if len(w.pendingSpells) != 1 || w.pendingSpells[0].TargetCharacterID != second.ID {
		t.Fatalf("pending after first character phase = %+v, want second target only", w.pendingSpells)
	}
	if err := w.applyPendingSpellTicksForCharacterLocked(&result, players, updated, now, second.ID); err != nil {
		t.Fatalf("second character phase = %v", err)
	}
	if players[second.ID].TransparentUntil == 0 || len(w.pendingSpells) != 0 {
		t.Fatalf("after second character phase = second:%d pending:%d, want active and empty", players[second.ID].TransparentUntil, len(w.pendingSpells))
	}
}

func TestPendingCharacterEventsPreserveCrossSourceEnqueueOrder(t *testing.T) {
	now := time.Now()
	w := &World{
		gameplay: config.DefaultGameplay(),
		rand:     rand.New(rand.NewSource(1)),
		monsters: map[string]*Monster{
			"monster": {ID: "monster", Alive: true, MapID: "map", MinAttack: 30, MaxAttack: 30},
		},
	}
	character := storage.Character{ID: "character", MapID: "map", HP: 50, MaxHP: 100}
	caster := storage.Character{ID: "caster", MapID: "map", HP: 100, MaxHP: 100}
	players := map[string]storage.Character{character.ID: character, caster.ID: caster}
	attacks := []pendingMonsterAttack{{EnqueueOrder: 1, DueAt: now.Add(-time.Second), MonsterID: "monster", TargetIDs: []string{character.ID}, Damage: 30}}
	w.pendingSpells = []pendingSpell{{EnqueueOrder: 2, DueAt: now.Add(-time.Second), CasterID: caster.ID, TargetCharacterID: character.ID, CharacterDamage: true, TargetX: character.X, TargetY: character.Y, Damage: 10}}
	result := TickResult{}
	updated := map[string]storage.Character{}
	remaining, err := w.applyPendingCharacterEventsLocked(&result, players, updated, now, character.ID, attacks)
	if err != nil {
		t.Fatalf("apply pending events = %v", err)
	}
	if len(remaining) != 0 || len(w.pendingSpells) != 0 {
		t.Fatalf("pending events remain: attacks=%d spells=%d", len(remaining), len(w.pendingSpells))
	}
	if got := players[character.ID].HP; got != 10 {
		t.Fatalf("character HP = %d, want attack then magic result 10", got)
	}
	if len(result.CharacterHits) != 2 || result.CharacterHits[0].AttackerID != "monster" || result.CharacterHits[1].AttackerID != caster.ID {
		t.Fatalf("character hit order = %+v, want monster then caster", result.CharacterHits)
	}
	orderedHits := make([]CharacterHit, 0, len(result.OrderedSpellEvents))
	for _, event := range result.OrderedSpellEvents {
		if event.Kind == OrderedSpellEventCharacterHit {
			orderedHits = append(orderedHits, event.CharacterHit)
		}
	}
	if len(orderedHits) != 2 || orderedHits[0].AttackerID != "monster" || orderedHits[1].AttackerID != caster.ID {
		t.Fatalf("ordered character hit events = %+v, want monster then caster", orderedHits)
	}
}

func TestPendingMonsterEventsPreserveCrossSourceEnqueueOrder(t *testing.T) {
	now := time.Now()
	w := &World{
		gameplay: config.DefaultGameplay(),
		rand:     rand.New(rand.NewSource(1)),
		monsters: map[string]*Monster{
			"attacker": {ID: "attacker", Alive: true, MapID: "map", MinAttack: 10, MaxAttack: 10},
			"target":   {ID: "target", Alive: true, MasterID: "owner", MapID: "map", HP: 100, MaxHP: 100},
		},
	}
	caster := storage.Character{ID: "caster", MapID: "map", HP: 100, MaxHP: 100}
	players := map[string]storage.Character{caster.ID: caster}
	attacks := []pendingMonsterAttack{{EnqueueOrder: 1, DueAt: now.Add(-time.Second), MonsterID: "attacker", MonsterTargets: []pendingMonsterTarget{{ID: "target", Damage: 10}}}}
	w.pendingSpells = []pendingSpell{{EnqueueOrder: 2, DueAt: now.Add(-time.Second), CasterID: caster.ID, TargetMonsterID: "target", SingleMagicStrike: true, Damage: 20}}
	result := TickResult{}
	updated := map[string]storage.Character{}
	remaining, err := w.applyPendingMonsterEventsLocked(&result, players, updated, attacks, now)
	if err != nil {
		t.Fatalf("apply pending monster events = %v", err)
	}
	if len(remaining) != 0 || len(w.pendingSpells) != 0 {
		t.Fatalf("pending monster events remain: attacks=%d spells=%d", len(remaining), len(w.pendingSpells))
	}
	if len(result.MonsterHits) != 2 || result.MonsterHits[0].Damage != 10 || result.MonsterHits[1].Damage != 20 {
		t.Fatalf("monster hit order = %+v, want damage 10 then 20", result.MonsterHits)
	}
	if len(result.OrderedMonsterEvents) != 2 || result.OrderedMonsterEvents[0].Hit.Damage != 10 || result.OrderedMonsterEvents[1].Hit.Damage != 20 {
		t.Fatalf("ordered monster hit events = %+v, want the same attack-then-spell order", result.OrderedMonsterEvents)
	}
}

func TestPendingMonsterSpellsPreserveSameTargetEnqueueOrder(t *testing.T) {
	now := time.Now()
	w := &World{
		gameplay: config.DefaultGameplay(),
		rand:     rand.New(rand.NewSource(1)),
		monsters: map[string]*Monster{
			"target": {ID: "target", Alive: true, MapID: "map", HP: 100, MaxHP: 100},
		},
	}
	caster := storage.Character{ID: "caster", MapID: "map", HP: 100, MaxHP: 100}
	players := map[string]storage.Character{caster.ID: caster}
	w.pendingSpells = []pendingSpell{
		{EnqueueOrder: 1, DueAt: now.Add(-time.Second), CasterID: caster.ID, TargetMonsterID: "target", SingleMagicStrike: true, Damage: 20},
		{EnqueueOrder: 2, DueAt: now.Add(-time.Second), CasterID: caster.ID, TargetMonsterID: "target", SingleMagicStrike: true, Damage: 10},
	}
	result := TickResult{}
	updated := map[string]storage.Character{}
	if err := w.applyPendingSpellTicksLocked(&result, players, updated, now); err != nil {
		t.Fatalf("apply pending monster spells = %v", err)
	}
	if len(result.MonsterHits) != 2 || result.MonsterHits[0].Damage != 20 || result.MonsterHits[1].Damage != 10 {
		t.Fatalf("same-target hit order = %+v, want queue order 20 then 10", result.MonsterHits)
	}
}

func TestPendingMonsterSpellsRestoreSameTargetEnqueueOrderAfterReordering(t *testing.T) {
	now := time.Now()
	w := &World{
		gameplay: config.DefaultGameplay(),
		rand:     rand.New(rand.NewSource(1)),
		monsters: map[string]*Monster{
			"target": {ID: "target", Alive: true, MapID: "map", HP: 100, MaxHP: 100},
		},
	}
	caster := storage.Character{ID: "caster", MapID: "map", HP: 100, MaxHP: 100}
	players := map[string]storage.Character{caster.ID: caster}
	w.pendingSpells = []pendingSpell{
		{EnqueueOrder: 2, DueAt: now.Add(-time.Second), CasterID: caster.ID, TargetMonsterID: "target", SingleMagicStrike: true, Damage: 10},
		{EnqueueOrder: 1, DueAt: now.Add(-time.Second), CasterID: caster.ID, TargetMonsterID: "target", SingleMagicStrike: true, Damage: 20},
	}
	result := TickResult{}
	updated := map[string]storage.Character{}
	if err := w.applyPendingSpellTicksLocked(&result, players, updated, now); err != nil {
		t.Fatalf("apply reordered pending monster spells = %v", err)
	}
	if len(result.MonsterHits) != 2 || result.MonsterHits[0].Damage != 20 || result.MonsterHits[1].Damage != 10 {
		t.Fatalf("reordered same-target hit order = %+v, want enqueue order 20 then 10", result.MonsterHits)
	}
}

func TestPendingMonsterSpellsKeepStableOrderWithoutEnqueueOrder(t *testing.T) {
	now := time.Now()
	w := &World{
		gameplay: config.DefaultGameplay(),
		rand:     rand.New(rand.NewSource(1)),
		monsters: map[string]*Monster{
			"target": {ID: "target", Alive: true, MapID: "map", HP: 100, MaxHP: 100},
		},
	}
	caster := storage.Character{ID: "caster", MapID: "map", HP: 100, MaxHP: 100}
	players := map[string]storage.Character{caster.ID: caster}
	w.pendingSpells = []pendingSpell{
		{DueAt: now.Add(-time.Second), CasterID: caster.ID, TargetMonsterID: "target", SingleMagicStrike: true, Damage: 20},
		{DueAt: now.Add(-time.Second), CasterID: caster.ID, TargetMonsterID: "target", SingleMagicStrike: true, Damage: 10},
	}
	result := TickResult{}
	updated := map[string]storage.Character{}
	if err := w.applyPendingSpellTicksLocked(&result, players, updated, now); err != nil {
		t.Fatalf("apply stable pending monster spells = %v", err)
	}
	if len(result.MonsterHits) != 2 || result.MonsterHits[0].Damage != 20 || result.MonsterHits[1].Damage != 10 {
		t.Fatalf("stable same-target hit order = %+v, want queue order 20 then 10", result.MonsterHits)
	}
}

func TestWorldTickSplitsMultiTargetMonsterDelayByTargetOrder(t *testing.T) {
	now := time.Unix(10, 0)
	w := &World{
		gameplay:      config.DefaultGameplay(),
		rand:          rand.New(rand.NewSource(1)),
		visibleActors: map[string][]string{},
		monsters: map[string]*Monster{
			"attacker": {ID: "attacker", Alive: true, MapID: "map"},
			"target-a": {ID: "target-a", Alive: true, MasterID: "owner", MapID: "map", HP: 100, MaxHP: 100},
			"target-b": {ID: "target-b", Alive: true, MasterID: "owner", MapID: "map", HP: 100, MaxHP: 100},
		},
		pendingMonsterAttacks: []pendingMonsterAttack{
			{DueAt: now, MonsterID: "attacker", MonsterTargets: []pendingMonsterTarget{{ID: "target-a", Damage: 20}}},
			{DueAt: now, MonsterID: "attacker", MonsterTargets: []pendingMonsterTarget{{ID: "target-b", Damage: 20}}},
		},
	}
	result := TickResult{}
	_, err := w.applyPendingMonsterEventsLocked(&result, nil, nil, w.pendingMonsterAttacks, now)
	if err != nil {
		t.Fatalf("applyPendingMonsterEventsLocked() error = %v", err)
	}
	if len(result.MonsterHits) != 2 || result.MonsterHits[0].MonsterID != "target-a" || result.MonsterHits[1].MonsterID != "target-b" {
		t.Fatalf("multi-target monster hits = %+v, want target-a then target-b", result.MonsterHits)
	}
}

func TestPendingMonsterEventsFollowTargetProcessOrderBeforeEnqueueOrder(t *testing.T) {
	now := time.Now()
	w := &World{
		gameplay: config.DefaultGameplay(),
		rand:     rand.New(rand.NewSource(1)),
		monsters: map[string]*Monster{
			"attacker": {ID: "attacker", Alive: true, MapID: "map", MinAttack: 10, MaxAttack: 10},
			"target-a": {ID: "target-a", Alive: true, MasterID: "owner", MapID: "map", HP: 100, MaxHP: 100, Spawn: data.StdSpawn{ReferenceOrder: 2}, SpawnMemberOrder: 1},
			"target-b": {ID: "target-b", Alive: true, MasterID: "owner", MapID: "map", HP: 100, MaxHP: 100, Spawn: data.StdSpawn{ReferenceOrder: 1}, SpawnMemberOrder: 1},
		},
	}
	caster := storage.Character{ID: "caster", MapID: "map", HP: 100, MaxHP: 100}
	players := map[string]storage.Character{caster.ID: caster}
	attacks := []pendingMonsterAttack{
		{EnqueueOrder: 1, DueAt: now.Add(-time.Second), MonsterID: "attacker", MonsterTargets: []pendingMonsterTarget{{ID: "target-a", Damage: 10}}},
		{EnqueueOrder: 2, DueAt: now.Add(-time.Second), MonsterID: "attacker", MonsterTargets: []pendingMonsterTarget{{ID: "target-b", Damage: 10}}},
	}
	w.pendingSpells = []pendingSpell{
		{EnqueueOrder: 3, DueAt: now.Add(-time.Second), CasterID: caster.ID, TargetMonsterID: "target-a", SingleMagicStrike: true, Damage: 20},
		{EnqueueOrder: 4, DueAt: now.Add(-time.Second), CasterID: caster.ID, TargetMonsterID: "target-b", SingleMagicStrike: true, Damage: 20},
	}
	result := TickResult{}
	updated := map[string]storage.Character{}
	if _, err := w.applyPendingMonsterEventsLocked(&result, players, updated, attacks, now); err != nil {
		t.Fatalf("apply pending monster events = %v", err)
	}
	if len(result.MonsterHits) != 4 {
		t.Fatalf("monster hit count = %d, want 4", len(result.MonsterHits))
	}
	if len(result.OrderedMonsterEvents) != 4 {
		t.Fatalf("ordered monster event count = %d, want 4", len(result.OrderedMonsterEvents))
	}
	for i, hit := range result.MonsterHits {
		want := "target-b"
		if i >= 2 {
			want = "target-a"
		}
		if hit.MonsterID != want {
			t.Fatalf("monster hit %d target = %q, want %q; hits=%+v", i, hit.MonsterID, want, result.MonsterHits)
		}
	}
}

func TestPendingMonsterEventsKeepLegacyAttackBeforeSpell(t *testing.T) {
	now := time.Now()
	w := &World{
		gameplay: config.DefaultGameplay(),
		rand:     rand.New(rand.NewSource(1)),
		monsters: map[string]*Monster{
			"attacker": {ID: "attacker", Alive: true, MapID: "map", MinAttack: 10, MaxAttack: 10},
			"target":   {ID: "target", Alive: true, MasterID: "owner", MapID: "map", HP: 100, MaxHP: 100},
		},
	}
	caster := storage.Character{ID: "caster", MapID: "map", HP: 100, MaxHP: 100}
	players := map[string]storage.Character{caster.ID: caster}
	attacks := []pendingMonsterAttack{{DueAt: now.Add(-time.Second), MonsterID: "attacker", MonsterTargets: []pendingMonsterTarget{{ID: "target", Damage: 10}}}}
	w.pendingSpells = []pendingSpell{{DueAt: now.Add(-time.Second), CasterID: caster.ID, TargetMonsterID: "target", SingleMagicStrike: true, Damage: 20}}
	result := TickResult{}
	updated := map[string]storage.Character{}
	if _, err := w.applyPendingMonsterEventsLocked(&result, players, updated, attacks, now); err != nil {
		t.Fatalf("apply pending monster events = %v", err)
	}
	if len(result.MonsterHits) != 2 || result.MonsterHits[0].Damage != 10 || result.MonsterHits[1].Damage != 20 {
		t.Fatalf("monster hit order = %+v, want damage 10 then 20", result.MonsterHits)
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
	target.StoneMode = false
	target.Ghost = true
	if w.isProperCharacterTargetLocked(caster, target) {
		t.Fatal("ghost target should be rejected")
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
	mon.StoneMode = false
	mon.Ghost = true
	if w.isProperMonsterTargetLocked(caster, nil, mon) {
		t.Fatal("ghost monster should be rejected")
	}
}

func TestSpellTargetNearSkipsDeadAndGhostCharacters(t *testing.T) {
	ghost := storage.Character{ID: "ghost", MapID: "map", X: 10, Y: 10, HP: 100, Ghost: true}
	dead := storage.Character{ID: "dead", MapID: "map", X: 10, Y: 10, HP: 0}
	w := &World{}
	if _, ok := w.spellTargetNearLocked([]storage.Character{ghost}, "map", 10, 10, CharacterActorID(ghost)); ok {
		t.Fatal("ghost character should not resolve as a spell target")
	}
	if _, ok := w.spellTargetNearLocked([]storage.Character{dead}, "map", 10, 10, CharacterActorID(dead)); ok {
		t.Fatal("dead character should not resolve as a spell target")
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

func TestMonsterSpellTargetUsesSummonCoordinatesForSafeZone(t *testing.T) {
	gameplay := config.DefaultGameplay()
	gameplay.Combat.RedHomeMap = "map"
	gameplay.Combat.RedHomeX = 2
	gameplay.Combat.RedHomeY = 2
	gameplay.Combat.SafeZoneSize = 1
	w := &World{
		data:     data.StdBundle{Maps: map[string]data.StdMap{"map": {ID: "map"}}},
		gameplay: gameplay,
	}
	caster := storage.Character{ID: "caster", MapID: "map", X: 10, Y: 10, Level: 20, HP: 1}
	master := storage.Character{ID: "master", MapID: "map", X: 10, Y: 10, Level: 20, HP: 1}
	summon := &Monster{ID: "summon", MapID: "map", X: 2, Y: 2, Alive: true, MasterID: master.ID}
	if w.isProperMonsterTargetLocked(caster, []storage.Character{master}, summon) {
		t.Fatal("summon inside red-home safe zone should be rejected by its own coordinates")
	}
	summon.X, summon.Y = 10, 10
	if !w.isProperMonsterTargetLocked(caster, []storage.Character{master}, summon) {
		t.Fatal("summon outside red-home safe zone should remain a valid target")
	}
}

func TestSafeZoneIncludesMapStartPoints(t *testing.T) {
	gameplay := config.DefaultGameplay()
	w := &World{
		data: data.StdBundle{Maps: map[string]data.StdMap{
			"map": {ID: "map", StartPoints: []data.StdStartPoint{{X: 5, Y: 5}}},
		}},
		gameplay: gameplay,
	}
	if !w.isSafeZoneLocked(storage.Character{MapID: "map", X: 6, Y: 5}) {
		t.Fatal("character inside a map start-point safe zone should be protected")
	}
	if w.isSafeZoneLocked(storage.Character{MapID: "map", X: 20, Y: 20}) {
		t.Fatal("character outside map start-point safe zones should not be protected")
	}
}

func TestMonsterStruckByCharacterRetargetsAdjacentAttacker(t *testing.T) {
	w := &World{gameplay: config.DefaultGameplay(), rand: rand.New(rand.NewSource(1))}
	caster := storage.Character{ID: "caster", MapID: "map", X: 11, Y: 10, HP: 1}
	oldTarget := storage.Character{ID: "old-target", MapID: "map", X: 10, Y: 11, HP: 1}
	mon := &Monster{ID: "monster", MapID: "map", X: 10, Y: 10, Alive: true, TargetCharacterID: oldTarget.ID, PerHealth: 5, PerSpell: 5}
	now := time.Unix(10, 0)

	w.monsterStruckByCharacterLocked(mon, caster, []storage.Character{oldTarget}, now)

	if mon.TargetCharacterID != caster.ID {
		t.Fatalf("TargetCharacterID = %q, want %q after adjacent Struck", mon.TargetCharacterID, caster.ID)
	}
	if mon.LastHitterID != caster.ID || !mon.LastHitterAt.Equal(now) {
		t.Fatalf("hitter state = %q/%v, want %q/%v after Struck", mon.LastHitterID, mon.LastHitterAt, caster.ID, now)
	}
	if !mon.LastAttackAt.After(now) {
		t.Fatalf("LastAttackAt = %v, want delayed after Struck", mon.LastAttackAt)
	}
	if mon.PerHealth != 5 || mon.PerSpell != 5 {
		t.Fatalf("recovery counters = %d/%d, want unchanged 5/5 after Struck", mon.PerHealth, mon.PerSpell)
	}
}

func TestAnimalDamageDefersTargetSelectionUntilStruck(t *testing.T) {
	for seed := int64(0); seed < 32; seed++ {
		w := &World{gameplay: config.DefaultGameplay(), rand: rand.New(rand.NewSource(seed))}
		mon := &Monster{ID: "animal", Race: 53, MapID: "map", X: 10, Y: 10, Alive: true, HP: 20, MaxHP: 20, TargetCharacterID: "old-target", LastAttackAt: time.Unix(1, 0)}
		caster := storage.Character{ID: "caster", MapID: "map", X: 20, Y: 20, HP: 20, MaxHP: 20}
		if _, err := w.attackMonsterWithBaseDamageLocked(caster, mon, 1); err != nil {
			t.Fatalf("attackMonsterWithBaseDamageLocked() error = %v", err)
		}
		if mon.TargetCharacterID == "old-target" {
			return
		}
	}
	t.Fatal("animal damage always retargeted a non-adjacent attacker")
}

func TestMonsterStruckPreservesExpiredHitClockWhenAddingDelay(t *testing.T) {
	now := time.Unix(10, 0)
	old := now.Add(-time.Second)
	monster := &Monster{ID: "expired-hit-clock", Level: 1, LastAttackAt: old}
	w := &World{}
	w.monsterStruckLocked(monster, now)

	want := old.Add(146 * time.Millisecond)
	if !monster.LastAttackAt.Equal(want) {
		t.Fatalf("LastAttackAt = %v, want %v after reference delay accumulation", monster.LastAttackAt, want)
	}
}

func TestMonsterStruckByCharacterRetargetsAdjacentAttackerFromMonsterTarget(t *testing.T) {
	w := &World{gameplay: config.DefaultGameplay(), rand: rand.New(rand.NewSource(1))}
	caster := storage.Character{ID: "caster", MapID: "map", X: 11, Y: 10, HP: 1}
	other := &Monster{ID: "other", MapID: "map", X: 10, Y: 11, Alive: true}
	mon := &Monster{ID: "monster", MapID: "map", X: 10, Y: 10, Alive: true, TargetMonsterID: other.ID, HolySeizeUntil: time.Unix(20, 0)}
	w.monsters = map[string]*Monster{mon.ID: mon, other.ID: other}
	now := time.Unix(10, 0)

	w.monsterStruckByCharacterLocked(mon, caster, nil, now)

	if mon.TargetCharacterID != caster.ID || mon.TargetMonsterID != "" {
		t.Fatalf("targets = character:%q monster:%q, want character:%q and no monster target", mon.TargetCharacterID, mon.TargetMonsterID, caster.ID)
	}
	if !mon.HolySeizeUntil.IsZero() {
		t.Fatalf("HolySeizeUntil = %v, want cleared after Struck", mon.HolySeizeUntil)
	}
}

func TestMonsterStruckByCharacterMarksAttackerForForeignSummon(t *testing.T) {
	w := &World{gameplay: config.DefaultGameplay(), rand: rand.New(rand.NewSource(1))}
	caster := storage.Character{ID: "caster", MapID: "map", X: 11, Y: 10, HP: 1}
	owner := storage.Character{ID: "owner", MapID: "map", HP: 1}
	mon := &Monster{ID: "summon", MapID: "map", X: 10, Y: 10, Alive: true, MasterID: owner.ID}
	now := time.Unix(10, 0)

	updated := w.monsterStruckByCharacterLocked(mon, caster, []storage.Character{owner}, now)
	if !updated.PKFlag || updated.PKFlagUntil != now.Add(60*time.Second).UnixNano() {
		t.Fatalf("attacker PK state = flag:%t until:%d, want active 60-second flag", updated.PKFlag, updated.PKFlagUntil)
	}
}

func TestMonsterStruckByCharacterUsesSummonTargetRelation(t *testing.T) {
	w := &World{gameplay: config.DefaultGameplay(), rand: rand.New(rand.NewSource(1))}
	master := storage.Character{ID: "owner", MapID: "map", TargetID: "caster", HP: 1}
	caster := storage.Character{ID: "caster", MapID: "map", X: 11, Y: 10, HP: 1}
	mon := &Monster{ID: "summon", MapID: "map", X: 10, Y: 10, Alive: true, MasterID: master.ID}

	w.monsterStruckByCharacterLocked(mon, caster, []storage.Character{master}, time.Unix(10, 0))

	if mon.TargetCharacterID != caster.ID {
		t.Fatalf("TargetCharacterID = %q, want %q from master target relation", mon.TargetCharacterID, caster.ID)
	}
}

func TestMonsterStruckByCharacterDoesNotTargetSummonOwnerOrRelaxedMaster(t *testing.T) {
	for _, tc := range []struct {
		name       string
		casterID   string
		relax      bool
		masterLast string
	}{
		{name: "owner", casterID: "owner"},
		{name: "relaxed", casterID: "caster", relax: true, masterLast: "caster"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := &World{gameplay: config.DefaultGameplay(), rand: rand.New(rand.NewSource(1))}
			master := storage.Character{ID: "owner", MapID: "map", SlaveRelax: tc.relax, LastHitterID: tc.masterLast, HP: 1}
			caster := storage.Character{ID: tc.casterID, MapID: "map", X: 11, Y: 10, HP: 1}
			mon := &Monster{ID: "summon", MapID: "map", X: 10, Y: 10, Alive: true, MasterID: master.ID}

			w.monsterStruckByCharacterLocked(mon, caster, []storage.Character{master}, time.Unix(10, 0))

			if mon.TargetCharacterID != "" {
				t.Fatalf("TargetCharacterID = %q, want no target", mon.TargetCharacterID)
			}
		})
	}
}

func TestMonsterStruckByCharacterResolvesSummonOwnerFromStore(t *testing.T) {
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("storage.Open() error = %v", err)
	}
	owner, err := store.InsertCharacter(storage.Character{ID: "owner", MapID: "map", TargetID: "caster", HP: 1})
	if err != nil {
		t.Fatalf("store.InsertCharacter() error = %v", err)
	}
	w := &World{gameplay: config.DefaultGameplay(), rand: rand.New(rand.NewSource(1)), store: store}
	caster := storage.Character{ID: "caster", MapID: "map", X: 11, Y: 10, HP: 1}
	mon := &Monster{ID: "summon", MapID: "map", X: 10, Y: 10, Alive: true, MasterID: owner.ID}

	w.monsterStruckByCharacterLocked(mon, caster, nil, time.Unix(10, 0))

	if mon.TargetCharacterID != caster.ID {
		t.Fatalf("TargetCharacterID = %q, want %q after resolving stored owner", mon.TargetCharacterID, caster.ID)
	}
}

func TestDirectMonsterDamageReturnsStruckAttackerState(t *testing.T) {
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("storage.Open() error = %v", err)
	}
	owner, err := store.InsertCharacter(storage.Character{ID: "owner", MapID: "map", HP: 1})
	if err != nil {
		t.Fatalf("store.InsertCharacter() error = %v", err)
	}
	w := &World{gameplay: config.DefaultGameplay(), rand: rand.New(rand.NewSource(1)), store: store}
	caster := storage.Character{ID: "caster", MapID: "map", X: 11, Y: 10, HP: 1}
	mon := &Monster{ID: "summon", Race: 50, MapID: "map", X: 10, Y: 10, Alive: true, HP: 10, MaxHP: 10, MasterID: owner.ID}
	w.monsters = map[string]*Monster{mon.ID: mon}

	_, err = w.attackMonsterDirectDamageLocked(caster, mon, 1)
	if err != nil {
		t.Fatalf("attackMonsterDirectDamageLocked() error = %v", err)
	}
	if len(w.pendingMonsterAttacks) != 1 {
		t.Fatalf("pending struck events = %d, want 1", len(w.pendingMonsterAttacks))
	}
	if err := w.applyDeferredMonsterAttackLocked(&TickResult{}, w.pendingMonsterAttacks[0], time.Now().Add(200*time.Millisecond)); err != nil {
		t.Fatalf("applyDeferredMonsterAttackLocked() error = %v", err)
	}
	if !w.pendingMonsterAttacks[0].StruckCharacter.PKFlag || w.pendingMonsterAttacks[0].StruckCharacter.PKFlagUntil <= time.Now().UnixNano() {
		t.Fatalf("deferred attacker PK state = flag:%t until:%d, want active flag", w.pendingMonsterAttacks[0].StruckCharacter.PKFlag, w.pendingMonsterAttacks[0].StruckCharacter.PKFlagUntil)
	}
}

func TestMonsterStruckByCharacterHonorsSummonPKRestrictions(t *testing.T) {
	for _, tc := range []struct {
		name      string
		ownerPK   int
		casterPK  int
		fightZone bool
	}{
		{name: "red owner", ownerPK: 200},
		{name: "red attacker", casterPK: 200},
		{name: "fight zone", fightZone: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := &World{gameplay: config.DefaultGameplay(), rand: rand.New(rand.NewSource(1)), data: data.StdBundle{Maps: map[string]data.StdMap{"map": {ID: "map", FightZone: tc.fightZone}}}}
			owner := storage.Character{ID: "owner", MapID: "map", HP: 1, PKPoint: tc.ownerPK}
			caster := storage.Character{ID: "caster", MapID: "map", X: 11, Y: 10, HP: 1, PKPoint: tc.casterPK}
			mon := &Monster{ID: "summon", MapID: "map", X: 10, Y: 10, Alive: true, MasterID: owner.ID}

			updated := w.monsterStruckByCharacterLocked(mon, caster, []storage.Character{owner}, time.Unix(10, 0))
			if updated.PKFlag || updated.PKFlagUntil != 0 {
				t.Fatalf("attacker PK state = flag:%t until:%d, want unchanged", updated.PKFlag, updated.PKFlagUntil)
			}
		})
	}
}

func TestMonsterStruckByCharacterDoesNotMarkWhenMasterAlreadyPKFlagged(t *testing.T) {
	now := time.Unix(10, 0)
	w := &World{gameplay: config.DefaultGameplay(), rand: rand.New(rand.NewSource(1))}
	owner := storage.Character{ID: "owner", MapID: "map", HP: 1, PKFlag: true}
	caster := storage.Character{ID: "caster", MapID: "map", HP: 1, PKFlag: true, PKFlagUntil: now.Add(time.Second).UnixNano()}
	mon := &Monster{ID: "summon", MapID: "map", Alive: true, MasterID: owner.ID}

	updated := w.monsterStruckByCharacterLocked(mon, caster, []storage.Character{owner}, now)

	if updated.PKFlagUntil != caster.PKFlagUntil {
		t.Fatalf("existing PK expiry = %d, want unchanged %d", updated.PKFlagUntil, caster.PKFlagUntil)
	}
}

func TestMonsterAttackAppliesReferenceMonsterStruckChain(t *testing.T) {
	now := time.Unix(10, 0)
	w := &World{rand: rand.New(rand.NewSource(1)), actionNow: now}
	attacker := &Monster{ID: "attacker", MasterID: "other-owner", MapID: "map", X: 10, Y: 10, Alive: true, MinAttack: 10, MaxAttack: 10}
	target := &Monster{ID: "target", MasterID: "owner", MapID: "map", X: 11, Y: 10, Alive: true, HP: 100, MaxHP: 100, Defense: 0, HolySeizeUntil: now.Add(time.Minute)}
	w.monsters = map[string]*Monster{attacker.ID: attacker, target.ID: target}

	if _, err := w.archerAttackMonsterLocked(attacker, target, 10); err != nil {
		t.Fatalf("archerAttackMonsterLocked() error = %v", err)
	}
	if target.TargetMonsterID != attacker.ID || target.TargetCharacterID != "" {
		t.Fatalf("target fields = monster:%q character:%q, want attacker monster target", target.TargetMonsterID, target.TargetCharacterID)
	}
	if target.LastHitterID != attacker.ID || !target.LastHitterAt.Equal(now) || target.ExpHitterID != attacker.ID || !target.ExpHitterAt.Equal(now) {
		t.Fatalf("hitter fields = last:%q/%v exp:%q/%v, want attacker/%v for both", target.LastHitterID, target.LastHitterAt, target.ExpHitterID, target.ExpHitterAt, now)
	}
	if !target.HolySeizeUntil.IsZero() || !target.LastAttackAt.After(now) {
		t.Fatalf("struck state = holy:%v last_attack:%v, want cleared holy and delayed attack", target.HolySeizeUntil, target.LastAttackAt)
	}
}

func TestNormalMonsterTargetAttackUsesReferenceHitPath(t *testing.T) {
	now := time.Unix(10, 0)
	w := &World{rand: rand.New(zeroSource{}), actionNow: now}
	attacker := &Monster{ID: "normal-attacker", Race: 81, MasterID: "attacker-owner", MapID: "map", X: 10, Y: 10, Alive: true, MinAttack: 10, MaxAttack: 10, Hit: 100, AttackIntervalMS: 1, LastAttackAt: now.Add(-time.Second), TargetMonsterID: "normal-target"}
	target := &Monster{ID: "normal-target", MasterID: "target-owner", MapID: "map", X: 11, Y: 10, Alive: true, HP: 100, MaxHP: 100, Speed: 10}
	w.monsters = map[string]*Monster{attacker.ID: attacker, target.ID: target}

	actions, _, _, err := w.tickMonsterMonsterTargetLocked(attacker, nil, now)
	if err != nil {
		t.Fatalf("tickMonsterMonsterTargetLocked() error = %v", err)
	}
	if len(actions) != 1 || actions[0].Kind != MonsterActionHit {
		t.Fatalf("actions = %+v, attacker = %+v, target = %+v, want one hit action", actions, attacker, target)
	}
	if len(w.monsterBehaviorHits) != 1 || w.monsterBehaviorHits[0].Magic || w.monsterBehaviorHits[0].ImpactDelay != 200*time.Millisecond {
		t.Fatalf("monster hits = %+v, want one physical 200ms hit", w.monsterBehaviorHits)
	}
	if target.HP >= target.MaxHP {
		t.Fatalf("target HP = %d, want damage", target.HP)
	}
}

func TestReferenceMonsterVsMonsterDamageAppliesStruckChain(t *testing.T) {
	now := time.Unix(10, 0)
	w := &World{rand: rand.New(rand.NewSource(1)), actionNow: now}
	attacker := &Monster{ID: "attacker", MasterID: "other-owner", MapID: "map", X: 10, Y: 10, Alive: true}
	target := &Monster{ID: "target", MasterID: "owner", MapID: "map", X: 11, Y: 10, Alive: true, HP: 100, MaxHP: 100, HolySeizeUntil: now.Add(time.Minute)}
	w.monsters = map[string]*Monster{attacker.ID: attacker, target.ID: target}

	if _, err := w.applyReferenceMonsterVsMonsterDamageAtLocked(attacker, target, 10, now); err != nil {
		t.Fatalf("applyReferenceMonsterVsMonsterDamageAtLocked() error = %v", err)
	}
	if target.TargetMonsterID != "" || target.TargetCharacterID != "" || target.LastHitterID != "" {
		t.Fatalf("target state = target:%q/%q hitter:%q, want deferred struck state", target.TargetMonsterID, target.TargetCharacterID, target.LastHitterID)
	}
	if target.HP != 90 {
		t.Fatalf("target HP = %d, want immediate damage", target.HP)
	}
	w.enqueueReferenceMonsterStruckLocked(attacker, target, now, 200*time.Millisecond)
	if err := w.applyDeferredMonsterAttackLocked(&TickResult{}, w.pendingMonsterAttacks[0], now.Add(200*time.Millisecond)); err != nil {
		t.Fatalf("deferred struck error = %v", err)
	}
	if target.LastHitterID != attacker.ID || !target.LastAttackAt.After(now) || !target.HolySeizeUntil.IsZero() {
		t.Fatalf("deferred struck state = hitter:%q last_attack:%v holy:%v, want applied struck chain", target.LastHitterID, target.LastAttackAt, target.HolySeizeUntil)
	}
}

func TestMonsterStruckByMonsterDoesNotTargetWildAttacker(t *testing.T) {
	w := &World{rand: rand.New(rand.NewSource(1))}
	attacker := &Monster{ID: "wild", MapID: "map", X: 10, Y: 10, Alive: true}
	target := &Monster{ID: "summon", MasterID: "owner", MapID: "map", X: 11, Y: 10, Alive: true, HolySeizeUntil: time.Unix(20, 0)}
	w.monsters = map[string]*Monster{attacker.ID: attacker, target.ID: target}

	w.monsterStruckByMonsterLocked(target, attacker, time.Unix(10, 0))

	if target.TargetMonsterID != "" {
		t.Fatalf("TargetMonsterID = %q, want no target for wild attacker", target.TargetMonsterID)
	}
	if !target.HolySeizeUntil.IsZero() {
		t.Fatalf("HolySeizeUntil = %v, want cleared after Struck", target.HolySeizeUntil)
	}
}

func TestMonsterLastHitterUsesEventTimestamp(t *testing.T) {
	w := &World{}
	mon := &Monster{ID: "monster"}
	now := time.Unix(10, 123)

	w.setMonsterLastHitterAtLocked(mon, "attacker", now)

	if !mon.LastHitterAt.Equal(now) || !mon.ExpHitterAt.Equal(now) {
		t.Fatalf("hitter timestamps = (%v, %v), want (%v, %v)", mon.LastHitterAt, mon.ExpHitterAt, now, now)
	}
}

func TestMonsterDamageResetsReferenceRecoveryCounters(t *testing.T) {
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("storage.Open() error = %v", err)
	}
	w := &World{rand: rand.New(rand.NewSource(1)), store: store}
	attacker := storage.Character{ID: "attacker", MapID: "map", HP: 10}

	physical := &Monster{ID: "physical", MapID: "map", Alive: true, HP: 100, MaxHP: 100, PerHealth: 5, PerSpell: 5}
	if _, err := w.attackMonsterWithDamageModeLocked(attacker, physical, 10, false); err != nil {
		t.Fatalf("physical damage error = %v", err)
	}
	if physical.PerHealth != 4 || physical.PerSpell != 4 {
		t.Fatalf("physical recovery counters = %d/%d, want 4/4", physical.PerHealth, physical.PerSpell)
	}

	magic := &Monster{ID: "magic", MapID: "map", Alive: true, HP: 100, MaxHP: 100, PerHealth: 5, PerSpell: 5}
	if _, err := w.applyMonsterMagicDamageLocked(attacker, magic, 10, false); err != nil {
		t.Fatalf("magic damage error = %v", err)
	}
	if magic.PerHealth != 4 || magic.PerSpell != 4 {
		t.Fatalf("magic recovery counters = %d/%d, want 4/4", magic.PerHealth, magic.PerSpell)
	}

	poison := &Monster{ID: "poison", MapID: "map", Alive: true, HP: 100, MaxHP: 100, PerHealth: 5, PerSpell: 5}
	if _, err := w.attackMonsterWithPoisonDamageLocked(attacker, poison, 10); err != nil {
		t.Fatalf("poison damage error = %v", err)
	}
	if poison.PerHealth != 5 || poison.PerSpell != 5 {
		t.Fatalf("poison recovery counters = %d/%d, want unchanged 5/5", poison.PerHealth, poison.PerSpell)
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
	players := []storage.Character{
		{ID: "observer", MapID: "map", X: 3, Y: 4, HP: 1, ObserverMode: true, ObjectOrder: 0},
		{ID: "character", MapID: "map", X: 3, Y: 4, HP: 1, ObjectOrder: 2},
	}
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

func TestCharacterAtExactPointUsesMapObjectOrder(t *testing.T) {
	players := []storage.Character{
		{ID: "observer", MapID: "map", X: 3, Y: 4, HP: 1, ObserverMode: true, ObjectOrder: 1},
		{ID: "later", MapID: "map", X: 3, Y: 4, HP: 1, ObjectOrder: 20},
		{ID: "earlier", MapID: "map", X: 3, Y: 4, HP: 1, ObjectOrder: 10},
	}
	w := &World{}
	selected, ok := w.characterAtExactPointLocked(players, "map", 3, 4)
	if !ok || selected.ID != "earlier" {
		t.Fatalf("World.characterAtExactPointLocked() = %+v,%t, want earlier,true", selected, ok)
	}
}

func TestCharactersInRadiusUsesMapScanAndObjectOrder(t *testing.T) {
	w := &World{}
	players := []storage.Character{
		{ID: "later-cell", MapID: "map", X: 4, Y: 3, HP: 1, ObjectOrder: 20},
		{ID: "same-cell-later", MapID: "map", X: 3, Y: 3, HP: 1, ObjectOrder: 20},
		{ID: "same-cell-first", MapID: "map", X: 3, Y: 3, HP: 1, ObjectOrder: 10},
	}
	got := w.charactersInRadiusLocked(players, "map", 3, 3, 1)
	if len(got) != 3 {
		t.Fatalf("charactersInRadiusLocked() count = %d, want 3", len(got))
	}
	if got[0].ID != "same-cell-first" || got[1].ID != "same-cell-later" || got[2].ID != "later-cell" {
		t.Fatalf("charactersInRadiusLocked() order = %v, want map scan then ObjectOrder", []string{got[0].ID, got[1].ID, got[2].ID})
	}
}

func TestMovingObjectAtPointKeepsUnorderedInputBeforeOrderedObject(t *testing.T) {
	w := &World{monsters: map[string]*Monster{
		"monster": {ID: "monster", MapID: "map", X: 3, Y: 4, Alive: true, ObjectOrder: 1},
	}}
	players := []storage.Character{{ID: "character", MapID: "map", X: 3, Y: 4, HP: 1}}
	selected := w.movingObjectAtPointLocked(players, "map", 3, 4)
	if selected.Character == nil || selected.Character.ID != "character" {
		t.Fatalf("selected object = %+v, want input character", selected)
	}
}

func TestMovingObjectAtPointSkipsGhostCharacter(t *testing.T) {
	w := &World{monsters: map[string]*Monster{}}
	players := []storage.Character{{ID: "ghost", MapID: "map", X: 3, Y: 4, HP: 1, Ghost: true}}
	selected := w.movingObjectAtPointLocked(players, "map", 3, 4)
	if selected.Character != nil || selected.Monster != nil {
		t.Fatalf("selected ghost object = %+v, want none", selected)
	}
}

func TestMovingObjectAtPointSkipsMonsterWithoutID(t *testing.T) {
	w := &World{monsters: map[string]*Monster{
		"": {MapID: "map", X: 3, Y: 4, HP: 1, Alive: true},
	}}
	selected := w.movingObjectAtPointLocked(nil, "map", 3, 4)
	if selected.Character != nil || selected.Monster != nil {
		t.Fatalf("selected anonymous monster = %+v, want none", selected)
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

func TestSpellTeleportUsesCharacterHomeMapForRedCharacter(t *testing.T) {
	w := &World{
		data: data.StdBundle{Maps: map[string]data.StdMap{
			"home":     {ID: "home", Width: 100, Height: 100},
			"red-home": {ID: "red-home", Width: 100, Height: 100},
		}},
		gameplay: func() config.Gameplay {
			gameplay := config.DefaultGameplay()
			gameplay.Combat.RedHomeMap = "red-home"
			return gameplay
		}(),
		rand: rand.New(rand.NewSource(1)),
	}
	ch := storage.Character{ID: "red-caster", MapID: "home", HomeMap: "home", PKPoint: 200}
	next, err := w.homeTeleportRandomCharacterLocked(ch)
	if err != nil {
		t.Fatalf("homeTeleportRandomCharacterLocked() error = %v", err)
	}
	if next.MapID != "home" {
		t.Fatalf("red character random teleport map = %q, want home", next.MapID)
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
