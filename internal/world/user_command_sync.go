package world

import "openmir2/internal/storage"

type UserCommandSyncer interface {
	TeleportSyncer
	SendBagAddItem(storage.Character, storage.UserItem)
	SendWeightChanged(storage.Character)
}

func ApplyUserCommandSync(syncer UserCommandSyncer, result UserCommandResult) {
	if result.Character.ID != "" {
		syncer.UpdateClient(result.Character)
	}
	if result.Teleport != nil {
		if result.Teleport.SpaceMoveFire {
			if ringSyncer, ok := syncer.(RingTeleportSyncer); ok {
				ringSyncer.SendTeleportRingMove(result.Teleport.From, result.Teleport.To)
			} else {
				ApplyTeleportSync(syncer, *result.Teleport)
			}
		} else {
			ApplyTeleportSync(syncer, *result.Teleport)
		}
	}
	for _, added := range result.AddedItems {
		syncer.SendBagAddItem(result.Character, added)
	}
	if len(result.AddedItems) > 0 {
		syncer.SendWeightChanged(result.Character)
	}
}
