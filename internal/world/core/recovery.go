package core

import (
	"time"

	"openmir2/internal/storage"
)

func QueueRecovery(ch storage.Character, hp, mp int) storage.Character {
	if hp > 0 {
		ch.IncHealth += hp
	}
	if mp > 0 {
		ch.IncSpell += mp
	}
	return ch
}

func ApplyQueuedRecovery(ch storage.Character, now time.Time) (storage.Character, bool) {
	if ch.HP <= 0 {
		return ch, false
	}
	if ch.IncHealth <= 0 && ch.IncSpell <= 0 && ch.IncHealing <= 0 {
		return ch, false
	}
	interval := recoveryInterval(ch.Level)
	nextAt := time.UnixMilli(ch.IncHealthSpellAt)
	if ch.IncHealthSpellAt != 0 && now.Before(nextAt.Add(interval)) {
		return ch, false
	}
	overrun := now.Sub(nextAt) - interval
	if overrun < 0 {
		overrun = 0
	}
	if overrun > 200*time.Millisecond {
		overrun = 200 * time.Millisecond
	}
	perTickHP := ch.Level/10 + 5
	if perTickHP <= 0 {
		perTickHP = 1
	}
	perTickMP := perTickHP
	perTickHealing := 5
	hp := ch.IncHealth
	if hp > perTickHP {
		hp = perTickHP
	}
	mp := ch.IncSpell
	if mp > perTickMP {
		mp = perTickMP
	}
	healing := ch.IncHealing
	if healing > perTickHealing {
		healing = perTickHealing
	}
	next := ApplyVitalDelta(ch, hp+healing, mp).Character
	next.IncHealth -= hp
	next.IncSpell -= mp
	next.IncHealing -= healing
	next.IncHealthSpellAt = now.Add(overrun).UnixMilli()
	if next.HP == next.MaxHP {
		next.IncHealth = 0
		next.IncHealing = 0
	}
	if next.MP == next.MaxMP {
		next.IncSpell = 0
	}
	if next.IncHealth == ch.IncHealth && next.IncSpell == ch.IncSpell && next.IncHealing == ch.IncHealing && next.HP == ch.HP && next.MP == ch.MP {
		return ch, false
	}
	return next, true
}

func recoveryInterval(level int) time.Duration {
	interval := time.Duration(600-minInt(400, level*10)) * time.Millisecond
	if interval < 200*time.Millisecond {
		return 200 * time.Millisecond
	}
	return interval
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
