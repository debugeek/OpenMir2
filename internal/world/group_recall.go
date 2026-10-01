package world

import (
	"fmt"
	"time"

	"openmir2/internal/storage"
)

const groupRecallCooldown = 180 * time.Second

func (w *World) handleAllowGroupRecallCommand(ch storage.Character) UserCommandResult {
	w.mu.Lock()
	defer w.mu.Unlock()
	ch.AllowGroupRecall = !ch.AllowGroupRecall
	if err := w.store.SaveCharacter(ch); err != nil {
		return UserCommandResult{Message: err.Error()}
	}
	if ch.AllowGroupRecall {
		return UserCommandResult{Character: ch, Message: "group recall enabled"}
	}
	return UserCommandResult{Character: ch, Message: "group recall disabled"}
}

func (w *World) handleGroupRecallCommand(owner storage.Character, players []storage.Character) UserCommandResult {
	w.mu.Lock()
	defer w.mu.Unlock()
	now := time.Now()
	if !owner.AdminMode && !w.hasRecallSuiteLocked(owner) {
		return UserCommandResult{Message: "group recall is not allowed"}
	}
	if mp, ok := w.data.Maps[owner.MapID]; !ok || mp.NoRecall {
		return UserCommandResult{Message: "this map does not allow group recall"}
	}
	if !owner.AdminMode && owner.GroupRecallUntil > now.UnixNano() {
		remaining := time.Until(time.Unix(0, owner.GroupRecallUntil)).Round(time.Second)
		return UserCommandResult{Message: fmt.Sprintf("group recall is cooling down: %s", remaining)}
	}
	if owner.GroupOwnerID != owner.ID {
		return UserCommandResult{Message: "only the group owner can recall"}
	}
	byID := make(map[string]storage.Character, len(players))
	for _, player := range players {
		byID[player.ID] = player
	}
	recallPlayers := append([]storage.Character(nil), players...)
	updatedOwner := owner
	updatedOwner.GroupRecallUntil = now.Add(groupRecallCooldown).UnixNano()
	if err := w.store.SaveCharacter(updatedOwner); err != nil {
		return UserCommandResult{Message: err.Error()}
	}
	result := UserCommandResult{Character: updatedOwner}
	for _, memberID := range owner.GroupMembers {
		if memberID == owner.ID {
			continue
		}
		member, ok := byID[memberID]
		if !ok || member.ID == "" {
			continue
		}
		if !member.AllowGroupRecall {
			result.Notices = append(result.Notices, member.Name+" is rejecting group recall")
			continue
		}
		if mp, ok := w.data.Maps[member.MapID]; !ok || mp.NoRecall {
			result.Notices = append(result.Notices, member.Name+" is in a map that does not allow recall")
			continue
		}
		position, ok := w.recallPositionLocked(owner, recallPlayers)
		if !ok {
			result.Notices = append(result.Notices, member.Name+" recall failed")
			continue
		}
		from := member
		member.MapID = owner.MapID
		member.X, member.Y = position[0], position[1]
		member.MapMoveAt = now.UnixNano()
		w.refreshCharacterObjectOrderLocked(&member)
		if err := w.store.SaveCharacter(member); err != nil {
			result.Notices = append(result.Notices, err.Error())
			continue
		}
		result.CharacterUpdates = append(result.CharacterUpdates, member)
		result.Teleports = append(result.Teleports, TeleportEvent{From: from, To: member})
		byID[member.ID] = member
		for i := range recallPlayers {
			if recallPlayers[i].ID == member.ID {
				recallPlayers[i] = member
				break
			}
		}
	}
	return result
}

func (w *World) recallPositionLocked(owner storage.Character, players []storage.Character) ([2]int, bool) {
	mp, ok := w.data.Maps[owner.MapID]
	if !ok {
		return [2]int{}, false
	}
	frontX, frontY, err := frontPosition(owner)
	if err != nil {
		return [2]int{}, false
	}
	occupied := make(map[[2]int]struct{}, len(players))
	for _, player := range players {
		if player.MapID == owner.MapID && player.HP > 0 {
			occupied[[2]int{player.X, player.Y}] = struct{}{}
		}
	}
	for radius := 0; radius <= 3; radius++ {
		for dy := -radius; dy <= radius; dy++ {
			for dx := -radius; dx <= radius; dx++ {
				if radius != 0 && abs(dx) != radius && abs(dy) != radius {
					continue
				}
				x, y := frontX+dx, frontY+dy
				if !mp.Walkable(x, y) {
					continue
				}
				if _, blocked := occupied[[2]int{x, y}]; blocked {
					continue
				}
				return [2]int{x, y}, true
			}
		}
	}
	return [2]int{}, false
}
