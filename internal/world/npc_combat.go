package world

import (
	"fmt"
	"time"

	"openmir2/internal/npc"
	"openmir2/internal/storage"
)

const (
	npcProcessBudget = 5 * time.Millisecond
)

func (w *World) trainerStateLocked(entity npc.Entity) *NPCCombatState {
	if !npc.IsTrainer(entity) || entity.ID == "" {
		return nil
	}
	if w.npcCombat == nil {
		w.npcCombat = make(map[string]*NPCCombatState)
	}
	state := w.npcCombat[entity.ID]
	if state == nil {
		state = &NPCCombatState{}
		w.npcCombat[entity.ID] = state
	}
	return state
}

func (w *World) canPlayerTargetTrainerLocked(entity npc.Entity) bool {
	return w.gameplay.Combat.NonPKServer && npc.IsTrainer(entity) && !entity.Hidden
}

func (w *World) trainerSpeechLocked(now time.Time, entity npc.Entity) []NPCSpeech {
	state := w.trainerStateLocked(entity)
	if state == nil || state.HitCount == 0 || !now.After(state.LastHitAt.Add(3*time.Second)) {
		return nil
	}
	speech := NPCSpeech{
		NPC:     entity,
		ActorID: w.NPCActorID(entity.ID),
		Text:    fmt.Sprintf("总破坏力%2d, 平均破坏力%2d", state.DamageSum, state.DamageSum/state.HitCount),
	}
	state.DamageSum = 0
	state.HitCount = 0
	state.LastHitAt = time.Time{}
	return []NPCSpeech{speech}
}

func (w *World) recordTrainerStrikeLocked(result *AttackResult, entity npc.Entity, damage int, now time.Time) {
	if result == nil || damage <= 0 || !npc.IsTrainer(entity) {
		return
	}
	state := w.trainerStateLocked(entity)
	if state == nil {
		return
	}
	state.DamageSum += damage
	state.HitCount++
	state.LastHitAt = now
	result.Connected = true
	result.Damage = damage
	result.NPCSpeeches = append(result.NPCSpeeches, NPCSpeech{
		NPC:     entity,
		ActorID: w.NPCActorID(entity.ID),
		Text:    fmt.Sprintf("破坏力%2d, 平均值%2d", damage, state.DamageSum/state.HitCount),
	})
}

func appendTrainerStruckHit(result *AttackResult, entity npc.Entity, damage int, attackerID string, magic bool) {
	if result == nil || damage <= 0 || !npc.IsTrainer(entity) {
		return
	}
	result.NPCStruckHits = append(result.NPCStruckHits, NPCStruckHit{
		NPC:        entity,
		Damage:     damage,
		AttackerID: attackerID,
		Magic:      magic,
	})
}

func appendTrainerStruckHitToTickResult(result *TickResult, entity npc.Entity, damage int, attackerID string, magic bool) {
	if result == nil || damage <= 0 || !npc.IsTrainer(entity) {
		return
	}
	result.NPCStruckHits = append(result.NPCStruckHits, NPCStruckHit{
		NPC:        entity,
		Damage:     damage,
		AttackerID: attackerID,
		Magic:      magic,
	})
}

func appendNPCCombatEvent(events *[]NPCCombatEvent, speech NPCSpeech, hit NPCStruckHit) {
	if events == nil {
		return
	}
	*events = append(*events, NPCCombatEvent{Speech: speech, HasSpeech: true, Struck: hit, HasStruck: true})
}

func (w *World) applyPendingTrainerSpellsLocked(result *TickResult, now time.Time) {
	if len(w.pendingSpells) == 0 {
		return
	}
	entities := w.npcEntitiesInMapOrdered("", true)
	byID := make(map[string]npc.Entity, len(entities))
	for _, entity := range entities {
		if npc.IsTrainer(entity) {
			byID[entity.ID] = entity
		}
	}
	remaining := w.pendingSpells[:0]
	for _, pending := range w.pendingSpells {
		if pending.TargetTrainerID == "" || now.Before(pending.DueAt) {
			remaining = append(remaining, pending)
			continue
		}
		entity, ok := byID[pending.TargetTrainerID]
		if !ok {
			continue
		}
		if pending.TargetRange > 0 && (abs(entity.X-pending.TargetX) > pending.TargetRange || abs(entity.Y-pending.TargetY) > pending.TargetRange) {
			continue
		}
		damage := pending.Damage
		if damage <= 0 {
			continue
		}
		var attack AttackResult
		w.recordTrainerStrikeLocked(&attack, entity, damage, now)
		result.NPCSpeeches = append(result.NPCSpeeches, attack.NPCSpeeches...)
		magicHit := NPCStruckHit{NPC: entity, Damage: damage, AttackerID: pending.CasterID, Magic: true}
		appendTrainerStruckHitToTickResult(result, entity, damage, pending.CasterID, true)
		if len(attack.NPCSpeeches) > 0 {
			appendNPCCombatEvent(&result.NPCCombatEvents, attack.NPCSpeeches[len(attack.NPCSpeeches)-1], magicHit)
		}
	}
	w.pendingSpells = remaining
}

func (w *World) runNPCsLocked(now time.Time) ([]NPCSpeech, []NPCAction) {
	speeches := make([]NPCSpeech, 0)
	actions := make([]NPCAction, 0)
	entities := w.npcEntitiesInMapOrdered("", true)
	filtered := entities[:0]
	for _, entity := range entities {
		if entity.CastleOfficial || npc.NormalizeKind(entity.Kind) == npc.KindMerchant {
			continue
		}
		filtered = append(filtered, entity)
	}
	entities = filtered
	if len(entities) == 0 {
		w.npcProcessCursor = 0
		return speeches, actions
	}
	if w.npcProcessCursor >= len(entities) {
		w.npcProcessCursor = 0
	}
	started := time.Now()
	processedAll := true
	for i := w.npcProcessCursor; i < len(entities); i++ {
		entity := entities[i]
		nextRunAt, known := w.npcNextRunAt[entity.ID]
		if !known {
			nextRunAt = now
		}
		if !nextRunAt.IsZero() && !now.After(nextRunAt) {
			continue
		}
		w.npcNextRunAt[entity.ID] = now.Add(250 * time.Millisecond)
		if npc.IsTrainer(entity) {
			speeches = append(speeches, w.trainerSpeechLocked(now, entity)...)
		}
		actions = append(actions, w.runNPCIdleEntityLocked(entity)...)
		if time.Since(started) > npcProcessBudget {
			w.npcProcessCursor = i
			processedAll = false
			break
		}
	}
	if processedAll {
		w.npcProcessCursor = 0
	}
	return speeches, actions
}

func (w *World) runMerchantsLocked(now time.Time) ([]NPCSpeech, []NPCAction) {
	speeches := make([]NPCSpeech, 0)
	actions := make([]NPCAction, 0)
	entities := w.npcEntitiesInMapOrdered("", true)
	filtered := entities[:0]
	for _, entity := range entities {
		if entity.CastleOfficial || npc.NormalizeKind(entity.Kind) == npc.KindMerchant {
			filtered = append(filtered, entity)
		}
	}
	entities = filtered
	if len(entities) == 0 {
		w.npcMerchantProcessCursor = 0
		return speeches, actions
	}
	if w.npcMerchantProcessCursor >= len(entities) {
		w.npcMerchantProcessCursor = 0
	}
	started := time.Now()
	processedAll := true
	for i := w.npcMerchantProcessCursor; i < len(entities); i++ {
		entity := entities[i]
		nextRunAt, known := w.npcNextRunAt[entity.ID]
		if !known {
			nextRunAt = now
		}
		if !nextRunAt.IsZero() && !now.After(nextRunAt) {
			continue
		}
		w.npcNextRunAt[entity.ID] = now.Add(250 * time.Millisecond)
		actions = append(actions, w.runNPCIdleEntityLocked(entity)...)
		if time.Since(started) > npcProcessBudget {
			w.npcMerchantProcessCursor = i
			processedAll = false
			break
		}
	}
	if processedAll {
		w.npcMerchantProcessCursor = 0
	}
	return speeches, actions
}

func (w *World) runNPCIdleEntityLocked(entity npc.Entity) []NPCAction {
	actions := make([]NPCAction, 0, 2)
	if w.npcFixedHides == nil {
		w.npcFixedHides = make(map[string]bool)
	}
	switch {
	case entity.GuildOfficial:
		if w.rand.Intn(40) == 0 {
			entity.Dir = w.rand.Intn(8)
			w.data.NPCs.Entities[entity.ID] = entity
			actions = append(actions, NPCAction{NPC: entity, Kind: NPCActionTurn})
		} else if w.rand.Intn(30) == 0 {
			actions = append(actions, NPCAction{NPC: entity, Kind: NPCActionHit})
		}
	case entity.CastleOfficial || npc.NormalizeKind(entity.Kind) == npc.KindMerchant:
		if w.rand.Intn(50) == 0 {
			entity.Dir = w.rand.Intn(8)
			w.data.NPCs.Entities[entity.ID] = entity
			actions = append(actions, NPCAction{NPC: entity, Kind: NPCActionTurn})
		} else if w.rand.Intn(50) == 0 {
			actions = append(actions, NPCAction{NPC: entity, Kind: NPCActionHit})
		}
	}
	if entity.CastleOfficial {
		underWar := false
		if castle, ok := w.castleRuntime[w.data.Castle.ID]; ok {
			underWar = castle.UnderWar
		} else if w.store != nil {
			if castle, ok := w.store.Castle(w.data.Castle.ID); ok {
				if w.castleRuntime == nil {
					w.castleRuntime = make(map[string]storage.Castle)
				}
				w.castleRuntime[w.data.Castle.ID] = castle
				underWar = castle.UnderWar
			}
		}
		if underWar && !w.npcFixedHides[entity.ID] {
			w.npcFixedHides[entity.ID] = true
			actions = append(actions, NPCAction{NPC: entity, Kind: NPCActionDisappear})
		} else if !underWar && w.npcFixedHides[entity.ID] {
			delete(w.npcFixedHides, entity.ID)
			actions = append(actions, NPCAction{NPC: entity, Kind: NPCActionReveal})
		}
	}
	return actions
}
