package world

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"openmir2/internal/data"
)

func (w *World) monsterTemplateByIDLocked(id string) (data.StdMonster, bool) {
	name := strings.TrimSpace(id)
	if name == "" {
		return data.StdMonster{}, false
	}
	if tpl, ok := w.data.Monsters[name]; ok {
		return tpl, true
	}
	return data.StdMonster{}, false
}

func newMonster(w *World, id string, tpl data.StdMonster, mapID string, x, y int, spawn data.StdSpawn) *Monster {
	now := time.Now()
	mon := &Monster{
		ID: id, TemplateID: tpl.ID, Name: tpl.Name, Race: tpl.Race, RaceImg: tpl.RaceImg, MonsterWeapon: tpl.MP & 0xFF, Appr: tpl.Appr,
		Level: tpl.Level, Undead: tpl.Undead, MapID: mapID, X: x, Y: y, Dir: 4, TargetX: -1, TargetY: -1, CoolEye: tpl.CoolEye,
		ViewRange:        tpl.ViewRange,
		LeashRange:       tpl.LeashRange,
		SearchNoTargetMS: tpl.SearchNoTargetMS, SearchHasTargetMS: tpl.SearchHasTargetMS,
		HP: tpl.HP, MaxHP: tpl.HP, MP: tpl.MP, MaxMP: tpl.MP, MinAttack: tpl.MinAttack,
		MaxAttack: tpl.MaxAttack, Defense: tpl.Defense, MagicDefense: tpl.MagicDefense,
		MagicAttack: tpl.MagicAttack, TaoAttack: tpl.TaoAttack, Speed: tpl.Speed, Hit: tpl.Hit,
		WalkSpeedMS: tpl.WalkSpeedMS, WalkStep: tpl.WalkStep, WalkWait: tpl.WalkWait,
		AttackIntervalMS: tpl.AttackIntervalMS, Experience: tpl.Experience,
		Alive: true, Spawn: spawn,
	}
	if _, ok := w.data.Drops[tpl.ID]; ok {
		mon.DropTable = tpl.ID
	}
	applyMonsterTemplateState(mon, tpl)
	if mon.AttackMax > 0 {
		mon.AttackCount = 0
	}
	mon.LastWalkAt = now.Add(-time.Duration(w.rand.Intn(3000)) * time.Millisecond)
	return mon
}

func applyMonsterTemplateState(mon *Monster, tpl data.StdMonster) {
	if tpl.Animal {
		mon.Animal = true
	}
	if tpl.FleeOnSight {
		mon.FleeOnSight = true
	}
	if tpl.UseMagic {
		mon.UseMagic = true
	}
	if tpl.Hidden {
		mon.Hidden = true
	}
	if tpl.FixedHideMode {
		mon.FixedHideMode = true
	}
	if tpl.StoneMode {
		mon.StoneMode = true
	}
	if tpl.FirstRevealPending {
		mon.FirstRevealPending = true
	}
	if tpl.AttackMax > 0 {
		mon.AttackMax = tpl.AttackMax
	}
	switch tpl.Race {
	case 107:
		mon.Dir = 5
	case 112:
		mon.GuardDirection = mon.Dir
	case 117:
		mon.ExplosionStartAt = time.Now()
	case 132:
		mon.Hidden = true
		mon.FixedHideMode = true
		mon.StoneMode = true
	}
}

// idSeq extracts the trailing sequence number from generated IDs like
// "mon-12" or "drop-3" so they sort numerically instead of lexically
// (which would otherwise put "mon-10" before "mon-2").
func idSeq(id string) int {
	_, suffix, ok := strings.Cut(id, "-")
	if !ok {
		return 0
	}
	n, err := strconv.Atoi(suffix)
	if err != nil {
		return 0
	}
	return n
}

func (w *World) SpawnMonsterByName(mapID string, x, y int, name string, count int) (SpawnResult, error) {
	return w.spawnMonsterByName(mapID, x, y, name, count, -1, -1)
}

func (w *World) SpawnMonsterByNameAvoid(mapID string, x, y int, name string, count, avoidX, avoidY int) (SpawnResult, error) {
	return w.spawnMonsterByName(mapID, x, y, name, count, avoidX, avoidY)
}

func (w *World) SpawnMonsterByNameAt(mapID string, x, y int, name string, count int) (SpawnResult, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	mp, ok := w.data.Maps[mapID]
	if !ok {
		return SpawnResult{}, fmt.Errorf("map %s not found", mapID)
	}
	if !mp.Walkable(x, y) {
		return SpawnResult{}, fmt.Errorf("spawn coordinate is blocked")
	}
	tpl, ok := w.monsterTemplateByIDLocked(name)
	if !ok {
		return SpawnResult{}, fmt.Errorf("monster %s not found", name)
	}
	if count <= 0 {
		count = 1
	}
	if count > 64 {
		count = 64
	}
	result := SpawnResult{Monsters: make([]Monster, 0, count)}
	for i := 0; i < count; i++ {
		id := fmt.Sprintf("mon-%d", w.nextID)
		w.nextID++
		spawn := data.StdSpawn{
			MapID:          mapID,
			MonsterID:      tpl.ID,
			X:              x,
			Y:              y,
			Count:          1,
			RespawnSeconds: 0,
		}
		mon := newMonster(w, id, tpl, mapID, x, y, spawn)
		w.monsters[id] = mon
		if count == 1 {
			w.occupyMonsterLocked(mon)
		}
		result.Monsters = append(result.Monsters, *mon)
	}
	return result, nil
}

func (w *World) spawnMonsterByName(mapID string, x, y int, name string, count, avoidX, avoidY int) (SpawnResult, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	mp, ok := w.data.Maps[mapID]
	if !ok {
		return SpawnResult{}, fmt.Errorf("map %s not found", mapID)
	}
	if !mp.Walkable(x, y) {
		return SpawnResult{}, fmt.Errorf("spawn coordinate is blocked")
	}
	tpl, ok := w.monsterTemplateByIDLocked(name)
	if !ok {
		return SpawnResult{}, fmt.Errorf("monster %s not found", name)
	}
	if count <= 0 {
		count = 1
	}
	if count > 64 {
		count = 64
	}
	result := SpawnResult{Monsters: make([]Monster, 0, count)}
	for i := 0; i < count; i++ {
		spawnX, spawnY, ok := w.findSpawnPositionAvoidLocked(mapID, x, y, min(count, 4), "", avoidX, avoidY)
		if !ok {
			break
		}
		id := fmt.Sprintf("mon-%d", w.nextID)
		w.nextID++
		spawn := data.StdSpawn{
			MapID:          mapID,
			MonsterID:      tpl.ID,
			X:              x,
			Y:              y,
			Count:          1,
			RespawnSeconds: 0,
		}
		mon := newMonster(w, id, tpl, mapID, spawnX, spawnY, spawn)
		w.monsters[id] = mon
		w.occupyMonsterLocked(mon)
		result.Monsters = append(result.Monsters, *mon)
	}
	if len(result.Monsters) == 0 {
		return SpawnResult{}, fmt.Errorf("no available spawn position for monster %s", name)
	}
	return result, nil
}
