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
	return ch, fmt.Errorf("target coordinate is blocked")
}

func TeleportRandomInMap(ch storage.Character, mp data.StdMap, rng *rand.Rand) (storage.Character, error) {
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
		ch.MapID, ch.X, ch.Y = mp.ID, pick[0], pick[1]
		return ch, nil
	}
	x := rng.Intn(mp.Width-edge-1) + edge
	y := rng.Intn(mp.Height-edge-1) + edge
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
