package world

import (
	"time"

	"openmir2/internal/storage"
)

func (w *World) applyCharacterTemporaryAbilityTickLocked(ch storage.Character, now time.Time) (storage.Character, bool) {
	next := ch
	changed := false
	expires := now.UnixNano()
	for i := 0; i < tempAbilityCount && i < len(next.ExtraAbil); i++ {
		if next.ExtraAbilTimes[i] <= 0 || next.ExtraAbilTimes[i] > expires {
			continue
		}
		next.ExtraAbil[i] = 0
		next.ExtraAbilTimes[i] = 0
		changed = true
	}
	if !changed {
		return ch, false
	}
	return next, true
}
