package world

import (
	"sort"
	"time"

	"openmir2/internal/data"
	"openmir2/internal/storage"
)

const fireWallTickInterval = 3 * time.Second

type fireFieldKey struct {
	MapID string
	X     int
	Y     int
}

type fireField struct {
	Order     uint64
	EventID   int32
	MapID     string
	X         int
	Y         int
	OwnerID   string
	Damage    int
	ExpiresAt time.Time
	NextTick  time.Time
}

func (w *World) fireWallDurationLocked(ch storage.Character, skill data.StdSkill, state storage.SkillState) time.Duration {
	duration := w.spellPowerFromBaseLocked(10, skill, state)
	combat := w.combatStatsLocked(ch)
	low := combat.MC
	high := combat.MCMax
	if high < low {
		high = low
	}
	if high > low {
		duration += (low + w.rand.Intn(high-low+1)) / 2
	} else {
		duration += low / 2
	}
	if duration < 1 {
		duration = 1
	}
	return time.Duration(duration) * time.Second
}

func (w *World) fireWallDamageLocked(ch storage.Character, skill data.StdSkill, state storage.SkillState) int {
	damage := w.spellMonsterDamageLocked(ch, skill, state)
	if damage < 1 {
		return 1
	}
	return damage
}

func (w *World) fireWallCellsLocked(mapID string, x, y int) []fireFieldKey {
	cells := []fireFieldKey{
		{MapID: mapID, X: x, Y: y - 1},
		{MapID: mapID, X: x - 1, Y: y},
		{MapID: mapID, X: x, Y: y},
		{MapID: mapID, X: x + 1, Y: y},
		{MapID: mapID, X: x, Y: y + 1},
	}
	seen := make(map[fireFieldKey]struct{}, len(cells))
	out := make([]fireFieldKey, 0, len(cells))
	for _, cell := range cells {
		if _, ok := seen[cell]; ok {
			continue
		}
		seen[cell] = struct{}{}
		out = append(out, cell)
	}
	return out
}

func (w *World) castFireWallLocked(ch storage.Character, skill data.StdSkill, state storage.SkillState, targetX, targetY int, now time.Time) int {
	created, _ := w.castFireWallWithEventsLocked(ch, skill, state, targetX, targetY, now)
	return created
}

func (w *World) castFireWallWithEventsLocked(ch storage.Character, skill data.StdSkill, state storage.SkillState, targetX, targetY int, now time.Time) (int, []SpellGroundEvent) {
	if _, ok := w.data.Maps[ch.MapID]; !ok {
		return 0, nil
	}
	damage := w.fireWallDamageLocked(ch, skill, state)
	duration := w.fireWallDurationLocked(ch, skill, state)
	createdEvents := make([]SpellGroundEvent, 0, 5)
	for _, cell := range w.fireWallCellsLocked(ch.MapID, targetX, targetY) {
		key := cell
		if _, ok := w.fireFields[key]; ok || w.groundEventAtLocked(cell) {
			continue
		}
		w.nextFireFieldID++
		eventID := w.nextGroundEventIDLocked()
		w.fireFields[key] = fireField{
			Order:     w.nextFireFieldID,
			EventID:   eventID,
			MapID:     cell.MapID,
			X:         cell.X,
			Y:         cell.Y,
			OwnerID:   ch.ID,
			Damage:    damage,
			ExpiresAt: now.Add(duration),
			NextTick:  now.Add(fireWallTickInterval),
		}
		event := SpellGroundEvent{
			ID: eventID, MapID: cell.MapID, X: cell.X, Y: cell.Y,
			Type: 5, Duration: duration, StartAt: now,
		}
		w.groundEvents[eventID] = event
		createdEvents = append(createdEvents, event)
	}
	return 1, createdEvents
}

func (w *World) groundEventAtLocked(cell fireFieldKey) bool {
	for _, event := range w.groundEvents {
		if event.MapID == cell.MapID && event.X == cell.X && event.Y == cell.Y {
			return true
		}
	}
	return false
}

func (w *World) applyFireWallTickLocked(players map[string]storage.Character, now time.Time) ([]AttackResult, []CharacterHit, []storage.Character) {
	monsterHits := []AttackResult{}
	characterHits := []CharacterHit{}
	updated := []storage.Character{}
	playerList := charactersFromMap(players)
	fields := make([]fireField, 0, len(w.fireFields))
	for _, field := range w.fireFields {
		fields = append(fields, field)
	}
	sort.Slice(fields, func(i, j int) bool {
		if fields[i].Order != fields[j].Order {
			return fields[i].Order < fields[j].Order
		}
		if fields[i].X != fields[j].X {
			return fields[i].X < fields[j].X
		}
		return fields[i].Y < fields[j].Y
	})
	for _, field := range fields {
		key := fireFieldKey{MapID: field.MapID, X: field.X, Y: field.Y}
		if !now.Before(field.ExpiresAt) {
			delete(w.fireFields, key)
			continue
		}
		if now.Before(field.NextTick) {
			continue
		}
		owner, ok := players[field.OwnerID]
		if !ok {
			owner = storage.Character{ID: field.OwnerID, MapID: field.MapID, X: field.X, Y: field.Y}
		}
		for _, areaTarget := range w.spellAreaTargetsLocked(playerList, field.MapID, field.X, field.Y, 0) {
			if areaTarget.Monster != nil {
				mon := areaTarget.Monster
				if !w.isProperMonsterTargetLocked(owner, playerList, mon) {
					continue
				}
				attackResult, err := w.attackMonsterWithImmediateMagicDamageLocked(owner, mon, field.Damage)
				if err != nil || attackResult.Damage <= 0 {
					continue
				}
				monsterHits = append(monsterHits, attackResult)
				continue
			}
			target := *areaTarget.Character
			if !w.isProperCharacterTargetLocked(owner, target) {
				continue
			}
			updatedTarget, hit, err := w.spellCharacterDamageWithPowerLocked(owner, target, field.Damage)
			if err != nil || hit.Damage <= 0 {
				continue
			}
			characterHits = append(characterHits, hit)
			updated = append(updated, updatedTarget)
		}
		field.NextTick = now.Add(fireWallTickInterval)
		w.fireFields[key] = field
	}
	return monsterHits, characterHits, updated
}

func charactersFromMap(players map[string]storage.Character) []storage.Character {
	out := make([]storage.Character, 0, len(players))
	for _, ch := range players {
		out = append(out, ch)
	}
	return out
}
