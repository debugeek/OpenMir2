package core

import (
	"fmt"
	"math/rand"

	"openmir2/internal/data"
	"openmir2/internal/storage"
)

func TeleportTo(ch storage.Character, mp data.StdMap, x, y int, rng *rand.Rand) (storage.Character, error) {
	if rng == nil {
		return ch, fmt.Errorf("teleport rng is nil")
	}
	if mp.Width <= 0 || mp.Height <= 0 {
		return ch, fmt.Errorf("map %s is invalid", mp.ID)
	}
	if mp.Walkable(x, y) {
		ch.MapID = mp.ID
		ch.X = x
		ch.Y = y
		return ch, nil
	}
	px, py, ok := findRandomWalkableNear(rng, mp, mp.SpawnX, mp.SpawnY, 4)
	if !ok {
		return ch, fmt.Errorf("target coordinate is blocked")
	}
	ch.MapID = mp.ID
	ch.X = px
	ch.Y = py
	return ch, nil
}

func TeleportRandomInMap(ch storage.Character, mp data.StdMap, rng *rand.Rand) (storage.Character, error) {
	if rng == nil {
		return ch, fmt.Errorf("teleport rng is nil")
	}
	if mp.Width <= 0 || mp.Height <= 0 {
		return ch, fmt.Errorf("map %s is invalid", mp.ID)
	}
	positions := make([][2]int, 0, mp.Width*mp.Height)
	for y := 0; y < mp.Height; y++ {
		for x := 0; x < mp.Width; x++ {
			if mp.Walkable(x, y) {
				positions = append(positions, [2]int{x, y})
			}
		}
	}
	if len(positions) == 0 {
		return ch, fmt.Errorf("no available teleport position")
	}
	pick := positions[rng.Intn(len(positions))]
	if len(positions) > 1 && pick[0] == ch.X && pick[1] == ch.Y {
		pick = positions[(rng.Intn(len(positions)-1)+1)%len(positions)]
	}
	ch.MapID = mp.ID
	ch.X = pick[0]
	ch.Y = pick[1]
	return ch, nil
}

func findRandomWalkableNear(rng *rand.Rand, mp data.StdMap, x, y, searchRadius int) (int, int, bool) {
	if searchRadius < 0 {
		searchRadius = 0
	}
	for attempt := 0; attempt < 31; attempt++ {
		xx, yy := x, y
		if searchRadius > 0 {
			xx = x - searchRadius + rng.Intn(searchRadius*2+1)
			yy = y - searchRadius + rng.Intn(searchRadius*2+1)
		}
		if mp.Walkable(xx, yy) {
			return xx, yy, true
		}
	}
	return 0, 0, false
}
