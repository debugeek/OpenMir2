package world

import (
	"time"

	"openmir2/internal/storage"
)

func (w *World) monsterIsSummonedLocked(mon *Monster) bool {
	return mon != nil && mon.MasterID != ""
}

func (w *World) summonedMonsterOwnerLocked(mon *Monster, players map[string]storage.Character, now time.Time) (storage.Character, bool) {
	if mon == nil || mon.MasterID == "" {
		return storage.Character{}, false
	}
	master, ok := players[mon.MasterID]
	if !ok || master.HP <= 0 || master.MapID != mon.MapID {
		return storage.Character{}, false
	}
	if !mon.MasterExpiresAt.IsZero() && now.After(mon.MasterExpiresAt) {
		return storage.Character{}, false
	}
	return master, true
}

func (w *World) findClosestMonsterTargetExceptLocked(mon *Monster, players map[string]storage.Character, viewRange int, excludeID string, now time.Time) (storage.Character, bool) {
	var target storage.Character
	best := 999999
	for _, ch := range players {
		if ch.ID == excludeID || ch.MapID != mon.MapID || ch.HP <= 0 {
			continue
		}
		if characterTransparentActive(ch, now) && !monsterCanSeeTransparent(mon) {
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

func (w *World) tickSummonedMonsterLocked(mon *Monster, players map[string]storage.Character, now time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	master, ok := w.summonedMonsterOwnerLocked(mon, players, now)
	if !ok {
		w.removeMonsterLocked(mon, false)
		return nil, nil, nil, nil
	}
	if mon.TargetCharacterID == master.ID {
		mon.TargetCharacterID = ""
		mon.TargetFocusAt = time.Time{}
	}
	if mon.TargetCharacterID != "" {
		target, ok := players[mon.TargetCharacterID]
		if !ok || target.MapID != mon.MapID || target.HP <= 0 {
			mon.TargetCharacterID = ""
			mon.TargetFocusAt = time.Time{}
		}
	}
	if mon.TargetCharacterID == "" && !now.Before(mon.NextSearchAt) {
		if target, ok := w.findClosestMonsterTargetExceptLocked(mon, players, mon.ViewRange, master.ID, now); ok {
			mon.TargetCharacterID = target.ID
			mon.TargetFocusAt = now
			mon.NextSearchAt = now.Add(time.Duration(w.monsterSearchHasTargetMSLocked(mon)) * time.Millisecond)
		} else {
			mon.NextSearchAt = now.Add(time.Duration(w.monsterSearchNoTargetMSLocked(mon)) * time.Millisecond)
		}
	}
	if mon.TargetCharacterID != "" {
		return w.tickMonsterTargetLocked(mon, players, now)
	}
	if abs(mon.X-master.X) <= 1 && abs(mon.Y-master.Y) <= 1 {
		return nil, nil, nil, nil
	}
	if w.moveMonsterTowardPointLocked(mon, master.X, master.Y, players) {
		mon.LastWalkAt = now
		return []MonsterAction{w.monsterActionLocked(mon, MonsterActionWalk)}, nil, nil, nil
	}
	return nil, nil, nil, nil
}
