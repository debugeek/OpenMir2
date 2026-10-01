package world

import (
	"fmt"
	"time"

	"openmir2/internal/data"
	"openmir2/internal/npc"
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
	w.mu.Lock()
	defer w.mu.Unlock()
	if x != ch.X || y != ch.Y {
		return ch, fmt.Errorf("turn coordinates do not match current position")
	}
	ch.Dir = dir
	return ch, nil
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
	CharacterHits []CharacterHit
	Character     storage.Character
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
	w.respawnLocked(now)
	if attackIdent == mir176.CMHeavyHit {
		if mineResult, handled, err := w.mineWithHeavyHitLocked(ch, dir); handled {
			applyMiningRecoveryDelay(&mineResult.Character)
			if err != nil {
				return AttackResult{}, err
			}
			return mineResult, nil
		}
	}
	applyAttackRecoveryDelay(&ch)
	result := AttackResult{Character: ch}
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
	applyAttackerWeaponDamage := func() {
		durability, deleted, featureChanged := w.applyWeaponDamageLocked(&ch)
		result.Durability = append(result.Durability, durability...)
		result.DeletedItems = append(result.DeletedItems, deleted...)
		result.FeatureChanged = result.FeatureChanged || featureChanged
	}
	appendNPCTrainingHit := func(entity npc.Entity, damage int) error {
		hit, err := w.applyNPCTrainingHitLocked(entity.ID, ch.ID, damage, false, now)
		if err != nil {
			return err
		}
		if hit.Damage > 0 {
			result.NPCTrainingHits = append(result.NPCTrainingHits, hit)
		}
		return nil
	}
	baseSecondaryDamage := 0
	if attackIdent == mir176.CMLongHit || attackIdent == mir176.CMWideHit {
		baseSecondaryDamage = w.characterAttackDamageLocked(ch, nil, mir176.CMHit)
	}
	secondaryDamage := 0
	if attackIdent == mir176.CMLongHit {
		if state, _, ok := ch.Skills.Get("刺杀剑术"); ok {
			if _, ok := w.data.Skills["刺杀剑术"]; ok {
				secondaryDamage = referenceRound(float64(baseSecondaryDamage) / float64(spellTrainLevel+2) * float64(state.Level+2))
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
	if w.weaponUpgradeTargetAtPointsLocked(ch, points, blockers...) {
		w.resolveWeaponUpgradeLocked(&ch, &result)
	}
	for _, point := range points {
		if entity, ok := w.trainerAtExactPointLocked(ch.MapID, point[0], point[1]); ok {
			damage := w.characterHitDamageForAttackLocked(ch, attackIdent)
			if attackIdent == mir176.CMLongHit || attackIdent == mir176.CMWideHit {
				damage = secondaryDamage
			}
			if damage > 0 {
				if err := appendNPCTrainingHit(entity, damage); err != nil {
					return AttackResult{}, err
				}
			}
			continue
		}
		if mon := w.monsterAtExactPointLocked(ch.MapID, point[0], point[1]); mon != nil {
			if !w.isProperMonsterTargetLocked(ch, blockers, mon) {
				continue
			}
			if attackIdent == mir176.CMLongHit || attackIdent == mir176.CMWideHit {
				if secondaryDamage <= 0 {
					continue
				}
				ch.TargetID = mon.ID
				hit, err := w.attackMonsterDirectDamageLocked(ch, mon, secondaryDamage, blockers...)
				if err != nil {
					return AttackResult{}, err
				}
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
				hit.Character.TargetID = mon.ID
			}
			consumeSpecialHit(&hit)
			return hit, w.store.SaveCharacter(hit.Character)
		}
		if target, ok := w.characterAtExactPointLocked(blockers, ch.MapID, point[0], point[1]); ok {
			if !w.isProperCharacterTargetLocked(ch, target) {
				continue
			}
			damage := w.characterHitDamageForAttackLocked(ch, attackIdent)
			if attackIdent == mir176.CMLongHit || attackIdent == mir176.CMWideHit {
				if secondaryDamage <= 0 {
					continue
				}
				ch.TargetID = target.ID
				damage = secondaryDamage
				_, hit, err := w.attackCharacterDirectDamageLocked(ch, target, damage)
				if err != nil {
					return AttackResult{}, err
				}
				hit.ImpactDelay = 500 * time.Millisecond
				if hit.Connected {
					applyAttackerWeaponDamage()
					ch = w.mergeStoredCharacterPKFlagLocked(ch)
					appendCharacterHit(hit)
				}
				continue
			}
			_, hit, err := w.attackCharacterWithDamageLocked(ch, target, damage)
			if err != nil {
				return AttackResult{}, err
			}
			if hit.Damage > 0 {
				applyAttackerWeaponDamage()
				ch = w.mergeStoredCharacterPKFlagLocked(ch)
				ch.TargetID = target.ID
				result.Character = ch
			}
			result.CharacterHits = []CharacterHit{hit}
			if hit.Damage > 0 {
				if err := trainMainAttack(); err != nil {
					return AttackResult{}, err
				}
			}
			consumeSpecialHit(nil)
			if fireHitActive {
				result.Character = ch
			}
			return result, w.store.SaveCharacter(ch)
		}
	}
	if attackIdent == mir176.CMLongHit || attackIdent == mir176.CMWideHit {
		primary := [2]int{ch.X + dirOffsets[dir][0], ch.Y + dirOffsets[dir][1]}
		if mon := w.monsterAtExactPointLocked(ch.MapID, primary[0], primary[1]); mon != nil {
			if w.isProperMonsterTargetLocked(ch, blockers, mon) {
				ch.TargetID = mon.ID
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
					if secondaryDamage > 0 {
						if err := w.applyMeleeTrainingLocked(&hit, attackIdent); err != nil {
							return AttackResult{}, err
						}
					}
					appendMonsterHit(hit)
				}
			}
		} else if target, ok := w.characterAtExactPointLocked(blockers, ch.MapID, primary[0], primary[1]); ok && w.isProperCharacterTargetLocked(ch, target) {
			ch.TargetID = target.ID
			primaryDamage := w.characterHitDamageForAttackLocked(ch, mir176.CMHit)
			if secondaryDamage > 0 {
				primaryDamage = baseSecondaryDamage
			}
			_, hit, err := w.attackCharacterWithDamageLocked(ch, target, primaryDamage)
			if err != nil {
				return AttackResult{}, err
			}
			appendCharacterHit(hit)
			if hit.Damage > 0 {
				applyAttackerWeaponDamage()
				ch = w.mergeStoredCharacterPKFlagLocked(ch)
				if err := trainMainAttack(); err != nil {
					return AttackResult{}, err
				}
			}
		}
	}
	consumeSpecialHit(nil)
	if fireHitActive {
		result.Character = ch
	}
	return result, w.store.SaveCharacter(ch)
}

func (w *World) weaponUpgradeTargetAtPointsLocked(ch storage.Character, points [][2]int, blockers ...storage.Character) bool {
	for _, point := range points {
		if entity, ok := w.trainerAtExactPointLocked(ch.MapID, point[0], point[1]); ok && entity.ID != "" {
			return true
		}
		if w.monsterAtExactPointLocked(ch.MapID, point[0], point[1]) != nil {
			return true
		}
		if _, ok := w.characterAtExactPointLocked(blockers, ch.MapID, point[0], point[1]); ok {
			return true
		}
	}
	return false
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
	result.FeatureChanged = true
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
	for _, mon := range w.monsters {
		if mon.Alive && mon.MapID == mapID && mon.X == x && mon.Y == y {
			return mon
		}
	}
	return nil
}

func (w *World) characterAtExactPointLocked(players []storage.Character, mapID string, x, y int) (storage.Character, bool) {
	for _, target := range players {
		if target.ID == "" || target.MapID != mapID || target.HP <= 0 {
			continue
		}
		if target.X == x && target.Y == y {
			return target, true
		}
	}
	return storage.Character{}, false
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
	w.syncCharacterHomeFromStartPointLocked(&ch)
	return ch, nil
}

// directionalStepLocked resolves CM_WALK/CM_RUN the way the reference
// server's WalkTo/RunTo do (BaseObject.cs, PlayObject.Base.cs): the
// destination tile is derived from the character's current position plus the
// direction offset repeated `steps` times (1 for walk, 2 for run), not taken
// from the client's reported (x, y) directly. Every tile along the way must
// be walkable, matching RunTo's CanWalkEx checks on both the +1 and +2 tiles
// — a distance-only bound would let a run "jump" a one-tile-wide obstacle by
// only checking the final tile. The client's (x, y) must match the derived
// destination. The requested coordinates select the direction before this
// function runs; they do not prevent the directional movement itself.
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
	for i := 1; i <= steps; i++ {
		destX, destY = ch.X+off[0]*i, ch.Y+off[1]*i
		if !mp.Walkable(destX, destY) {
			return MovementResult{Character: ch}, fmt.Errorf("move is blocked")
		}
		if w.monsterAtLockedWithRun(ch.MapID, destX, destY, "", ignoreRunMonsters) {
			return MovementResult{Character: ch}, fmt.Errorf("move is blocked")
		}
		for _, blocker := range blockers {
			if blocker.ID != "" && blocker.ID != ch.ID && blocker.HP > 0 && !blocker.ObserverMode && !ignoreRunHumans && blocker.MapID == ch.MapID && blocker.X == destX && blocker.Y == destY {
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
	oldMapID, oldX, oldY := ch.MapID, ch.X, ch.Y
	ch.X = destX
	ch.Y = destY
	result := MovementResult{Character: ch}
	if hit, ok := w.movementGroundEventHitLocked(ch); ok {
		ch = hit.Character
		result.Character = ch
		result.CharacterHits = append(result.CharacterHits, hit)
	}
	if gate != nil {
		targetMap, targetExists := w.data.Maps[gate.ToMap]
		if !targetExists || !targetMap.Walkable(gate.ToX, gate.ToY) {
			ch.MapID, ch.X, ch.Y = oldMapID, oldX, oldY
			result.Character = ch
			return result, fmt.Errorf("map gate destination is invalid")
		}
		if targetMap.NeedHole && !w.hasDigOutEventLocked(oldMapID, destX, destY, time.Now()) {
			ch.MapID, ch.X, ch.Y = oldMapID, oldX, oldY
			result.Character = ch
			return result, fmt.Errorf("map gate requires an active dig-out event")
		}
		if !w.doorsOpenAroundLocked(oldMapID, destX, destY, time.Now()) {
			ch.MapID, ch.X, ch.Y = oldMapID, oldX, oldY
			result.Character = ch
			return result, fmt.Errorf("map gate requires nearby doors to be open")
		}
		ch.MapID = targetMap.ID
		ch.X = gate.ToX
		ch.Y = gate.ToY
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
	w.syncCharacterHomeFromStartPointLocked(&ch)
	result.Character = ch
	return result, nil
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
