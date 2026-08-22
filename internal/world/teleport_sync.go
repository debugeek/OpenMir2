package world

import "openmir2/internal/storage"

type TeleportSyncer interface {
	UpdateClient(storage.Character)
	SendSpaceMoveState(storage.Character)
	BroadcastTeleportMove(storage.Character, storage.Character)
}

func ApplyTeleportSync(syncer TeleportSyncer, event TeleportEvent) {
	syncer.UpdateClient(event.To)
	syncer.SendSpaceMoveState(event.To)
	syncer.BroadcastTeleportMove(event.From, event.To)
}
