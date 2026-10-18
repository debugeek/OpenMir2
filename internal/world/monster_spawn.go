package world

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"openmir2/internal/data"
	"openmir2/internal/storage"
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
	nextRunAt := now
	if w.rand != nil {
		nextRunAt = now.Add(time.Duration(w.rand.Intn(1500)) * time.Millisecond)
	}
	mon := &Monster{
		ID: id, TemplateID: tpl.ID, Name: tpl.Name, Behavior: tpl.Behavior, Race: tpl.Race, RaceImg: tpl.RaceImg, MonsterWeapon: tpl.MP & 0xFF, Appr: tpl.Appr,
		Level: tpl.Level, Undead: tpl.Undead, MapID: mapID, X: x, Y: y, Dir: w.rand.Intn(8), TargetX: -1, TargetY: -1, GuardDirection: -1,
		NoTame:           tpl.NoTame,
		WantRefMsg:       tpl.Race == 112,
		ViewRange:        tpl.ViewRange,
		LeashRange:       tpl.LeashRange,
		SearchNoTargetMS: tpl.SearchNoTargetMS, SearchHasTargetMS: tpl.SearchHasTargetMS,
		HP: tpl.HP, MaxHP: tpl.HP, MP: 0, MaxMP: tpl.MP, MinAttack: tpl.MinAttack,
		MaxAttack: tpl.MaxAttack, Defense: tpl.Defense, MagicDefense: tpl.MagicDefense, MagicDefenseMax: tpl.MagicDefenseMax, AntiMagic: tpl.AntiMagic, AntiPoison: tpl.AntiPoison,
		MagicAttack: tpl.MagicAttack, MagicAttackMax: tpl.MagicAttackMax, TaoAttack: tpl.TaoAttack, Speed: tpl.Speed, Hit: tpl.Hit,
		WalkSpeedMS: tpl.WalkSpeedMS, WalkStep: tpl.WalkStep, WalkWait: tpl.WalkWait,
		RunIntervalMS:    250,
		AttackIntervalMS: tpl.AttackIntervalMS, Experience: tpl.Experience,
		Alive: true, Spawn: spawn, PerHealing: 5, PerHealth: 5, PerSpell: 5, IncHealthSpellAt: now.UnixMilli(), HealthTickAt: now.UnixMilli(), SpellTickAt: now.UnixMilli(), PoisonHealthTickAt: now, NextRunAt: nextRunAt,
		ThinkAt: now,
	}
	mon.CastleDoor = tpl.Behavior == "castle_door"
	mon.CastleWall = tpl.Behavior == "castle_wall"
	w.initializeMonsterGenerationStateLocked(mon, tpl, true)
	if w.nextObjectOrder > 0 {
		mon.ObjectOrder = w.nextObjectOrder
		w.nextObjectOrder++
	}
	if !monsterHasNumericActorID(mon.ID) {
		if w.nextMonsterActorID <= 0 {
			w.nextMonsterActorID = 500000
		}
		mon.ActorID = w.nextMonsterActorID
		w.nextMonsterActorID++
	}
	if _, ok := w.data.Drops[tpl.ID]; ok {
		mon.DropTable = tpl.ID
	}
	w.initializeMonsterDropItemsLocked(mon)
	applyMonsterTemplateState(mon, tpl)
	if tpl.Race == 117 {
		mon.ExplosionStartAt = now
	}
	mon.NextSearchAt = now.Add(time.Duration(w.monsterInitialSearchDelayMSLocked(mon)) * time.Millisecond)
	mon.VisibleSearchIntervalMS = w.monsterVisibleSearchDelayMSLocked(mon)
	if !monsterVisibleSearchStartsImmediately(mon) {
		mon.NextVisibleSearchAt = now.Add(time.Duration(mon.VisibleSearchIntervalMS) * time.Millisecond)
	}
	mon.BaseMaxHP = mon.MaxHP
	mon.BaseMinAttack = mon.MinAttack
	mon.BaseMaxAttack = mon.MaxAttack
	mon.BaseWalkSpeedMS = mon.WalkSpeedMS
	if mon.AttackMax > 0 {
		mon.AttackCount = 0
	}
	mon.LastAttackAt = now.Add(-time.Duration(w.rand.Intn(3000)) * time.Millisecond)
	mon.LastWalkAt = now.Add(-time.Duration(w.rand.Intn(3000)) * time.Millisecond)
	mon.LastTargetSearchAt = now
	if tpl.Race == 92 {
		mon.CowKingMoveAt = now
		mon.CowKingStoredAttack = mon.AttackIntervalMS
		mon.CowKingStoredWalk = mon.WalkSpeedMS
	}
	if tpl.Race == 200 {
		mon.TargetFocusAt = now
	}
	if tpl.Race == 100 {
		applyWhiteSkeletonTiming(mon)
		mon.LastWalkAt = now.Add(2 * time.Second)
	}
	if tpl.Race == 102 {
		mon.StoneDangerLevel = 5
	}
	if tpl.Race == 107 || tpl.Behavior == "centipede_king" {
		mon.TargetFocusAt = now
		mon.CentipedeHideAt = now
	}
	return mon
}

func (w *World) initializeMonsterSchedulingLocked(mon *Monster, now time.Time) {
	mon.NextRunAt = now
	if w.rand != nil {
		mon.NextRunAt = now.Add(time.Duration(w.rand.Intn(1500)) * time.Millisecond)
	}
	mon.NextSearchAt = now.Add(time.Duration(w.monsterInitialSearchDelayMSLocked(mon)) * time.Millisecond)
	if mon.VisibleSearchIntervalMS <= 0 {
		mon.VisibleSearchIntervalMS = w.monsterVisibleSearchDelayMSLocked(mon)
	}
	if monsterVisibleSearchStartsImmediately(mon) {
		mon.NextVisibleSearchAt = time.Time{}
	} else {
		mon.NextVisibleSearchAt = now.Add(time.Duration(mon.VisibleSearchIntervalMS) * time.Millisecond)
	}
}

func (w *World) initializeMonsterGenerationStateLocked(mon *Monster, tpl data.StdMonster, resetZilkinKillCount bool) {
	mon.MeatQuality = initialMonsterMeatQuality(w, tpl, mon.ID)
	mon.CoolEye = 0
	if tpl.CoolEye > 0 && w.rand.Intn(100) < tpl.CoolEye {
		mon.CoolEye = 1
	}
	if tpl.Race == 51 {
		mon.BodyLeathery = 50
	} else if tpl.Race == 52 {
		mon.BodyLeathery = 150
		if w.rand.Intn(30) == 0 {
			mon.FleeOnSight = true
			mon.ViewRange = 5
			mon.MeatQuality = 10000 + w.rand.Intn(20000)
		} else {
			mon.MeatQuality = 8000 + w.rand.Intn(8000)
		}
	} else if tpl.Race == 53 {
		mon.BodyLeathery = 150
		mon.MeatQuality = 8000 + w.rand.Intn(8000)
	}
	if tpl.Race == 96 && resetZilkinKillCount {
		mon.ZilkinKillCount = 0
		if w.rand.Intn(3) == 0 {
			mon.ZilkinKillCount = w.rand.Intn(3) + 1
		}
	}
}

func applyWhiteSkeletonTiming(mon *Monster) {
	mon.AttackIntervalMS = 3000 - int(mon.SlaveMakeLevel)*600
	mon.WalkSpeedMS = 1200 - int(mon.SlaveMakeLevel)*250
}

func initialMonsterMeatQuality(w *World, tpl data.StdMonster, id string) int {
	switch tpl.Race {
	case 51:
		return 3000 + w.rand.Intn(3500)
	}
	return 0
}

func boostSummonedMonsterLocked(mon *Monster) {
	if mon == nil || mon.MaxHP <= 0 || mon.HP >= mon.MaxHP {
		return
	}
	mon.HP += (mon.MaxHP - mon.HP) / 2
}

func applyMonsterTemplateState(mon *Monster, tpl data.StdMonster) {
	if tpl.Animal || tpl.Race == 51 || tpl.Race == 52 || tpl.Race == 53 || tpl.Race == 82 || tpl.Race == 84 || tpl.Race == 85 || tpl.Race == 90 || tpl.Race == 105 || tpl.Race == 106 {
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
	if tpl.Race == 95 {
		mon.Hidden = true
		mon.FixedHideMode = true
	}
	if tpl.Race == 100 {
		mon.Hidden = true
		mon.FixedHideMode = true
		mon.FirstRevealPending = true
	}
	if tpl.Race == 107 || tpl.Behavior == "centipede_king" {
		mon.Hidden = true
		mon.FixedHideMode = true
	}
	if tpl.StoneMode {
		mon.StoneMode = true
	}
	if tpl.Race == 101 || tpl.Race == 102 {
		mon.StoneMode = true
	}
	if tpl.FirstRevealPending {
		mon.FirstRevealPending = true
	}
	if tpl.AttackMax > 0 {
		mon.AttackMax = tpl.AttackMax
	} else {
		switch tpl.Race {
		case 87:
			mon.AttackMax = 2
		case 93:
			mon.AttackMax = 3
		case 104:
			mon.AttackMax = 6
		}
	}
	switch {
	case tpl.Race == 107 || tpl.Behavior == "centipede_king":
		mon.Dir = 5
	case tpl.Race == 102:
		mon.Dir = 5
	}
	switch tpl.Race {
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
		w.registerMonsterLocked(mon, nil)
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
		w.registerMonsterLocked(mon, nil)
		w.occupyMonsterLocked(mon)
		result.Monsters = append(result.Monsters, *mon)
	}
	if len(result.Monsters) == 0 {
		return SpawnResult{}, fmt.Errorf("no available spawn position for monster %s", name)
	}
	return result, nil
}

func (w *World) summonMonsterNearCharacterLocked(master storage.Character, players []storage.Character, name string, duration time.Duration, slaveLevel, slaveExpLevel byte) (*Monster, error) {
	mp, ok := w.data.Maps[master.MapID]
	if !ok {
		return nil, fmt.Errorf("map %s not found", master.MapID)
	}
	playerMap := make(map[string]storage.Character, len(players))
	for _, ch := range players {
		if ch.ID == "" {
			continue
		}
		playerMap[ch.ID] = ch
	}
	tpl, ok := w.monsterTemplateByIDLocked(name)
	if !ok {
		return nil, fmt.Errorf("monster %s not found", name)
	}
	now := time.Now()
	dir := master.Dir
	if dir < 0 || dir >= len(dirOffsets) {
		dir = 0
	}
	off := dirOffsets[dir]
	x, y := master.X+off[0], master.Y+off[1]
	if !mp.Walkable(x, y) || w.movingObjectAtLocked(playerMap, master.MapID, x, y, "") {
		return nil, fmt.Errorf("no available spawn position for monster %s", name)
	}
	id := fmt.Sprintf("mon-%d", w.nextID)
	w.nextID++
	spawn := data.StdSpawn{
		MapID:          master.MapID,
		MonsterID:      tpl.ID,
		X:              x,
		Y:              y,
		Count:          1,
		RespawnSeconds: 0,
	}
	mon := newMonster(w, id, tpl, master.MapID, x, y, spawn)
	mon.Hidden = false
	mon.FixedHideMode = false
	mon.MasterID = master.ID
	mon.MasterName = master.Name
	mon.SlaveMakeLevel = slaveLevel
	w.applySummonedMonsterExpLevelLocked(mon, slaveExpLevel)
	if mon.Race == 100 {
		applyWhiteSkeletonTiming(mon)
	}
	if duration > 0 {
		mon.MasterExpiresAt = now.Add(duration)
	}
	boostSummonedMonsterLocked(mon)
	w.registerMonsterLocked(mon, nil)
	w.occupyMonsterLocked(mon)
	return mon, nil
}

func (w *World) applySummonedMonsterExpLevelLocked(mon *Monster, level byte) {
	mon.SlaveExpLevel = level
	if level < byte(len(w.gameplay.Monster.SlaveColors)) {
		mon.SlaveNameColor = w.gameplay.Monster.SlaveColors[level]
		mon.SlaveColorSet = true
	}
	if level == 0 {
		return
	}
	if mon.Race == 100 || mon.Race == 113 || mon.Race == 114 {
		bonus := referenceRound((float64(level)*0.1 + 0.3) * 3 * float64(level))
		mon.MaxHP = mon.BaseMaxHP + referenceRound((float64(level)*0.1+0.3)*float64(mon.BaseMaxHP))*int(level)
		mon.MaxAttack = mon.BaseMaxAttack + bonus
		return
	}
	maxHP := mon.BaseMaxHP + referenceRound(float64(mon.BaseMaxHP)*0.15)*int(level)
	limit := mon.BaseMaxHP + int(level)*60
	if maxHP > limit {
		maxHP = limit
	}
	mon.MaxHP = maxHP
	mon.MaxAttack = mon.BaseMaxAttack + int(level)*2
}
