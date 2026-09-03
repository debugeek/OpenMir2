package world

import (
	"time"

	"openmir2/internal/storage"
)

func Plain6ClassName(job string) string {
	switch job {
	case "0":
		return "warrior"
	case "1":
		return "wizard"
	case "2":
		return "taoist"
	default:
		if job == "" {
			return "warrior"
		}
		return job
	}
}

func Plain6ClassID(class string) int {
	switch NormalizeClass(class) {
	case "wizard":
		return 1
	case "taoist":
		return 2
	default:
		return 0
	}
}

func SubAbilitySpeed(class string) byte {
	if Plain6ClassID(class) == 2 {
		return 18
	}
	return 15
}

func (w *World) CreateCharacterWithAppearanceAtRandomStartPoint(account, name, class string, hair, sex int) (storage.Character, error) {
	mapID, x, y := w.RandomNewCharacterSpawn()
	return w.CreateCharacterWithAppearance(account, name, class, hair, sex, mapID, x, y)
}

func (w *World) CreateCharacterWithAppearance(account, name, class string, hair, sex int, mapID string, x, y int) (storage.Character, error) {
	base := Base(class, 1)
	now := time.Now().UnixMilli()
	ch := storage.Character{
		Account:          account,
		Name:             name,
		Class:            class,
		Hair:             hair,
		Sex:              sex,
		Level:            1,
		HomeMap:          mapID,
		HomeX:            x,
		HomeY:            y,
		MapID:            mapID,
		X:                x,
		Y:                y,
		MaxHP:            base.MaxHP,
		HP:               base.MaxHP,
		MaxMP:            base.MaxMP,
		MP:               base.MaxMP,
		IncHealthSpellAt: now,
		BagItems:         []storage.UserItem{{ItemID: "木剑"}},
	}
	return w.store.InsertCharacter(ch)
}

func (w *World) NormalizeCharacterState(ch storage.Character) (storage.Character, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	changed := false
	if ch.IncHealthSpellAt == 0 {
		ch.IncHealthSpellAt = time.Now().UnixMilli()
		changed = true
	}
	if w.normalizeBagItemMakeIndexesLocked(&ch) {
		changed = true
	}
	if w.normalizeEquippedItemsLocked(&ch) {
		changed = true
	}
	if w.pruneStaleEquippedItemsLocked(&ch) {
		changed = true
	}
	if changed {
		_ = w.store.SaveCharacter(ch)
	}
	return ch, changed
}

func CanInspectCharacterAt(ch storage.Character, x, y int) bool {
	return CanObserveAt(ch, x, y, 1)
}

func CanObserveAt(ch storage.Character, x, y, viewRange int) bool {
	if ch.MapID == "" {
		return false
	}
	return abs(ch.X-x) <= viewRange && abs(ch.Y-y) <= viewRange
}

func (w *World) HumanFeatureForCharacter(ch storage.Character) int32 {
	dressShape := 0
	if item, ok := w.equippedItemLocked(ch, SlotDress); ok {
		itemID := item.ItemID
		if item, ok := w.Item(itemID); ok {
			dressShape = item.Shape
		}
	}
	weaponShape := 0
	if item, ok := w.equippedItemLocked(ch, SlotWeapon); ok {
		itemID := item.ItemID
		if item, ok := w.Item(itemID); ok {
			weaponShape = item.Shape
		}
	}
	sex := ch.Sex
	if sex != 0 {
		sex = 1
	}
	hair := ch.Hair
	if hair < 0 {
		hair = 0
	}
	if dressShape < 0 {
		dressShape = 0
	}
	if weaponShape < 0 {
		weaponShape = 0
	}
	dress := dressShape*2 + sex
	weapon := weaponShape*2 + sex
	hairFeature := hair*2 + sex
	return int32(uint32(0) | uint32(weapon)<<8 | uint32(hairFeature)<<16 | uint32(dress)<<24)
}

func (w *World) CharacterDisplayName(ch storage.Character) string {
	return ch.Name
}

func (w *World) CharacterNameColor(ch storage.Character) uint16 {
	return 255
}

func (w *World) CharacterAreaState(ch storage.Character) int32 {
	return 0
}

func (w *World) CharacterStatus(ch storage.Character) int32 {
	return characterStatus(ch, time.Now(), true)
}

func characterStatus(ch storage.Character, now time.Time, includeExpired bool) int32 {
	active := func(until int64) bool {
		return until > 0 && (includeExpired || until > now.UnixNano())
	}
	status := int32(0)
	if ch.Sitting {
		status |= 1
	}
	transparent := characterTransparentActive(ch, now)
	if includeExpired {
		transparent = ch.TransparentUntil > 0
	}
	if transparent {
		status |= 0x00800000
	}
	if active(ch.DefenceUpUntil) {
		status |= 0x00400000
	}
	if active(ch.MagDefenceUpUntil) {
		status |= 0x00200000
	}
	if active(ch.BubbleDefenceUntil) {
		status |= 0x00100000
	}
	if active(ch.PoisonHealthUntil) {
		status |= -2147483648
	}
	if ch.PoisonArmorLevel > 0 && active(ch.PoisonArmorUntil) {
		status |= int32(uint32(0x40000000))
	}
	if active(ch.ParalyzedUntil) {
		status |= 0x04000000
	}
	return status
}

func (w *World) CharacterHitSpeed(ch storage.Character) int32 {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.normalizeEquippedItemsLocked(&ch)
	stats := w.combatStatsLocked(ch)
	return int32(stats.HitSpeed + int(ch.ExtraAbil[3]))
}

func (w *World) CharacterFeatureEx(ch storage.Character) int32 {
	return 0
}
