package world

import (
	"fmt"
	"sort"
	"time"

	"openmir2/internal/npc"
)

type NPCTrainingHit struct {
	NPC       npc.Entity
	Attacker  string
	Damage    int
	Magic     bool
	Total     int
	HitCount  int
	HitAt     time.Time
	Summary   bool
	SummaryAt time.Time
}

func (w *World) ApplyNPCTrainingHit(npcID, attacker string, damage int, magic bool, now time.Time) (NPCTrainingHit, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.applyNPCTrainingHitLocked(npcID, attacker, damage, magic, now)
}

func (w *World) applyNPCTrainingHitLocked(npcID, attacker string, damage int, magic bool, now time.Time) (NPCTrainingHit, error) {
	entity, ok := w.data.NPCs.Entities[npcID]
	if !ok || !npc.IsTrainer(entity) {
		return NPCTrainingHit{}, fmt.Errorf("npc %q is not a trainer", npcID)
	}
	if attacker == "" || damage <= 0 || entity.Hidden {
		return NPCTrainingHit{}, nil
	}
	state := w.npcTraining[npcID]
	if !state.LastHitAt.IsZero() && now.Sub(state.LastHitAt) > 3*time.Second {
		state = NPCTrainingState{}
	}
	state.DamageTotal += damage
	state.HitCount++
	state.LastHitAt = now
	w.npcTraining[npcID] = state
	return NPCTrainingHit{NPC: entity, Attacker: attacker, Damage: damage, Magic: magic, Total: state.DamageTotal, HitCount: state.HitCount, HitAt: now}, nil
}

func (w *World) trainerAtExactPointLocked(mapID string, x, y int) (npc.Entity, bool) {
	for _, entity := range w.data.NPCs.Entities {
		if npc.IsTrainer(entity) && !entity.Hidden && entity.MapID == mapID && entity.X == x && entity.Y == y {
			return entity, true
		}
	}
	return npc.Entity{}, false
}

func (w *World) trainerByActorIDLocked(actorID int32, mapID string, x, y int) (npc.Entity, bool) {
	if actorID == 0 {
		return npc.Entity{}, false
	}
	for _, entity := range w.data.NPCs.Entities {
		if !npc.IsTrainer(entity) || entity.Hidden || entity.MapID != mapID || w.NPCActorID(entity.ID) != actorID {
			continue
		}
		if abs(entity.X-x) <= 1 && abs(entity.Y-y) <= 1 {
			return entity, true
		}
	}
	return npc.Entity{}, false
}

func (w *World) trainersInRadiusLocked(mapID string, x, y, radius int) []npc.Entity {
	seen := make(map[string]struct{})
	trainers := make([]npc.Entity, 0)
	for _, entity := range w.data.NPCs.Entities {
		if !npc.IsTrainer(entity) || entity.Hidden || entity.MapID != mapID || abs(entity.X-x) > radius || abs(entity.Y-y) > radius {
			continue
		}
		if _, ok := seen[entity.ID]; ok {
			continue
		}
		seen[entity.ID] = struct{}{}
		trainers = append(trainers, entity)
	}
	sort.SliceStable(trainers, func(i, j int) bool {
		if trainers[i].X != trainers[j].X {
			return trainers[i].X < trainers[j].X
		}
		if trainers[i].Y != trainers[j].Y {
			return trainers[i].Y < trainers[j].Y
		}
		return trainers[i].ID < trainers[j].ID
	})
	return trainers
}

func (w *World) FlushNPCTraining(npcID string, now time.Time) (NPCTrainingHit, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.flushNPCTrainingLocked(npcID, now)
}

func (w *World) flushNPCTrainingLocked(npcID string, now time.Time) (NPCTrainingHit, bool) {
	entity, ok := w.data.NPCs.Entities[npcID]
	if !ok || !npc.IsTrainer(entity) {
		return NPCTrainingHit{}, false
	}
	state := w.npcTraining[npcID]
	if state.HitCount == 0 || state.LastHitAt.IsZero() || now.Sub(state.LastHitAt) <= 3*time.Second {
		return NPCTrainingHit{}, false
	}
	delete(w.npcTraining, npcID)
	return NPCTrainingHit{NPC: entity, Total: state.DamageTotal, HitCount: state.HitCount, Summary: true, SummaryAt: now}, true
}
