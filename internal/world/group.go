package world

import (
	"fmt"

	"openmir2/internal/storage"
)

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
			if err := w.store.SaveCharacter(ch); err != nil {
				return ch, GroupModeResult{}, err
			}
			return ch, GroupModeResult{Character: ch, Sync: GroupSyncEvent{Updated: []storage.Character{ch}}, ResponseParam: 0}, nil
		}
		if ch.GroupOwnerID != ch.ID {
			changed, err := w.removeGroupMemberLocked(ch.GroupOwnerID, ch.ID, true)
			if err != nil {
				return ch, GroupModeResult{}, err
			}
			event := GroupSyncEvent{Updated: append([]storage.Character(nil), changed...)}
			for _, entry := range changed {
				if entry.ID == ch.ID {
					ch = entry
					break
				}
			}
			return ch, GroupModeResult{Character: ch, Sync: event, ResponseParam: 0}, nil
		}
	}
	ch.AllowGroup = true
	if err := w.store.SaveCharacter(ch); err != nil {
		return ch, GroupModeResult{}, err
	}
	return ch, GroupModeResult{Character: ch, Sync: GroupSyncEvent{Updated: []storage.Character{ch}}, ResponseParam: 1}, nil
}

func (w *World) CreateGroup(owner, target storage.Character, onlineMemberCount int) (storage.Character, storage.Character, error) {
	updatedOwner, updatedTarget, _, err := w.CreateGroupWithResult(owner, target, onlineMemberCount)
	return updatedOwner, updatedTarget, err
}

func (w *World) CreateGroupWithResult(owner, target storage.Character, onlineMemberCount int) (storage.Character, storage.Character, GroupSyncEvent, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if owner.GroupOwnerID != "" {
		return owner, target, GroupSyncEvent{}, fmt.Errorf("group already exists")
	}
	if target.ID == "" || target.ID == owner.ID || target.HP <= 0 {
		return owner, target, GroupSyncEvent{}, fmt.Errorf("invalid group target")
	}
	if target.GroupOwnerID != "" {
		return owner, target, GroupSyncEvent{}, fmt.Errorf("target already in group")
	}
	if !target.AllowGroup {
		return owner, target, GroupSyncEvent{}, fmt.Errorf("target not allowing group")
	}
	if onlineMemberCount > 10 {
		return owner, target, GroupSyncEvent{}, fmt.Errorf("group is full")
	}
	owner.GroupOwnerID = owner.ID
	owner.AllowGroup = true
	owner.GroupMembers = []string{owner.ID, target.ID}
	target.GroupOwnerID = owner.ID
	if err := w.store.SaveCharacter(owner); err != nil {
		return owner, target, GroupSyncEvent{}, err
	}
	if err := w.store.SaveCharacter(target); err != nil {
		return owner, target, GroupSyncEvent{}, err
	}
	return owner, target, GroupSyncEvent{Updated: []storage.Character{owner, target}, MemberListOwnerID: owner.ID}, nil
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
	if target.ID == "" || target.ID == owner.ID || target.HP <= 0 {
		return owner, target, GroupSyncEvent{}, fmt.Errorf("invalid group target")
	}
	if target.GroupOwnerID != "" {
		return owner, target, GroupSyncEvent{}, fmt.Errorf("target already in group")
	}
	if !target.AllowGroup {
		return owner, target, GroupSyncEvent{}, fmt.Errorf("target not allowing group")
	}
	if onlineMemberCount > 10 {
		return owner, target, GroupSyncEvent{}, fmt.Errorf("group is full")
	}
	owner.GroupMembers = append(owner.GroupMembers, target.ID)
	target.GroupOwnerID = owner.ID
	if err := w.store.SaveCharacter(target); err != nil {
		return owner, target, GroupSyncEvent{}, err
	}
	if err := w.store.SaveCharacter(owner); err != nil {
		return owner, target, GroupSyncEvent{}, err
	}
	return owner, target, GroupSyncEvent{Updated: []storage.Character{owner, target}, MemberListOwnerID: owner.ID}, nil
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
	changed, err := w.removeGroupMemberLocked(owner.ID, target.ID, false)
	if err != nil {
		return owner, target, GroupSyncEvent{}, err
	}
	event := GroupSyncEvent{}
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
		event.Cancel = append(event.Cancel, owner)
	}
	if owner.GroupOwnerID != "" {
		event.MemberListOwnerID = owner.GroupOwnerID
	}
	return owner, target, event, nil
}

func (w *World) removeGroupMemberLocked(ownerID, memberID string, clearMemberAllow bool) ([]storage.Character, error) {
	if ownerID == "" || memberID == "" {
		return nil, nil
	}
	changed := []storage.Character{}
	member, memberOK := w.store.Character(memberID)
	if memberOK {
		member.GroupOwnerID = ""
		if clearMemberAllow {
			member.AllowGroup = false
		}
		if err := w.store.SaveCharacter(member); err != nil {
			return nil, err
		}
		changed = append(changed, member)
	}
	owner, ownerOK := w.store.Character(ownerID)
	if !ownerOK {
		return changed, nil
	}
	owner.GroupMembers = removeString(owner.GroupMembers, memberID)
	if len(owner.GroupMembers) <= 1 {
		owner.GroupOwnerID = ""
		owner.GroupMembers = nil
	}
	if err := w.store.SaveCharacter(owner); err != nil {
		return nil, err
	}
	changed = append(changed, owner)
	return changed, nil
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
		for _, memberID := range ch.GroupMembers {
			if memberID == ch.ID {
				continue
			}
			if member, ok := w.store.Character(memberID); ok {
				member.GroupOwnerID = ""
				_ = w.store.SaveCharacter(member)
				changed = append(changed, member)
				event.Cancel = append(event.Cancel, member)
			}
		}
		ch.GroupOwnerID = ""
		ch.GroupMembers = nil
		if err := w.store.SaveCharacter(ch); err != nil {
			return nil, GroupSyncEvent{}, err
		}
		changed = append(changed, ch)
		event.Updated = append(event.Updated, changed...)
		return changed, event, nil
	}
	changed, err := w.removeGroupMemberLocked(ch.GroupOwnerID, ch.ID, false)
	if err != nil {
		return nil, GroupSyncEvent{}, err
	}
	event := GroupSyncEvent{Updated: append([]storage.Character(nil), changed...)}
	for _, entry := range changed {
		if entry.ID == ch.ID && entry.GroupOwnerID == "" {
			event.Cancel = append(event.Cancel, entry)
		}
	}
	if ch.GroupOwnerID != "" {
		event.MemberListOwnerID = ch.GroupOwnerID
	}
	return changed, event, nil
}
