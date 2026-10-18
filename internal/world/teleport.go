package world

import (
	"fmt"
	"math/rand"
	"time"

	"openmir2/internal/data"
	"openmir2/internal/storage"
	"openmir2/internal/world/core"
)

func (w *World) Teleport(ch storage.Character, mapID string, x, y int) (storage.Character, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.teleportLocked(ch, mapID, x, y)
}

func (w *World) TeleportRandomInMap(ch storage.Character, mapID string) (storage.Character, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.teleportRandomInMapLocked(ch, mapID)
}

func (w *World) TeleportRandomInCurrentMap(ch storage.Character) (storage.Character, error) {
	return w.TeleportRandomInMap(ch, ch.MapID)
}

func (w *World) PrepareTeleport(ch storage.Character, mapID string, x, y int) (storage.Character, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.prepareTeleportLocked(ch, mapID, x, y)
}

func (w *World) PrepareRandomTeleportInMap(ch storage.Character, mapID string) (storage.Character, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.prepareRandomTeleportInMapLocked(ch, mapID)
}

func (w *World) teleportLocked(ch storage.Character, mapID string, x, y int) (storage.Character, error) {
	next, err := w.prepareTeleportLocked(ch, mapID, x, y)
	if err != nil {
		return ch, err
	}
	return next, nil
}

func (w *World) prepareTeleportLocked(ch storage.Character, mapID string, x, y int) (storage.Character, error) {
	mp, ok := w.data.Maps[mapID]
	if !ok {
		return ch, fmt.Errorf("map %s not found", mapID)
	}
	next, err := spaceMoveToMap(ch, mp, x, y, w.rand)
	if err != nil {
		return ch, err
	}
	w.refreshCharacterObjectOrderLocked(&next)
	next.MapMoveAt = time.Now().UnixNano()
	return next, nil
}

func (w *World) teleportRandomInMapLocked(ch storage.Character, mapID string) (storage.Character, error) {
	next, err := w.prepareRandomTeleportInMapLocked(ch, mapID)
	if err != nil {
		return ch, err
	}
	return next, nil
}

func (w *World) prepareRandomTeleportInMapLocked(ch storage.Character, mapID string) (storage.Character, error) {
	mp, ok := w.data.Maps[mapID]
	if !ok {
		return ch, fmt.Errorf("map %s not found", mapID)
	}
	next, err := randomMoveToMap(ch, mp, w.rand)
	if err != nil {
		return ch, err
	}
	w.refreshCharacterObjectOrderLocked(&next)
	next.MapMoveAt = time.Now().UnixNano()
	return next, nil
}

func (w *World) itemWeight(itemID string) int {
	item, ok := w.data.Items[itemID]
	if !ok {
		return 0
	}
	return item.Weight
}

func (w *World) homeTeleportCharacterLocked(ch storage.Character) (storage.Character, error) {
	mapID, x, y := w.homeDestinationLocked(ch)
	mp, ok := w.data.Maps[mapID]
	if !ok {
		return ch, fmt.Errorf("map %s not found", mapID)
	}
	next, err := spaceMoveToMap(ch, mp, x, y, w.rand)
	if err != nil {
		return ch, err
	}
	w.refreshCharacterObjectOrderLocked(&next)
	next.MapMoveAt = time.Now().UnixNano()
	return next, nil
}

func (w *World) homeTeleportRandomCharacterLocked(ch storage.Character) (storage.Character, error) {
	mapID := ch.HomeMap
	if mapID == "" {
		mapID = ch.MapID
	}
	if mapID == "" {
		return ch, fmt.Errorf("character has no home map")
	}
	mp, ok := w.data.Maps[mapID]
	if !ok {
		return ch, fmt.Errorf("map %s not found", mapID)
	}
	next, err := randomMoveToHomeMap(ch, mp, w.rand)
	if err != nil {
		return ch, err
	}
	w.refreshCharacterObjectOrderLocked(&next)
	next.MapMoveAt = time.Now().UnixNano()
	return next, nil
}

func (w *World) homeTeleportRandomHomeMapLocked(ch storage.Character) (storage.Character, error) {
	mapID := ch.HomeMap
	if mapID == "" {
		mapID = ch.MapID
	}
	if mapID == "" {
		return ch, fmt.Errorf("character has no home map")
	}
	mp, ok := w.data.Maps[mapID]
	if !ok {
		return ch, fmt.Errorf("map %s not found", mapID)
	}
	next, err := randomMoveToHomeMap(ch, mp, w.rand)
	if err != nil {
		return ch, err
	}
	w.refreshCharacterObjectOrderLocked(&next)
	next.MapMoveAt = time.Now().UnixNano()
	return next, nil
}

func (w *World) homeDestinationLocked(ch storage.Character) (string, int, int) {
	if ch.PKPoint/100 >= 2 && w.gameplay.Combat.RedHomeMap != "" {
		return w.gameplay.Combat.RedHomeMap, w.gameplay.Combat.RedHomeX, w.gameplay.Combat.RedHomeY
	}
	mapID := ch.HomeMap
	if mapID == "" {
		mapID = ch.MapID
	}
	return mapID, ch.HomeX, ch.HomeY
}

func (w *World) loginReviveDestinationLocked(ch storage.Character) (string, int, int, error) {
	mapID := ch.HomeMap
	x := ch.HomeX - 2 + w.rand.Intn(5)
	y := ch.HomeY - 2 + w.rand.Intn(5)
	if ch.PKPoint/100 >= 2 {
		mapID = w.gameplay.Combat.RedHomeMap
		x = w.gameplay.Combat.RedHomeX + w.rand.Intn(13)
		y = w.gameplay.Combat.RedHomeY + w.rand.Intn(13)
	}
	if w.characterInCastleWarAreaLocked(ch) && ch.PKPoint/100 < 2 {
		if castle, ok := w.castleSnapshotLocked(w.data.Castle.ID); ok && castle.OwnerGuildID != "" && castle.OwnerGuildID == ch.GuildID {
			mapID = w.data.Castle.HomeMap
			x = w.data.Castle.HomeX
			y = w.data.Castle.HomeY
		}
	}
	mp, ok := w.data.Maps[mapID]
	if !ok {
		return "", 0, 0, fmt.Errorf("map %s not found", mapID)
	}
	for attempt := 0; attempt <= 5; attempt++ {
		if mp.Walkable(x, y) && w.castleMapWalkableLocked(mapID, x, y) {
			return mapID, x, y, nil
		}
		if attempt == 5 {
			break
		}
		x = x - 3 + w.rand.Intn(6)
		y = y - 3 + w.rand.Intn(6)
	}
	fallbackMap := ch.HomeMap
	if _, ok := w.data.Maps[fallbackMap]; !ok {
		return "", 0, 0, fmt.Errorf("map %s not found", fallbackMap)
	}
	return fallbackMap, ch.HomeX, ch.HomeY, nil
}

func randomMoveToHomeMap(ch storage.Character, mp data.StdMap, rng *rand.Rand) (storage.Character, error) {
	return randomMoveToMap(ch, mp, rng)
}

func randomMoveToMap(ch storage.Character, mp data.StdMap, rng *rand.Rand) (storage.Character, error) {
	if rng == nil {
		return ch, fmt.Errorf("teleport rng is nil")
	}
	if mp.Width <= 0 || mp.Height <= 0 {
		return ch, fmt.Errorf("map %s is invalid", mp.ID)
	}
	edge := 50
	if mp.Height < 150 {
		edge = 20
		if mp.Height < 30 {
			edge = 2
		}
	}
	if mp.Width-edge-1 <= 0 || mp.Height-edge-1 <= 0 {
		return core.TeleportRandomInMap(ch, mp, rng)
	}
	x := rng.Intn(mp.Width-edge-1) + edge
	y := rng.Intn(mp.Height-edge-1) + edge
	return spaceMoveToMap(ch, mp, x, y, rng)
}

func spaceMoveToMap(ch storage.Character, mp data.StdMap, x, y int, rng *rand.Rand) (storage.Character, error) {
	if rng == nil {
		return ch, fmt.Errorf("teleport rng is nil")
	}
	if mp.Width <= 0 || mp.Height <= 0 {
		return ch, fmt.Errorf("map %s is invalid", mp.ID)
	}
	stepX := 10
	if mp.Width < 80 {
		stepX = 3
	}
	wallY := 50
	if mp.Height < 150 {
		wallY = 15
		if mp.Height < 50 {
			wallY = 2
		}
	}
	for attempt := 0; attempt < 201; attempt++ {
		if mp.Walkable(x, y) {
			ch.MapID = mp.ID
			ch.X = x
			ch.Y = y
			return ch, nil
		}
		if x < mp.Width-wallY-1 {
			x += stepX
		} else {
			x = rng.Intn(mp.Width)
			if y < mp.Height-wallY-1 {
				y += stepX
			} else {
				y = rng.Intn(mp.Height)
			}
		}
	}
	return ch, fmt.Errorf("no available teleport position")
}

func (w *World) ReviveCharacterAtHome(ch storage.Character) (storage.Character, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	next, err := w.homeTeleportCharacterLocked(ch)
	if err != nil {
		return ch, err
	}
	if next.MaxHP > 0 || next.MaxMP > 0 {
		next = core.SetVitals(next, next.MaxHP, next.MaxMP).Character
	}
	return next, nil
}

func (w *World) ReviveCharacterForLogin(ch storage.Character) (storage.Character, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if mp, ok := w.data.Maps[ch.MapID]; ok && mp.Fight3Zone && ch.FightZoneDieCount < 3 {
		ch = core.SetVitals(ch, ch.MaxHP, ch.MaxMP).Character
		ch.DieInFight3Zone = true
		return ch, nil
	}
	if mp, ok := w.data.Maps[ch.MapID]; ok && mp.Fight3Zone {
		ch.FightZoneDieCount = 0
	}
	mapID, x, y, err := w.loginReviveDestinationLocked(ch)
	if err != nil {
		return ch, err
	}
	next := ch
	next.MapID = mapID
	next.X = x
	next.Y = y
	w.refreshCharacterObjectOrderLocked(&next)
	next.MapMoveAt = time.Now().UnixNano()
	next.ParalyzedUntil = 0
	next.DefenceUpUntil = 0
	next.MagDefenceUpUntil = 0
	next.BubbleDefenceLevel = 0
	next.BubbleDefenceUntil = 0
	next.PoisonHealthLevel = 0
	next.PoisonHealthPoint = 0
	next.PoisonHealthStartAt = 0
	next.PoisonHealthUntil = 0
	next.PoisonHealthTickAt = 0
	next.PoisonArmorLevel = 0
	next.PoisonArmorStartAt = 0
	next.PoisonArmorUntil = 0
	next.TransparentUntil = 0
	next.ShowHPOpenAt = 0
	next.ShowHPDuration = 0
	next.ShowHPUntil = 0
	bubbleDefenceActive := false
	transparentHideMode := false
	next.BubbleDefenceActive = &bubbleDefenceActive
	next.TransparentHideMode = &transparentHideMode
	next.HP = 14
	return next, nil
}

func (w *World) RandomMoveAfterFight3Revive(ch storage.Character) (storage.Character, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	mp, ok := w.data.Maps[ch.MapID]
	if !ok {
		return ch, fmt.Errorf("map %s not found", ch.MapID)
	}
	next, err := randomMoveToMap(ch, mp, w.rand)
	if err != nil {
		return ch, err
	}
	w.refreshCharacterObjectOrderLocked(&next)
	next.MapMoveAt = time.Now().UnixNano()
	next.DieInFight3Zone = false
	return next, nil
}

func (w *World) RandomMoveAfterCrowdedLogin(ch storage.Character, players []storage.Character) (storage.Character, bool, error) {
	if ch.Level > 7 {
		return ch, false, nil
	}
	count := 0
	for _, player := range players {
		if player.Ghost || player.MapID != ch.MapID || abs(player.X-ch.X) >= 10 || abs(player.Y-ch.Y) >= 10 {
			continue
		}
		count++
	}
	if count < 80 {
		return ch, false, nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	mp, ok := w.data.Maps[ch.MapID]
	if !ok {
		return ch, false, fmt.Errorf("map %s not found", ch.MapID)
	}
	next, err := randomMoveToMap(ch, mp, w.rand)
	if err != nil {
		return ch, false, err
	}
	w.refreshCharacterObjectOrderLocked(&next)
	next.MapMoveAt = time.Now().UnixNano()
	return next, true, nil
}
