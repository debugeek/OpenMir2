package world

import (
	"fmt"

	"openmir2/internal/data"
	"openmir2/internal/storage"
)

func (w *World) occupiedActorsLocked(players []storage.Character) map[monsterPosition]string {
	occupied := make(map[monsterPosition]string, len(players)+len(w.monsters))
	for _, ch := range players {
		if ch.ID == "" || ch.HP <= 0 || ch.MapID == "" {
			continue
		}
		occupied[monsterPosition{MapID: ch.MapID, X: ch.X, Y: ch.Y}] = ch.ID
	}
	for _, mon := range w.monsters {
		if mon == nil || !mon.Alive || mon.MapID == "" {
			continue
		}
		occupied[monsterPosition{MapID: mon.MapID, X: mon.X, Y: mon.Y}] = mon.ID
	}
	return occupied
}

func (w *World) canOccupyLocked(mp data.StdMap, occupied map[monsterPosition]string, mapID string, x, y int, selfID string) bool {
	if !mp.Walkable(x, y) {
		return false
	}
	if occupant, ok := occupied[monsterPosition{MapID: mapID, X: x, Y: y}]; ok && occupant != selfID {
		return false
	}
	return true
}

func (w *World) findCharacterAtTileLocked(players []storage.Character, mapID string, x, y int, exceptID string) (storage.Character, bool) {
	for _, target := range players {
		if target.ID == "" || target.ID == exceptID || target.MapID != mapID || target.HP <= 0 {
			continue
		}
		if target.X == x && target.Y == y {
			return target, true
		}
	}
	return storage.Character{}, false
}

func (w *World) findCharacterByIDLocked(players []storage.Character, id string) (storage.Character, bool) {
	for _, target := range players {
		if target.ID == id {
			return target, true
		}
	}
	return storage.Character{}, false
}

func (w *World) monsterByIDLocked(id string) (*Monster, bool) {
	mon, ok := w.monsters[id]
	if !ok || mon == nil {
		return nil, false
	}
	return mon, true
}

func appendOrUpdateAffectedCharacter(result *SkillCastResult, ch storage.Character) {
	for i := range result.AffectedCharacters {
		if result.AffectedCharacters[i].ID == ch.ID {
			result.AffectedCharacters[i] = ch
			return
		}
	}
	result.AffectedCharacters = append(result.AffectedCharacters, ch)
}

func appendOrUpdateMonsterAction(result *SkillCastResult, action MonsterAction) {
	if action.MonsterID == "" {
		result.MonsterActions = append(result.MonsterActions, action)
		return
	}
	for i := range result.MonsterActions {
		if result.MonsterActions[i].MonsterID == action.MonsterID {
			result.MonsterActions[i] = action
			return
		}
	}
	result.MonsterActions = append(result.MonsterActions, action)
}

func (w *World) pushCharacterAwayLocked(caster storage.Character, target storage.Character, push int, mp data.StdMap, occupied map[monsterPosition]string) (storage.Character, bool, error) {
	if target.ID == "" || target.MapID != caster.MapID || target.HP <= 0 || target.ID == caster.ID {
		return target, false, nil
	}
	dir := direction(caster.X, caster.Y, target.X, target.Y)
	if dir < 0 || dir >= len(dirOffsets) {
		return target, false, nil
	}
	off := dirOffsets[dir]
	moved := false
	for i := 0; i < push; i++ {
		nextX := target.X + off[0]
		nextY := target.Y + off[1]
		if !w.canOccupyLocked(mp, occupied, target.MapID, nextX, nextY, target.ID) {
			break
		}
		delete(occupied, monsterPosition{MapID: target.MapID, X: target.X, Y: target.Y})
		target.X = nextX
		target.Y = nextY
		occupied[monsterPosition{MapID: target.MapID, X: target.X, Y: target.Y}] = target.ID
		moved = true
	}
	if !moved {
		return target, false, nil
	}
	if err := w.store.SaveCharacter(target); err != nil {
		return target, false, err
	}
	return target, true, nil
}

func (w *World) pushMonsterAwayLocked(caster storage.Character, mon *Monster, push int, mp data.StdMap, occupied map[monsterPosition]string) (MonsterAction, bool) {
	if mon == nil || !mon.Alive || mon.MapID != caster.MapID {
		return MonsterAction{}, false
	}
	dir := direction(caster.X, caster.Y, mon.X, mon.Y)
	if dir < 0 || dir >= len(dirOffsets) {
		return MonsterAction{}, false
	}
	off := dirOffsets[dir]
	moved := false
	for i := 0; i < push; i++ {
		nextX := mon.X + off[0]
		nextY := mon.Y + off[1]
		if !w.canOccupyLocked(mp, occupied, mon.MapID, nextX, nextY, mon.ID) {
			break
		}
		w.vacateMonsterLocked(mon)
		mon.X = nextX
		mon.Y = nextY
		mon.Dir = dir
		w.occupyMonsterLocked(mon)
		occupied[monsterPosition{MapID: mon.MapID, X: mon.X, Y: mon.Y}] = mon.ID
		moved = true
	}
	if !moved {
		return MonsterAction{}, false
	}
	return w.monsterActionLocked(mon, MonsterActionWalk), true
}

func (w *World) castChargeSkillLocked(result *SkillCastResult, ch storage.Character, state storage.SkillState, players []storage.Character, targetX, targetY int) (storage.Character, error) {
	mp, ok := w.data.Maps[ch.MapID]
	if !ok {
		return ch, fmt.Errorf("map %s not found", ch.MapID)
	}
	if targetX == ch.X && targetY == ch.Y {
		return ch, fmt.Errorf("no valid target")
	}
	dir := direction(ch.X, ch.Y, targetX, targetY)
	if dir < 0 || dir >= len(dirOffsets) {
		return ch, fmt.Errorf("no valid target")
	}
	push := maxInt(2, int(state.Level)+1)
	occupied := w.occupiedActorsLocked(players)
	for step := 0; step < push; step++ {
		off := dirOffsets[dir]
		nextX := ch.X + off[0]
		nextY := ch.Y + off[1]
		if !mp.Walkable(nextX, nextY) {
			break
		}
		if occupant, ok := occupied[monsterPosition{MapID: ch.MapID, X: nextX, Y: nextY}]; ok && occupant != ch.ID {
			target, found := w.findCharacterByIDLocked(players, occupant)
			if found {
				if ch.Level <= target.Level {
					break
				}
				next, moved, err := w.pushCharacterAwayLocked(ch, target, 1, mp, occupied)
				if err != nil {
					return ch, err
				}
				if !moved {
					break
				}
				for i := range players {
					if players[i].ID == next.ID {
						players[i] = next
						break
					}
				}
				appendOrUpdateAffectedCharacter(result, next)
			} else if mon, found := w.monsterByIDLocked(occupant); found {
				if ch.Level <= mon.Level {
					break
				}
				action, moved := w.pushMonsterAwayLocked(ch, mon, 1, mp, occupied)
				if !moved {
					break
				}
				appendOrUpdateMonsterAction(result, action)
			} else {
				break
			}
		}
		if !w.canOccupyLocked(mp, occupied, ch.MapID, nextX, nextY, ch.ID) {
			break
		}
		ch.X = nextX
		ch.Y = nextY
		ch.Dir = dir
	}
	result.Character = ch
	return ch, nil
}

func (w *World) castPushAroundSkillLocked(result *SkillCastResult, ch storage.Character, state storage.SkillState, players []storage.Character) (storage.Character, error) {
	mp, ok := w.data.Maps[ch.MapID]
	if !ok {
		return ch, fmt.Errorf("map %s not found", ch.MapID)
	}
	occupied := w.occupiedActorsLocked(players)
	affectedChars := w.charactersInRadiusLocked(players, ch.MapID, ch.X, ch.Y, 1)
	nearbyMonsters := w.monstersInRadiusLocked(ch.MapID, ch.X, ch.Y, 1)
	push := maxInt(1, int(state.Level)) + w.rand.Intn(2)
	for _, target := range affectedChars {
		if target.ID == ch.ID {
			continue
		}
		if ch.Level <= target.Level {
			continue
		}
		threshold := 6 + int(state.Level)*3 + (ch.Level - target.Level)
		if threshold < 0 {
			threshold = 0
		}
		if w.rand.Intn(20) >= threshold {
			continue
		}
		next, moved, err := w.pushCharacterAwayLocked(ch, target, push, mp, occupied)
		if err != nil {
			return ch, err
		}
		if !moved {
			continue
		}
		result.AffectedCharacters = append(result.AffectedCharacters, next)
	}
	for _, mon := range nearbyMonsters {
		if mon == nil || !mon.Alive {
			continue
		}
		if ch.Level <= mon.Level {
			continue
		}
		threshold := 6 + int(state.Level)*3 + (ch.Level - mon.Level)
		if threshold < 0 {
			threshold = 0
		}
		if w.rand.Intn(20) >= threshold {
			continue
		}
		action, moved := w.pushMonsterAwayLocked(ch, mon, push, mp, occupied)
		if !moved {
			continue
		}
		result.MonsterActions = append(result.MonsterActions, action)
	}
	result.Character = ch
	return ch, nil
}
