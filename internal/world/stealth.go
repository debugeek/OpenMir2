package world

import (
	"sort"
	"time"

	"openmir2/internal/data"
	"openmir2/internal/storage"
)

func characterTransparentActive(ch storage.Character, now time.Time) bool {
	if ch.TransparentUntil <= 0 {
		return false
	}
	return now.UnixNano() <= ch.TransparentUntil
}

func characterTransparentStatePresent(ch storage.Character) bool {
	if ch.TransparentUntil <= 0 {
		return false
	}
	if ch.TransparentHideMode != nil {
		return *ch.TransparentHideMode
	}
	return true
}

func setCharacterTransparentRuntime(ch *storage.Character, active bool) {
	if ch == nil {
		return
	}
	ch.TransparentHideMode = &active
}

func (w *World) applyStealthRingStateLocked(ch *storage.Character, now time.Time) bool {
	if ch == nil {
		return false
	}
	for slot := 0; slot < useSlotCount; slot++ {
		entry, ok := w.equippedItemLocked(*ch, slot)
		if !ok {
			continue
		}
		item, ok := w.data.Items[entry.ItemID]
		if !ok || (item.Shape != 111 && item.AniCount != 111) {
			continue
		}
		until := now.Add(60 * time.Second).UnixNano()
		setCharacterTransparentRuntime(ch, true)
		if until > ch.TransparentUntil {
			ch.TransparentUntil = until
			return true
		}
		return false
	}
	return false
}

func monsterCanSeeTransparent(mon *Monster) bool {
	return mon != nil && mon.CoolEye > 0
}

func (w *World) stealthDurationLocked(ch storage.Character, skill data.StdSkill, state storage.SkillState, base int) time.Duration {
	combat := w.combatStatsLocked(ch)
	power := w.spellPower13Locked(base, skill, state)
	low := combat.SC
	high := combat.SCMax
	if high > low {
		power += (low + w.rand.Intn(high-low+1)) * 3
	} else {
		power += low * 3
	}
	if power < 1 {
		power = 1
	}
	return time.Duration(power) * time.Second
}

func (w *World) showHealthDurationLocked(ch storage.Character, skill data.StdSkill, state storage.SkillState) time.Duration {
	combat := w.combatStatsLocked(ch)
	sc := combat.SC
	if combat.SCMax > sc {
		sc += w.rand.Intn(combat.SCMax - sc + 1)
	}
	power := w.spellPower13Locked(sc*2+30, skill, state)
	if power < 1 {
		power = 1
	}
	return time.Duration(power) * time.Second
}

func setCharacterTransparentLocked(ch *storage.Character, until time.Time) bool {
	if until.UnixNano() <= ch.TransparentUntil {
		return false
	}
	setCharacterTransparentRuntime(ch, true)
	ch.TransparentUntil = until.UnixNano()
	return true
}

func clearCharacterTransparentLocked(ch *storage.Character) bool {
	if ch.TransparentUntil == 0 {
		return false
	}
	setCharacterTransparentRuntime(ch, false)
	ch.TransparentUntil = 0
	return true
}

func (w *World) breakNearbyMonsterTargetsForStealthLocked(ch storage.Character) {
	monsters := make([]*Monster, 0, len(w.monsters))
	for _, mon := range w.monsters {
		monsters = append(monsters, mon)
	}
	sort.SliceStable(monsters, func(i, j int) bool {
		if monsters[i].ObjectOrder != monsters[j].ObjectOrder {
			if monsters[i].ObjectOrder == 0 {
				return false
			}
			if monsters[j].ObjectOrder == 0 {
				return true
			}
			return monsters[i].ObjectOrder < monsters[j].ObjectOrder
		}
		return monsters[i].ID < monsters[j].ID
	})
	for _, mon := range monsters {
		if mon == nil || !mon.Alive || mon.Race < 50 || mon.MapID != ch.MapID || mon.TargetCharacterID != ch.ID {
			continue
		}
		if abs(mon.X-ch.X) > 9 || abs(mon.Y-ch.Y) > 9 {
			continue
		}
		if abs(mon.X-ch.X) > 1 || abs(mon.Y-ch.Y) > 1 || w.rand.Intn(2) == 0 {
			mon.TargetCharacterID = ""
			mon.TargetFocusAt = time.Time{}
		}
	}
}

func (w *World) breakNearbyMonsterTargetsForMonsterStealthLocked(target *Monster) {
	if target == nil {
		return
	}
	monsters := make([]*Monster, 0, len(w.monsters))
	for _, mon := range w.monsters {
		monsters = append(monsters, mon)
	}
	sort.SliceStable(monsters, func(i, j int) bool {
		if monsters[i].ObjectOrder != monsters[j].ObjectOrder {
			if monsters[i].ObjectOrder == 0 {
				return false
			}
			if monsters[j].ObjectOrder == 0 {
				return true
			}
			return monsters[i].ObjectOrder < monsters[j].ObjectOrder
		}
		return monsters[i].ID < monsters[j].ID
	})
	for _, mon := range monsters {
		if mon == nil || !mon.Alive || mon.Race < 50 || mon.MapID != target.MapID || mon.TargetCharacterID != target.ID {
			continue
		}
		if abs(mon.X-target.X) > 9 || abs(mon.Y-target.Y) > 9 {
			continue
		}
		if abs(mon.X-target.X) > 1 || abs(mon.Y-target.Y) > 1 || w.rand.Intn(2) == 0 {
			mon.TargetCharacterID = ""
			mon.TargetFocusAt = time.Time{}
		}
	}
}

func (w *World) applyCharacterStealthTickLocked(ch storage.Character, now time.Time) (storage.Character, bool) {
	if ch.TransparentUntil <= 0 || now.UnixNano() <= ch.TransparentUntil {
		return ch, false
	}
	next := ch
	setCharacterTransparentRuntime(&next, false)
	next.TransparentUntil = 0
	return next, true
}

func (w *World) stealthAffectedTargetsLocked(caster storage.Character, players []storage.Character, targetX, targetY int) []spellAreaTarget {
	affected := make([]spellAreaTarget, 0, 8)
	for _, areaTarget := range w.spellAreaTargetsLocked(players, caster.MapID, targetX, targetY, 1) {
		if areaTarget.Character != nil {
			target := *areaTarget.Character
			if target.ID != "" && w.isProperFriendLocked(caster, target) {
				affected = append(affected, spellAreaTarget{Character: &target})
			}
			continue
		}
		if areaTarget.Monster != nil && w.isFriendlySummonedMonsterLocked(caster, players, areaTarget.Monster) {
			affected = append(affected, spellAreaTarget{Monster: areaTarget.Monster})
		}
	}
	return affected
}
