package world

import (
	"fmt"
	"time"

	"openmir2/internal/data"
	"openmir2/internal/protocol/mir176"
	"openmir2/internal/storage"
)

// dirOffsets maps a facing direction (0-7, clockwise from north) to its tile delta.
var dirOffsets = [8][2]int{
	{0, -1}, {1, -1}, {1, 0}, {1, 1}, {0, 1}, {-1, 1}, {-1, 0}, {-1, -1},
}

func (w *World) Move(ch storage.Character, x, y int) (storage.Character, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.stepLocked(ch, x, y, 1)
}

func (w *World) Turn(ch storage.Character, x, y, dir int) (storage.Character, error) {
	result, err := w.TurnWithEvents(ch, x, y, dir)
	return result.Character, err
}

func (w *World) TurnWithEvents(ch storage.Character, x, y, dir int) (MovementResult, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if x != ch.X || y != ch.Y {
		return MovementResult{Character: ch}, fmt.Errorf("turn coordinates do not match current position")
	}
	if _, ok := w.data.Maps[ch.MapID]; !ok {
		return MovementResult{Character: ch}, fmt.Errorf("map %s not found", ch.MapID)
	}
	ch.Dir = dir
	result := MovementResult{Character: ch}
	if hit, ok := w.movementGroundEventHitLocked(ch); ok {
		ch = hit.Character
		result.Character = ch
		result.CharacterHits = append(result.CharacterHits, hit)
	}
	mp := w.data.Maps[ch.MapID]
	for i := range mp.Connections {
		connection := &mp.Connections[i]
		if connection.FromX != ch.X || connection.FromY != ch.Y {
			continue
		}
		result.SuppressTurnBroadcast = true
		targetMap, ok := w.data.Maps[connection.ToMap]
		if !ok || !targetMap.Walkable(connection.ToX, connection.ToY) {
			return result, fmt.Errorf("map gate destination is invalid")
		}
		if targetMap.NeedHole && !w.hasDigOutEventLocked(ch.MapID, ch.X, ch.Y, time.Now()) {
			return result, nil
		}
		if !w.doorsOpenAroundLocked(ch.MapID, ch.X, ch.Y, time.Now()) {
			return result, nil
		}
		ch.MapID = targetMap.ID
		ch.X = connection.ToX
		ch.Y = connection.ToY
		w.refreshCharacterObjectOrderLocked(&ch)
		ch.MapMoveAt = time.Now().UnixNano()
		result.Character = ch
		return result, nil
	}
	return result, nil
}

func (w *World) Walk(ch storage.Character, x, y, dir int, blockers ...storage.Character) (storage.Character, error) {
	result, err := w.WalkWithEvents(ch, x, y, dir, blockers...)
	return result.Character, err
}

// WalkWithEvents applies one walk step and returns any ground-event impacts
// generated at the resulting tile before the caller emits the move result.
func (w *World) WalkWithEvents(ch storage.Character, x, y, dir int, blockers ...storage.Character) (MovementResult, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := validDir(dir); err != nil {
		return MovementResult{Character: ch}, err
	}
	ch.Dir = dir
	return w.directionalStepLocked(ch, x, y, dir, 1, blockers)
}

func (w *World) Run(ch storage.Character, x, y, dir int, blockers ...storage.Character) (storage.Character, error) {
	result, err := w.RunWithEvents(ch, x, y, dir, blockers...)
	return result.Character, err
}

// RunWithEvents applies one run action and returns any ground-event impacts
// generated at the resulting tile before the caller emits the move result.
func (w *World) RunWithEvents(ch storage.Character, x, y, dir int, blockers ...storage.Character) (MovementResult, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := validDir(dir); err != nil {
		return MovementResult{Character: ch}, err
	}
	ch.Dir = dir
	return w.directionalStepLocked(ch, x, y, dir, 2, blockers)
}

type MovementResult struct {
	CharacterHits         []CharacterHit
	Character             storage.Character
	SuppressTurnBroadcast bool
	SuppressMoveBroadcast bool
}

// Hit resolves a melee swing (CM_HIT and its variants) in the character's
// facing direction, attacking whatever living monster occupies that tile.
// A swing that connects with nothing is not an error, it just carries no
// AttackResult.MonsterID.
func (w *World) Hit(ch storage.Character, x, y, dir int, blockers ...storage.Character) (AttackResult, error) {
	return w.HitWithIdent(ch, x, y, dir, mir176.CMHit, blockers...)
}

func (w *World) HitWithIdent(ch storage.Character, x, y, dir int, attackIdent uint16, blockers ...storage.Character) (AttackResult, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := validDir(dir); err != nil {
		return AttackResult{}, err
	}
	if x != ch.X || y != ch.Y {
		return AttackResult{}, fmt.Errorf("hit coordinates do not match current position")
	}
	now := time.Now()
	fireHitActive := attackIdent == mir176.CMFireHit && ch.FireHitArmed
	powerHitActive := attackIdent == mir176.CMPowerHit && ch.PowerHitArmed
	if attackIdent == mir176.CMFireHit && !fireHitActive || attackIdent == mir176.CMPowerHit && !powerHitActive {
		attackIdent = mir176.CMHit
	}
	ch.Dir = dir
	if attackIdent == mir176.CMHeavyHit {
		if mineResult, handled, err := w.mineWithHeavyHitLocked(ch, ch.Dir, blockers...); handled {
			applyMiningRecoveryDelay(&mineResult.Character)
			if err != nil {
				return AttackResult{}, err
			}
			return mineResult, nil
		}
	}
	result := AttackResult{Character: ch}
	finishAttack := func(out AttackResult) AttackResult {
		applyAttackRecoveryDelay(&out.Character)
		return out
	}
	consumeSpecialHit := func(hit *AttackResult) {
		if !fireHitActive && !powerHitActive {
			return
		}
		if fireHitActive {
			ch.FireHitArmed = false
			ch.FireHitLatestAt = now.UnixNano()
		}
		if powerHitActive {
			ch.PowerHitArmed = false
		}
		result.Character = ch
		if hit != nil {
			hit.Character.FireHitArmed = false
			hit.Character.FireHitLatestAt = ch.FireHitLatestAt
			hit.Character.PowerHitArmed = false
		}
	}
	appendMonsterHit := func(hit AttackResult) {
		if len(result.MonsterHits) == 0 {
			result.MonsterID = hit.MonsterID
			result.Damage = hit.Damage
			result.MonsterHP = hit.MonsterHP
			result.MonsterMaxHP = hit.MonsterMaxHP
			result.MonsterRaceImg = hit.MonsterRaceImg
			result.MonsterWeapon = hit.MonsterWeapon
			result.MonsterAppr = hit.MonsterAppr
			result.MonsterX = hit.MonsterX
			result.MonsterY = hit.MonsterY
			result.MonsterDir = hit.MonsterDir
			result.MonsterStatus = hit.MonsterStatus
		}
		result.MonsterHits = append(result.MonsterHits, hit)
		result.Experience += hit.Experience
		result.CurrentExp = hit.CurrentExp
		result.LevelUp = result.LevelUp || hit.LevelUp
		result.SkillChanged = result.SkillChanged || hit.SkillChanged
		result.SkillExp = result.SkillExp || hit.SkillExp
		result.SkillLevelUp = result.SkillLevelUp || hit.SkillLevelUp
		result.SkillExperiences = append(result.SkillExperiences, hit.SkillExperiences...)
		if result.SkillMagicID == 0 {
			result.SkillMagicID = hit.SkillMagicID
			result.SkillLevel = hit.SkillLevel
			result.SkillTrain = hit.SkillTrain
			result.SkillExpDelay = hit.SkillExpDelay
		}
		ch = hit.Character
		result.Character = ch
	}
	trainMainAttack := func() error {
		if err := w.applyMeleeTrainingLocked(&result, attackIdent); err != nil {
			return err
		}
		ch = result.Character
		return nil
	}
	appendCharacterHit := func(hit CharacterHit) {
		result.CharacterHits = append(result.CharacterHits, hit)
		result.Character = ch
	}
	weaponDamageApplied := false
	applyAttackerWeaponDamage := func() {
		if weaponDamageApplied {
			return
		}
		durability, deleted, featureChanged := w.applyWeaponDamageLocked(&ch)
		result.Durability = append(result.Durability, durability...)
		result.DeletedItems = append(result.DeletedItems, deleted...)
		result.FeatureChanged = result.FeatureChanged || featureChanged
		weaponDamageApplied = true
	}
	trainerHit := false
	applyTrainerWeaponDamage := func() {
		if !trainerHit || weaponDamageApplied {
			return
		}
		applyAttackerWeaponDamage()
	}
	front := [2]int{ch.X + dirOffsets[dir][0], ch.Y + dirOffsets[dir][1]}
	frontTarget := w.movingObjectAtPointLocked(blockers, ch.MapID, front[0], front[1])
	if frontTarget.Monster != nil || frontTarget.Character != nil || frontTarget.Trainer != nil {
		if weapon, ok := ch.EquippedItems[SlotWeapon]; ok && weapon.Desc[10] != 0 {
			w.resolveWeaponUpgradeLocked(&ch, &result)
		}
	}
	baseSecondaryDamage := 0
	if attackIdent == mir176.CMLongHit || attackIdent == mir176.CMWideHit {
		baseSecondaryDamage = w.characterAttackDamageLocked(ch, nil, mir176.CMHit)
	}
	secondaryDamage := 0
	if attackIdent == mir176.CMLongHit {
		if state, _, ok := ch.Skills.Get("刺杀剑术"); ok {
			if _, ok := w.data.Skills["刺杀剑术"]; ok {
				secondaryDamage = referenceRound(float64(baseSecondaryDamage) / float64(spellTrainLevel+2) * float64(state.Level+2) * float64(w.swordLongPowerRateLocked()) / 100)
			}
		}
	}
	if attackIdent == mir176.CMWideHit {
		if state, _, ok := ch.Skills.Get("半月弯刀"); ok {
			if _, ok := w.data.Skills["半月弯刀"]; ok {
				secondaryDamage = referenceRound(float64(baseSecondaryDamage) / float64(spellTrainLevel+10) * float64(state.Level+2))
			}
		}
	}
	points := w.hitPointsForAttackLocked(ch.X, ch.Y, dir, attackIdent)
	for _, point := range points {
		if (attackIdent == mir176.CMLongHit || attackIdent == mir176.CMWideHit) && secondaryDamage <= 0 {
			continue
		}
		areaTarget := w.movingObjectAtPointLocked(blockers, ch.MapID, point[0], point[1])
		if areaTarget.Trainer != nil && w.canPlayerTargetTrainerLocked(*areaTarget.Trainer) {
			damage := secondaryDamage
			if damage <= 0 {
				damage = w.characterAttackPowerLocked(ch, mir176.CMHit)
			}
			w.recordTrainerStrikeLocked(&result, *areaTarget.Trainer, damage, now)
			appendTrainerStruckHit(&result, *areaTarget.Trainer, damage, ch.ID, false)
			appendNPCCombatEvent(&result.NPCCombatEvents, result.NPCSpeeches[len(result.NPCSpeeches)-1], result.NPCStruckHits[len(result.NPCStruckHits)-1])
			trainerHit = true
			result.Character = ch
			consumeSpecialHit(nil)
			if attackIdent != mir176.CMLongHit && attackIdent != mir176.CMWideHit {
				applyTrainerWeaponDamage()
				return finishAttack(result), nil
			}
			continue
		}
		if mon := areaTarget.Monster; mon != nil {
			if !w.isProperMonsterTargetLocked(ch, blockers, mon) {
				continue
			}
			if attackIdent == mir176.CMLongHit || attackIdent == mir176.CMWideHit {
				if secondaryDamage <= 0 {
					continue
				}
				hit, err := w.attackMonsterSecondaryDamageLocked(ch, mon, secondaryDamage, blockers...)
				if err != nil {
					return AttackResult{}, err
				}
				setCharacterTargetLocked(&hit.Character, mon.ID, now)
				ch = hit.Character
				result.Character = ch
				if hit.Connected && hit.Damage > 0 {
					appendMonsterHit(hit)
				}
				continue
			}
			hit, err := w.attackLocked(ch, mon, attackIdent, blockers...)
			if err != nil {
				return AttackResult{}, err
			}
			if hit.Damage > 0 {
				setCharacterTargetLocked(&hit.Character, mon.ID, now)
			}
			consumeSpecialHit(&hit)
			return finishAttack(hit), nil
		}
		if areaTarget.Character != nil {
			target := *areaTarget.Character
			if !w.isProperCharacterTargetLocked(ch, target) {
				continue
			}
			damage := w.characterHitDamageForAttackLocked(ch, attackIdent)
			if attackIdent == mir176.CMLongHit || attackIdent == mir176.CMWideHit {
				if secondaryDamage <= 0 {
					continue
				}
				damage = secondaryDamage
				_, hit, err := w.attackCharacterSecondaryDamageLocked(ch, target, damage)
				if err != nil {
					return AttackResult{}, err
				}
				setCharacterTargetLocked(&ch, target.ID, now)
				result.Character = ch
				hit.ImpactDelay = 500 * time.Millisecond
				if hit.Connected {
					ch = w.mergeStoredCharacterPKFlagLocked(ch)
					appendCharacterHit(hit)
				}
				continue
			}
			_, hit, err := w.attackCharacterWithDamageLocked(ch, target, damage)
			if err != nil {
				return AttackResult{}, err
			}
			if hit.Connected || hit.WeaponDamageEligible {
				if hit.WeaponDamageEligible {
					applyAttackerWeaponDamage()
				}
				result.Character = ch
			}
			if hit.Connected {
				ch = w.mergeStoredCharacterPKFlagLocked(ch)
				setCharacterTargetLocked(&ch, target.ID, now)
				result.Character = ch
			}
			if hit.Connected {
				result.CharacterHits = []CharacterHit{hit}
				if err := trainMainAttack(); err != nil {
					return AttackResult{}, err
				}
				if !hit.WeaponDamageEligible {
					applyAttackerWeaponDamage()
				}
				result.Character = ch
			}
			consumeSpecialHit(nil)
			if fireHitActive {
				result.Character = ch
			}
			return finishAttack(result), nil
		}
	}
	if attackIdent == mir176.CMLongHit || attackIdent == mir176.CMWideHit {
		primary := [2]int{ch.X + dirOffsets[dir][0], ch.Y + dirOffsets[dir][1]}
		areaTarget := w.movingObjectAtPointLocked(blockers, ch.MapID, primary[0], primary[1])
		if areaTarget.Trainer != nil && w.canPlayerTargetTrainerLocked(*areaTarget.Trainer) {
			damage := secondaryDamage
			if damage <= 0 {
				damage = w.characterAttackPowerLocked(ch, mir176.CMHit)
			}
			w.recordTrainerStrikeLocked(&result, *areaTarget.Trainer, damage, now)
			appendTrainerStruckHit(&result, *areaTarget.Trainer, damage, ch.ID, false)
			appendNPCCombatEvent(&result.NPCCombatEvents, result.NPCSpeeches[len(result.NPCSpeeches)-1], result.NPCStruckHits[len(result.NPCStruckHits)-1])
			trainerHit = true
			result.Character = ch
			consumeSpecialHit(nil)
			applyTrainerWeaponDamage()
			return finishAttack(result), nil
		}
		if mon := areaTarget.Monster; mon != nil {
			if w.isProperMonsterTargetLocked(ch, blockers, mon) {
				var hit AttackResult
				var err error
				if secondaryDamage > 0 {
					hit, err = w.attackMonsterWithBaseDamageLocked(ch, mon, baseSecondaryDamage, blockers...)
				} else {
					hit, err = w.attackLockedWithDamageIdent(ch, mon, attackIdent, mir176.CMHit, blockers...)
				}
				if err != nil {
					return AttackResult{}, err
				}
				if hit.MonsterID != "" {
					setCharacterTargetLocked(&hit.Character, mon.ID, now)
					if secondaryDamage > 0 {
						if err := w.applyMeleeTrainingLocked(&hit, attackIdent); err != nil {
							return AttackResult{}, err
						}
					}
					if hit.WeaponDamageEligible {
						durability, deleted, featureChanged := w.applyWeaponDamageLocked(&hit.Character)
						hit.Durability = append(hit.Durability, durability...)
						hit.DeletedItems = append(hit.DeletedItems, deleted...)
						hit.FeatureChanged = hit.FeatureChanged || featureChanged
						weaponDamageApplied = true
					}
					appendMonsterHit(hit)
				}
			}
		} else if areaTarget.Character != nil && w.isProperCharacterTargetLocked(ch, *areaTarget.Character) {
			target := *areaTarget.Character
			primaryDamage := w.characterHitDamageForAttackLocked(ch, mir176.CMHit)
			if secondaryDamage > 0 {
				primaryDamage = baseSecondaryDamage
			}
			_, hit, err := w.attackCharacterWithDamageLocked(ch, target, primaryDamage)
			if err != nil {
				return AttackResult{}, err
			}
			if hit.Connected || hit.WeaponDamageEligible {
				if hit.WeaponDamageEligible {
					applyAttackerWeaponDamage()
				}
				result.Character = ch
			}
			if hit.Connected {
				setCharacterTargetLocked(&ch, target.ID, now)
				appendCharacterHit(hit)
				ch = w.mergeStoredCharacterPKFlagLocked(ch)
				if err := trainMainAttack(); err != nil {
					return AttackResult{}, err
				}
				if !hit.WeaponDamageEligible {
					applyAttackerWeaponDamage()
				}
				result.Character = ch
			}
		}
	}
	consumeSpecialHit(nil)
	applyTrainerWeaponDamage()
	if fireHitActive {
		result.Character = ch
	}
	return finishAttack(result), nil
}

func (w *World) swordLongPowerRateLocked() int {
	if w.gameplay.Combat.SwordLongPowerRate <= 0 {
		return 100
	}
	return w.gameplay.Combat.SwordLongPowerRate
}

func (w *World) resolveWeaponUpgradeLocked(ch *storage.Character, result *AttackResult) {
	weapon, ok := ch.EquippedItems[SlotWeapon]
	if !ok || weapon.Desc[10] == 0 {
		return
	}
	marker := weapon.Desc[10]
	if int(weapon.Desc[0])+int(weapon.Desc[1])+int(weapon.Desc[2]) >= w.gameplay.Item.UpgradeWeaponMaxPoint || marker == 1 {
		result.DeletedItems = append(result.DeletedItems, weapon)
		result.WeaponBroken = true
		delete(ch.EquippedItems, SlotWeapon)
		result.FeatureChanged = true
		result.Character = *ch
		return
	}
	if marker >= 10 && marker <= 13 {
		weapon.Desc[0] = clampByteAdd(weapon.Desc[0], marker-9)
	} else if marker >= 20 && marker <= 23 {
		weapon.Desc[1] = clampByteAdd(weapon.Desc[1], marker-19)
	} else if marker >= 30 && marker <= 33 {
		weapon.Desc[2] = clampByteAdd(weapon.Desc[2], marker-29)
	}
	weapon.Desc[10] = 0
	ch.EquippedItems[SlotWeapon] = weapon
	result.UpdatedItems = append(result.UpdatedItems, weapon)
	result.AbilityChanged = true
	result.SubAbilityChanged = true
	result.Character = *ch
}

func clampByteAdd(a, b byte) byte {
	if int(a)+int(b) > 255 {
		return 255
	}
	return a + b
}

func applyAttackRecoveryDelay(ch *storage.Character) {
	if ch == nil {
		return
	}
	ch.HealthTick -= 30
	ch.SpellTick -= 100
	if ch.SpellTick < 0 {
		ch.SpellTick = 0
	}
	ch.PerHealth -= 2
	ch.PerSpell -= 2
}

func applyMiningRecoveryDelay(ch *storage.Character) {
	if ch == nil {
		return
	}
	ch.HealthTick -= 30
	ch.SpellTick -= 50
	if ch.SpellTick < 0 {
		ch.SpellTick = 0
	}
	ch.PerHealth -= 2
	ch.PerSpell -= 2
}

func (w *World) hitPointsForAttackLocked(x, y, dir int, attackIdent uint16) [][2]int {
	off := dirOffsets[dir]
	switch attackIdent {
	case mir176.CMLongHit:
		return [][2]int{{x + off[0]*2, y + off[1]*2}}
	case mir176.CMWideHit:
		points := make([][2]int, 0, 3)
		for _, rel := range []int{7, 1, 2} {
			fd := (dir + rel) % 8
			foff := dirOffsets[fd]
			points = append(points, [2]int{x + foff[0], y + foff[1]})
		}
		return points
	default:
		return [][2]int{{x + off[0], y + off[1]}}
	}
}

func (w *World) monsterAtExactPointLocked(mapID string, x, y int) *Monster {
	w.ensureMonsterCellRegistryLocked()
	var found *Monster
	for id := range w.monsterCells[monsterPosition{MapID: mapID, X: x, Y: y}] {
		mon := w.monsters[id]
		if mon != nil && mon.Alive && !mon.Ghost && !mon.FixedHideMode && mon.MapID == mapID && mon.X == x && mon.Y == y {
			if found == nil || monsterObjectOrderBefore(mon, found) {
				found = mon
			}
		}
	}
	return found
}

func (w *World) characterAtExactPointLocked(players []storage.Character, mapID string, x, y int) (storage.Character, bool) {
	var selected storage.Character
	for _, target := range players {
		if target.ID == "" || target.MapID != mapID || w.characterDeadLocked(target) || target.Ghost || target.ObserverMode {
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

func characterObjectOrderBefore(candidate, current storage.Character) bool {
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

func (w *World) characterHitDamageForAttackLocked(ch storage.Character, attackIdent uint16) int {
	return w.characterAttackDamageLocked(ch, nil, attackIdent)
}

func (w *World) stepLocked(ch storage.Character, x, y, maxDist int) (storage.Character, error) {
	mp, ok := w.data.Maps[ch.MapID]
	if !ok {
		return ch, fmt.Errorf("map %s not found", ch.MapID)
	}
	if !mp.Walkable(x, y) {
		return ch, fmt.Errorf("target coordinate is blocked")
	}
	if abs(ch.X-x) > maxDist || abs(ch.Y-y) > maxDist {
		return ch, fmt.Errorf("move too far")
	}
	ch.X = x
	ch.Y = y
	w.refreshCharacterObjectOrderLocked(&ch)
	ch.MapMoveAt = time.Now().UnixNano()
	if characterTransparentStatePresent(ch) {
		ch.TransparentUntil = time.Now().Add(time.Second).UnixNano()
	}
	return ch, nil
}

// directionalStepLocked resolves CM_WALK/CM_RUN the way the reference
// server's WalkTo/RunTo do (BaseObject.cs, PlayObject.Base.cs): the
// destination tile is derived from the character's current position plus the
// direction offset repeated `steps` times (1 for walk, 2 for run), not taken
// from the client's reported (x, y) directly. Every tile along the way must
// be walkable, matching RunTo's CanWalkEx checks on both the +1 and +2 tiles
// — a distance-only bound would let a run "jump" a one-tile-wide obstacle by
// only checking the final tile. The requested coordinates select the
// direction before this function runs; they do not prevent the directional
// movement itself.
func (w *World) directionalStepLocked(ch storage.Character, x, y, dir, steps int, blockers []storage.Character) (MovementResult, error) {
	mp, ok := w.data.Maps[ch.MapID]
	if !ok {
		return MovementResult{Character: ch}, fmt.Errorf("map %s not found", ch.MapID)
	}
	off := dirOffsets[dir]
	destX, destY := ch.X, ch.Y
	ignoreRunObjects := steps == 2 && w.gameplay.Movement.DisableHumanRun
	ignoreRunHumans := steps == 2 && (ignoreRunObjects || w.gameplay.Movement.RunHuman || mp.RunHuman)
	ignoreRunMonsters := steps == 2 && (ignoreRunObjects || w.gameplay.Movement.RunMon || mp.RunMon)
	ignoreRunGuards := steps == 2 && (ignoreRunObjects || w.gameplay.Movement.RunGuard)
	ignoreRunNPCs := steps == 2 && (ignoreRunObjects || w.gameplay.Movement.RunNPC)
	for i := 1; i <= steps; i++ {
		destX, destY = ch.X+off[0]*i, ch.Y+off[1]*i
		if !mp.Walkable(destX, destY) || !w.castleMapWalkableLocked(ch.MapID, destX, destY) {
			return MovementResult{Character: ch}, fmt.Errorf("move is blocked")
		}
		warRunBlocks := steps == 2 && !ignoreRunObjects && w.runWarBlocksMovementAtLocked(ch.MapID, destX, destY)
		if w.monsterAtLockedWithRunFlags(ch.MapID, destX, destY, "", ignoreRunMonsters && !warRunBlocks, ignoreRunGuards && !warRunBlocks) {
			return MovementResult{Character: ch}, fmt.Errorf("move is blocked")
		}
		if w.npcAtLockedWithRun(ch.MapID, destX, destY, ignoreRunNPCs && !warRunBlocks) {
			return MovementResult{Character: ch}, fmt.Errorf("move is blocked")
		}
		for _, blocker := range blockers {
			if blocker.ID != "" && blocker.ID != ch.ID && !w.characterDeadLocked(blocker) && !blocker.Ghost && !blocker.ObserverMode && blocker.MapID == ch.MapID && blocker.X == destX && blocker.Y == destY && (!ignoreRunHumans || warRunBlocks) {
				return MovementResult{Character: ch}, fmt.Errorf("move is blocked")
			}
		}
	}
	var gate *data.StdMapConnection
	for i := range mp.Connections {
		connection := &mp.Connections[i]
		if connection.FromX != destX || connection.FromY != destY {
			continue
		}
		gate = connection
		break
	}
	oldMapID := ch.MapID
	oldX, oldY := ch.X, ch.Y
	ch.X = destX
	ch.Y = destY
	result := MovementResult{Character: ch}
	if hit, ok := w.movementGroundEventHitLocked(ch); ok {
		ch = hit.Character
		result.Character = ch
		result.CharacterHits = append(result.CharacterHits, hit)
	}
	if gate != nil {
		now := time.Now()
		canEnter := w.doorsOpenAroundLocked(oldMapID, destX, destY, now)
		targetMap, targetExists := w.data.Maps[gate.ToMap]
		if targetExists && targetMap.NeedHole && !w.hasDigOutEventLocked(oldMapID, destX, destY, now) {
			canEnter = false
		}
		if canEnter {
			if !targetExists || !targetMap.Walkable(gate.ToX, gate.ToY) {
				ch.MapID = oldMapID
				ch.X = oldX
				ch.Y = oldY
				result.Character = ch
				return result, fmt.Errorf("map gate destination is invalid")
			}
			ch.MapID = targetMap.ID
			ch.X = gate.ToX
			ch.Y = gate.ToY
		} else {
			result.SuppressMoveBroadcast = true
		}
	}
	w.refreshCharacterObjectOrderLocked(&ch)
	ch.MapMoveAt = time.Now().UnixNano()
	if steps == 2 {
		ch.HealthTick -= 60
		ch.SpellTick -= 10
		if ch.SpellTick < 0 {
			ch.SpellTick = 0
		}
		ch.PerHealth--
		ch.PerSpell--
	} else {
		ch.HealthTick -= 10
	}
	if characterTransparentStatePresent(ch) {
		ch.TransparentUntil = time.Now().Add(time.Second).UnixNano()
	}
	result.Character = ch
	return result, nil
}

func (w *World) npcAtLockedWithRun(mapID string, x, y int, ignoreNPC bool) bool {
	if ignoreNPC {
		return false
	}
	for _, entity := range w.npcEntitiesInMapOrdered(mapID, false) {
		if entity.MapID == mapID && entity.X == x && entity.Y == y {
			return true
		}
	}
	return false
}

func (w *World) movementGroundEventHitLocked(ch storage.Character) (CharacterHit, bool) {
	key := fireFieldKey{MapID: ch.MapID, X: ch.X, Y: ch.Y}
	field, ok := w.fireFields[key]
	if !ok || field.OwnerID == "" || !field.ExpiresAt.IsZero() && time.Now().After(field.ExpiresAt) || field.Damage <= 0 {
		return CharacterHit{}, false
	}
	owner := field.Owner
	if owner.ID == "" {
		owner = storage.Character{ID: field.OwnerID, MapID: field.MapID, X: field.X, Y: field.Y}
	}
	if !w.isProperCharacterAreaTargetLocked(owner, ch) {
		return CharacterHit{}, false
	}
	updated, hit, err := w.spellCharacterDamageWithPowerLocked(owner, ch, field.Damage)
	if err != nil || hit.Damage <= 0 {
		return CharacterHit{}, false
	}
	hit.Character = updated
	return hit, true
}

func (w *World) hasDigOutEventLocked(mapID string, x, y int, now time.Time) bool {
	for _, event := range w.groundEvents {
		if event.Type != 1 || event.MapID != mapID || event.X != x || event.Y != y {
			continue
		}
		if event.Duration <= 0 || !now.After(event.StartAt.Add(event.Duration)) {
			return true
		}
	}
	return false
}

func validDir(dir int) error {
	if dir < 0 || dir > 7 {
		return fmt.Errorf("invalid direction %d", dir)
	}
	return nil
}
