package world

import (
	"fmt"
	"sort"

	"openmir2/internal/data"
	"openmir2/internal/storage"
	"openmir2/internal/world/core"
)

func (w *World) visibleSpellAreaTargetsLocked(players []storage.Character, mapID string, x, y, radius int) []spellAreaTarget {
	targets := w.spellAreaTargetsLocked(players, mapID, x, y, radius)
	sort.SliceStable(targets, func(i, j int) bool {
		aOrder, bOrder := targets[i].ObjectOrder(), targets[j].ObjectOrder()
		if aOrder != bOrder {
			if aOrder == 0 {
				return false
			}
			if bOrder == 0 {
				return true
			}
			return aOrder < bOrder
		}
		_, _, aID := targets[i].CharacterOrMonster()
		_, _, bID := targets[j].CharacterOrMonster()
		return aID < bID
	})
	return targets
}

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

func (w *World) pushCharacterAwayLocked(caster storage.Character, target storage.Character, push int, mp data.StdMap, occupied map[monsterPosition]string) (storage.Character, []CharacterPush, bool, error) {
	if target.ID == "" || target.MapID != caster.MapID || target.HP <= 0 || target.ID == caster.ID {
		return target, nil, false, nil
	}
	dir := direction(caster.X, caster.Y, target.X, target.Y)
	if dir < 0 || dir >= len(dirOffsets) {
		return target, nil, false, nil
	}
	off := dirOffsets[dir]
	moved := false
	pushes := make([]CharacterPush, 0, push)
	for i := 0; i < push; i++ {
		nextX := target.X + off[0]
		nextY := target.Y + off[1]
		if !w.canOccupyLocked(mp, occupied, target.MapID, nextX, nextY, target.ID) {
			break
		}
		delete(occupied, monsterPosition{MapID: target.MapID, X: target.X, Y: target.Y})
		target.X = nextX
		target.Y = nextY
		w.refreshCharacterObjectOrderLocked(&target)
		occupied[monsterPosition{MapID: target.MapID, X: target.X, Y: target.Y}] = target.ID
		moved = true
		target.Dir = (dir + 4) % 8
		pushes = append(pushes, CharacterPush{Character: target, Dir: target.Dir})
	}
	if !moved {
		return target, nil, false, nil
	}
	target.Dir = (dir + 4) % 8
	if err := w.store.SaveCharacter(target); err != nil {
		return target, pushes, false, err
	}
	return target, pushes, true, nil
}

func (w *World) pushMonsterAwayLocked(caster storage.Character, mon *Monster, push int, mp data.StdMap, occupied map[monsterPosition]string) (MonsterAction, []MonsterAction, bool) {
	if mon == nil || !mon.Alive || mon.MapID != caster.MapID {
		return MonsterAction{}, nil, false
	}
	dir := direction(caster.X, caster.Y, mon.X, mon.Y)
	if dir < 0 || dir >= len(dirOffsets) {
		return MonsterAction{}, nil, false
	}
	off := dirOffsets[dir]
	moved := false
	actions := make([]MonsterAction, 0, push)
	for i := 0; i < push; i++ {
		nextX := mon.X + off[0]
		nextY := mon.Y + off[1]
		if !w.canOccupyLocked(mp, occupied, mon.MapID, nextX, nextY, mon.ID) {
			break
		}
		w.vacateMonsterLocked(mon)
		mon.X = nextX
		mon.Y = nextY
		if w.nextObjectOrder > 0 {
			mon.ObjectOrder = w.nextObjectOrder
			w.nextObjectOrder++
		}
		mon.Dir = (dir + 4) % 8
		w.occupyMonsterLocked(mon)
		occupied[monsterPosition{MapID: mon.MapID, X: mon.X, Y: mon.Y}] = mon.ID
		moved = true
		actions = append(actions, w.monsterActionLocked(mon, MonsterActionPush))
	}
	if !moved {
		return MonsterAction{}, nil, false
	}
	return actions[len(actions)-1], actions, true
}

func (w *World) castChargeDirectionLocked(result *SkillCastResult, ch storage.Character, state storage.SkillState, players []storage.Character, dir int) (storage.Character, error) {
	mp, ok := w.data.Maps[ch.MapID]
	if !ok {
		return ch, fmt.Errorf("map %s not found", ch.MapID)
	}
	if dir < 0 || dir >= len(dirOffsets) {
		return ch, fmt.Errorf("no valid target")
	}
	push := maxInt(2, int(state.Level)+1)
	remainingPower := int(state.Level) + 1
	selfDamagePower := remainingPower
	occupied := w.occupiedActorsLocked(players)
	var lastCharacter storage.Character
	var lastMonster *Monster
	front := monsterPosition{MapID: ch.MapID, X: ch.X + dirOffsets[dir][0], Y: ch.Y + dirOffsets[dir][1]}
	frontOccupied := false
	if occupant, ok := occupied[front]; ok && occupant != ch.ID {
		frontOccupied = true
	}
	kung := false
	if frontOccupied {
		for step := 0; step <= push; step++ {
			off := dirOffsets[dir]
			nextX := ch.X + off[0]
			nextY := ch.Y + off[1]
			occupant, ok := occupied[monsterPosition{MapID: ch.MapID, X: nextX, Y: nextY}]
			if !ok || occupant == ch.ID {
				break
			}
			if state.Level >= 3 {
				pushX := ch.X + dirOffsets[dir][0]*2
				pushY := ch.Y + dirOffsets[dir][1]*2
				if pushed, ok := occupied[monsterPosition{MapID: ch.MapID, X: pushX, Y: pushY}]; ok && pushed != ch.ID {
					if preTarget, ok := w.findCharacterByIDLocked(players, pushed); ok {
						if w.canMotaeboCharacterLocked(ch, preTarget, int(state.Level)) {
							if next, pushes, moved, err := w.pushCharacterAwayLocked(ch, preTarget, 1, mp, occupied); err != nil {
								return ch, err
							} else if moved {
								for _, push := range pushes {
									result.OrderedEvents = append(result.OrderedEvents, SpellEvent{Kind: SpellEventCharacterPush, CharacterPush: push})
								}
								for i := range players {
									if players[i].ID == next.ID {
										players[i] = next
										break
									}
								}
							}
						}
					} else if preMonster, ok := w.monsterByIDLocked(pushed); ok && w.canMotaeboMonsterLocked(ch, players, preMonster, int(state.Level)) {
						action, actions, moved := w.pushMonsterAwayLocked(ch, preMonster, 1, mp, occupied)
						if moved {
							appendOrUpdateMonsterAction(result, action)
							for _, action := range actions {
								result.OrderedEvents = append(result.OrderedEvents, SpellEvent{Kind: SpellEventMonsterAction, MonsterAction: action})
							}
						}
					}
				}
			}
			target, found := w.findCharacterByIDLocked(players, occupant)
			if found {
				if !w.canMotaeboCharacterLocked(ch, target, int(state.Level)) {
					kung = true
					break
				}
				selfDamagePower = 0
				lastCharacter = target
				lastMonster = nil
				next, pushes, moved, err := w.pushCharacterAwayLocked(ch, target, 1, mp, occupied)
				if err != nil {
					return ch, err
				}
				if !moved {
					kung = true
					break
				}
				for _, push := range pushes {
					result.OrderedEvents = append(result.OrderedEvents, SpellEvent{Kind: SpellEventCharacterPush, CharacterPush: push})
				}
				lastCharacter = next
				for i := range players {
					if players[i].ID == next.ID {
						players[i] = next
						break
					}
				}
			} else if mon, found := w.monsterByIDLocked(occupant); found {
				if !w.canMotaeboMonsterLocked(ch, players, mon, int(state.Level)) {
					kung = true
					break
				}
				selfDamagePower = 0
				lastCharacter = storage.Character{}
				lastMonster = mon
				action, actions, moved := w.pushMonsterAwayLocked(ch, mon, 1, mp, occupied)
				if !moved {
					kung = true
					break
				}
				appendOrUpdateMonsterAction(result, action)
				for _, action := range actions {
					result.OrderedEvents = append(result.OrderedEvents, SpellEvent{Kind: SpellEventMonsterAction, MonsterAction: action})
				}
			} else {
				kung = true
				break
			}
			if w.canOccupyLocked(mp, occupied, ch.MapID, nextX, nextY, ch.ID) {
				ch.X = nextX
				ch.Y = nextY
				w.refreshCharacterObjectOrderLocked(&ch)
				ch.Dir = dir
				rush := SpellRush{Character: ch, Dir: dir, X: nextX, Y: nextY}
				result.Rushes = append(result.Rushes, rush)
				result.OrderedEvents = append(result.OrderedEvents, SpellEvent{Kind: SpellEventRush, Rush: rush})
			}
			remainingPower = maxInt(0, remainingPower-1)
		}
	} else {
		for step := 0; step <= push; step++ {
			off := dirOffsets[dir]
			nextX := ch.X + off[0]
			nextY := ch.Y + off[1]
			if !w.canOccupyLocked(mp, occupied, ch.MapID, nextX, nextY, ch.ID) {
				if mp.Walkable(nextX, nextY) {
					selfDamagePower = 0
				} else {
					kung = true
				}
				break
			}
			ch.X = nextX
			ch.Y = nextY
			w.refreshCharacterObjectOrderLocked(&ch)
			ch.Dir = dir
			rush := SpellRush{Character: ch, Dir: dir, X: nextX, Y: nextY}
			result.Rushes = append(result.Rushes, rush)
			result.OrderedEvents = append(result.OrderedEvents, SpellEvent{Kind: SpellEventRush, Rush: rush})
			selfDamagePower = maxInt(0, selfDamagePower-1)
		}
	}
	if lastCharacter.ID != "" {
		damage := w.rand.Intn((remainingPower+1)*10) + ((remainingPower + 1) * 10)
		next, hit, err := w.chargeCharacterWithDamageLocked(ch, lastCharacter, damage)
		if err != nil {
			return ch, err
		}
		if hit.Damage > 0 {
			if len(result.OrderedEvents) == 0 {
				appendOrUpdateAffectedCharacter(result, next)
			}
			result.CharacterHits = append(result.CharacterHits, hit)
			result.OrderedEvents = append(result.OrderedEvents, SpellEvent{Kind: SpellEventCharacterHit, Character: ch, CharacterHit: hit})
		}
	}
	if lastMonster != nil {
		damage := w.rand.Intn((remainingPower+1)*10) + ((remainingPower + 1) * 10)
		damage = w.monsterPhysicalDamageAfterDefenseLocked(lastMonster, damage)
		if lastMonster.Undead > 0 && damage > 0 {
			damage += w.combatStatsLocked(ch).Undead
		}
		hit, err := w.attackMonsterWithDamageModeLocked(ch, lastMonster, damage, false, players...)
		if err != nil {
			return ch, err
		}
		if hit.Damage > 0 {
			result.MonsterHits = append(result.MonsterHits, hit)
			result.OrderedEvents = append(result.OrderedEvents, SpellEvent{Kind: SpellEventMonsterHit, MonsterHit: hit})
		}
	}
	if kung {
		rush := SpellRush{Character: ch, Dir: dir, X: ch.X, Y: ch.Y, Kung: true}
		result.Rushes = append(result.Rushes, rush)
		result.OrderedEvents = append(result.OrderedEvents, SpellEvent{Kind: SpellEventRush, Rush: rush})
	}
	if selfDamagePower > 0 {
		damage := w.rand.Intn(selfDamagePower*10) + (selfDamagePower+1)*3
		damage = w.characterPhysicalDamageAfterDefenseLocked(&ch, damage)
		ch, damage, durability, featureChanged := w.applyCharacterStruckLocked(ch, damage)
		change := core.ApplyVitalDelta(ch, -damage, 0)
		ch = change.Character
		if damage > 0 {
			ch.SpellTick = 0
			result.CharacterHits = append(result.CharacterHits, CharacterHit{Character: ch, Damage: damage, Durability: durability, FeatureChanged: featureChanged, Dead: change.Dead})
			result.OrderedEvents = append(result.OrderedEvents, SpellEvent{Kind: SpellEventCharacterHit, Character: ch, CharacterHit: result.CharacterHits[len(result.CharacterHits)-1]})
		}
		if err := w.store.SaveCharacter(ch); err != nil {
			return ch, err
		}
	}
	result.Character = ch
	return ch, nil
}

func (w *World) chargeCharacterWithDamageLocked(caster storage.Character, target storage.Character, damage int) (storage.Character, CharacterHit, error) {
	damage = w.characterPhysicalDamageAfterDefenseLocked(&target, damage)
	var durability []SpellDurability
	var featureChanged bool
	target, damage, durability, featureChanged = w.applyCharacterStruckLocked(target, damage)
	change := core.ApplyVitalDelta(target, -damage, 0)
	target = change.Character
	if damage > 0 {
		target.SpellTick = 0
	}
	hit := CharacterHit{Character: target, Damage: damage, Durability: durability, FeatureChanged: featureChanged, AttackerID: caster.ID, AttackerX: caster.X, AttackerY: caster.Y, Dead: change.Dead}
	return target, hit, w.store.SaveCharacter(target)
}

func (w *World) canMotaeboCharacterLocked(caster, target storage.Character, magicLevel int) bool {
	if caster.Level <= target.Level {
		return false
	}
	threshold := magicLevel*4 + 6 + caster.Level - target.Level
	if w.rand.Intn(20) >= threshold {
		return false
	}
	return w.isProperCharacterTargetLocked(caster, target)
}

func (w *World) canMotaeboMonsterLocked(caster storage.Character, players []storage.Character, target *Monster, magicLevel int) bool {
	if target == nil || target.StoneMode || caster.Level <= target.Level {
		return false
	}
	threshold := magicLevel*4 + 6 + caster.Level - target.Level
	if w.rand.Intn(20) >= threshold {
		return false
	}
	return w.isProperMonsterTargetLocked(caster, players, target)
}

func (w *World) castPushAroundSkillLocked(result *SkillCastResult, ch storage.Character, state storage.SkillState, players []storage.Character) (storage.Character, error) {
	mp, ok := w.data.Maps[ch.MapID]
	if !ok {
		return ch, fmt.Errorf("map %s not found", ch.MapID)
	}
	occupied := w.occupiedActorsLocked(players)
	for _, areaTarget := range w.visibleSpellAreaTargetsLocked(playersWithCaster(players, ch), ch.MapID, ch.X, ch.Y, 1) {
		if areaTarget.Character != nil {
			target := *areaTarget.Character
			if target.ID == ch.ID || target.HP <= 0 || target.StoneMode || ch.Level <= target.Level {
				continue
			}
			threshold := 6 + int(state.Level)*3 + (ch.Level - target.Level)
			if w.rand.Intn(20) >= threshold {
				continue
			}
			if !w.isProperCharacterTargetLocked(ch, target) {
				continue
			}
			result.PushTargets++
			push := maxInt(1, int(state.Level)) + w.rand.Intn(2)
			_, pushes, moved, err := w.pushCharacterAwayLocked(ch, target, push, mp, occupied)
			if err != nil {
				return ch, err
			}
			if !moved {
				continue
			}
			for _, characterPush := range pushes {
				result.CharacterPushes = append(result.CharacterPushes, characterPush)
			}
			finalTarget := pushes[len(pushes)-1].Character
			result.AffectedCharacters = append(result.AffectedCharacters, finalTarget)
			result.AffectedTargets = append(result.AffectedTargets, SpellAffectedTarget{Character: &finalTarget})
			continue
		}
		mon := areaTarget.Monster
		if mon == nil || !mon.Alive || mon.StoneMode || ch.Level <= mon.Level {
			continue
		}
		threshold := 6 + int(state.Level)*3 + (ch.Level - mon.Level)
		if w.rand.Intn(20) >= threshold {
			continue
		}
		if !w.isProperMonsterTargetLocked(ch, players, mon) {
			continue
		}
		result.PushTargets++
		push := maxInt(1, int(state.Level)) + w.rand.Intn(2)
		action, actions, moved := w.pushMonsterAwayLocked(ch, mon, push, mp, occupied)
		if !moved {
			continue
		}
		action.Kind = MonsterActionPush
		result.MonsterActions = append(result.MonsterActions, action)
		result.MonsterPushes = append(result.MonsterPushes, actions...)
		finalTarget := Monster(*mon)
		result.AffectedMonsters = append(result.AffectedMonsters, finalTarget)
		result.AffectedTargets = append(result.AffectedTargets, SpellAffectedTarget{Monster: &finalTarget})
	}
	result.Character = ch
	return ch, nil
}
