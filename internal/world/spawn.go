package world

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"openmir2/internal/data"
	"openmir2/internal/storage"
)

const monsterRegenBudget = 5 * time.Millisecond

const (
	monsterUserFull    = 1000
	monsterZenFastStep = 300
)

func (w *World) spawnInitial() {
	for _, sp := range w.data.Spawns {
		tpl, ok := w.data.Monsters[sp.MonsterID]
		if !ok {
			continue
		}
		desired := w.desiredSpawnCountLocked(sp)
		cluster := sp.MissionGenRate > 0 && w.rand.Intn(100) < sp.MissionGenRate
		centerX, centerY := sp.X, sp.Y
		if cluster {
			centerX, centerY = w.referenceSpawnCoordinateLocked(sp)
		}
		for i := 0; i < desired; i++ {
			var x, y int
			var ok bool
			if cluster {
				x, y = centerX-10+w.rand.Intn(20), centerY-10+w.rand.Intn(20)
				x, y, ok = w.resolveReferenceSpawnPositionLocked(sp.MapID, x, y, "")
			} else {
				x, y = w.referenceSpawnCoordinateLocked(sp)
				x, y, ok = w.resolveReferenceSpawnPositionLocked(sp.MapID, x, y, "")
			}
			if !ok {
				break
			}
			mon := w.createSpawnMonsterLocked(sp, tpl, x, y)
			if mon != nil {
				w.occupyMonsterLocked(mon)
			}
		}
	}
}

func (w *World) respawnLocked(now time.Time, players []PlayerSnapshot, result *TickResult) {
	due := make([]*Monster, 0, len(w.monsters))
	for _, mon := range w.monsters {
		if mon != nil && !mon.Alive && !mon.RespawnAt.IsZero() && !now.Before(mon.RespawnAt) {
			due = append(due, mon)
		}
	}
	sort.SliceStable(due, func(i, j int) bool {
		leftGroup, leftMember, leftObject, leftID := monsterProcessOrder(due[i])
		rightGroup, rightMember, rightObject, rightID := monsterProcessOrder(due[j])
		if leftGroup != rightGroup {
			return leftGroup < rightGroup
		}
		if leftMember != rightMember {
			return leftMember < rightMember
		}
		if leftObject != rightObject {
			return leftObject < rightObject
		}
		return leftID < rightID
	})
	for _, mon := range due {
		if mon.Race == 96 && (mon.Ghost || !w.monsterHasVisibleActorLocked(mon, players)) {
			continue
		}
		tpl, ok := w.data.Monsters[mon.TemplateID]
		if !ok {
			continue
		}
		x, y := mon.X, mon.Y
		ok = mon.Race == 96
		if !ok {
			x, y, ok = w.spawnPositionForSpawnLocked(mon.Spawn, mon.ID)
			if !ok {
				mon.RespawnAt = now.Add(time.Second)
				continue
			}
		}
		if mon.SlaveExpLevel > 0 {
			mon.MaxHP = mon.BaseMaxHP
			mon.MinAttack = mon.BaseMinAttack
			mon.MaxAttack = mon.BaseMaxAttack
			mon.WalkSpeedMS = mon.BaseWalkSpeedMS
			mon.SlaveExpLevel = 0
			mon.SlaveNameColor = 0
			mon.SlaveColorSet = false
		}
		zilkinRebirth := mon.ZilkinRebirth
		if zilkinRebirth {
			mon.MaxHP /= 2
			if mon.MaxHP < 1 {
				mon.MaxHP = 1
			}
			mon.Experience /= 2
			mon.HP = mon.MaxHP
			mon.ZilkinRebirth = false
		} else {
			mon.Name = tpl.Name
			mon.Behavior = tpl.Behavior
			mon.Race = tpl.Race
			mon.RaceImg = tpl.RaceImg
			mon.MonsterWeapon = tpl.MP & 0xFF
			mon.Appr = tpl.Appr
			mon.Level = tpl.Level
			mon.Undead = tpl.Undead
			mon.NoTame = tpl.NoTame
			mon.WantRefMsg = tpl.Race == 112
			mon.Experience = tpl.Experience
			mon.MaxHP = tpl.HP
			mon.HP = mon.MaxHP
			mon.MaxMP = tpl.MP
			mon.MP = 0
			mon.MinAttack = tpl.MinAttack
			mon.MaxAttack = tpl.MaxAttack
			mon.Defense = tpl.Defense
			mon.MagicDefense = tpl.MagicDefense
			mon.MagicDefenseMax = tpl.MagicDefenseMax
			mon.AntiMagic = tpl.AntiMagic
			mon.AntiPoison = tpl.AntiPoison
			mon.MagicAttack = tpl.MagicAttack
			mon.MagicAttackMax = tpl.MagicAttackMax
			mon.TaoAttack = tpl.TaoAttack
			mon.Speed = tpl.Speed
			mon.Hit = tpl.Hit
			mon.IncHealth = 0
			mon.IncSpell = 0
			mon.IncHealing = 0
			mon.PerHealing = 5
			mon.PerHealth = 5
			mon.PerSpell = 5
			mon.HealthTick = 0
			mon.SpellTick = 0
			mon.IncHealthSpellAt = now.UnixMilli()
			mon.HealthTickAt = now.UnixMilli()
			mon.SpellTickAt = now.UnixMilli()
			mon.CoolEye = 0
			mon.BodyLeathery = 0
			mon.BaseMaxHP = tpl.HP
			mon.BaseMinAttack = tpl.MinAttack
			mon.BaseMaxAttack = tpl.MaxAttack
			mon.BaseWalkSpeedMS = tpl.WalkSpeedMS
		}
		mon.ViewRange = tpl.ViewRange
		mon.LeashRange = tpl.LeashRange
		mon.SearchNoTargetMS = tpl.SearchNoTargetMS
		mon.SearchHasTargetMS = tpl.SearchHasTargetMS
		mon.AttackIntervalMS = tpl.AttackIntervalMS
		mon.WalkSpeedMS = tpl.WalkSpeedMS
		mon.WalkStep = tpl.WalkStep
		mon.WalkWait = tpl.WalkWait
		mon.RunIntervalMS = 250
		mon.Alive = true
		mon.DeathAt = time.Time{}
		mon.GhostAt = time.Time{}
		mon.Ghost = false
		mon.X, mon.Y = x, y
		if mon.Race != 96 {
			mon.Dir = w.rand.Intn(8)
		}
		if w.monsterIsCentipedeLocked(mon) {
			mon.Dir = 5
		}
		w.spawnStateForLocked(mon.Spawn).activeCount++
		w.occupyMonsterLocked(mon)
		mon.RespawnAt = time.Time{}
		mon.TargetCharacterID = ""
		mon.TargetMonsterID = ""
		mon.PendingDeath = false
		mon.DeathHitterID = ""
		mon.LastHitterID = ""
		mon.LastHitterAt = time.Time{}
		mon.ExpHitterID = ""
		mon.ExpHitterAt = time.Time{}
		mon.TargetFocusAt = time.Time{}
		mon.CastleTauntAt = time.Time{}
		mon.ChildIDs = nil
		mon.TransparentUntil = time.Time{}
		mon.PoisonHealthLevel = 0
		mon.PoisonHealthPoint = 0
		mon.PoisonHealthStartAt = time.Time{}
		mon.PoisonHealthUntil = time.Time{}
		mon.PoisonHealthTickAt = now
		mon.PoisonArmorLevel = 0
		mon.PoisonArmorStartAt = time.Time{}
		mon.PoisonArmorUntil = time.Time{}
		mon.PoisonSourceID = ""
		mon.HolySeizeUntil = time.Time{}
		mon.CrazyUntil = time.Time{}
		mon.ParalyzedUntil = time.Time{}
		mon.DefenceUpUntil = 0
		mon.MagDefenceUpUntil = 0
		mon.ShowHPOpenAt = 0
		mon.ShowHPUntil = 0
		mon.ShowHPDuration = 0
		mon.ThinkAt = now
		mon.DupMode = false
		if mon.Race == 200 {
			mon.TargetFocusAt = now
		}
		mon.LastTargetSearchAt = now
		if w.monsterIsCentipedeLocked(mon) {
			mon.TargetFocusAt = now
		}
		mon.NextSearchAt = time.Time{}
		mon.NextVisibleSearchAt = time.Time{}
		mon.VisibleSearchIntervalMS = 0
		mon.ProcessRunCount = 0
		mon.NextRunAt = time.Time{}
		mon.CowKingPhase = 0
		mon.CowKingState = 0
		mon.CowKingPhaseAt = time.Time{}
		mon.CowKingStoredAttack = 0
		mon.CowKingStoredWalk = 0
		mon.CowKingMoveAt = time.Time{}
		mon.CentipedeAttackAt = time.Time{}
		mon.CentipedeHideAt = time.Time{}
		if mon.Race == 92 {
			mon.CowKingMoveAt = now
			mon.CowKingStoredAttack = mon.AttackIntervalMS
			mon.CowKingStoredWalk = mon.WalkSpeedMS
		}
		if w.monsterIsCentipedeLocked(mon) {
			mon.CentipedeHideAt = now
		}
		mon.LastAttackAt = now.Add(-time.Duration(w.rand.Intn(3000)) * time.Millisecond)
		mon.LastWalkAt = now.Add(-time.Duration(w.rand.Intn(3000)) * time.Millisecond)
		if mon.Race == 96 && zilkinRebirth {
			mon.LastWalkAt = now.Add(time.Second)
		}
		if mon.Race == 100 {
			applyWhiteSkeletonTiming(mon)
			mon.LastWalkAt = now.Add(2 * time.Second)
		}
		mon.WalkCount = 0
		mon.WalkWaitTick = time.Time{}
		mon.WalkWaitLocked = false
		mon.Hidden = false
		w.resetAnimalCorpseLocked(mon)
		mon.FixedHideMode = false
		mon.StoneMode = false
		if mon.Race == 102 {
			mon.StoneDangerLevel = 5
		}
		mon.Animal = false
		mon.FleeOnSight = false
		mon.RunAwayMode = false
		mon.RunAwayUntil = time.Time{}
		mon.FirstRevealPending = false
		mon.GuardDirection = -1
		mon.AttackCount = 0
		mon.AttackMax = 0
		mon.UseMagic = false
		applyMonsterTemplateState(mon, tpl)
		mon.NoTame = tpl.NoTame
		mon.NoItem = false
		w.initializeMonsterGenerationStateLocked(mon, tpl, false)
		w.initializeMonsterDropItemsLocked(mon)
		w.initializeMonsterSchedulingLocked(mon, now)
		mon.AppearStartAt = time.Time{}
		mon.ParentID = ""
		mon.MasterID = ""
		mon.MasterName = ""
		mon.MasterExpiresAt = time.Time{}
		mon.MasterTick = time.Time{}
		mon.MasterDeadSince = time.Time{}
		mon.MasterGhostSince = time.Time{}
		mon.SlaveMakeLevel = 0
		mon.SlaveExpLevel = 0
		mon.SlaveExpProgress = 0
		mon.SlaveNameColor = 0
		mon.SlaveColorSet = false
		mon.ExplosionStartAt = time.Time{}
		if mon.Race == 117 {
			mon.ExplosionStartAt = now
		}
		mon.TargetX = -1
		mon.TargetY = -1
		if result != nil {
			result.RespawnedMonsters = append(result.RespawnedMonsters, *mon)
		}
	}
}

func (w *World) tryZilkinRebirthLocked(mon *Monster, now time.Time, players []PlayerSnapshot, result *TickResult) bool {
	if mon == nil || mon.Race != 96 || mon.Alive || mon.Ghost || !mon.ZilkinRebirth || mon.RespawnAt.IsZero() || now.Before(mon.RespawnAt) || !w.monsterHasVisibleActorLocked(mon, players) {
		return false
	}
	mon.MaxHP /= 2
	mon.Experience /= 2
	mon.HP = mon.MaxHP
	mon.Alive = true
	mon.DeathAt = time.Time{}
	mon.GhostAt = time.Time{}
	mon.RespawnAt = time.Time{}
	mon.ZilkinRebirth = false
	mon.LastWalkAt = now.Add(time.Second)
	w.spawnStateForLocked(mon.Spawn).activeCount++
	w.occupyMonsterLocked(mon)
	if result != nil {
		result.RespawnedMonsters = append(result.RespawnedMonsters, *mon)
	}
	return true
}

func (w *World) monsterHasVisibleActorLocked(mon *Monster, players []PlayerSnapshot) bool {
	if mon == nil {
		return false
	}
	if _, registered := w.monsters[mon.ID]; !registered {
		w.rebuildMonsterCellRegistryLocked()
	}
	for _, player := range players {
		if monsterHasVisibleCharacterLocked(mon, player.Character) {
			return true
		}
	}
	for _, key := range w.visibleActorKeysLocked(mon.ID, mon.MapID, mon.X, mon.Y, mon.ViewRange, nil) {
		if !strings.HasPrefix(key, "monster:") || key == monsterVisibleActorKey(mon.ID) {
			continue
		}
		candidate := w.monsters[strings.TrimPrefix(key, "monster:")]
		if candidate != nil && candidate.MasterID != "" && abs(candidate.X-mon.X) <= 3 && abs(candidate.Y-mon.Y) <= 3 {
			return true
		}
	}
	return false
}

func (w *World) monsterVisibleActiveLocked(mon *Monster) bool {
	if mon == nil {
		return false
	}
	if mon.VisibleSearchIntervalMS <= 0 {
		return true
	}
	for _, key := range w.visibleActors[mon.ID] {
		if strings.HasPrefix(key, "character:") {
			return true
		}
		if !strings.HasPrefix(key, "monster:") || key == monsterVisibleActorKey(mon.ID) {
			continue
		}
		candidate := w.monsters[strings.TrimPrefix(key, "monster:")]
		if candidate != nil && candidate.MasterID != "" {
			return true
		}
	}
	return false
}

func monsterHasVisibleCharacterLocked(mon *Monster, ch storage.Character) bool {
	return ch.MapID == mon.MapID && !ch.Ghost && !ch.ObserverMode && abs(ch.X-mon.X) <= mon.ViewRange && abs(ch.Y-mon.Y) <= mon.ViewRange
}

func (w *World) cleanupDeadMonstersLocked(now time.Time) {
	w.markDeadMonstersLocked(now)
	for id, mon := range w.monsters {
		if mon == nil || mon.Alive || mon.RespawnAt != (time.Time{}) || mon.DeathAt.IsZero() || !mon.Ghost {
			continue
		}
		if mon.Ghost && now.After(mon.GhostAt.Add(5*time.Minute)) {
			w.unregisterMonsterLocked(id)
			w.unindexMonsterCellLocked(mon)
			delete(w.monsters, id)
		}
	}
}

func (w *World) markDeadMonstersLocked(now time.Time) {
	for _, mon := range w.monsters {
		if mon == nil || mon.Alive || mon.CastleDoor || mon.CastleWall || mon.RespawnAt != (time.Time{}) || mon.DeathAt.IsZero() || mon.Ghost {
			continue
		}
		if now.After(mon.DeathAt.Add(3 * time.Minute)) {
			mon.Ghost = true
			mon.GhostAt = now
		}
	}
}

func (w *World) replenishMonsterSpawnLocked(now time.Time, result *TickResult) {
	w.replenishMonsterSpawnForPopulationLocked(now, result, 0)
}

func (w *World) replenishMonsterSpawnForPopulationLocked(now time.Time, result *TickResult, userCount int) {
	if len(w.data.Spawns) == 0 {
		return
	}
	if !w.monsterRegenAt.IsZero() && !now.After(w.monsterRegenAt) {
		return
	}
	w.monsterRegenAt = now.Add(monsterRegenInterval)
	if w.monsterRegenCursor >= len(w.data.Spawns) {
		w.monsterRegenCursor = 0
	}
	sp := w.data.Spawns[w.monsterRegenCursor]
	w.monsterRegenCursor = (w.monsterRegenCursor + 1) % len(w.data.Spawns)
	state := w.spawnStateForLocked(sp)
	interval := monsterZenTime(time.Duration(sp.RespawnSeconds)*time.Minute, userCount)
	if interval <= 0 {
		interval = time.Minute
	}
	if !state.lastRegenAt.IsZero() && !now.After(state.lastRegenAt.Add(interval)) {
		return
	}
	desired := w.desiredSpawnCountLocked(sp)
	if state.activeCount >= desired {
		state.lastRegenAt = now
		return
	}
	tpl, ok := w.data.Monsters[sp.MonsterID]
	if !ok {
		return
	}
	regenStarted := time.Now()
	cluster := sp.MissionGenRate > 0 && w.rand.Intn(100) < sp.MissionGenRate
	centerX, centerY := sp.X, sp.Y
	if cluster {
		centerX, centerY = w.referenceSpawnCoordinateLocked(sp)
	}
	completed := true
	for state.activeCount < desired {
		if time.Since(regenStarted) > monsterRegenBudget {
			completed = false
			break
		}
		var x, y int
		var ok bool
		if cluster {
			x, y = centerX-10+w.rand.Intn(20), centerY-10+w.rand.Intn(20)
			x, y, ok = w.resolveReferenceSpawnPositionLocked(sp.MapID, x, y, "")
		} else {
			x, y = w.referenceSpawnCoordinateLocked(sp)
			x, y, ok = w.resolveReferenceSpawnPositionLocked(sp.MapID, x, y, "")
		}
		if !ok {
			break
		}
		mon := w.createSpawnMonsterLocked(sp, tpl, x, y)
		if mon == nil {
			break
		}
		w.occupyMonsterLocked(mon)
		result.SpawnedMonsters = append(result.SpawnedMonsters, *mon)
	}
	if completed {
		state.lastRegenAt = now
	}
}

func monsterZenTime(duration time.Duration, userCount int) time.Duration {
	if duration >= 30*time.Minute || userCount <= monsterUserFull {
		return duration
	}
	steps := float64(userCount-monsterUserFull) / monsterZenFastStep
	if steps > 6 {
		steps = 6
	}
	return duration - time.Duration(float64(duration)/10*steps+0.5)
}

func (w *World) desiredSpawnCountLocked(spawn data.StdSpawn) int {
	return desiredSpawnCountForRate(spawn.Count, 10)
}

func desiredSpawnCountForRate(count, rate int) int {
	if rate <= 0 {
		rate = 10
	}
	desired := referenceRound(math.Max(1, float64(count)) / (float64(rate) / 10.0))
	if desired < 1 {
		return 1
	}
	return desired
}

func spawnStateKey(spawn data.StdSpawn) string {
	if spawn.ID != "" {
		return spawn.ID
	}
	if spawn.ReferenceOrder <= 0 {
		return "dynamic-monsters"
	}
	return fmt.Sprintf("%s:%s:%d:%d:%d:%d:%d:%d", spawn.MapID, spawn.MonsterID, spawn.X, spawn.Y, spawn.Range, spawn.Count, spawn.RespawnSeconds, spawn.MissionGenRate)
}

func (w *World) spawnStateForLocked(spawn data.StdSpawn) *spawnState {
	if w.spawns == nil {
		w.spawns = map[string]*spawnState{}
	}
	key := spawnStateKey(spawn)
	if state, ok := w.spawns[key]; ok {
		return state
	}
	state := &spawnState{spawn: spawn}
	w.spawns[key] = state
	w.monsterProcessGroups = append(w.monsterProcessGroups, key)
	return state
}

func (w *World) createSpawnMonsterLocked(spawn data.StdSpawn, tpl data.StdMonster, x, y int) *Monster {
	id := fmt.Sprintf("mon-%d", w.nextID)
	w.nextID++
	if spawn.ID == "" {
		spawn.ID = spawnStateKey(spawn)
	}
	mon := newMonster(w, id, tpl, spawn.MapID, x, y, spawn)
	state := w.spawnStateForLocked(spawn)
	state.nextMemberOrder++
	mon.SpawnMemberOrder = state.nextMemberOrder
	w.registerMonsterLocked(mon, state)
	state.activeCount++
	return mon
}

func (w *World) registerMonsterLocked(mon *Monster, state *spawnState) {
	if mon == nil {
		return
	}
	w.monsterProcessRegistryValidated = false
	if w.monsterProcessGroupByID == nil {
		w.monsterProcessGroupByID = map[string]string{}
	}
	if state == nil {
		if mon.Spawn.ID == "" {
			mon.Spawn.ID = spawnStateKey(mon.Spawn)
		}
		state = w.spawnStateForLocked(mon.Spawn)
	}
	if mon.SpawnMemberOrder == 0 {
		state.nextMemberOrder++
		mon.SpawnMemberOrder = state.nextMemberOrder
	}
	w.monsters[mon.ID] = mon
	w.indexMonsterCellLocked(mon)
	if _, exists := w.monsterProcessGroupByID[mon.ID]; !exists {
		state.members = append(state.members, mon)
		w.monsterProcessGroupByID[mon.ID] = spawnStateKey(state.spawn)
	}
}

func (w *World) unregisterMonsterLocked(id string) {
	w.monsterProcessRegistryValidated = false
	delete(w.visibleActors, id)
	delete(w.monsterStatusClocks, id)
	key, ok := w.monsterProcessGroupByID[id]
	if !ok {
		return
	}
	delete(w.monsterProcessGroupByID, id)
	state := w.spawns[key]
	if state == nil {
		return
	}
	for i, member := range state.members {
		if member == nil || member.ID != id {
			continue
		}
		state.members = append(state.members[:i], state.members[i+1:]...)
		if key == w.monsterProcessGroupKeyLocked() && i < w.monsterProcessMemberCursor {
			w.monsterProcessMemberCursor--
		}
		break
	}
}

func (w *World) monsterProcessGroupKeyLocked() string {
	if w.monsterProcessGroupCursor < 0 || w.monsterProcessGroupCursor >= len(w.monsterProcessGroups) {
		return ""
	}
	return w.monsterProcessGroups[w.monsterProcessGroupCursor]
}

func (w *World) ensureMonsterProcessRegistryLocked() {
	if len(w.monsterProcessGroupByID) == len(w.monsters) && (len(w.monsters) == 0 || len(w.monsterProcessGroups) > 0) {
		if w.monsterProcessRegistryValidated {
			return
		}
		if w.monsterProcessGroupCursor >= 0 && w.monsterProcessGroupCursor < len(w.monsterProcessGroups) {
			state := w.spawns[w.monsterProcessGroups[w.monsterProcessGroupCursor]]
			if state != nil {
				for _, member := range state.members {
					if member == nil || w.monsters[member.ID] != member {
						w.rebuildMonsterProcessRegistryLocked()
						return
					}
				}
			}
		}
		w.monsterProcessRegistryValidated = true
		return
	}
	w.rebuildMonsterProcessRegistryLocked()
}

type monsterProcessEntry struct {
	monster    *Monster
	group      int
	member     int
	nextGroup  int
	nextMember int
}

func (w *World) rebuildMonsterProcessRegistryLocked() {
	w.monsterProcessRegistryValidated = false
	for _, state := range w.spawns {
		state.members = nil
	}
	w.monsterProcessGroupByID = make(map[string]string, len(w.monsters))
	monsters := make([]*Monster, 0, len(w.monsters))
	for _, mon := range w.monsters {
		if mon != nil {
			monsters = append(monsters, mon)
		}
	}
	sort.SliceStable(monsters, func(i, j int) bool {
		leftGroup, leftMember, leftObject, leftID := monsterProcessOrder(monsters[i])
		rightGroup, rightMember, rightObject, rightID := monsterProcessOrder(monsters[j])
		if leftGroup != rightGroup {
			return leftGroup < rightGroup
		}
		if leftMember != rightMember {
			return leftMember < rightMember
		}
		if leftObject != rightObject {
			return leftObject < rightObject
		}
		return leftID < rightID
	})
	for _, mon := range monsters {
		w.registerMonsterLocked(mon, nil)
	}
	w.monsterProcessRegistryValidated = true
}

func (w *World) nextMonsterProcessEntryLocked(processed map[string]struct{}) (monsterProcessEntry, bool) {
	w.ensureMonsterProcessRegistryLocked()
	if len(w.monsterProcessGroups) == 0 {
		return monsterProcessEntry{}, false
	}
	if w.monsterProcessGroupCursor < 0 {
		w.monsterProcessGroupCursor = 0
		w.monsterProcessMemberCursor = 0
	}
	groupIndex := w.monsterProcessGroupCursor
	memberIndex := w.monsterProcessMemberCursor
	if w.monsterProcessGroupCursor >= len(w.monsterProcessGroups) {
		w.monsterProcessGroupCursor = 0
		w.monsterProcessMemberCursor = 0
		return monsterProcessEntry{}, false
	}
	for groupIndex < len(w.monsterProcessGroups) {
		key := w.monsterProcessGroups[groupIndex]
		state := w.spawns[key]
		if state != nil {
			if memberIndex < 0 || memberIndex > len(state.members) {
				memberIndex = 0
			}
			for member := memberIndex; member < len(state.members); member++ {
				mon := state.members[member]
				if mon == nil {
					continue
				}
				id := mon.ID
				if _, alreadyProcessed := processed[id]; alreadyProcessed {
					continue
				}
				nextGroup, nextMember := groupIndex, member+1
				if nextMember >= len(state.members) {
					nextGroup = groupIndex + 1
					nextMember = 0
				}
				return monsterProcessEntry{monster: mon, group: groupIndex, member: member, nextGroup: nextGroup, nextMember: nextMember}, true
			}
		}
		groupIndex++
		memberIndex = 0
	}
	return monsterProcessEntry{}, false
}

func monsterProcessOrder(mon *Monster) (int, uint64, uint64, string) {
	if mon == nil {
		return 1 << 30, 1 << 30, 1 << 30, ""
	}
	group := mon.Spawn.ReferenceOrder
	if group <= 0 {
		group = 1 << 30
		positionX := uint64(uint32(mon.X + 1<<15))
		positionY := uint64(uint32(mon.Y + 1<<15))
		return group, positionX<<32 | positionY, mon.ObjectOrder, mon.ID
	}
	member := mon.SpawnMemberOrder
	if member == 0 {
		member = mon.ObjectOrder
	}
	return group, member, mon.ObjectOrder, mon.ID
}

func (w *World) rollDropsLocked(mon *Monster, ownerID, ownerGroupID string, blockers ...storage.Character) []GroundDrop {
	if mon == nil {
		return nil
	}
	if mon.NoItem {
		return nil
	}
	if mapData, ok := w.data.Maps[mon.MapID]; ok && mapData.NoDropItem {
		return nil
	}
	if len(mon.CorpseItems) == 0 && mon.CorpseGold == 0 {
		return nil
	}
	out := []GroundDrop{}
	for _, instance := range mon.CorpseItems {
		id := fmt.Sprintf("drop-%d", w.nextID)
		w.nextID++
		out = append(out, GroundDrop{
			ID:        id,
			MapID:     mon.MapID,
			ItemID:    instance.ItemID,
			Count:     1,
			MakeIndex: instance.MakeIndex,
			OwnerID:   ownerID,
			PickupAt:  time.Now().Add(time.Duration(w.gameplay.Item.FloorItemCanPickUpMS) * time.Millisecond),
			Dura:      instance.Dura,
			DuraMax:   instance.DuraMax,
			Desc:      instance.Desc,
		})
		if ownerGroupID != "" {
			out[len(out)-1].OwnerGroupID = ownerGroupID
		}
	}
	if mon.CorpseGold > 0 {
		remaining := mon.CorpseGold
		for drops := 0; remaining > 0 && drops < 17; drops++ {
			count := remaining
			if count > 2000 {
				count = 2000
			}
			out = append(out, GroundDrop{ID: fmt.Sprintf("drop-%d", w.nextID), MapID: mon.MapID, ItemID: "金币", Count: count, OwnerID: ownerID, OwnerGroupID: ownerGroupID, PickupAt: time.Now().Add(time.Duration(w.gameplay.Item.FloorItemCanPickUpMS) * time.Millisecond)})
			w.nextID++
			remaining -= count
		}
	}
	goldBefore := 0
	for _, drop := range w.drops {
		if drop.MapID == mon.MapID && drop.ItemID == "金币" {
			goldBefore += drop.Count
		}
	}
	placed := w.placeReferenceDropsLocked(mon.MapID, mon.X, mon.Y, 3, out, blockers...)
	if len(placed) == 0 {
		return placed
	}
	placedItems := make(map[int32]struct{}, len(placed))
	placedGold := 0
	for _, drop := range placed {
		if drop.ItemID == "金币" {
			continue
		}
		placedItems[drop.MakeIndex] = struct{}{}
	}
	goldAfter := 0
	for _, drop := range w.drops {
		if drop.MapID == mon.MapID && drop.ItemID == "金币" {
			goldAfter += drop.Count
		}
	}
	if goldAfter > goldBefore {
		placedGold = goldAfter - goldBefore
	}
	remainingItems := mon.CorpseItems[:0]
	for _, item := range mon.CorpseItems {
		if _, ok := placedItems[item.MakeIndex]; !ok {
			remainingItems = append(remainingItems, item)
		}
	}
	mon.CorpseItems = remainingItems
	mon.CorpseGold -= placedGold
	if mon.CorpseGold < 0 {
		mon.CorpseGold = 0
	}
	return placed
}

func (w *World) initializeMonsterDropItemsLocked(mon *Monster) {
	if mon == nil || mon.DropTable == "" {
		return
	}
	table, ok := w.data.Drops[mon.DropTable]
	if !ok {
		return
	}
	for _, entry := range table.Entries {
		if entry.Chance <= 0 || w.rand.Float64() >= entry.Chance {
			continue
		}
		if entry.ItemID == "金币" {
			count := entry.MinCount/2 + w.rand.Intn(entry.MinCount)
			mon.CorpseGold += count
			continue
		}
		item, ok := w.data.Items[entry.ItemID]
		if !ok {
			continue
		}
		instance := w.createUserItemFromStd(item, 0, [14]byte{})
		instance.Dura = uint16(referenceRound(float64(instance.DuraMax) / 100 * float64(20+w.rand.Intn(80))))
		if rate := w.gameplay.Item.MonsterRandomAddValue; rate > 0 && w.rand.Intn(rate) == 0 {
			applyReferenceMonsterDropUpgrade(w.rand, item, &instance)
		}
		if item.StdMode == 15 || item.StdMode == 19 || item.StdMode == 20 || item.StdMode == 21 || item.StdMode == 22 || item.StdMode == 23 || item.StdMode == 24 || item.StdMode == 26 {
			if item.Shape == 130 || item.Shape == 131 || item.Shape == 132 {
				applyReferenceUnknownMonsterDropUpgrade(w.rand, item.StdMode, &instance)
			}
		}
		mon.CorpseItems = append(mon.CorpseItems, instance)
	}
}

func (w *World) spawnPositionForSpawnLocked(spawn data.StdSpawn, exceptID string) (int, int, bool) {
	cluster := spawn.MissionGenRate > 0 && w.rand.Intn(100) < spawn.MissionGenRate
	if cluster {
		centerX, centerY := w.referenceSpawnCoordinateLocked(spawn)
		x, y := centerX-10+w.rand.Intn(20), centerY-10+w.rand.Intn(20)
		return w.resolveReferenceSpawnPositionLocked(spawn.MapID, x, y, exceptID)
	}
	x, y := w.referenceSpawnCoordinateLocked(spawn)
	return w.resolveReferenceSpawnPositionLocked(spawn.MapID, x, y, exceptID)
}

func (w *World) referenceSpawnCoordinateLocked(spawn data.StdSpawn) (int, int) {
	span := spawn.Range*2 + 1
	if span <= 0 {
		span = 1
	}
	return spawn.X - spawn.Range + w.rand.Intn(span), spawn.Y - spawn.Range + w.rand.Intn(span)
}

func (w *World) resolveReferenceSpawnPositionLocked(mapID string, x, y int, exceptID string) (int, int, bool) {
	mp, ok := w.data.Maps[mapID]
	if !ok || mp.Width <= 0 || mp.Height <= 0 {
		return 0, 0, false
	}
	step := 3
	if mp.Width < 50 {
		step = 2
	}
	edge := 50
	if mp.Height < 250 {
		edge = 20
		if mp.Height < 30 {
			edge = 2
		}
	}
	for attempt := 0; attempt < 31; attempt++ {
		if mp.Walkable(x, y) && !w.monsterAtLocked(mapID, x, y, exceptID) {
			return x, y, true
		}
		if mp.Width-edge-1 > x {
			x += step
			continue
		}
		x = w.rand.Intn(mp.Width/2) + edge
		if mp.Height-edge-1 > y {
			y += step
			continue
		}
		y = w.rand.Intn(mp.Height/2) + edge
	}
	return 0, 0, false
}

func (w *World) findSpawnPositionAvoidLocked(mapID string, x, y, searchRadius int, exceptID string, avoidX, avoidY int) (int, int, bool) {
	mp, ok := w.data.Maps[mapID]
	if !ok {
		return 0, 0, false
	}
	if searchRadius == 0 {
		searchRadius = 1
	}
	for radius := 0; radius <= searchRadius; radius++ {
		for yy := y - radius; yy <= y+radius; yy++ {
			for xx := x - radius; xx <= x+radius; xx++ {
				if radius > 0 && xx > x-radius && xx < x+radius && yy > y-radius && yy < y+radius {
					continue
				}
				if xx == avoidX && yy == avoidY {
					continue
				}
				if mp.Walkable(xx, yy) && !w.monsterAtLocked(mapID, xx, yy, exceptID) {
					return xx, yy, true
				}
			}
		}
	}
	return 0, 0, false
}

func (w *World) findRandomSpawnPositionLocked(mapID string, x, y, searchRadius int, exceptID string) (int, int, bool) {
	return w.findRandomSpawnPositionAvoidLocked(mapID, x, y, searchRadius, exceptID, -1, -1)
}

func (w *World) findRandomSpawnPositionAvoidLocked(mapID string, x, y, searchRadius int, exceptID string, avoidX, avoidY int) (int, int, bool) {
	mp, ok := w.data.Maps[mapID]
	if !ok {
		return 0, 0, false
	}
	if searchRadius < 0 {
		searchRadius = 0
	}
	for attempt := 0; attempt < 31; attempt++ {
		xx, yy := x, y
		if searchRadius > 0 {
			xx = x - searchRadius + w.rand.Intn(searchRadius*2+1)
			yy = y - searchRadius + w.rand.Intn(searchRadius*2+1)
		}
		if xx == avoidX && yy == avoidY {
			continue
		}
		if mp.Walkable(xx, yy) && !w.monsterAtLocked(mapID, xx, yy, exceptID) {
			return xx, yy, true
		}
	}
	return w.findSpawnPositionAvoidLocked(mapID, x, y, searchRadius, exceptID, avoidX, avoidY)
}
