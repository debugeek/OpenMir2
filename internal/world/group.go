package world

import (
	"fmt"

	"openmir2/internal/storage"
)

func (w *World) hasRecallSuiteLocked(ch storage.Character) bool {
	if w.data.Items == nil || ch.EquippedItems == nil {
		return false
	}
	hasShape := func(slot int, shape int) bool {
		entry, ok := w.activeEquippedItemLocked(ch, slot)
		if !ok {
			return false
		}
		item, ok := w.data.Items[entry.ItemID]
		return ok && item.Shape == shape
	}
	return hasShape(SlotNecklace, 123) &&
		(hasShape(SlotRingL, 122) || hasShape(SlotRingR, 122)) &&
		(hasShape(SlotArmRingL, 124) || hasShape(SlotArmRingR, 124)) &&
		hasShape(SlotHelmet, 125)
}

func (w *World) HasRecallSuite(ch storage.Character) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.hasRecallSuiteLocked(ch)
}

func (w *World) hasSpiritSuiteLocked(ch storage.Character) bool {
	if w.data.Items == nil || ch.EquippedItems == nil {
		return false
	}
	hasShape := func(slot int, shape int) bool {
		entry, ok := w.activeEquippedItemLocked(ch, slot)
		if !ok {
			return false
		}
		item, ok := w.data.Items[entry.ItemID]
		return ok && item.Shape == shape
	}
	return hasShape(SlotNecklace, 127) &&
		(hasShape(SlotRingL, 128) || hasShape(SlotRingR, 128)) &&
		(hasShape(SlotArmRingL, 126) || hasShape(SlotArmRingR, 126)) &&
		hasShape(SlotHelmet, 129)
}

func (w *World) SetGroupMode(ch storage.Character, allow bool) (storage.Character, []storage.Character, error) {
	updated, result, err := w.SetGroupModeWithResult(ch, allow)
	return updated, result.Sync.Updated, err
}

type GroupModeResult struct {
	Character     storage.Character
	Sync          GroupSyncEvent
	ResponseParam uint16
}

func (w *World) SetGroupModeWithResult(ch storage.Character, allow bool) (storage.Character, GroupModeResult, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !allow {
		if ch.GroupOwnerID == "" {
			ch.AllowGroup = false
			return ch, GroupModeResult{Character: ch, Sync: GroupSyncEvent{Updated: []storage.Character{ch}}, ResponseParam: 0}, nil
		}
		if ch.GroupOwnerID != ch.ID {
			owner, _ := w.store.Character(ch.GroupOwnerID)
			changed, err := w.removeGroupMemberLocked(ch.GroupOwnerID, ch.ID, true)
			if err != nil {
				return ch, GroupModeResult{}, err
			}
			event := GroupSyncEvent{Updated: append([]storage.Character(nil), changed...), Cancel: []storage.Character{ch}, Messages: groupExitMessages(owner, ch)}
			ownerID := ch.GroupOwnerID
			for _, entry := range changed {
				if entry.ID != ownerID {
					continue
				}
				if entry.GroupOwnerID == "" {
					event.CancelAfterMessages = append(event.CancelAfterMessages, entry)
					event.MessagesAfterCancel = append(event.MessagesAfterCancel, groupDisbandMessage(entry)...)
				} else {
					event.MemberListOwnerID = ownerID
				}
				break
			}
			for _, entry := range changed {
				if entry.ID == ch.ID {
					ch = entry
					break
				}
			}
			return ch, GroupModeResult{Character: ch, Sync: event, ResponseParam: 0}, nil
		}
		return ch, GroupModeResult{Character: ch, Sync: GroupSyncEvent{}, ResponseParam: 1}, nil
	}
	ch.AllowGroup = true
	return ch, GroupModeResult{Character: ch, Sync: GroupSyncEvent{Updated: []storage.Character{ch}}, ResponseParam: 1}, nil
}

func (w *World) CreateGroup(owner, target storage.Character, onlineMemberCount int) (storage.Character, storage.Character, error) {
	updatedOwner, updatedTarget, _, err := w.CreateGroupWithResult(owner, target, onlineMemberCount)
	return updatedOwner, updatedTarget, err
}

func (w *World) CreateGroupWithResult(owner, target storage.Character, _ int) (storage.Character, storage.Character, GroupSyncEvent, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if owner.GroupOwnerID != "" {
		return owner, target, GroupSyncEvent{}, fmt.Errorf("group already exists")
	}
	if target.ID == "" || target.ID == owner.ID || w.characterDeadLocked(target) || target.Ghost {
		return owner, target, GroupSyncEvent{}, fmt.Errorf("invalid group target")
	}
	if target.GroupOwnerID != "" {
		return owner, target, GroupSyncEvent{}, fmt.Errorf("target already in group")
	}
	if !target.AllowGroup {
		return owner, target, GroupSyncEvent{}, fmt.Errorf("target not allowing group")
	}
	owner.GroupOwnerID = owner.ID
	owner.AllowGroup = true
	owner.GroupMembers = []string{owner.ID, target.ID}
	target.GroupOwnerID = owner.ID
	if err := w.store.SetCharacterRuntimePair(owner, target); err != nil {
		return owner, target, GroupSyncEvent{}, err
	}
	return owner, target, GroupSyncEvent{Updated: []storage.Character{owner, target}, Messages: groupJoinMessagesForMembers(owner, target), MemberListOwnerID: owner.ID}, nil
}

func (w *World) AddGroupMember(owner, target storage.Character, onlineMemberCount int) (storage.Character, storage.Character, error) {
	updatedOwner, updatedTarget, _, err := w.AddGroupMemberWithResult(owner, target, onlineMemberCount)
	return updatedOwner, updatedTarget, err
}

func (w *World) AddGroupMemberWithResult(owner, target storage.Character, onlineMemberCount int) (storage.Character, storage.Character, GroupSyncEvent, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if owner.GroupOwnerID != owner.ID {
		return owner, target, GroupSyncEvent{}, fmt.Errorf("not group owner")
	}
	if onlineMemberCount > 10 {
		return owner, target, GroupSyncEvent{}, fmt.Errorf("group is full")
	}
	if target.ID == "" || target.ID == owner.ID || w.characterDeadLocked(target) || target.Ghost {
		return owner, target, GroupSyncEvent{}, fmt.Errorf("invalid group target")
	}
	if target.GroupOwnerID != "" {
		return owner, target, GroupSyncEvent{}, fmt.Errorf("target already in group")
	}
	if !target.AllowGroup {
		return owner, target, GroupSyncEvent{}, fmt.Errorf("target not allowing group")
	}
	owner.GroupMembers = append(owner.GroupMembers, target.ID)
	target.GroupOwnerID = owner.ID
	if err := w.store.SetCharacterRuntimePair(owner, target); err != nil {
		return owner, target, GroupSyncEvent{}, err
	}
	return owner, target, GroupSyncEvent{Updated: []storage.Character{owner, target}, Messages: groupJoinMessagesForActor(owner, target), MemberListOwnerID: owner.ID}, nil
}

func (w *World) DelGroupMember(owner, target storage.Character) (storage.Character, storage.Character, error) {
	updatedOwner, updatedTarget, _, err := w.DelGroupMemberWithResult(owner, target)
	return updatedOwner, updatedTarget, err
}

func (w *World) DelGroupMemberWithResult(owner, target storage.Character) (storage.Character, storage.Character, GroupSyncEvent, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if owner.GroupOwnerID != owner.ID {
		return owner, target, GroupSyncEvent{}, fmt.Errorf("not group owner")
	}
	members := append([]string(nil), owner.GroupMembers...)
	memberOfGroup := false
	for _, memberID := range owner.GroupMembers {
		if memberID == target.ID {
			memberOfGroup = true
			break
		}
	}
	if !memberOfGroup {
		return owner, target, GroupSyncEvent{}, fmt.Errorf("target not group member")
	}
	if target.ID == owner.ID {
		return w.disbandGroupByOwnerLocked(owner)
	}
	changed, err := w.removeGroupMemberLocked(owner.ID, target.ID, false)
	if err != nil {
		return owner, target, GroupSyncEvent{}, err
	}
	event := GroupSyncEvent{Messages: groupExitMessagesForIDs(members, target)}
	for _, entry := range changed {
		switch entry.ID {
		case owner.ID:
			owner = entry
		case target.ID:
			target = entry
		}
	}
	event.Updated = append(event.Updated, changed...)
	if target.GroupOwnerID == "" {
		event.Cancel = append(event.Cancel, target)
	}
	if owner.GroupOwnerID == "" && owner.ID != target.ID {
		event.CancelAfterMessages = append(event.CancelAfterMessages, owner)
		event.MessagesAfterCancel = append(event.MessagesAfterCancel, groupDisbandMessage(owner)...)
	}
	if owner.GroupOwnerID != "" {
		event.MemberListOwnerID = owner.GroupOwnerID
	}
	return owner, target, event, nil
}

func (w *World) disbandGroupByOwnerLocked(owner storage.Character) (storage.Character, storage.Character, GroupSyncEvent, error) {
	recipients := append([]string(nil), owner.GroupMembers...)
	changed := make([]storage.Character, 0, len(recipients))
	event := GroupSyncEvent{}
	for i := len(recipients) - 1; i >= 0; i-- {
		member, ok := w.store.Character(recipients[i])
		if !ok {
			if recipients[i] != owner.ID {
				continue
			}
			member = owner
		}
		event.Messages = append(event.Messages, groupExitMessagesForIDs(recipients, member)...)
		member.GroupOwnerID = ""
		member.GroupMembers = nil
		changed = append(changed, member)
		event.Cancel = append(event.Cancel, member)
	}
	if len(changed) == 0 {
		return owner, owner, GroupSyncEvent{}, nil
	}
	if err := w.store.SetCharacterRuntimeBatch(changed); err != nil {
		return owner, owner, GroupSyncEvent{}, err
	}
	event.Updated = append(event.Updated, changed...)
	for _, member := range changed {
		if member.ID == owner.ID {
			event.CancelAfterMessages = append(event.CancelAfterMessages, member)
			return member, member, event, nil
		}
	}
	return owner, owner, event, nil
}

func groupJoinMessages(owner, target storage.Character) []GroupMessage {
	messages := make([]GroupMessage, 0, len(owner.GroupMembers))
	for _, recipientID := range owner.GroupMembers {
		messages = append(messages, GroupMessage{RecipientID: recipientID, ActorID: target.ID, Text: target.Name + " 已加入小组"})
	}
	return messages
}

func groupJoinMessagesForActor(owner, actor storage.Character) []GroupMessage {
	recipients := append([]string(nil), owner.GroupMembers...)
	messages := make([]GroupMessage, 0, len(recipients))
	for _, recipientID := range recipients {
		messages = append(messages, GroupMessage{RecipientID: recipientID, ActorID: actor.ID, Text: actor.Name + " 已加入小组"})
	}
	return messages
}

func groupJoinMessagesForMembers(owner, target storage.Character) []GroupMessage {
	recipients := append([]string(nil), owner.GroupMembers...)
	messages := make([]GroupMessage, 0, len(recipients)*2)
	for _, actor := range []storage.Character{owner, target} {
		for _, recipientID := range recipients {
			messages = append(messages, GroupMessage{RecipientID: recipientID, ActorID: actor.ID, Text: actor.Name + " 已加入小组"})
		}
	}
	return messages
}

func groupExitMessages(owner, member storage.Character) []GroupMessage {
	if owner.ID == "" {
		return nil
	}
	return groupExitMessagesForIDs(owner.GroupMembers, member)
}

func groupExitMessagesForIDs(recipientIDs []string, member storage.Character) []GroupMessage {
	messages := make([]GroupMessage, 0, len(recipientIDs))
	for _, recipientID := range recipientIDs {
		messages = append(messages, GroupMessage{RecipientID: recipientID, ActorID: member.ID, Text: member.Name + " 已退出小组"})
	}
	return messages
}

func groupDisbandMessage(owner storage.Character) []GroupMessage {
	if owner.ID == "" {
		return nil
	}
	return []GroupMessage{{RecipientID: owner.ID, ActorID: owner.ID, Text: "你的小组已解散"}}
}

func (w *World) removeGroupMemberLocked(ownerID, memberID string, clearMemberAllow bool) ([]storage.Character, error) {
	if ownerID == "" || memberID == "" {
		return nil, nil
	}
	member, memberOK := w.store.Character(memberID)
	if memberOK {
		member.GroupOwnerID = ""
		if clearMemberAllow {
			member.AllowGroup = false
		}
	}
	owner, ownerOK := w.store.Character(ownerID)
	if !ownerOK {
		if !memberOK {
			return nil, nil
		}
		if err := w.store.SetCharacterRuntime(member); err != nil {
			return nil, err
		}
		return []storage.Character{member}, nil
	}
	owner.GroupMembers = removeString(owner.GroupMembers, memberID)
	if len(owner.GroupMembers) <= 1 {
		owner.GroupOwnerID = ""
		owner.GroupMembers = nil
	}
	if memberOK {
		if err := w.store.SetCharacterRuntimePair(member, owner); err != nil {
			return nil, err
		}
		return []storage.Character{member, owner}, nil
	}
	if err := w.store.SetCharacterRuntime(owner); err != nil {
		return nil, err
	}
	return []storage.Character{owner}, nil
}

func (w *World) settleDeadCharacterGroupLocked(ch *storage.Character, players map[string]storage.Character) (GroupSyncEvent, error) {
	if ch == nil || ch.GroupOwnerID == "" {
		return GroupSyncEvent{}, nil
	}
	if ch.GroupOwnerID == ch.ID {
		event := GroupSyncEvent{}
		recipients := append([]string(nil), ch.GroupMembers...)
		changed := make([]storage.Character, 0, len(ch.GroupMembers)+1)
		for i := len(ch.GroupMembers) - 1; i >= 0; i-- {
			memberID := ch.GroupMembers[i]
			actor := *ch
			if memberID != ch.ID {
				member, ok := players[memberID]
				if !ok {
					continue
				}
				actor = member
			}
			event.Messages = append(event.Messages, groupExitMessagesForIDs(recipients, actor)...)
			if memberID == ch.ID {
				continue
			}
			member, ok := players[memberID]
			if !ok {
				continue
			}
			member.GroupOwnerID = ""
			member.GroupMembers = nil
			changed = append(changed, member)
		}
		ch.GroupOwnerID = ""
		ch.GroupMembers = nil
		changed = append(changed, *ch)
		if err := w.store.SetCharacterRuntimeBatch(changed); err != nil {
			return GroupSyncEvent{}, err
		}
		for _, member := range changed[:len(changed)-1] {
			players[member.ID] = member
			event.Updated = append(event.Updated, member)
			event.Cancel = append(event.Cancel, member)
		}
		event.MessagesAfterCancel = append(event.MessagesAfterCancel, groupDisbandMessage(*ch)...)
		event.CancelAfterMessages = append(event.CancelAfterMessages, *ch)
		return event, nil
	}
	owner, ok := players[ch.GroupOwnerID]
	if !ok {
		return GroupSyncEvent{}, nil
	}
	oldMembers := append([]string(nil), owner.GroupMembers...)
	owner.GroupMembers = removeString(owner.GroupMembers, ch.ID)
	if len(owner.GroupMembers) <= 1 {
		owner.GroupOwnerID = ""
		owner.GroupMembers = nil
	}
	ch.GroupOwnerID = ""
	ch.GroupMembers = nil
	if err := w.store.SetCharacterRuntimeBatch([]storage.Character{owner, *ch}); err != nil {
		return GroupSyncEvent{}, err
	}
	players[owner.ID] = owner
	event := GroupSyncEvent{Updated: []storage.Character{owner}, Messages: groupExitMessagesForIDs(oldMembers, *ch)}
	if owner.GroupOwnerID != "" {
		event.MemberListOwnerID = owner.ID
	}
	event.Cancel = append(event.Cancel, *ch)
	if owner.GroupOwnerID == "" {
		event.MessagesAfterCancel = append(event.MessagesAfterCancel, groupDisbandMessage(owner)...)
		event.CancelAfterMessages = append(event.CancelAfterMessages, owner)
	}
	return event, nil
}

func removeString(values []string, target string) []string {
	out := values[:0]
	for _, value := range values {
		if value == target {
			continue
		}
		out = append(out, value)
	}
	return out
}

func (w *World) HandleGroupDisconnect(ch storage.Character) ([]storage.Character, error) {
	changed, _, err := w.HandleGroupDisconnectWithResult(ch)
	return changed, err
}

func (w *World) HandleGroupDisconnectWithResult(ch storage.Character) ([]storage.Character, GroupSyncEvent, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if ch.ID == "" || ch.GroupOwnerID == "" {
		return nil, GroupSyncEvent{}, nil
	}
	if ch.GroupOwnerID == ch.ID {
		changed := []storage.Character{}
		event := GroupSyncEvent{}
		recipients := append([]string(nil), ch.GroupMembers...)
		for i := len(ch.GroupMembers) - 1; i >= 0; i-- {
			memberID := ch.GroupMembers[i]
			actor := ch
			if memberID != ch.ID {
				member, ok := w.store.Character(memberID)
				if !ok {
					continue
				}
				actor = member
			}
			event.Messages = append(event.Messages, groupExitMessagesForIDs(recipients, actor)...)
			if memberID == ch.ID {
				continue
			}
			if member, ok := w.store.Character(memberID); ok {
				member.GroupOwnerID = ""
				changed = append(changed, member)
				event.Cancel = append(event.Cancel, member)
			}
		}
		ch.GroupOwnerID = ""
		ch.GroupMembers = nil
		changed = append(changed, ch)
		if err := w.store.SetCharacterRuntimeBatch(changed); err != nil {
			return nil, GroupSyncEvent{}, err
		}
		event.Updated = append(event.Updated, changed...)
		return changed, event, nil
	}
	ownerID := ch.GroupOwnerID
	owner, _ := w.store.Character(ownerID)
	oldMembers := append([]string(nil), owner.GroupMembers...)
	changed, err := w.removeGroupMemberLocked(ownerID, ch.ID, false)
	if err != nil {
		return nil, GroupSyncEvent{}, err
	}
	event := GroupSyncEvent{
		Updated:  append([]storage.Character(nil), changed...),
		Messages: groupExitMessagesForIDs(oldMembers, ch),
	}
	for _, entry := range changed {
		if entry.ID == ch.ID && entry.GroupOwnerID == "" {
			event.Cancel = append(event.Cancel, entry)
		}
		if entry.ID == ownerID && entry.GroupOwnerID == "" {
			event.MessagesAfterCancel = append(event.MessagesAfterCancel, groupDisbandMessage(entry)...)
			event.CancelAfterMessages = append(event.CancelAfterMessages, entry)
		}
	}
	if ownerID != "" {
		for _, entry := range changed {
			if entry.ID == ownerID && entry.GroupOwnerID != "" {
				event.MemberListOwnerID = ownerID
				break
			}
		}
	}
	return changed, event, nil
}
