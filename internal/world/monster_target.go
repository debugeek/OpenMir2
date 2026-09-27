package world

import (
	"time"

	"openmir2/internal/storage"
)

func (w *World) monsterIsPassiveLocked(mon *Monster) bool {
	return (mon.Race == 51 || mon.Race == 52) && !mon.Animal
}

func (w *World) monsterIsBeeQueenLocked(mon *Monster) bool {
	return mon.Race == 103
}

func (w *World) monsterIsAnimalLocked(mon *Monster) bool {
	return mon.Animal && mon.Race != 53 && mon.Race != 84
}

func (w *World) monsterIsStickLocked(mon *Monster) bool {
	return mon.Race == 85
}

func (w *World) monsterCannotBePushedLocked(mon *Monster) bool {
	return mon.Race == 85 || mon.Race == 103 || mon.Race == 116 || mon.Behavior == "centipede_king"
}

func (w *World) monsterIsCentipedeLocked(mon *Monster) bool {
	return mon.Behavior == "centipede_king"
}

func (w *World) monsterIsArcherLocked(mon *Monster) bool {
	return mon.Race == 104 || mon.Race == 112
}

func (w *World) monsterIsWhiteSkeletonLocked(mon *Monster) bool {
	return mon.Race == 100 || mon.Race == 87 && mon.TemplateID == "变异骷髅"
}

func (w *World) monsterIsStoneLocked(mon *Monster) bool {
	return mon.Race == 101 || mon.Race == 102
}

func (w *World) monsterIsDualAxeLocked(mon *Monster) bool {
	return mon.Race == 87 && mon.TemplateID == "掷斧骷髅"
}

func (w *World) monsterIsThornDarkLocked(mon *Monster) bool {
	return mon.Race == 93
}

func (w *World) monsterIsGasAttackLocked(mon *Monster) bool {
	return mon.Race == 90 || mon.Race == 105 || mon.Race == 106
}

func (w *World) monsterIsMagicCowLocked(mon *Monster) bool {
	return mon.Race == 91
}

func (w *World) monsterIsDigOutZombieLocked(mon *Monster) bool {
	return mon.Race == 95
}

func (w *World) monsterIsBigHeartLocked(mon *Monster) bool {
	return mon.Race == 115
}

func (w *World) monsterIsSpiderHouseLocked(mon *Monster) bool {
	return mon.Race == 116
}

func (w *World) monsterIsExplosionSpiderLocked(mon *Monster) bool {
	return mon.Race == 117
}

func (w *World) monsterIsSpitSpiderLocked(mon *Monster) bool {
	return mon.Race == 82 || mon.Race == 118 || mon.Race == 119
}

func (w *World) monsterIsElectronicScorpionLocked(mon *Monster) bool {
	return mon.Race == 200
}

func (w *World) monsterStickComeOutRangeLocked(mon *Monster) int {
	return 4
}

func (w *World) monsterStickAttackRangeLocked(mon *Monster) int {
	return 4
}

func (w *World) monsterCentipedeComeOutRangeLocked(mon *Monster) int {
	return 4
}

func (w *World) monsterCentipedeAttackRangeLocked(mon *Monster) int {
	return 6
}

func (w *World) findClosestMonsterTargetLocked(mon *Monster, players map[string]storage.Character, viewRange int) (storage.Character, bool) {
	var target storage.Character
	best := 999999
	for _, ch := range players {
		if !w.monsterCanTargetCharacterLocked(mon, ch) {
			continue
		}
		if abs(ch.X-mon.X) > viewRange || abs(ch.Y-mon.Y) > viewRange {
			continue
		}
		dist := abs(ch.X-mon.X) + abs(ch.Y-mon.Y)
		if dist < best || dist == best && monsterTargetOrderPreferred(ch, target) {
			best = dist
			target = ch
		}
	}
	if target.ID == "" {
		return storage.Character{}, false
	}
	return target, true
}

func (w *World) findClosestMonsterTargetStrictLocked(mon *Monster, players map[string]storage.Character, viewRange int) (storage.Character, bool) {
	var target storage.Character
	best := 999999
	for _, ch := range players {
		if !w.monsterCanTargetCharacterLocked(mon, ch) {
			continue
		}
		if abs(ch.X-mon.X) >= viewRange || abs(ch.Y-mon.Y) >= viewRange {
			continue
		}
		dist := abs(ch.X-mon.X) + abs(ch.Y-mon.Y)
		if dist < best || dist == best && monsterTargetOrderPreferred(ch, target) {
			best = dist
			target = ch
		}
	}
	if target.ID == "" {
		return storage.Character{}, false
	}
	return target, true
}

func (w *World) monsterCanTargetCharacterLocked(mon *Monster, ch storage.Character) bool {
	if mon == nil || ch.MapID != mon.MapID || ch.HP <= 0 || ch.AdminMode || ch.StoneMode {
		return false
	}
	return !characterTransparentStatePresent(ch) || monsterCanSeeTransparent(mon)
}

func (w *World) monsterCanKeepCharacterTargetLocked(mon *Monster, ch storage.Character) bool {
	return mon != nil && ch.MapID == mon.MapID && ch.HP > 0 && !ch.AdminMode && !ch.StoneMode
}

func monsterTargetOrderPreferred(candidate, current storage.Character) bool {
	if current.ID == "" {
		return true
	}
	if candidate.ObjectOrder != 0 && current.ObjectOrder != 0 && candidate.ObjectOrder != current.ObjectOrder {
		return candidate.ObjectOrder < current.ObjectOrder
	}
	return candidate.ID < current.ID
}

func (w *World) clearInvalidMonsterTargetLocked(mon *Monster, players map[string]storage.Character, now time.Time) {
	if mon.TargetCharacterID == "" {
		return
	}
	target, ok := players[mon.TargetCharacterID]
	tooFar := abs(target.X-mon.X) > w.monsterLeashRangeLocked(mon) || abs(target.Y-mon.Y) > w.monsterLeashRangeLocked(mon)
	focusExpired := !mon.TargetFocusAt.IsZero() && now.Sub(mon.TargetFocusAt) > 30*time.Second
	retainSpecialFocus := tooFar && (w.monsterIsCentipedeLocked(mon) || w.monsterIsStickLocked(mon))
	if !ok || !w.monsterCanKeepCharacterTargetLocked(mon, target) || tooFar || focusExpired {
		mon.TargetCharacterID = ""
		if !retainSpecialFocus {
			mon.TargetFocusAt = time.Time{}
		}
		mon.NextSearchAt = now.Add(time.Duration(w.monsterSearchNoTargetMSLocked(mon)) * time.Millisecond)
		mon.TargetX = -1
		mon.TargetY = -1
		mon.RunAwayMode = false
		mon.RunAwayUntil = time.Time{}
	}
}

func (w *World) searchMonsterTargetLocked(mon *Monster, players map[string]storage.Character, now time.Time) {
	var target storage.Character
	best := 999999
	for _, ch := range players {
		if !w.monsterCanTargetCharacterLocked(mon, ch) {
			continue
		}
		if abs(ch.X-mon.X) > w.monsterViewRangeLocked(mon) || abs(ch.Y-mon.Y) > w.monsterViewRangeLocked(mon) {
			continue
		}
		dist := abs(ch.X-mon.X) + abs(ch.Y-mon.Y)
		if dist < best || dist == best && monsterTargetOrderPreferred(ch, target) {
			best = dist
			target = ch
		}
	}
	if target.ID == "" {
		mon.NextSearchAt = now.Add(time.Duration(w.monsterSearchNoTargetMSLocked(mon)) * time.Millisecond)
		return
	}
	mon.TargetCharacterID = target.ID
	mon.TargetFocusAt = now
	mon.NextSearchAt = now.Add(time.Duration(w.monsterSearchHasTargetMSLocked(mon)) * time.Millisecond)
}

func (w *World) monsterViewRangeLocked(mon *Monster) int {
	return mon.ViewRange
}

func (w *World) monsterLeashRangeLocked(mon *Monster) int {
	return mon.LeashRange
}

func (w *World) monsterSearchNoTargetMSLocked(mon *Monster) int {
	return mon.SearchNoTargetMS
}

func (w *World) monsterSearchHasTargetMSLocked(mon *Monster) int {
	return mon.SearchHasTargetMS
}
