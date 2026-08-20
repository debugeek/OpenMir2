package world

import (
	"fmt"

	"openmir2/internal/storage"
)

func (w *World) SetGroupMode(ch storage.Character, allow bool) (storage.Character, []storage.Character, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !allow {
		if ch.GroupOwnerID == "" {
			ch.AllowGroup = false
			if err := w.store.SaveCharacter(ch); err != nil {
				return ch, nil, err
			}
			return ch, nil, nil
		}
		if ch.GroupOwnerID != ch.ID {
			changed, err := w.removeGroupMemberLocked(ch.GroupOwnerID, ch.ID, true)
			if err != nil {
				return ch, nil, err
			}
			for _, entry := range changed {
				if entry.ID == ch.ID {
					ch = entry
					break
				}
			}
			return ch, changed, nil
		}
	}
	ch.AllowGroup = true
	if err := w.store.SaveCharacter(ch); err != nil {
		return ch, nil, err
	}
	return ch, []storage.Character{ch}, nil
}

func (w *World) CreateGroup(owner, target storage.Character, onlineMemberCount int) (storage.Character, storage.Character, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if owner.GroupOwnerID != "" {
		return owner, target, fmt.Errorf("group already exists")
	}
	if target.ID == "" || target.ID == owner.ID || target.HP <= 0 {
		return owner, target, fmt.Errorf("invalid group target")
	}
	if target.GroupOwnerID != "" {
		return owner, target, fmt.Errorf("target already in group")
	}
	if !target.AllowGroup {
		return owner, target, fmt.Errorf("target not allowing group")
	}
	if onlineMemberCount > 10 {
		return owner, target, fmt.Errorf("group is full")
	}
	owner.GroupOwnerID = owner.ID
	owner.AllowGroup = true
	owner.GroupMembers = []string{owner.ID, target.ID}
	target.GroupOwnerID = owner.ID
	if err := w.store.SaveCharacter(owner); err != nil {
		return owner, target, err
	}
	if err := w.store.SaveCharacter(target); err != nil {
		return owner, target, err
	}
	return owner, target, nil
}

func (w *World) AddGroupMember(owner, target storage.Character, onlineMemberCount int) (storage.Character, storage.Character, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if owner.GroupOwnerID != owner.ID {
		return owner, target, fmt.Errorf("not group owner")
	}
	if target.ID == "" || target.ID == owner.ID || target.HP <= 0 {
		return owner, target, fmt.Errorf("invalid group target")
	}
	if target.GroupOwnerID != "" {
		return owner, target, fmt.Errorf("target already in group")
	}
	if !target.AllowGroup {
		return owner, target, fmt.Errorf("target not allowing group")
	}
	if onlineMemberCount > 10 {
		return owner, target, fmt.Errorf("group is full")
	}
	owner.GroupMembers = append(owner.GroupMembers, target.ID)
	target.GroupOwnerID = owner.ID
	if err := w.store.SaveCharacter(target); err != nil {
		return owner, target, err
	}
	if err := w.store.SaveCharacter(owner); err != nil {
		return owner, target, err
	}
	return owner, target, nil
}

func (w *World) DelGroupMember(owner, target storage.Character) (storage.Character, storage.Character, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if owner.GroupOwnerID != owner.ID {
		return owner, target, fmt.Errorf("not group owner")
	}
	changed, err := w.removeGroupMemberLocked(owner.ID, target.ID, false)
	if err != nil {
		return owner, target, err
	}
	for _, entry := range changed {
		switch entry.ID {
		case owner.ID:
			owner = entry
		case target.ID:
			target = entry
		}
	}
	return owner, target, nil
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
	w.mu.Lock()
	defer w.mu.Unlock()
	if ch.ID == "" || ch.GroupOwnerID == "" {
		return nil, nil
	}
	if ch.GroupOwnerID == ch.ID {
		changed := []storage.Character{}
		for _, memberID := range ch.GroupMembers {
			if memberID == ch.ID {
				continue
			}
			if member, ok := w.store.Character(memberID); ok {
				member.GroupOwnerID = ""
				_ = w.store.SaveCharacter(member)
				changed = append(changed, member)
			}
		}
		ch.GroupOwnerID = ""
		ch.GroupMembers = nil
		if err := w.store.SaveCharacter(ch); err != nil {
			return nil, err
		}
		return append(changed, ch), nil
	}
	changed, err := w.removeGroupMemberLocked(ch.GroupOwnerID, ch.ID, false)
	if err != nil {
		return nil, err
	}
	return changed, nil
}
