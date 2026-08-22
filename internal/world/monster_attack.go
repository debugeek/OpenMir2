package world

import (
	"fmt"
	"time"

	"openmir2/internal/storage"
	"openmir2/internal/world/core"
)

func (w *World) Attack(ch storage.Character, monsterID string, blockers ...storage.Character) (AttackResult, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.respawnLocked(time.Now())
	mon, ok := w.monsters[monsterID]
	if !ok || !mon.Alive {
		return AttackResult{}, fmt.Errorf("monster not found")
	}
	if mon.MapID != ch.MapID {
		return AttackResult{}, fmt.Errorf("monster is on another map")
	}
	if mon.Hidden {
		return AttackResult{}, fmt.Errorf("monster is hidden")
	}
	if abs(ch.X-mon.X) > 1 || abs(ch.Y-mon.Y) > 1 {
		return AttackResult{}, fmt.Errorf("monster is out of range")
	}
	return w.attackLocked(ch, mon, blockers...)
}

func (w *World) attackLocked(ch storage.Character, mon *Monster, blockers ...storage.Character) (AttackResult, error) {
	now := time.Now()
	damage := w.characterAttackDamageLocked(ch, mon)
	if damage < 1 {
		damage = 1
	}
	hp := core.ApplyHPDelta(mon.HP, mon.MaxHP, -damage)
	mon.HP = hp.HP
	mon.TargetCharacterID = ch.ID
	mon.TargetFocusAt = now
	mon.NextSearchAt = now.Add(time.Duration(w.monsterSearchHasTargetMSLocked(mon)) * time.Millisecond)
	result := AttackResult{
		MonsterID:      mon.ID,
		Damage:         damage,
		MonsterHP:      hp.HP,
		MonsterMaxHP:   mon.MaxHP,
		MonsterRaceImg: mon.RaceImg,
		MonsterWeapon:  mon.MonsterWeapon,
		MonsterAppr:    mon.Appr,
		MonsterX:       mon.X,
		MonsterY:       mon.Y,
		MonsterDir:     mon.Dir,
	}
	if hp.Dead {
		w.vacateMonsterLocked(mon)
		mon.Alive = false
		mon.TargetCharacterID = ""
		mon.TargetFocusAt = time.Time{}
		state := w.spawnStateForLocked(mon.Spawn)
		if state.activeCount > 0 {
			state.activeCount--
		}
		delay := mon.Spawn.RespawnSeconds
		if delay > 0 {
			mon.RespawnAt = now.Add(time.Duration(delay) * time.Second)
		}
		var expGained int
		var leveled bool
		var err error
		ch, _, expGained, leveled, err = gainExperienceLocked(w, ch, mon.Experience)
		if err != nil {
			return AttackResult{}, err
		}
		result.Experience = expGained
		result.CurrentExp = ch.Experience
		result.LevelUp = leveled
		result.Dead = true
		if mon.Animal {
			mon.RunAwayMode = true
			mon.TargetCharacterID = ch.ID
			mon.TargetFocusAt = now
			mon.TargetX, mon.TargetY = fleePointForMonster(mon, ch)
			mon.NextSearchAt = now.Add(time.Duration(w.monsterSearchHasTargetMSLocked(mon)) * time.Millisecond)
		}
		result.Drops = w.rollDropsLocked(mon, ch.ID, blockers...)
	}
	result.Character = ch
	return result, w.store.SaveCharacter(ch)
}

func (w *World) characterAttackDamageLocked(ch storage.Character, mon *Monster) int {
	stats := w.combatStatsLocked(ch)
	minAttack := 3 + max(ch.Level, 1) + stats.DC
	maxAttack := 3 + max(ch.Level, 1) + stats.DCMax
	if maxAttack < minAttack {
		maxAttack = minAttack
	}
	damage := minAttack
	if maxAttack > minAttack {
		damage += w.rand.Intn(maxAttack - minAttack + 1)
	}
	return damage - mon.Defense
}

func (w *World) monsterAttackCharacterLocked(mon *Monster, ch storage.Character) (storage.Character, CharacterHit, error) {
	damage := mon.MinAttack
	if mon.MaxAttack > mon.MinAttack {
		damage += w.rand.Intn(mon.MaxAttack - mon.MinAttack + 1)
	}
	if damage < 1 {
		damage = 1
	}
	return w.monsterAttackCharacterWithDamageLocked(mon, ch, damage)
}

func (w *World) monsterAttackCharacterWithDamageLocked(mon *Monster, ch storage.Character, damage int) (storage.Character, CharacterHit, error) {
	if damage < 1 {
		damage = 1
	}
	change := core.ApplyVitalDelta(ch, -damage, 0)
	ch = change.Character
	dead := false
	if change.Dead {
		dead = true
		mon.TargetCharacterID = ""
		mon.TargetFocusAt = time.Time{}
		mon.NextSearchAt = time.Now()
	}
	hit := CharacterHit{
		Character:       ch,
		Damage:          damage,
		AttackerID:      mon.ID,
		AttackerRaceImg: mon.RaceImg,
		AttackerAppr:    mon.Appr,
		AttackerX:       mon.X,
		AttackerY:       mon.Y,
		Dead:            dead,
	}
	return ch, hit, w.store.SaveCharacter(ch)
}
