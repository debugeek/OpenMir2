package world

import (
	"fmt"
	"time"

	"openmir2/internal/storage"
)

// dirOffsets maps a facing direction (0-7, clockwise from north) to its tile delta.
var dirOffsets = [8][2]int{
	{0, -1}, {1, -1}, {1, 0}, {1, 1}, {0, 1}, {-1, 1}, {-1, 0}, {-1, -1},
}

func (w *World) Move(ch storage.Character, x, y int) (storage.Character, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.stepLocked(ch, x, y, 1)
}

func (w *World) Turn(ch storage.Character, x, y, dir int) (storage.Character, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := validDir(dir); err != nil {
		return ch, err
	}
	if x != ch.X || y != ch.Y {
		return ch, fmt.Errorf("turn coordinates do not match current position")
	}
	ch.Dir = dir
	return ch, w.store.SaveCharacter(ch)
}

func (w *World) Walk(ch storage.Character, x, y, dir int) (storage.Character, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := validDir(dir); err != nil {
		return ch, err
	}
	ch.Dir = dir
	return w.directionalStepLocked(ch, x, y, dir, 1)
}

func (w *World) Run(ch storage.Character, x, y, dir int) (storage.Character, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := validDir(dir); err != nil {
		return ch, err
	}
	ch.Dir = dir
	return w.directionalStepLocked(ch, x, y, dir, 2)
}

func (w *World) SitDown(ch storage.Character, x, y, dir int) (storage.Character, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := validDir(dir); err != nil {
		return ch, err
	}
	if x != ch.X || y != ch.Y {
		return ch, fmt.Errorf("sitdown coordinates do not match current position")
	}
	ch.Dir = dir
	ch.Sitting = !ch.Sitting
	return ch, w.store.SaveCharacter(ch)
}

// Hit resolves a melee swing (CM_HIT and its variants) in the character's
// facing direction, attacking whatever living monster occupies that tile.
// A swing that connects with nothing is not an error, it just carries no
// AttackResult.MonsterID.
func (w *World) Hit(ch storage.Character, x, y, dir int, blockers ...storage.Character) (AttackResult, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := validDir(dir); err != nil {
		return AttackResult{}, err
	}
	if x != ch.X || y != ch.Y {
		return AttackResult{}, fmt.Errorf("hit coordinates do not match current position")
	}
	ch.Dir = dir
	w.respawnLocked(time.Now())
	off := dirOffsets[dir]
	targetX, targetY := ch.X+off[0], ch.Y+off[1]
	for _, mon := range w.monsters {
		if mon.Alive && mon.MapID == ch.MapID && mon.X == targetX && mon.Y == targetY {
			return w.attackLocked(ch, mon, blockers...)
		}
	}
	return AttackResult{Character: ch}, w.store.SaveCharacter(ch)
}

func (w *World) stepLocked(ch storage.Character, x, y, maxDist int) (storage.Character, error) {
	mp, ok := w.data.Maps[ch.MapID]
	if !ok {
		return ch, fmt.Errorf("map %s not found", ch.MapID)
	}
	if !mp.Walkable(x, y) {
		return ch, fmt.Errorf("target coordinate is blocked")
	}
	if abs(ch.X-x) > maxDist || abs(ch.Y-y) > maxDist {
		return ch, fmt.Errorf("move too far")
	}
	ch.X = x
	ch.Y = y
	w.syncCharacterHomeFromStartPointLocked(&ch)
	return ch, w.store.SaveCharacter(ch)
}

// directionalStepLocked resolves CM_WALK/CM_RUN the way the reference
// server's WalkTo/RunTo do (BaseObject.cs, PlayObject.Base.cs): the
// destination tile is derived from the character's current position plus the
// direction offset repeated `steps` times (1 for walk, 2 for run), not taken
// from the client's reported (x, y) directly. Every tile along the way must
// be walkable, matching RunTo's CanWalkEx checks on both the +1 and +2 tiles
// — a distance-only bound would let a run "jump" a one-tile-wide obstacle by
// only checking the final tile. The client's (x, y) must match the derived
// destination exactly, which also rejects diagonal-skewed moves (e.g. a run
// claiming dx=2, dy=1) that no direction actually produces.
func (w *World) directionalStepLocked(ch storage.Character, x, y, dir, steps int) (storage.Character, error) {
	mp, ok := w.data.Maps[ch.MapID]
	if !ok {
		return ch, fmt.Errorf("map %s not found", ch.MapID)
	}
	off := dirOffsets[dir]
	destX, destY := ch.X, ch.Y
	for i := 1; i <= steps; i++ {
		destX, destY = ch.X+off[0]*i, ch.Y+off[1]*i
		if !mp.Walkable(destX, destY) {
			return ch, fmt.Errorf("move is blocked")
		}
	}
	if x != destX || y != destY {
		return ch, fmt.Errorf("move coordinates do not match direction")
	}
	ch.X = destX
	ch.Y = destY
	w.syncCharacterHomeFromStartPointLocked(&ch)
	return ch, w.store.SaveCharacter(ch)
}

func validDir(dir int) error {
	if dir < 0 || dir > 7 {
		return fmt.Errorf("invalid direction %d", dir)
	}
	return nil
}
