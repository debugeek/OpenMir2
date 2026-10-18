package world

import (
	"time"

	"openmir2/internal/storage"
)

func (w *World) applyCharacterProtectionTickLocked(ch storage.Character, now time.Time) (storage.Character, bool) {
	next := ch
	changed := false
	if next.DefenceUpUntil > 0 {
		until, active, clockChanged := w.advanceCharacterStatusClockLocked(next.ID, statusClockDefence, next.DefenceUpUntil, now)
		if !active {
			next.DefenceUpUntil = 0
			changed = true
		} else if clockChanged {
			next.DefenceUpUntil = until
		}
	}
	if next.MagDefenceUpUntil > 0 {
		until, active, clockChanged := w.advanceCharacterStatusClockLocked(next.ID, statusClockMagicDefence, next.MagDefenceUpUntil, now)
		if !active {
			next.MagDefenceUpUntil = 0
			changed = true
		} else if clockChanged {
			next.MagDefenceUpUntil = until
		}
	}
	if next.BubbleDefenceUntil > 0 {
		until, active, clockChanged := w.advanceCharacterStatusClockLocked(next.ID, statusClockBubbleDefence, next.BubbleDefenceUntil, now)
		if !active {
			next.BubbleDefenceUntil = 0
			activeBubble := false
			next.BubbleDefenceActive = &activeBubble
			changed = true
		} else if clockChanged {
			next.BubbleDefenceUntil = until
		}
	}
	if next.DefenceUpUntil == 0 {
		delete(w.characterStatusClocks[next.ID], statusClockDefence)
	}
	if next.MagDefenceUpUntil == 0 {
		delete(w.characterStatusClocks[next.ID], statusClockMagicDefence)
	}
	if next.BubbleDefenceUntil == 0 {
		delete(w.characterStatusClocks[next.ID], statusClockBubbleDefence)
	}
	if next.DefenceUpUntil != ch.DefenceUpUntil || next.MagDefenceUpUntil != ch.MagDefenceUpUntil || next.BubbleDefenceUntil != ch.BubbleDefenceUntil {
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
	if mon.DefenceUpUntil > 0 {
		until, active, clockChanged := w.advanceMonsterStatusClockLocked(mon.ID, statusClockDefence, time.Unix(0, mon.DefenceUpUntil), now)
		if !active {
			mon.DefenceUpUntil = 0
			changed = true
		} else if clockChanged {
			mon.DefenceUpUntil = until.UnixNano()
		}
	}
	if mon.MagDefenceUpUntil > 0 {
		until, active, clockChanged := w.advanceMonsterStatusClockLocked(mon.ID, statusClockMagicDefence, time.Unix(0, mon.MagDefenceUpUntil), now)
		if !active {
			mon.MagDefenceUpUntil = 0
			changed = true
		} else if clockChanged {
			mon.MagDefenceUpUntil = until.UnixNano()
		}
	}
	return changed
}

func (w *World) applyCharacterShowHPOpenTickLocked(ch storage.Character, now time.Time) (storage.Character, bool) {
	if ch.ShowHPOpenAt <= 0 || now.UnixNano() < ch.ShowHPOpenAt {
		return ch, false
	}
	next := ch
	if next.ShowHPDuration > 0 {
		next.ShowHPUntil = next.ShowHPOpenAt - (1500 * int64(time.Millisecond)) + next.ShowHPDuration
		next.ShowHPDuration = 0
	}
	next.ShowHPOpenAt = 0
	return next, true
}

func (w *World) applyCharacterShowHPTickLocked(ch storage.Character, now time.Time) (storage.Character, bool) {
	if ch.ShowHPUntil <= 0 {
		return ch, false
	}
	next := ch
	if now.UnixNano() > ch.ShowHPUntil {
		w.advanceCharacterStatusClockLocked(ch.ID, statusClockShowHP, 0, now)
		next.ShowHPUntil = 0
		next.ShowHPDuration = 0
		return next, true
	}
	until, active, clockChanged := w.advanceCharacterStatusClockLocked(ch.ID, statusClockShowHP, ch.ShowHPUntil, now)
	if !active {
		next.ShowHPUntil = 0
		next.ShowHPDuration = 0
		return next, true
	}
	if clockChanged {
		next.ShowHPUntil = until
		return next, true
	}
	return ch, false
}

func (w *World) applyMonsterShowHPOpenTickLocked(mon *Monster, now time.Time) bool {
	if mon == nil || mon.ShowHPOpenAt <= 0 || now.UnixNano() < mon.ShowHPOpenAt {
		return false
	}
	if mon.ShowHPDuration > 0 {
		mon.ShowHPUntil = mon.ShowHPOpenAt - 1500*int64(time.Millisecond) + mon.ShowHPDuration
		mon.ShowHPDuration = 0
	}
	mon.ShowHPOpenAt = 0
	return true
}

func (w *World) applyMonsterShowHPTickLocked(mon *Monster, now time.Time) bool {
	if mon == nil || mon.ShowHPUntil <= 0 {
		return false
	}
	if now.UnixNano() > mon.ShowHPUntil {
		w.advanceMonsterStatusClockLocked(mon.ID, statusClockShowHP, time.Time{}, now)
		mon.ShowHPUntil = 0
		mon.ShowHPDuration = 0
		return true
	}
	until, active, clockChanged := w.advanceMonsterStatusClockLocked(mon.ID, statusClockShowHP, time.Unix(0, mon.ShowHPUntil), now)
	if !active {
		mon.ShowHPUntil = 0
		mon.ShowHPDuration = 0
		return true
	}
	if clockChanged {
		mon.ShowHPUntil = until.UnixNano()
		return true
	}
	return false
}
