package world

import "openmir2/internal/storage"

type GroupSyncer interface {
	UpdateClient(storage.Character)
	SendGroupCancel(storage.Character)
	SendGroupMembers(string)
}

type GroupSyncEvent struct {
	Updated           []storage.Character
	Cancel            []storage.Character
	MemberListOwnerID string
}

func ApplyGroupSync(syncer GroupSyncer, event GroupSyncEvent) {
	for _, ch := range event.Updated {
		syncer.UpdateClient(ch)
	}
	for _, ch := range event.Cancel {
		syncer.SendGroupCancel(ch)
	}
	if event.MemberListOwnerID != "" {
		syncer.SendGroupMembers(event.MemberListOwnerID)
	}
}
