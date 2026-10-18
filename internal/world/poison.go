package world

import (
	"math/rand"
	"time"

	"openmir2/internal/data"
	"openmir2/internal/storage"
	"openmir2/internal/world/core"
)

const (
	poisonHealthTickInterval    = 2500 * time.Millisecond
	poisonDamageArmorRate       = 12
	poisonDamageArmorBaseRate   = 10
	poisonBaseHealthPowerGrey   = 40
	poisonBaseHealthPowerYellow = 30
)

func (w *World) poisonHealthTickInterval() time.Duration {
	interval := w.gameplay.Combat.PoisonTickIntervalMS
	if interval <= 0 {
		interval = int(poisonHealthTickInterval / time.Millisecond)
	}
	return time.Duration(interval) * time.Millisecond
}

func poisonChanceOK(rng *rand.Rand, avoid int) bool {
	if avoid < 0 {
		avoid = 0
	}
	return rng.Intn(avoid+7) <= 6
}

func poisonDamageMultiplier(active bool) float64 {
	if !active {
		return 1
	}
	return float64(poisonDamageArmorRate) / float64(poisonDamageArmorBaseRate)
}

func characterPoisonArmorActive(ch storage.Character, now time.Time) bool {
	return ch.PoisonArmorLevel != 0 && ch.PoisonArmorUntil > 0 && now.UnixNano() <= ch.PoisonArmorUntil && (ch.PoisonArmorStartAt == 0 || now.UnixNano() >= ch.PoisonArmorStartAt)
}

func characterPoisonHealthActive(ch storage.Character, now time.Time) bool {
	return ch.PoisonHealthUntil > 0 && now.UnixNano() <= ch.PoisonHealthUntil && (ch.PoisonHealthStartAt == 0 || now.UnixNano() >= ch.PoisonHealthStartAt)
}

func monsterPoisonArmorActive(mon *Monster, now time.Time) bool {
	return mon != nil && mon.PoisonArmorLevel != 0 && !mon.PoisonArmorUntil.IsZero() && !now.After(mon.PoisonArmorUntil) && (mon.PoisonArmorStartAt.IsZero() || !now.Before(mon.PoisonArmorStartAt))
}

func monsterPoisonHealthActive(mon *Monster, now time.Time) bool {
	return mon != nil && !mon.PoisonHealthUntil.IsZero() && !now.After(mon.PoisonHealthUntil) && (mon.PoisonHealthStartAt.IsZero() || !now.Before(mon.PoisonHealthStartAt))
}

func (w *World) poisonAvoidanceLocked(ch storage.Character) int {
	avoid := 0
	for _, entry := range ch.EquippedItems {
		item, ok := w.data.Items[entry.ItemID]
		if !ok {
			continue
		}
		item = UpgradeClientItemForDisplay(item, entry, false)
		avoid += item.ToxAvoid
	}
	if avoid < 0 {
		return 0
	}
	return avoid
}

func (w *World) poisonSpellPowerLocked(ch storage.Character, skill data.StdSkill, state storage.SkillState, base int) int {
	combat := w.combatStatsLocked(ch)
	roll := w.spellPower13Locked(base, skill, state)
	low := combat.SC
	high := combat.SCMax
	sc := low
	if high > low {
		sc += w.rand.Intn(high - low + 1)
	}
	roll += sc * 2
	if roll < 1 {
		return 1
	}
	return roll
}

func poisonLevelFromPower(power int) byte {
	if power < 0 {
		power = 0
	}
	if power > 255 {
		power = 255
	}
	return byte(power)
}

func poisonDamageFromPoint(point int) int {
	if point < 0 {
		point = 0
	}
	return point + 1
}

func poisonEffectDuration(power int) time.Duration {
	if power <= 0 {
		return 0
	}
	return time.Duration(power) * time.Second
}

func (w *World) applyCharacterPoisonTickLocked(ch storage.Character, now time.Time) (storage.Character, bool) {
	next := ch
	changed := false
	if ch.HP > 0 && characterPoisonHealthActive(ch, now) {
		interval := w.poisonHealthTickInterval()
		nextTick := now.Add(interval)
		if ch.PoisonHealthTickAt > 0 {
			nextTick = time.Unix(0, ch.PoisonHealthTickAt).Add(interval)
		}
		if now.After(nextTick) {
			damage := poisonDamageFromPoint(ch.PoisonHealthPoint)
			hpDamage := w.applyCharacterMagicShieldLocked(&next, damage)
			next = core.ApplyVitalDelta(next, -hpDamage, 0).Character
			next.SpellTick = 0
			next.HealthTick = 0
			next.PoisonHealthTickAt = now.UnixNano()
			changed = true
		}
	} else if ch.PoisonHealthLevel != 0 || ch.PoisonHealthPoint != 0 || ch.PoisonHealthStartAt != 0 || ch.PoisonHealthUntil != 0 {
		next.PoisonHealthLevel = 0
		next.PoisonHealthPoint = 0
		next.PoisonHealthStartAt = 0
		next.PoisonHealthUntil = 0
		changed = true
	}
	if ch.PoisonHealthTickAt > 0 && now.Sub(time.Unix(0, ch.PoisonHealthTickAt)) > w.poisonHealthTickInterval() {
		next.PoisonHealthTickAt = now.UnixNano()
		changed = true
	}
	if !characterPoisonArmorActive(ch, now) && (ch.PoisonArmorLevel != 0 || ch.PoisonArmorStartAt != 0 || ch.PoisonArmorUntil != 0) {
		next.PoisonArmorLevel = 0
		next.PoisonArmorStartAt = 0
		next.PoisonArmorUntil = 0
		changed = true
	}
	return next, changed
}

func (w *World) applyMonsterPoisonTickLocked(mon *Monster, players map[string]storage.Character, now time.Time) ([]AttackResult, bool, error) {
	if mon == nil || !mon.Alive || mon.HP <= 0 {
		return nil, false, nil
	}
	if !monsterPoisonHealthActive(mon, now) {
		if mon.PoisonHealthLevel != 0 || mon.PoisonHealthPoint != 0 || !mon.PoisonHealthStartAt.IsZero() || !mon.PoisonHealthUntil.IsZero() {
			mon.PoisonHealthLevel = 0
			mon.PoisonHealthPoint = 0
			mon.PoisonHealthStartAt = time.Time{}
			mon.PoisonHealthUntil = time.Time{}
		}
		if !mon.PoisonHealthTickAt.IsZero() && now.Sub(mon.PoisonHealthTickAt) > w.poisonHealthTickInterval() {
			mon.PoisonHealthTickAt = now
		}
		if !monsterPoisonArmorActive(mon, now) && (mon.PoisonArmorLevel != 0 || !mon.PoisonArmorStartAt.IsZero() || !mon.PoisonArmorUntil.IsZero()) {
			mon.PoisonArmorLevel = 0
			mon.PoisonArmorStartAt = time.Time{}
			mon.PoisonArmorUntil = time.Time{}
		}
		return nil, false, nil
	}
	nextTick := mon.PoisonHealthTickAt.Add(w.poisonHealthTickInterval())
	if mon.PoisonHealthTickAt.IsZero() || now.After(nextTick) {
		damage := poisonDamageFromPoint(mon.PoisonHealthPoint)
		source := players[mon.PoisonSourceID]
		if source.ID != "" && !w.characterDeadLocked(source) {
			result, err := w.attackMonsterWithPoisonDamageLocked(source, mon, damage)
			if err != nil {
				return nil, false, err
			}
			if result.Damage > 0 {
				mon.HealthTick = 0
				mon.SpellTick = 0
			}
			mon.PoisonHealthTickAt = now
			return []AttackResult{result}, result.Dead, nil
		}
		w.decayMonsterMeatQualityLocked(mon, 1)
		change := core.ApplyHPDelta(mon.HP, mon.MaxHP, -damage)
		mon.HP = change.HP
		if damage > 0 {
			mon.HealthTick = 0
			mon.SpellTick = 0
		}
		mon.PoisonHealthTickAt = now
		result := AttackResult{
			MonsterID: mon.ID, MonsterMapID: mon.MapID, Damage: damage,
			MonsterHP: mon.HP, MonsterMaxHP: mon.MaxHP, MonsterRaceImg: mon.RaceImg,
			MonsterWeapon: mon.MonsterWeapon, MonsterAppr: mon.Appr,
			MonsterX: mon.X, MonsterY: mon.Y, MonsterDir: mon.Dir,
			Character: storage.Character{},
		}
		if change.Dead {
			mon.PendingDeath = true
			result.DeathDeferred = true
		}
		return []AttackResult{result}, change.Dead, nil
	}
	return nil, false, nil
}

func setCharacterHealthPoisonLocked(ch *storage.Character, level byte, point int, until, startAt time.Time) bool {
	if ch == nil {
		return false
	}
	changed := false
	startNS := startAt.UnixNano()
	untilNS := until.UnixNano()
	if ch.PoisonHealthLevel != level {
		ch.PoisonHealthLevel = level
		changed = true
	}
	if ch.PoisonHealthPoint != point {
		ch.PoisonHealthPoint = point
		changed = true
	}
	if ch.PoisonHealthStartAt != startNS {
		ch.PoisonHealthStartAt = startNS
		changed = true
	}
	if ch.PoisonHealthUntil <= startNS || ch.PoisonHealthUntil-startNS < untilNS-startNS {
		if ch.PoisonHealthUntil != untilNS {
			ch.PoisonHealthUntil = untilNS
			changed = true
		}
	}
	return changed
}

func setCharacterArmorPoisonLocked(ch *storage.Character, point int, until, startAt time.Time) bool {
	changed := false
	if ch.PoisonHealthPoint != point {
		ch.PoisonHealthPoint = point
		changed = true
	}
	if ch.PoisonArmorLevel < poisonDamageArmorRate {
		ch.PoisonArmorLevel = poisonDamageArmorRate
		changed = true
	}
	if startAt.UnixNano() > ch.PoisonArmorStartAt {
		ch.PoisonArmorStartAt = startAt.UnixNano()
		changed = true
	}
	if until.UnixNano() > ch.PoisonArmorUntil {
		ch.PoisonArmorUntil = until.UnixNano()
		changed = true
	}
	return changed
}

func setMonsterHealthPoisonLocked(mon *Monster, level byte, point int, until time.Time, sourceID string, now time.Time) bool {
	if mon == nil {
		return false
	}
	changed := false
	startAt := now
	requestedDuration := until.Sub(startAt)
	if mon.PoisonHealthLevel != level {
		mon.PoisonHealthLevel = level
		changed = true
	}
	if mon.PoisonHealthPoint != point {
		mon.PoisonHealthPoint = point
		changed = true
	}
	if !mon.PoisonHealthStartAt.Equal(startAt) {
		mon.PoisonHealthStartAt = startAt
		changed = true
	}
	if mon.PoisonHealthUntil.IsZero() || mon.PoisonHealthUntil.Before(startAt) || mon.PoisonHealthUntil.Sub(startAt) < requestedDuration {
		if !mon.PoisonHealthUntil.Equal(until) {
			mon.PoisonHealthUntil = until
			changed = true
		}
	}
	if sourceID != "" && mon.PoisonSourceID != sourceID {
		mon.PoisonSourceID = sourceID
		changed = true
	}
	return changed
}

func setMonsterArmorPoisonLocked(mon *Monster, point int, until, startAt time.Time) bool {
	if mon == nil {
		return false
	}
	changed := false
	if mon.PoisonHealthPoint != point {
		mon.PoisonHealthPoint = point
		changed = true
	}
	if mon.PoisonArmorLevel < poisonDamageArmorRate {
		mon.PoisonArmorLevel = poisonDamageArmorRate
		changed = true
	}
	if mon.PoisonArmorStartAt.IsZero() || mon.PoisonArmorStartAt.Before(startAt) {
		mon.PoisonArmorStartAt = startAt
		changed = true
	}
	if until.After(mon.PoisonArmorUntil) {
		mon.PoisonArmorUntil = until
		changed = true
	}
	return changed
}

func (w *World) consumePoisonPowderLocked(ch *storage.Character) (storage.UserItem, bool) {
	for _, slot := range []int{SlotArmRingL, SlotBujuk} {
		item, ok := w.equippedItemLocked(*ch, slot)
		if !ok || item.ItemID == "" {
			continue
		}
		if referenceRound(float64(item.Dura)/100.0) < 1 {
			continue
		}
		std, ok := w.data.Items[item.ItemID]
		if !ok || std.StdMode != 25 || (std.Shape != 1 && std.Shape != 2) {
			continue
		}
		if item.Dura > 100 {
			item.Dura -= 100
		} else {
			item.Dura = 0
		}
		if item.Dura == 0 {
			w.deleteEquippedItemLocked(ch, slot)
		} else {
			w.setEquippedItemLocked(ch, slot, item)
		}
		return item, true
	}
	return storage.UserItem{}, false
}

func (w *World) consumeMagicAmuletLocked(ch *storage.Character, amount uint16) bool {
	units := amount
	duraCost := amount * 100
	for _, slot := range []int{SlotArmRingL, SlotBujuk} {
		item, ok := w.equippedItemLocked(*ch, slot)
		if !ok || item.ItemID == "" || referenceRound(float64(item.Dura)/100.0) < int(units) {
			continue
		}
		std, ok := w.data.Items[item.ItemID]
		if !ok || std.StdMode != 25 || std.Shape != 5 {
			continue
		}
		if item.Dura > duraCost {
			item.Dura -= duraCost
		} else {
			item.Dura = 0
		}
		if item.Dura == 0 {
			w.deleteEquippedItemLocked(ch, slot)
		} else {
			w.setEquippedItemLocked(ch, slot, item)
		}
		return true
	}
	return false
}

func (w *World) characterAtPointLocked(players []storage.Character, mapID string, x, y int, targetID int32) (storage.Character, bool) {
	for _, target := range players {
		if target.ID == "" || target.MapID != mapID || w.characterDeadLocked(target) || target.Ghost {
			continue
		}
		if targetID != 0 {
			if abs(target.X-x) > 1 || abs(target.Y-y) > 1 {
				continue
			}
			if CharacterActorID(target) == targetID {
				return target, true
			}
			continue
		}
		if target.X != x || target.Y != y {
			continue
		}
		return target, true
	}
	return storage.Character{}, false
}
