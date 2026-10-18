package world

import "time"

type statusClockKind uint8

const (
	statusClockTransparent statusClockKind = iota
	statusClockParalyzed
	statusClockDefence
	statusClockMagicDefence
	statusClockBubbleDefence
	statusClockShowHP
)

type statusClock struct {
	SourceUntil int64
	TickAt      int64
	Remaining   int
}

func (w *World) advanceStatusClockLocked(clocks map[string]map[statusClockKind]statusClock, id string, kind statusClockKind, until int64, now time.Time) (int64, bool, bool) {
	if until <= 0 {
		if entries := clocks[id]; entries != nil {
			delete(entries, kind)
			if len(entries) == 0 {
				delete(clocks, id)
			}
		}
		return 0, false, false
	}
	nowNS := now.UnixNano()
	entries := clocks[id]
	if entries == nil {
		entries = map[statusClockKind]statusClock{}
		clocks[id] = entries
	}
	clock, ok := entries[kind]
	if !ok || clock.SourceUntil != until {
		remaining := int((until - nowNS + int64(time.Second) - 1) / int64(time.Second))
		if remaining < 1 {
			remaining = 1
		}
		clock = statusClock{SourceUntil: until, TickAt: until - int64(remaining)*int64(time.Second), Remaining: remaining}
	}
	changed := false
	if nowNS-clock.TickAt > int64(time.Second) {
		clock.Remaining--
		clock.TickAt += int64(time.Second)
		changed = true
	}
	if clock.Remaining <= 0 {
		delete(entries, kind)
		return 0, false, true
	}
	entries[kind] = clock
	if changed {
		nextUntil := nowNS + int64(clock.Remaining)*int64(time.Second)
		clock.SourceUntil = nextUntil
		entries[kind] = clock
		return nextUntil, true, true
	}
	return until, true, false
}

func (w *World) advanceCharacterStatusClockLocked(id string, kind statusClockKind, until int64, now time.Time) (int64, bool, bool) {
	if w.characterStatusClocks == nil {
		w.characterStatusClocks = make(map[string]map[statusClockKind]statusClock)
	}
	return w.advanceStatusClockLocked(w.characterStatusClocks, id, kind, until, now)
}

func (w *World) resetCharacterStatusClockLocked(id string, kind statusClockKind, until int64, now time.Time) {
	if w.characterStatusClocks == nil {
		w.characterStatusClocks = make(map[string]map[statusClockKind]statusClock)
	}
	if until <= 0 {
		delete(w.characterStatusClocks[id], kind)
		return
	}
	remaining := int((until - now.UnixNano() + int64(time.Second) - 1) / int64(time.Second))
	if remaining < 1 {
		remaining = 1
	}
	entries := w.characterStatusClocks[id]
	if entries == nil {
		entries = make(map[statusClockKind]statusClock)
		w.characterStatusClocks[id] = entries
	}
	entries[kind] = statusClock{SourceUntil: until, TickAt: now.UnixNano(), Remaining: remaining}
}

func (w *World) advanceMonsterStatusClockLocked(id string, kind statusClockKind, until time.Time, now time.Time) (time.Time, bool, bool) {
	if w.monsterStatusClocks == nil {
		w.monsterStatusClocks = make(map[string]map[statusClockKind]statusClock)
	}
	next, active, changed := w.advanceStatusClockLocked(w.monsterStatusClocks, id, kind, timestampOrZero(until), now)
	if !active {
		return time.Time{}, false, changed
	}
	return time.Unix(0, next), true, changed
}

func (w *World) resetMonsterStatusClockLocked(id string, kind statusClockKind, until time.Time, now time.Time) {
	if w.monsterStatusClocks == nil {
		w.monsterStatusClocks = make(map[string]map[statusClockKind]statusClock)
	}
	if until.IsZero() {
		delete(w.monsterStatusClocks[id], kind)
		return
	}
	remaining := int((until.UnixNano() - now.UnixNano() + int64(time.Second) - 1) / int64(time.Second))
	if remaining < 1 {
		remaining = 1
	}
	entries := w.monsterStatusClocks[id]
	if entries == nil {
		entries = make(map[statusClockKind]statusClock)
		w.monsterStatusClocks[id] = entries
	}
	entries[kind] = statusClock{SourceUntil: until.UnixNano(), TickAt: now.UnixNano(), Remaining: remaining}
}

func timestampOrZero(value time.Time) int64 {
	if value.IsZero() {
		return 0
	}
	return value.UnixNano()
}
