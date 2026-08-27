package world

import (
	"time"

	"openmir2/internal/storage"
)

func (w *World) applyCharacterProtectionTickLocked(ch storage.Character, now time.Time) (storage.Character, bool) {
	next := ch
	changed := false
	if next.DefenceUpUntil > 0 && now.UnixNano() >= next.DefenceUpUntil {
		next.DefenceUpUntil = 0
		changed = true
	}
	if next.MagDefenceUpUntil > 0 && now.UnixNano() >= next.MagDefenceUpUntil {
		next.MagDefenceUpUntil = 0
		changed = true
	}
	if next.BubbleDefenceUntil > 0 && now.UnixNano() >= next.BubbleDefenceUntil {
		next.BubbleDefenceUntil = 0
		next.BubbleDefenceLevel = 0
		changed = true
	}
	if !changed {
		return ch, false
	}
	return next, true
}

func (w *World) applyMonsterProtectionTickLocked(mon *Monster, now time.Time) bool {
	if mon == nil {
		return false
	}
	changed := false
	if mon.DefenceUpUntil > 0 && now.UnixNano() >= mon.DefenceUpUntil {
		mon.DefenceUpUntil = 0
		changed = true
	}
	if mon.MagDefenceUpUntil > 0 && now.UnixNano() >= mon.MagDefenceUpUntil {
		mon.MagDefenceUpUntil = 0
		changed = true
	}
	return changed
}

func (w *World) applyCharacterShowHPOpenTickLocked(ch storage.Character, now time.Time) (storage.Character, bool) {
	if ch.ShowHPOpenAt <= 0 || now.UnixNano() < ch.ShowHPOpenAt {
		return ch, false
	}
	next := ch
	next.ShowHPOpenAt = 0
	return next, true
}

func (w *World) applyCharacterShowHPTickLocked(ch storage.Character, now time.Time) (storage.Character, bool) {
	if ch.ShowHPUntil <= 0 || now.UnixNano() < ch.ShowHPUntil {
		return ch, false
	}
	next := ch
	next.ShowHPUntil = 0
	return next, true
}
