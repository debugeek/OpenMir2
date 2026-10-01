package world

import "openmir2/internal/storage"

type PickupSyncer interface {
	UpdateClient(storage.Character)
	BroadcastDropHide(storage.Character, string)
	SendGoldChanged(storage.Character, int)
	SendBagAddItem(storage.Character, storage.UserItem)
	SendWeightChanged(storage.Character)
}

func ApplyPickupSync(syncer PickupSyncer, result PickupResult) {
	syncer.UpdateClient(result.Character)
	if result.GoldChanged {
		syncer.BroadcastDropHide(result.Character, result.Drop.ID)
		syncer.SendGoldChanged(result.Character, result.Gold)
		return
	}
	syncer.BroadcastDropHide(result.Character, result.Drop.ID)
	syncer.SendWeightChanged(result.Character)
	for _, added := range result.AddedItems {
		syncer.SendBagAddItem(result.Character, added)
	}
}
