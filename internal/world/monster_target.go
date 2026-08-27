package world

import (
	"time"

	"openmir2/internal/storage"
)

func (w *World) monsterIsPassiveLocked(mon *Monster) bool {
	return mon.Race == 51 || mon.Race == 52
}

func (w *World) monsterIsBeeQueenLocked(mon *Monster) bool {
	return mon.Race == 103
}

func (w *World) monsterIsAnimalLocked(mon *Monster) bool {
	return mon.Animal
}

func (w *World) monsterIsStickLocked(mon *Monster) bool {
	return mon.Race == 85
}

func (w *World) monsterIsCentipedeLocked(mon *Monster) bool {
	return mon.Race == 107
}

func (w *World) monsterIsArcherLocked(mon *Monster) bool {
	return mon.Race == 112
}

func (w *World) monsterIsWhiteSkeletonLocked(mon *Monster) bool {
	return mon.Race == 87
}

func (w *World) monsterIsStoneLocked(mon *Monster) bool {
	return mon.Race == 101 || mon.Race == 102
}

func (w *World) monsterIsDualAxeLocked(mon *Monster) bool {
	return mon.Race == 104
}

func (w *World) monsterIsGasAttackLocked(mon *Monster) bool {
	return mon.Race == 105 || mon.Race == 106
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
	return mon.Race == 118 || mon.Race == 119
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
		if ch.MapID != mon.MapID || ch.HP <= 0 {
			continue
		}
		if characterTransparentActive(ch, time.Now()) && !monsterCanSeeTransparent(mon) {
			continue
		}
		if abs(ch.X-mon.X) > viewRange || abs(ch.Y-mon.Y) > viewRange {
			continue
		}
		dist := abs(ch.X-mon.X) + abs(ch.Y-mon.Y)
		if dist < best {
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
		if ch.MapID != mon.MapID || ch.HP <= 0 {
			continue
		}
		if characterTransparentActive(ch, time.Now()) && !monsterCanSeeTransparent(mon) {
			continue
		}
		if abs(ch.X-mon.X) >= viewRange || abs(ch.Y-mon.Y) >= viewRange {
			continue
		}
		dist := abs(ch.X-mon.X) + abs(ch.Y-mon.Y)
		if dist < best {
			best = dist
			target = ch
		}
	}
	if target.ID == "" {
		return storage.Character{}, false
	}
	return target, true
}

func (w *World) clearInvalidMonsterTargetLocked(mon *Monster, players map[string]storage.Character, now time.Time) {
	if mon.TargetCharacterID == "" {
		return
	}
	target, ok := players[mon.TargetCharacterID]
	if !ok || target.HP <= 0 || target.MapID != mon.MapID || abs(target.X-mon.X) > w.monsterLeashRangeLocked(mon) || abs(target.Y-mon.Y) > w.monsterLeashRangeLocked(mon) || (characterTransparentActive(target, now) && !monsterCanSeeTransparent(mon)) {
		mon.TargetCharacterID = ""
		mon.NextSearchAt = now.Add(time.Duration(w.monsterSearchNoTargetMSLocked(mon)) * time.Millisecond)
		mon.TargetX = -1
		mon.TargetY = -1
		mon.RunAwayMode = false
	}
}

func (w *World) searchMonsterTargetLocked(mon *Monster, players map[string]storage.Character, now time.Time) {
	var target storage.Character
	best := 999999
	for _, ch := range players {
		if ch.MapID != mon.MapID || ch.HP <= 0 {
			continue
		}
		if characterTransparentActive(ch, now) && !monsterCanSeeTransparent(mon) {
			continue
		}
		if abs(ch.X-mon.X) > w.monsterViewRangeLocked(mon) || abs(ch.Y-mon.Y) > w.monsterViewRangeLocked(mon) {
			continue
		}
		dist := abs(ch.X-mon.X) + abs(ch.Y-mon.Y)
		if dist < best {
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
