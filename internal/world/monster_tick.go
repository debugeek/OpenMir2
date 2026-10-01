package world

import (
	"fmt"
	"math"
	"sort"
	"time"

	"openmir2/internal/data"
	"openmir2/internal/npc"
	"openmir2/internal/storage"
	"openmir2/internal/world/core"
)

func (w *World) experienceGroupMembersLocked(killer storage.Character, players map[string]storage.Character) []storage.Character {
	if killer.ID == "" || killer.GroupOwnerID == "" {
		return nil
	}
	members := make([]storage.Character, 0, len(players))
	if owner, ok := players[killer.GroupOwnerID]; ok && len(owner.GroupMembers) > 0 {
		for _, memberID := range owner.GroupMembers {
			member, ok := players[memberID]
			if !ok || member.HP <= 0 || member.MapID != killer.MapID || member.GroupOwnerID != killer.GroupOwnerID {
				continue
			}
			if absInt(member.X-killer.X) > 12 || absInt(member.X-killer.X) > 12 {
				continue
			}
			members = append(members, member)
		}
		return members
	}
	for _, member := range players {
		if member.HP <= 0 || member.MapID != killer.MapID || member.GroupOwnerID != killer.GroupOwnerID {
			continue
		}
		if absInt(member.X-killer.X) > 12 || absInt(member.X-killer.X) > 12 {
			continue
		}
		members = append(members, member)
	}
	return members
}

func (w *World) settleCharacterDeathLocked(ch *storage.Character, now time.Time) []GroundDrop {
	if ch == nil {
		return nil
	}
	drops := make([]GroundDrop, 0)
	if w.gameplay.Combat.DieScatterBag {
		for _, entry := range ch.BagItems {
			if entry.ItemID == "" {
				continue
			}
			drops = append(drops, GroundDrop{
				ID:        fmt.Sprintf("drop-%d", w.nextID),
				MapID:     ch.MapID,
				ItemID:    entry.ItemID,
				Count:     1,
				MakeIndex: entry.MakeIndex,
				PickupAt:  now.Add(time.Duration(w.gameplay.Item.FloorItemCanPickUpMS) * time.Millisecond),
				Dura:      entry.Dura,
				DuraMax:   entry.DuraMax,
				Desc:      entry.Desc,
			})
			w.nextID++
		}
		ch.BagItems = nil
	}
	if w.gameplay.Combat.DieDropGold && ch.Gold > 0 {
		drops = append(drops, GroundDrop{ID: fmt.Sprintf("drop-%d", w.nextID), MapID: ch.MapID, ItemID: "金币", Count: ch.Gold, PickupAt: now.Add(time.Duration(w.gameplay.Item.FloorItemCanPickUpMS) * time.Millisecond)})
		w.nextID++
		ch.Gold = 0
	}
	if !ch.GuildWarArea {
		if mapData, ok := w.data.Maps[ch.MapID]; !ok || !mapData.Safe {
			addBodyLuck(ch, -float64(ch.Level*5))
		}
	}
	ch.GroupOwnerID = ""
	return w.placeDropsLocked(ch.MapID, ch.X, ch.Y, 3, drops)
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func (w *World) settlePendingCharacterDeathsLocked(players []PlayerSnapshot, playersByID, updated map[string]storage.Character, result *TickResult, now time.Time) error {
	for _, player := range players {
		ch, ok := playersByID[player.Character.ID]
		if !ok {
			continue
		}
		if ch.PKFlag && ch.PKFlagUntil > 0 && now.UnixNano() > ch.PKFlagUntil {
			ch.PKFlag = false
			ch.PKFlagUntil = 0
			updated[ch.ID] = ch
			result.NameColorCharacters = append(result.NameColorCharacters, ch)
		}
		lastHitter, hitterPresent := playersByID[ch.LastHitterID]
		monsterHitter, monsterHitterPresent := w.monsters[ch.LastHitterID]
		monsterHitterDead := monsterHitterPresent && (monsterHitter.HP <= 0 || !monsterHitter.Alive)
		if ch.LastHitterID != "" && ch.LastHitterAt > 0 && (now.UnixNano()-ch.LastHitterAt > int64(30*time.Second) || hitterPresent && lastHitter.HP <= 0 || monsterHitterDead) {
			ch.LastHitterID = ""
			ch.LastHitterAt = 0
			updated[ch.ID] = ch
		}
		_, pending := w.pendingCharacterDeaths[ch.ID]
		if !pending {
			if ch.HP > 0 {
				delete(w.finalizedCharacterDeaths, ch.ID)
				continue
			}
			if _, finalized := w.finalizedCharacterDeaths[ch.ID]; finalized {
				continue
			}
		}
		delete(w.pendingCharacterDeaths, ch.ID)
		if ch.HP <= 0 {
			blockedRevival := false
			if hitter, ok := playersByID[ch.LastHitterID]; ok {
				blockedRevival = w.characterHasUnrevivalItemLocked(hitter)
			}
			if !blockedRevival {
				if revival, revived, err := w.reviveCharacterLocked(&ch, now); err != nil {
					return err
				} else if revived {
					playersByID[ch.ID] = ch
					updated[ch.ID] = ch
					delete(w.finalizedCharacterDeaths, ch.ID)
					result.CharacterRevivals = append(result.CharacterRevivals, revival)
					continue
				}
			}
			w.finalizedCharacterDeaths[ch.ID] = struct{}{}
			groupEvent, err := w.settleDeadCharacterGroupLocked(&ch, playersByID)
			if err != nil {
				return err
			}
			if len(groupEvent.Updated) > 0 || len(groupEvent.Cancel) > 0 || groupEvent.MemberListOwnerID != "" {
				result.GroupSyncEvents = append(result.GroupSyncEvents, groupEvent)
			}
			result.CharacterDrops = append(result.CharacterDrops, w.settleCharacterDeathLocked(&ch, now)...)
			if killer, ok := playersByID[ch.LastHitterID]; ok && w.resolvePKDeathLocked(&ch, &killer) {
				playersByID[killer.ID] = killer
				updated[killer.ID] = killer
				result.PKDeathMessages = append(result.PKDeathMessages,
					PKDeathMessage{Character: killer, Text: w.gameplay.Combat.PKMurderMessage},
					PKDeathMessage{Character: ch, Text: fmt.Sprintf(w.gameplay.Combat.PKKilledMessage, killer.Name)},
				)
			}
			playersByID[ch.ID] = ch
			updated[ch.ID] = ch
			if err := w.store.SaveCharacter(ch); err != nil {
				return err
			}
			result.CharacterDeaths = append(result.CharacterDeaths, ch)
		}
	}
	for id := range w.pendingCharacterDeaths {
		if _, ok := playersByID[id]; !ok {
			delete(w.pendingCharacterDeaths, id)
		}
	}
	return nil
}

func (w *World) Tick(players []PlayerSnapshot, now time.Time) (TickResult, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	previousActionNow := w.actionNow
	w.actionNow = now
	defer func() { w.actionNow = previousActionNow }()
	w.respawnLocked(now)
	result := TickResult{}
	w.replenishMonsterSpawnLocked(now, &result)
	w.cleanupDeadMonstersLocked(now)
	w.cleanupCastleDefenseSlotsLocked()
	remainingSpawns := w.pendingMonsterSpawns[:0]
	for _, pending := range w.pendingMonsterSpawns {
		if now.Before(pending.DueAt) {
			remainingSpawns = append(remainingSpawns, pending)
			continue
		}
		parent := w.monsters[pending.ParentID]
		before := make(map[string]struct{}, len(w.monsters))
		for id := range w.monsters {
			before[id] = struct{}{}
		}
		if parent == nil || !parent.Alive || !w.spawnChildMonsterLocked(parent, pending.ChildName, now) {
			continue
		}
		for _, child := range w.monsters {
			if child.ParentID == parent.ID && child.Alive && child.ID != parent.ID {
				if _, exists := before[child.ID]; exists {
					continue
				}
				result.SpawnedMonsters = append(result.SpawnedMonsters, *child)
			}
		}
	}
	w.pendingMonsterSpawns = remainingSpawns
	for id, event := range w.groundEvents {
		if now.After(event.StartAt.Add(event.Duration)) {
			delete(w.groundEvents, id)
			event.ClosedAt = now
			if w.closedGroundEvents == nil {
				w.closedGroundEvents = make(map[int32]SpellGroundEvent)
			}
			w.closedGroundEvents[id] = event
			result.GroundEventHides = append(result.GroundEventHides, id)
			result.GroundEventHideDetails = append(result.GroundEventHideDetails, event)
			continue
		}
		result.GroundEvents = append(result.GroundEvents, event)
	}
	for id, event := range w.closedGroundEvents {
		if !event.ClosedAt.IsZero() && !now.Before(event.ClosedAt.Add(5*time.Minute)) {
			delete(w.closedGroundEvents, id)
		}
	}
	sort.SliceStable(result.GroundEventHideDetails, func(i, j int) bool {
		left, right := result.GroundEventHideDetails[i], result.GroundEventHideDetails[j]
		if left.MapID != right.MapID {
			return left.MapID < right.MapID
		}
		if left.X != right.X {
			return left.X < right.X
		}
		if left.Y != right.Y {
			return left.Y < right.Y
		}
		return left.ID < right.ID
	})
	result.GroundEventHides = result.GroundEventHides[:0]
	for _, event := range result.GroundEventHideDetails {
		result.GroundEventHides = append(result.GroundEventHides, event.ID)
	}
	sort.Slice(result.GroundEvents, func(i, j int) bool {
		left, right := result.GroundEvents[i], result.GroundEvents[j]
		if left.MapID != right.MapID {
			return left.MapID < right.MapID
		}
		if left.X != right.X {
			return left.X < right.X
		}
		return left.Y < right.Y
	})
	updated := map[string]storage.Character{}
	playersByID := map[string]storage.Character{}
	for _, player := range players {
		ch := player.Character
		if ch.ID == "" {
			continue
		}
		playersByID[ch.ID] = ch
	}
	remainingAttacks := w.pendingMonsterAttacks[:0]
	for _, pending := range w.pendingMonsterAttacks {
		if now.Before(pending.DueAt) {
			remainingAttacks = append(remainingAttacks, pending)
			continue
		}
		mon := w.monsters[pending.MonsterID]
		if mon == nil || !mon.Alive {
			continue
		}
		if len(pending.Targets) > 0 {
			for _, pendingTarget := range pending.Targets {
				if pendingTarget.Monster {
					target := w.monsters[pendingTarget.ID]
					if target == nil || !target.Alive || target.MapID != mon.MapID || target.MasterID == "" {
						continue
					}
					caster := storage.Character{ID: mon.ID, MapID: mon.MapID, X: mon.X, Y: mon.Y}
					damage := w.monsterMagicDamageAfterDefenseLocked(target, pendingTarget.Damage)
					if damage <= 0 {
						continue
					}
					w.monsterMagicStruckLocked(target, now)
					hit, err := w.applyMonsterMagicDamageLocked(caster, target, damage, false)
					if err != nil {
						return TickResult{}, err
					}
					if hit.Damage > 0 {
						result.MonsterHits = append(result.MonsterHits, hit)
					}
					continue
				}
				target, ok := playersByID[pendingTarget.ID]
				if !ok || !w.monsterCanTargetCharacterLocked(mon, target) {
					continue
				}
				if pendingTarget.PhysicalDamage > 0 || pendingTarget.MagicDamage > 0 {
					physical := w.characterPhysicalDamageAfterDefenseLocked(&target, pendingTarget.PhysicalDamage)
					magical := w.characterMagicDamageAfterDefenseLocked(target, pendingTarget.MagicDamage, now)
					magical = applyCharacterMagicBubbleLocked(&target, magical, now)
					magical = w.applyCharacterMagicShieldLocked(&target, magical)
					if physical+magical <= 0 {
						continue
					}
					updatedTarget, hit, err := w.attackCharacterDirectDamageLocked(storage.Character{ID: mon.ID}, target, physical+magical)
					if err != nil {
						return TickResult{}, err
					}
					playersByID[pendingTarget.ID] = updatedTarget
					updated[pendingTarget.ID] = updatedTarget
					hit.ImpactDelay = pending.ImpactDelay
					if hit.Damage > 0 {
						result.CharacterHits = append(result.CharacterHits, hit)
					}
					continue
				}
				oldMagic := mon.UseMagic
				mon.UseMagic = true
				updatedTarget, hit, err := w.monsterAttackCharacterWithDamageLocked(mon, target, pendingTarget.Damage)
				mon.UseMagic = oldMagic
				if err != nil {
					return TickResult{}, err
				}
				playersByID[pendingTarget.ID] = updatedTarget
				updated[pendingTarget.ID] = updatedTarget
				if pending.ImpactDelay > 0 {
					hit.ImpactDelay = pending.ImpactDelay
					hit.Magic = true
				}
				if hit.Damage > 0 {
					result.CharacterHits = append(result.CharacterHits, hit)
				}
			}
			continue
		}
		for _, targetID := range pending.TargetIDs {
			target, ok := playersByID[targetID]
			if !ok || !w.monsterCanTargetCharacterLocked(mon, target) {
				continue
			}
			oldMagic := mon.UseMagic
			mon.UseMagic = true
			updatedTarget, hit, err := w.monsterAttackCharacterWithDamageLocked(mon, target, pending.Damage)
			mon.UseMagic = oldMagic
			if err != nil {
				return TickResult{}, err
			}
			playersByID[targetID] = updatedTarget
			updated[targetID] = updatedTarget
			if pending.ImpactDelay > 0 {
				hit.ImpactDelay = pending.ImpactDelay
				hit.Magic = true
			}
			if hit.Damage > 0 {
				result.CharacterHits = append(result.CharacterHits, hit)
			}
		}
		for _, pendingTarget := range pending.MonsterTargets {
			target := w.monsters[pendingTarget.ID]
			if target == nil || !target.Alive || target.MapID != mon.MapID || target.ID == mon.ID || target.MasterID == "" {
				continue
			}
			caster := storage.Character{ID: mon.ID, MapID: mon.MapID, X: mon.X, Y: mon.Y}
			damage := w.monsterMagicDamageAfterDefenseLocked(target, pendingTarget.Damage)
			if damage <= 0 {
				continue
			}
			w.monsterMagicStruckLocked(target, now)
			hit, err := w.applyMonsterMagicDamageLocked(caster, target, damage, false)
			if err != nil {
				return TickResult{}, err
			}
			if hit.Damage > 0 {
				result.MonsterHits = append(result.MonsterHits, hit)
			}
		}
	}
	w.pendingMonsterAttacks = remainingAttacks
	if err := w.applyPendingSpellTicksLocked(&result, playersByID, updated, now); err != nil {
		return TickResult{}, err
	}
	for id, entity := range w.data.NPCs.Entities {
		if !npc.IsTrainer(entity) {
			continue
		}
		if summary, ok := w.flushNPCTrainingLocked(id, now); ok {
			result.NPCTrainingHits = append(result.NPCTrainingHits, summary)
			result.OrderedSpellEvents = append(result.OrderedSpellEvents, OrderedSpellEvent{Kind: OrderedSpellEventNPCTraining, NPCTrainingHit: summary})
		}
	}
	if err := w.settlePendingCharacterDeathsLocked(players, playersByID, updated, &result, now); err != nil {
		return TickResult{}, err
	}
	for _, player := range players {
		ch, ok := playersByID[player.Character.ID]
		if !ok {
			continue
		}
		alive := ch.HP > 0
		statusCharacter := ch
		statusCharacter.ShowHPUntil = 0
		originalStatus := characterStatus(statusCharacter, now, true)
		var next storage.Character
		var changed bool
		if alive {
			naturalMPChanged := w.applyCharacterNaturalSpellTickLocked(&ch, now)
			if naturalMPChanged {
				updated[ch.ID] = ch
			}
			next, changed = core.ApplyQueuedRecovery(ch, now)
			if changed {
				ch = next
				updated[ch.ID] = ch
			}
		}
		next, changed = w.applyCharacterPoisonTickLocked(ch, now)
		if changed {
			ch = next
			if player.Character.HP > 0 && ch.HP <= 0 {
				ch.IncHealth = 0
				ch.IncSpell = 0
				ch.IncHealing = 0
				w.deferCharacterDeathLocked(ch)
			}
			updated[ch.ID] = ch
		}
		next, changed = w.applyCharacterStealthTickLocked(ch, now)
		if changed {
			ch = next
			updated[ch.ID] = ch
		}
		next, changed = w.applyCharacterProtectionTickLocked(ch, now)
		if changed {
			ch = next
			updated[ch.ID] = ch
			if ch.DefenceUpUntil != player.Character.DefenceUpUntil || ch.MagDefenceUpUntil != player.Character.MagDefenceUpUntil {
				result.AbilityRefreshCharacters = append(result.AbilityRefreshCharacters, ch)
			}
		}
		if ch.ParalyzedUntil > 0 && now.UnixNano() > ch.ParalyzedUntil {
			ch.ParalyzedUntil = 0
			updated[ch.ID] = ch
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
		statusCharacter = ch
		statusCharacter.ShowHPUntil = 0
		if originalStatus != characterStatus(statusCharacter, now, true) {
			result.StatusRefreshCharacters = append(result.StatusRefreshCharacters, ch)
			result.OrderedSpellEvents = append(result.OrderedSpellEvents, OrderedSpellEvent{
				Kind:      OrderedSpellEventCharacterStatus,
				Character: ch,
			})
		}
		playersByID[ch.ID] = ch
	}
	monsters := make([]*Monster, 0, len(w.monsters))
	for _, mon := range w.monsters {
		monsters = append(monsters, mon)
	}
	sort.SliceStable(monsters, func(i, j int) bool {
		if monsters[i].X != monsters[j].X {
			return monsters[i].X < monsters[j].X
		}
		if monsters[i].Y != monsters[j].Y {
			return monsters[i].Y < monsters[j].Y
		}
		return monsters[i].ObjectOrder < monsters[j].ObjectOrder
	})
	for _, mon := range monsters {
		if w.applyMonsterShowHPOpenTickLocked(mon, now) {
			result.ShowHPOpenedMonsters = append(result.ShowHPOpenedMonsters, *mon)
		}
		if w.applyMonsterShowHPTickLocked(mon, now) {
			result.ShowHPExpiredMonsters = append(result.ShowHPExpiredMonsters, *mon)
		}
		if !mon.TransparentUntil.IsZero() && now.After(mon.TransparentUntil) {
			mon.TransparentUntil = time.Time{}
			result.StatusRefreshMonsters = append(result.StatusRefreshMonsters, *mon)
		}
		if w.applyMonsterHealingTickLocked(mon, now) {
			result.AffectedMonsters = append(result.AffectedMonsters, *mon)
		}
	}
	fireMonsterHits, fireCharacterHits, fireUpdated := w.applyFireWallTickLocked(playersByID, now)
	result.NPCTrainingHits = append(result.NPCTrainingHits, w.pendingNPCTraining...)
	for _, hit := range w.pendingNPCTraining {
		result.OrderedSpellEvents = append(result.OrderedSpellEvents, OrderedSpellEvent{Kind: OrderedSpellEventNPCTraining, NPCTrainingHit: hit})
	}
	w.pendingNPCTraining = w.pendingNPCTraining[:0]
	result.MonsterHits = append(result.MonsterHits, fireMonsterHits...)
	result.CharacterHits = append(result.CharacterHits, fireCharacterHits...)
	for _, ch := range fireUpdated {
		playersByID[ch.ID] = ch
		updated[ch.ID] = ch
	}
	for _, mon := range monsters {
		if mon.LastHitterID != "" && !mon.LastHitterAt.IsZero() && (now.Sub(mon.LastHitterAt) > 30*time.Second || mon.LastHitterID != "" && !w.monsterAttackerAliveLocked(mon.LastHitterID, playersByID)) {
			mon.LastHitterID = ""
			mon.LastHitterAt = time.Time{}
		}
		if mon.ExpHitterID != "" && !mon.ExpHitterAt.IsZero() && (now.Sub(mon.ExpHitterAt) > 6*time.Second || !w.monsterAttackerAliveLocked(mon.ExpHitterID, playersByID)) {
			mon.ExpHitterID = ""
			mon.ExpHitterAt = time.Time{}
		}
		if mon.PendingDeath {
			killerID := mon.ExpHitterID
			if killerID == "" {
				killerID = mon.LastHitterID
			}
			if killerID == "" && mon.PoisonSourceID != "" {
				killerID = mon.DeathHitterID
			}
			killer, killerOK := w.monsterExperienceOwnerLocked(killerID, playersByID, now)
			if !killerOK {
				killer = storage.Character{}
			}
			groupMembers := w.experienceGroupMembersLocked(killer, playersByID)
			death, err := w.killMonsterWithDamageLocked(killer, mon, 0, groupMembers)
			if err != nil {
				return TickResult{}, err
			}
			result.MonsterDeaths = append(result.MonsterDeaths, death)
			if death.Character.ID != "" {
				playersByID[death.Character.ID] = death.Character
				updated[death.Character.ID] = death.Character
			}
			if death.Experience > 0 || death.LevelUp {
				result.SpellExperience = append(result.SpellExperience, SpellExperience{
					CharacterID: killer.ID, Experience: death.Experience,
					CurrentExp: death.CurrentExp, LevelUp: death.LevelUp,
					Character: death.Character,
				})
			}
			result.SpellExperience = append(result.SpellExperience, death.GroupExperiences...)
			continue
		}
		if !mon.Alive {
			continue
		}
		controlled := false
		if mon.MasterID != "" && !mon.MasterExpiresAt.IsZero() && now.After(mon.MasterExpiresAt) {
			mon.MasterID = ""
			mon.MasterName = ""
			mon.MasterExpiresAt = time.Time{}
			mon.NoTame = false
			mon.HP /= 10
			result.NameMonsters = append(result.NameMonsters, *mon)
		}
		if !mon.CrazyUntil.IsZero() && now.After(mon.CrazyUntil) {
			mon.CrazyUntil = time.Time{}
			result.NameColorMonsters = append(result.NameColorMonsters, *mon)
		}
		if !mon.HolySeizeUntil.IsZero() {
			if !now.After(mon.HolySeizeUntil) {
				controlled = true
			} else {
				mon.HolySeizeUntil = time.Time{}
				result.NameColorMonsters = append(result.NameColorMonsters, *mon)
			}
		}
		if !mon.ParalyzedUntil.IsZero() {
			if !now.After(mon.ParalyzedUntil) {
				controlled = true
			} else {
				mon.ParalyzedUntil = time.Time{}
				result.StatusRefreshMonsters = append(result.StatusRefreshMonsters, *mon)
			}
		}
		if w.applyMonsterProtectionTickLocked(mon, now) {
			result.StatusRefreshMonsters = append(result.StatusRefreshMonsters, *mon)
		}
		if controlled {
			continue
		}
		wasAlive := mon.Alive
		var trace *MonsterTickTrace
		if w.monsterTraceEnabled {
			w.monsterTraceTick++
			trace = &MonsterTickTrace{Tick: w.monsterTraceTick, NowMS: now.UnixMilli(), MonsterID: mon.ID, StateBefore: monsterTraceState(mon)}
			w.monsterTraceCurrent = trace
		}
		events, hits, chars, err := w.tickMonsterLocked(mon, playersByID, now)
		if trace != nil {
			trace.StateAfter = monsterTraceState(mon)
			trace.Actions = append(trace.Actions, events...)
			trace.Decision = monsterTraceDecision(trace)
			result.MonsterTraces = append(result.MonsterTraces, *trace)
			w.monsterTraceCurrent = nil
		}
		if err != nil {
			return TickResult{}, err
		}
		result.MonsterActions = append(result.MonsterActions, events...)
		result.CharacterHits = append(result.CharacterHits, hits...)
		for _, ch := range chars {
			playersByID[ch.ID] = ch
			updated[ch.ID] = ch
		}
		if wasAlive && !mon.Alive {
			result.MonsterDeaths = append(result.MonsterDeaths, AttackResult{
				MonsterID: mon.ID, MonsterHP: 0, MonsterMaxHP: mon.MaxHP,
				MonsterRaceImg: mon.RaceImg, MonsterWeapon: mon.MonsterWeapon, MonsterAppr: mon.Appr,
				MonsterX: mon.X, MonsterY: mon.Y, MonsterMapID: mon.MapID, MonsterDir: mon.Dir,
				MonsterStatus: MonsterStatus(*mon, now), Dead: true,
			})
		}
		if !mon.Alive {
			continue
		}
		previousStatus := MonsterStatus(*mon, now)
		poisonHits, killed, err := w.applyMonsterPoisonTickLocked(mon, playersByID, now)
		if err != nil {
			return TickResult{}, err
		}
		if MonsterStatus(*mon, now) != previousStatus {
			result.StatusRefreshMonsters = append(result.StatusRefreshMonsters, *mon)
		}
		result.MonsterHits = append(result.MonsterHits, poisonHits...)
		if killed {
			continue
		}
	}
	updatedIDs := make([]string, 0, len(updated))
	for id := range updated {
		updatedIDs = append(updatedIDs, id)
	}
	sort.Strings(updatedIDs)
	for _, id := range updatedIDs {
		result.Characters = append(result.Characters, updated[id])
	}
	return result, nil
}

func (w *World) monsterAttackerAliveLocked(id string, players map[string]storage.Character) bool {
	if attacker, ok := players[id]; ok {
		return attacker.HP > 0
	}
	if attacker, ok := w.monsters[id]; ok {
		return attacker.Alive && attacker.HP > 0
	}
	return false
}

func (w *World) monsterExperienceOwnerLocked(attackerID string, players map[string]storage.Character, now time.Time) (storage.Character, bool) {
	if attacker, ok := players[attackerID]; ok {
		return attacker, true
	}
	attacker, ok := w.monsters[attackerID]
	if !ok || attacker.MasterID == "" {
		return storage.Character{}, false
	}
	owner, ok := players[attacker.MasterID]
	if !ok || owner.HP <= 0 {
		return storage.Character{}, false
	}
	if !attacker.MasterExpiresAt.IsZero() && now.After(attacker.MasterExpiresAt) {
		return storage.Character{}, false
	}
	return owner, true
}

func (w *World) applyCharacterNaturalSpellTickLocked(ch *storage.Character, now time.Time) bool {
	if ch == nil || ch.HP <= 0 {
		return false
	}
	if ch.HealthTickAt == 0 {
		ch.HealthTickAt = now.UnixMilli()
	}
	if ch.SpellTickAt == 0 {
		ch.SpellTickAt = now.UnixMilli()
	}
	nowMS := now.UnixMilli()
	healthElapsed := nowMS - ch.HealthTickAt
	spellElapsed := nowMS - ch.SpellTickAt
	changed := false
	if healthElapsed > 0 {
		ch.HealthTickAt = nowMS
		ch.HealthTick += int(healthElapsed / 20)
	}
	if spellElapsed > 0 {
		ch.SpellTickAt = nowMS
		ch.SpellTick += int(spellElapsed / 20)
	}
	healthFillMS := w.gameplay.Recovery.HealthFillTimeMS
	if healthFillMS <= 0 {
		healthFillMS = 300
	}
	spellFillMS := w.gameplay.Recovery.SpellFillTimeMS
	if spellFillMS <= 0 {
		spellFillMS = 800
	}
	if ch.HP < ch.MaxHP && ch.HealthTick >= healthFillMS {
		ch.HealthTick = 0
		gain := ch.MaxHP/75 + 1
		if ch.HP+gain > ch.MaxHP {
			gain = ch.MaxHP - ch.HP
		}
		if gain > 0 {
			ch.HP += gain
			changed = true
		}
	} else if ch.HealthTick >= healthFillMS {
		ch.HealthTick = 0
	}
	if ch.MP < ch.MaxMP && ch.SpellTick >= spellFillMS {
		ch.SpellTick = 0
		gain := ch.MaxMP/18 + 1
		if ch.MP+gain > ch.MaxMP {
			gain = ch.MaxMP - ch.MP
		}
		if gain > 0 {
			ch.MP += gain
			changed = true
		}
	} else if ch.SpellTick >= spellFillMS {
		ch.SpellTick = 0
	}
	if ch.HealthTick < -healthFillMS && ch.HP > 1 {
		ch.HP--
		ch.HealthTick += healthFillMS
		changed = true
	}
	return changed
}

func characterListFromMap(players map[string]storage.Character) []storage.Character {
	list := make([]storage.Character, 0, len(players))
	for _, player := range players {
		list = append(list, player)
	}
	return list
}

func (w *World) applyPendingSpellTicksLocked(result *TickResult, players, updated map[string]storage.Character, now time.Time) (err error) {
	if len(w.pendingSpells) == 0 {
		return nil
	}
	originalMonsters := make(map[string]*Monster, len(w.monsters))
	originalMonsterValues := make(map[string]Monster, len(w.monsters))
	for id, mon := range w.monsters {
		if mon == nil {
			continue
		}
		originalMonsters[id] = mon
		originalMonsterValues[id] = *mon
	}
	rollbackMonsters := func() {
		for id := range w.monsters {
			original, ok := originalMonsters[id]
			if !ok {
				delete(w.monsters, id)
				continue
			}
			*original = originalMonsterValues[id]
			w.monsters[id] = original
		}
		for id, original := range originalMonsters {
			if _, ok := w.monsters[id]; !ok {
				w.monsters[id] = original
			}
		}
	}
	defer func() {
		if err != nil {
			rollbackMonsters()
		}
	}()
	sort.SliceStable(w.pendingSpells, func(i, j int) bool {
		left, right := w.pendingSpells[i], w.pendingSpells[j]
		leftTarget, rightTarget := pendingSpellTargetKey(left), pendingSpellTargetKey(right)
		if leftTarget == rightTarget {
			return false
		}
		leftOrder, leftKnown := w.pendingSpellTargetOrderLocked(left, players)
		rightOrder, rightKnown := w.pendingSpellTargetOrderLocked(right, players)
		if leftKnown && rightKnown && leftOrder != rightOrder {
			return leftOrder < rightOrder
		}
		if leftKnown != rightKnown {
			return leftKnown
		}
		return left.DueAt.Before(right.DueAt)
	})
	remaining := make([]pendingSpell, 0, len(w.pendingSpells))
	for _, pending := range w.pendingSpells {
		if now.Before(pending.DueAt) {
			remaining = append(remaining, pending)
			continue
		}
		if pending.ShowHealthDuration > 0 {
			showUntil := pending.ShowHealthStartedAt.Add(pending.ShowHealthDuration).UnixNano()
			if pending.TargetCharacterID != "" {
				target, ok := players[pending.TargetCharacterID]
				if !ok {
					continue
				}
				target.ShowHPOpenAt = 0
				target.ShowHPDuration = 0
				target.ShowHPUntil = showUntil
				players[target.ID] = target
				updated[target.ID] = target
				if err := w.store.SaveCharacter(target); err != nil {
					return err
				}
				result.ShowHPOpenedCharacters = append(result.ShowHPOpenedCharacters, target)
				result.OrderedSpellEvents = append(result.OrderedSpellEvents, OrderedSpellEvent{Kind: OrderedSpellEventCharacterOpenHealth, Character: target})
				continue
			}
			mon := w.monsters[pending.TargetMonsterID]
			if mon == nil {
				continue
			}
			mon.ShowHPOpenAt = 0
			mon.ShowHPDuration = 0
			mon.ShowHPUntil = showUntil
			result.ShowHPOpenedMonsters = append(result.ShowHPOpenedMonsters, *mon)
			result.OrderedSpellEvents = append(result.OrderedSpellEvents, OrderedSpellEvent{Kind: OrderedSpellEventMonsterOpenHealth, Monster: *mon})
			continue
		}
		if !pending.TransparentUntil.IsZero() || pending.TransparentDuration > 0 {
			transparentUntil := pending.TransparentUntil
			if pending.TransparentDuration > 0 {
				transparentUntil = now.Add(pending.TransparentDuration)
			}
			if pending.TargetCharacterID != "" {
				target, ok := players[pending.TargetCharacterID]
				if !ok {
					continue
				}
				if target.TransparentUntil > 0 {
					continue
				}
				if setCharacterTransparentLocked(&target, transparentUntil) {
					w.breakNearbyMonsterTargetsForStealthLocked(target)
					if err := w.store.SaveCharacter(target); err != nil {
						return err
					}
					players[target.ID] = target
					updated[target.ID] = target
					result.StatusRefreshCharacters = append(result.StatusRefreshCharacters, target)
					result.OrderedStatusRefreshes = append(result.OrderedStatusRefreshes, StatusRefreshEvent{Character: &target})
					result.OrderedSpellEvents = append(result.OrderedSpellEvents, OrderedSpellEvent{Kind: OrderedSpellEventCharacterStatus, Character: target})
				}
				continue
			}
			mon := w.monsters[pending.TargetMonsterID]
			if mon == nil {
				continue
			}
			if mon.TransparentUntil.IsZero() {
				mon.TransparentUntil = transparentUntil
				w.breakNearbyMonsterTargetsForMonsterStealthLocked(mon)
				result.StatusRefreshMonsters = append(result.StatusRefreshMonsters, *mon)
				result.OrderedStatusRefreshes = append(result.OrderedStatusRefreshes, StatusRefreshEvent{Monster: mon})
				result.OrderedSpellEvents = append(result.OrderedSpellEvents, OrderedSpellEvent{Kind: OrderedSpellEventMonsterStatus, Monster: *mon})
			}
			continue
		}
		if pending.PoisonDuration > 0 || pending.PoisonHealth || pending.PoisonArmor || pending.ParalysisDuration > 0 {
			poisonUntil := pending.PoisonUntil
			if pending.PoisonDuration > 0 {
				poisonUntil = now.Add(pending.PoisonDuration)
			}
			if pending.TargetCharacterID != "" {
				target, ok := players[pending.TargetCharacterID]
				if !ok {
					continue
				}
				if caster, ok := players[pending.CasterID]; ok && caster.ID != target.ID && w.isProperCharacterTargetLocked(target, caster) {
					target.TargetID = caster.ID
					target.LastHitterID = caster.ID
					target.LastHitterAt = now.UnixNano()
					if !caster.PKFlag {
						caster.PKFlag = true
						caster.PKFlagUntil = now.Add(60 * time.Second).UnixNano()
						result.NameColorCharacters = append(result.NameColorCharacters, caster)
					}
					players[caster.ID] = caster
					updated[caster.ID] = caster
					if err := w.store.SaveCharacter(caster); err != nil {
						return err
					}
				}
				previousStatus := characterStatus(target, now, false)
				if pending.PoisonHealth {
					setCharacterHealthPoisonLocked(&target, pending.PoisonHealthLevel, poisonUntil, now)
				}
				if pending.PoisonArmor {
					setCharacterArmorPoisonLocked(&target, poisonUntil, now)
				}
				if pending.ParalysisDuration > 0 {
					paralyzedUntil := now.Add(pending.ParalysisDuration).UnixNano()
					if paralyzedUntil > target.ParalyzedUntil {
						target.ParalyzedUntil = paralyzedUntil
					}
				}
				if err := w.store.SaveCharacter(target); err != nil {
					return err
				}
				if characterStatus(target, now, false) != previousStatus {
					result.StatusRefreshCharacters = append(result.StatusRefreshCharacters, target)
					result.OrderedStatusRefreshes = append(result.OrderedStatusRefreshes, StatusRefreshEvent{Character: &target})
					result.OrderedSpellEvents = append(result.OrderedSpellEvents, OrderedSpellEvent{Kind: OrderedSpellEventCharacterStatus, Character: target})
				}
				if pending.PoisonHealth || pending.PoisonArmor || pending.PoisonNotification {
					seconds := 0
					if pending.PoisonDuration > 0 {
						seconds = int(pending.PoisonDuration / time.Second)
					} else if !poisonUntil.IsZero() {
						seconds = int(poisonUntil.Sub(now) / time.Second)
					}
					if pending.PoisonNotification && pending.ParalysisDuration > 0 {
						seconds = int(pending.ParalysisDuration / time.Second)
					}
					points := pending.PoisonPoint
					if points == 0 {
						points = int(pending.PoisonHealthLevel)
					}
					if pending.PoisonNotification {
						points = pending.PoisonPoint
					}
					notification := PoisonNotification{Character: target, Seconds: seconds, Points: points}
					result.PoisonNotifications = append(result.PoisonNotifications, notification)
					if pending.PoisonNotification {
						result.OrderedSpellEvents = append(result.OrderedSpellEvents, OrderedSpellEvent{Kind: OrderedSpellEventPoisonNotification, PoisonNotification: notification})
					}
				}
				players[target.ID] = target
				updated[target.ID] = target
				if pending.PoisonHealth || pending.PoisonArmor {
					result.AffectedCharacters = append(result.AffectedCharacters, target)
				}
				continue
			}
			mon := w.monsters[pending.TargetMonsterID]
			if mon == nil {
				continue
			}
			previousStatus := MonsterStatus(*mon, now)
			if caster, ok := players[pending.CasterID]; ok && w.isProperDelayedMonsterPoisonTargetLocked(caster, characterListFromMap(players), mon) {
				mon.TargetCharacterID = pending.CasterID
			}
			if pending.PoisonHealth {
				setMonsterHealthPoisonLocked(mon, pending.PoisonHealthLevel, poisonUntil, pending.CasterID, now)
			}
			if pending.PoisonArmor {
				setMonsterArmorPoisonLocked(mon, poisonUntil, now)
			}
			if pending.ParalysisDuration > 0 {
				if caster, ok := players[pending.CasterID]; ok {
					target := *mon
					target.Alive = true
					if w.isProperMonsterTargetLocked(caster, characterListFromMap(players), &target) {
						w.setMonsterLastHitterAtLocked(mon, pending.CasterID, now)
					}
				}
				paralyzedUntil := now.Add(pending.ParalysisDuration)
				if paralyzedUntil.After(mon.ParalyzedUntil) {
					mon.ParalyzedUntil = paralyzedUntil
				}
			}
			if MonsterStatus(*mon, now) != previousStatus {
				result.StatusRefreshMonsters = append(result.StatusRefreshMonsters, *mon)
				result.OrderedStatusRefreshes = append(result.OrderedStatusRefreshes, StatusRefreshEvent{Monster: mon})
				result.OrderedSpellEvents = append(result.OrderedSpellEvents, OrderedSpellEvent{Kind: OrderedSpellEventMonsterStatus, Monster: *mon})
			}
			continue
		}
		if pending.Healing > 0 {
			if pending.TargetCharacterID != "" {
				target, ok := players[pending.TargetCharacterID]
				if !ok {
					continue
				}
				updatedTarget := target
				if updatedTarget.IncHealing+pending.Healing < 300 {
					updatedTarget.IncHealing += pending.Healing
					updatedTarget.PerHealing = 5
				} else {
					updatedTarget.IncHealing = 300
				}
				if updatedTarget.IncHealing != target.IncHealing {
					if err := w.store.SaveCharacter(updatedTarget); err != nil {
						return err
					}
					players[updatedTarget.ID] = updatedTarget
					updated[updatedTarget.ID] = updatedTarget
					result.HealingCharacters = append(result.HealingCharacters, updatedTarget.ID)
				}
				continue
			}
			mon := w.monsters[pending.TargetMonsterID]
			if mon == nil {
				continue
			}
			if mon.IncHealing+pending.Healing < 300 {
				mon.IncHealing += pending.Healing
				mon.PerHealing = 5
			} else {
				mon.IncHealing = 300
			}
			continue
		}
		caster, ok := players[pending.CasterID]
		if !ok {
			continue
		}
		setCasterTarget := func(targetID string) error {
			if !pending.SetCasterTarget || targetID == "" {
				return nil
			}
			caster.TargetID = targetID
			players[caster.ID] = caster
			updated[caster.ID] = caster
			return w.store.SaveCharacter(caster)
		}
		if pending.CharacterDamage {
			target, ok := players[pending.TargetCharacterID]
			if !ok {
				continue
			}
			if pending.SingleMagicStrike {
				var damage int
				target, damage = w.prepareCharacterMagicDamageLocked(target, pending.Damage, now)
				if damage <= 0 {
					players[target.ID] = target
					updated[target.ID] = target
					if err := w.store.SaveCharacter(target); err != nil {
						return err
					}
					continue
				}
				updatedTarget, hit, err := w.applyPreparedCharacterMagicDamageLocked(caster, target, damage)
				if err != nil {
					return err
				}
				players[updatedTarget.ID] = updatedTarget
				updated[updatedTarget.ID] = updatedTarget
				if hit.Damage > 0 {
					result.CharacterHits = append(result.CharacterHits, hit)
					result.OrderedSpellEvents = append(result.OrderedSpellEvents, OrderedSpellEvent{Kind: OrderedSpellEventCharacterHit, CharacterHit: hit})
				}
				continue
			}
			if pending.CharacterBubbleAfter != 0 && target.BubbleDefenceUntil == pending.CharacterBubbleBefore {
				target.BubbleDefenceUntil = pending.CharacterBubbleAfter
				target.BubbleDefenceLevel = pending.CharacterBubbleLevel
			}
			precheckDamage := pending.Damage
			if !pending.CharacterDamagePrepared {
				precheckDamage = w.characterMagicDamageAfterDefenseLocked(target, pending.Damage, now)
				precheckDamage = applyCharacterMagicBubbleLocked(&target, precheckDamage, now)
			}
			if precheckDamage > 0 {
				if err := setCasterTarget(target.ID); err != nil {
					return err
				}
			}
			targetRange := pending.TargetRange
			if targetRange == 0 {
				targetRange = 2
			}
			if abs(target.X-pending.TargetX) > targetRange || abs(target.Y-pending.TargetY) > targetRange {
				players[target.ID] = target
				updated[target.ID] = target
				if err := w.store.SaveCharacter(target); err != nil {
					return err
				}
				continue
			}
			if precheckDamage <= 0 {
				players[target.ID] = target
				updated[target.ID] = target
				if err := w.store.SaveCharacter(target); err != nil {
					return err
				}
				continue
			}
			if !pending.CharacterDamagePrepared {
				var finalDamage int
				target, finalDamage = w.prepareCharacterMagicDamageLocked(target, pending.Damage, now)
				if finalDamage <= 0 {
					players[target.ID] = target
					updated[target.ID] = target
					if err := w.store.SaveCharacter(target); err != nil {
						return err
					}
					continue
				}
				precheckDamage = finalDamage
			}
			markCasterPK := w.isProperCharacterTargetLocked(caster, target)
			updatedTarget, hit, err := w.applyPreparedCharacterMagicDamageLocked(caster, target, precheckDamage)
			if err != nil {
				return err
			}
			players[updatedTarget.ID] = updatedTarget
			updated[updatedTarget.ID] = updatedTarget
			if hit.Damage <= 0 {
				continue
			}
			if markCasterPK {
				wasPKFlagged := caster.PKFlag
				caster.PKFlag = true
				caster.PKFlagUntil = now.Add(60 * time.Second).UnixNano()
				players[caster.ID] = caster
				updated[caster.ID] = caster
				if err := w.store.SaveCharacter(caster); err != nil {
					return err
				}
				if !wasPKFlagged {
					result.NameColorCharacters = append(result.NameColorCharacters, caster)
				}
			}
			result.CharacterHits = append(result.CharacterHits, hit)
			result.OrderedSpellEvents = append(result.OrderedSpellEvents, OrderedSpellEvent{Kind: OrderedSpellEventCharacterHit, CharacterHit: hit})
			continue
		}
		if pending.TargetNPCID != "" {
			entity, ok := w.data.NPCs.Entities[pending.TargetNPCID]
			targetRange := pending.TargetRange
			if targetRange == 0 {
				targetRange = 2
			}
			if !ok || !npc.IsTrainer(entity) || entity.Hidden || entity.MapID != caster.MapID || abs(entity.X-pending.TargetX) > targetRange || abs(entity.Y-pending.TargetY) > targetRange || !w.magCanHitTargetLocked(caster.MapID, caster.X, caster.Y, entity.X, entity.Y) {
				continue
			}
			hit, err := w.applyNPCTrainingHitLocked(entity.ID, caster.ID, pending.Damage, true, now)
			if err != nil {
				return err
			}
			if hit.Damage > 0 {
				result.NPCTrainingHits = append(result.NPCTrainingHits, hit)
				result.OrderedSpellEvents = append(result.OrderedSpellEvents, OrderedSpellEvent{Kind: OrderedSpellEventNPCTraining, NPCTrainingHit: hit})
			}
			continue
		}
		mon := w.monsters[pending.TargetMonsterID]
		if mon == nil {
			continue
		}
		if pending.SingleMagicStrike {
			damage := w.monsterMagicDamageAfterDefenseLocked(mon, pending.Damage)
			if damage <= 0 {
				continue
			}
			w.monsterMagicStruckLocked(mon, now)
			hit, err := w.applyMonsterMagicDamageLocked(caster, mon, damage, false)
			if err != nil {
				return err
			}
			if hit.Damage > 0 {
				result.MonsterHits = append(result.MonsterHits, hit)
				result.OrderedSpellEvents = append(result.OrderedSpellEvents, OrderedSpellEvent{Kind: OrderedSpellEventMonsterHit, MonsterHit: hit})
			}
			continue
		}
		precheckDamage := pending.Damage
		precheckDamage = w.monsterMagicDamageAfterDefenseLocked(mon, pending.Damage)
		if mon.Undead > 0 && precheckDamage > 0 {
			precheckDamage += w.combatStatsLocked(caster).Undead
		}
		if precheckDamage <= 0 {
			continue
		}
		if err := setCasterTarget(mon.ID); err != nil {
			return err
		}
		targetRange := pending.TargetRange
		if targetRange == 0 {
			targetRange = 2
		}
		if abs(mon.X-pending.TargetX) > targetRange || abs(mon.Y-pending.TargetY) > targetRange {
			continue
		}
		damage := pending.Damage
		if mon.Race >= 50 {
			damage = referenceRound(float64(pending.Damage) / 1.2)
		}
		w.monsterMagicStruckLocked(mon, now)
		damage = w.monsterMagicDamageAfterDefenseLocked(mon, damage)
		if mon.Undead > 0 && damage > 0 {
			damage += w.combatStatsLocked(caster).Undead
		}
		hit, err := w.applyMonsterMagicDamageLocked(caster, mon, damage, false)
		if err != nil {
			return err
		}
		if hit.Damage <= 0 {
			continue
		}
		result.MonsterHits = append(result.MonsterHits, hit)
		result.OrderedSpellEvents = append(result.OrderedSpellEvents, OrderedSpellEvent{Kind: OrderedSpellEventMonsterHit, MonsterHit: hit})
		players[hit.Character.ID] = hit.Character
		updated[hit.Character.ID] = hit.Character
		if err := w.store.SaveCharacter(hit.Character); err != nil {
			return err
		}
		result.SpellExperience = append(result.SpellExperience, SpellExperience{CharacterID: hit.Character.ID, Experience: hit.Experience, CurrentExp: hit.CurrentExp, LevelUp: hit.LevelUp, Character: hit.Character})
	}
	w.pendingSpells = remaining
	return nil
}

func pendingSpellTargetKey(pending pendingSpell) string {
	if pending.TargetCharacterID != "" {
		return "character:" + pending.TargetCharacterID
	}
	if pending.TargetMonsterID != "" {
		return "monster:" + pending.TargetMonsterID
	}
	return ""
}

func (w *World) pendingSpellTargetOrderLocked(pending pendingSpell, players map[string]storage.Character) (uint64, bool) {
	if pending.TargetCharacterID != "" {
		character, ok := players[pending.TargetCharacterID]
		return character.ObjectOrder, ok && character.ObjectOrder != 0
	}
	if pending.TargetMonsterID != "" {
		monster, ok := w.monsters[pending.TargetMonsterID]
		if !ok || monster == nil {
			return 0, false
		}
		return monster.ObjectOrder, monster.ObjectOrder != 0
	}
	return 0, false
}

func (w *World) applyMonsterHealingTickLocked(mon *Monster, now time.Time) bool {
	if mon == nil || !mon.Alive || (mon.IncHealth <= 0 && mon.IncSpell <= 0 && mon.IncHealing <= 0) {
		return false
	}
	interval := core.RecoveryInterval(mon.Level)
	nextAt := time.UnixMilli(mon.IncHealthSpellAt)
	if mon.IncHealthSpellAt != 0 && now.Before(nextAt.Add(interval)) {
		return false
	}
	overrun := now.Sub(nextAt) - interval
	if overrun < 0 {
		overrun = 0
	}
	if overrun > 200*time.Millisecond {
		overrun = 200 * time.Millisecond
	}
	perHealing := mon.PerHealing
	if perHealing <= 0 {
		perHealing = 1
	}
	perHealth := mon.PerHealth
	if perHealth <= 0 {
		perHealth = 1
	}
	perSpell := mon.PerSpell
	if perSpell <= 0 {
		perSpell = 1
	}
	healing := mon.IncHealing
	if healing > perHealing {
		healing = perHealing
	}
	hp := mon.IncHealth
	if hp > perHealth {
		hp = perHealth
	}
	mp := mon.IncSpell
	if mp > perSpell {
		mp = perSpell
	}
	healed := core.ApplyVitalDelta(storage.Character{HP: mon.HP, MaxHP: mon.MaxHP, MP: mon.MP, MaxMP: mon.MaxMP}, hp+healing, mp)
	mon.IncHealth -= hp
	mon.IncSpell -= mp
	mon.IncHealing -= healing
	mon.IncHealthSpellAt = now.Add(overrun).UnixMilli()
	mon.PerHealth = mon.Level/10 + 5
	mon.PerSpell = mon.Level/10 + 5
	mon.PerHealing = 5
	if healed.Changed {
		mon.HP = healed.Character.HP
		mon.MP = healed.Character.MP
	}
	if mon.HP >= mon.MaxHP {
		mon.IncHealth = 0
		mon.IncHealing = 0
	}
	if mon.MP >= mon.MaxMP {
		mon.IncSpell = 0
	}
	return healed.Changed
}

func (w *World) tickMonsterLocked(mon *Monster, players map[string]storage.Character, now time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	if now.Year() >= 2000 && mon.Spawn.MapID != "" && mon.NextSearchAt.IsZero() {
		mon.NextSearchAt = now.Add(time.Duration(w.monsterInitialSearchDelayMSLocked(mon)) * time.Millisecond)
	}
	if mon.RunIntervalMS > 0 && !mon.NextRunAt.IsZero() && now.Before(mon.NextRunAt) {
		return nil, nil, nil, nil
	}
	if mon.RunIntervalMS > 0 {
		mon.NextRunAt = now.Add(time.Duration(mon.RunIntervalMS) * time.Millisecond)
	}
	if mon.MasterID != "" {
		return w.tickSummonedMonsterLocked(mon, players, now)
	}
	actions := []MonsterAction{}
	if mon.RunAwayMode && !mon.RunAwayUntil.IsZero() {
		if now.After(mon.RunAwayUntil) {
			mon.RunAwayMode = false
			mon.RunAwayUntil = time.Time{}
			mon.TargetX = -1
			mon.TargetY = -1
		} else if mon.TargetCharacterID != "" {
			return w.finishMonsterTickLocked(mon, players, now, actions, w.tickFleeAnimalMonsterLocked)
		}
	}
	if w.monsterIsWhiteSkeletonLocked(mon) && mon.FirstRevealPending {
		mon.FirstRevealPending = false
		mon.Hidden = false
		mon.FixedHideMode = false
		mon.Dir = 5
		actions = append(actions, w.monsterActionLocked(mon, MonsterActionReveal))
	}
	if w.monsterIsBeeQueenLocked(mon) {
		return w.tickInclusiveCustomAndInheritedLocked(mon, players, now, actions, w.tickBeeQueenLocked)
	}
	if w.monsterIsExplosionSpiderLocked(mon) {
		return w.tickImmediateCustomAndInheritedLocked(mon, players, now, actions, w.tickExplosionSpiderLocked)
	}
	if w.monsterIsStoneLocked(mon) {
		resultActions, hits, chars, err := w.tickInclusiveCustomLocked(mon, players, now, actions, w.tickStoneMonsterLocked)
		if err != nil {
			return nil, nil, nil, err
		}
		if !mon.StoneMode {
			inheritedActions, inheritedHits, inheritedChars, err := w.finishMonsterTickLocked(mon, players, now, resultActions, w.tickNormalMonsterLocked)
			if err != nil {
				return nil, nil, nil, err
			}
			return inheritedActions, append(hits, inheritedHits...), append(chars, inheritedChars...), nil
		}
		return resultActions, hits, chars, nil
	}
	if w.monsterIsDualAxeLocked(mon) {
		return w.tickAlwaysInheritedCustomLocked(mon, players, now, actions, w.tickDualAxeMonsterLocked)
	}
	if w.monsterIsThornDarkLocked(mon) {
		return w.tickAlwaysInheritedCustomLocked(mon, players, now, actions, w.tickDualAxeMonsterLocked)
	}
	if w.monsterIsSpiderHouseLocked(mon) {
		return w.tickInclusiveCustomAndInheritedLocked(mon, players, now, actions, w.tickSpiderHouseLocked)
	}
	if w.monsterIsBigHeartLocked(mon) {
		return w.tickBigHeartAndInheritedLocked(mon, players, now, actions)
	}
	if w.monsterIsElectronicScorpionLocked(mon) {
		return w.tickAlwaysInheritedCustomLocked(mon, players, now, actions, w.tickElectronicScorpionLocked)
	}
	if w.monsterIsCowKingLocked(mon) {
		return w.tickCowKingMonsterLocked(mon, players, now, actions)
	}
	if w.monsterIsMagicCowLocked(mon) {
		return w.tickAlwaysInheritedCustomLocked(mon, players, now, actions, w.tickMagicCowMonsterLocked)
	}
	if w.monsterIsDigOutZombieLocked(mon) {
		return w.tickDigOutAndInheritedLocked(mon, players, now, actions)
	}
	if mon.Race == 94 {
		return w.tickAlwaysInheritedCustomLocked(mon, players, now, actions, w.tickLightingZombieLocked)
	}
	if w.monsterIsGasAttackLocked(mon) {
		return w.tickAlwaysInheritedCustomLocked(mon, players, now, actions, w.tickGasAttackMonsterLocked)
	}
	if w.monsterIsSpitSpiderLocked(mon) {
		return w.tickAlwaysInheritedCustomLocked(mon, players, now, actions, w.tickSpitSpiderLocked)
	}
	if w.monsterIsArcherLocked(mon) {
		resultActions, hits, chars, err := w.tickArcherMonsterLocked(mon, players, now)
		if err != nil {
			return nil, nil, nil, err
		}
		if w.thinkMonsterLocked(mon, players, now) {
			resultActions = append(resultActions, w.monsterActionLocked(mon, MonsterActionWalk))
		}
		return resultActions, hits, chars, nil
	}
	if w.monsterIsStickLocked(mon) {
		return w.tickStickAndInheritedLocked(mon, players, now, actions)
	}
	if w.monsterIsCentipedeLocked(mon) {
		return w.tickCentipedeAndInheritedLocked(mon, players, now, actions)
	}
	if mon.FixedHideMode || mon.StoneMode {
		return actions, nil, nil, nil
	}
	if w.monsterIsAnimalLocked(mon) {
		return w.finishMonsterTickLocked(mon, players, now, actions, w.tickAnimalMonsterLocked)
	}
	if w.monsterIsPassiveLocked(mon) {
		return w.finishMonsterTickLocked(mon, players, now, actions, w.tickPassiveMonsterLocked)
	}
	return w.finishMonsterTickLocked(mon, players, now, actions, w.tickNormalMonsterLocked)
}

func (w *World) tickAlwaysInheritedCustomLocked(mon *Monster, players map[string]storage.Character, now time.Time, prefix []MonsterAction, custom func(*Monster, map[string]storage.Character, time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error)) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	actions, hits, chars, err := custom(mon, players, now)
	if err != nil {
		return nil, nil, nil, err
	}
	combined := append(prefix, actions...)
	if mon.PendingDeath {
		return combined, hits, chars, nil
	}
	inheritedActions, inheritedHits, inheritedChars, err := w.finishMonsterTickLocked(mon, players, now, combined, w.tickNormalMonsterLocked)
	if err != nil {
		return nil, nil, nil, err
	}
	return inheritedActions, append(hits, inheritedHits...), append(chars, inheritedChars...), nil
}

func (w *World) tickBigHeartAndInheritedLocked(mon *Monster, players map[string]storage.Character, now time.Time, prefix []MonsterAction) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	actions, hits, chars, err := w.tickBigHeartLocked(mon, players, now)
	if err != nil {
		return nil, nil, nil, err
	}
	combined := append(prefix, actions...)
	if mon.PendingDeath {
		return combined, hits, chars, nil
	}
	inheritedActions, inheritedHits, inheritedChars, err := w.finishMonsterTickLocked(mon, players, now, combined, w.tickNormalMonsterLocked)
	if err != nil {
		return nil, nil, nil, err
	}
	return inheritedActions, append(hits, inheritedHits...), append(chars, inheritedChars...), nil
}

func (w *World) tickStickAndInheritedLocked(mon *Monster, players map[string]storage.Character, now time.Time, prefix []MonsterAction) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	if !w.monsterWalkReadyLocked(mon, now) {
		return prefix, nil, nil, nil
	}
	actions, hits, chars, err := w.tickStickMonsterLocked(mon, players, now)
	if err != nil {
		return nil, nil, nil, err
	}
	combined := append(prefix, actions...)
	if mon.FixedHideMode {
		return combined, hits, chars, nil
	}
	inheritedActions, inheritedHits, inheritedChars, err := w.finishMonsterTickLocked(mon, players, now, combined, w.tickNormalMonsterLocked)
	if err != nil {
		return nil, nil, nil, err
	}
	return inheritedActions, append(hits, inheritedHits...), append(chars, inheritedChars...), nil
}

func (w *World) tickCentipedeAndInheritedLocked(mon *Monster, players map[string]storage.Character, now time.Time, prefix []MonsterAction) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	if !w.monsterWalkReadyLocked(mon, now) {
		return prefix, nil, nil, nil
	}
	actions, hits, chars, err := w.tickCentipedeMonsterLocked(mon, players, now)
	if err != nil {
		return nil, nil, nil, err
	}
	combined := append(prefix, actions...)
	if mon.FixedHideMode {
		return combined, hits, chars, nil
	}
	inheritedActions, inheritedHits, inheritedChars, err := w.finishMonsterTickLocked(mon, players, now, combined, w.tickNormalMonsterLocked)
	if err != nil {
		return nil, nil, nil, err
	}
	return inheritedActions, append(hits, inheritedHits...), append(chars, inheritedChars...), nil
}

func (w *World) tickDigOutAndInheritedLocked(mon *Monster, players map[string]storage.Character, now time.Time, prefix []MonsterAction) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	if !w.monsterWalkReadyLocked(mon, now) {
		return prefix, nil, nil, nil
	}
	actions, hits, chars, err := w.tickDigOutZombieLocked(mon, players, now)
	if err != nil {
		return nil, nil, nil, err
	}
	combined := append(prefix, actions...)
	if mon.FixedHideMode {
		return combined, hits, chars, nil
	}
	inheritedActions, inheritedHits, inheritedChars, err := w.finishMonsterTickLocked(mon, players, now, combined, w.tickNormalMonsterLocked)
	if err != nil {
		return nil, nil, nil, err
	}
	return inheritedActions, append(hits, inheritedHits...), append(chars, inheritedChars...), nil
}

func (w *World) tickLightingZombieLocked(mon *Monster, players map[string]storage.Character, now time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	if mon.LastTargetSearchAt.IsZero() {
		mon.LastTargetSearchAt = now
	}
	if now.Sub(mon.LastTargetSearchAt) <= 8*time.Second {
		return nil, nil, nil, nil
	}
	if mon.TargetCharacterID == "" {
		mon.LastTargetSearchAt = now
		w.searchMonsterTargetLocked(mon, players, now)
	}
	if mon.TargetCharacterID == "" {
		return nil, nil, nil, nil
	}
	target, ok := players[mon.TargetCharacterID]
	if !ok || !w.monsterCanKeepCharacterTargetLocked(mon, target) {
		mon.TargetCharacterID = ""
		mon.TargetX, mon.TargetY = -1, -1
		mon.TargetFocusAt = time.Time{}
		return nil, nil, nil, nil
	}
	if abs(mon.X-target.X) > 5 || abs(mon.Y-target.Y) > 5 {
		return nil, nil, nil, nil
	}
	if abs(mon.X-target.X) <= 2 && abs(mon.Y-target.Y) <= 2 && w.rand.Intn(3) != 0 {
		return nil, nil, nil, nil
	}
	if abs(mon.X-target.X) <= 4 && abs(mon.Y-target.Y) <= 4 && mon.Dir >= 0 && mon.Dir < len(dirOffsets) {
		mon.TargetX = mon.X - dirOffsets[mon.Dir][0]
		mon.TargetY = mon.Y - dirOffsets[mon.Dir][1]
	}
	if now.Sub(mon.LastAttackAt) <= time.Duration(w.monsterAttackIntervalMSLocked(mon))*time.Millisecond {
		return nil, nil, nil, nil
	}
	mon.LastAttackAt = now
	mon.TargetFocusAt = now
	mon.HolySeizeUntil = time.Time{}
	mon.Dir = direction(mon.X, mon.Y, target.X, target.Y)
	action := w.monsterActionLocked(mon, MonsterActionLighting)
	action.TargetActor = CharacterActorID(target)
	action.TargetX = target.X
	action.TargetY = target.Y
	power := mon.MinAttack
	if mon.MaxAttack > mon.MinAttack {
		power += w.rand.Intn(mon.MaxAttack - mon.MinAttack + 1)
	}
	mapData, ok := w.data.Maps[mon.MapID]
	if !ok {
		return nil, nil, nil, nil
	}
	targetIDs := make([]string, 0)
	monsterTargets := make([]pendingMonsterTarget, 0)
	orderedTargets := make([]pendingMonsterTarget, 0)
	seen := map[string]struct{}{}
	playerList := make([]storage.Character, 0, len(players))
	for _, candidate := range players {
		playerList = append(playerList, candidate)
	}
	endX, endY, endValid := spellLineNextPosition(mapData, mon.X, mon.Y, mon.Dir, 9)
	if !endValid {
		endX, endY = mon.X, mon.Y
	}
	x, y := mon.X, mon.Y
	for step := 1; step <= 13; step++ {
		lineDir := mon.Dir
		if step > 1 {
			lineDir = direction(x, y, endX, endY)
		}
		nextX, nextY, valid := spellLineNextPosition(mapData, x, y, lineDir, 1)
		if !valid {
			break
		}
		x, y = nextX, nextY
		areaTarget := w.movingObjectAtPointLocked(playerList, mon.MapID, x, y)
		if areaTarget.Character != nil {
			candidate := *areaTarget.Character
			if w.monsterCanTargetCharacterLocked(mon, candidate) {
				if _, exists := seen[candidate.ID]; !exists && w.characterMagicHitAllowedLocked(candidate) {
					seen[candidate.ID] = struct{}{}
					targetIDs = append(targetIDs, candidate.ID)
					orderedTargets = append(orderedTargets, pendingMonsterTarget{ID: candidate.ID, Damage: power, Order: candidate.ObjectOrder})
					power = referenceRound(float64(power) * 1.5)
				}
			}
		} else if areaTarget.Monster != nil {
			candidate := areaTarget.Monster
			if candidate.ID != mon.ID && candidate.MasterID != "" && w.monsterMagicHitAllowedLocked(candidate) {
				if _, exists := seen[candidate.ID]; !exists {
					seen[candidate.ID] = struct{}{}
					monsterTargets = append(monsterTargets, pendingMonsterTarget{ID: candidate.ID, Damage: power})
					orderedTargets = append(orderedTargets, pendingMonsterTarget{ID: candidate.ID, Damage: power, Monster: true, Order: candidate.ObjectOrder})
					power = referenceRound(float64(power) * 1.5)
				}
			}
		}
		if x == endX && y == endY {
			break
		}
	}
	if len(targetIDs) > 0 || len(monsterTargets) > 0 {
		w.pendingMonsterAttacks = append(w.pendingMonsterAttacks, pendingMonsterAttack{DueAt: now.Add(600 * time.Millisecond), MonsterID: mon.ID, Targets: orderedTargets, TargetIDs: targetIDs, MonsterTargets: monsterTargets, Damage: power, ImpactDelay: 600 * time.Millisecond})
	}
	return []MonsterAction{action}, nil, nil, nil
}

func (w *World) monsterInitialSearchDelayMSLocked(mon *Monster) int {
	if mon.Race == 93 || mon.Race == 104 {
		return 3000
	}
	base, span := 3000, 2000
	switch mon.Race {
	case 53, 81, 82, 83, 84, 86, 88, 89, 90, 91, 92, 94, 97, 101, 102, 105, 106, 118, 119, 200:
		base, span = 1500, 1500
	case 85, 95, 96, 100, 103, 116, 117:
		base, span = 2500, 1500
	}
	return base + w.monsterTraceIntn("search.initial_delay", span)
}

func (w *World) tickDigOutZombieLocked(mon *Monster, players map[string]storage.Character, now time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	if !mon.FixedHideMode {
		return nil, nil, nil, nil
	}
	for _, target := range players {
		if !w.monsterCanTargetCharacterLocked(mon, target) || abs(mon.X-target.X) > 3 || abs(mon.Y-target.Y) > 3 {
			continue
		}
		eventID := w.nextGroundEventIDLocked()
		w.addGroundEventLocked(SpellGroundEvent{ID: eventID, MapID: mon.MapID, X: mon.X, Y: mon.Y, Type: 1, Duration: 5 * time.Minute, StartAt: now})
		mon.Hidden = false
		mon.FixedHideMode = false
		mon.LastWalkAt = now.Add(time.Second)
		return []MonsterAction{w.monsterActionLocked(mon, MonsterActionReveal)}, nil, nil, nil
	}
	return nil, nil, nil, nil
}

func (w *World) finishMonsterTickLocked(mon *Monster, players map[string]storage.Character, now time.Time, prefix []MonsterAction, tick func(*Monster, map[string]storage.Character, time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error)) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	if w.thinkMonsterLocked(mon, players, now) {
		return append(prefix, w.monsterActionLocked(mon, MonsterActionWalk)), nil, nil, nil
	}
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

func (w *World) tickInclusiveCustomAndInheritedLocked(mon *Monster, players map[string]storage.Character, now time.Time, prefix []MonsterAction, custom func(*Monster, map[string]storage.Character, time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error)) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	if mon.LastWalkAt.IsZero() || now.Before(mon.LastWalkAt) {
		mon.LastWalkAt = now.Add(-time.Duration(w.monsterWalkSpeedMSLocked(mon)) * time.Millisecond)
	}
	if now.Sub(mon.LastWalkAt) < time.Duration(w.monsterWalkSpeedMSLocked(mon))*time.Millisecond {
		return prefix, nil, nil, nil
	}
	mon.LastWalkAt = now
	actions, _, _, err := custom(mon, players, now)
	if err != nil {
		return nil, nil, nil, err
	}
	return w.finishMonsterTickLocked(mon, players, now, append(prefix, actions...), w.tickNormalMonsterLocked)
}

func (w *World) tickCustomAndInheritedLocked(mon *Monster, players map[string]storage.Character, now time.Time, prefix []MonsterAction, custom func(*Monster, map[string]storage.Character, time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error)) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	actions, hits, chars, err := w.finishMonsterTickLocked(mon, players, now, prefix, custom)
	if err != nil {
		return nil, nil, nil, err
	}
	if len(actions) > 0 {
		switch actions[len(actions)-1].Kind {
		case MonsterActionHit, MonsterActionFlyAxe, MonsterActionLighting:
			return actions, hits, chars, nil
		}
	}
	inheritedActions, inheritedHits, inheritedChars, err := w.finishMonsterTickLocked(mon, players, now, actions, w.tickNormalMonsterLocked)
	if err != nil {
		return nil, nil, nil, err
	}
	return inheritedActions, append(hits, inheritedHits...), append(chars, inheritedChars...), nil
}

func (w *World) tickInclusiveCustomLocked(mon *Monster, players map[string]storage.Character, now time.Time, prefix []MonsterAction, custom func(*Monster, map[string]storage.Character, time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error)) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	if mon.LastWalkAt.IsZero() || now.Before(mon.LastWalkAt) {
		mon.LastWalkAt = now.Add(-time.Duration(w.monsterWalkSpeedMSLocked(mon)) * time.Millisecond)
	}
	if now.Sub(mon.LastWalkAt) < time.Duration(w.monsterWalkSpeedMSLocked(mon))*time.Millisecond {
		return prefix, nil, nil, nil
	}
	mon.LastWalkAt = now
	actions, hits, chars, err := custom(mon, players, now)
	if err != nil {
		return nil, nil, nil, err
	}
	if len(prefix) == 0 {
		return actions, hits, chars, nil
	}
	return append(prefix, actions...), hits, chars, nil
}

func (w *World) tickImmediateCustomAndInheritedLocked(mon *Monster, players map[string]storage.Character, now time.Time, prefix []MonsterAction, custom func(*Monster, map[string]storage.Character, time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error)) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	actions, hits, chars, err := custom(mon, players, now)
	if err != nil {
		return nil, nil, nil, err
	}
	if len(actions) > 0 {
		return append(prefix, actions...), hits, chars, nil
	}
	actions = prefix
	if mon.PendingDeath {
		return actions, hits, chars, nil
	}
	inheritedActions, inheritedHits, inheritedChars, err := w.finishMonsterTickLocked(mon, players, now, actions, w.tickNormalMonsterLocked)
	if err != nil {
		return nil, nil, nil, err
	}
	if len(hits) == 0 && len(chars) == 0 {
		return inheritedActions, inheritedHits, inheritedChars, nil
	}
	return inheritedActions, append(hits, inheritedHits...), append(chars, inheritedChars...), nil
}

func (w *World) tickNormalMonsterLocked(mon *Monster, players map[string]storage.Character, now time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	if w.monsterUsesReferencePeriodicSearchLocked(mon) {
		if mon.LastTargetSearchAt.IsZero() {
			mon.LastTargetSearchAt = now
		} else if now.Sub(mon.LastTargetSearchAt) > 8*time.Second || (mon.TargetCharacterID == "" && now.Sub(mon.LastTargetSearchAt) > time.Second) {
			mon.LastTargetSearchAt = now
			w.searchMonsterTargetLocked(mon, players, now)
		}
	}
	periodicSearch := w.monsterUsesReferencePeriodicSearchLocked(mon)
	if mon.TargetCharacterID == "" && ((periodicSearch && now.After(mon.NextSearchAt)) || (!periodicSearch && !now.Before(mon.NextSearchAt))) {
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
		if ok && target.MapID != mon.MapID && (w.monsterIsDualAxeLocked(mon) || w.monsterIsThornDarkLocked(mon)) {
			if now.Sub(mon.LastAttackAt) <= time.Duration(w.monsterAttackIntervalMSLocked(mon))*time.Millisecond {
				return nil, nil, nil, nil
			}
			mon.TargetCharacterID = ""
			mon.TargetX, mon.TargetY = -1, -1
			mon.TargetFocusAt = time.Time{}
			return nil, nil, nil, nil
		}
		if !ok || !w.monsterCanKeepCharacterTargetLocked(mon, target) {
			mon.TargetCharacterID = ""
			mon.TargetX, mon.TargetY = -1, -1
			mon.TargetFocusAt = time.Time{}
			mon.NextSearchAt = now.Add(time.Duration(w.monsterSearchNoTargetMSLocked(mon)) * time.Millisecond)
			return nil, nil, nil, nil
		}
		dir := walkDirection(mon.X, mon.Y, target.X, target.Y)
		if (mon.X != target.X || mon.Y != target.Y) && abs(mon.X-target.X) <= 1 && abs(mon.Y-target.Y) <= 1 {
			if now.Sub(mon.LastAttackAt) <= time.Duration(w.monsterAttackIntervalMSLocked(mon))*time.Millisecond {
				return nil, nil, nil, nil
			}
			mon.Dir = dir
			mon.LastAttackAt = now
			mon.TargetFocusAt = now
			mon.HolySeizeUntil = time.Time{}
			updated, hit, err := w.monsterAttackCharacterLocked(mon, target)
			if err != nil {
				return nil, nil, nil, err
			}
			if hit.Damage <= 0 {
				return []MonsterAction{w.monsterActionLocked(mon, MonsterActionHit)}, nil, nil, nil
			}
			return []MonsterAction{w.monsterActionLocked(mon, MonsterActionHit)}, []CharacterHit{hit}, []storage.Character{updated}, nil
		}
		mon.TargetX, mon.TargetY = target.X, target.Y
		if !w.moveMonsterTowardLocked(mon, target, dir, players) {
			return nil, nil, nil, nil
		}
		mon.LastWalkAt = now
		return []MonsterAction{w.monsterActionLocked(mon, MonsterActionWalk)}, nil, nil, nil
	}
	return nil, nil, nil, nil
}

func (w *World) tickBeeQueenLocked(mon *Monster, players map[string]storage.Character, now time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	if now.Sub(mon.LastAttackAt) < time.Duration(w.monsterAttackIntervalMSLocked(mon))*time.Millisecond {
		return nil, nil, nil, nil
	}
	mon.LastAttackAt = now
	if target, ok := w.findClosestMonsterTargetLocked(mon, players, w.monsterViewRangeLocked(mon)); ok {
		mon.TargetCharacterID = target.ID
		mon.TargetFocusAt = now
		mon.NextSearchAt = now.Add(time.Duration(w.monsterSearchHasTargetMSLocked(mon)) * time.Millisecond)
	} else if mon.TargetCharacterID == "" {
		mon.NextSearchAt = now.Add(time.Duration(w.monsterSearchNoTargetMSLocked(mon)) * time.Millisecond)
	}
	if mon.TargetCharacterID == "" {
		return nil, nil, nil, nil
	}
	target, ok := players[mon.TargetCharacterID]
	if !ok || !w.monsterCanKeepCharacterTargetLocked(mon, target) {
		mon.TargetCharacterID = ""
		mon.TargetX, mon.TargetY = -1, -1
		mon.TargetFocusAt = time.Time{}
		mon.NextSearchAt = now.Add(time.Duration(w.monsterSearchNoTargetMSLocked(mon)) * time.Millisecond)
		if action, ok := w.wanderMonsterLocked(mon, players); ok {
			return []MonsterAction{action}, nil, nil, nil
		}
		return nil, nil, nil, nil
	}
	if w.countMonsterGenerationEntriesLocked(mon.ID) >= 15 {
		return nil, nil, nil, nil
	}
	mon.TargetFocusAt = now
	childName := "蜜蜂"
	w.pendingMonsterSpawns = append(w.pendingMonsterSpawns, pendingMonsterSpawn{DueAt: now.Add(500 * time.Millisecond), ParentID: mon.ID, ChildName: childName})
	return []MonsterAction{w.monsterActionLocked(mon, MonsterActionHit)}, nil, nil, nil
}

func (w *World) tickCentipedeMonsterLocked(mon *Monster, players map[string]storage.Character, now time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	if mon.FixedHideMode {
		if !mon.CentipedeHideAt.IsZero() && now.Sub(mon.CentipedeHideAt) <= 10*time.Second {
			return nil, nil, nil, nil
		}
		if _, ok := w.findClosestMonsterTargetStrictLocked(mon, players, w.monsterCentipedeComeOutRangeLocked(mon)); ok {
			mon.Hidden = false
			mon.FixedHideMode = false
			mon.HP = mon.MaxHP
			mon.TargetFocusAt = now
			mon.CentipedeHideAt = now
			mon.CentipedeAttackAt = now.Add(3 * time.Second)
			return []MonsterAction{w.monsterActionLocked(mon, MonsterActionReveal)}, nil, nil, nil
		}
		return nil, nil, nil, nil
	}
	if mon.TargetCharacterID == "" {
		if now.Before(mon.CentipedeAttackAt) {
			return nil, nil, nil, nil
		}
		_, ok := w.findClosestMonsterTargetLocked(mon, players, w.monsterCentipedeAttackRangeLocked(mon))
		if !ok {
			if now.Sub(mon.CentipedeHideAt) <= 10*time.Second {
				return nil, nil, nil, nil
			}
			mon.Hidden = true
			mon.FixedHideMode = true
			mon.TargetX = -1
			mon.TargetY = -1
			mon.TargetFocusAt = now
			mon.CentipedeAttackAt = time.Time{}
			return []MonsterAction{w.monsterActionLocked(mon, MonsterActionHide)}, nil, nil, nil
		}
	}
	if _, ok := w.findClosestMonsterTargetLocked(mon, players, w.monsterCentipedeAttackRangeLocked(mon)); !ok {
		mon.TargetCharacterID = ""
		if now.Sub(mon.CentipedeHideAt) > 10*time.Second {
			mon.Hidden = true
			mon.FixedHideMode = true
			mon.TargetX = -1
			mon.TargetY = -1
			mon.CentipedeHideAt = now
			mon.CentipedeAttackAt = time.Time{}
			return []MonsterAction{w.monsterActionLocked(mon, MonsterActionHide)}, nil, nil, nil
		}
		return nil, nil, nil, nil
	}
	attackInterval := time.Duration(w.monsterAttackIntervalMSLocked(mon)) * time.Millisecond
	if attackInterval < 3*time.Second {
		attackInterval = 3 * time.Second
	}
	if now.Sub(mon.LastAttackAt) <= attackInterval {
		return nil, nil, nil, nil
	}
	mon.LastAttackAt = now
	mon.CentipedeAttackAt = now.Add(3 * time.Second)
	targetIDs := make([]string, 0)
	for _, candidate := range players {
		if !w.monsterCanTargetCharacterLocked(mon, candidate) || abs(mon.X-candidate.X) >= w.monsterViewRangeLocked(mon) || abs(mon.Y-candidate.Y) >= w.monsterViewRangeLocked(mon) {
			continue
		}
		mon.TargetFocusAt = now
		targetIDs = append(targetIDs, candidate.ID)
	}
	sort.SliceStable(targetIDs, func(i, j int) bool {
		left := players[targetIDs[i]]
		right := players[targetIDs[j]]
		if left.ObjectOrder != 0 && right.ObjectOrder != 0 && left.ObjectOrder != right.ObjectOrder {
			return left.ObjectOrder < right.ObjectOrder
		}
		return targetIDs[i] < targetIDs[j]
	})
	if len(targetIDs) > 0 {
		power := mon.MinAttack
		if mon.MaxAttack > mon.MinAttack {
			power += w.rand.Intn(mon.MaxAttack - mon.MinAttack + 1)
		}
		w.pendingMonsterAttacks = append(w.pendingMonsterAttacks, pendingMonsterAttack{DueAt: now.Add(600 * time.Millisecond), MonsterID: mon.ID, TargetIDs: targetIDs, Damage: power, ImpactDelay: 600 * time.Millisecond})
		for _, targetID := range targetIDs {
			if w.rand.Intn(4) != 0 {
				continue
			}
			pending := pendingSpell{DueAt: now, TargetCharacterID: targetID, PoisonNotification: true}
			if w.rand.Intn(3) != 0 {
				pending.PoisonHealth = true
				pending.PoisonHealthLevel = 3
				pending.PoisonPoint = 3
				pending.PoisonDuration = 60 * time.Second
			} else {
				pending.ParalysisDuration = 5 * time.Second
			}
			w.pendingSpells = append(w.pendingSpells, pending)
		}
	}
	return []MonsterAction{w.monsterActionLocked(mon, MonsterActionHit)}, nil, nil, nil
}

func (w *World) tickDualAxeMonsterLocked(mon *Monster, players map[string]storage.Character, now time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	if mon.LastTargetSearchAt.IsZero() {
		mon.LastTargetSearchAt = now
	}
	if now.Sub(mon.LastTargetSearchAt) >= 5*time.Second {
		mon.LastTargetSearchAt = now
		if target, ok := w.findClosestMonsterTargetLocked(mon, players, mon.ViewRange); ok {
			mon.TargetCharacterID = target.ID
			mon.TargetFocusAt = now
		}
	}
	if mon.TargetCharacterID == "" {
		if action, ok := w.wanderMonsterLocked(mon, players); ok {
			return []MonsterAction{action}, nil, nil, nil
		}
		return nil, nil, nil, nil
	}
	target, ok := players[mon.TargetCharacterID]
	if ok && target.MapID != mon.MapID {
		if now.Sub(mon.LastAttackAt) <= time.Duration(w.monsterAttackIntervalMSLocked(mon))*time.Millisecond {
			return nil, nil, nil, nil
		}
		mon.TargetCharacterID = ""
		mon.TargetX, mon.TargetY = -1, -1
		mon.TargetFocusAt = time.Time{}
		return nil, nil, nil, nil
	}
	if !ok || !w.monsterCanKeepCharacterTargetLocked(mon, target) {
		mon.TargetCharacterID = ""
		mon.TargetX = -1
		mon.TargetY = -1
		mon.TargetFocusAt = time.Time{}
		mon.NextSearchAt = now.Add(time.Duration(w.monsterSearchNoTargetMSLocked(mon)) * time.Millisecond)
		return nil, nil, nil, nil
	}
	if abs(mon.X-target.X) <= 4 && abs(mon.Y-target.Y) <= 4 {
		if (abs(mon.X-target.X) > 2 || abs(mon.Y-target.Y) > 2) || w.rand.Intn(5) == 0 {
			if mon.Dir >= 0 && mon.Dir < len(dirOffsets) {
				mon.TargetX = mon.X - dirOffsets[mon.Dir][0]
				mon.TargetY = mon.Y - dirOffsets[mon.Dir][1]
			}
		}
	}
	if abs(mon.X-target.X) <= 7 && abs(mon.Y-target.Y) <= 7 {
		if now.Sub(mon.LastAttackAt) > time.Duration(w.monsterAttackIntervalMSLocked(mon))*time.Millisecond {
			mon.LastAttackAt = now
			mon.TargetFocusAt = now
			if mon.AttackMax == 0 {
				mon.AttackMax = 2
				if mon.Race == 93 {
					mon.AttackMax = 3
				}
				if mon.Race == 104 {
					mon.AttackMax = 6
				}
			}
			if mon.AttackCount < mon.AttackMax-1 {
				mon.AttackCount++
				mon.Dir = direction(mon.X, mon.Y, target.X, target.Y)
				if !w.monsterCanFlyLocked(mon, target) {
					return nil, nil, nil, nil
				}
				updated, hit, err := w.monsterAttackCharacterLocked(mon, target)
				if err != nil {
					return nil, nil, nil, err
				}
				distance := maxInt(abs(mon.X-target.X), abs(mon.Y-target.Y))
				hit.ImpactDelay = time.Duration(distance*50+600) * time.Millisecond
				action := w.monsterActionLocked(mon, MonsterActionFlyAxe)
				action.TargetActor = CharacterActorID(target)
				action.TargetX = target.X
				action.TargetY = target.Y
				if hit.Damage <= 0 {
					return []MonsterAction{action}, nil, nil, nil
				}
				return []MonsterAction{action}, []CharacterHit{hit}, []storage.Character{updated}, nil
			}
			if w.rand.Intn(5) == 0 {
				mon.AttackCount = 0
			}
		}
		return nil, nil, nil, nil
	}
	if abs(mon.X-target.X) > 7 || abs(mon.Y-target.Y) > 7 {
		dir := direction(mon.X, mon.Y, target.X, target.Y)
		if w.moveMonsterTowardLocked(mon, target, dir, players) {
			mon.TargetX, mon.TargetY = target.X, target.Y
			mon.LastWalkAt = now
			return []MonsterAction{w.monsterActionLocked(mon, MonsterActionWalk)}, nil, nil, nil
		}
	}
	return nil, nil, nil, nil
}

func (w *World) monsterCanFlyLocked(mon *Monster, target storage.Character) bool {
	mapData, ok := w.data.Maps[mon.MapID]
	if !ok {
		return false
	}
	stepX := float64(target.X-mon.X) / 10
	stepY := float64(target.Y-mon.Y) / 10
	for i := 0; i < 10; i++ {
		x := int(math.Round(float64(mon.X) + stepX*float64(i+1)))
		y := int(math.Round(float64(mon.Y) + stepY*float64(i+1)))
		if !mapData.Walkable(x, y) {
			return false
		}
	}
	return true
}

func (w *World) tickSpitSpiderLocked(mon *Monster, players map[string]storage.Character, now time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	if w.monsterUsesReferencePeriodicSearchLocked(mon) {
		if mon.LastTargetSearchAt.IsZero() {
			mon.LastTargetSearchAt = now
		} else if now.Sub(mon.LastTargetSearchAt) > 8*time.Second || (mon.TargetCharacterID == "" && now.Sub(mon.LastTargetSearchAt) > time.Second) {
			mon.LastTargetSearchAt = now
			w.searchMonsterTargetLocked(mon, players, now)
		}
	}
	periodicSearch := w.monsterUsesReferencePeriodicSearchLocked(mon)
	if mon.TargetCharacterID == "" && ((periodicSearch && now.After(mon.NextSearchAt)) || (!periodicSearch && !now.Before(mon.NextSearchAt))) {
		w.searchMonsterTargetLocked(mon, players, now)
	}
	if mon.TargetCharacterID == "" {
		return nil, nil, nil, nil
	}
	target, ok := players[mon.TargetCharacterID]
	if !ok || !w.monsterCanKeepCharacterTargetLocked(mon, target) {
		mon.TargetCharacterID = ""
		mon.TargetX, mon.TargetY = -1, -1
		mon.TargetFocusAt = time.Time{}
		mon.NextSearchAt = now.Add(time.Duration(w.monsterSearchNoTargetMSLocked(mon)) * time.Millisecond)
		return nil, nil, nil, nil
	}
	if abs(mon.X-target.X) <= 2 && abs(mon.Y-target.Y) <= 2 {
		if !w.monsterSpitTargetInDirectionLocked(mon, target, direction(mon.X, mon.Y, target.X, target.Y)) {
			mon.TargetX, mon.TargetY = target.X, target.Y
			return nil, nil, nil, nil
		}
		if now.Sub(mon.LastAttackAt) < time.Duration(w.monsterAttackIntervalMSLocked(mon))*time.Millisecond {
			return nil, nil, nil, nil
		}
		mon.LastAttackAt = now
		mon.TargetFocusAt = now
		mon.Dir = direction(mon.X, mon.Y, target.X, target.Y)
		mon.HolySeizeUntil = time.Time{}
		power := mon.MinAttack
		if mon.MaxAttack > mon.MinAttack {
			power += w.rand.Intn(mon.MaxAttack - mon.MinAttack + 1)
		}
		if power < 1 {
			return nil, nil, nil, nil
		}
		hits := []CharacterHit{}
		updated := []storage.Character{}
		oldMagic := mon.UseMagic
		mon.UseMagic = true
		playerList := make([]storage.Character, 0, len(players))
		for _, candidate := range players {
			playerList = append(playerList, candidate)
		}
		spitOffsets := [8][][2]int{
			{{0, -2}, {0, -1}}, {{2, -2}, {1, -1}}, {{1, 0}, {2, 0}}, {{1, 1}, {2, 2}},
			{{0, 1}, {0, 2}}, {{-1, 1}, {-2, 2}}, {{-1, 0}, {-2, 0}}, {{-1, -1}, {-2, -2}},
		}
		for _, offset := range spitOffsets[mon.Dir] {
			areaTarget := w.movingObjectAtPointLocked(playerList, mon.MapID, mon.X+offset[0], mon.Y+offset[1])
			if areaTarget.Character == nil {
				continue
			}
			candidate := *areaTarget.Character
			if w.rand.Intn(w.characterSpeedPointLocked(candidate)) >= w.monsterHitPointLocked(mon) {
				continue
			}
			if power <= 0 {
				break
			}
			changed, hit, err := w.monsterAttackCharacterWithDamageLocked(mon, candidate, power)
			if err != nil {
				mon.UseMagic = oldMagic
				return nil, nil, nil, err
			}
			power = hit.Damage
			hit.Magic = true
			hit.ImpactDelay = 300 * time.Millisecond
			if (mon.Race == 82 || mon.Race == 118 || mon.Race == 119) && w.rand.Intn(maxInt(mon.AntiPoison, 0)+20) == 0 && hit.Damage > 0 {
				w.pendingSpells = append(w.pendingSpells, pendingSpell{
					DueAt: now, TargetCharacterID: changed.ID, PoisonHealthLevel: 1,
					PoisonHealth: true, PoisonNotification: true, PoisonPoint: 1,
					PoisonDuration: 30 * time.Second,
				})
			}
			if changed.ID == candidate.ID {
				updated = append(updated, changed)
				hits = append(hits, hit)
			}
		}
		mon.UseMagic = oldMagic
		return []MonsterAction{w.monsterActionLocked(mon, MonsterActionHit)}, hits, updated, nil
	}
	return nil, nil, nil, nil
}

func (w *World) monsterSpitTargetInDirectionLocked(mon *Monster, ch storage.Character, dir int) bool {
	if !w.monsterCanTargetCharacterLocked(mon, ch) || dir < 0 || dir >= 8 {
		return false
	}
	dx, dy := ch.X-mon.X, ch.Y-mon.Y
	if abs(dx) > 2 || abs(dy) > 2 {
		return false
	}
	if abs(dx) <= 1 && abs(dy) <= 1 {
		return true
	}
	patterns := [8][][2]int{
		{{0, -2}, {0, -1}}, {{2, -2}, {1, -1}}, {{1, 0}, {2, 0}}, {{1, 1}, {2, 2}},
		{{0, 1}, {0, 2}}, {{-1, 1}, {-2, 2}}, {{-1, 0}, {-2, 0}}, {{-1, -1}, {-2, -2}},
	}
	for _, offset := range patterns[dir] {
		if dx == offset[0] && dy == offset[1] {
			return true
		}
	}
	return false
}

func (w *World) tickGasAttackMonsterLocked(mon *Monster, players map[string]storage.Character, now time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	if mon.LastTargetSearchAt.IsZero() {
		mon.LastTargetSearchAt = now
	}
	searchInterval := 8 * time.Second
	if mon.TargetCharacterID == "" {
		searchInterval = time.Second
	}
	searchReady := now.Sub(mon.LastTargetSearchAt) > searchInterval
	if searchReady {
		mon.LastTargetSearchAt = now
		w.searchMonsterTargetIgnoringHideLocked(mon, players, now)
	}
	if mon.TargetCharacterID == "" {
		if action, ok := w.wanderMonsterLocked(mon, players); ok {
			return []MonsterAction{action}, nil, nil, nil
		}
		return nil, nil, nil, nil
	}
	target, ok := players[mon.TargetCharacterID]
	if !ok || !w.monsterCanKeepCharacterTargetLocked(mon, target) {
		mon.TargetCharacterID = ""
		mon.TargetX, mon.TargetY = -1, -1
		mon.TargetFocusAt = time.Time{}
		mon.NextSearchAt = now.Add(time.Duration(w.monsterSearchNoTargetMSLocked(mon)) * time.Millisecond)
		if action, ok := w.wanderMonsterLocked(mon, players); ok {
			return []MonsterAction{action}, nil, nil, nil
		}
		return nil, nil, nil, nil
	}
	if abs(mon.X-target.X) > 1 || abs(mon.Y-target.Y) > 1 {
		mon.TargetX, mon.TargetY = target.X, target.Y
		dir := direction(mon.X, mon.Y, target.X, target.Y)
		if !w.moveMonsterTowardLocked(mon, target, dir, players) {
			return nil, nil, nil, nil
		}
		return []MonsterAction{w.monsterActionLocked(mon, MonsterActionWalk)}, nil, nil, nil
	}
	if now.Sub(mon.LastAttackAt) <= time.Duration(w.monsterAttackIntervalMSLocked(mon))*time.Millisecond {
		return nil, nil, nil, nil
	}
	mon.LastAttackAt = now
	mon.TargetFocusAt = now
	mon.Dir = direction(mon.X, mon.Y, target.X, target.Y)
	mon.HolySeizeUntil = time.Time{}
	mapData, mapOK := w.data.Maps[mon.MapID]
	if !mapOK {
		return []MonsterAction{w.monsterActionLocked(mon, MonsterActionHit)}, nil, nil, nil
	}
	frontX, frontY, frontOK := spellLineNextPosition(mapData, mon.X, mon.Y, mon.Dir, 1)
	front := storage.Character{}
	if frontOK {
		playerList := make([]storage.Character, 0, len(players))
		for _, candidate := range players {
			playerList = append(playerList, candidate)
		}
		frontObject := w.movingObjectAtPointLocked(playerList, mon.MapID, frontX, frontY)
		if frontObject.Character != nil {
			front = *frontObject.Character
		}
	}
	if front.ID == "" || !w.monsterCanTargetCharacterLocked(mon, front) {
		return []MonsterAction{w.monsterActionLocked(mon, MonsterActionHit)}, nil, nil, nil
	}
	target = front
	if w.characterSpeedPointLocked(target) > 0 && w.rand.Intn(w.characterSpeedPointLocked(target)) >= w.monsterHitPointLocked(mon) {
		return []MonsterAction{w.monsterActionLocked(mon, MonsterActionHit)}, nil, nil, nil
	}
	updated, hit, err := w.monsterMagicAttackCharacterLocked(mon, target)
	if err != nil {
		return nil, nil, nil, err
	}
	hit.ImpactDelay = 300 * time.Millisecond
	if hit.Damage <= 0 {
		return []MonsterAction{w.monsterActionLocked(mon, MonsterActionHit)}, nil, nil, nil
	}
	hiddenTarget := target.TransparentHideMode != nil && *target.TransparentHideMode
	if mon.Race == 105 && hiddenTarget && w.rand.Intn(3) == 0 {
		setCharacterTransparentLocked(&updated, now.Add(time.Second))
	}
	if hit.Damage > 0 && w.rand.Intn(maxInt(target.AntiPoison, 0)+20) == 0 {
		w.pendingSpells = append(w.pendingSpells, pendingSpell{
			DueAt: now, CasterID: mon.ID, TargetCharacterID: target.ID,
			ParalysisDuration: 5 * time.Second,
		})
	}
	return []MonsterAction{w.monsterActionLocked(mon, MonsterActionHit)}, []CharacterHit{hit}, []storage.Character{updated}, nil
}

func (w *World) tickMagicCowMonsterLocked(mon *Monster, players map[string]storage.Character, now time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	if w.monsterIsCowKingLocked(mon) {
		if action, moved := w.runMagicCowPeriodicMoveLocked(mon, players, now); moved {
			w.updateMagicCowPhaseLocked(mon, now)
			if action.Kind != 0 {
				return []MonsterAction{action}, nil, nil, nil
			}
		}
	}
	if mon.LastTargetSearchAt.IsZero() {
		mon.LastTargetSearchAt = now
	} else if now.Sub(mon.LastTargetSearchAt) > 8*time.Second || (mon.TargetCharacterID == "" && now.Sub(mon.LastTargetSearchAt) > time.Second) {
		mon.LastTargetSearchAt = now
		w.searchMonsterTargetLocked(mon, players, now)
	}
	if mon.TargetCharacterID == "" && !now.Before(mon.NextSearchAt) {
		w.searchMonsterTargetLocked(mon, players, now)
	}
	if mon.TargetCharacterID == "" {
		if action, ok := w.wanderMonsterLocked(mon, players); ok {
			return []MonsterAction{action}, nil, nil, nil
		}
		return nil, nil, nil, nil
	}
	target, ok := players[mon.TargetCharacterID]
	if !ok || !w.monsterCanKeepCharacterTargetLocked(mon, target) {
		mon.TargetCharacterID = ""
		mon.TargetX, mon.TargetY = -1, -1
		mon.TargetFocusAt = time.Time{}
		mon.NextSearchAt = now.Add(time.Duration(w.monsterSearchNoTargetMSLocked(mon)) * time.Millisecond)
		if action, ok := w.wanderMonsterLocked(mon, players); ok {
			return []MonsterAction{action}, nil, nil, nil
		}
		return nil, nil, nil, nil
	}
	if abs(mon.X-target.X) > 1 || abs(mon.Y-target.Y) > 1 {
		mon.TargetX, mon.TargetY = target.X, target.Y
		dir := direction(mon.X, mon.Y, target.X, target.Y)
		if !w.moveMonsterTowardLocked(mon, target, dir, players) {
			return nil, nil, nil, nil
		}
		mon.LastWalkAt = now
		return []MonsterAction{w.monsterActionLocked(mon, MonsterActionWalk)}, nil, nil, nil
	}
	if now.Sub(mon.LastAttackAt) <= time.Duration(w.monsterAttackIntervalMSLocked(mon))*time.Millisecond {
		return nil, nil, nil, nil
	}
	mon.LastAttackAt = now
	mon.TargetFocusAt = now
	mon.Dir = direction(mon.X, mon.Y, target.X, target.Y)
	mon.HolySeizeUntil = time.Time{}
	if !w.monsterIsCowKingLocked(mon) {
		mapData, mapOK := w.data.Maps[mon.MapID]
		if !mapOK {
			return []MonsterAction{w.monsterActionLocked(mon, MonsterActionHit)}, nil, nil, nil
		}
		frontX, frontY, frontOK := spellLineNextPosition(mapData, mon.X, mon.Y, mon.Dir, 1)
		if !frontOK {
			return []MonsterAction{w.monsterActionLocked(mon, MonsterActionHit)}, nil, nil, nil
		}
		playerList := make([]storage.Character, 0, len(players))
		for _, candidate := range players {
			playerList = append(playerList, candidate)
		}
		frontObject := w.movingObjectAtPointLocked(playerList, mon.MapID, frontX, frontY)
		if frontObject.Character == nil || !w.monsterCanTargetCharacterLocked(mon, *frontObject.Character) {
			return []MonsterAction{w.monsterActionLocked(mon, MonsterActionHit)}, nil, nil, nil
		}
		target = *frontObject.Character
	}
	attack := w.monsterMagicAttackCharacterLocked
	if w.monsterIsCowKingLocked(mon) {
		attack = w.monsterMixedAttackCharacterLocked
	}
	updated, hit, err := attack(mon, target)
	if err != nil {
		return nil, nil, nil, err
	}
	hit.AttackerID = mon.ID
	hit.AttackerRaceImg = mon.RaceImg
	hit.AttackerAppr = mon.Appr
	hit.AttackerX, hit.AttackerY = mon.X, mon.Y
	hit.ImpactDelay = 300 * time.Millisecond
	if w.monsterIsCowKingLocked(mon) {
		hit.ImpactDelay = 200 * time.Millisecond
	}
	if hit.Damage <= 0 {
		return []MonsterAction{w.monsterActionLocked(mon, MonsterActionHit)}, nil, nil, nil
	}
	return []MonsterAction{w.monsterActionLocked(mon, MonsterActionHit)}, []CharacterHit{hit}, []storage.Character{updated}, nil
}

func (w *World) tickCowKingMonsterLocked(mon *Monster, players map[string]storage.Character, now time.Time, prefix []MonsterAction) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	periodic := !mon.CowKingMoveAt.IsZero() && now.Sub(mon.CowKingMoveAt) > 30*time.Second
	if action, moved := w.runMagicCowPeriodicMoveLocked(mon, players, now); moved {
		if action.Kind != 0 {
			return append(prefix, action), nil, nil, nil
		}
	} else if periodic {
		w.updateMagicCowPhaseLocked(mon, now)
	}
	customActions, hits, chars, err := w.finishMonsterTickLocked(mon, players, now, prefix, w.tickMagicCowMonsterLocked)
	if err != nil {
		return nil, nil, nil, err
	}
	if mon.PendingDeath {
		return customActions, hits, chars, nil
	}
	inheritedActions, inheritedHits, inheritedChars, err := w.finishMonsterTickLocked(mon, players, now, customActions, w.tickNormalMonsterLocked)
	if err != nil {
		return nil, nil, nil, err
	}
	return inheritedActions, append(hits, inheritedHits...), append(chars, inheritedChars...), nil
}

func (w *World) runMagicCowPeriodicMoveLocked(mon *Monster, players map[string]storage.Character, now time.Time) (MonsterAction, bool) {
	if mon.CowKingMoveAt.IsZero() {
		mon.CowKingMoveAt = now
		return MonsterAction{}, false
	}
	if !mon.CowKingMoveAt.IsZero() && now.Sub(mon.CowKingMoveAt) <= 30*time.Second {
		return MonsterAction{}, false
	}
	mon.CowKingMoveAt = now
	if mon.TargetCharacterID == "" {
		return MonsterAction{}, false
	}
	target, ok := players[mon.TargetCharacterID]
	if !ok || w.magicCowBlockedAroundLocked(target, players) < 5 {
		return MonsterAction{}, false
	}
	off := dirOffsets[target.Dir%len(dirOffsets)]
	x, y := target.X-off[0], target.Y-off[1]
	mp, exists := w.data.Maps[mon.MapID]
	if exists && mp.Walkable(x, y) && !w.movingObjectAtLocked(players, mon.MapID, x, y, mon.ID) {
		oldX, oldY := mon.X, mon.Y
		w.moveMonsterLocked(mon, x, y)
		action := w.monsterActionLocked(mon, MonsterActionSpaceMove)
		action.PreviousMapID, action.PreviousX, action.PreviousY = mon.MapID, oldX, oldY
		return action, true
	}
	if !exists || mp.Width < 4 || mp.Height < 4 {
		return MonsterAction{}, true
	}
	edge := 50
	if mp.Height < 150 {
		edge = 20
		if mp.Height < 30 {
			edge = 2
		}
	}
	if mp.Width <= edge+1 || mp.Height <= edge+1 {
		return MonsterAction{}, true
	}
	x = edge + w.rand.Intn(mp.Width-edge-1)
	y = edge + w.rand.Intn(mp.Height-edge-1)
	if mp.Walkable(x, y) && !w.movingObjectAtLocked(players, mon.MapID, x, y, mon.ID) {
		oldX, oldY := mon.X, mon.Y
		w.moveMonsterLocked(mon, x, y)
		action := w.monsterActionLocked(mon, MonsterActionSpaceMove)
		action.PreviousMapID, action.PreviousX, action.PreviousY = mon.MapID, oldX, oldY
		return action, true
	}
	return MonsterAction{}, true
}

func (w *World) magicCowBlockedAroundLocked(ch storage.Character, players map[string]storage.Character) int {
	mp, ok := w.data.Maps[ch.MapID]
	if !ok {
		return 9
	}
	blocked := 0
	for dx := -1; dx <= 1; dx++ {
		for dy := -1; dy <= 1; dy++ {
			if (dx != 0 || dy != 0) && (!mp.Walkable(ch.X+dx, ch.Y+dy) || w.movingObjectAtLocked(players, ch.MapID, ch.X+dx, ch.Y+dy, "")) {
				blocked++
			}
		}
	}
	return blocked
}

func (w *World) updateMagicCowPhaseLocked(mon *Monster, now time.Time) {
	if mon.MaxHP <= 0 {
		return
	}
	step := mon.MaxHP / 7
	if step <= 0 {
		return
	}
	phase := 7 - mon.HP/step
	if phase < 0 {
		phase = 0
	}
	if phase >= 2 && phase != mon.CowKingPhase {
		mon.CowKingPhase = phase
		mon.CowKingState = 1
		mon.CowKingPhaseAt = now
		mon.CowKingStoredAttack = mon.AttackIntervalMS
		mon.CowKingStoredWalk = mon.WalkSpeedMS
	}
	if mon.CowKingState == 1 {
		if now.Sub(mon.CowKingPhaseAt) < 8*time.Second {
			mon.AttackIntervalMS = 10000
			return
		}
		mon.CowKingState = 2
		mon.CowKingPhaseAt = now
	}
	if mon.CowKingState == 2 {
		if now.Sub(mon.CowKingPhaseAt) < 8*time.Second {
			mon.AttackIntervalMS = 500
			mon.WalkSpeedMS = 400
			return
		}
		mon.CowKingState = 0
		mon.AttackIntervalMS = mon.CowKingStoredAttack
		mon.WalkSpeedMS = mon.CowKingStoredWalk
	}
}

func (w *World) tickArcherMonsterLocked(mon *Monster, players map[string]storage.Character, now time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	if w.monsterWalkReadyInclusiveLocked(mon, now) {
		mon.LastWalkAt = now
		target, found := w.findClosestMonsterTargetLocked(mon, players, w.monsterViewRangeLocked(mon))
		if found {
			mon.TargetCharacterID = target.ID
			mon.TargetFocusAt = now
		} else {
			mon.TargetCharacterID = ""
			mon.TargetX, mon.TargetY = -1, -1
			mon.TargetFocusAt = time.Time{}
		}
	}
	if mon.TargetCharacterID == "" {
		if mon.GuardDirection >= 0 && mon.Dir != mon.GuardDirection {
			mon.Dir = mon.GuardDirection
			return []MonsterAction{w.monsterActionLocked(mon, MonsterActionTurn)}, nil, nil, nil
		}
		return nil, nil, nil, nil
	}
	target, ok := players[mon.TargetCharacterID]
	if !ok || !w.monsterCanKeepCharacterTargetLocked(mon, target) {
		mon.TargetCharacterID = ""
		mon.TargetX, mon.TargetY = -1, -1
		mon.TargetFocusAt = time.Time{}
		return nil, nil, nil, nil
	}
	if now.Sub(mon.LastAttackAt) < time.Duration(w.monsterAttackIntervalMSLocked(mon))*time.Millisecond {
		return nil, nil, nil, nil
	}
	mon.LastAttackAt = now
	mon.Dir = direction(mon.X, mon.Y, target.X, target.Y)
	mon.TargetFocusAt = now
	updated, hit, err := w.monsterAttackCharacterLocked(mon, target)
	if err != nil {
		return nil, nil, nil, err
	}
	if mon.Race == 112 {
		distance := maxInt(abs(mon.X-target.X), abs(mon.Y-target.Y))
		hit.ImpactDelay = time.Duration(distance*50+600) * time.Millisecond
		if hit.Damage > 0 {
			mon.ExpHitterID = ""
			mon.ExpHitterAt = time.Time{}
		}
	}
	actionKind := MonsterActionHit
	if mon.Race == 112 {
		actionKind = MonsterActionFlyAxe
	}
	action := w.monsterActionLocked(mon, actionKind)
	if actionKind == MonsterActionFlyAxe {
		action.TargetActor = CharacterActorID(target)
		action.TargetX = target.X
		action.TargetY = target.Y
	}
	if actionKind == MonsterActionFlyAxe && hit.Damage <= 0 {
		return []MonsterAction{action}, nil, nil, nil
	}
	return []MonsterAction{action}, []CharacterHit{hit}, []storage.Character{updated}, nil
}

func (w *World) tickStickMonsterLocked(mon *Monster, players map[string]storage.Character, now time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	if mon.FixedHideMode {
		if _, ok := w.findClosestMonsterTargetStrictLocked(mon, players, w.monsterStickComeOutRangeLocked(mon)); ok {
			mon.Hidden = false
			mon.FixedHideMode = false
			mon.NextSearchAt = now
			return []MonsterAction{w.monsterActionLocked(mon, MonsterActionReveal)}, nil, nil, nil
		}
		return nil, nil, nil, nil
	}
	if mon.TargetCharacterID != "" && now.Sub(mon.LastAttackAt) > time.Duration(w.monsterAttackIntervalMSLocked(mon))*time.Millisecond {
		if target, ok := w.findClosestMonsterTargetLocked(mon, players, w.monsterViewRangeLocked(mon)); ok {
			mon.TargetCharacterID = target.ID
			mon.TargetFocusAt = now
		}
	}
	if mon.TargetCharacterID == "" && !now.Before(mon.NextSearchAt) {
		target, ok := w.findClosestMonsterTargetLocked(mon, players, w.monsterViewRangeLocked(mon))
		if !ok {
			mon.Hidden = true
			mon.FixedHideMode = true
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
	if !ok || !w.monsterCanKeepCharacterTargetLocked(mon, target) {
		mon.TargetCharacterID = ""
		mon.TargetFocusAt = time.Time{}
		mon.Hidden = true
		mon.FixedHideMode = true
		mon.TargetX = -1
		mon.TargetY = -1
		mon.NextSearchAt = now.Add(time.Duration(w.monsterSearchNoTargetMSLocked(mon)) * time.Millisecond)
		return []MonsterAction{w.monsterActionLocked(mon, MonsterActionHide)}, nil, nil, nil
	}
	attackRange := w.monsterStickAttackRangeLocked(mon)
	if abs(mon.X-target.X) > attackRange || abs(mon.Y-target.Y) > attackRange {
		mon.Hidden = true
		mon.FixedHideMode = true
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
		mon.HolySeizeUntil = time.Time{}
		updated, hit, err := w.monsterAttackCharacterLocked(mon, target)
		if err != nil {
			return nil, nil, nil, err
		}
		actionKind := MonsterActionHit
		if mon.UseMagic {
			actionKind = MonsterActionLighting
		}
		action := w.monsterActionLocked(mon, actionKind)
		action.TargetActor = CharacterActorID(target)
		action.TargetX = target.X
		action.TargetY = target.Y
		if hit.Damage <= 0 {
			return []MonsterAction{action}, nil, nil, nil
		}
		return []MonsterAction{action}, []CharacterHit{hit}, []storage.Character{updated}, nil
	}
	mon.TargetX, mon.TargetY = target.X, target.Y
	return nil, nil, nil, nil
}

func (w *World) tickStoneMonsterLocked(mon *Monster, players map[string]storage.Character, now time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	if mon.StoneMode {
		if target, ok := w.findClosestMonsterTargetLocked(mon, players, w.monsterViewRangeLocked(mon)); ok {
			if abs(mon.X-target.X) <= 2 && abs(mon.Y-target.Y) <= 2 {
				mon.StoneMode = false
				if mon.Race == 102 {
					eventID := w.nextGroundEventIDLocked()
					w.addGroundEventLocked(SpellGroundEvent{ID: eventID, MapID: mon.MapID, X: mon.X, Y: mon.Y, Type: 6, Duration: 5 * time.Minute, StartAt: now})
				}
				actions := []MonsterAction{w.monsterActionLocked(mon, MonsterActionReveal)}
				if mon.Race == 101 {
					nearby := make([]*Monster, 0)
					for _, other := range w.monsters {
						if other == mon || !other.Alive || other.Race != 101 || !other.StoneMode || other.MapID != mon.MapID {
							continue
						}
						if abs(other.X-mon.X) <= 7 && abs(other.Y-mon.Y) <= 7 {
							other.StoneMode = false
							nearby = append(nearby, other)
						}
					}
					sort.SliceStable(nearby, func(i, j int) bool {
						if nearby[i].ObjectOrder != nearby[j].ObjectOrder {
							return nearby[i].ObjectOrder < nearby[j].ObjectOrder
						}
						return nearby[i].ID < nearby[j].ID
					})
					for _, other := range nearby {
						actions = append(actions, w.monsterActionLocked(other, MonsterActionReveal))
					}
				}
				return actions, nil, nil, nil
			}
		}
		return nil, nil, nil, nil
	}
	if mon.TargetCharacterID == "" && !now.Before(mon.NextSearchAt) {
		target, ok := w.findClosestMonsterTargetLocked(mon, players, w.monsterViewRangeLocked(mon))
		if !ok {
			w.maybeCallStoneKingSlavesLocked(mon, now)
			mon.NextSearchAt = now.Add(time.Duration(w.monsterSearchNoTargetMSLocked(mon)) * time.Millisecond)
			return nil, nil, nil, nil
		}
		mon.TargetCharacterID = target.ID
		mon.TargetFocusAt = now
		mon.NextSearchAt = now.Add(time.Duration(w.monsterSearchHasTargetMSLocked(mon)) * time.Millisecond)
		w.maybeCallStoneKingSlavesLocked(mon, now)
	}
	if (mon.Race == 101 || mon.Race == 102) && mon.TargetCharacterID != "" && !now.Before(mon.NextSearchAt) {
		mon.NextSearchAt = now.Add(time.Duration(w.monsterSearchHasTargetMSLocked(mon)) * time.Millisecond)
		w.searchMonsterTargetLocked(mon, players, now)
		w.maybeCallStoneKingSlavesLocked(mon, now)
	}
	if mon.TargetCharacterID != "" {
		target, ok := players[mon.TargetCharacterID]
		if ok && w.monsterCanKeepCharacterTargetLocked(mon, target) && abs(mon.X-target.X) <= 1 && abs(mon.Y-target.Y) <= 1 && now.Sub(mon.LastAttackAt) > time.Duration(w.monsterAttackIntervalMSLocked(mon))*time.Millisecond {
			mon.Dir = direction(mon.X, mon.Y, target.X, target.Y)
			mon.LastAttackAt = now
			mon.TargetFocusAt = now
			mon.HolySeizeUntil = time.Time{}
			attack := w.monsterAttackCharacterLocked
			if mon.Race == 102 {
				attack = w.monsterMagicAttackCharacterLocked
			}
			updated, hit, err := attack(mon, target)
			if err != nil {
				return nil, nil, nil, err
			}
			if hit.Damage <= 0 {
				return []MonsterAction{w.monsterActionLocked(mon, MonsterActionHit)}, nil, nil, nil
			}
			return []MonsterAction{w.monsterActionLocked(mon, MonsterActionHit)}, []CharacterHit{hit}, []storage.Character{updated}, nil
		}
	}
	return w.tickNormalMonsterLocked(mon, players, now)
}

func (w *World) maybeCallStoneKingSlavesLocked(mon *Monster, now time.Time) {
	if mon.Race != 102 {
		return
	}
	if mon.HP == mon.MaxHP {
		mon.StoneDangerLevel = 5
		return
	}
	if mon.StoneDangerLevel > 0 && mon.StoneDangerLevel*mon.MaxHP > mon.HP*5 {
		mon.StoneDangerLevel--
		w.callStoneKingSlavesLocked(mon, now)
	}
}

func (w *World) callStoneKingSlavesLocked(mon *Monster, now time.Time) {
	const maxSlaves = 30
	names := []string{"祖玛卫士", "祖玛雕像", "祖玛弓箭手", "楔蛾"}
	count := w.monsterTraceIntn("stone_king.slave_count", 6) + 6
	if remaining := maxSlaves - w.countMonsterGenerationEntriesLocked(mon.ID); count > remaining {
		count = remaining
	}
	if count <= 0 || mon.Dir < 0 || mon.Dir >= len(dirOffsets) {
		return
	}
	off := dirOffsets[mon.Dir]
	x, y := mon.X+off[0], mon.Y+off[1]
	for i := 0; i < count; i++ {
		tpl, ok := w.monsterTemplateByIDLocked(names[w.monsterTraceIntn("stone_king.slave_kind", len(names))])
		if !ok {
			continue
		}
		child := w.createSpawnMonsterLocked(data.StdSpawn{MapID: mon.MapID, MonsterID: tpl.ID, X: x, Y: y, Count: 1}, tpl, x, y)
		if child == nil {
			continue
		}
		child.ParentID = mon.ID
		child.TargetFocusAt = now
	}
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
			mon.RunAwayUntil = time.Time{}
			mon.TargetX = -1
			mon.TargetY = -1
			mon.NextSearchAt = now.Add(time.Duration(w.monsterSearchHasTargetMSLocked(mon)) * time.Millisecond)
		} else {
			mon.RunAwayMode = false
			mon.RunAwayUntil = time.Time{}
			mon.TargetX = -1
			mon.TargetY = -1
			mon.NextSearchAt = now.Add(time.Duration(w.monsterSearchNoTargetMSLocked(mon)) * time.Millisecond)
		}
	}
	if mon.RunAwayMode && mon.TargetCharacterID != "" {
		target, ok := players[mon.TargetCharacterID]
		if !ok || !w.monsterCanKeepCharacterTargetLocked(mon, target) {
			mon.TargetCharacterID = ""
			mon.TargetFocusAt = time.Time{}
			mon.RunAwayMode = false
			mon.RunAwayUntil = time.Time{}
			mon.TargetX = -1
			mon.TargetY = -1
			mon.NextSearchAt = now.Add(time.Duration(w.monsterSearchNoTargetMSLocked(mon)) * time.Millisecond)
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

func (w *World) explosionSpiderLocked(mon *Monster, players map[string]storage.Character, now time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	mon.HP = core.ApplyHPDelta(mon.HP, mon.MaxHP, -mon.HP).HP
	mon.PendingDeath = true
	mon.DeathHitterID = mon.ExpHitterID
	if mon.DeathHitterID == "" {
		mon.DeathHitterID = mon.LastHitterID
	}
	var hits []CharacterHit
	var updated []storage.Character
	var nearest storage.Character
	best := 999999
	power := mon.MinAttack
	if mon.MaxAttack > mon.MinAttack {
		power += w.rand.Intn(mon.MaxAttack - mon.MinAttack + 1)
	}
	for _, ch := range players {
		if !w.monsterCanTargetCharacterLocked(mon, ch) {
			continue
		}
		if abs(ch.X-mon.X) <= 1 && abs(ch.Y-mon.Y) <= 1 {
			if dist := abs(ch.X-mon.X) + abs(ch.Y-mon.Y); dist < best {
				best = dist
				nearest = ch
			}
			changed, hit, err := w.monsterMixedAttackCharacterWithPowerLocked(mon, ch, power)
			if err != nil {
				return nil, nil, nil, err
			}
			if hit.Damage > 0 {
				hit.ImpactDelay = 700 * time.Millisecond
				hits = append(hits, hit)
				updated = append(updated, changed)
			}
		}
	}
	if nearest.ID != "" {
		mon.Dir = direction(mon.X, mon.Y, nearest.X, nearest.Y)
	}
	return nil, hits, updated, nil
}

func (w *World) tickBigHeartLocked(mon *Monster, players map[string]storage.Character, now time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	if now.Sub(mon.LastAttackAt) <= time.Duration(w.monsterAttackIntervalMSLocked(mon))*time.Millisecond {
		return nil, nil, nil, nil
	}
	visible := false
	for _, ch := range players {
		if ch.MapID != mon.MapID {
			continue
		}
		if abs(ch.X-mon.X) <= mon.ViewRange && abs(ch.Y-mon.Y) <= mon.ViewRange {
			visible = true
		}
	}
	if !visible {
		return nil, nil, nil, nil
	}
	mon.LastAttackAt = now
	actions := []MonsterAction{w.monsterActionLocked(mon, MonsterActionHit)}
	power := mon.MinAttack
	if mon.MaxAttack > mon.MinAttack {
		power += w.rand.Intn(mon.MaxAttack - mon.MinAttack + 1)
	}
	targetIDs := make([]string, 0)
	for _, ch := range players {
		if !w.monsterCanTargetCharacterLocked(mon, ch) {
			continue
		}
		if abs(ch.X-mon.X) <= mon.ViewRange && abs(ch.Y-mon.Y) <= mon.ViewRange {
			targetIDs = append(targetIDs, ch.ID)
		}
	}
	sort.SliceStable(targetIDs, func(i, j int) bool {
		left := players[targetIDs[i]]
		right := players[targetIDs[j]]
		if left.ObjectOrder != 0 && right.ObjectOrder != 0 && left.ObjectOrder != right.ObjectOrder {
			return left.ObjectOrder < right.ObjectOrder
		}
		return targetIDs[i] < targetIDs[j]
	})
	if len(targetIDs) > 0 {
		w.pendingMonsterAttacks = append(w.pendingMonsterAttacks, pendingMonsterAttack{DueAt: now.Add(200 * time.Millisecond), MonsterID: mon.ID, TargetIDs: targetIDs, Damage: power})
	}
	return actions, nil, nil, nil
}

func (w *World) tickElectronicScorpionLocked(mon *Monster, players map[string]storage.Character, now time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	mon.UseMagic = mon.HP < mon.MaxHP/2
	if mon.LastTargetSearchAt.IsZero() {
		mon.LastTargetSearchAt = now
	}
	if mon.TargetCharacterID == "" && now.Sub(mon.LastTargetSearchAt) > 1*time.Second {
		mon.LastTargetSearchAt = now
		w.searchMonsterTargetLocked(mon, players, now)
	}
	if mon.TargetCharacterID == "" {
		return nil, nil, nil, nil
	}
	target, ok := players[mon.TargetCharacterID]
	if !ok || !w.monsterCanKeepCharacterTargetLocked(mon, target) {
		mon.TargetCharacterID = ""
		mon.TargetX, mon.TargetY = -1, -1
		mon.TargetFocusAt = time.Time{}
		if mon.Race == 200 {
			mon.TargetFocusAt = now
			mon.LastTargetSearchAt = now
		}
		mon.NextSearchAt = now.Add(time.Duration(w.monsterSearchNoTargetMSLocked(mon)) * time.Millisecond)
		return nil, nil, nil, nil
	}
	nx := abs(mon.X - target.X)
	ny := abs(mon.Y - target.Y)
	if nx <= 2 && ny <= 2 {
		if !mon.UseMagic && nx != 2 && ny != 2 {
			return nil, nil, nil, nil
		}
		if now.Sub(mon.LastAttackAt) <= time.Duration(w.monsterAttackIntervalMSLocked(mon))*time.Millisecond {
			return nil, nil, nil, nil
		}
		mon.LastAttackAt = now
		mon.TargetFocusAt = now
		mon.Dir = direction(mon.X, mon.Y, target.X, target.Y)
		updated, hit, err := w.monsterMagicAttackCharacterLocked(mon, target)
		if err != nil {
			return nil, nil, nil, err
		}
		hit.ImpactDelay = 200 * time.Millisecond
		if hit.Damage > 0 && mon.MP&0xFF > 0 {
			mon.HP = minInt(mon.MaxHP, mon.HP+hit.Damage/(mon.MP&0xFF))
		}
		action := w.monsterActionLocked(mon, MonsterActionLighting)
		action.TargetActor = CharacterActorID(target)
		action.TargetX = target.X
		action.TargetY = target.Y
		if hit.Damage <= 0 {
			return []MonsterAction{action}, nil, nil, nil
		}
		return []MonsterAction{action}, []CharacterHit{hit}, []storage.Character{updated}, nil
	}
	return w.tickMonsterTargetLocked(mon, players, now)
}

func (w *World) tickExplosionSpiderLocked(mon *Monster, players map[string]storage.Character, now time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	if mon.ExplosionStartAt.IsZero() {
		mon.ExplosionStartAt = now
	}
	if now.Sub(mon.ExplosionStartAt) > 60*time.Second {
		mon.ExplosionStartAt = now
		return w.explosionSpiderLocked(mon, players, now)
	}
	if mon.TargetCharacterID != "" {
		if target, ok := players[mon.TargetCharacterID]; ok && w.monsterCanKeepCharacterTargetLocked(mon, target) && abs(mon.X-target.X) <= 1 && abs(mon.Y-target.Y) <= 1 && now.Sub(mon.LastAttackAt) > time.Duration(w.monsterAttackIntervalMSLocked(mon))*time.Millisecond {
			mon.LastAttackAt = now
			mon.TargetFocusAt = now
			mon.Dir = direction(mon.X, mon.Y, target.X, target.Y)
			return w.explosionSpiderLocked(mon, players, now)
		}
	}
	return nil, nil, nil, nil
}

func (w *World) tickSpiderHouseLocked(mon *Monster, players map[string]storage.Character, now time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	if now.Sub(mon.LastAttackAt) < time.Duration(w.monsterAttackIntervalMSLocked(mon))*time.Millisecond {
		return nil, nil, nil, nil
	}
	mon.LastAttackAt = now
	w.searchMonsterTargetLocked(mon, players, now)
	if mon.TargetCharacterID == "" {
		return nil, nil, nil, nil
	}
	target, ok := players[mon.TargetCharacterID]
	if !ok || !w.monsterCanKeepCharacterTargetLocked(mon, target) {
		mon.TargetCharacterID = ""
		mon.TargetX, mon.TargetY = -1, -1
		mon.TargetFocusAt = time.Time{}
		return nil, nil, nil, nil
	}
	if w.countMonsterGenerationEntriesLocked(mon.ID) >= 15 {
		return nil, nil, nil, nil
	}
	childName := "爆裂蜘蛛"
	w.pendingMonsterSpawns = append(w.pendingMonsterSpawns, pendingMonsterSpawn{DueAt: now.Add(500 * time.Millisecond), ParentID: mon.ID, ChildName: childName})
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

func (w *World) countMonsterGenerationEntriesLocked(parentID string) int {
	count := 0
	for _, mon := range w.monsters {
		if mon.ParentID == parentID {
			count++
		}
	}
	return count
}

func (w *World) spawnChildMonsterLocked(parent *Monster, childName string, now time.Time) bool {
	childCount := w.countMonsterChildrenLocked(parent.ID)
	if parent.Race == 103 || parent.Race == 116 {
		childCount = w.countMonsterGenerationEntriesLocked(parent.ID)
	}
	if childCount >= 15 {
		return false
	}
	tpl, ok := w.monsterTemplateByIDLocked(childName)
	if !ok {
		return false
	}
	childX, childY := parent.X, parent.Y+1
	if parent.Race == 103 {
		childX, childY = parent.X, parent.Y
	} else {
		mp, ok := w.data.Maps[parent.MapID]
		if !ok {
			return false
		}
		if !mp.Walkable(childX, childY) {
			return false
		}
	}
	child := w.createSpawnMonsterLocked(data.StdSpawn{MapID: parent.MapID, MonsterID: tpl.ID, X: childX, Y: childY, Count: 1, RespawnSeconds: 0}, tpl, childX, childY)
	if child == nil {
		return false
	}
	if child.X != parent.X || child.Y != parent.Y {
		if _, occupied := w.occupied[monsterPosition{MapID: parent.MapID, X: childX, Y: childY}]; !occupied {
			w.occupyMonsterLocked(child)
		}
	}
	child.ParentID = parent.ID
	child.TargetCharacterID = parent.TargetCharacterID
	child.TargetFocusAt = now
	return true
}

func (w *World) spawnChildMonsterAtLocked(parent *Monster, childName string, x, y int, now time.Time) bool {
	if w.countMonsterChildrenLocked(parent.ID) >= 15 {
		return false
	}
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
