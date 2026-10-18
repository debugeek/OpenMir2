package world

import (
	"fmt"
	"strings"
	"time"

	"openmir2/internal/storage"
)

const groupRecallCooldown = 180 * time.Second

func (w *World) handleAllowGroupRecallCommand(ch storage.Character, params []string) UserCommandResult {
	if len(params) > 0 && strings.HasPrefix(params[0], "?") {
		return UserCommandResult{SystemNotices: []SystemNotice{{
			Message: "此命令用于允许或禁止编组传送功能。", Foreground: 0xFF, Background: 0x38,
		}}}
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	ch.AllowGroupRecall = !ch.AllowGroupRecall
	message := "[禁止天地合一]"
	if ch.AllowGroupRecall {
		message = "[允许天地合一]"
	}
	return UserCommandResult{
		Character: ch,
		SystemNotices: []SystemNotice{{
			Message: message, Foreground: 0xDB, Background: 0xFF,
		}},
	}
}

func (w *World) handleGroupRecallCommand(owner storage.Character, players []storage.Character) UserCommandResult {
	w.mu.Lock()
	defer w.mu.Unlock()
	now := time.Now()
	if !w.hasRecallSuiteLocked(owner) {
		return UserCommandResult{SystemNotices: []SystemNotice{{
			Message: "您现在还无法使用此功能！！！", Foreground: 0xFF, Background: 0x38,
		}}}
	}
	if mp, ok := w.data.Maps[owner.MapID]; !ok || mp.NoRecall {
		return UserCommandResult{SystemNotices: []SystemNotice{{
			Message: "此地图禁止使用此命令！！！", Foreground: 0xFF, Background: 0x38,
		}}}
	}
	if owner.GroupRecallUntil > now.UnixNano() {
		remainingDuration := time.Until(time.Unix(0, owner.GroupRecallUntil))
		remaining := int((remainingDuration + time.Second - 1) / time.Second)
		return UserCommandResult{SystemNotices: []SystemNotice{{
			Message: fmt.Sprintf("%d seconds, You can use it again.", remaining), Foreground: 0xFF, Background: 0x38,
		}}}
	}
	if owner.GroupOwnerID != owner.ID {
		return UserCommandResult{}
	}
	byID := make(map[string]storage.Character, len(players))
	for _, player := range players {
		byID[player.ID] = player
	}
	recallPlayers := append([]storage.Character(nil), players...)
	updatedOwner := owner
	updatedOwner.GroupRecallUntil = now.Add(groupRecallCooldown).UnixNano()
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
			result.Notices = append(result.Notices, member.Name+" is now rejecting GroupRecall.")
			continue
		}
		if mp, ok := w.data.Maps[member.MapID]; !ok || mp.NoRecall {
			result.Notices = append(result.Notices, member.Name+" map does not allow recall.")
			continue
		}
		position, ok := w.recallPositionLocked(owner, recallPlayers)
		if !ok {
			result.Notices = append(result.Notices, "召唤失败！！！")
			continue
		}
		from := member
		member.MapID = owner.MapID
		member.X, member.Y = position[0], position[1]
		member.MapMoveAt = now.UnixNano()
		w.refreshCharacterObjectOrderLocked(&member)
		result.CharacterUpdates = append(result.CharacterUpdates, member)
		result.Teleports = append(result.Teleports, TeleportEvent{From: from, To: member, SpaceMoveHide: true})
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
	frontX, frontY, err := frontPosition(owner)
	if err != nil {
		return [2]int{}, false
	}
	if mp, ok := w.data.Maps[owner.MapID]; ok {
		if frontX < 0 {
			frontX = 0
		} else if frontX >= mp.Width {
			frontX = mp.Width - 1
		}
		if frontY < 0 {
			frontY = 0
		} else if frontY >= mp.Height {
			frontY = mp.Height - 1
		}
	}
	byID := make(map[string]storage.Character, len(players))
	for _, player := range players {
		byID[player.ID] = player
	}
	for radius := 0; radius <= 3; radius++ {
		for dy := -radius; dy <= radius; dy++ {
			for dx := -radius; dx <= radius; dx++ {
				x, y := frontX+dx, frontY+dy
				if w.movingObjectAtLocked(byID, owner.MapID, x, y, "") {
					continue
				}
				return [2]int{x, y}, true
			}
		}
	}
	return [2]int{}, false
}
