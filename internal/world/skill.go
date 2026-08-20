package world

import (
	"openmir2/internal/data"
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
	"困魔咒":   16,
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

func (w *World) Skill(skillID string) (data.StdSkill, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	skill, ok := w.data.Skills[skillID]
	if !ok {
		return data.StdSkill{}, false
	}
	return skill, true
}

func (w *World) MagicIDByName(name string) (uint16, bool) {
	id, ok := skillIDs[name]
	return id, ok
}
