package world

import "openmir2/internal/storage"

type GroupSyncer interface {
	UpdateClient(storage.Character)
	SendGroupMessage(string, string, string)
	SendGroupCancel(storage.Character)
	SendGroupMembers(string)
}

type GroupMessage struct {
	RecipientID string
	ActorID     string
	Text        string
}

type GroupSyncEvent struct {
	Updated             []storage.Character
	Messages            []GroupMessage
	MessagesAfterCancel []GroupMessage
	Cancel              []storage.Character
	CancelAfterMessages []storage.Character
	MemberListOwnerID   string
}

func ApplyGroupMessages(syncer GroupSyncer, messages []GroupMessage) {
	for _, message := range messages {
		syncer.SendGroupMessage(message.RecipientID, message.ActorID, message.Text)
	}
}

func ApplyGroupSync(syncer GroupSyncer, event GroupSyncEvent) {
	for _, ch := range event.Updated {
		syncer.UpdateClient(ch)
	}
	for _, ch := range event.Cancel {
		syncer.SendGroupCancel(ch)
	}
	for _, message := range event.MessagesAfterCancel {
		syncer.SendGroupMessage(message.RecipientID, message.ActorID, message.Text)
	}
	for _, ch := range event.CancelAfterMessages {
		syncer.SendGroupCancel(ch)
	}
	if event.MemberListOwnerID != "" {
		syncer.SendGroupMembers(event.MemberListOwnerID)
	}
}
