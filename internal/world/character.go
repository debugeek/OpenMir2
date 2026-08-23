package world

import (
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

func HumanFeature(ch storage.Character, dressShape, weaponShape int) int32 {
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

func (w *World) CreateCharacter(account, name, class, mapID string, x, y int) (storage.Character, error) {
	return w.CreateCharacterWithAppearance(account, name, class, 0, 0, mapID, x, y)
}

func (w *World) CreateCharacterAtRandomStartPoint(account, name, class string) (storage.Character, error) {
	return w.CreateCharacterWithAppearanceAtRandomStartPoint(account, name, class, 0, 0)
}

func (w *World) CreateCharacterWithAppearanceAtRandomStartPoint(account, name, class string, hair, sex int) (storage.Character, error) {
	mapID, x, y := w.RandomNewCharacterSpawn()
	return w.CreateCharacterWithAppearance(account, name, class, hair, sex, mapID, x, y)
}

func (w *World) CreateCharacterWithAppearance(account, name, class string, hair, sex int, mapID string, x, y int) (storage.Character, error) {
	base := Base(class, 1)
	ch := storage.Character{
		Account:  account,
		Name:     name,
		Class:    class,
		Hair:     hair,
		Sex:      sex,
		Level:    1,
		HomeMap:  mapID,
		HomeX:    x,
		HomeY:    y,
		MapID:    mapID,
		X:        x,
		Y:        y,
		MaxHP:    base.MaxHP,
		HP:       base.MaxHP,
		MaxMP:    base.MaxMP,
		MP:       base.MaxMP,
		BagItems: []storage.UserItem{{ItemID: "木剑"}},
	}
	return w.store.InsertCharacter(ch)
}

func (w *World) NormalizeCharacterBagItems(ch storage.Character) (storage.Character, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	changed := w.normalizeBagItemMakeIndexesLocked(&ch)
	if changed {
		_ = w.store.SaveCharacter(ch)
	}
	return ch, changed
}

func (w *World) NormalizeCharacterState(ch storage.Character) (storage.Character, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	changed := w.normalizeBagItemMakeIndexesLocked(&ch)
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

func (w *World) CharacterAttackMode(ch storage.Character) byte {
	return byte(ch.AttackMode)
}

func (w *World) CharacterAreaState(ch storage.Character) int32 {
	return 0
}

func (w *World) CharacterStatus(ch storage.Character) int32 {
	if ch.Sitting {
		return 1
	}
	return 0
}

func (w *World) CharacterFeatureEx(ch storage.Character) int32 {
	return 0
}
