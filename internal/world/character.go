package world

import (
	"time"

	"openmir2/internal/storage"
)

func CharacterObjectLight(ch storage.Character) int {
	item, ok := ch.EquippedItems[SlotRightHand]
	if ok && item.ItemID != "" && item.Dura > 0 {
		return 3
	}
	return 0
}

func (w *World) CharacterObjectLight(ch storage.Character) int {
	return CharacterObjectLight(ch)
}

func (w *World) resolvePKDeathLocked(victim, killer *storage.Character) bool {
	if victim == nil || killer == nil || victim.ID == "" || killer.ID == "" || victim.ID == killer.ID || victim.MapID != killer.MapID {
		return false
	}
	if w.gameplay.Combat.NonPKServer || victim.FreePKArea || w.guildsAtWar(*victim, *killer) {
		return false
	}
	if mapData, ok := w.data.Maps[victim.MapID]; ok {
		if mapData.Safe || mapData.FightZone || mapData.Fight3Zone {
			return false
		}
	}
	if characterPKLevel(*victim) >= 2 || victim.PKFlag {
		return false
	}
	points := w.gameplay.Combat.KillHumanAddPKPoint
	if points <= 0 {
		return false
	}
	killer.PKPoint += points
	if w.gameplay.Combat.KillHumanDecLuckPoint > 0 {
		addBodyLuck(killer, -float64(w.gameplay.Combat.KillHumanDecLuckPoint))
	}
	return true
}

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
	mapID, homeX, homeY := w.RandomNewCharacterSpawn()
	x := homeX + w.RandomIntn(3) - 2
	y := homeY + w.RandomIntn(3) - 2
	starterItems := []string{"蜡烛", "金创药(小量)", "木剑"}
	if sex == 0 {
		starterItems = append(starterItems, "布衣(男)")
	} else {
		starterItems = append(starterItems, "布衣(女)")
	}
	items := make([]storage.UserItem, 0, len(starterItems))
	for _, itemID := range starterItems {
		item, err := w.NewItemInstance(itemID)
		if err != nil {
			return storage.Character{}, err
		}
		items = append(items, item)
	}
	return w.createCharacterWithItemsAtHome(account, name, class, hair, sex, mapID, homeX, homeY, x, y, items, 15, 15)
}

func (w *World) CreateCharacterWithAppearance(account, name, class string, hair, sex int, mapID string, x, y int) (storage.Character, error) {
	return w.createCharacterWithItems(account, name, class, hair, sex, mapID, x, y, []storage.UserItem{{ItemID: "木剑"}}, -1, -1)
}

func (w *World) createCharacterWithItems(account, name, class string, hair, sex int, mapID string, x, y int, items []storage.UserItem, initialHP, initialMP int) (storage.Character, error) {
	return w.createCharacterWithItemsAtHome(account, name, class, hair, sex, mapID, x, y, x, y, items, initialHP, initialMP)
}

func (w *World) createCharacterWithItemsAtHome(account, name, class string, hair, sex int, mapID string, homeX, homeY, x, y int, items []storage.UserItem, initialHP, initialMP int) (storage.Character, error) {
	base := Base(class, 1)
	if initialHP < 0 {
		initialHP = base.MaxHP
	}
	if initialMP < 0 {
		initialMP = base.MaxMP
	}
	nowTime := time.Now()
	now := nowTime.UnixMilli()
	ch := storage.Character{
		Account:            account,
		Name:               name,
		Class:              class,
		Hair:               hair,
		Sex:                sex,
		Level:              1,
		HomeMap:            mapID,
		HomeX:              homeX,
		HomeY:              homeY,
		MapID:              mapID,
		X:                  x,
		Y:                  y,
		MaxHP:              base.MaxHP,
		HP:                 initialHP,
		MaxMP:              base.MaxMP,
		MP:                 initialMP,
		IncHealthSpellAt:   now,
		HealthTickAt:       now,
		SpellTickAt:        now,
		PoisonHealthTickAt: nowTime.UnixNano(),
		PerHealth:          5,
		PerSpell:           5,
		PerHealing:         5,
		BagItems:           items,
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
	if ch.HealthTickAt == 0 {
		ch.HealthTickAt = time.Now().UnixMilli()
	}
	if ch.SpellTickAt == 0 {
		ch.SpellTickAt = time.Now().UnixMilli()
	}
	if ch.PoisonHealthTickAt == 0 {
		ch.PoisonHealthTickAt = time.Now().UnixNano()
	}
	if ch.PerHealth == 0 {
		ch.PerHealth = 5
	}
	if ch.PerSpell == 0 {
		ch.PerSpell = 5
	}
	if ch.PerHealing == 0 {
		ch.PerHealing = 5
	}
	if w.normalizeBagItemMakeIndexesLocked(&ch) {
		changed = true
	}
	if w.normalizeStorageItemMakeIndexesLocked(&ch) {
		changed = true
	}
	if w.normalizeEquippedItemsLocked(&ch) {
		changed = true
	}
	if w.normalizeBagItemMakeIndexesLocked(&ch) {
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

func filterDisplayName(name string) string {
	for i := 0; i < len(name); i++ {
		if (name[i] >= '0' && name[i] <= '9') || name[i] == '-' {
			return name[:i]
		}
	}
	return name
}

func (w *World) CharacterNameColor(ch storage.Character) uint16 {
	if ch.PKPoint >= 200 {
		return 0xF9
	}
	if ch.PKPoint >= 100 {
		return 0xFB
	}
	if ch.PKFlag {
		return 0x2F
	}
	return 255
}

func (w *World) CharacterNameColorFor(observer, target storage.Character) uint16 {
	color := w.CharacterNameColor(target)
	if target.PKPoint >= 200 {
		return color
	}
	if mapData, ok := w.data.Maps[target.MapID]; ok && mapData.Fight3Zone {
		if observer.GuildID == target.GuildID {
			return 0xB4
		}
		return 0x45
	}
	if target.GuildID != "" && observer.GuildID == target.GuildID {
		return 0xB4
	}
	if w.guildWarArea(observer) && w.guildWarArea(target) && charactersShareAlliance(observer, target) {
		return 0xB4
	}
	if w.guildWarArea(observer) && w.guildWarArea(target) && observer.FreePKArea && target.FreePKArea {
		return 0xDD
	}
	if w.guildsAtWar(observer, target) {
		return 0x45
	}
	return color
}

func charactersShareAlliance(a, b storage.Character) bool {
	aAlliances := append([]string(nil), a.GuildAllianceIDs...)
	if a.GuildAllianceID != "" && !containsAllianceID(aAlliances, a.GuildAllianceID) {
		aAlliances = append(aAlliances, a.GuildAllianceID)
	}
	bAlliances := append([]string(nil), b.GuildAllianceIDs...)
	if b.GuildAllianceID != "" && !containsAllianceID(bAlliances, b.GuildAllianceID) {
		bAlliances = append(bAlliances, b.GuildAllianceID)
	}
	for _, alliance := range aAlliances {
		if containsAllianceID(bAlliances, alliance) {
			return true
		}
	}
	return false
}

func containsAllianceID(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func (w *World) CharacterAreaState(ch storage.Character) int32 {
	state := int32(0)
	if mapData, ok := w.data.Maps[ch.MapID]; ok {
		if mapData.FightZone {
			state |= 1
		}
		if mapData.Safe {
			state |= 2
		}
	}
	if ch.FreePKArea {
		state |= 4
	}
	return state
}

func (w *World) guildWarArea(ch storage.Character) bool {
	if ch.GuildWarArea {
		return true
	}
	if ch.GuildID == "" {
		return false
	}
	if w.store == nil {
		return false
	}
	if mapData, ok := w.data.Maps[ch.MapID]; !ok || mapData.Safe {
		return false
	}
	guild, ok := w.store.Guild(ch.GuildID)
	if !ok {
		return false
	}
	for _, active := range guild.Wars {
		if active {
			return true
		}
	}
	return false
}

func (w *World) guildsAtWar(a, b storage.Character) bool {
	if a.GuildID == "" || b.GuildID == "" {
		return false
	}
	if w.store == nil {
		return a.GuildWarArea && b.GuildWarArea
	}
	first, firstOK := w.store.Guild(a.GuildID)
	second, secondOK := w.store.Guild(b.GuildID)
	return firstOK && secondOK && first.Wars[b.GuildID] && second.Wars[a.GuildID]
}

func (w *World) CharacterStatus(ch storage.Character) int32 {
	return characterStatus(ch, time.Now(), true)
}

func characterStatus(ch storage.Character, now time.Time, includeExpired bool) int32 {
	active := func(until int64) bool {
		return until > 0 && (includeExpired || until >= now.UnixNano())
	}
	status := int32(0)
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
	if active(ch.ShowHPUntil) {
		status |= 0x00000002
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
	w.mu.Lock()
	defer w.mu.Unlock()
	w.normalizeEquippedItemsLocked(&ch)

	dressEffect := 0
	if item, ok := w.equippedItemLocked(ch, SlotRightHand); ok {
		if standard, ok := w.data.Items[item.ItemID]; ok {
			if standard.Shape >= 1 && standard.Shape <= 50 {
				dressEffect = standard.Shape
			}
		}
	}
	if item, ok := w.equippedItemLocked(ch, SlotDress); ok {
		if standard, ok := w.data.Items[item.ItemID]; ok {
			if item.Desc[5] > 0 {
				dressEffect = int(item.Desc[5])
			}
			if standard.AniCount > 0 {
				dressEffect = standard.AniCount
			}
		}
	}
	return int32(uint16(dressEffect) << 8)
}
