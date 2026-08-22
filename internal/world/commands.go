package world

import (
	"fmt"
	"strconv"
	"strings"

	"openmir2/internal/storage"
)

type UserCommandResult struct {
	Message    string
	Monsters   []Monster
	AddedItems []storage.UserItem
	Character  storage.Character
	Teleport   *TeleportEvent
}

type ChatResult struct {
	Message string
	Global  bool
}

type SayResult struct {
	Command *UserCommandResult
	Chat    *ChatResult
}

func (w *World) HandleUserCommand(activeChar storage.Character, line string) (UserCommandResult, bool) {
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
		return w.handleMoveCommand(activeChar, params), true
	default:
		return UserCommandResult{Message: "unknown command: @" + name}, true
	}
}

func (w *World) HandleChat(activeChar storage.Character, line string) (ChatResult, bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "@") {
		return ChatResult{}, false
	}
	if strings.HasPrefix(line, "!") {
		return ChatResult{
			Message: "(!)" + activeChar.Name + ": " + strings.TrimPrefix(line, "!"),
			Global:  true,
		}, true
	}
	return ChatResult{Message: activeChar.Name + ":" + line}, true
}

func (w *World) HandleSay(activeChar storage.Character, line string) (SayResult, bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return SayResult{}, false
	}
	if strings.HasPrefix(line, "@") {
		result, ok := w.HandleUserCommand(activeChar, line)
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
	line = strings.TrimSpace(line)
	if line == "" || line[0] != '@' {
		return "", nil, false
	}
	line = strings.TrimSpace(line[1:])
	if line == "" {
		return "", nil, false
	}
	parts := strings.FieldsFunc(line, func(r rune) bool {
		return r == ' ' || r == ':' || r == ',' || r == '\t'
	})
	if len(parts) == 0 {
		return "", nil, false
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

func (w *World) handleMoveCommand(activeChar storage.Character, params []string) UserCommandResult {
	switch len(params) {
	case 0:
		if activeChar.MapID == "" {
			return UserCommandResult{Message: "character has no current map"}
		}
		updated, err := w.TeleportRandomInCurrentMap(activeChar)
		if err != nil {
			return UserCommandResult{Message: err.Error()}
		}
		return UserCommandResult{
			Message:   fmt.Sprintf("moved to %s %d %d", updated.MapID, updated.X, updated.Y),
			Character: updated,
			Teleport:  newTeleportEvent(activeChar, updated),
		}
	case 1:
		updated, err := w.TeleportRandomInMap(activeChar, params[0])
		if err != nil {
			return UserCommandResult{Message: err.Error()}
		}
		return UserCommandResult{
			Message:   fmt.Sprintf("moved to %s %d %d", updated.MapID, updated.X, updated.Y),
			Character: updated,
			Teleport:  newTeleportEvent(activeChar, updated),
		}
	case 2:
		if activeChar.MapID == "" {
			return UserCommandResult{Message: "character has no current map"}
		}
		x, err := strconv.Atoi(params[0])
		if err != nil {
			return UserCommandResult{Message: "usage: @Move [map] x y"}
		}
		y, err := strconv.Atoi(params[1])
		if err != nil {
			return UserCommandResult{Message: "usage: @Move [map] x y"}
		}
		updated, err := w.Teleport(activeChar, activeChar.MapID, x, y)
		if err != nil {
			return UserCommandResult{Message: err.Error()}
		}
		return UserCommandResult{
			Message:   fmt.Sprintf("moved to %s %d %d", updated.MapID, updated.X, updated.Y),
			Character: updated,
			Teleport:  newTeleportEvent(activeChar, updated),
		}
	case 3:
		x, err := strconv.Atoi(params[1])
		if err != nil {
			return UserCommandResult{Message: "usage: @Move [map] x y"}
		}
		y, err := strconv.Atoi(params[2])
		if err != nil {
			return UserCommandResult{Message: "usage: @Move [map] x y"}
		}
		updated, err := w.Teleport(activeChar, params[0], x, y)
		if err != nil {
			return UserCommandResult{Message: err.Error()}
		}
		return UserCommandResult{
			Message:   fmt.Sprintf("moved to %s %d %d", updated.MapID, updated.X, updated.Y),
			Character: updated,
			Teleport:  newTeleportEvent(activeChar, updated),
		}
	default:
		return UserCommandResult{Message: "usage: @Move [map] x y"}
	}
}
