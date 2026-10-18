package world

import (
	"strings"
	"time"

	"openmir2/internal/storage"
)

var referenceSpitMap = [8][][2]int{
	{{0, -2}, {0, -1}},
	{{2, -2}, {1, -1}},
	{{1, 0}, {2, 0}},
	{{1, 1}, {2, 2}},
	{{0, 1}, {0, 2}},
	{{-1, 1}, {-2, 2}},
	{{-1, 0}, {-2, 0}},
	{{-1, -1}, {-2, -2}},
}

func monsterTargetAliveLocked(mon *Monster) bool {
	return mon != nil && mon.Alive && !mon.PendingDeath && !mon.Ghost
}

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
	return mon.Race == 85 || mon.Race == 103 || mon.Race == 116 || mon.Race == 107 || mon.Behavior == "centipede_king"
}

func (w *World) monsterIsCentipedeLocked(mon *Monster) bool {
	return mon.Race == 107 || mon.Behavior == "centipede_king"
}

func (w *World) monsterUsesAnimalObjectRunLocked(mon *Monster) bool {
	return w.monsterIsBeeQueenLocked(mon) || w.monsterIsSpiderHouseLocked(mon) || w.monsterIsBigHeartLocked(mon) || w.monsterIsStickLocked(mon) || w.monsterIsCentipedeLocked(mon)
}

func (w *World) monsterIsArcherLocked(mon *Monster) bool {
	return mon.Race == 112
}

func (w *World) monsterIsWhiteSkeletonLocked(mon *Monster) bool {
	return mon.Race == 100
}

func (w *World) monsterIsStoneLocked(mon *Monster) bool {
	return mon.Race == 101 || mon.Race == 102
}

func (w *World) monsterIsDualAxeLocked(mon *Monster) bool {
	return mon.Race == 87 || mon.Race == 104
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

func (w *World) monsterIsCowKingLocked(mon *Monster) bool {
	return mon.Race == 92
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
	for _, key := range w.visibleActorKeysLocked(mon.ID, mon.MapID, mon.X, mon.Y, viewRange, players) {
		if !strings.HasPrefix(key, "character:") {
			continue
		}
		ch, ok := players[strings.TrimPrefix(key, "character:")]
		if !ok {
			continue
		}
		if !w.monsterCanTargetCharacterLocked(mon, ch) {
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

func (w *World) findClosestArcherCharacterTargetLocked(mon *Monster, players map[string]storage.Character, viewRange int) (storage.Character, bool) {
	var target storage.Character
	best := 999999
	for _, key := range w.visibleActorKeysLocked(mon.ID, mon.MapID, mon.X, mon.Y, viewRange, players) {
		if !strings.HasPrefix(key, "character:") {
			continue
		}
		ch, ok := players[strings.TrimPrefix(key, "character:")]
		if !ok {
			continue
		}
		if !w.archerCanTargetCharacterLocked(mon, ch) {
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
	for _, key := range w.visibleActorKeysLocked(mon.ID, mon.MapID, mon.X, mon.Y, viewRange, players) {
		if !strings.HasPrefix(key, "character:") {
			continue
		}
		ch, ok := players[strings.TrimPrefix(key, "character:")]
		if !ok {
			continue
		}
		if !w.monsterCanTargetCharacterLocked(mon, ch) {
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

func (w *World) centipedeHasTargetLocked(mon *Monster, players map[string]storage.Character, viewRange int, strict bool) bool {
	for _, key := range w.visibleActorKeysLocked(mon.ID, mon.MapID, mon.X, mon.Y, viewRange, players) {
		if strings.HasPrefix(key, "character:") {
			ch, ok := players[strings.TrimPrefix(key, "character:")]
			if !ok || (!strict && !w.monsterCanKeepCharacterTargetLocked(mon, ch)) || (strict && !w.monsterCanTargetCharacterLocked(mon, ch)) {
				continue
			}
			if strict && (abs(ch.X-mon.X) >= viewRange || abs(ch.Y-mon.Y) >= viewRange) || !strict && (abs(ch.X-mon.X) > viewRange || abs(ch.Y-mon.Y) > viewRange) {
				continue
			}
			return true
		}
		if !strings.HasPrefix(key, "monster:") {
			continue
		}
		candidate := w.monsters[strings.TrimPrefix(key, "monster:")]
		if !monsterTargetAliveLocked(candidate) || candidate.ID == "" || candidate.ID == mon.ID || candidate.FixedHideMode || candidate.AdminMode || candidate.StoneMode || candidate.MapID != mon.MapID || candidate.MasterID == "" || mon.MasterID != "" && candidate.MasterID == mon.MasterID {
			continue
		}
		if strict && candidate.Hidden && !monsterCanSeeTransparent(mon) {
			continue
		}
		if strict && (abs(candidate.X-mon.X) >= viewRange || abs(candidate.Y-mon.Y) >= viewRange) || !strict && (abs(candidate.X-mon.X) > viewRange || abs(candidate.Y-mon.Y) > viewRange) {
			continue
		}
		return true
	}
	return false
}

func (w *World) stoneHasVisibleTargetLocked(mon *Monster, players map[string]storage.Character, viewRange int) bool {
	for _, key := range w.visibleActorKeysLocked(mon.ID, mon.MapID, mon.X, mon.Y, viewRange, players) {
		if strings.HasPrefix(key, "character:") {
			target, ok := players[strings.TrimPrefix(key, "character:")]
			if ok && w.monsterCanTargetCharacterLocked(mon, target) && abs(target.X-mon.X) <= viewRange && abs(target.Y-mon.Y) <= viewRange {
				return true
			}
			continue
		}
		if !strings.HasPrefix(key, "monster:") {
			continue
		}
		target := w.monsters[strings.TrimPrefix(key, "monster:")]
		if monsterTargetAliveLocked(target) && target.ID != "" && target.ID != mon.ID && !target.FixedHideMode && !target.AdminMode && !target.StoneMode && target.MapID == mon.MapID && target.MasterID != "" && (mon.MasterID == "" || target.MasterID != mon.MasterID) && (!target.Hidden || monsterCanSeeTransparent(mon)) && abs(target.X-mon.X) <= viewRange && abs(target.Y-mon.Y) <= viewRange {
			return true
		}
	}
	return false
}

func (w *World) findClosestArcherMonsterTargetLocked(mon *Monster) (*Monster, bool) {
	if mon == nil {
		return nil, false
	}
	var target *Monster
	best := 999999
	for _, key := range w.visibleActorKeysLocked(mon.ID, mon.MapID, mon.X, mon.Y, mon.ViewRange, nil) {
		if !strings.HasPrefix(key, "monster:") {
			continue
		}
		candidate := w.monsters[strings.TrimPrefix(key, "monster:")]
		if !monsterTargetAliveLocked(candidate) || candidate.ID == "" || candidate.ID == mon.ID || candidate.MapID != mon.MapID {
			continue
		}
		if mon.CastleID != "" {
			if !w.castleArcherCanTargetMonsterLocked(mon, candidate) {
				continue
			}
		} else if candidate.ID != mon.LastHitterID && candidate.TargetCharacterID != mon.ID && candidate.TargetMonsterID != mon.ID {
			continue
		}
		if abs(candidate.X-mon.X) > mon.ViewRange || abs(candidate.Y-mon.Y) > mon.ViewRange {
			continue
		}
		distance := abs(candidate.X-mon.X) + abs(candidate.Y-mon.Y)
		if target == nil || distance < best {
			target = candidate
			best = distance
		}
	}
	return target, target != nil
}

func (w *World) findClosestArcherTargetLocked(mon *Monster, players map[string]storage.Character) (fleeTarget, bool) {
	var target fleeTarget
	best := 999999
	for _, key := range w.visibleActorKeysLocked(mon.ID, mon.MapID, mon.X, mon.Y, w.monsterViewRangeLocked(mon), players) {
		var distance int
		if strings.HasPrefix(key, "character:") {
			ch, ok := players[strings.TrimPrefix(key, "character:")]
			if !ok || !w.archerCanTargetCharacterLocked(mon, ch) {
				continue
			}
			if abs(ch.X-mon.X) > mon.ViewRange || abs(ch.Y-mon.Y) > mon.ViewRange {
				continue
			}
			distance = abs(ch.X-mon.X) + abs(ch.Y-mon.Y)
			if distance >= best {
				continue
			}
			copy := ch
			target = fleeTarget{character: &copy}
		} else if strings.HasPrefix(key, "monster:") {
			candidate := w.monsters[strings.TrimPrefix(key, "monster:")]
			if !monsterTargetAliveLocked(candidate) || candidate.ID == "" || candidate.ID == mon.ID || candidate.AdminMode || candidate.StoneMode || candidate.MapID != mon.MapID {
				continue
			}
			if mon.CastleID != "" {
				if !w.castleArcherCanTargetMonsterLocked(mon, candidate) {
					continue
				}
			} else if candidate.ID != mon.LastHitterID && candidate.TargetCharacterID != mon.ID && candidate.TargetMonsterID != mon.ID {
				continue
			}
			if abs(candidate.X-mon.X) > mon.ViewRange || abs(candidate.Y-mon.Y) > mon.ViewRange {
				continue
			}
			distance = abs(candidate.X-mon.X) + abs(candidate.Y-mon.Y)
			if distance >= best {
				continue
			}
			target = fleeTarget{monster: candidate}
		} else {
			continue
		}
		best = distance
	}
	return target, target.character != nil || target.monster != nil
}

func (w *World) castleArcherCanTargetCharacterLocked(mon *Monster, ch storage.Character) bool {
	castle, ok := w.castleSnapshotLocked(mon.CastleID)
	if !ok {
		return false
	}
	if ch.ID == mon.LastHitterID {
		return true
	}
	proper := false
	if tauntAt, found := w.characterCastleTauntAt[ch.ID]; found {
		if w.monsterActionTimeLocked().Sub(tauntAt) < 2*time.Minute {
			proper = true
		} else {
			delete(w.characterCastleTauntAt, ch.ID)
		}
	}
	if castle.UnderWar {
		proper = true
	}
	if castle.OwnerGuildID != "" && guildProtectsCastleTarget(w, castle.OwnerGuildID, ch) {
		return false
	}
	return proper
}

func (w *World) archerCanTargetCharacterLocked(mon *Monster, ch storage.Character) bool {
	if !w.monsterCanTargetCharacterLocked(mon, ch) {
		return false
	}
	if mon.CastleID != "" {
		return w.castleArcherCanTargetCharacterLocked(mon, ch)
	}
	return ch.ID == mon.LastHitterID || w.characterTargetsArcherGuardLocked(ch) || characterPKLevel(ch) >= 2
}

func (w *World) characterTargetsArcherGuardLocked(ch storage.Character) bool {
	target := w.monsters[ch.TargetID]
	return target != nil && target.Race == 112
}

func (w *World) castleArcherCanTargetMonsterLocked(mon, candidate *Monster) bool {
	castle, ok := w.castleSnapshotLocked(mon.CastleID)
	if !ok || candidate.CastleID == mon.CastleID {
		return false
	}
	now := w.monsterActionTimeLocked()
	if candidate.ID == mon.LastHitterID {
		return true
	}
	proper := false
	if !candidate.CastleTauntAt.IsZero() {
		if now.Sub(candidate.CastleTauntAt) < 2*time.Minute {
			proper = true
		} else {
			candidate.CastleTauntAt = time.Time{}
		}
	}
	if castle.UnderWar {
		proper = true
	}
	if candidate.MasterID != "" {
		if master, found := w.store.Character(candidate.MasterID); found && castle.OwnerGuildID != "" && guildProtectsCastleTarget(w, castle.OwnerGuildID, master) {
			return candidate.MasterID == mon.LastHitterID || candidate.ID == mon.LastHitterID
		}
	}
	return proper
}

func guildProtectsCastleTarget(w *World, ownerGuildID string, ch storage.Character) bool {
	if ch.GuildID == ownerGuildID {
		return true
	}
	owner, ok := w.store.Guild(ownerGuildID)
	if !ok {
		return false
	}
	if owner.Alliance == ch.GuildID {
		return true
	}
	for _, alliance := range owner.Alliances {
		if alliance == ch.GuildID {
			return true
		}
	}
	return containsAllianceID(ch.GuildAllianceIDs, ownerGuildID) || ch.GuildAllianceID == ownerGuildID
}

func (w *World) findClosestSummonedMonsterTargetLocked(mon *Monster, players map[string]storage.Character, viewRange int) (*Monster, bool) {
	if mon == nil {
		return nil, false
	}
	var target *Monster
	best := 999999
	for _, key := range w.visibleActorKeysLocked(mon.ID, mon.MapID, mon.X, mon.Y, viewRange, players) {
		if !strings.HasPrefix(key, "monster:") {
			continue
		}
		candidate := w.monsters[strings.TrimPrefix(key, "monster:")]
		if !monsterTargetAliveLocked(candidate) || candidate.ID == "" || candidate.ID == mon.ID || candidate.FixedHideMode || candidate.AdminMode || candidate.StoneMode || candidate.MapID != mon.MapID || candidate.MasterID == "" || mon.MasterID != "" && candidate.MasterID == mon.MasterID {
			continue
		}
		if candidate.Hidden && !monsterCanSeeTransparent(mon) {
			continue
		}
		if abs(candidate.X-mon.X) > viewRange || abs(candidate.Y-mon.Y) > viewRange {
			continue
		}
		distance := abs(candidate.X-mon.X) + abs(candidate.Y-mon.Y)
		if target == nil || distance < best {
			target = candidate
			best = distance
		}
	}
	return target, target != nil
}

type fleeTarget struct {
	character *storage.Character
	monster   *Monster
}

func (w *World) findClosestFleeTargetLocked(mon *Monster, players map[string]storage.Character, viewRange int) (fleeTarget, bool) {
	var target fleeTarget
	best := 999999
	for _, key := range w.visibleActorKeysLocked(mon.ID, mon.MapID, mon.X, mon.Y, viewRange, players) {
		var distance int
		var candidateCharacter *storage.Character
		var candidateMonster *Monster
		if strings.HasPrefix(key, "character:") {
			ch, ok := players[strings.TrimPrefix(key, "character:")]
			if !ok || !w.monsterCanTargetCharacterLocked(mon, ch) || characterTransparentStatePresent(ch) && !monsterCanSeeTransparent(mon) {
				continue
			}
			if mon.MasterID != "" {
				if ch.ID == mon.MasterID {
					continue
				}
				if master, ok := players[mon.MasterID]; ok && w.isProperFriendLocked(master, ch) {
					continue
				}
			}
			if abs(ch.X-mon.X) > viewRange || abs(ch.Y-mon.Y) > viewRange {
				continue
			}
			copy := ch
			candidateCharacter = &copy
			distance = abs(ch.X-mon.X) + abs(ch.Y-mon.Y)
		} else if strings.HasPrefix(key, "monster:") {
			candidate := w.monsters[strings.TrimPrefix(key, "monster:")]
			if !monsterTargetAliveLocked(candidate) || candidate.ID == "" || candidate.ID == mon.ID || candidate.MasterID == "" || mon.MasterID != "" && candidate.MasterID == mon.MasterID || candidate.FixedHideMode || candidate.AdminMode || candidate.StoneMode || candidate.MapID != mon.MapID {
				continue
			}
			if candidate.Hidden && !monsterCanSeeTransparent(mon) {
				continue
			}
			if abs(candidate.X-mon.X) > viewRange || abs(candidate.Y-mon.Y) > viewRange {
				continue
			}
			candidateMonster = candidate
			distance = abs(candidate.X-mon.X) + abs(candidate.Y-mon.Y)
		} else {
			continue
		}
		if distance >= best {
			continue
		}
		target = fleeTarget{character: candidateCharacter, monster: candidateMonster}
		best = distance
	}
	return target, target.character != nil || target.monster != nil
}

func (w *World) lastVisibleAnimalActorPointLocked(mon *Monster, players map[string]storage.Character) (int, int, bool) {
	if mon == nil {
		return 0, 0, false
	}
	var x, y int
	found := false
	for _, key := range w.visibleActorKeysLocked(mon.ID, mon.MapID, mon.X, mon.Y, mon.ViewRange, players) {
		if strings.HasPrefix(key, "character:") {
			ch, ok := players[strings.TrimPrefix(key, "character:")]
			if !ok {
				continue
			}
			x, y, found = ch.X, ch.Y, true
			continue
		}
		if strings.HasPrefix(key, "monster:") {
			candidate := w.monsters[strings.TrimPrefix(key, "monster:")]
			if candidate == nil || candidate.ID == mon.ID {
				continue
			}
			x, y, found = candidate.X, candidate.Y, true
		}
	}
	return x, y, found
}

func (w *World) monsterCanTargetCharacterLocked(mon *Monster, ch storage.Character) bool {
	if mon == nil || ch.ID == "" || ch.MapID != mon.MapID || w.characterDeadLocked(ch) || ch.AdminMode || ch.Ghost || ch.StoneMode {
		return false
	}
	if mon.MasterID != "" && w.isSafeZoneLocked(ch) {
		return false
	}
	return !characterTransparentStatePresent(ch) || monsterCanSeeTransparent(mon)
}

func (w *World) monsterCanKeepCharacterTargetLocked(mon *Monster, ch storage.Character) bool {
	return mon != nil && ch.ID != "" && ch.MapID == mon.MapID && !w.characterDeadLocked(ch) && !ch.AdminMode && !ch.StoneMode && (mon.MasterID == "" || !w.isSafeZoneLocked(ch))
}

func (w *World) clearInvalidMonsterTargetLocked(mon *Monster, players map[string]storage.Character, now time.Time) {
	if mon.TargetCharacterID == "" && mon.TargetMonsterID == "" {
		return
	}
	if mon.TargetCharacterID != "" {
		target, ok := players[mon.TargetCharacterID]
		if ok && target.ID != "" && !w.characterDeadLocked(target) && !target.AdminMode && !target.Ghost && !target.StoneMode && (mon.MasterID == "" || !w.isSafeZoneLocked(target)) {
			return
		}
		mon.TargetCharacterID = ""
		mon.TargetFocusAt = time.Time{}
		mon.NextSearchAt = now.Add(time.Duration(w.monsterSearchNoTargetMSLocked(mon)) * time.Millisecond)
		mon.TargetX = -1
		mon.TargetY = -1
		mon.RunAwayMode = false
		mon.RunAwayUntil = time.Time{}
		return
	}
	target := w.monsters[mon.TargetMonsterID]
	if !monsterTargetAliveLocked(target) || target.ID == "" || target.ID == mon.ID || target.MasterID == "" && mon.Race != 112 || mon.MasterID != "" && target.MasterID == mon.MasterID || target.FixedHideMode || target.AdminMode || target.StoneMode || target.MapID != mon.MapID {
		mon.TargetMonsterID = ""
		mon.TargetFocusAt = time.Time{}
		mon.NextSearchAt = now.Add(time.Duration(w.monsterSearchNoTargetMSLocked(mon)) * time.Millisecond)
		mon.TargetX = -1
		mon.TargetY = -1
		mon.RunAwayMode = false
		mon.RunAwayUntil = time.Time{}
	}
}

func (w *World) cleanupMonsterTargetAfterRunLocked(mon *Monster, players map[string]storage.Character, now time.Time) {
	if mon == nil {
		return
	}
	if mon.TargetCharacterID != "" {
		target, ok := players[mon.TargetCharacterID]
		if !ok || target.ID == "" || w.characterDeadLocked(target) || target.Ghost || target.MapID != mon.MapID || abs(target.X-mon.X) > 15 || abs(target.Y-mon.Y) > 15 || !mon.TargetFocusAt.IsZero() && now.Sub(mon.TargetFocusAt) > 30*time.Second {
			mon.TargetCharacterID = ""
			mon.TargetFocusAt = time.Time{}
			mon.TargetX = -1
			mon.TargetY = -1
		}
		return
	}
	if mon.TargetMonsterID != "" {
		target := w.monsters[mon.TargetMonsterID]
		if !monsterTargetAliveLocked(target) || target.ID == "" || target.MapID != mon.MapID || abs(target.X-mon.X) > 15 || abs(target.Y-mon.Y) > 15 || !mon.TargetFocusAt.IsZero() && now.Sub(mon.TargetFocusAt) > 30*time.Second {
			mon.TargetMonsterID = ""
			mon.TargetFocusAt = time.Time{}
			mon.TargetX = -1
			mon.TargetY = -1
		}
	}
}

func (w *World) searchMonsterTargetLocked(mon *Monster, players map[string]storage.Character, now time.Time) {
	w.searchMonsterTargetWithInitialDistanceLocked(mon, players, now, 999)
}

func (w *World) searchDualAxeTargetLocked(mon *Monster, players map[string]storage.Character, now time.Time) {
	w.searchMonsterTargetWithInitialDistanceLocked(mon, players, now, 9999)
}

func (w *World) searchMonsterTargetWithInitialDistanceLocked(mon *Monster, players map[string]storage.Character, now time.Time, initialDistance int) {
	targetCharacterID := ""
	targetMonsterID := ""
	best := initialDistance
	for _, key := range w.visibleActorKeysLocked(mon.ID, mon.MapID, mon.X, mon.Y, w.monsterViewRangeLocked(mon), players) {
		if strings.HasPrefix(key, "character:") {
			ch, ok := players[strings.TrimPrefix(key, "character:")]
			canTarget := w.monsterCanTargetCharacterLocked(mon, ch)
			if mon.Race == 105 {
				canTarget = w.monsterCanKeepCharacterTargetLocked(mon, ch)
			}
			if !ok || !canTarget || abs(ch.X-mon.X) > w.monsterViewRangeLocked(mon) || abs(ch.Y-mon.Y) > w.monsterViewRangeLocked(mon) {
				continue
			}
			dist := abs(ch.X-mon.X) + abs(ch.Y-mon.Y)
			if dist < best {
				best = dist
				targetCharacterID = ch.ID
				targetMonsterID = ""
			}
			continue
		}
		if !strings.HasPrefix(key, "monster:") {
			continue
		}
		summon := w.monsters[strings.TrimPrefix(key, "monster:")]
		if !monsterTargetAliveLocked(summon) || summon.ID == mon.ID || summon.FixedHideMode || summon.AdminMode || summon.StoneMode || summon.MapID != mon.MapID || summon.MasterID == "" || mon.MasterID != "" && summon.MasterID == mon.MasterID || summon.Hidden && mon.Race != 105 && !monsterCanSeeTransparent(mon) || abs(summon.X-mon.X) > w.monsterViewRangeLocked(mon) || abs(summon.Y-mon.Y) > w.monsterViewRangeLocked(mon) {
			continue
		}
		dist := abs(summon.X-mon.X) + abs(summon.Y-mon.Y)
		if dist < best {
			best = dist
			targetCharacterID = ""
			targetMonsterID = summon.ID
		}
	}
	if targetCharacterID == "" && targetMonsterID == "" {
		mon.NextSearchAt = now.Add(time.Duration(w.monsterSearchNoTargetMSLocked(mon)) * time.Millisecond)
		return
	}
	mon.TargetMonsterID = targetMonsterID
	mon.TargetCharacterID = targetCharacterID
	mon.TargetFocusAt = now
	mon.NextSearchAt = now.Add(time.Duration(w.monsterSearchHasTargetMSLocked(mon)) * time.Millisecond)
}

func monsterTargetOrderPreferred(candidate, current storage.Character) bool {
	if current.ID == "" {
		return true
	}
	if candidate.ObjectOrder != 0 || current.ObjectOrder != 0 {
		if candidate.ObjectOrder == 0 {
			return false
		}
		if current.ObjectOrder == 0 {
			return true
		}
		if candidate.ObjectOrder != current.ObjectOrder {
			return candidate.ObjectOrder < current.ObjectOrder
		}
	}
	return candidate.ID < current.ID
}

func (w *World) monsterViewRangeLocked(mon *Monster) int {
	return mon.ViewRange
}

func (w *World) monsterLeashRangeLocked(mon *Monster) int {
	return mon.LeashRange
}

func (w *World) monsterSearchNoTargetMSLocked(mon *Monster) int {
	if mon.SearchNoTargetMS == 0 {
		switch mon.Race {
		case 53, 81, 83, 84, 86, 88, 89, 97:
			return 1000
		}
	}
	return mon.SearchNoTargetMS
}

func (w *World) monsterSearchHasTargetMSLocked(mon *Monster) int {
	return mon.SearchHasTargetMS
}

func (w *World) monsterUsesReferencePeriodicSearchLocked(mon *Monster) bool {
	switch mon.Race {
	case 53, 81, 82, 83, 84, 86, 88, 89, 96, 97, 100, 118, 119:
		return true
	default:
		return false
	}
}
