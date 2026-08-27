package world

import (
	"math"
	"time"

	"openmir2/internal/data"
	"openmir2/internal/storage"
)

func characterTransparentActive(ch storage.Character, now time.Time) bool {
	if ch.TransparentUntil <= 0 {
		return false
	}
	return now.UnixNano() < ch.TransparentUntil
}

func monsterCanSeeTransparent(mon *Monster) bool {
	return mon != nil && mon.CoolEye > 0
}

func (w *World) stealthDurationLocked(ch storage.Character, skill data.StdSkill, state storage.SkillState, base int) time.Duration {
	combat := w.combatStatsLocked(ch)
	trainLevel := skill.TrainLevel1
	if trainLevel < 0 {
		trainLevel = 0
	}
	d10 := float64(base) / 3.0
	d18 := float64(base) - d10
	power := int(math.Round(d18/float64(trainLevel+1)*float64(int(state.Level)+1) + d10))
	low := combat.SC
	high := combat.SCMax
	if high > low {
		power += low + w.rand.Intn(high-low+1)
	} else {
		power += low
	}
	if power < 1 {
		power = 1
	}
	return time.Duration(power) * time.Second
}

func (w *World) showHealthDurationLocked(ch storage.Character, skill data.StdSkill, state storage.SkillState) time.Duration {
	combat := w.combatStatsLocked(ch)
	nInt := combat.SC
	high := combat.SCMax
	if high > nInt {
		nInt = nInt + w.rand.Intn(high-nInt+1)
	}
	nInt = nInt*2 + 30
	base := skill.Power
	if base <= 0 {
		base = 1
	}
	maxPower := skill.MaxPower
	if maxPower < base {
		maxPower = base
	}
	roll := base
	if maxPower > base {
		roll += w.rand.Intn(maxPower-base + 1)
	}
	trainLevel := skill.TrainLevel1
	if trainLevel < 0 {
		trainLevel = 0
	}
	d10 := float64(nInt) / 3.0
	d18 := float64(nInt) - d10
	power := int(math.Round(d18/float64(trainLevel+1)*float64(int(state.Level)+1) + d10 + float64(roll)))
	if power < 1 {
		power = 1
	}
	return time.Duration(power) * time.Second
}

func setCharacterTransparentLocked(ch *storage.Character, until time.Time) bool {
	if until.UnixNano() <= ch.TransparentUntil {
		return false
	}
	ch.TransparentUntil = until.UnixNano()
	return true
}

func clearCharacterTransparentLocked(ch *storage.Character) bool {
	if ch.TransparentUntil == 0 {
		return false
	}
	ch.TransparentUntil = 0
	return true
}

func (w *World) breakNearbyMonsterTargetsForStealthLocked(ch storage.Character) {
	for _, mon := range w.monsters {
		if mon == nil || !mon.Alive || mon.MapID != ch.MapID || mon.TargetCharacterID != ch.ID {
			continue
		}
		if abs(mon.X-ch.X) > 9 || abs(mon.Y-ch.Y) > 9 {
			continue
		}
		if abs(mon.X-ch.X) > 1 || abs(mon.Y-ch.Y) > 1 || w.rand.Intn(2) == 0 {
			mon.TargetCharacterID = ""
			mon.TargetFocusAt = time.Time{}
			mon.NextSearchAt = time.Time{}
		}
	}
}

func (w *World) applyCharacterStealthTickLocked(ch storage.Character, now time.Time) (storage.Character, bool) {
	if ch.TransparentUntil <= 0 || now.UnixNano() < ch.TransparentUntil {
		return ch, false
	}
	next := ch
	next.TransparentUntil = 0
	return next, true
}

func (w *World) stealthAffectedTargetsLocked(caster storage.Character, players []storage.Character, targetX, targetY int) []storage.Character {
	affected := make([]storage.Character, 0, 8)
	for _, target := range players {
		if target.ID == "" || target.MapID != caster.MapID || target.HP <= 0 {
			continue
		}
		if abs(target.X-targetX) > 1 || abs(target.Y-targetY) > 1 {
			continue
		}
		if !w.isProperFriendLocked(caster, target) {
			continue
		}
		affected = append(affected, target)
	}
	return affected
}
