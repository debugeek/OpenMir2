package world

import (
	"fmt"

	"openmir2/internal/data"
	"openmir2/internal/storage"
)

var skillIDs = map[string]uint16{
	"火球术":   1,
	"治愈术":   2,
	"基本剑术":  3,
	"精神力战法": 4,
	"大火球":   5,
	"施毒术":   6,
	"攻杀剑术":  7,
	"抗拒火环":  8,
	"地狱火":   9,
	"疾光电影":  10,
	"雷电术":   11,
	"刺杀剑术":  12,
	"灵魂火符":  13,
	"幽灵盾":   14,
	"神圣战甲术": 15,
	"困魔咒":   50,
	"召唤骷髅":  17,
	"隐身术":   18,
	"集体隐身术": 19,
	"诱惑之光":  20,
	"瞬息移动":  21,
	"火墙":    22,
	"爆裂火焰":  23,
	"地狱雷光":  24,
	"半月弯刀":  25,
	"烈火剑法":  26,
	"野蛮冲撞":  27,
	"心灵启示":  28,
	"群体治疗术": 29,
	"召唤神兽":  30,
	"魔法盾":   31,
	"圣言术":   32,
	"冰咆哮":   33,
}

var magicNames = func() map[uint16]string {
	out := make(map[uint16]string, len(skillIDs))
	for name, id := range skillIDs {
		out[id] = name
	}
	return out
}()

func isWarriorSkill(skillID string) bool {
	switch skillID {
	case "基本剑术", "精神力战法", "攻杀剑术", "刺杀剑术", "半月弯刀", "烈火剑法", "野蛮冲撞":
		return true
	default:
		return false
	}
}

func (w *World) Skill(skillID string) (data.StdSkill, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	skill, ok := w.data.Skills[skillID]
	if !ok {
		return data.StdSkill{}, false
	}
	return skill, true
}

func itemGrantedSkillID(item data.StdItem) string {
	switch {
	case item.Shape == 115 || item.AniCount == 115:
		return "火球术"
	case item.Shape == 116 || item.AniCount == 116:
		return "治愈术"
	default:
		return ""
	}
}

func (w *World) itemSkillChangesLocked(before, after storage.Character) ([]storage.SkillState, []string) {
	added := make([]storage.SkillState, 0, 2)
	removed := make([]string, 0, 2)
	for _, skillID := range []string{"火球术", "治愈术"} {
		if before.Skills.Has(skillID) || after.Skills.Has(skillID) {
			continue
		}
		_, _, had := w.skillStateLocked(before, skillID)
		_, _, has := w.skillStateLocked(after, skillID)
		switch {
		case !had && has:
			added = append(added, storage.SkillState{ID: skillID, Level: 1})
		case had && !has:
			removed = append(removed, skillID)
		}
	}
	return added, removed
}

func (w *World) skillStateLocked(ch storage.Character, skillID string) (storage.SkillState, int, bool) {
	if state, idx, ok := ch.Skills.Get(skillID); ok {
		return state, idx, true
	}
	for slot := 0; slot < useSlotCount; slot++ {
		entry, ok := w.equippedItemLocked(ch, slot)
		if !ok {
			continue
		}
		item, ok := w.data.Items[entry.ItemID]
		if !ok || itemGrantedSkillID(item) != skillID {
			continue
		}
		return storage.SkillState{ID: skillID}, -1, true
	}
	return storage.SkillState{}, -1, false
}

func (w *World) EffectiveSkillStates(ch storage.Character) []storage.SkillState {
	w.mu.Lock()
	defer w.mu.Unlock()
	states := append([]storage.SkillState(nil), ch.Skills...)
	seen := make(map[string]struct{}, len(states))
	for _, state := range states {
		seen[state.ID] = struct{}{}
	}
	for slot := 0; slot < useSlotCount; slot++ {
		entry, ok := w.equippedItemLocked(ch, slot)
		if !ok {
			continue
		}
		item, ok := w.data.Items[entry.ItemID]
		if !ok {
			continue
		}
		skillID := itemGrantedSkillID(item)
		if skillID == "" {
			continue
		}
		if _, ok := seen[skillID]; ok {
			continue
		}
		states = append(states, storage.SkillState{ID: skillID})
		seen[skillID] = struct{}{}
	}
	return states
}

func (w *World) MagicIDByName(name string) (uint16, bool) {
	id, ok := skillIDs[name]
	return id, ok
}

func (w *World) SkillIDByMagicID(magicID uint16) (string, bool) {
	name, ok := magicNames[magicID]
	return name, ok
}

func (w *World) SetSkillHotkey(ch storage.Character, skillID string, hotkey byte) (storage.Character, bool, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for i := range ch.Skills {
		if ch.Skills[i].ID != skillID {
			continue
		}
		if ch.Skills[i].Hotkey == hotkey {
			return ch, false, nil
		}
		ch.Skills[i].Hotkey = hotkey
		if err := w.store.SaveCharacter(ch); err != nil {
			return ch, false, err
		}
		return ch, true, nil
	}
	return ch, false, fmt.Errorf("skill %s not learned", skillID)
}
