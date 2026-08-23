package core

import (
	"math/rand"
	"testing"

	"openmir2/internal/data"
	"openmir2/internal/storage"
)

func TestTeleportToWalkableTile(t *testing.T) {
	ch := storage.Character{ID: "char-1", MapID: "old-map", X: 1, Y: 1}
	mp := data.StdMap{ID: "map-1", Width: 4, Height: 4}

	updated, err := TeleportTo(ch, mp, 2, 2, rand.New(rand.NewSource(1)))
	if err != nil {
		t.Fatalf("TeleportTo() error = %v", err)
	}
	if updated.MapID != mp.ID || updated.X != 2 || updated.Y != 2 {
		t.Fatalf("teleport = %s (%d,%d), want %s (2,2)", updated.MapID, updated.X, updated.Y, mp.ID)
	}
}

func TestTeleportToBlockedTileFails(t *testing.T) {
	ch := storage.Character{ID: "char-1", MapID: "old-map", X: 1, Y: 1}
	mp := data.StdMap{ID: "map-1", Width: 4, Height: 4, Blocked: []data.StdPoint{{X: 3, Y: 3}}}

	updated, err := TeleportTo(ch, mp, 99, 99, rand.New(rand.NewSource(2)))
	if err == nil {
		t.Fatalf("TeleportTo() error = nil, want blocked target error")
	}
	if updated.MapID != ch.MapID || updated.X != ch.X || updated.Y != ch.Y {
		t.Fatalf("teleport = %+v, want unchanged character", updated)
	}
}

func TestTeleportRandomInMapMovesCharacter(t *testing.T) {
	ch := storage.Character{ID: "char-1", MapID: "old-map", X: 0, Y: 0}
	mp := data.StdMap{ID: "map-1", Width: 2, Height: 2}

	updated, err := TeleportRandomInMap(ch, mp, rand.New(rand.NewSource(3)))
	if err != nil {
		t.Fatalf("TeleportRandomInMap() error = %v", err)
	}
	if updated.MapID != mp.ID {
		t.Fatalf("MapID = %q, want %q", updated.MapID, mp.ID)
	}
	if updated.X == ch.X && updated.Y == ch.Y {
		t.Fatalf("teleport did not move character")
	}
	if !mp.Walkable(updated.X, updated.Y) {
		t.Fatalf("teleport landed on blocked tile (%d,%d)", updated.X, updated.Y)
	}
}
