package world

import (
	"fmt"
	"math"
	"math/rand"
	"sort"
	"time"

	"openmir2/internal/data"
	"openmir2/internal/storage"
)

const defaultTurnUndeadLevel = 50
const defaultTamingCount = 5
const defaultSummonCount = 1
const defaultTamingHPRate = 100
const defaultSpellAttackRange = 8
const spellDelayMagic = 600 * time.Millisecond
const spellTrainLevel = 3

func referenceRound(value float64) int {
	if value < 0 {
		return -referenceRound(-value)
	}
	base := math.Floor(value)
	fraction := value - base
	if fraction > 0.5 || fraction == 0.5 && int64(base)%2 != 0 {
		base++
	}
	return int(base)
}

type pendingSpell struct {
	DueAt                 time.Time
	CasterID              string
	TargetMonsterID       string
	TargetCharacterID     string
	SetCasterTarget       bool
	CharacterDamage       bool
	Healing               int
	TargetX               int
	TargetY               int
	Damage                int
	PoisonHealthLevel     byte
	PoisonHealth          bool
	PoisonArmor           bool
	PoisonNotification    bool
	PoisonPoint           int
	PoisonUntil           time.Time
	PoisonDuration        time.Duration
	ParalysisDuration     time.Duration
	CharacterBubbleBefore int64
	CharacterBubbleAfter  int64
	CharacterBubbleLevel  byte
	TransparentUntil      time.Time
	TransparentDuration   time.Duration
	ShowHealthDuration    time.Duration
	ShowHealthStartedAt   time.Time
}

type SpellEventKind uint8

const (
	SpellEventStart SpellEventKind = iota
	SpellEventCasterState
	SpellEventMagicFire
	SpellEventSpaceMoveFire
	SpellEventSpaceMoveMapChange
	SpellEventSpaceMoveShow
	SpellEventCharacter
	SpellEventTeleport
	SpellEventSummon
	SpellEventMonsterRefresh
	SpellEventMonsterNameColor
	SpellEventMonsterUsername
	SpellEventMonsterHit
	SpellEventMonsterAction
	SpellEventCharacterHit
	SpellEventCharacterNameColor
	SpellEventAffectedCharacter
	SpellEventHealingGauge
	SpellEventExperience
	SpellEventLevelUp
	SpellEventSkillExp
	SpellEventRush
	SpellEventCharacterPush
	SpellEventItemDelete
	SpellEventDurability
	SpellEventMagicFireFail
)

type SpellRush struct {
	Character storage.Character
	Dir       int
	X         int
	Y         int
	Kung      bool
}

type CharacterPush struct {
	Character storage.Character
	Dir       int
}

type SpellGroundEvent struct {
	ID       int32
	MapID    string
	X        int
	Y        int
	Type     int
	Param    int
	Duration time.Duration
	StartAt  time.Time
}

type SpellDurability struct {
	Slot    int
	Dura    uint16
	DuraMax uint16
}

type SpellEvent struct {
	Kind                    SpellEventKind
	Caster                  storage.Character
	Character               storage.Character
	Previous                storage.Character
	MagicID                 uint16
	Effect                  int
	TargetX                 int
	TargetY                 int
	TargetID                int32
	Teleport                bool
	SuppressStateBroadcast  bool
	SuppressStatusBroadcast bool
	Monster                 Monster
	MonsterHit              AttackResult
	MonsterAction           MonsterAction
	CharacterHit            CharacterHit
	Experience              int
	CurrentExp              int
	SkillLevel              byte
	SkillTrain              int
	SkillExpDelay           time.Duration
	SkillExpReplacePending  bool
	SystemMessage           string
	SendHealth              bool
	SendAbility             bool
	SendStatus              bool
	SendUserState           bool
	HealingGauge            bool
	Rush                    SpellRush
	CharacterPush           CharacterPush
	GroundEvent             SpellGroundEvent
	DeletedItem             storage.UserItem
	Durability              SpellDurability
}

type SpellHealingGaugeTarget struct {
	Character *storage.Character
	Monster   *Monster
}

type SpellAffectedTarget struct {
	Character *storage.Character
	Monster   *Monster
}

type SpellImpact struct {
	MonsterHit   *AttackResult
	CharacterHit *CharacterHit
}

type SkillCastResult struct {
	Character              storage.Character
	SkillID                string
	TargetX                int
	TargetY                int
	TargetIDResolved       bool
	StartTargetID          int32
	SpellStarted           bool
	SpellFailed            bool
	SpaceMoveFireInBranch  bool
	SpaceMoveMagicFire     bool
	SuppressMagicFire      bool
	MagicTargetID          int32
	MagicFireTargetX       int
	MagicFireTargetY       int
	MagicFireTargetSet     bool
	ManaCost               int
	ManaConsumed           bool
	CooldownMS             int
	Experience             int
	CurrentExp             int
	LevelUp                bool
	SkillChanged           bool
	SkillLevelUp           bool
	SkillTraining          bool
	ChargeActionSucceeded  bool
	SkillLevel             byte
	SkillTrain             int
	DefenceDurationSeconds int
	MonsterHit             *AttackResult
	MonsterHits            []AttackResult
	MonsterActions         []MonsterAction
	CharacterHits          []CharacterHit
	NameColorCharacters    []storage.Character
	AffectedCharacters     []storage.Character
	HealingGaugeTargets    []SpellHealingGaugeTarget
	AffectedMonsters       []Monster
	NameColorMonsters      []Monster
	NameMonsters           []Monster
	AffectedTargets        []SpellAffectedTarget
	Impacts                []SpellImpact
	SummonedMonsters       []Monster
	Events                 []SpellEvent
	Rushes                 []SpellRush
	CharacterPushes        []CharacterPush
	MonsterPushes          []MonsterAction
	PushTargets            int
	OrderedEvents          []SpellEvent
	GroundEvents           []SpellGroundEvent
}

func (w *World) SpellCost(skill data.StdSkill, state storage.SkillState) int {
	if skill.Spell <= 0 && skill.DefSpell <= 0 {
		return 0
	}
	trainLevel := spellTrainLevel
	cost := referenceRound(float64(skill.Spell)/float64(trainLevel+1)*float64(int(state.Level)+1)) + skill.DefSpell
	if cost < 0 {
		return 0
	}
	return cost
}

func (w *World) SpellTargetInRange(ch storage.Character, targetX, targetY int) bool {
	return abs(ch.X-targetX) <= defaultSpellAttackRange && abs(ch.Y-targetY) <= defaultSpellAttackRange
}

func (w *World) CastSkill(ch storage.Character, skillID string, targetX, targetY int, targetID int32) (SkillCastResult, error) {
	return w.CastSkillWithPlayers(ch, skillID, targetX, targetY, targetID, nil)
}

func (w *World) CastSkillWithPlayers(ch storage.Character, skillID string, targetX, targetY int, targetID int32, players []storage.Character) (SkillCastResult, error) {
	if skillID == "野蛮冲撞" {
		return w.DoCharge(ch, direction(ch.X, ch.Y, targetX, targetY), players)
	}
	return w.DoSpell(ch, skillID, targetX, targetY, targetID, players)
}

func (w *World) DoCharge(ch storage.Character, dir int, players []storage.Character) (result SkillCastResult, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.normalizeEquippedItemsLocked(&ch)
	state, idx, ok := ch.Skills.Get("野蛮冲撞")
	if !ok {
		return SkillCastResult{}, fmt.Errorf("skill 野蛮冲撞 not learned")
	}
	skill, ok := w.data.Skills["野蛮冲撞"]
	if !ok {
		return SkillCastResult{}, fmt.Errorf("skill 野蛮冲撞 not found")
	}
	ch.Dir = dir
	cost := w.SpellCost(skill, state)
	result = SkillCastResult{Character: ch, SkillID: "野蛮冲撞", ManaCost: cost, ManaConsumed: cost > 0, CooldownMS: skill.Delay}
	if ch.MP < cost {
		return result, nil
	}
	ch.MP -= cost
	start := ch
	if ch.EquippedItems != nil {
		start.EquippedItems = make(map[int]storage.UserItem, len(ch.EquippedItems))
		for slot, item := range ch.EquippedItems {
			start.EquippedItems[slot] = item
		}
	}
	result.Character = ch
	result.SpellStarted = true
	ch, err = w.castChargeDirectionLocked(&result, ch, state, players, dir)
	if err != nil {
		result.Character = ch
		return result, err
	}
	skillTrained := result.ChargeActionSucceeded
	if skillTrained {
		previousTrain := state.Train
		previousLevel := state.Level
		if state.Level < 3 && ch.Level > skillNeedLevel(skill, state.Level) {
			if w.applySkillTrainingLocked(ch.Level, skill, &state, magicTrainPointsForSkill(w.rand)) {
				result.SkillChanged = true
				result.SkillLevelUp = state.Level > previousLevel
			}
		}
		result.SkillTraining = state.Train != previousTrain || state.Level != previousLevel
	}
	ch.Skills[idx] = state
	result.Character = ch
	result.SkillLevel = state.Level
	result.SkillTrain = state.Train
	if err := w.store.SaveCharacter(ch); err != nil {
		return result, err
	}
	result.Events = w.spellEvents(start, result, skill, ch.X, ch.Y, 0)
	return result, nil
}

func (w *World) DoSpell(ch storage.Character, skillID string, targetX, targetY int, targetID int32, players []storage.Character) (result SkillCastResult, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if isWarriorSkill(skillID) {
		return SkillCastResult{}, fmt.Errorf("skill %s is handled by the warrior spell entry", skillID)
	}
	w.normalizeEquippedItemsLocked(&ch)
	state, idx, ok := ch.Skills.Get(skillID)
	if !ok {
		return SkillCastResult{}, fmt.Errorf("skill %s not learned", skillID)
	}
	skill, ok := w.data.Skills[skillID]
	if !ok {
		return SkillCastResult{}, fmt.Errorf("skill %s not found", skillID)
	}
	now := time.Now()
	pendingStart := len(w.pendingSpells)
	if ch.MapID == "" {
		return SkillCastResult{}, fmt.Errorf("character has no current map")
	}
	if targetX < 0 || targetY < 0 {
		return SkillCastResult{}, fmt.Errorf("invalid spell target")
	}
	targetIDResolved := false
	startTargetID := int32(0)
	if targetID != 0 {
		if target, ok := w.spellTargetNearLocked(players, ch.MapID, targetX, targetY, targetID); ok {
			targetX, targetY = target.X, target.Y
			startTargetID = targetID
			targetIDResolved = target.HP > 0
		} else if target := w.monsterTargetLocked(ch.MapID, targetX, targetY, targetID, 1); target != nil {
			targetX, targetY = target.X, target.Y
			startTargetID = targetID
			targetIDResolved = true
		} else if target := w.spellMonsterTargetNearLocked(ch.MapID, targetX, targetY, targetID, 1); target != nil {
			targetX, targetY = target.X, target.Y
			startTargetID = targetID
		}
		if !targetIDResolved {
			for _, target := range players {
				if target.HP > 0 && target.MapID == ch.MapID && CharacterActorID(target) == targetID && abs(target.X-targetX) <= 1 && abs(target.Y-targetY) <= 1 {
					targetX, targetY = target.X, target.Y
					targetIDResolved = true
					break
				}
			}
		}
		if !targetIDResolved {
			for _, target := range w.monsters {
				if target != nil && target.Alive && target.MapID == ch.MapID && MonsterActorID(*target) == targetID && abs(target.X-targetX) <= 1 && abs(target.Y-targetY) <= 1 {
					targetX, targetY = target.X, target.Y
					targetIDResolved = true
					break
				}
			}
		}
	}
	if targetID != 0 {
		ch.Dir = Direction(ch.X, ch.Y, targetX, targetY)
	}
	cost := w.SpellCost(skill, state)
	if ch.MP < cost {
		return SkillCastResult{Character: ch, SkillID: skillID, ManaCost: cost, CooldownMS: skill.Delay}, fmt.Errorf("not enough mp")
	}
	ch.MP -= cost
	start := ch
	if ch.EquippedItems != nil {
		start.EquippedItems = make(map[int]storage.UserItem, len(ch.EquippedItems))
		for slot, item := range ch.EquippedItems {
			start.EquippedItems[slot] = item
		}
	}
	result = SkillCastResult{
		Character:        ch,
		SkillID:          skillID,
		TargetX:          targetX,
		TargetY:          targetY,
		TargetIDResolved: targetIDResolved,
		StartTargetID:    startTargetID,
		SpellStarted:     false,
		ManaCost:         cost,
		ManaConsumed:     cost > 0,
		CooldownMS:       skill.Delay,
	}
	if !w.SpellTargetInRange(ch, targetX, targetY) {
		if err := w.store.SaveCharacter(result.Character); err != nil {
			return SkillCastResult{}, err
		}
		return result, fmt.Errorf("spell target out of range")
	}
	spellStarted := false
	defer func() {
		if err != nil && spellStarted {
			w.pendingSpells = w.pendingSpells[:pendingStart]
			result.Character = ch
			result.SkillID = skillID
			result.TargetX = targetX
			result.TargetY = targetY
			result.TargetIDResolved = targetIDResolved
			result.StartTargetID = startTargetID
			result.SpellStarted = true
			result.ManaCost = cost
			result.ManaConsumed = cost > 0
			result.CooldownMS = skill.Delay
			if saveErr := w.store.SaveCharacter(ch); saveErr != nil {
				result = SkillCastResult{}
				err = saveErr
				return
			}
			result.Events = w.spellFailureEvents(start, result, skill, targetX, targetY, targetID)
		}
	}()
	result.SpellStarted = true
	spellStarted = true
	magicID, _ := w.MagicIDByName(skillID)
	if ch.SoftVersionDate == 0 && ch.ClientTick == 0 && magicID > 40 {
		result.SpellFailed = true
		return result, fmt.Errorf("skill %s is unsupported by legacy client", skillID)
	}
	skillTrained := false
	poisonApplied := false
	fireWallCreated := 0

	switch skillID {
	case "火球术", "大火球":
		mon := w.explicitMonsterTargetLocked(ch.MapID, targetX, targetY, targetID, 1)
		if mon != nil {
			if w.magCanHitTargetLocked(ch.MapID, ch.X, ch.Y, mon.X, mon.Y) && w.isProperMonsterTargetLocked(ch, players, mon) && w.monsterMagicHitAllowedLocked(mon) {
				damage := w.spellMonsterDamageLocked(ch, skill, state)
				w.pendingSpells = append(w.pendingSpells, pendingSpell{DueAt: now.Add(spellDelayMagic), CasterID: ch.ID, TargetMonsterID: mon.ID, TargetX: targetX, TargetY: targetY, Damage: damage, SetCasterTarget: true})
				result.MagicTargetID = MonsterActorID(*mon)
				skillTrained = mon.Race >= 50
			}
			result.Character = ch
			break
		}
		target, ok := w.explicitCharacterTargetLocked(players, ch.MapID, targetX, targetY, targetID)
		if !ok || !w.magCanHitTargetLocked(ch.MapID, ch.X, ch.Y, target.X, target.Y) || !w.isProperCharacterTargetLocked(ch, target) || !w.characterMagicHitAllowedLocked(target) {
			result.Character = ch
			break
		}
		damage := w.spellDamageLocked(ch, skill, state)
		w.pendingSpells = append(w.pendingSpells, pendingSpell{DueAt: now.Add(spellDelayMagic), CasterID: ch.ID, TargetCharacterID: target.ID, CharacterDamage: true, TargetX: targetX, TargetY: targetY, Damage: damage, SetCasterTarget: true})
		result.MagicTargetID = CharacterActorID(target)
	case "冰咆哮":
		var validTarget bool
		ch, validTarget, err = w.castExplosionSkillLocked(&result, ch, skill, state, targetX, targetY, players)
		if err != nil {
			return SkillCastResult{}, err
		}
		skillTrained = validTarget
	case "治愈术":
		var heal int
		target, ok := w.explicitCharacterTargetLocked(players, ch.MapID, targetX, targetY, targetID)
		if !ok {
			if mon := w.explicitMonsterTargetLocked(ch.MapID, targetX, targetY, targetID, 1); mon != nil {
				result.Character = ch
				result.MagicTargetID = MonsterActorID(*mon)
				if w.isFriendlySummonedMonsterLocked(ch, players, mon) {
					heal = w.spellHealAmountLocked(ch, skill, state)
					if mon.HP < mon.MaxHP {
						w.pendingSpells = append(w.pendingSpells, pendingSpell{DueAt: now.Add(800 * time.Millisecond), CasterID: ch.ID, TargetMonsterID: mon.ID, TargetX: targetX, TargetY: targetY, Healing: heal})
						skillTrained = true
					}
					if w.canSeeHealGaugeLocked(ch) {
						copy := *mon
						result.HealingGaugeTargets = []SpellHealingGaugeTarget{{Monster: &copy}}
					}
				}
				break
			}
			target = ch
			targetX, targetY = ch.X, ch.Y
			result.TargetX = targetX
			result.TargetY = targetY
			result.MagicFireTargetX = targetX
			result.MagicFireTargetY = targetY
			result.MagicFireTargetSet = true
		} else if target.ID != ch.ID && !w.isProperFriendLocked(ch, target) {
			result.MagicTargetID = CharacterActorID(target)
			result.Character = ch
			break
		}
		heal = w.spellHealAmountLocked(ch, skill, state)
		if target.HP < target.MaxHP {
			w.pendingSpells = append(w.pendingSpells, pendingSpell{DueAt: now.Add(800 * time.Millisecond), CasterID: ch.ID, TargetCharacterID: target.ID, TargetX: targetX, TargetY: targetY, Healing: heal})
			skillTrained = true
		}
		result.MagicTargetID = CharacterActorID(target)
		if w.canSeeHealGaugeLocked(ch) && target.ID != "" {
			result.HealingGaugeTargets = []SpellHealingGaugeTarget{{Character: &target}}
		}
	case "群体治疗术":
		heal := w.spellHealAmountLocked(ch, skill, state)
		changed, err := w.queueGroupHealingLocked(ch, players, targetX, targetY, heal, now)
		if err != nil {
			return result, err
		}
		if w.canSeeHealGaugeLocked(ch) {
			result.HealingGaugeTargets = w.healingGaugeTargetsLocked(ch, players, targetX, targetY)
		}
		skillTrained = changed
	case "神圣战甲术":
		if !w.consumeMagicAmuletLocked(&ch, 1) {
			return result, fmt.Errorf("no magic amulet")
		}
		duration := w.groupDefenceDurationLocked(ch, skill, state)
		affected, affectedMonsters, validTargets, err := w.groupDefenceCharactersLocked(ch, skill, state, players, targetX, targetY, false, duration, now)
		if err != nil {
			return result, err
		}
		result.AffectedCharacters = affected
		result.AffectedMonsters = affectedMonsters
		result.DefenceDurationSeconds = int(duration / time.Second)
		result.AffectedTargets = orderSpellAffectedTargets(affected, affectedMonsters)
		for _, target := range affected {
			if target.ID == ch.ID {
				ch = target
				result.Character = target
				break
			}
		}
		skillTrained = validTargets
	case "幽灵盾":
		if !w.consumeMagicAmuletLocked(&ch, 1) {
			return result, fmt.Errorf("no magic amulet")
		}
		duration := w.groupDefenceDurationLocked(ch, skill, state)
		affected, affectedMonsters, validTargets, err := w.groupDefenceCharactersLocked(ch, skill, state, players, targetX, targetY, true, duration, now)
		if err != nil {
			return result, err
		}
		result.AffectedCharacters = affected
		result.AffectedMonsters = affectedMonsters
		result.DefenceDurationSeconds = int(duration / time.Second)
		result.AffectedTargets = orderSpellAffectedTargets(affected, affectedMonsters)
		for _, target := range affected {
			if target.ID == ch.ID {
				ch = target
				result.Character = target
				break
			}
		}
		skillTrained = validTargets
	case "魔法盾":
		if ch.BubbleDefenceUntil > 0 {
			result.Character = ch
			break
		}
		duration := w.magicShieldDurationLocked(ch, skill, state)
		if duration < time.Second {
			duration = time.Second
		}
		ch.BubbleDefenceUntil = now.Add(duration).UnixNano()
		ch.BubbleDefenceLevel = state.Level
		result.Character = ch
		skillTrained = true
	case "圣言术":
		mon := w.explicitMonsterTargetLocked(ch.MapID, targetX, targetY, targetID, 1)
		if mon == nil || !w.isProperMonsterTargetLocked(ch, players, mon) {
			result.Character = ch
			break
		}
		result.MagicTargetID = MonsterActorID(*mon)
		if mon.Undead <= 0 {
			result.Character = ch
			break
		}
		w.monsterStruckByCharacterLocked(mon, ch, players, now)
		ch.TargetID = mon.ID
		if mon.TargetCharacterID == "" {
			mon.RunAwayMode = true
			mon.RunAwayUntil = now.Add(10 * time.Second)
			mon.TargetX = -1
			mon.TargetY = -1
		}
		casterLevel := ch.Level
		if w.rand.Intn(2)+(casterLevel-1) <= mon.Level {
			result.Character = ch
			break
		}
		if mon.Level >= defaultTurnUndeadLevel {
			result.Character = ch
			break
		}
		chance := (int(state.Level) * 8) - int(state.Level) + 15 + (casterLevel - mon.Level)
		if chance < 0 {
			chance = 0
		}
		if w.rand.Intn(100) < chance {
			w.setMonsterLastHitterLocked(mon, ch.ID)
			mon.HP = 0
			mon.PendingDeath = true
			mon.DeathHitterID = ch.ID
			result.Character = ch
			skillTrained = true
		} else {
			result.Character = ch
		}
	case "隐身术":
		if !w.consumeMagicAmuletLocked(&ch, 1) {
			return result, fmt.Errorf("no magic amulet")
		}
		duration := w.stealthDurationLocked(ch, skill, state, 30)
		if ch.TransparentUntil > 0 {
			result.Character = ch
			break
		}
		setCharacterTransparentLocked(&ch, now.Add(duration))
		w.breakNearbyMonsterTargetsForStealthLocked(ch)
		result.Character = ch
		skillTrained = true
	case "集体隐身术":
		if !w.consumeMagicAmuletLocked(&ch, 1) {
			return result, fmt.Errorf("no magic amulet")
		}
		duration := w.stealthDurationLocked(ch, skill, state, 30)
		affected := w.stealthAffectedTargetsLocked(ch, playersWithCaster(players, ch), targetX, targetY)
		groupStealthApplied := false
		for _, areaTarget := range affected {
			if areaTarget.Character != nil {
				target := *areaTarget.Character
				if target.TransparentUntil > 0 {
					continue
				}
				w.pendingSpells = append(w.pendingSpells, pendingSpell{DueAt: now.Add(800 * time.Millisecond), CasterID: ch.ID, TargetCharacterID: target.ID, TargetX: target.X, TargetY: target.Y, TransparentDuration: duration})
				groupStealthApplied = true
				continue
			}
			if areaTarget.Monster != nil {
				mon := areaTarget.Monster
				if !mon.TransparentUntil.IsZero() {
					continue
				}
				w.pendingSpells = append(w.pendingSpells, pendingSpell{DueAt: now.Add(800 * time.Millisecond), CasterID: ch.ID, TargetMonsterID: mon.ID, TargetX: mon.X, TargetY: mon.Y, TransparentDuration: duration})
				groupStealthApplied = true
			}
		}
		result.Character = ch
		skillTrained = groupStealthApplied
	case "瞬息移动":
		result.SpaceMoveMagicFire = true
		if w.rand.Intn(11) >= int(state.Level)*2+4 {
			result.Character = ch
			break
		}
		result.SpaceMoveFireInBranch = true
		next, err := w.homeTeleportRandomCharacterLocked(ch)
		if err != nil {
			return SkillCastResult{}, err
		}
		ch = next
		result.Character = ch
		skillTrained = true
	case "心灵启示":
		target, ok := w.explicitCharacterTargetLocked(players, ch.MapID, targetX, targetY, targetID)
		if !ok {
			if mon := w.explicitMonsterTargetLocked(ch.MapID, targetX, targetY, targetID, 1); mon != nil {
				result.MagicTargetID = MonsterActorID(*mon)
				if mon.ShowHPUntil > 0 {
					result.Character = ch
					break
				}
				if w.rand.Intn(6) > int(state.Level)+3 {
					result.Character = ch
					break
				}
				duration := w.showHealthDurationLocked(ch, skill, state)
				w.pendingSpells = append(w.pendingSpells, pendingSpell{DueAt: now.Add(1500 * time.Millisecond), CasterID: ch.ID, TargetMonsterID: mon.ID, ShowHealthDuration: duration, ShowHealthStartedAt: now})
				skillTrained = true
				result.Character = ch
				break
			}
			result.Character = ch
			break
		}
		result.MagicTargetID = CharacterActorID(target)
		if target.ShowHPUntil > 0 {
			result.Character = ch
			break
		}
		if w.rand.Intn(6) > int(state.Level)+3 {
			result.Character = ch
			break
		}
		duration := w.showHealthDurationLocked(ch, skill, state)
		w.pendingSpells = append(w.pendingSpells, pendingSpell{DueAt: now.Add(1500 * time.Millisecond), CasterID: ch.ID, TargetCharacterID: target.ID, ShowHealthDuration: duration, ShowHealthStartedAt: now})
		skillTrained = true
	case "施毒术":
		mon := w.explicitMonsterTargetLocked(ch.MapID, targetX, targetY, targetID, 1)
		if mon != nil && !w.isProperMonsterTargetLocked(ch, players, mon) {
			return result, fmt.Errorf("no valid poison target")
		}
		validMonster := w.isProperMonsterTargetLocked(ch, players, mon)
		target, validCharacter := w.explicitCharacterTargetLocked(players, ch.MapID, targetX, targetY, targetID)
		if !validMonster && (!validCharacter || !w.isProperCharacterTargetLocked(ch, target)) {
			return result, fmt.Errorf("no valid poison target")
		}
		poisonItem, ok := w.consumePoisonPowderLocked(&ch)
		if !ok {
			return result, fmt.Errorf("no valid poison target")
		}
		poisonStd, ok := w.data.Items[poisonItem.ItemID]
		if !ok {
			return result, fmt.Errorf("item %s not found", poisonItem.ItemID)
		}
		applyHealthPoison := poisonStd.Shape == 1
		applyArmorPoison := poisonStd.Shape == 2
		if !applyHealthPoison && !applyArmorPoison {
			return result, fmt.Errorf("no valid poison target")
		}
		preparePoison := func() (time.Duration, byte, int) {
			basePower := poisonBaseHealthPowerGrey
			if applyArmorPoison {
				basePower = poisonBaseHealthPowerYellow
			}
			poisonPower := w.poisonSpellPowerLocked(ch, skill, state, basePower)
			duration := poisonEffectDuration(poisonPower)
			poisonLevel := poisonLevelFromPower(referenceRound(float64(int(state.Level)) / 3.0 * float64(poisonPower) / 10.0))
			if !applyHealthPoison {
				poisonLevel = 0
			}
			poisonPoint := referenceRound(float64(int(state.Level)) / 3.0 * float64(poisonPower) / 10.0)
			return duration, poisonLevel, poisonPoint
		}
		if validMonster {
			result.MagicTargetID = MonsterActorID(*mon)
			poisonAccepted := poisonChanceOK(w.rand, mon.AntiPoison)
			if !poisonAccepted {
				ch.TargetID = mon.ID
				result.Character = ch
				break
			}
			duration, poisonHealthLevel, poisonPoint := preparePoison()
			w.pendingSpells = append(w.pendingSpells, pendingSpell{
				DueAt: now.Add(time.Second), CasterID: ch.ID, TargetMonsterID: mon.ID,
				TargetX: targetX, TargetY: targetY, PoisonHealthLevel: poisonHealthLevel,
				PoisonPoint: poisonPoint, PoisonHealth: applyHealthPoison, PoisonArmor: applyArmorPoison, PoisonDuration: duration,
			})
			ch.TargetID = mon.ID
			result.Character = ch
			poisonApplied = true
			skillTrained = true
			break
		}
		if !validCharacter || !w.isProperCharacterTargetLocked(ch, target) {
			return result, fmt.Errorf("no valid poison target")
		}
		result.MagicTargetID = CharacterActorID(target)
		poisonAccepted := poisonChanceOK(w.rand, w.poisonAvoidanceLocked(target)+target.AntiPoison)
		if !poisonAccepted {
			ch.TargetID = target.ID
			result.Character = ch
			break
		}
		duration, poisonHealthLevel, poisonPoint := preparePoison()
		w.pendingSpells = append(w.pendingSpells, pendingSpell{
			DueAt: now.Add(time.Second), CasterID: ch.ID, TargetCharacterID: target.ID,
			TargetX: targetX, TargetY: targetY, PoisonHealthLevel: poisonHealthLevel,
			PoisonPoint: poisonPoint, PoisonHealth: applyHealthPoison, PoisonArmor: applyArmorPoison, PoisonDuration: duration, PoisonNotification: true,
		})
		ch.TargetID = target.ID
		result.Character = ch
		poisonApplied = true
		skillTrained = true
	case "灵魂火符":
		if !w.consumeMagicAmuletLocked(&ch, 1) {
			return result, fmt.Errorf("no magic amulet")
		}
		if mon := w.explicitMonsterTargetLocked(ch.MapID, targetX, targetY, targetID, 1); mon != nil {
			if !w.magCanHitTargetLocked(ch.MapID, ch.X, ch.Y, mon.X, mon.Y) || !w.isProperMonsterTargetLocked(ch, players, mon) || !w.monsterMagicHitAllowedLocked(mon) {
				result.Character = ch
				break
			}
			damage := w.spellSpiritDamageLocked(ch, skill, state)
			w.pendingSpells = append(w.pendingSpells, pendingSpell{DueAt: now.Add(1200 * time.Millisecond), CasterID: ch.ID, TargetMonsterID: mon.ID, TargetX: targetX, TargetY: targetY, Damage: damage, SetCasterTarget: true})
			result.MagicTargetID = MonsterActorID(*mon)
			skillTrained = mon.Race >= 50
			break
		}
		target, ok := w.explicitCharacterTargetLocked(players, ch.MapID, targetX, targetY, targetID)
		if !ok || !w.magCanHitTargetLocked(ch.MapID, ch.X, ch.Y, target.X, target.Y) || !w.isProperCharacterTargetLocked(ch, target) || !w.characterMagicHitAllowedLocked(target) {
			result.Character = ch
			break
		}
		result.Character = ch
		w.pendingSpells = append(w.pendingSpells, pendingSpell{DueAt: now.Add(1200 * time.Millisecond), CasterID: ch.ID, TargetCharacterID: target.ID, CharacterDamage: true, TargetX: targetX, TargetY: targetY, Damage: w.spellSpiritDamageLocked(ch, skill, state), SetCasterTarget: true})
		result.MagicTargetID = CharacterActorID(target)
	case "雷电术":
		if mon := w.explicitMonsterTargetLocked(ch.MapID, targetX, targetY, targetID, 1); mon != nil {
			if !w.isProperMonsterTargetLocked(ch, players, mon) || !w.monsterMagicHitAllowedLocked(mon) {
				result.Character = ch
				break
			}
			damage := w.spellMonsterDamageLocked(ch, skill, state)
			if mon.Undead > 0 {
				damage = referenceRound(float64(damage) * 1.5)
			}
			w.pendingSpells = append(w.pendingSpells, pendingSpell{DueAt: now.Add(spellDelayMagic), CasterID: ch.ID, TargetMonsterID: mon.ID, TargetX: targetX, TargetY: targetY, Damage: damage, SetCasterTarget: true})
			result.MagicTargetID = MonsterActorID(*mon)
			skillTrained = mon.Race >= 50
			break
		}
		target, ok := w.explicitCharacterTargetLocked(players, ch.MapID, targetX, targetY, targetID)
		if !ok || !w.isProperCharacterTargetLocked(ch, target) || !w.characterMagicHitAllowedLocked(target) {
			result.Character = ch
			break
		}
		result.Character = ch
		w.pendingSpells = append(w.pendingSpells, pendingSpell{DueAt: now.Add(spellDelayMagic), CasterID: ch.ID, TargetCharacterID: target.ID, CharacterDamage: true, TargetX: targetX, TargetY: targetY, Damage: w.spellDamageLocked(ch, skill, state), SetCasterTarget: true})
		result.MagicTargetID = CharacterActorID(target)
	case "疾光电影":
		var hit bool
		lineTargetID := int32(0)
		if targetIDResolved {
			lineTargetID = targetID
		}
		ch, hit, err = w.castLightningLineSkillLocked(&result, ch, skill, state, players, int32(targetX), int32(targetY), lineTargetID, skillLightningRange, true, now)
		if err != nil {
			return SkillCastResult{}, err
		}
		skillTrained = hit
	case "爆裂火焰":
		var validTarget bool
		ch, validTarget, err = w.castExplosionSkillLocked(&result, ch, skill, state, targetX, targetY, players)
		if err != nil {
			return SkillCastResult{}, err
		}
		skillTrained = validTarget
	case "地狱雷光":
		var validTarget bool
		ch, validTarget, err = w.castElectricBlizzardSkillLocked(&result, ch, skill, state, players)
		if err != nil {
			return SkillCastResult{}, err
		}
		skillTrained = validTarget
	case "抗拒火环":
		ch, err = w.castPushAroundSkillLocked(&result, ch, state, players)
		if err != nil {
			return SkillCastResult{}, err
		}
		skillTrained = result.PushTargets > 0
	case "火墙":
		fireWallCreated, result.GroundEvents = w.castFireWallWithEventsLocked(ch, skill, state, targetX, targetY, now)
		result.Character = ch
		skillTrained = fireWallCreated > 0
	case "召唤骷髅", "召唤神兽":
		templateID := "骷髅"
		amuletCost := uint16(1)
		if skillID == "召唤神兽" {
			templateID = "神兽"
			amuletCost = 5
		}
		if !w.consumeMagicAmuletLocked(&ch, amuletCost) {
			result.Character = ch
			result.SpellFailed = true
			break
		}
		if existing := w.activeSummonedMonsterByTemplateLocked(ch.ID, templateID, now); existing != nil {
			if skillID == "召唤神兽" && w.recallSummonedMonsterNearCharacterLocked(existing, ch, players) {
				result.AffectedMonsters = []Monster{*existing}
			}
			result.Character = ch
			skillTrained = false
			break
		}
		if w.countActiveSummonedMonstersLocked(ch.ID, templateID, now) >= defaultSummonCount {
			result.Character = ch
			break
		}
		summoned, err := w.summonMonsterNearCharacterLocked(ch, players, templateID, 10*24*time.Hour, state.Level)
		if err != nil {
			result.Character = ch
			break
		}
		if summoned != nil {
			result.SummonedMonsters = []Monster{*summoned}
		}
		result.Character = ch
		skillTrained = len(result.SummonedMonsters) > 0
	case "诱惑之光":
		target := w.explicitMonsterTargetLocked(ch.MapID, targetX, targetY, targetID, 1)
		if !w.isProperMonsterTargetLocked(ch, players, target) {
			result.Character = ch
			break
		}
		result.MagicTargetID = MonsterActorID(*target)
		magicLevel := int(state.Level)
		if magicLevel < 0 {
			magicLevel = 0
		}
		tamingGate := magicLevel >= 4
		if !tamingGate {
			tamingGate = w.rand.Intn(4-magicLevel) == 0
		}
		if tamingGate {
			target.TargetCharacterID = ""
			target.TargetFocusAt = time.Time{}
			if w.rand.Intn(2) == 0 {
				if target.Level <= ch.Level+2 {
					hpGate := target.MaxHP / defaultTamingHPRate
					if hpGate <= 2 {
						hpGate = 2
					} else {
						hpGate *= 2
					}
					if w.rand.Intn(21)+ch.Level+magicLevel*5 > target.Level+10 {
						eligible := !target.NoTame && target.Undead <= 0 && target.Level <= 50 && w.countActiveSummonedMonstersLocked(ch.ID, "", now) < defaultTamingCount
						if eligible {
							if target.MasterID != ch.ID && w.rand.Intn(hpGate) == 0 {
								if target.MasterID != "" {
									target.HP /= 10
								}
								level := maxInt(ch.Level, 0)
								duration := time.Duration(w.rand.Intn(level+1)+60*(level/10)+int(state.Level)*20) * time.Minute
								target.HolySeizeUntil = time.Time{}
								target.CrazyUntil = time.Time{}
								target.NextSearchAt = now
								target.RunAwayMode = false
								target.RunAwayUntil = time.Time{}
								target.MasterID = ch.ID
								target.MasterName = ch.Name
								target.MasterExpiresAt = now.Add(duration)
								target.SlaveMakeLevel = byte(magicLevel)
								if target.MasterTick.IsZero() {
									target.MasterTick = now
								}
								target.NoTame = true
								walkLimit := 1500 - magicLevel*200
								if walkLimit > 0 && target.WalkSpeedMS > walkLimit {
									target.WalkSpeedMS = walkLimit
								}
								attackLimit := 2000 - magicLevel*200
								if attackLimit > 0 && target.AttackIntervalMS > attackLimit {
									target.AttackIntervalMS = attackLimit
								}
								result.AffectedMonsters = []Monster{*target}
								result.NameMonsters = []Monster{*target}
							} else if w.rand.Intn(14) == 0 {
								target.HP = 0
								target.PendingDeath = true
							}
						} else if !eligible && target.Undead > 0 && w.rand.Intn(20) == 0 {
							target.HP = 0
							target.PendingDeath = true
						} else if !eligible && target.Undead <= 0 && w.rand.Intn(20) == 0 {
							target.CrazyUntil = now.Add(time.Duration(w.rand.Intn(20)+10) * time.Second)
							result.NameColorMonsters = []Monster{*target}
						}
					} else if target.Undead <= 0 {
						target.CrazyUntil = now.Add(time.Duration(w.rand.Intn(20)+10) * time.Second)
						result.NameColorMonsters = []Monster{*target}
					}
				} else {
					target.HolySeizeUntil = now.Add(time.Duration(w.rand.Intn(magicLevel*5+10)) * time.Second)
					result.NameColorMonsters = []Monster{*target}
				}
			} else {
				if target.Undead <= 0 {
					target.CrazyUntil = now.Add(time.Duration(w.rand.Intn(20)+10) * time.Second)
					result.NameColorMonsters = []Monster{*target}
				}
			}
			skillTrained = true
		} else if magicLevel < 4 && w.rand.Intn(2) == 0 {
			skillTrained = true
		}
		result.Character = ch
	case "地狱火":
		var hit bool
		lineTargetID := int32(0)
		if targetIDResolved {
			lineTargetID = targetID
		}
		ch, hit, err = w.castLightningLineSkillLocked(&result, ch, skill, state, players, int32(targetX), int32(targetY), lineTargetID, 5, false, now)
		if err != nil {
			return result, err
		}
		skillTrained = hit
	case "困魔咒":
		players = playersWithCaster(players, ch)
		mon := w.explicitMonsterTargetLocked(ch.MapID, targetX, targetY, targetID, 1)
		if mon != nil && !w.magCanHitTargetLocked(ch.MapID, ch.X, ch.Y, mon.X, mon.Y) {
			result.Character = ch
			break
		}
		if mon != nil && !w.isProperMonsterTargetLocked(ch, players, mon) {
			result.Character = ch
			break
		}
		target := storage.Character{}
		targetOK := mon == nil
		if mon == nil {
			target, targetOK = w.explicitCharacterTargetLocked(players, ch.MapID, targetX, targetY, targetID)
		}
		if mon == nil && (!targetOK || !w.isProperCharacterTargetLocked(ch, target)) {
			result.Character = ch
			break
		}
		if mon != nil {
			if !w.monsterMagicHitAllowedLocked(mon) || abs(mon.X-targetX) > 1 || abs(mon.Y-targetY) > 1 {
				result.Character = ch
				break
			}
		} else if !w.magCanHitTargetLocked(ch.MapID, ch.X, ch.Y, target.X, target.Y) || !w.characterMagicHitAllowedLocked(target) || abs(target.X-targetX) > 1 || abs(target.Y-targetY) > 1 {
			result.Character = ch
			break
		}
		power := w.spellMabePowerLocked(ch, skill, state)
		first := pendingSpell{DueAt: now.Add(spellDelayMagic), CasterID: ch.ID, TargetX: targetX, TargetY: targetY, Damage: power / 3, SetCasterTarget: true}
		if mon != nil {
			first.TargetMonsterID = mon.ID
			result.MagicTargetID = MonsterActorID(*mon)
		} else {
			first.TargetCharacterID = target.ID
			first.CharacterDamage = true
			result.MagicTargetID = CharacterActorID(target)
		}
		w.pendingSpells = append(w.pendingSpells, first)
		level := targetLevel(mon, target)
		if w.rand.Intn(2)+ch.Level-1 > level && w.rand.Intn(100) < maxInt(10, int(state.Level)*7+15+ch.Level-level) && w.rand.Intn(21) < int(state.Level)*2+4 {
			second := first
			if mon == nil {
				target.LastHitterID = ch.ID
				target.LastHitterAt = now.UnixNano()
				for i := range players {
					if players[i].ID == target.ID {
						players[i] = target
						break
					}
				}
				if err := w.store.SaveCharacter(target); err != nil {
					return result, err
				}
				ch.PKFlag = true
				ch.PKFlagUntil = now.Add(60 * time.Second).UnixNano()
				ch.TargetID = target.ID
			} else {
				w.setMonsterLastHitterLocked(mon, ch.ID)
			}
			if mon != nil {
				second.Damage = w.monsterMagicDamageAfterDefenseLocked(mon, power)
				if mon.Undead > 0 && second.Damage > 0 {
					second.Damage += w.combatStatsLocked(ch).Undead
				}
			} else {
				bubbleBefore := target.BubbleDefenceUntil
				target, second.Damage = w.prepareCharacterMagicDamageLocked(target, power, now)
				second.CharacterBubbleBefore = bubbleBefore
				second.CharacterBubbleAfter = target.BubbleDefenceUntil
				second.CharacterBubbleLevel = target.BubbleDefenceLevel
			}
			paralysisSeconds := second.Damage / 20
			if state.Level > 0 {
				paralysisSeconds += w.rand.Intn(int(state.Level))
			}
			w.pendingSpells = append(w.pendingSpells, second)
			if mon != nil || !w.characterCannotParalyzeLocked(target) {
				control := pendingSpell{
					DueAt: now.Add(650 * time.Millisecond), CasterID: ch.ID,
					ParalysisDuration:  time.Duration(paralysisSeconds) * time.Second,
					PoisonNotification: target.ID != "" && mon == nil, PoisonPoint: int(state.Level),
				}
				if mon != nil {
					control.TargetMonsterID = mon.ID
				} else {
					control.TargetCharacterID = target.ID
				}
				w.pendingSpells = append(w.pendingSpells, control)
			}
			skillTrained = true
		}
		result.Character = ch
	default:
		return result, fmt.Errorf("skill %s effect not implemented", skillID)
	}
	if skillID == "施毒术" {
		skillTrained = poisonApplied
	}
	if result.SpellFailed {
		result.Character = ch
		result.Events = w.spellFailureEvents(start, result, skill, result.TargetX, result.TargetY, targetID)
		return result, fmt.Errorf("spell failed")
	}
	if skillTrained {
		previousTrain := state.Train
		previousLevel := state.Level
		if state.Level < 3 && ch.Level >= skillNeedLevel(skill, state.Level) {
			points := magicTrainPointsForSkill(w.rand)
			if w.applySkillTrainingLocked(ch.Level, skill, &state, points) {
				result.SkillChanged = true
				result.SkillLevelUp = state.Level > previousLevel
			}
		}
		result.SkillTraining = state.Train != previousTrain || state.Level != previousLevel
	}
	if stored, ok := w.store.Character(ch.ID); ok && !start.PKFlag && stored.PKFlag {
		ch.PKFlag = stored.PKFlag
		ch.PKFlagUntil = stored.PKFlagUntil
		result.NameColorCharacters = append(result.NameColorCharacters, stored)
	}
	ch.Skills[idx] = state
	result.Character = ch
	result.SkillLevel = state.Level
	result.SkillTrain = state.Train
	if result.Character.ID != "" {
		if err := w.store.SaveCharacter(result.Character); err != nil {
			return SkillCastResult{}, err
		}
	}
	result.Events = w.spellEvents(start, result, skill, result.TargetX, result.TargetY, targetID)
	return result, nil
}

func (w *World) spellEvents(previous storage.Character, result SkillCastResult, skill data.StdSkill, targetX, targetY int, targetID int32) []SpellEvent {
	magicID, _ := w.MagicIDByName(result.SkillID)
	casterState := previous
	casterState.MP = result.Character.MP
	startTargetID := result.StartTargetID
	if startTargetID == 0 && (targetID == 0 || result.TargetIDResolved) {
		startTargetID = targetID
	}
	magicTargetID := int32(0)
	if result.MagicTargetID != 0 {
		magicTargetID = result.MagicTargetID
	} else if result.TargetIDResolved {
		magicTargetID = targetID
	}
	if magicTargetID == 0 && result.SkillID == "治愈术" {
		magicTargetID = CharacterActorID(result.Character)
	}
	events := make([]SpellEvent, 0, 16)
	events = append(events, SpellEvent{Kind: SpellEventCasterState, Character: casterState, Previous: previous, SendHealth: result.ManaConsumed})
	if result.SkillID != "野蛮冲撞" {
		events = append(events, SpellEvent{Kind: SpellEventStart, Caster: previous, MagicID: magicID, Effect: skill.Effect, TargetX: targetX, TargetY: targetY, TargetID: startTargetID})
	}
	appendEquipmentEvents := func() {
		slots := make([]int, 0, len(previous.EquippedItems))
		for slot := range previous.EquippedItems {
			slots = append(slots, slot)
		}
		sort.Ints(slots)
		for _, slot := range slots {
			before := previous.EquippedItems[slot]
			after, ok := result.Character.EquippedItems[slot]
			if !ok {
				events = append(events, SpellEvent{Kind: SpellEventItemDelete, Character: result.Character, DeletedItem: before})
				continue
			}
			if before.Dura != after.Dura || before.DuraMax != after.DuraMax {
				events = append(events, SpellEvent{Kind: SpellEventDurability, Character: result.Character, Durability: SpellDurability{Slot: slot, Dura: after.Dura, DuraMax: after.DuraMax}})
			}
		}
	}
	appendEquipmentEvents()
	appendMagicFire := func(caster storage.Character) {
		fireX, fireY := targetX, targetY
		if result.MagicFireTargetSet {
			fireX, fireY = result.MagicFireTargetX, result.MagicFireTargetY
		}
		events = append(events, SpellEvent{Kind: SpellEventMagicFire, Caster: caster, MagicID: magicID, Effect: skill.Effect, TargetX: fireX, TargetY: fireY, TargetID: magicTargetID})
	}
	if result.SpaceMoveFireInBranch {
		appendMagicFire(previous)
		events = append(events, SpellEvent{Kind: SpellEventSpaceMoveFire, Caster: previous})
	}
	if len(result.OrderedEvents) > 0 {
		events = append(events, result.OrderedEvents...)
	} else {
		for _, rush := range result.Rushes {
			events = append(events, SpellEvent{Kind: SpellEventRush, Rush: rush})
		}
	}
	teleported := previous.MapID != result.Character.MapID || previous.X != result.Character.X || previous.Y != result.Character.Y
	if result.Character.ID != "" && result.SkillID != "野蛮冲撞" && !result.SpaceMoveFireInBranch {
		events = append(events, SpellEvent{Kind: SpellEventCharacter, Character: result.Character, Previous: previous, Teleport: teleported && result.SpaceMoveFireInBranch, SuppressStateBroadcast: len(result.Rushes) > 0, SuppressStatusBroadcast: result.SkillID == "神圣战甲术" || result.SkillID == "幽灵盾"})
	}
	if teleported && len(result.Rushes) == 0 {
		if result.SpaceMoveFireInBranch {
			events = append(events, SpellEvent{Kind: SpellEventSpaceMoveMapChange, Character: result.Character})
		} else {
			events = append(events, SpellEvent{Kind: SpellEventTeleport, Character: result.Character, Previous: previous})
		}
		if result.SpaceMoveFireInBranch {
			events = append(events, SpellEvent{Kind: SpellEventSpaceMoveShow, Character: result.Character})
		}
	}
	pushIndexes := make(map[string]int)
	monsterPushIndexes := make(map[string]int)
	appendAffectedCharacter := func(ch storage.Character) {
		pushIndex := pushIndexes[ch.ID]
		for pushIndex < len(result.CharacterPushes) && result.CharacterPushes[pushIndex].Character.ID != ch.ID {
			pushIndex++
		}
		if pushIndex < len(result.CharacterPushes) {
			for pushIndex < len(result.CharacterPushes) && result.CharacterPushes[pushIndex].Character.ID == ch.ID {
				events = append(events, SpellEvent{Kind: SpellEventCharacterPush, CharacterPush: result.CharacterPushes[pushIndex]})
				pushIndex++
			}
			pushIndexes[ch.ID] = pushIndex
			return
		}
		events = append(events, SpellEvent{
			Kind:                    SpellEventAffectedCharacter,
			Character:               ch,
			SendHealth:              result.SkillID != "治愈术" && result.SkillID != "群体治疗术" && result.SkillID != "心灵启示" && result.SkillID != "神圣战甲术" && result.SkillID != "幽灵盾" && ch.ID != result.Character.ID,
			SendAbility:             result.SkillID == "神圣战甲术" || result.SkillID == "幽灵盾",
			SendStatus:              false,
			SystemMessage:           defenceSystemMessage(result.SkillID, result.DefenceDurationSeconds),
			SuppressStatusBroadcast: result.SkillID == "神圣战甲术" || result.SkillID == "幽灵盾",
			SendUserState:           false,
		})
	}
	appendAffectedMonster := func(mon Monster) {
		pushIndex := monsterPushIndexes[mon.ID]
		for pushIndex < len(result.MonsterPushes) && result.MonsterPushes[pushIndex].MonsterID != mon.ID {
			pushIndex++
		}
		if pushIndex < len(result.MonsterPushes) {
			for pushIndex < len(result.MonsterPushes) && result.MonsterPushes[pushIndex].MonsterID == mon.ID {
				events = append(events, SpellEvent{Kind: SpellEventMonsterAction, MonsterAction: result.MonsterPushes[pushIndex]})
				pushIndex++
			}
			monsterPushIndexes[mon.ID] = pushIndex
			return
		}
		nameColorChanged := false
		for _, changed := range result.NameColorMonsters {
			if changed.ID == mon.ID {
				events = append(events, SpellEvent{Kind: SpellEventMonsterNameColor, Monster: mon})
				nameColorChanged = true
				break
			}
		}
		for _, changed := range result.NameMonsters {
			if changed.ID == mon.ID {
				events = append(events, SpellEvent{Kind: SpellEventMonsterUsername, Monster: mon})
				return
			}
		}
		if !nameColorChanged {
			events = append(events, SpellEvent{Kind: SpellEventMonsterRefresh, Monster: mon})
		}
	}
	if len(result.AffectedTargets) > 0 {
		for _, target := range result.AffectedTargets {
			if target.Character != nil {
				appendAffectedCharacter(*target.Character)
			} else if target.Monster != nil {
				if result.SkillID != "神圣战甲术" && result.SkillID != "幽灵盾" {
					appendAffectedMonster(*target.Monster)
				}
			}
		}
	} else {
		affectedMonsters := append([]Monster(nil), result.AffectedMonsters...)
		seenMonsters := make(map[string]struct{}, len(affectedMonsters))
		for _, mon := range affectedMonsters {
			seenMonsters[mon.ID] = struct{}{}
		}
		for _, mon := range result.NameColorMonsters {
			if _, ok := seenMonsters[mon.ID]; !ok {
				affectedMonsters = append(affectedMonsters, mon)
				seenMonsters[mon.ID] = struct{}{}
			}
		}
		for _, mon := range affectedMonsters {
			appendAffectedMonster(mon)
		}
		for _, ch := range result.AffectedCharacters {
			appendAffectedCharacter(ch)
		}
	}
	for _, target := range result.HealingGaugeTargets {
		event := SpellEvent{Kind: SpellEventHealingGauge, Caster: result.Character}
		if target.Character != nil {
			event.Character = *target.Character
		}
		if target.Monster != nil {
			event.Monster = *target.Monster
		}
		events = append(events, event)
	}
	if len(result.OrderedEvents) == 0 && len(result.Impacts) > 0 {
		for _, impact := range result.Impacts {
			if impact.MonsterHit != nil {
				events = append(events, SpellEvent{Kind: SpellEventMonsterHit, MonsterHit: *impact.MonsterHit})
			} else if impact.CharacterHit != nil {
				events = append(events, SpellEvent{Kind: SpellEventCharacterHit, Character: result.Character, CharacterHit: *impact.CharacterHit})
			}
		}
	} else if len(result.OrderedEvents) == 0 && len(result.MonsterHits) > 0 {
		for _, hit := range result.MonsterHits {
			events = append(events, SpellEvent{Kind: SpellEventMonsterHit, MonsterHit: hit})
		}
	} else if len(result.OrderedEvents) == 0 && result.MonsterHit != nil {
		events = append(events, SpellEvent{Kind: SpellEventMonsterHit, MonsterHit: *result.MonsterHit})
	}
	if len(result.OrderedEvents) == 0 {
		for _, action := range result.MonsterActions {
			events = append(events, SpellEvent{Kind: SpellEventMonsterAction, MonsterAction: action})
		}
	}
	if len(result.OrderedEvents) == 0 && len(result.Impacts) == 0 {
		for _, hit := range result.CharacterHits {
			events = append(events, SpellEvent{Kind: SpellEventCharacterHit, Character: result.Character, CharacterHit: hit})
		}
	}
	for _, ch := range result.NameColorCharacters {
		events = append(events, SpellEvent{Kind: SpellEventCharacterNameColor, Character: ch})
	}
	for _, mon := range result.SummonedMonsters {
		events = append(events, SpellEvent{Kind: SpellEventSummon, Monster: mon})
	}
	if result.SkillID != "野蛮冲撞" && !result.SuppressMagicFire && !result.SpaceMoveFireInBranch {
		appendMagicFire(result.Character)
	}
	if result.Experience > 0 {
		events = append(events, SpellEvent{Kind: SpellEventExperience, Character: result.Character, Experience: result.Experience, CurrentExp: result.CurrentExp})
	}
	if result.SkillTraining {
		delay := time.Second
		if result.SkillLevelUp {
			delay = 800 * time.Millisecond
		}
		events = append(events, SpellEvent{Kind: SpellEventSkillExp, MagicID: magicID, SkillLevel: result.SkillLevel, SkillTrain: result.SkillTrain, SkillExpDelay: delay, SkillExpReplacePending: result.SkillLevelUp, Character: result.Character})
	}
	if result.LevelUp {
		events = append(events, SpellEvent{Kind: SpellEventLevelUp, Character: result.Character})
	}
	return events
}

func (w *World) spellFailureEvents(previous storage.Character, result SkillCastResult, skill data.StdSkill, targetX, targetY int, targetID int32) []SpellEvent {
	magicID, _ := w.MagicIDByName(result.SkillID)
	casterState := previous
	casterState.MP = result.Character.MP
	startTargetID := result.StartTargetID
	if startTargetID == 0 && (targetID == 0 || result.TargetIDResolved) {
		startTargetID = targetID
	}
	magicTargetID := result.MagicTargetID
	if magicTargetID == 0 && (targetID == 0 || result.TargetIDResolved) {
		magicTargetID = targetID
	}
	events := []SpellEvent{{Kind: SpellEventCasterState, Character: casterState, Previous: previous, SendHealth: result.ManaConsumed}}
	events = append(events, SpellEvent{Kind: SpellEventStart, Caster: previous, MagicID: magicID, Effect: skill.Effect, TargetX: targetX, TargetY: targetY, TargetID: startTargetID})
	slots := make([]int, 0, len(previous.EquippedItems))
	for slot := range previous.EquippedItems {
		slots = append(slots, slot)
	}
	sort.Ints(slots)
	for _, slot := range slots {
		before := previous.EquippedItems[slot]
		after, ok := result.Character.EquippedItems[slot]
		if !ok {
			events = append(events, SpellEvent{Kind: SpellEventItemDelete, Character: result.Character, DeletedItem: before})
			continue
		}
		if before.Dura != after.Dura || before.DuraMax != after.DuraMax {
			events = append(events, SpellEvent{Kind: SpellEventDurability, Character: result.Character, Durability: SpellDurability{Slot: slot, Dura: after.Dura, DuraMax: after.DuraMax}})
		}
	}
	return events
}

func (w *World) advanceSkillTrainingLocked(skill data.StdSkill, state *storage.SkillState) bool {
	if state == nil {
		return false
	}
	trainRequired := skillTrainRequirement(skill, state.Level)
	if trainRequired <= 0 || state.Level >= 3 || state.Train < trainRequired {
		if state.Train < 0 {
			state.Train = 0
		}
		return false
	}
	state.Train -= trainRequired
	state.Level++
	if state.Train < 0 {
		state.Train = 0
	}
	return true
}

func (w *World) applySkillTrainingLocked(charLevel int, skill data.StdSkill, state *storage.SkillState, points int) bool {
	if points <= 0 {
		return false
	}
	if state == nil || state.Level >= 3 {
		return false
	}
	needLevel := skillNeedLevel(skill, state.Level)
	if needLevel <= 0 || charLevel < needLevel {
		return false
	}
	state.Train += points
	w.advanceSkillTrainingLocked(skill, state)
	return true
}

func skillNeedLevel(skill data.StdSkill, level byte) int {
	need := skill.NeedLevel1
	switch level {
	case 1:
		if skill.NeedLevel2 > 0 {
			need = skill.NeedLevel2
		}
	case 2:
		if skill.NeedLevel3 > 0 {
			need = skill.NeedLevel3
		} else if skill.NeedLevel2 > 0 {
			need = skill.NeedLevel2
		}
	}
	return need
}

func skillTrainRequirement(skill data.StdSkill, level byte) int {
	train := skill.TrainLevel1
	switch level {
	case 1:
		if skill.TrainLevel2 > 0 {
			train = skill.TrainLevel2
		}
	case 2:
		if skill.TrainLevel3 > 0 {
			train = skill.TrainLevel3
		} else if skill.TrainLevel2 > 0 {
			train = skill.TrainLevel2
		}
	}
	return train
}

func magicTrainPointsForSkill(r *rand.Rand) int {
	if r == nil {
		return 1
	}
	return r.Intn(3) + 1
}

func (w *World) countActiveSummonedMonstersLocked(masterID, templateID string, _ time.Time) int {
	count := 0
	for _, mon := range w.monsters {
		if mon == nil || !mon.Alive || mon.MasterID != masterID {
			continue
		}
		if templateID != "" && mon.TemplateID != templateID {
			continue
		}
		count++
	}
	return count
}

func (w *World) activeSummonedMonsterByTemplateLocked(masterID, templateID string, _ time.Time) *Monster {
	for _, mon := range w.monsters {
		if mon == nil || !mon.Alive || mon.MasterID != masterID {
			continue
		}
		if templateID != "" && mon.TemplateID != templateID {
			continue
		}
		return mon
	}
	return nil
}

func (w *World) queueGroupHealingLocked(caster storage.Character, players []storage.Character, targetX, targetY, heal int, now time.Time) (bool, error) {
	if heal < 1 {
		heal = 1
	}
	players = playersWithCaster(players, caster)
	changed := false
	playerByID := make(map[string]storage.Character, len(players))
	for _, target := range players {
		if target.ID != "" {
			playerByID[target.ID] = target
		}
	}
	for _, target := range w.spellAreaTargetsLocked(players, caster.MapID, targetX, targetY, 1) {
		if target.Character != nil {
			if !w.isProperFriendLocked(caster, *target.Character) || target.Character.HP >= target.Character.MaxHP {
				continue
			}
			w.pendingSpells = append(w.pendingSpells, pendingSpell{DueAt: now.Add(800 * time.Millisecond), CasterID: caster.ID, TargetCharacterID: target.Character.ID, TargetX: targetX, TargetY: targetY, Healing: heal})
			changed = true
			continue
		}
		mon := target.Monster
		if mon == nil || mon.HP >= mon.MaxHP {
			continue
		}
		master, ok := playerByID[mon.MasterID]
		if !ok || !w.isProperFriendLocked(caster, master) {
			continue
		}
		w.pendingSpells = append(w.pendingSpells, pendingSpell{DueAt: now.Add(800 * time.Millisecond), CasterID: caster.ID, TargetMonsterID: mon.ID, TargetX: targetX, TargetY: targetY, Healing: heal})
		changed = true
	}
	return changed, nil
}

func (w *World) canSeeHealGaugeLocked(caster storage.Character) bool {
	state, _, ok := caster.Skills.Get("心灵启示")
	return ok && state.Level >= 2
}

func (w *World) healingGaugeTargetsLocked(caster storage.Character, players []storage.Character, targetX, targetY int) []SpellHealingGaugeTarget {
	players = playersWithCaster(players, caster)
	targets := make([]SpellHealingGaugeTarget, 0)
	playerByID := make(map[string]storage.Character, len(players))
	for _, target := range players {
		if target.ID != "" {
			playerByID[target.ID] = target
		}
	}
	for _, areaTarget := range w.spellAreaTargetsLocked(players, caster.MapID, targetX, targetY, 1) {
		if areaTarget.Character != nil {
			if !w.isProperFriendLocked(caster, *areaTarget.Character) {
				continue
			}
			target := *areaTarget.Character
			targets = append(targets, SpellHealingGaugeTarget{Character: &target})
			continue
		}
		mon := areaTarget.Monster
		if mon == nil {
			continue
		}
		master, ok := playerByID[mon.MasterID]
		if ok && w.isProperFriendLocked(caster, master) {
			copy := *mon
			targets = append(targets, SpellHealingGaugeTarget{Monster: &copy})
		}
	}
	return targets
}

func (w *World) groupDefenceDurationLocked(ch storage.Character, skill data.StdSkill, state storage.SkillState) time.Duration {
	combat := w.combatStatsLocked(ch)
	low := combat.SC * 10
	rangeSize := combat.SCMax - combat.SC + 1
	if rangeSize < 1 {
		rangeSize = 1
	}
	duration := w.attackPowerLocked(combat, w.spellPower13Locked(60, skill, state)+low, rangeSize)
	if duration < 1 {
		duration = 1
	}
	return time.Duration(duration) * time.Second
}

func defenceSystemMessage(skillID string, seconds int) string {
	switch skillID {
	case "神圣战甲术":
		return fmt.Sprintf("防御力增加%d秒", seconds)
	case "幽灵盾":
		return fmt.Sprintf("魔法防御力增加%d秒", seconds)
	default:
		return ""
	}
}

func (w *World) groupDefenceCharactersLocked(caster storage.Character, skill data.StdSkill, state storage.SkillState, players []storage.Character, targetX, targetY int, magic bool, duration time.Duration, now time.Time) ([]storage.Character, []Monster, bool, error) {
	players = playersWithCaster(players, caster)
	affected := make([]storage.Character, 0, 8)
	affectedMonsters := make([]Monster, 0, 8)
	validTargets := false
	expires := now.Add(duration).UnixNano()
	refreshExpiry := func(current int64) int64 {
		if current <= now.UnixNano() {
			return expires
		}
		remaining := time.Unix(0, current).Sub(now)
		remainingSeconds := time.Duration(math.Ceil(remaining.Seconds())) * time.Second
		if remainingSeconds > duration {
			return now.Add(remainingSeconds).UnixNano()
		}
		return expires
	}
	for _, target := range w.spellAreaTargetsWithLifeFilterLocked(players, caster.MapID, targetX, targetY, 3, false) {
		if target.Character != nil {
			ch := *target.Character
			if !w.isProperFriendLocked(caster, ch) {
				continue
			}
			validTargets = true
			changed := false
			if magic {
				nextExpiry := refreshExpiry(ch.MagDefenceUpUntil)
				if ch.MagDefenceUpUntil != nextExpiry {
					ch.MagDefenceUpUntil = nextExpiry
					changed = true
				}
			} else {
				nextExpiry := refreshExpiry(ch.DefenceUpUntil)
				if ch.DefenceUpUntil != nextExpiry {
					ch.DefenceUpUntil = nextExpiry
					changed = true
				}
			}
			if changed {
				if err := w.store.SaveCharacter(ch); err != nil {
					return nil, nil, false, err
				}
			}
			affected = append(affected, ch)
			continue
		}
		mon := target.Monster
		if !w.isFriendlySummonedMonsterLocked(caster, players, mon) {
			continue
		}
		validTargets = true
		if magic {
			nextExpiry := refreshExpiry(mon.MagDefenceUpUntil)
			if mon.MagDefenceUpUntil != nextExpiry {
				mon.MagDefenceUpUntil = nextExpiry
			}
		} else {
			nextExpiry := refreshExpiry(mon.DefenceUpUntil)
			if mon.DefenceUpUntil != nextExpiry {
				mon.DefenceUpUntil = nextExpiry
			}
		}
		affectedMonsters = append(affectedMonsters, *mon)
	}
	return affected, affectedMonsters, validTargets, nil
}

func (w *World) monstersInRadiusLocked(mapID string, x, y, radius int) []*Monster {
	if radius < 0 {
		radius = 0
	}
	out := make([]*Monster, 0, 8)
	for _, mon := range w.monsters {
		if mon == nil || !mon.Alive || mon.MapID != mapID {
			continue
		}
		if abs(mon.X-x) > radius || abs(mon.Y-y) > radius {
			continue
		}
		out = append(out, mon)
	}
	sort.Slice(out, func(i, j int) bool {
		di := abs(out[i].X-x) + abs(out[i].Y-y)
		dj := abs(out[j].X-x) + abs(out[j].Y-y)
		if di != dj {
			return di < dj
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func (w *World) monsterTargetLocked(mapID string, x, y int, targetID int32, radius int) *Monster {
	if targetID != 0 {
		for _, mon := range w.monsters {
			if mon == nil || !mon.Alive || mon.MapID != mapID || MonsterActorID(*mon) != targetID {
				continue
			}
			if abs(mon.X-x) <= radius && abs(mon.Y-y) <= radius {
				return mon
			}
		}
		return nil
	}
	return w.monsterAtPointLocked(mapID, x, y, 0)
}

func (w *World) explicitMonsterTargetLocked(mapID string, x, y int, targetID int32, radius int) *Monster {
	if targetID == 0 {
		return nil
	}
	return w.monsterTargetLocked(mapID, x, y, targetID, radius)
}

func (w *World) spellTargetNearLocked(players []storage.Character, mapID string, x, y int, targetID int32) (storage.Character, bool) {
	for _, target := range players {
		if target.ID == "" || target.MapID != mapID || CharacterActorID(target) != targetID {
			continue
		}
		if abs(target.X-x) <= 1 && abs(target.Y-y) <= 1 {
			return target, true
		}
	}
	return storage.Character{}, false
}

func (w *World) spellMonsterTargetNearLocked(mapID string, x, y int, targetID int32, radius int) *Monster {
	for _, mon := range w.monsters {
		if mon == nil || mon.MapID != mapID || MonsterActorID(*mon) != targetID {
			continue
		}
		if abs(mon.X-x) <= radius && abs(mon.Y-y) <= radius {
			return mon
		}
	}
	return nil
}

func (w *World) explicitCharacterTargetLocked(players []storage.Character, mapID string, x, y int, targetID int32) (storage.Character, bool) {
	if targetID == 0 {
		return storage.Character{}, false
	}
	return w.characterAtPointLocked(players, mapID, x, y, targetID)
}

func (w *World) isProperFriendLocked(a, b storage.Character) bool {
	if a.ID == "" || b.ID == "" {
		return false
	}
	switch a.AttackMode {
	case 0, 1:
		return true
	case 2:
		if a.ID == b.ID {
			return true
		}
		if a.GroupOwnerID == "" || b.GroupOwnerID == "" {
			return false
		}
		return a.GroupOwnerID == b.ID || b.GroupOwnerID == a.ID || a.GroupOwnerID == b.GroupOwnerID
	case 3:
		if a.ID == b.ID || (a.GuildID != "" && a.GuildID == b.GuildID) {
			return true
		}
		return a.GuildWarArea && b.GuildWarArea && a.GuildAllianceID != "" && a.GuildAllianceID == b.GuildAllianceID
	case 4:
		if a.ID == b.ID {
			return true
		}
		if characterPKLevel(a) >= 2 {
			return characterPKLevel(b) < 2
		}
		return characterPKLevel(b) >= 2
	default:
		return false
	}
}

func (w *World) isFriendlySummonedMonsterLocked(caster storage.Character, players []storage.Character, mon *Monster) bool {
	if mon == nil || mon.Race < 50 || mon.MasterID == "" {
		return false
	}
	if mon.MasterID == caster.ID {
		return true
	}
	master, ok := w.characterByIDLocked(players, mon.MasterID)
	if !ok {
		return false
	}
	return w.isProperFriendLocked(caster, master)
}

func (w *World) characterByIDLocked(players []storage.Character, id string) (storage.Character, bool) {
	for _, ch := range players {
		if ch.ID == id {
			return ch, true
		}
	}
	return storage.Character{}, false
}

func targetLevel(mon *Monster, ch storage.Character) int {
	if mon != nil {
		return mon.Level
	}
	return ch.Level
}

func (w *World) spellMonsterDamageLocked(ch storage.Character, skill data.StdSkill, state storage.SkillState) int {
	return w.spellDamageLocked(ch, skill, state)
}

func (w *World) spellMabePowerLocked(ch storage.Character, skill data.StdSkill, state storage.SkillState) int {
	base := skill.Power
	if base < 0 {
		base = 0
	}
	level := int(state.Level) + 1
	power := referenceRound(float64(base) / float64(spellTrainLevel+1) * float64(level))
	power += w.spellDefenseBonusLocked(skill)
	combat := w.combatStatsLocked(ch)
	low, high := combat.MC, combat.MCMax
	if high < low {
		high = low
	}
	return w.attackPowerLocked(combat, power+low, high-low+1)
}

func (w *World) spellSpiritDamageLocked(ch storage.Character, skill data.StdSkill, state storage.SkillState) int {
	combat := w.combatStatsLocked(ch)
	damage := w.spellScaledPowerLocked(skill, state)
	low := combat.SC
	high := combat.SCMax
	if high < low {
		high = low
	}
	damage = w.attackPowerLocked(combat, damage+low, high-low+1)
	if damage < 1 {
		damage = 1
	}
	return damage
}

func (w *World) spellDamageLocked(ch storage.Character, skill data.StdSkill, state storage.SkillState) int {
	combat := w.combatStatsLocked(ch)
	damage := w.spellScaledPowerLocked(skill, state)
	low := combat.MC
	high := combat.MCMax
	if high < low {
		high = low
	}
	damage = w.attackPowerLocked(combat, damage+low, high-low+1)
	if damage < 1 {
		damage = 1
	}
	return damage
}

func (w *World) spellCharacterDamageLocked(caster storage.Character, target storage.Character, skill data.StdSkill, state storage.SkillState) (storage.Character, CharacterHit, error) {
	damage := w.spellDamageLocked(caster, skill, state)
	return w.spellCharacterDamageWithPowerLocked(caster, target, damage)
}

func (w *World) spellHealAmountLocked(ch storage.Character, skill data.StdSkill, state storage.SkillState) int {
	combat := w.combatStatsLocked(ch)
	heal := w.spellScaledPowerLocked(skill, state)
	low := combat.SC
	high := combat.SCMax
	if high < low {
		high = low
	}
	if high > low {
		heal += w.attackPowerLocked(combat, low*2, (high-low)*2+1)
	} else {
		heal += low * 2
	}
	if heal < 1 {
		heal = 1
	}
	return heal
}

func (w *World) attackPowerLocked(combat CombatStats, base, power int) int {
	if power < 0 {
		power = 0
	}
	if combat.Luck > 0 {
		if w.rand.Intn(10-minInt(9, combat.Luck)) == 0 {
			return base + power
		}
	}
	result := base + w.rand.Intn(power+1)
	if combat.Luck < 0 {
		bound := 10 - maxInt(0, -combat.Luck)
		roll := 0
		if bound > 0 {
			roll = w.rand.Intn(bound)
		}
		if roll == 0 {
			return base
		}
	}
	return result
}

func (w *World) spellScaledPowerLocked(skill data.StdSkill, state storage.SkillState) int {
	base := skill.Power
	if base <= 0 {
		base = 1
	}
	maxPower := skill.MaxPower
	if maxPower < base {
		maxPower = base
	}
	roll := base
	if maxPower > base {
		roll += w.rand.Intn(maxPower - base)
	}
	power := w.spellPowerFromBaseLocked(roll, skill, state)
	if power < 1 {
		power = 1
	}
	return power
}

func (w *World) spellPowerFromBaseLocked(base int, skill data.StdSkill, state storage.SkillState) int {
	power := referenceRound(float64(base) / float64(spellTrainLevel+1) * float64(int(state.Level)+1))
	power += w.spellDefenseBonusLocked(skill)
	if power < 1 {
		power = 1
	}
	return power
}

func (w *World) spellPower13Locked(base int, skill data.StdSkill, state storage.SkillState) int {
	level := int(state.Level) + 1
	d10 := float64(base) / 3.0
	d18 := float64(base) - d10
	power := referenceRound(d18/float64(spellTrainLevel+1)*float64(level) + d10)
	power += w.spellDefenseBonusLocked(skill)
	if power < 1 {
		power = 1
	}
	return power
}

func (w *World) spellDefenseBonusLocked(skill data.StdSkill) int {
	low := skill.DefPower
	high := skill.DefMaxPower
	if high < low {
		high = low
	}
	if high > low {
		return low + w.rand.Intn(high-low)
	}
	return low
}

func (w *World) magicShieldDurationLocked(ch storage.Character, skill data.StdSkill, state storage.SkillState) time.Duration {
	combat := w.combatStatsLocked(ch)
	roll := combat.MC
	if high := combat.MCMax; high > roll {
		roll += w.rand.Intn(high - roll + 1)
	}
	roll += 15
	if roll < 1 {
		roll = 1
	}
	scaled := w.spellPowerFromBaseLocked(roll, skill, state)
	if scaled < 1 {
		scaled = 1
	}
	return time.Duration(scaled) * time.Second
}
