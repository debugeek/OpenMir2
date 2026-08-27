package world

import (
	"time"

	"openmir2/internal/data"
	"openmir2/internal/storage"
	"openmir2/internal/world/core"
)

func (w *World) Tick(players []PlayerSnapshot, now time.Time) (TickResult, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.respawnLocked(now)
	result := TickResult{}
	updated := map[string]storage.Character{}
	playersByID := map[string]storage.Character{}
	for _, player := range players {
		ch := player.Character
		if ch.ID == "" || ch.HP <= 0 {
			continue
		}
		next, changed := core.ApplyQueuedRecovery(ch, now)
		if changed {
			ch = next
			updated[ch.ID] = ch
		}
		next, changed = w.applyCharacterPoisonTickLocked(ch, now)
		if changed {
			ch = next
			updated[ch.ID] = ch
		}
		next, changed = w.applyCharacterStealthTickLocked(ch, now)
		if changed {
			ch = next
			updated[ch.ID] = ch
			result.StateRefreshCharacters = append(result.StateRefreshCharacters, ch)
		}
		next, changed = w.applyCharacterProtectionTickLocked(ch, now)
		if changed {
			ch = next
			updated[ch.ID] = ch
			result.AbilityRefreshCharacters = append(result.AbilityRefreshCharacters, ch)
		}
		next, changed = w.applyCharacterShowHPOpenTickLocked(ch, now)
		if changed {
			ch = next
			updated[ch.ID] = ch
			result.ShowHPOpenedCharacters = append(result.ShowHPOpenedCharacters, ch)
		}
		next, changed = w.applyCharacterShowHPTickLocked(ch, now)
		if changed {
			ch = next
			updated[ch.ID] = ch
			result.ShowHPExpiredCharacters = append(result.ShowHPExpiredCharacters, ch)
		}
		next, changed = w.applyCharacterTemporaryAbilityTickLocked(ch, now)
		if changed {
			ch = next
			updated[ch.ID] = ch
			result.AbilityRefreshCharacters = append(result.AbilityRefreshCharacters, ch)
		}
		playersByID[ch.ID] = ch
	}
	fireMonsterHits, fireCharacterHits, fireUpdated := w.applyFireWallTickLocked(playersByID, now)
	result.MonsterHits = append(result.MonsterHits, fireMonsterHits...)
	result.CharacterHits = append(result.CharacterHits, fireCharacterHits...)
	for _, ch := range fireUpdated {
		playersByID[ch.ID] = ch
		updated[ch.ID] = ch
	}
	for _, mon := range w.monsters {
		if !mon.Alive {
			continue
		}
		w.applyMonsterProtectionTickLocked(mon, now)
		poisonHits, killed, err := w.applyMonsterPoisonTickLocked(mon, playersByID, now)
		if err != nil {
			return TickResult{}, err
		}
		result.MonsterHits = append(result.MonsterHits, poisonHits...)
		if killed {
			continue
		}
		events, hits, chars, err := w.tickMonsterLocked(mon, playersByID, now)
		if err != nil {
			return TickResult{}, err
		}
		result.MonsterActions = append(result.MonsterActions, events...)
		result.CharacterHits = append(result.CharacterHits, hits...)
		for _, ch := range chars {
			playersByID[ch.ID] = ch
			updated[ch.ID] = ch
		}
	}
	for _, ch := range updated {
		result.Characters = append(result.Characters, ch)
	}
	return result, nil
}

func (w *World) tickMonsterLocked(mon *Monster, players map[string]storage.Character, now time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	if now.Year() >= 2000 && mon.Spawn.MapID != "" && mon.NextSearchAt.IsZero() {
		mon.NextSearchAt = now.Add(time.Duration(2000+w.rand.Intn(2000)) * time.Millisecond)
	}
	if mon.MasterID != "" {
		return w.tickSummonedMonsterLocked(mon, players, now)
	}
	w.clearInvalidMonsterTargetLocked(mon, players, now)
	actions := []MonsterAction{}
	if w.monsterIsWhiteSkeletonLocked(mon) && mon.FirstRevealPending {
		mon.FirstRevealPending = false
		mon.Hidden = false
		mon.FixedHideMode = false
		mon.Dir = 5
		actions = append(actions, w.monsterActionLocked(mon, MonsterActionReveal))
	}
	if w.monsterIsBeeQueenLocked(mon) {
		return w.finishMonsterTickLocked(mon, players, now, actions, w.tickBeeQueenLocked)
	}
	if w.monsterIsExplosionSpiderLocked(mon) {
		return w.finishMonsterTickLocked(mon, players, now, actions, w.tickExplosionSpiderLocked)
	}
	if w.monsterIsStoneLocked(mon) {
		return w.finishMonsterTickLocked(mon, players, now, actions, w.tickStoneMonsterLocked)
	}
	if w.monsterIsDualAxeLocked(mon) {
		return w.finishMonsterTickLocked(mon, players, now, actions, w.tickDualAxeMonsterLocked)
	}
	if w.monsterIsSpiderHouseLocked(mon) {
		return w.finishMonsterTickLocked(mon, players, now, actions, w.tickSpiderHouseLocked)
	}
	if w.monsterIsBigHeartLocked(mon) {
		return w.finishMonsterTickLocked(mon, players, now, actions, w.tickBigHeartLocked)
	}
	if w.monsterIsElectronicScorpionLocked(mon) {
		return w.finishMonsterTickLocked(mon, players, now, actions, w.tickElectronicScorpionLocked)
	}
	if w.monsterIsGasAttackLocked(mon) {
		return w.finishMonsterTickLocked(mon, players, now, actions, w.tickGasAttackMonsterLocked)
	}
	if w.monsterIsSpitSpiderLocked(mon) {
		return w.finishMonsterTickLocked(mon, players, now, actions, w.tickSpitSpiderLocked)
	}
	if w.monsterIsArcherLocked(mon) {
		return w.finishMonsterTickLocked(mon, players, now, actions, w.tickArcherMonsterLocked)
	}
	if w.monsterIsStickLocked(mon) {
		return w.finishMonsterTickLocked(mon, players, now, actions, w.tickStickMonsterLocked)
	}
	if w.monsterIsCentipedeLocked(mon) {
		return w.finishMonsterTickLocked(mon, players, now, actions, w.tickCentipedeMonsterLocked)
	}
	if w.monsterIsAnimalLocked(mon) {
		return w.finishMonsterTickLocked(mon, players, now, actions, w.tickAnimalMonsterLocked)
	}
	if w.monsterIsPassiveLocked(mon) {
		return w.finishMonsterTickLocked(mon, players, now, actions, w.tickPassiveMonsterLocked)
	}
	return w.finishMonsterTickLocked(mon, players, now, actions, w.tickNormalMonsterLocked)
}

func (w *World) finishMonsterTickLocked(mon *Monster, players map[string]storage.Character, now time.Time, prefix []MonsterAction, tick func(*Monster, map[string]storage.Character, time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error)) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	if !w.monsterWalkReadyLocked(mon, now) {
		return prefix, nil, nil, nil
	}
	actions, hits, chars, err := tick(mon, players, now)
	if err != nil {
		return nil, nil, nil, err
	}
	if len(prefix) == 0 {
		return actions, hits, chars, nil
	}
	combined := make([]MonsterAction, 0, len(prefix)+len(actions))
	combined = append(combined, prefix...)
	combined = append(combined, actions...)
	return combined, hits, chars, nil
}

func (w *World) tickNormalMonsterLocked(mon *Monster, players map[string]storage.Character, now time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	if mon.TargetCharacterID == "" && !now.Before(mon.NextSearchAt) {
		w.searchMonsterTargetLocked(mon, players, now)
	}
	if mon.TargetCharacterID == "" {
		if mon.TargetFocusAt.IsZero() {
			if action, ok := w.wanderMonsterLocked(mon, players); ok {
				return []MonsterAction{action}, nil, nil, nil
			}
		}
		return nil, nil, nil, nil
	}
	return w.tickMonsterTargetLocked(mon, players, now)
}

func (w *World) tickMonsterTargetLocked(mon *Monster, players map[string]storage.Character, now time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	if mon.TargetCharacterID != "" {
		target, ok := players[mon.TargetCharacterID]
		if !ok {
			mon.TargetCharacterID = ""
			return nil, nil, nil, nil
		}
		dir := direction(mon.X, mon.Y, target.X, target.Y)
		if abs(mon.X-target.X) <= 1 && abs(mon.Y-target.Y) <= 1 {
			if now.Sub(mon.LastAttackAt) < time.Duration(w.monsterAttackIntervalMSLocked(mon))*time.Millisecond {
				return nil, nil, nil, nil
			}
			mon.Dir = dir
			mon.LastAttackAt = now
			mon.TargetFocusAt = now
			updated, hit, err := w.monsterAttackCharacterLocked(mon, target)
			if err != nil {
				return nil, nil, nil, err
			}
			return []MonsterAction{w.monsterActionLocked(mon, MonsterActionHit)}, []CharacterHit{hit}, []storage.Character{updated}, nil
		}
		if !w.moveMonsterTowardLocked(mon, target, dir, players) {
			return nil, nil, nil, nil
		}
		mon.LastWalkAt = now
		return []MonsterAction{w.monsterActionLocked(mon, MonsterActionWalk)}, nil, nil, nil
	}
	return nil, nil, nil, nil
}

func (w *World) tickBeeQueenLocked(mon *Monster, players map[string]storage.Character, now time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	if mon.TargetCharacterID == "" && !now.Before(mon.NextSearchAt) {
		if target, ok := w.findClosestMonsterTargetLocked(mon, players, w.monsterViewRangeLocked(mon)); ok {
			mon.TargetCharacterID = target.ID
			mon.TargetFocusAt = now
			mon.NextSearchAt = now.Add(time.Duration(w.monsterSearchHasTargetMSLocked(mon)) * time.Millisecond)
		} else {
			mon.NextSearchAt = now.Add(time.Duration(w.monsterSearchNoTargetMSLocked(mon)) * time.Millisecond)
		}
	}
	if mon.TargetCharacterID == "" {
		return nil, nil, nil, nil
	}
	if w.countMonsterChildrenLocked(mon.ID) >= 15 {
		return nil, nil, nil, nil
	}
	if now.Sub(mon.LastAttackAt) < time.Duration(w.monsterAttackIntervalMSLocked(mon))*time.Millisecond {
		return nil, nil, nil, nil
	}
	mon.LastAttackAt = now
	mon.TargetFocusAt = now
	childName := "蝙蝠"
	if !w.spawnChildMonsterAtLocked(mon, childName, mon.X, mon.Y, now) {
		return nil, nil, nil, nil
	}
	return []MonsterAction{w.monsterActionLocked(mon, MonsterActionHit)}, nil, nil, nil
}

func (w *World) tickCentipedeMonsterLocked(mon *Monster, players map[string]storage.Character, now time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	if mon.Hidden {
		if target, ok := w.findClosestMonsterTargetStrictLocked(mon, players, w.monsterCentipedeComeOutRangeLocked(mon)); ok {
			mon.Hidden = false
			mon.FixedHideMode = false
			mon.StoneMode = false
			mon.TargetCharacterID = target.ID
			mon.TargetFocusAt = now
			mon.Dir = direction(mon.X, mon.Y, target.X, target.Y)
			mon.LastAttackAt = now
			return []MonsterAction{w.monsterActionLocked(mon, MonsterActionReveal)}, nil, nil, nil
		}
		return nil, nil, nil, nil
	}
	if mon.TargetCharacterID == "" {
		if now.Sub(mon.TargetFocusAt) <= 10*time.Second {
			return nil, nil, nil, nil
		}
		target, ok := w.findClosestMonsterTargetLocked(mon, players, w.monsterCentipedeAttackRangeLocked(mon))
		if !ok {
			mon.Hidden = true
			mon.FixedHideMode = true
			mon.StoneMode = true
			mon.TargetX = -1
			mon.TargetY = -1
			mon.TargetFocusAt = now
			return []MonsterAction{w.monsterActionLocked(mon, MonsterActionHide)}, nil, nil, nil
		}
		mon.TargetCharacterID = target.ID
		mon.TargetFocusAt = now
	}
	target, ok := w.findClosestMonsterTargetLocked(mon, players, w.monsterCentipedeAttackRangeLocked(mon))
	if !ok {
		mon.TargetCharacterID = ""
		if now.Sub(mon.TargetFocusAt) > 10*time.Second {
			mon.Hidden = true
			mon.FixedHideMode = true
			mon.StoneMode = true
			mon.TargetX = -1
			mon.TargetY = -1
			mon.TargetFocusAt = now
			return []MonsterAction{w.monsterActionLocked(mon, MonsterActionHide)}, nil, nil, nil
		}
		return nil, nil, nil, nil
	}
	mon.TargetCharacterID = target.ID
	if now.Sub(mon.LastAttackAt) < time.Duration(w.monsterAttackIntervalMSLocked(mon))*time.Millisecond {
		return nil, nil, nil, nil
	}
	mon.Dir = direction(mon.X, mon.Y, target.X, target.Y)
	mon.LastAttackAt = now
	mon.TargetFocusAt = now
	updated, hit, err := w.monsterAttackCharacterLocked(mon, target)
	if err != nil {
		return nil, nil, nil, err
	}
	return []MonsterAction{w.monsterActionLocked(mon, MonsterActionHit)}, []CharacterHit{hit}, []storage.Character{updated}, nil
}

func (w *World) tickDualAxeMonsterLocked(mon *Monster, players map[string]storage.Character, now time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	if mon.TargetCharacterID == "" && now.Sub(mon.TargetFocusAt) >= 5*time.Second {
		if target, ok := w.findClosestMonsterTargetLocked(mon, players, mon.ViewRange); ok {
			mon.TargetCharacterID = target.ID
			mon.TargetFocusAt = now
		}
	}
	if mon.TargetCharacterID == "" {
		return nil, nil, nil, nil
	}
	target, ok := players[mon.TargetCharacterID]
	if !ok || target.MapID != mon.MapID || target.HP <= 0 {
		mon.TargetCharacterID = ""
		mon.TargetX = -1
		mon.TargetY = -1
		return nil, nil, nil, nil
	}
	if abs(mon.X-target.X) <= 7 && abs(mon.Y-target.Y) <= 7 {
		if now.Sub(mon.LastAttackAt) >= time.Duration(w.monsterAttackIntervalMSLocked(mon))*time.Millisecond {
			mon.LastAttackAt = now
			mon.TargetFocusAt = now
			if mon.AttackMax == 0 {
				mon.AttackMax = 6
			}
			if mon.AttackCount < mon.AttackMax-1 {
				mon.AttackCount++
				mon.Dir = direction(mon.X, mon.Y, target.X, target.Y)
				updated, hit, err := w.monsterAttackCharacterLocked(mon, target)
				if err != nil {
					return nil, nil, nil, err
				}
				return []MonsterAction{w.monsterActionLocked(mon, MonsterActionHit)}, []CharacterHit{hit}, []storage.Character{updated}, nil
			}
			if w.rand.Intn(5) == 0 {
				mon.AttackCount = 0
			}
		}
		return nil, nil, nil, nil
	}
	if abs(mon.X-target.X) <= 11 && abs(mon.Y-target.Y) <= 11 {
		if moved, ok := w.retreatFromTargetLocked(mon, target); ok {
			mon.LastWalkAt = now
			return []MonsterAction{moved}, nil, nil, nil
		}
	}
	return nil, nil, nil, nil
}

func (w *World) tickSpitSpiderLocked(mon *Monster, players map[string]storage.Character, now time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	if mon.TargetCharacterID == "" && !now.Before(mon.NextSearchAt) {
		w.searchMonsterTargetLocked(mon, players, now)
	}
	if mon.TargetCharacterID == "" {
		return nil, nil, nil, nil
	}
	target, ok := players[mon.TargetCharacterID]
	if !ok || target.MapID != mon.MapID || target.HP <= 0 {
		mon.TargetCharacterID = ""
		mon.NextSearchAt = now.Add(time.Duration(w.monsterSearchNoTargetMSLocked(mon)) * time.Millisecond)
		return nil, nil, nil, nil
	}
	if abs(mon.X-target.X) <= 2 && abs(mon.Y-target.Y) <= 2 {
		if now.Sub(mon.LastAttackAt) < time.Duration(w.monsterAttackIntervalMSLocked(mon))*time.Millisecond {
			return nil, nil, nil, nil
		}
		mon.LastAttackAt = now
		mon.TargetFocusAt = now
		mon.Dir = direction(mon.X, mon.Y, target.X, target.Y)
		updated, hit, err := w.monsterAttackCharacterLocked(mon, target)
		if err != nil {
			return nil, nil, nil, err
		}
		return []MonsterAction{w.monsterActionLocked(mon, MonsterActionHit)}, []CharacterHit{hit}, []storage.Character{updated}, nil
	}
	return w.tickNormalMonsterLocked(mon, players, now)
}

func (w *World) tickGasAttackMonsterLocked(mon *Monster, players map[string]storage.Character, now time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	if mon.TargetCharacterID == "" && !now.Before(mon.NextSearchAt) {
		w.searchMonsterTargetLocked(mon, players, now)
	}
	if mon.TargetCharacterID == "" {
		return nil, nil, nil, nil
	}
	target, ok := players[mon.TargetCharacterID]
	if !ok || target.MapID != mon.MapID || target.HP <= 0 {
		mon.TargetCharacterID = ""
		mon.NextSearchAt = now.Add(time.Duration(w.monsterSearchNoTargetMSLocked(mon)) * time.Millisecond)
		return nil, nil, nil, nil
	}
	if abs(mon.X-target.X) > 2 || abs(mon.Y-target.Y) > 2 {
		return nil, nil, nil, nil
	}
	if now.Sub(mon.LastAttackAt) < time.Duration(w.monsterAttackIntervalMSLocked(mon))*time.Millisecond {
		return nil, nil, nil, nil
	}
	mon.LastAttackAt = now
	mon.TargetFocusAt = now
	mon.Dir = direction(mon.X, mon.Y, target.X, target.Y)
	updated, hit, err := w.monsterAttackCharacterLocked(mon, target)
	if err != nil {
		return nil, nil, nil, err
	}
	return []MonsterAction{w.monsterActionLocked(mon, MonsterActionHit)}, []CharacterHit{hit}, []storage.Character{updated}, nil
}

func (w *World) tickArcherMonsterLocked(mon *Monster, players map[string]storage.Character, now time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	if mon.TargetCharacterID == "" && !now.Before(mon.NextSearchAt) {
		if target, ok := w.findClosestMonsterTargetLocked(mon, players, w.monsterViewRangeLocked(mon)); ok {
			mon.TargetCharacterID = target.ID
			mon.TargetFocusAt = now
			mon.NextSearchAt = now.Add(time.Duration(w.monsterSearchHasTargetMSLocked(mon)) * time.Millisecond)
		} else {
			mon.NextSearchAt = now.Add(time.Duration(w.monsterSearchNoTargetMSLocked(mon)) * time.Millisecond)
			return nil, nil, nil, nil
		}
	}
	if mon.TargetCharacterID == "" {
		return nil, nil, nil, nil
	}
	target, ok := players[mon.TargetCharacterID]
	if !ok || target.MapID != mon.MapID || abs(target.X-mon.X) > w.monsterViewRangeLocked(mon) || abs(target.Y-mon.Y) > w.monsterViewRangeLocked(mon) {
		mon.TargetCharacterID = ""
		mon.NextSearchAt = now.Add(time.Duration(w.monsterSearchNoTargetMSLocked(mon)) * time.Millisecond)
		if mon.GuardDirection >= 0 && mon.Dir != mon.GuardDirection {
			mon.Dir = mon.GuardDirection
			return []MonsterAction{w.monsterActionLocked(mon, MonsterActionTurn)}, nil, nil, nil
		}
		return nil, nil, nil, nil
	}
	if now.Sub(mon.LastAttackAt) < time.Duration(w.monsterAttackIntervalMSLocked(mon))*time.Millisecond {
		return nil, nil, nil, nil
	}
	mon.Dir = direction(mon.X, mon.Y, target.X, target.Y)
	mon.LastAttackAt = now
	mon.TargetFocusAt = now
	updated, hit, err := w.monsterAttackCharacterLocked(mon, target)
	if err != nil {
		return nil, nil, nil, err
	}
	return []MonsterAction{w.monsterActionLocked(mon, MonsterActionHit)}, []CharacterHit{hit}, []storage.Character{updated}, nil
}

func (w *World) tickStickMonsterLocked(mon *Monster, players map[string]storage.Character, now time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	if mon.Hidden {
		if target, ok := w.findClosestMonsterTargetStrictLocked(mon, players, w.monsterStickComeOutRangeLocked(mon)); ok {
			mon.Hidden = false
			mon.FixedHideMode = false
			mon.StoneMode = false
			mon.Dir = direction(mon.X, mon.Y, target.X, target.Y)
			mon.NextSearchAt = now.Add(time.Duration(w.monsterSearchHasTargetMSLocked(mon)) * time.Millisecond)
			return []MonsterAction{w.monsterActionLocked(mon, MonsterActionReveal)}, nil, nil, nil
		}
		return nil, nil, nil, nil
	}
	if mon.TargetCharacterID == "" && !now.Before(mon.NextSearchAt) {
		target, ok := w.findClosestMonsterTargetLocked(mon, players, w.monsterViewRangeLocked(mon))
		if !ok {
			mon.Hidden = true
			mon.FixedHideMode = true
			mon.StoneMode = true
			mon.TargetX = -1
			mon.TargetY = -1
			mon.NextSearchAt = now.Add(time.Duration(w.monsterSearchNoTargetMSLocked(mon)) * time.Millisecond)
			return []MonsterAction{w.monsterActionLocked(mon, MonsterActionHide)}, nil, nil, nil
		}
		mon.TargetCharacterID = target.ID
		mon.TargetFocusAt = now
		mon.NextSearchAt = now.Add(time.Duration(w.monsterSearchHasTargetMSLocked(mon)) * time.Millisecond)
	}
	if mon.TargetCharacterID == "" {
		return nil, nil, nil, nil
	}
	target, ok := players[mon.TargetCharacterID]
	if !ok || target.MapID != mon.MapID || target.HP <= 0 {
		mon.TargetCharacterID = ""
		mon.Hidden = true
		mon.FixedHideMode = true
		mon.StoneMode = true
		mon.TargetX = -1
		mon.TargetY = -1
		mon.NextSearchAt = now.Add(time.Duration(w.monsterSearchNoTargetMSLocked(mon)) * time.Millisecond)
		return []MonsterAction{w.monsterActionLocked(mon, MonsterActionHide)}, nil, nil, nil
	}
	attackRange := w.monsterStickAttackRangeLocked(mon)
	if abs(mon.X-target.X) > attackRange || abs(mon.Y-target.Y) > attackRange {
		mon.TargetCharacterID = ""
		mon.Hidden = true
		mon.FixedHideMode = true
		mon.StoneMode = true
		mon.TargetX = -1
		mon.TargetY = -1
		mon.NextSearchAt = now.Add(time.Duration(w.monsterSearchNoTargetMSLocked(mon)) * time.Millisecond)
		return []MonsterAction{w.monsterActionLocked(mon, MonsterActionHide)}, nil, nil, nil
	}
	if abs(mon.X-target.X) <= 1 && abs(mon.Y-target.Y) <= 1 {
		if now.Sub(mon.LastAttackAt) < time.Duration(w.monsterAttackIntervalMSLocked(mon))*time.Millisecond {
			return nil, nil, nil, nil
		}
		mon.Dir = direction(mon.X, mon.Y, target.X, target.Y)
		mon.LastAttackAt = now
		mon.TargetFocusAt = now
		updated, hit, err := w.monsterAttackCharacterLocked(mon, target)
		if err != nil {
			return nil, nil, nil, err
		}
		return []MonsterAction{w.monsterActionLocked(mon, MonsterActionHit)}, []CharacterHit{hit}, []storage.Character{updated}, nil
	}
	return nil, nil, nil, nil
}

func (w *World) tickStoneMonsterLocked(mon *Monster, players map[string]storage.Character, now time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	if mon.StoneMode {
		if target, ok := w.findClosestMonsterTargetLocked(mon, players, w.monsterViewRangeLocked(mon)); ok {
			if abs(mon.X-target.X) <= 2 && abs(mon.Y-target.Y) <= 2 {
				mon.StoneMode = false
				mon.TargetCharacterID = target.ID
				mon.TargetFocusAt = now
				mon.NextSearchAt = now.Add(time.Duration(w.monsterSearchHasTargetMSLocked(mon)) * time.Millisecond)
				return []MonsterAction{w.monsterActionLocked(mon, MonsterActionReveal)}, nil, nil, nil
			}
		}
		return nil, nil, nil, nil
	}
	if mon.TargetCharacterID == "" && !now.Before(mon.NextSearchAt) {
		target, ok := w.findClosestMonsterTargetLocked(mon, players, w.monsterViewRangeLocked(mon))
		if !ok {
			mon.NextSearchAt = now.Add(time.Duration(w.monsterSearchNoTargetMSLocked(mon)) * time.Millisecond)
			return nil, nil, nil, nil
		}
		mon.TargetCharacterID = target.ID
		mon.TargetFocusAt = now
		mon.NextSearchAt = now.Add(time.Duration(w.monsterSearchHasTargetMSLocked(mon)) * time.Millisecond)
	}
	return w.tickNormalMonsterLocked(mon, players, now)
}

func (w *World) tickPassiveMonsterLocked(mon *Monster, players map[string]storage.Character, now time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	if mon.TargetCharacterID != "" {
		return w.tickMonsterTargetLocked(mon, players, now)
	}
	if mon.TargetFocusAt.IsZero() {
		if action, ok := w.wanderMonsterLocked(mon, players); ok {
			return []MonsterAction{action}, nil, nil, nil
		}
	}
	return nil, nil, nil, nil
}

func (w *World) tickAnimalMonsterLocked(mon *Monster, players map[string]storage.Character, now time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	if mon.FleeOnSight {
		return w.tickFleeAnimalMonsterLocked(mon, players, now)
	}
	return w.tickPassiveMonsterLocked(mon, players, now)
}

func (w *World) tickFleeAnimalMonsterLocked(mon *Monster, players map[string]storage.Character, now time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	if mon.TargetCharacterID == "" && !now.Before(mon.NextSearchAt) {
		if target, ok := w.findClosestMonsterTargetLocked(mon, players, mon.ViewRange); ok {
			mon.TargetCharacterID = target.ID
			mon.TargetFocusAt = now
			mon.RunAwayMode = true
			mon.TargetX = -1
			mon.TargetY = -1
			mon.NextSearchAt = now.Add(time.Duration(w.monsterSearchHasTargetMSLocked(mon)) * time.Millisecond)
		} else {
			mon.RunAwayMode = false
			mon.TargetX = -1
			mon.TargetY = -1
			mon.NextSearchAt = now.Add(time.Duration(w.monsterSearchNoTargetMSLocked(mon)) * time.Millisecond)
		}
	}
	if mon.RunAwayMode && mon.TargetCharacterID != "" {
		target, ok := players[mon.TargetCharacterID]
		if !ok || target.MapID != mon.MapID || target.HP <= 0 {
			mon.TargetCharacterID = ""
			mon.TargetFocusAt = time.Time{}
			mon.RunAwayMode = false
			mon.TargetX = -1
			mon.TargetY = -1
			return w.tickPassiveMonsterLocked(mon, players, now)
		}
		if abs(mon.X-target.X) <= 6 && abs(mon.Y-target.Y) <= 6 {
			mon.TargetX, mon.TargetY = fleePointForMonster(mon, target)
		}
		if mon.TargetX >= 0 && mon.TargetY >= 0 {
			if mon.X == mon.TargetX && mon.Y == mon.TargetY {
				mon.TargetX = -1
				mon.TargetY = -1
				return nil, nil, nil, nil
			}
			if w.moveMonsterTowardPointLocked(mon, mon.TargetX, mon.TargetY, players) {
				mon.LastWalkAt = now
				return []MonsterAction{w.monsterActionLocked(mon, MonsterActionWalk)}, nil, nil, nil
			}
			return nil, nil, nil, nil
		}
	}
	return w.tickPassiveMonsterLocked(mon, players, now)
}

func (w *World) explosionSpiderLocked(mon *Monster, players map[string]storage.Character) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	mon.HP = core.ApplyHPDelta(mon.HP, mon.MaxHP, -mon.HP).HP
	var hits []CharacterHit
	var updated []storage.Character
	var nearest storage.Character
	best := 999999
	for _, ch := range players {
		if ch.MapID != mon.MapID || ch.HP <= 0 {
			continue
		}
		if abs(ch.X-mon.X) <= 1 && abs(ch.Y-mon.Y) <= 1 {
			if dist := abs(ch.X-mon.X) + abs(ch.Y-mon.Y); dist < best {
				best = dist
				nearest = ch
			}
			dmg := mon.MinAttack
			if mon.MaxAttack > mon.MinAttack {
				dmg += w.rand.Intn(mon.MaxAttack - mon.MinAttack + 1)
			}
			if dmg < 1 {
				dmg = 1
			}
			change := core.ApplyVitalDelta(ch, -(dmg / 2), 0)
			ch = change.Character
			hit := CharacterHit{
				Character:       ch,
				Damage:          dmg / 2,
				AttackerID:      mon.ID,
				AttackerRaceImg: mon.RaceImg,
				AttackerAppr:    mon.Appr,
				AttackerX:       mon.X,
				AttackerY:       mon.Y,
				Dead:            change.Dead,
			}
			hits = append(hits, hit)
			updated = append(updated, ch)
		}
	}
	if nearest.ID != "" {
		mon.Dir = direction(mon.X, mon.Y, nearest.X, nearest.Y)
	}
	return []MonsterAction{w.monsterActionLocked(mon, MonsterActionHit)}, hits, updated, nil
}

func (w *World) tickBigHeartLocked(mon *Monster, players map[string]storage.Character, now time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	if now.Sub(mon.LastAttackAt) < time.Duration(w.monsterAttackIntervalMSLocked(mon))*time.Millisecond {
		return nil, nil, nil, nil
	}
	var nearest storage.Character
	best := 999999
	for _, ch := range players {
		if ch.MapID != mon.MapID || ch.HP <= 0 {
			continue
		}
		if abs(ch.X-mon.X) <= mon.ViewRange && abs(ch.Y-mon.Y) <= mon.ViewRange {
			dist := abs(ch.X-mon.X) + abs(ch.Y-mon.Y)
			if dist < best {
				best = dist
				nearest = ch
			}
		}
	}
	if nearest.ID == "" {
		return nil, nil, nil, nil
	}
	mon.LastAttackAt = now
	mon.TargetFocusAt = now
	mon.Dir = direction(mon.X, mon.Y, nearest.X, nearest.Y)
	actions := []MonsterAction{w.monsterActionLocked(mon, MonsterActionHit)}
	hits := []CharacterHit{}
	updated := []storage.Character{}
	power := mon.MinAttack
	if mon.MaxAttack > mon.MinAttack {
		power += w.rand.Intn(mon.MaxAttack - mon.MinAttack + 1)
	}
	if power < 1 {
		power = 1
	}
	for _, ch := range players {
		if ch.MapID != mon.MapID || ch.HP <= 0 {
			continue
		}
		if abs(ch.X-mon.X) <= mon.ViewRange && abs(ch.Y-mon.Y) <= mon.ViewRange {
			next, hit, err := w.monsterAttackCharacterWithDamageLocked(mon, ch, power)
			if err != nil {
				return nil, nil, nil, err
			}
			hits = append(hits, hit)
			updated = append(updated, next)
		}
	}
	return actions, hits, updated, nil
}

func (w *World) tickElectronicScorpionLocked(mon *Monster, players map[string]storage.Character, now time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	if mon.TargetCharacterID == "" && now.Sub(mon.TargetFocusAt) >= 1*time.Second {
		w.searchMonsterTargetLocked(mon, players, now)
	}
	if mon.TargetCharacterID == "" {
		return nil, nil, nil, nil
	}
	target, ok := players[mon.TargetCharacterID]
	if !ok || target.MapID != mon.MapID || target.HP <= 0 {
		mon.TargetCharacterID = ""
		return nil, nil, nil, nil
	}
	mon.UseMagic = mon.HP < mon.MaxHP/2
	nx := abs(mon.X - target.X)
	ny := abs(mon.Y - target.Y)
	if nx <= 2 && ny <= 2 {
		if !mon.UseMagic && nx != 2 && ny != 2 {
			return nil, nil, nil, nil
		}
		if now.Sub(mon.LastAttackAt) < time.Duration(w.monsterAttackIntervalMSLocked(mon))*time.Millisecond {
			return nil, nil, nil, nil
		}
		mon.LastAttackAt = now
		mon.TargetFocusAt = now
		mon.Dir = direction(mon.X, mon.Y, target.X, target.Y)
		updated, hit, err := w.monsterAttackCharacterLocked(mon, target)
		if err != nil {
			return nil, nil, nil, err
		}
		return []MonsterAction{w.monsterActionLocked(mon, MonsterActionHit)}, []CharacterHit{hit}, []storage.Character{updated}, nil
	}
	return nil, nil, nil, nil
}

func (w *World) tickExplosionSpiderLocked(mon *Monster, players map[string]storage.Character, now time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	if mon.ExplosionStartAt.IsZero() {
		mon.ExplosionStartAt = now
	}
	if now.Sub(mon.ExplosionStartAt) > 60*time.Second {
		mon.ExplosionStartAt = now
		return w.explosionSpiderLocked(mon, players)
	}
	if mon.TargetCharacterID == "" && !now.Before(mon.NextSearchAt) {
		w.searchMonsterTargetLocked(mon, players, now)
	}
	if mon.TargetCharacterID == "" {
		return nil, nil, nil, nil
	}
	target, ok := players[mon.TargetCharacterID]
	if !ok || target.MapID != mon.MapID || target.HP <= 0 {
		mon.TargetCharacterID = ""
		mon.NextSearchAt = now.Add(time.Duration(w.monsterSearchNoTargetMSLocked(mon)) * time.Millisecond)
		return nil, nil, nil, nil
	}
	if abs(mon.X-target.X) <= 1 && abs(mon.Y-target.Y) <= 1 {
		if now.Sub(mon.LastAttackAt) < time.Duration(w.monsterAttackIntervalMSLocked(mon))*time.Millisecond {
			return nil, nil, nil, nil
		}
		mon.LastAttackAt = now
		mon.TargetFocusAt = now
		return w.explosionSpiderLocked(mon, players)
	}
	return w.tickNormalMonsterLocked(mon, players, now)
}

func (w *World) tickSpiderHouseLocked(mon *Monster, players map[string]storage.Character, now time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	if now.Sub(mon.LastAttackAt) < time.Duration(w.monsterAttackIntervalMSLocked(mon))*time.Millisecond {
		return nil, nil, nil, nil
	}
	w.searchMonsterTargetLocked(mon, players, now)
	if mon.TargetCharacterID == "" {
		return nil, nil, nil, nil
	}
	if w.countMonsterChildrenLocked(mon.ID) >= 15 {
		return nil, nil, nil, nil
	}
	childName := "爆裂蜘蛛"
	if !w.spawnChildMonsterLocked(mon, childName, now) {
		return nil, nil, nil, nil
	}
	mon.LastAttackAt = now
	mon.TargetFocusAt = now
	return []MonsterAction{w.monsterActionLocked(mon, MonsterActionHit)}, nil, nil, nil
}

func (w *World) countMonsterChildrenLocked(parentID string) int {
	count := 0
	for _, mon := range w.monsters {
		if mon.Alive && !mon.Hidden && mon.ParentID == parentID {
			count++
		}
	}
	return count
}

func (w *World) spawnChildMonsterLocked(parent *Monster, childName string, now time.Time) bool {
	tpl, ok := w.monsterTemplateByIDLocked(childName)
	if !ok {
		return false
	}
	if _, ok := w.data.Maps[parent.MapID]; !ok {
		return false
	}
	child := w.createSpawnMonsterLocked(data.StdSpawn{MapID: parent.MapID, MonsterID: tpl.ID, X: parent.X, Y: parent.Y, Count: 1, RespawnSeconds: 0}, tpl, parent.X, parent.Y+1)
	if child == nil {
		return false
	}
	if child.X != parent.X || child.Y != parent.Y {
		w.occupyMonsterLocked(child)
	}
	child.ParentID = parent.ID
	child.TargetCharacterID = parent.TargetCharacterID
	child.TargetFocusAt = now
	return true
}

func (w *World) spawnChildMonsterAtLocked(parent *Monster, childName string, x, y int, now time.Time) bool {
	tpl, ok := w.monsterTemplateByIDLocked(childName)
	if !ok {
		return false
	}
	if _, ok := w.data.Maps[parent.MapID]; !ok {
		return false
	}
	child := w.createSpawnMonsterLocked(data.StdSpawn{MapID: parent.MapID, MonsterID: tpl.ID, X: x, Y: y, Count: 1, RespawnSeconds: 0}, tpl, x, y)
	if child == nil {
		return false
	}
	if child.X != parent.X || child.Y != parent.Y {
		w.occupyMonsterLocked(child)
	}
	child.ParentID = parent.ID
	child.TargetCharacterID = parent.TargetCharacterID
	child.TargetFocusAt = now
	return true
}
