package world

import "openmir2/internal/storage"

type TeleportEvent struct {
	From storage.Character
	To   storage.Character
}

func newTeleportEvent(from, to storage.Character) *TeleportEvent {
	return &TeleportEvent{From: from, To: to}
}
