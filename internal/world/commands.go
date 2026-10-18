package world

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"openmir2/internal/storage"
)

type UserCommandResult struct {
	Message          string
	Monsters         []Monster
	AddedItems       []storage.UserItem
	Character        storage.Character
	Teleport         *TeleportEvent
	CharacterUpdates []storage.Character
	Teleports        []TeleportEvent
	Notices          []string
	SystemNotices    []SystemNotice
}

type SystemNotice struct {
	Message    string
	Foreground byte
	Background byte
}

const (
	attackModeAll = iota
	attackModePeaceful
	attackModeGroup
	attackModeGuild
	attackModePK
)

type ChatResult struct {
	Message    string
	Global     bool
	Private    bool
	Group      bool
	Guild      bool
	TargetName string
}

type SayResult struct {
	Command *UserCommandResult
	Chat    *ChatResult
}

func (w *World) HandleUserCommand(activeChar storage.Character, line string) (UserCommandResult, bool) {
	return w.handleUserCommand(activeChar, line, nil)
}

func (w *World) HandleUserCommandWithPlayers(activeChar storage.Character, line string, players []storage.Character) (UserCommandResult, bool) {
	return w.handleUserCommand(activeChar, line, players)
}

func (w *World) handleUserCommand(activeChar storage.Character, line string, players []storage.Character) (UserCommandResult, bool) {
	name, params, ok := parseUserCommand(line)
	if !ok {
		return UserCommandResult{}, false
	}
	switch strings.ToLower(name) {
	case "mob":
		return w.handleMobCommand(activeChar, params), true
	case "make":
		return w.handleMakeCommand(activeChar, params), true
	case "move":
		return w.handleMoveCommand(activeChar, params, players), true
	case "allowgrouprecall":
		return w.handleAllowGroupRecallCommand(activeChar, params), true
	case "grouprecall":
		return w.handleGroupRecallCommand(activeChar, players), true
	case "attackmode":
		return w.handleAttackModeCommand(activeChar, params), true
	case "rest":
		return w.handleRestCommand(activeChar), true
	case "diary":
		return UserCommandResult{}, true
	default:
		return UserCommandResult{SystemNotices: []SystemNotice{{
			Message:    "@" + name + " 此命令不正确，或没有足够的权限！！！",
			Foreground: 0xFF,
			Background: 0x38,
		}}}, true
	}
}

func (w *World) handleRestCommand(ch storage.Character) UserCommandResult {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, mon := range w.monsters {
		if mon != nil && mon.MasterID == ch.ID {
			ch.SlaveRelax = !ch.SlaveRelax
			message := "宝宝行动：攻击"
			if ch.SlaveRelax {
				message = "宝宝行动：休息"
			}
			return UserCommandResult{
				Character: ch,
				SystemNotices: []SystemNotice{{
					Message: message, Foreground: 0xDB, Background: 0xFF,
				}},
			}
		}
	}
	return UserCommandResult{}
}

func (w *World) handleAttackModeCommand(ch storage.Character, params []string) UserCommandResult {
	mode := -1
	if len(params) > 0 {
		mode, _ = strconv.Atoi(params[0])
	}
	if mode < attackModeAll || mode > attackModePK {
		mode = ch.AttackMode + 1
		if mode > attackModePK {
			mode = attackModeAll
		}
	}
	ch.AttackMode = mode
	messages := [...]string{
		"[攻击模式: 全体攻击]",
		"[攻击模式: 和平攻击]",
		"[攻击模式: 编组攻击]",
		"[攻击模式: 行会攻击]",
		"[攻击模式: 红名攻击]",
	}
	foreground, background := byte(0xDB), byte(0xFF)
	if mode == attackModeAll {
		foreground, background = 0xFF, 0xFC
	}
	return UserCommandResult{
		Character: ch,
		SystemNotices: []SystemNotice{{
			Message: messages[mode], Foreground: foreground, Background: background,
		}},
	}
}

func (w *World) HandleChat(activeChar storage.Character, line string) (ChatResult, bool) {
	if line == "" || strings.HasPrefix(line, "@") {
		return ChatResult{}, false
	}
	if strings.HasPrefix(line, "/") {
		targetName, message := referenceChatPrivateParts(strings.TrimPrefix(line, "/"))
		return ChatResult{Message: activeChar.Name + "=> " + message, Private: true, TargetName: targetName}, true
	}
	if strings.HasPrefix(line, "!!") {
		message := strings.TrimPrefix(line, "!!")
		return ChatResult{Message: activeChar.Name + ": " + message, Group: true}, true
	}
	if strings.HasPrefix(line, "!~") {
		message := strings.TrimPrefix(line, "!~")
		if activeChar.GuildID == "" {
			return ChatResult{}, false
		}
		return ChatResult{Message: activeChar.Name + ": " + message, Guild: true}, true
	}
	if strings.HasPrefix(line, "!") {
		return ChatResult{
			Message: "(!)" + activeChar.Name + ": " + strings.TrimPrefix(line, "!"),
			Global:  true,
		}, true
	}
	return ChatResult{Message: activeChar.Name + ":" + line}, true
}

func referenceChatPrivateParts(raw string) (string, string) {
	separator := strings.IndexByte(raw, ' ')
	if separator < 0 {
		return raw, ""
	}
	if separator > 0 {
		return raw[:separator], raw[separator+1:]
	}
	if next := strings.IndexByte(raw[1:], ' '); next >= 0 {
		return raw[:next+1], raw[next+2:]
	}
	return raw, ""
}

func (w *World) HandleSay(activeChar storage.Character, line string) (SayResult, bool) {
	return w.handleSay(activeChar, line, nil)
}

func (w *World) HandleSayWithPlayers(activeChar storage.Character, line string, players []storage.Character) (SayResult, bool) {
	return w.handleSay(activeChar, line, players)
}

func (w *World) handleSay(activeChar storage.Character, line string, players []storage.Character) (SayResult, bool) {
	if line == "" {
		return SayResult{}, false
	}
	if strings.HasPrefix(line, "@") {
		result, ok := w.handleUserCommand(activeChar, line, players)
		if !ok {
			return SayResult{}, false
		}
		return SayResult{Command: &result}, true
	}
	result, ok := w.HandleChat(activeChar, line)
	if !ok {
		return SayResult{}, false
	}
	return SayResult{Chat: &result}, true
}

func parseUserCommand(line string) (string, []string, bool) {
	if line == "" || line[0] != '@' {
		return "", nil, false
	}
	parts := strings.FieldsFunc(line[1:], func(r rune) bool {
		return r == ' ' || r == ':' || r == ',' || r == '\t'
	})
	if len(parts) == 0 {
		return "", nil, true
	}
	return parts[0], parts[1:], true
}

func frontPosition(ch storage.Character) (int, int, error) {
	if ch.Dir < 0 || ch.Dir > 7 {
		return 0, 0, fmt.Errorf("invalid direction %d", ch.Dir)
	}
	x, y := ch.X, ch.Y
	switch ch.Dir {
	case 0:
		y--
	case 1:
		x++
		y--
	case 2:
		x++
	case 3:
		x++
		y++
	case 4:
		y++
	case 5:
		x--
		y++
	case 6:
		x--
	case 7:
		x--
		y--
	}
	return x, y, nil
}

func (w *World) handleMobCommand(activeChar storage.Character, params []string) UserCommandResult {
	if len(params) == 0 {
		return UserCommandResult{Message: "usage: @Mob monster-id count level"}
	}
	count := 1
	if len(params) > 1 {
		if n, err := strconv.Atoi(params[1]); err == nil && n > 0 {
			count = n
		}
	}
	if count > 64 {
		count = 64
	}
	x, y, err := frontPosition(activeChar)
	if err != nil {
		return UserCommandResult{Message: err.Error()}
	}
	result, err := w.SpawnMonsterByNameAt(activeChar.MapID, x, y, params[0], count)
	if err != nil {
		return UserCommandResult{Message: err.Error()}
	}
	spawnedName := params[0]
	if len(result.Monsters) > 0 && result.Monsters[0].Name != "" {
		spawnedName = result.Monsters[0].Name
	}
	return UserCommandResult{
		Message:  fmt.Sprintf("spawned %d %s", len(result.Monsters), spawnedName),
		Monsters: result.Monsters,
	}
}

func (w *World) handleMakeCommand(activeChar storage.Character, params []string) UserCommandResult {
	if len(params) == 0 {
		return UserCommandResult{Message: "usage: @Make item-name count"}
	}
	itemName := params[0]
	count := 1
	if len(params) > 1 {
		if n, err := strconv.Atoi(params[1]); err == nil && n > 0 {
			count = n
		}
	}
	if count > 10 {
		count = 10
	}
	updated, added, err := w.MakeItemsByName(activeChar, itemName, count)
	if err != nil {
		return UserCommandResult{Message: err.Error()}
	}
	if len(added) == 0 {
		return UserCommandResult{Message: "bag is full"}
	}
	return UserCommandResult{
		Message:    fmt.Sprintf("made %d %s", len(added), itemName),
		AddedItems: added,
		Character:  updated,
	}
}

func (w *World) handleMoveCommand(activeChar storage.Character, params []string, players []storage.Character) UserCommandResult {
	if result, handled := w.handleTeleportRingMove(activeChar, params, players); handled {
		return result
	}
	return moveSystemNotice("您现在还无法使用此功能！！！", 0xFF, 0x38)
}

func (w *World) handleTeleportRingMove(activeChar storage.Character, params []string, players []storage.Character) (UserCommandResult, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.hasTeleportRingLocked(activeChar) {
		return UserCommandResult{}, false
	}
	x, y := -1, -1
	rawX, rawY := "", ""
	if len(params) > 0 {
		rawX = params[0]
		x, _ = strconv.Atoi(params[0])
	}
	if len(params) > 1 {
		rawY = params[1]
		y, _ = strconv.Atoi(params[1])
	}
	if activeChar.MapID == "" {
		return UserCommandResult{Message: "character has no current map"}, true
	}
	mp, ok := w.data.Maps[activeChar.MapID]
	if !ok {
		return UserCommandResult{Message: "character has no current map"}, true
	}
	if mp.NoPositionMove {
		return moveSystemNotice("此地图禁止使用此命令！！！", 0xFF, 0x38), true
	}
	if !mp.Walkable(x, y) {
		return moveSystemNotice(fmt.Sprintf("无法移动到地图: %s X:%s Y:%s", activeChar.MapID, rawX, rawY), 0xDB, 0xFF), true
	}
	if !w.gameplay.Movement.UserMoveCanDupObj && w.monsterAtLocked(activeChar.MapID, x, y, "") {
		return moveSystemNotice(fmt.Sprintf("无法移动到地图: %s X:%s Y:%s", activeChar.MapID, rawX, rawY), 0xDB, 0xFF), true
	}
	if !w.gameplay.Movement.UserMoveCanDupObj {
		for _, player := range players {
			if player.ID != activeChar.ID && !w.characterDeadLocked(player) && !player.Ghost && player.MapID == activeChar.MapID && player.X == x && player.Y == y {
				return moveSystemNotice(fmt.Sprintf("无法移动到地图: %s X:%s Y:%s", activeChar.MapID, rawX, rawY), 0xDB, 0xFF), true
			}
		}
	}
	if !w.gameplay.Movement.UserMoveCanOnItem && w.groundDropAtLocked(activeChar.MapID, x, y) {
		return moveSystemNotice(fmt.Sprintf("无法移动到地图: %s X:%s Y:%s", activeChar.MapID, rawX, rawY), 0xDB, 0xFF), true
	}
	now := time.Now()
	cooldown := time.Duration(w.gameplay.Movement.UserMoveCooldownMS) * time.Millisecond
	if activeChar.TeleportRingAt != 0 && now.Sub(time.Unix(0, activeChar.TeleportRingAt)) <= cooldown {
		remaining := int(cooldown/time.Second) - int(now.Sub(time.Unix(0, activeChar.TeleportRingAt))/time.Second)
		if remaining < 1 {
			remaining = 1
		}
		return moveSystemNotice(fmt.Sprintf("%d秒之后才可以再使用此功能！！！", remaining), 0xFF, 0x38), true
	}
	from := activeChar
	activeChar.X, activeChar.Y = x, y
	activeChar.TeleportRingAt = now.UnixNano()
	w.refreshCharacterObjectOrderLocked(&activeChar)
	return UserCommandResult{
		Character: activeChar,
		Teleport:  &TeleportEvent{From: from, To: activeChar, SpaceMoveFire: true},
	}, true
}

func moveSystemNotice(message string, foreground, background byte) UserCommandResult {
	return UserCommandResult{SystemNotices: []SystemNotice{{
		Message: message, Foreground: foreground, Background: background,
	}}}
}

func (w *World) groundDropAtLocked(mapID string, x, y int) bool {
	for _, drop := range w.drops {
		if drop.MapID == mapID && drop.X == x && drop.Y == y {
			return true
		}
	}
	return false
}

func (w *World) hasTeleportRingLocked(ch storage.Character) bool {
	for slot := 0; slot < useSlotCount; slot++ {
		item, ok := w.activeEquippedItemLocked(ch, slot)
		if !ok {
			continue
		}
		std, ok := w.data.Items[item.ItemID]
		if !ok {
			continue
		}
		if (slot == SlotRingL || slot == SlotRingR) && std.Shape == 112 {
			return true
		}
		if slot != SlotDress && slot != SlotWeapon && slot != SlotRightHand && (std.Shape == 157 || std.Shape == 158 || std.Shape == 159 || std.Shape == 160) {
			return true
		}
		if (slot == SlotDress || slot == SlotWeapon || slot == SlotRightHand) && (std.AniCount == 112 || (std.AniCount >= 157 && std.AniCount <= 160)) {
			return true
		}
	}
	return false
}
