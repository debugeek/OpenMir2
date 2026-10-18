package world

import (
	"fmt"
	"time"

	"openmir2/internal/data"
	"openmir2/internal/storage"
	"openmir2/internal/world/core"
)

func (w *World) visibleSpellAreaTargetsLocked(observerID string, players []storage.Character, mapID string, x, y, radius int) []spellAreaTarget {
	targets := w.hostileSpellAreaTargetsLocked(players, mapID, x, y, radius)
	if _, isMonsterObserver := w.monsters[observerID]; !isMonsterObserver {
		filtered := targets[:0]
		for _, target := range targets {
			if target.Character != nil && target.Character.ID == observerID {
				continue
			}
			filtered = append(filtered, target)
		}
		return filtered
	}
	byKey := make(map[string]spellAreaTarget, len(targets))
	for _, target := range targets {
		if target.Character != nil {
			byKey[characterVisibleActorKey(target.Character.ID)] = target
		} else if target.Monster != nil {
			byKey[monsterVisibleActorKey(target.Monster.ID)] = target
		} else if target.Trainer != nil {
			byKey["npc:"+target.Trainer.ID] = target
		}
	}
	ordered := make([]spellAreaTarget, 0, len(targets))
	seen := make(map[string]struct{}, len(targets))
	for _, key := range w.visibleActors[observerID] {
		target, ok := byKey[key]
		if !ok {
			continue
		}
		ordered = append(ordered, target)
		seen[key] = struct{}{}
	}
	return ordered
}

func (w *World) occupiedActorsLocked(players []storage.Character) map[monsterPosition]string {
	occupied := make(map[monsterPosition]string, len(players)+len(w.monsters)+len(w.data.NPCs.Entities))
	for _, ch := range players {
		if ch.ID == "" || w.characterDeadLocked(ch) || ch.Ghost || ch.MapID == "" {
			continue
		}
		occupied[monsterPosition{MapID: ch.MapID, X: ch.X, Y: ch.Y}] = ch.ID
	}
	for _, mon := range w.monsters {
		if mon == nil || mon.ID == "" || !mon.Alive || mon.Ghost || mon.FixedHideMode || mon.MapID == "" {
			continue
		}
		occupied[monsterPosition{MapID: mon.MapID, X: mon.X, Y: mon.Y}] = mon.ID
	}
	for _, entity := range w.npcEntitiesInMapOrdered("", true) {
		if entity.Hidden || entity.MapID == "" {
			continue
		}
		position := monsterPosition{MapID: entity.MapID, X: entity.X, Y: entity.Y}
		if _, exists := occupied[position]; !exists {
			occupied[position] = entity.ID
		}
	}
	return occupied
}

func (w *World) occupiedMovingObjectsLocked(players []storage.Character) map[monsterPosition]string {
	occupied := make(map[monsterPosition]string, len(players)+len(w.monsters))
	for _, ch := range players {
		if ch.ID == "" || w.characterDeadLocked(ch) || ch.Ghost || ch.ObserverMode || ch.MapID == "" {
			continue
		}
		occupied[monsterPosition{MapID: ch.MapID, X: ch.X, Y: ch.Y}] = ch.ID
	}
	for _, mon := range w.monsters {
		if mon == nil || mon.ID == "" || !mon.Alive || mon.Ghost || mon.FixedHideMode || mon.MapID == "" {
			continue
		}
		occupied[monsterPosition{MapID: mon.MapID, X: mon.X, Y: mon.Y}] = mon.ID
	}
	return occupied
}

func characterValues(characters map[string]storage.Character) []storage.Character {
	values := make([]storage.Character, 0, len(characters))
	for _, character := range characters {
		values = append(values, character)
	}
	return values
}

func (w *World) movingObjectCountAtLocked(players []storage.Character, mapID string, x, y int) int {
	count := 0
	for _, ch := range players {
		if ch.ID == "" || ch.MapID != mapID || ch.X != x || ch.Y != y || w.characterDeadLocked(ch) || ch.Ghost || ch.ObserverMode {
			continue
		}
		count++
	}
	for _, mon := range w.monsters {
		if mon == nil || mon.MapID != mapID || mon.X != x || mon.Y != y || !mon.Alive || mon.Ghost || mon.FixedHideMode {
			continue
		}
		count++
	}
	return count
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
	var selected storage.Character
	for _, target := range players {
		if target.ID == "" || target.ID == exceptID || target.MapID != mapID || w.characterDeadLocked(target) || target.Ghost || target.ObserverMode {
			continue
		}
		if target.X == x && target.Y == y {
			if selected.ID == "" || characterObjectOrderBefore(target, selected) {
				selected = target
			}
		}
	}
	return selected, selected.ID != ""
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
	if target.ID == "" || target.MapID != caster.MapID || w.characterDeadLocked(target) || target.Ghost || target.ID == caster.ID {
		return target, nil, false, nil
	}
	dir := direction(caster.X, caster.Y, target.X, target.Y)
	return w.pushCharacterInDirectionLocked(target, push, dir, mp, occupied)
}

func (w *World) pushCharacterInDirectionLocked(target storage.Character, push, dir int, mp data.StdMap, occupied map[monsterPosition]string) (storage.Character, []CharacterPush, bool, error) {
	if dir < 0 || dir >= len(dirOffsets) {
		return target, nil, false, nil
	}
	off := dirOffsets[dir]
	oldDir := target.Dir
	backDir := (dir + 4) % 8
	target.Dir = dir
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
		pushes = append(pushes, CharacterPush{Character: target, Dir: backDir})
	}
	if !moved {
		target.Dir = oldDir
		return target, nil, false, nil
	}
	target.Dir = backDir
	return target, pushes, true, nil
}

func (w *World) pushMonsterAwayLocked(caster storage.Character, mon *Monster, push int, mp data.StdMap, occupied map[monsterPosition]string) (MonsterAction, []MonsterAction, bool) {
	if mon == nil || !mon.Alive || mon.Ghost || mon.MapID != caster.MapID {
		return MonsterAction{}, nil, false
	}
	dir := direction(caster.X, caster.Y, mon.X, mon.Y)
	return w.pushMonsterInDirectionLocked(mon, push, dir, mp, occupied)
}

func (w *World) pushMonsterInDirectionLocked(mon *Monster, push, dir int, mp data.StdMap, occupied map[monsterPosition]string) (MonsterAction, []MonsterAction, bool) {
	if mon == nil || !mon.Alive || mon.Ghost {
		return MonsterAction{}, nil, false
	}
	if dir < 0 || dir >= len(dirOffsets) {
		return MonsterAction{}, nil, false
	}
	off := dirOffsets[dir]
	moved := false
	actions := make([]MonsterAction, 0, push)
	oldDir := mon.Dir
	backDir := (dir + 4) % 8
	mon.Dir = dir
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
		w.occupyMonsterLocked(mon)
		occupied[monsterPosition{MapID: mon.MapID, X: mon.X, Y: mon.Y}] = mon.ID
		moved = true
		if mon.Race >= 50 {
			if mon.LastWalkAt.IsZero() {
				mon.LastWalkAt = time.Unix(0, 0)
			}
			mon.LastWalkAt = mon.LastWalkAt.Add(800 * time.Millisecond)
		}
		action := w.monsterActionLocked(mon, MonsterActionPush)
		action.Dir = backDir
		actions = append(actions, action)
	}
	if !moved {
		mon.Dir = oldDir
		return MonsterAction{}, nil, false
	}
	mon.Dir = backDir
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
			selfDamagePower = 0
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
				result.ChargeActionSucceeded = true
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
			if hit.AttackerNameColorChanged {
				ch = w.mergeStoredCharacterPKFlagLocked(ch)
				result.NameColorCharacters = append(result.NameColorCharacters, ch)
			}
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
		if damage > 0 && lastMonster.Undead > 0 {
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
		nextX := ch.X + dirOffsets[dir][0]
		nextY := ch.Y + dirOffsets[dir][1]
		rush := SpellRush{Character: ch, Dir: dir, X: nextX, Y: nextY, Kung: true}
		result.Rushes = append(result.Rushes, rush)
		result.OrderedEvents = append(result.OrderedEvents, SpellEvent{Kind: SpellEventRush, Rush: rush})
	}
	if selfDamagePower > 0 {
		damage := w.rand.Intn(selfDamagePower*10) + (remainingPower+1)*3
		damage = w.characterPhysicalDamageAfterDefenseLocked(&ch, damage)
		magicShield := w.characterHasMagicShieldLocked(ch) && !w.characterMagicShieldBlockedLocked(ch)
		ch, damage, durability, deletedItems, featureChanged := w.applyCharacterStruckLocked(ch, damage)
		hpDamage := w.applyCharacterMagicShieldStateLocked(&ch, damage, magicShield)
		change := core.ApplyVitalDelta(ch, -hpDamage, 0)
		ch = change.Character
		if change.Dead {
			w.deferCharacterDeathLocked(ch)
		}
		if hpDamage > 0 {
			ch.HealthTick = 0
			ch.SpellTick = 0
			result.CharacterHits = append(result.CharacterHits, CharacterHit{Character: ch, Damage: damage, Durability: durability, DeletedItems: deletedItems, FeatureChanged: featureChanged, Dead: change.Dead, DeathDeferred: change.Dead})
			result.OrderedEvents = append(result.OrderedEvents, SpellEvent{Kind: SpellEventCharacterHit, Character: ch, CharacterHit: result.CharacterHits[len(result.CharacterHits)-1]})
		}
	}
	result.Character = ch
	return ch, nil
}

func (w *World) chargeCharacterWithDamageLocked(caster storage.Character, target storage.Character, damage int) (storage.Character, CharacterHit, error) {
	now := w.monsterActionTimeLocked()
	canMarkCasterPK := w.isProperCharacterTargetLocked(caster, target)
	canSetCasterPK := canMarkCasterPK && w.canSetCharacterPKFlagLocked(target, caster)
	attackerNameColorChanged := false
	damage = w.characterPhysicalDamageAfterDefenseLocked(&target, damage)
	magicShield := w.characterHasMagicShieldLocked(target) && !w.characterMagicShieldBlockedLocked(target)
	var durability []SpellDurability
	var deletedItems []storage.UserItem
	var featureChanged bool
	target, damage, durability, deletedItems, featureChanged = w.applyCharacterStruckLocked(target, damage)
	hpDamage := w.applyCharacterMagicShieldStateLocked(&target, damage, magicShield)
	change := core.ApplyVitalDelta(target, -hpDamage, 0)
	target = change.Character
	if change.Dead {
		w.deferCharacterDeathLocked(target)
	}
	if hpDamage > 0 {
		target.HealthTick = 0
		target.SpellTick = 0
		if canMarkCasterPK {
			w.setCharacterLastHitterAtLocked(&target, caster.ID, now)
			attackerNameColorChanged = canSetCasterPK && !caster.PKFlag
			if canSetCasterPK {
				caster.PKFlag = true
				caster.PKFlagUntil = now.Add(60 * time.Second).UnixNano()
			}
		}
	}
	hit := CharacterHit{Character: target, Damage: damage, Durability: durability, DeletedItems: deletedItems, FeatureChanged: featureChanged, AttackerID: caster.ID, AttackerActor: CharacterActorID(caster), AttackerX: caster.X, AttackerY: caster.Y, AttackerNameColorChanged: attackerNameColorChanged, Dead: change.Dead, DeathDeferred: change.Dead}
	return target, hit, nil
}

func (w *World) canMotaeboCharacterLocked(caster, target storage.Character, magicLevel int) bool {
	if target.StoneMode || caster.Level <= target.Level {
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
	for _, areaTarget := range w.visibleSpellAreaTargetsLocked(ch.ID, playersWithCaster(players, ch), ch.MapID, ch.X, ch.Y, 1) {
		if areaTarget.Character != nil {
			target := *areaTarget.Character
			if target.ID == ch.ID || w.characterDeadLocked(target) || target.Ghost || target.StoneMode || ch.Level <= target.Level {
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
