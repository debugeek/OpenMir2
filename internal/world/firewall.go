package world

import (
	"time"

	"openmir2/internal/data"
	"openmir2/internal/storage"
)

const fireWallTickInterval = time.Second

type fireFieldKey struct {
	MapID string
	X     int
	Y     int
}

type fireField struct {
	MapID     string
	X         int
	Y         int
	OwnerID   string
	Damage    int
	ExpiresAt time.Time
	NextTick  time.Time
}

func (w *World) fireWallDurationLocked(ch storage.Character, skill data.StdSkill, state storage.SkillState) time.Duration {
	base := data.StdSkill{Power: 10, MaxPower: 10, TrainLevel1: skill.TrainLevel1}
	duration := w.spellScaledPowerLocked(base, state)
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
		{MapID: mapID, X: x - 1, Y: y - 2},
		{MapID: mapID, X: x + 1, Y: y - 2},
		{MapID: mapID, X: x - 2, Y: y - 1},
		{MapID: mapID, X: x + 2, Y: y - 1},
		{MapID: mapID, X: x - 2, Y: y + 1},
		{MapID: mapID, X: x + 2, Y: y + 1},
		{MapID: mapID, X: x - 1, Y: y + 2},
		{MapID: mapID, X: x + 1, Y: y + 2},
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
	damage := w.fireWallDamageLocked(ch, skill, state)
	duration := w.fireWallDurationLocked(ch, skill, state)
	created := 0
	for _, cell := range w.fireWallCellsLocked(ch.MapID, targetX, targetY) {
		key := cell
		if existing, ok := w.fireFields[key]; ok && now.Before(existing.ExpiresAt) {
			continue
		}
		w.fireFields[key] = fireField{
			MapID:     cell.MapID,
			X:         cell.X,
			Y:         cell.Y,
			OwnerID:   ch.ID,
			Damage:    damage,
			ExpiresAt: now.Add(duration),
			NextTick:  now,
		}
		created++
	}
	return created
}

func (w *World) applyFireWallTickLocked(players map[string]storage.Character, now time.Time) ([]AttackResult, []CharacterHit, []storage.Character) {
	monsterHits := []AttackResult{}
	characterHits := []CharacterHit{}
	updated := []storage.Character{}
	playerList := charactersFromMap(players)
	for key, field := range w.fireFields {
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
		for _, mon := range w.monstersInRadiusLocked(field.MapID, field.X, field.Y, 0) {
			attackResult, err := w.attackMonsterWithDamageLocked(owner, mon, field.Damage)
			if err != nil {
				continue
			}
			monsterHits = append(monsterHits, attackResult)
		}
		for _, target := range w.charactersInRadiusLocked(playerList, field.MapID, field.X, field.Y, 0) {
			updatedTarget, hit, err := w.spellCharacterDamageWithPowerLocked(owner, target, field.Damage)
			if err != nil {
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
