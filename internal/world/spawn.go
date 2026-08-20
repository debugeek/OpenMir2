package world

import (
	"fmt"
	"math"
	"time"

	"openmir2/internal/data"
	"openmir2/internal/storage"
)

func (w *World) spawnInitial() {
	for _, sp := range w.data.Spawns {
		tpl := w.data.Monsters[sp.MonsterID]
		desired := w.desiredSpawnCountLocked(sp)
		cluster := sp.MissionGenRate > 0 && w.rand.Intn(100) < sp.MissionGenRate
		centerX, centerY := sp.X, sp.Y
		if cluster {
			if x, y, ok := w.findRandomSpawnPositionLocked(sp.MapID, sp.X, sp.Y, sp.Range, ""); ok {
				centerX, centerY = x, y
			} else {
				cluster = false
			}
		}
		for i := 0; i < desired; i++ {
			var x, y int
			var ok bool
			if cluster {
				x, y, ok = w.findRandomSpawnPositionLocked(sp.MapID, centerX-10+w.rand.Intn(21), centerY-10+w.rand.Intn(21), 0, "")
				if !ok {
					x, y, ok = w.findRandomSpawnPositionAvoidLocked(sp.MapID, centerX, centerY, 10, "", -1, -1)
				}
			} else {
				x, y, ok = w.findRandomSpawnPositionLocked(sp.MapID, sp.X, sp.Y, sp.Range, "")
			}
			if !ok {
				break
			}
			mon := w.createSpawnMonsterLocked(sp, tpl, x, y)
			if mon != nil {
				w.occupyMonsterLocked(mon)
			}
		}
	}
}

func (w *World) respawnLocked(now time.Time) {
	for _, mon := range w.monsters {
		if !mon.Alive && !mon.RespawnAt.IsZero() && !now.Before(mon.RespawnAt) {
			tpl, ok := w.data.Monsters[mon.TemplateID]
			if !ok {
				continue
			}
			x, y, ok := w.spawnPositionForSpawnLocked(mon.Spawn, mon.ID)
			if !ok {
				mon.RespawnAt = now.Add(time.Second)
				continue
			}
			mon.HP = mon.MaxHP
			mon.Alive = true
			mon.X, mon.Y = x, y
			mon.Dir = 4
			if mon.Race == 107 {
				mon.Dir = 5
			}
			w.spawnStateForLocked(mon.Spawn).activeCount++
			w.occupyMonsterLocked(mon)
			mon.RespawnAt = time.Time{}
			mon.TargetCharacterID = ""
			mon.TargetFocusAt = time.Time{}
			mon.LastAttackAt = time.Time{}
			mon.LastWalkAt = time.Time{}
			mon.WalkCount = 0
			mon.WalkWaitTick = time.Time{}
			mon.WalkWaitLocked = false
			mon.Hidden = false
			mon.FixedHideMode = false
			mon.StoneMode = false
			mon.Animal = false
			mon.FleeOnSight = false
			mon.RunAwayMode = false
			mon.FirstRevealPending = false
			mon.GuardDirection = 4
			if mon.Race == 112 {
				mon.GuardDirection = mon.Dir
			}
			mon.AttackCount = 0
			mon.AttackMax = 0
			mon.UseMagic = false
			applyMonsterTemplateState(mon, tpl)
			mon.AppearStartAt = time.Time{}
			mon.ParentID = ""
			mon.ExplosionStartAt = time.Time{}
			if mon.Race == 117 {
				mon.ExplosionStartAt = now
			}
			mon.TargetX = -1
			mon.TargetY = -1
		}
	}
}

func (w *World) desiredSpawnCountLocked(spawn data.StdSpawn) int {
	rate := 10
	if mp, ok := w.data.Maps[spawn.MapID]; ok && mp.MonsterSpawnRate > 0 {
		rate = mp.MonsterSpawnRate
	}
	return desiredSpawnCountForRate(spawn.Count, rate)
}

func desiredSpawnCountForRate(count, rate int) int {
	if rate <= 0 {
		rate = 10
	}
	desired := int(math.Round(math.Max(1, float64(count)) / (float64(rate) / 10.0)))
	if desired < 1 {
		return 1
	}
	return desired
}

func spawnStateKey(spawn data.StdSpawn) string {
	if spawn.ID != "" {
		return spawn.ID
	}
	return fmt.Sprintf("%s:%s:%d:%d:%d:%d:%d:%d", spawn.MapID, spawn.MonsterID, spawn.X, spawn.Y, spawn.Range, spawn.Count, spawn.RespawnSeconds, spawn.MissionGenRate)
}

func (w *World) spawnStateForLocked(spawn data.StdSpawn) *spawnState {
	key := spawnStateKey(spawn)
	if state, ok := w.spawns[key]; ok {
		return state
	}
	state := &spawnState{spawn: spawn}
	w.spawns[key] = state
	return state
}

func (w *World) createSpawnMonsterLocked(spawn data.StdSpawn, tpl data.StdMonster, x, y int) *Monster {
	id := fmt.Sprintf("mon-%d", w.nextID)
	w.nextID++
	if spawn.ID == "" {
		spawn.ID = spawnStateKey(spawn)
	}
	mon := newMonster(w, id, tpl, spawn.MapID, x, y, spawn)
	w.monsters[id] = mon
	w.spawnStateForLocked(spawn).activeCount++
	return mon
}

func (w *World) rollDropsLocked(mon *Monster, ownerID string, blockers ...storage.Character) []GroundDrop {
	table, ok := w.data.Drops[mon.DropTable]
	if !ok {
		return nil
	}
	out := []GroundDrop{}
	for _, entry := range table.Entries {
		if entry.Chance <= 0 || w.rand.Float64() >= entry.Chance {
			continue
		}
		count := entry.MinCount
		if entry.MaxCount > entry.MinCount {
			count += w.rand.Intn(entry.MaxCount - entry.MinCount + 1)
		}
		id := fmt.Sprintf("drop-%d", w.nextID)
		item, ok := w.data.Items[entry.ItemID]
		if !ok {
			continue
		}
		instance := w.createUserItemFromStd(item, 0, [14]byte{})
		out = append(out, GroundDrop{
			ID:        id,
			MapID:     mon.MapID,
			ItemID:    entry.ItemID,
			Count:     count,
			MakeIndex: instance.MakeIndex,
			OwnerID:   ownerID,
			PickupAt:  time.Now().Add(time.Duration(w.gameplay.Item.FloorItemCanPickUpMS) * time.Millisecond),
			Dura:      instance.Dura,
			DuraMax:   instance.DuraMax,
		})
	}
	return w.placeDropsLocked(mon.MapID, mon.X, mon.Y, 3, out, blockers...)
}

func (w *World) spawnPositionForSpawnLocked(spawn data.StdSpawn, exceptID string) (int, int, bool) {
	cluster := spawn.MissionGenRate > 0 && w.rand.Intn(100) < spawn.MissionGenRate
	if cluster {
		centerX, centerY, ok := w.findRandomSpawnPositionLocked(spawn.MapID, spawn.X, spawn.Y, spawn.Range, exceptID)
		if !ok {
			return 0, 0, false
		}
		x, y, ok := w.findRandomSpawnPositionAvoidLocked(spawn.MapID, centerX-10+w.rand.Intn(21), centerY-10+w.rand.Intn(21), 0, exceptID, -1, -1)
		if ok {
			return x, y, true
		}
		return w.findRandomSpawnPositionAvoidLocked(spawn.MapID, centerX, centerY, 10, exceptID, -1, -1)
	}
	return w.findRandomSpawnPositionLocked(spawn.MapID, spawn.X, spawn.Y, spawn.Range, exceptID)
}

func (w *World) findSpawnPositionLocked(mapID string, x, y, searchRadius int, exceptID string) (int, int, bool) {
	return w.findSpawnPositionAvoidLocked(mapID, x, y, searchRadius, exceptID, -1, -1)
}

func (w *World) findSpawnPositionAvoidLocked(mapID string, x, y, searchRadius int, exceptID string, avoidX, avoidY int) (int, int, bool) {
	mp, ok := w.data.Maps[mapID]
	if !ok {
		return 0, 0, false
	}
	if searchRadius == 0 {
		searchRadius = 1
	}
	for radius := 0; radius <= searchRadius; radius++ {
		for yy := y - radius; yy <= y+radius; yy++ {
			for xx := x - radius; xx <= x+radius; xx++ {
				if radius > 0 && xx > x-radius && xx < x+radius && yy > y-radius && yy < y+radius {
					continue
				}
				if xx == avoidX && yy == avoidY {
					continue
				}
				if mp.Walkable(xx, yy) && !w.monsterAtLocked(mapID, xx, yy, exceptID) {
					return xx, yy, true
				}
			}
		}
	}
	return 0, 0, false
}

func (w *World) findRandomSpawnPositionLocked(mapID string, x, y, searchRadius int, exceptID string) (int, int, bool) {
	return w.findRandomSpawnPositionAvoidLocked(mapID, x, y, searchRadius, exceptID, -1, -1)
}

func (w *World) findRandomSpawnPositionAvoidLocked(mapID string, x, y, searchRadius int, exceptID string, avoidX, avoidY int) (int, int, bool) {
	mp, ok := w.data.Maps[mapID]
	if !ok {
		return 0, 0, false
	}
	if searchRadius < 0 {
		searchRadius = 0
	}
	for attempt := 0; attempt < 31; attempt++ {
		xx, yy := x, y
		if searchRadius > 0 {
			xx = x - searchRadius + w.rand.Intn(searchRadius*2+1)
			yy = y - searchRadius + w.rand.Intn(searchRadius*2+1)
		}
		if xx == avoidX && yy == avoidY {
			continue
		}
		if mp.Walkable(xx, yy) && !w.monsterAtLocked(mapID, xx, yy, exceptID) {
			return xx, yy, true
		}
	}
	return w.findSpawnPositionAvoidLocked(mapID, x, y, searchRadius, exceptID, avoidX, avoidY)
}
