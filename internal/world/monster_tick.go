package world

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"openmir2/internal/data"
	"openmir2/internal/storage"
	"openmir2/internal/world/core"
)

const defaultCharacterMakeGhostTime = 3 * time.Minute

func (w *World) characterMakeGhostTime() time.Duration {
	if w.gameplay.Recovery.MakeGhostTimeMS <= 0 {
		return defaultCharacterMakeGhostTime
	}
	return time.Duration(w.gameplay.Recovery.MakeGhostTimeMS) * time.Millisecond
}

func (w *World) settleCharacterDeathLocked(ch *storage.Character, now time.Time, blockers ...storage.Character) []GroundDrop {
	if ch == nil {
		return nil
	}
	drops := make([]GroundDrop, 0)
	mapData, hasMap := w.data.Maps[ch.MapID]
	fightZone := hasMap && (mapData.FightZone || mapData.Fight3Zone)
	if hasMap && mapData.Fight3Zone {
		ch.FightZoneDieCount++
	}
	if !fightZone {
		protectedDrops := w.characterHasAngryRingLocked(*ch)
		if !protectedDrops && w.characterDeathDropsEquippedItemsLocked(*ch) {
			drops = append(drops, w.dropEquippedItemsLocked(ch, now, blockers...)...)
		}
		if w.gameplay.Combat.DieScatterBag {
			drops = append(drops, w.dropBagItemsOnDeathLocked(ch, now, blockers...)...)
		}
	}
	if !fightZone && w.gameplay.Combat.DieDropGold && ch.Gold > 0 {
		remaining := ch.Gold
		chunkSize := w.gameplay.Combat.DieGoldDropChunkSize
		for chunks := 0; remaining > 0 && chunks < 17; chunks++ {
			count := remaining
			if count > chunkSize {
				count = chunkSize
			}
			gold := GroundDrop{ID: fmt.Sprintf("drop-%d", w.nextID), MapID: ch.MapID, ItemID: "金币", Count: count, OwnerID: ch.ID, OwnerGroupID: ch.GroupOwnerID, PickupAt: now.Add(time.Duration(w.gameplay.Item.FloorItemCanPickUpMS) * time.Millisecond), SourceCharacterID: ch.ID, SourceMapID: ch.MapID, SourceX: ch.X, SourceY: ch.Y}
			w.nextID++
			placed := w.placeReferenceDropsLocked(ch.MapID, ch.X, ch.Y, 3, []GroundDrop{gold}, blockers...)
			if len(placed) == 0 {
				w.nextID--
				break
			}
			drops = append(drops, placed...)
			remaining -= count
		}
		ch.Gold = remaining
	}
	if !fightZone {
		addBodyLuck(ch, -float64(ch.Level*5))
	}
	return drops
}

func (w *World) characterHasAngryRingLocked(ch storage.Character) bool {
	for slot, entry := range ch.EquippedItems {
		if entry.ItemID == "" || entry.Dura <= 0 {
			continue
		}
		item, ok := w.data.Items[entry.ItemID]
		if !ok {
			continue
		}
		if (slot == SlotRingL || slot == SlotRingR) && item.Shape == 117 {
			return true
		}
		if slot != SlotDress && slot != SlotWeapon && slot != SlotRightHand && item.Shape == 170 {
			return true
		}
		if (slot == SlotDress || slot == SlotWeapon || slot == SlotRightHand) && (item.AniCount == 117 || item.AniCount == 170) {
			return true
		}
	}
	return false
}

func (w *World) characterDeathDropsEquippedItemsLocked(ch storage.Character) bool {
	if ch.ExpHitterID == "" {
		return true
	}
	if mon := w.monsters[ch.ExpHitterID]; mon != nil {
		if mon.MasterID != "" {
			return w.gameplay.Combat.KillHumanDropUseItem
		}
		return w.gameplay.Combat.KillMonsterDropUseItem
	}
	return w.gameplay.Combat.KillHumanDropUseItem
}

func (w *World) dropEquippedItemsLocked(ch *storage.Character, now time.Time, blockers ...storage.Character) []GroundDrop {
	if ch == nil {
		return nil
	}
	rate := w.gameplay.Combat.DieDropUseItemRate
	if characterPKLevel(*ch) > 2 {
		rate = w.gameplay.Combat.DieRedDropUseItemRate
	}
	drops := make([]GroundDrop, 0)
	for slot := 0; slot < useSlotCount; slot++ {
		if w.rand.Intn(rate) != 0 {
			continue
		}
		entry, equipped := ch.EquippedItems[slot]
		if !equipped || entry.ItemID == "" {
			continue
		}
		item, ok := w.data.Items[entry.ItemID]
		if !ok {
			continue
		}
		if item.StdMode == 40 {
			if entry.Dura > 2000 {
				entry.Dura -= 2000
			} else {
				entry.Dura = 0
			}
			ch.EquippedItems[slot] = entry
		}
		drop := GroundDrop{ID: fmt.Sprintf("drop-%d", w.nextID), MapID: ch.MapID, ItemID: entry.ItemID, Count: 1, MakeIndex: entry.MakeIndex, OwnerID: ch.ID, OwnerGroupID: ch.GroupOwnerID, PickupAt: now.Add(time.Duration(w.gameplay.Item.FloorItemCanPickUpMS) * time.Millisecond), Dura: entry.Dura, DuraMax: entry.DuraMax, Desc: entry.Desc, SourceCharacterID: ch.ID, SourceMapID: ch.MapID, SourceX: ch.X, SourceY: ch.Y}
		w.nextID++
		placed := w.placeReferenceDropsLocked(ch.MapID, ch.X, ch.Y, 2, []GroundDrop{drop}, blockers...)
		if len(placed) == 0 {
			continue
		}
		delete(ch.EquippedItems, slot)
		drops = append(drops, placed[0])
	}
	return drops
}

func (w *World) dropBagItemsOnDeathLocked(ch *storage.Character, now time.Time, blockers ...storage.Character) []GroundDrop {
	if ch == nil {
		return nil
	}
	if mapData, ok := w.data.Maps[ch.MapID]; ok && mapData.NoDropItem {
		return nil
	}
	drops := make([]GroundDrop, 0, len(ch.BagItems))
	dropAll := w.gameplay.Combat.DieRedScatterBagAll && characterPKLevel(*ch) >= 2
	for i := len(ch.BagItems) - 1; i >= 0; i-- {
		entry := ch.BagItems[i]
		if entry.ItemID == "" {
			continue
		}
		if !dropAll && w.rand.Intn(w.gameplay.Combat.DieScatterBagRate) != 0 {
			continue
		}
		if item, ok := w.data.Items[entry.ItemID]; ok && item.StdMode == 40 {
			if entry.Dura > 2000 {
				entry.Dura -= 2000
			} else {
				entry.Dura = 0
			}
			ch.BagItems[i] = entry
		}
		drop := GroundDrop{ID: fmt.Sprintf("drop-%d", w.nextID), MapID: ch.MapID, ItemID: entry.ItemID, Count: 1, MakeIndex: entry.MakeIndex, OwnerID: ch.ID, OwnerGroupID: ch.GroupOwnerID, PickupAt: now.Add(time.Duration(w.gameplay.Item.FloorItemCanPickUpMS) * time.Millisecond), Dura: entry.Dura, DuraMax: entry.DuraMax, Desc: entry.Desc, SourceCharacterID: ch.ID, SourceMapID: ch.MapID, SourceX: ch.X, SourceY: ch.Y}
		w.nextID++
		placed := w.placeReferenceDropsLocked(ch.MapID, ch.X, ch.Y, 3, []GroundDrop{drop}, blockers...)
		if len(placed) == 0 {
			continue
		}
		ch.BagItems = append(ch.BagItems[:i], ch.BagItems[i+1:]...)
		drops = append(drops, placed[0])
	}
	return drops
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func (w *World) settlePendingCharacterDeathsLocked(players []PlayerSnapshot, playersByID, updated map[string]storage.Character, result *TickResult, now time.Time) error {
	for _, player := range players {
		ch, ok := playersByID[player.Character.ID]
		if !ok {
			continue
		}
		_, pending := w.pendingCharacterDeaths[ch.ID]
		if !pending {
			if ch.HP > 0 {
				delete(w.finalizedCharacterDeaths, ch.ID)
				delete(w.characterDeathAt, ch.ID)
				continue
			}
			if _, finalized := w.finalizedCharacterDeaths[ch.ID]; finalized {
				continue
			}
		}
		delete(w.pendingCharacterDeaths, ch.ID)
		if ch.HP <= 0 {
			blockedRevival := false
			if hitter, ok := playersByID[ch.LastHitterID]; ok {
				blockedRevival = w.characterHasUnrevivalItemLocked(hitter)
			}
			if !blockedRevival {
				if revival, revived, err := w.reviveCharacterLocked(&ch, now); err != nil {
					return err
				} else if revived {
					playersByID[ch.ID] = ch
					updated[ch.ID] = ch
					delete(w.finalizedCharacterDeaths, ch.ID)
					delete(w.characterDeathAt, ch.ID)
					result.CharacterRevivals = append(result.CharacterRevivals, revival)
					result.OrderedSpellEvents = append(result.OrderedSpellEvents, OrderedSpellEvent{Kind: OrderedSpellEventCharacterRevival, CharacterRevival: revival})
					continue
				}
			}
			w.finalizedCharacterDeaths[ch.ID] = struct{}{}
			if w.characterDeathAt == nil {
				w.characterDeathAt = make(map[string]time.Time)
			}
			if _, recorded := w.characterDeathAt[ch.ID]; !recorded {
				w.characterDeathAt[ch.ID] = now
			}
			ch.IncHealth = 0
			ch.IncSpell = 0
			ch.IncHealing = 0
			if killer, ok := w.characterKillerForDeathLocked(ch, playersByID); ok && w.resolvePKDeathLocked(&ch, &killer) {
				playersByID[killer.ID] = killer
				updated[killer.ID] = killer
				messages := []PKDeathMessage{
					{Character: killer, Text: w.gameplay.Combat.PKMurderMessage},
					{Character: ch, Text: fmt.Sprintf(w.gameplay.Combat.PKKilledMessage, killer.Name)},
				}
				result.PKDeathMessages = append(result.PKDeathMessages, messages...)
				for _, message := range messages {
					result.OrderedSpellEvents = append(result.OrderedSpellEvents, OrderedSpellEvent{Kind: OrderedSpellEventPKDeathNotice, PKDeathNotice: message})
				}
			}
			blockers := make([]storage.Character, 0, len(playersByID))
			for _, blocker := range playersByID {
				blockers = append(blockers, blocker)
			}
			drops := w.settleCharacterDeathLocked(&ch, now, blockers...)
			result.CharacterDrops = append(result.CharacterDrops, drops...)
			for _, drop := range drops {
				result.OrderedSpellEvents = append(result.OrderedSpellEvents, OrderedSpellEvent{Kind: OrderedSpellEventCharacterDrop, CharacterDrop: drop})
			}
			groupEvent, err := w.settleDeadCharacterGroupLocked(&ch, playersByID)
			if err != nil {
				return err
			}
			if len(groupEvent.Updated) > 0 || len(groupEvent.Cancel) > 0 || groupEvent.MemberListOwnerID != "" {
				result.GroupSyncEvents = append(result.GroupSyncEvents, groupEvent)
				result.OrderedSpellEvents = append(result.OrderedSpellEvents, OrderedSpellEvent{Kind: OrderedSpellEventCharacterGroupSync, CharacterGroupSync: groupEvent})
			}
			ch.GroupOwnerID = ""
			playersByID[ch.ID] = ch
			updated[ch.ID] = ch
			result.CharacterDeaths = append(result.CharacterDeaths, ch)
			result.OrderedSpellEvents = append(result.OrderedSpellEvents, OrderedSpellEvent{Kind: OrderedSpellEventCharacterDeath, CharacterDeath: ch})
		}
	}
	for id := range w.pendingCharacterDeaths {
		if _, ok := playersByID[id]; !ok {
			delete(w.pendingCharacterDeaths, id)
		}
	}
	return nil
}

func (w *World) characterKillerForDeathLocked(victim storage.Character, playersByID map[string]storage.Character) (storage.Character, bool) {
	if killer, ok := playersByID[victim.LastHitterID]; ok {
		return killer, true
	}
	mon, ok := w.monsters[victim.LastHitterID]
	if !ok || mon == nil || mon.MasterID == "" {
		return storage.Character{}, false
	}
	killer, ok := playersByID[mon.MasterID]
	return killer, ok
}

func (w *World) clearCharacterLastHitterAfterRunLocked(ch storage.Character, playersByID, updated map[string]storage.Character, now time.Time) storage.Character {
	lastHitter, hitterPresent := playersByID[ch.LastHitterID]
	monsterHitter, monsterHitterPresent := w.monsters[ch.LastHitterID]
	monsterHitterDead := monsterHitterPresent && (monsterHitter.HP <= 0 || !monsterHitter.Alive || monsterHitter.Ghost)
	expHitter, expHitterPresent := playersByID[ch.ExpHitterID]
	expMonsterHitter, expMonsterHitterPresent := w.monsters[ch.ExpHitterID]
	expMonsterHitterDead := expMonsterHitterPresent && (expMonsterHitter.HP <= 0 || !expMonsterHitter.Alive || expMonsterHitter.Ghost)
	changed := false
	if ch.LastHitterID != "" && ch.LastHitterAt > 0 && (now.UnixNano()-ch.LastHitterAt > int64(30*time.Second) || hitterPresent && (w.characterDeadLocked(lastHitter) || lastHitter.Ghost) || monsterHitterDead) {
		ch.LastHitterID = ""
		ch.LastHitterAt = 0
		changed = true
	}
	if ch.ExpHitterID != "" && ch.ExpHitterAt > 0 && (now.UnixNano()-ch.ExpHitterAt > int64(6*time.Second) || expHitterPresent && (w.characterDeadLocked(expHitter) || expHitter.Ghost) || expMonsterHitterDead) {
		ch.ExpHitterID = ""
		ch.ExpHitterAt = 0
		changed = true
	}
	if changed {
		updated[ch.ID] = ch
	}
	return ch
}

func (w *World) clearCharacterTargetAfterRunLocked(ch storage.Character, playersByID map[string]storage.Character, updated map[string]storage.Character, now time.Time) storage.Character {
	if ch.TargetID == "" {
		return ch
	}
	if ch.TargetFocusAt == 0 {
		ch.TargetFocusAt = now.UnixNano()
		updated[ch.ID] = ch
	}
	targetMapID, targetX, targetY := "", 0, 0
	targetAlive := false
	if target, ok := playersByID[ch.TargetID]; ok {
		targetMapID, targetX, targetY = target.MapID, target.X, target.Y
		targetAlive = !w.characterDeadLocked(target) && !target.Ghost
	} else if mon := w.monsters[ch.TargetID]; mon != nil {
		targetMapID, targetX, targetY = mon.MapID, mon.X, mon.Y
		targetAlive = monsterTargetAliveLocked(mon)
	}
	if !targetAlive || targetMapID != ch.MapID || absInt(targetX-ch.X) > 15 || absInt(targetY-ch.Y) > 15 || now.Sub(time.Unix(0, ch.TargetFocusAt)) > 30*time.Second {
		ch.TargetID = ""
		ch.TargetFocusAt = 0
		updated[ch.ID] = ch
	}
	return ch
}

func setCharacterTargetLocked(ch *storage.Character, targetID string, now time.Time) {
	if ch == nil {
		return
	}
	ch.TargetID = targetID
	if targetID == "" {
		ch.TargetFocusAt = 0
		return
	}
	ch.TargetFocusAt = now.UnixNano()
}

func (w *World) applyCharacterPKAndGroupMaintenanceLocked(ch storage.Character, playersByID, updated map[string]storage.Character, result *TickResult, now time.Time) storage.Character {
	if ch.PKPointDecayAt == 0 {
		ch.PKPointDecayAt = now.UnixMilli()
	} else {
		decayInterval := w.gameplay.Combat.PKPointDecayTimeMS
		if decayInterval <= 0 {
			decayInterval = 120000
		}
		decayCount := w.gameplay.Combat.PKPointDecayCount
		if decayCount <= 0 {
			decayCount = 1
		}
		if now.UnixMilli()-ch.PKPointDecayAt > int64(decayInterval) {
			ch.PKPointDecayAt = now.UnixMilli()
			updated[ch.ID] = ch
			if ch.PKPoint > 0 {
				oldLevel := characterPKLevel(ch)
				ch.PKPoint -= decayCount
				if ch.PKPoint < 0 {
					ch.PKPoint = 0
				}
				updated[ch.ID] = ch
				if oldLevel > 0 && oldLevel <= 2 && characterPKLevel(ch) != oldLevel {
					result.NameColorCharacters = append(result.NameColorCharacters, ch)
					result.OrderedSpellEvents = append(result.OrderedSpellEvents, OrderedSpellEvent{Kind: OrderedSpellEventCharacterNameColor, Character: ch})
				}
			}
		}
	}
	if ch.PKFlag && ch.PKFlagUntil > 0 && now.UnixNano() > ch.PKFlagUntil {
		ch.PKFlag = false
		ch.PKFlagUntil = 0
		updated[ch.ID] = ch
		result.NameColorCharacters = append(result.NameColorCharacters, ch)
		result.OrderedSpellEvents = append(result.OrderedSpellEvents, OrderedSpellEvent{Kind: OrderedSpellEventCharacterNameColor, Character: ch})
	}
	lastVerifyAt := w.groupVerifyAt[ch.ID]
	if lastVerifyAt == 0 {
		if w.groupVerifyAt == nil {
			w.groupVerifyAt = make(map[string]int64)
		}
		w.groupVerifyAt[ch.ID] = now.UnixNano()
		return ch
	}
	if now.Sub(time.Unix(0, lastVerifyAt)) > 30*time.Second {
		if w.groupVerifyAt == nil {
			w.groupVerifyAt = make(map[string]int64)
		}
		w.groupVerifyAt[ch.ID] = now.UnixNano()
		if ch.GroupOwnerID != "" {
			if owner, ok := playersByID[ch.GroupOwnerID]; !ok || w.characterDeadLocked(owner) || owner.Ghost {
				ch.GroupOwnerID = ""
				updated[ch.ID] = ch
			}
		}
		if ch.GroupOwnerID == ch.ID && len(ch.GroupMembers) > 0 {
			members := ch.GroupMembers[:0]
			for _, memberID := range ch.GroupMembers {
				member, ok := playersByID[memberID]
				if ok && (w.characterDeadLocked(member) || member.Ghost) {
					continue
				}
				members = append(members, memberID)
			}
			if len(members) != len(ch.GroupMembers) {
				ch.GroupMembers = members
				updated[ch.ID] = ch
			}
		}
	}
	return ch
}

func (w *World) characterInCastleWarAreaLocked(ch storage.Character) bool {
	castle, ok := w.castleSnapshotLocked(w.data.Castle.ID)
	if !ok || !castle.UnderWar {
		return false
	}
	castleData := w.data.Castle
	if ch.MapID == castleData.CastleMap {
		return absInt(castleData.HomeX-ch.X) < castleData.WarRangeX && absInt(castleData.HomeY-ch.Y) < castleData.WarRangeY
	}
	if ch.MapID == castleData.PalaceMap || ch.MapID == castleData.SecretMap {
		return true
	}
	for _, mapID := range castleData.WarMaps {
		if ch.MapID == mapID {
			return true
		}
	}
	return false
}

func (w *World) applyPendingMonsterAttackCharacterLocked(result *TickResult, playersByID, updated map[string]storage.Character, now time.Time, pending pendingMonsterAttack, characterID string) (bool, error) {
	mon := w.monsters[pending.MonsterID]
	var targetID string
	var target pendingMonsterTarget
	if len(pending.Targets) > 0 {
		target = pending.Targets[0]
		if target.Monster || target.ID != characterID {
			return false, nil
		}
		targetID = target.ID
	} else if len(pending.TargetIDs) > 0 {
		targetID = pending.TargetIDs[0]
		target.ID = targetID
		target.Damage = pending.Damage
	} else {
		return false, nil
	}
	if mon == nil || (!target.ReferenceDelay && !target.DirectMagicStrike && !monsterTargetAliveLocked(mon)) {
		return true, nil
	}
	character, ok := playersByID[targetID]
	if !ok || character.ID == "" {
		return true, nil
	}
	if !target.ReferenceDelay && !target.DirectMagicStrike && !w.monsterCanKeepCharacterTargetLocked(mon, character) {
		return true, nil
	}
	if target.ReferenceDelay || target.DirectMagicStrike {
		damage := target.Damage
		damage = w.characterMagicDamageAfterDefenseLocked(character, damage, now)
		damage = applyCharacterMagicBubbleLocked(&character, damage, now)
		if damage <= 0 {
			return true, nil
		}
		if target.ReferenceDelay {
			mon.TargetCharacterID = targetID
			mon.TargetMonsterID = ""
			mon.TargetFocusAt = now
			if target.TargetRange >= 0 && (abs(character.X-target.TargetX) > target.TargetRange || abs(character.Y-target.TargetY) > target.TargetRange) {
				return true, nil
			}
		}
		lastHitterID := character.LastHitterID
		lastHitterAt := character.LastHitterAt
		expHitterID := character.ExpHitterID
		expHitterAt := character.ExpHitterAt
		healthTick := character.HealthTick
		spellTick := character.SpellTick
		perHealth := character.PerHealth
		perSpell := character.PerSpell
		updatedCharacter, hit, err := w.attackCharacterDirectMagicDamageAtLocked(storage.Character{ID: mon.ID}, character, damage, now)
		if err != nil {
			return true, err
		}
		updatedCharacter.LastHitterID = lastHitterID
		updatedCharacter.LastHitterAt = lastHitterAt
		updatedCharacter.ExpHitterID = expHitterID
		updatedCharacter.ExpHitterAt = expHitterAt
		updatedCharacter.HealthTick = healthTick
		updatedCharacter.SpellTick = spellTick
		updatedCharacter.PerHealth = perHealth
		updatedCharacter.PerSpell = perSpell
		playersByID[targetID] = updatedCharacter
		updated[targetID] = updatedCharacter
		hit.AttackerActor = MonsterActorID(*mon)
		hit.ImpactDelay = pending.ImpactDelay
		hit.Magic = true
		if hit.Damage > 0 {
			result.CharacterHits = append(result.CharacterHits, hit)
			result.OrderedSpellEvents = append(result.OrderedSpellEvents, OrderedSpellEvent{Kind: OrderedSpellEventCharacterHit, CharacterHit: hit})
		}
		return true, nil
	}
	if target.PhysicalDamage > 0 || target.MagicDamage > 0 {
		physical := w.characterPhysicalDamageAfterDefenseAtLocked(&character, target.PhysicalDamage, now)
		magical := w.characterMagicDamageAfterDefenseLocked(character, target.MagicDamage, now)
		magical = applyCharacterMagicBubbleLocked(&character, magical, now)
		if physical+magical <= 0 {
			return true, nil
		}
		if target.ReferenceDelay {
			mon.TargetCharacterID = targetID
			mon.TargetMonsterID = ""
			mon.TargetFocusAt = now
		}
		if target.ReferenceDelay && target.TargetRange >= 0 && (abs(character.X-target.TargetX) > target.TargetRange || abs(character.Y-target.TargetY) > target.TargetRange) {
			return true, nil
		}
		updatedCharacter, hit, err := w.attackCharacterDirectDamageAtLocked(storage.Character{ID: mon.ID}, character, physical+magical, now)
		if err != nil {
			return true, err
		}
		playersByID[targetID] = updatedCharacter
		updated[targetID] = updatedCharacter
		hit.ImpactDelay = pending.ImpactDelay
		if hit.Damage > 0 {
			result.CharacterHits = append(result.CharacterHits, hit)
			result.OrderedSpellEvents = append(result.OrderedSpellEvents, OrderedSpellEvent{Kind: OrderedSpellEventCharacterHit, CharacterHit: hit})
		}
		return true, nil
	}
	oldMagic := mon.UseMagic
	mon.UseMagic = true
	updatedCharacter, hit, err := w.monsterAttackCharacterWithDamageAtLocked(mon, character, target.Damage, now)
	mon.UseMagic = oldMagic
	if err != nil {
		return true, err
	}
	playersByID[targetID] = updatedCharacter
	updated[targetID] = updatedCharacter
	if pending.ImpactDelay > 0 {
		hit.ImpactDelay = pending.ImpactDelay
		hit.Magic = true
	}
	if hit.Damage > 0 {
		result.CharacterHits = append(result.CharacterHits, hit)
		result.OrderedSpellEvents = append(result.OrderedSpellEvents, OrderedSpellEvent{Kind: OrderedSpellEventCharacterHit, CharacterHit: hit})
	}
	return true, nil
}

func (w *World) applyPendingMonsterAttacksForCharacterLocked(result *TickResult, playersByID, updated map[string]storage.Character, now time.Time, characterID string, attacks []pendingMonsterAttack) ([]pendingMonsterAttack, error) {
	remaining := make([]pendingMonsterAttack, 0, len(attacks))
	for _, pending := range attacks {
		consumed, err := w.applyPendingMonsterAttackCharacterLocked(result, playersByID, updated, now, pending, characterID)
		if err != nil {
			return nil, err
		}
		if !consumed {
			remaining = append(remaining, pending)
		}
	}
	return remaining, nil
}

func pendingMonsterAttackTargetsCharacter(pending pendingMonsterAttack, characterID string) bool {
	if len(pending.Targets) > 0 {
		target := pending.Targets[0]
		return !target.Monster && target.ID == characterID
	}
	return len(pending.TargetIDs) > 0 && pending.TargetIDs[0] == characterID
}

func pendingEventBefore(leftOrder, rightOrder uint64, leftAttack bool) bool {
	if leftOrder != 0 && rightOrder != 0 {
		return leftOrder < rightOrder
	}
	if leftOrder == 0 && rightOrder == 0 {
		return false
	}
	if leftOrder != 0 {
		return true
	}
	if rightOrder != 0 {
		return false
	}
	return leftAttack
}

func pendingOrderBefore(leftOrder, rightOrder uint64) bool {
	if leftOrder == 0 {
		return false
	}
	if rightOrder == 0 {
		return true
	}
	return leftOrder < rightOrder
}

func pendingAttackWins(attackIndex, spellIndex int, attackOrder, spellOrder uint64) bool {
	if spellIndex < 0 {
		return attackIndex >= 0
	}
	if attackIndex < 0 {
		return false
	}
	if attackOrder == 0 && spellOrder == 0 {
		return true
	}
	return pendingEventBefore(attackOrder, spellOrder, true)
}

func (w *World) applyOnePendingSpellForCharacterLocked(result *TickResult, players, updated map[string]storage.Character, now time.Time, characterID string, index int) error {
	selected := w.pendingSpells[index]
	rest := make([]pendingSpell, 0, len(w.pendingSpells)-1)
	rest = append(rest, w.pendingSpells[:index]...)
	rest = append(rest, w.pendingSpells[index+1:]...)
	w.pendingSpells = []pendingSpell{selected}
	err := w.applyPendingSpellTicksForCharacterLocked(result, players, updated, now, characterID)
	processed := w.pendingSpells
	w.pendingSpells = append(rest, processed...)
	return err
}

func (w *World) applyPendingCharacterEventsLocked(result *TickResult, playersByID, updated map[string]storage.Character, now time.Time, characterID string, attacks []pendingMonsterAttack) ([]pendingMonsterAttack, error) {
	for {
		attackIndex := -1
		var attackOrder uint64
		for i, pending := range attacks {
			if now.Before(pending.DueAt) || !pendingMonsterAttackTargetsCharacter(pending, characterID) {
				continue
			}
			if attackIndex < 0 || pendingEventBefore(pending.EnqueueOrder, attackOrder, true) {
				attackIndex = i
				attackOrder = pending.EnqueueOrder
			}
		}
		spellIndex := -1
		var spellOrder uint64
		for i, pending := range w.pendingSpells {
			if pending.TargetCharacterID != characterID || now.Before(pending.DueAt) {
				continue
			}
			if spellIndex < 0 || pendingEventBefore(pending.EnqueueOrder, spellOrder, false) {
				spellIndex = i
				spellOrder = pending.EnqueueOrder
			}
		}
		if attackIndex < 0 && spellIndex < 0 {
			return attacks, nil
		}
		if pendingAttackWins(attackIndex, spellIndex, attackOrder, spellOrder) {
			consumed, err := w.applyPendingMonsterAttackCharacterLocked(result, playersByID, updated, now, attacks[attackIndex], characterID)
			if err != nil {
				return nil, err
			}
			if consumed {
				attacks = append(attacks[:attackIndex], attacks[attackIndex+1:]...)
			}
			continue
		}
		if err := w.applyOnePendingSpellForCharacterLocked(result, playersByID, updated, now, characterID, spellIndex); err != nil {
			return nil, err
		}
	}
}

func (w *World) Tick(players []PlayerSnapshot, now time.Time) (TickResult, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for id, disconnected := range w.disconnectedCharacters {
		if now.After(disconnected.Until) {
			delete(w.disconnectedCharacters, id)
		}
	}
	if w.characterNextRunAt == nil {
		w.characterNextRunAt = make(map[string]time.Time)
	}
	if w.characterCastleCheckAt == nil {
		w.characterCastleCheckAt = make(map[string]time.Time)
	}
	players = orderedPlayerSnapshots(players)
	previousActionNow := w.actionNow
	w.actionNow = now
	defer func() { w.actionNow = previousActionNow }()
	previousCharacterProcessOrders := w.tickCharacterProcessOrders
	w.tickCharacterProcessOrders = make(map[string]uint64, len(players))
	for _, player := range players {
		if player.Character.ID != "" && player.ProcessOrder != 0 {
			w.tickCharacterProcessOrders[player.Character.ID] = player.ProcessOrder
		}
	}
	defer func() { w.tickCharacterProcessOrders = previousCharacterProcessOrders }()
	w.monsterBehaviorHits = w.monsterBehaviorHits[:0]
	w.monsterBehaviorGroundHits = w.monsterBehaviorGroundHits[:0]
	w.monsterBehaviorGroundHitStart = 0
	w.monsterBehaviorStatuses = w.monsterBehaviorStatuses[:0]
	w.monsterBehaviorCharacterStatuses = w.monsterBehaviorCharacterStatuses[:0]
	w.monsterBehaviorPoisonNotifications = w.monsterBehaviorPoisonNotifications[:0]
	previousMonsterHealingOrder := w.monsterHealingOrder
	w.monsterHealingOrder = make(map[string]uint64)
	defer func() { w.monsterHealingOrder = previousMonsterHealingOrder }()
	result := TickResult{CharacterStageProcessed: true}
	updated := map[string]storage.Character{}
	playersByID := map[string]storage.Character{}
	for _, player := range players {
		ch := player.Character
		if ch.ID == "" {
			continue
		}
		playersByID[ch.ID] = ch
	}
	remainingAttacks := w.pendingMonsterAttacks[:0]
	dueAttacks := make([]pendingMonsterAttack, 0)
	skippedCharacterIDs := make(map[string]struct{})
	for _, pending := range w.pendingMonsterAttacks {
		if now.Before(pending.DueAt) {
			remainingAttacks = append(remainingAttacks, pending)
			continue
		}
		mon := w.monsters[pending.MonsterID]
		if mon == nil {
			continue
		}
		attackerAlive := monsterTargetAliveLocked(mon)
		if len(pending.Targets) > 0 {
			for _, pendingTarget := range pending.Targets {
				if !attackerAlive && !pendingTarget.ReferenceDelay && !pendingTarget.DirectMagicStrike && !pendingTarget.StruckOnly {
					continue
				}
				if pendingTarget.Monster {
					dueAttacks = append(dueAttacks, pendingMonsterAttack{EnqueueOrder: pending.EnqueueOrder, DueAt: pending.DueAt, MonsterID: pending.MonsterID, StruckCharacter: pending.StruckCharacter, StruckPlayers: pending.StruckPlayers, MonsterTargets: []pendingMonsterTarget{pendingTarget}, Damage: pending.Damage, ImpactDelay: pending.ImpactDelay})
					continue
				}
				dueAttacks = append(dueAttacks, pendingMonsterAttack{EnqueueOrder: pending.EnqueueOrder, DueAt: pending.DueAt, MonsterID: pending.MonsterID, Targets: []pendingMonsterTarget{pendingTarget}, Damage: pending.Damage, ImpactDelay: pending.ImpactDelay})
			}
			continue
		}
		if attackerAlive {
			for _, targetID := range pending.TargetIDs {
				dueAttacks = append(dueAttacks, pendingMonsterAttack{EnqueueOrder: pending.EnqueueOrder, DueAt: pending.DueAt, MonsterID: pending.MonsterID, TargetIDs: []string{targetID}, Damage: pending.Damage, ImpactDelay: pending.ImpactDelay})
			}
		}
		for _, pendingTarget := range pending.MonsterTargets {
			if !attackerAlive && !pendingTarget.ReferenceDelay && !pendingTarget.DirectMagicStrike && !pendingTarget.StruckOnly {
				continue
			}
			dueAttacks = append(dueAttacks, pendingMonsterAttack{EnqueueOrder: pending.EnqueueOrder, DueAt: pending.DueAt, MonsterID: pending.MonsterID, StruckCharacter: pending.StruckCharacter, StruckPlayers: pending.StruckPlayers, MonsterTargets: []pendingMonsterTarget{pendingTarget}, Damage: pending.Damage, ImpactDelay: pending.ImpactDelay})
		}
	}
	w.pendingMonsterAttacks = remainingAttacks
	characterProcessStarted := time.Now()
	processBudget := w.characterProcessBudget
	if processBudget <= 0 {
		processBudget = characterProcessBudget
	}
	characterProcessCursor := w.characterProcessCursor
	characterStageComplete := true
	if characterProcessCursor >= len(players) && len(players) > 0 {
		w.characterProcessCursor = 0
		for _, player := range players {
			if player.Character.ID != "" {
				skippedCharacterIDs[player.Character.ID] = struct{}{}
			}
		}
	} else {
		for playerIndex := characterProcessCursor; playerIndex < len(players); playerIndex++ {
			processed := playerIndex - characterProcessCursor
			if processed > 0 && time.Since(characterProcessStarted) > processBudget {
				characterStageComplete = false
				w.characterProcessCursor = playerIndex
				break
			}
			player := players[playerIndex]
			ch, ok := playersByID[player.Character.ID]
			if !ok {
				continue
			}
			if nextRunAt, exists := w.characterNextRunAt[ch.ID]; exists && !now.After(nextRunAt) {
				skippedCharacterIDs[ch.ID] = struct{}{}
				continue
			}
			w.characterNextRunAt[ch.ID] = now.Add(characterRunInterval)
			originalTargetID := ch.TargetID
			eventStart := len(result.OrderedSpellEvents)
			castleFreePKAreaChanged := false
			castleFreePKArea := w.characterInCastleWarAreaLocked(ch)
			lastCastleCheck, castleCheckExists := w.characterCastleCheckAt[ch.ID]
			castleCheckDue := !castleCheckExists || now.Sub(lastCastleCheck) > time.Second
			if castleCheckDue {
				w.characterCastleCheckAt[ch.ID] = now
			}
			if castleFreePKArea || castleCheckDue {
				if ch.FreePKArea != castleFreePKArea {
					ch.FreePKArea = castleFreePKArea
					castleFreePKAreaChanged = true
					playersByID[ch.ID] = ch
					updated[ch.ID] = ch
				}
			}
			if lastSearch, exists := w.characterSearchAt[ch.ID]; !exists {
				if w.characterSearchAt == nil {
					w.characterSearchAt = make(map[string]time.Time)
				}
				w.characterSearchAt[ch.ID] = now
				if w.characterGameTimes == nil {
					w.characterGameTimes = make(map[string]byte)
				}
				if _, exists := w.characterGameTimes[ch.ID]; !exists {
					w.characterGameTimes[ch.ID] = GameTime(now)
				}
			} else if now.Sub(lastSearch) > time.Second {
				w.characterSearchAt[ch.ID] = now
				gameTime := GameTime(now)
				if previousGameTime, exists := w.characterGameTimes[ch.ID]; !exists {
					if w.characterGameTimes == nil {
						w.characterGameTimes = make(map[string]byte)
					}
					w.characterGameTimes[ch.ID] = gameTime
				} else if previousGameTime != gameTime {
					w.characterGameTimes[ch.ID] = gameTime
					result.OrderedSpellEvents = append(result.OrderedSpellEvents, OrderedSpellEvent{
						Kind:      OrderedSpellEventCharacterDayChanging,
						Character: ch,
						GameTime:  gameTime,
						DayBright: byte(w.mapDayBrightLocked(ch.MapID, gameTime)),
					})
				}
			}
			if w.characterDuplicateCheckAt == nil {
				w.characterDuplicateCheckAt = make(map[string]time.Time)
			}
			if w.characterDuplicateAt == nil {
				w.characterDuplicateAt = make(map[string]time.Time)
			}
			lastDuplicateCheck, duplicateCheckExists := w.characterDuplicateCheckAt[ch.ID]
			if !duplicateCheckExists {
				w.characterDuplicateCheckAt[ch.ID] = now
			} else if now.Sub(lastDuplicateCheck) > 3*time.Second {
				w.characterDuplicateCheckAt[ch.ID] = now
				if w.syncCharacterHomeFromStartPointLocked(&ch) {
					playersByID[ch.ID] = ch
					updated[ch.ID] = ch
				}
				characters := characterValues(playersByID)
				duplicateCount := w.movingObjectCountAtLocked(characters, ch.MapID, ch.X, ch.Y)
				if duplicateCount >= 2 {
					if _, exists := w.characterDuplicateAt[ch.ID]; !exists {
						w.characterDuplicateAt[ch.ID] = now
					}
				} else {
					delete(w.characterDuplicateAt, ch.ID)
				}
				if duplicateAt, exists := w.characterDuplicateAt[ch.ID]; exists && now.Sub(duplicateAt) < 20*time.Second && ((duplicateCount >= 3 && now.Sub(duplicateAt) > 3*time.Second) || (duplicateCount == 2 && now.Sub(duplicateAt) > 10*time.Second)) {
					if mp, exists := w.data.Maps[ch.MapID]; exists {
						occupied := w.occupiedMovingObjectsLocked(characters)
						pushed, pushes, moved, err := w.pushCharacterInDirectionLocked(ch, 1, w.rand.Intn(8), mp, occupied)
						if err != nil {
							return TickResult{}, err
						}
						if moved {
							ch = pushed
							playersByID[ch.ID] = ch
							updated[ch.ID] = ch
							for _, push := range pushes {
								result.OrderedSpellEvents = append(result.OrderedSpellEvents, OrderedSpellEvent{Kind: OrderedSpellEventCharacterPush, CharacterPush: push})
							}
						}
					}
				}
			}
			var err error
			dueAttacks, err = w.applyPendingCharacterEventsLocked(&result, playersByID, updated, now, ch.ID, dueAttacks)
			if err != nil {
				return TickResult{}, err
			}
			ch, ok = playersByID[player.Character.ID]
			if !ok {
				continue
			}
			_, finalized := w.finalizedCharacterDeaths[ch.ID]
			if finalized && !ch.Ghost {
				if deathAt, recorded := w.characterDeathAt[ch.ID]; recorded && now.Sub(deathAt) > w.characterMakeGhostTime() {
					ch.Ghost = true
					playersByID[ch.ID] = ch
					updated[ch.ID] = ch
					result.CharacterGhosts = append(result.CharacterGhosts, ch)
				}
			}
			statusCharacter := ch
			statusCharacter.ShowHPUntil = 0
			originalStatus := characterStatus(statusCharacter, now, true)
			var next storage.Character
			var changed bool
			abilityRefreshQueued := false
			if finalized || ch.HP <= 0 {
				if w.advanceCharacterNaturalSpellClockLocked(&ch, now) {
					updated[ch.ID] = ch
				}
			}
			if !finalized && ch.HP > 0 {
				naturalMPChanged, healthSpellChanges := w.applyCharacterNaturalSpellTickTraceLocked(&ch, now)
				result.OrderedHealthSpellChanges = append(result.OrderedHealthSpellChanges, healthSpellChanges...)
				for _, character := range healthSpellChanges {
					result.OrderedSpellEvents = append(result.OrderedSpellEvents, OrderedSpellEvent{Kind: OrderedSpellEventCharacterHealth, Character: character})
				}
				if naturalMPChanged {
					updated[ch.ID] = ch
				}
			}
			playersByID[ch.ID] = ch
			if err := w.settlePendingCharacterDeathsLocked([]PlayerSnapshot{{Character: ch}}, playersByID, updated, &result, now); err != nil {
				return TickResult{}, err
			}
			ch, ok = playersByID[player.Character.ID]
			if !ok {
				continue
			}
			_, finalized = w.finalizedCharacterDeaths[ch.ID]
			if !finalized {
				next, changed = core.ApplyQueuedRecovery(ch, now)
				if next.IncHealthSpellAt != ch.IncHealthSpellAt {
					result.RuntimeCharacters = append(result.RuntimeCharacters, next)
				}
				if changed {
					result.OrderedHealthSpellChanges = append(result.OrderedHealthSpellChanges, next)
					result.OrderedSpellEvents = append(result.OrderedSpellEvents, OrderedSpellEvent{Kind: OrderedSpellEventCharacterHealth, Character: next})
				}
				ch = next
				if changed {
					updated[ch.ID] = ch
				}
			}
			if !finalized && w.applyCharacterNegativeHealthTickLocked(&ch) {
				updated[ch.ID] = ch
				result.OrderedHealthSpellChanges = append(result.OrderedHealthSpellChanges, ch)
				result.OrderedSpellEvents = append(result.OrderedSpellEvents, OrderedSpellEvent{Kind: OrderedSpellEventCharacterHealth, Character: ch})
			}
			if ch.TargetID != "" && (ch.TargetID != originalTargetID || ch.TargetFocusAt == 0) {
				ch.TargetFocusAt = now.UnixNano()
				updated[ch.ID] = ch
			}
			ch = w.clearCharacterTargetAfterRunLocked(ch, playersByID, updated, now)
			ch = w.clearCharacterLastHitterAfterRunLocked(ch, playersByID, updated, now)
			playersByID[ch.ID] = ch
			next, changed = w.applyCharacterShowHPOpenTickLocked(ch, now)
			if changed {
				ch = next
				updated[ch.ID] = ch
				result.ShowHPOpenedCharacters = append(result.ShowHPOpenedCharacters, ch)
				result.OrderedSpellEvents = append(result.OrderedSpellEvents, OrderedSpellEvent{Kind: OrderedSpellEventCharacterOpenHealth, Character: ch})
			}
			next, changed = w.applyCharacterShowHPTickLocked(ch, now)
			if changed {
				ch = next
				updated[ch.ID] = ch
				if ch.ShowHPUntil == 0 {
					result.ShowHPExpiredCharacters = append(result.ShowHPExpiredCharacters, ch)
					result.OrderedSpellEvents = append(result.OrderedSpellEvents, OrderedSpellEvent{Kind: OrderedSpellEventCharacterCloseHealth, Character: ch})
				}
			}
			ch = w.applyCharacterPKAndGroupMaintenanceLocked(ch, playersByID, updated, &result, now)
			playersByID[ch.ID] = ch
			next, changed = w.applyCharacterStealthTickLocked(ch, now)
			if changed {
				ch = next
				updated[ch.ID] = ch
			}
			next, changed = w.applyCharacterProtectionTickLocked(ch, now)
			if changed {
				defenceWasActive := player.Character.DefenceUpUntil > 0
				magicDefenceWasActive := player.Character.MagDefenceUpUntil > 0
				ch = next
				defenceIsActive := ch.DefenceUpUntil > 0
				magicDefenceIsActive := ch.MagDefenceUpUntil > 0
				updated[ch.ID] = ch
				if defenceWasActive && !defenceIsActive {
					result.OrderedSpellEvents = append(result.OrderedSpellEvents, OrderedSpellEvent{Kind: OrderedSpellEventCharacterNotice, CharacterNotice: CharacterNotice{Character: ch, Text: "防御力恢复正常"}})
					abilityRefreshQueued = true
				}
				if magicDefenceWasActive && !magicDefenceIsActive {
					result.OrderedSpellEvents = append(result.OrderedSpellEvents, OrderedSpellEvent{Kind: OrderedSpellEventCharacterNotice, CharacterNotice: CharacterNotice{Character: ch, Text: "魔法防御力恢复正常"}})
					abilityRefreshQueued = true
				}
			}
			if ch.ParalyzedUntil > 0 {
				until, active, clockChanged := w.advanceCharacterStatusClockLocked(ch.ID, statusClockParalyzed, ch.ParalyzedUntil, now)
				if !active {
					ch.ParalyzedUntil = 0
					updated[ch.ID] = ch
				} else if clockChanged {
					ch.ParalyzedUntil = until
					updated[ch.ID] = ch
				}
			}
			next, changed = w.applyCharacterTemporaryAbilityTickLocked(ch, now)
			if changed {
				temporaryAbilityNotices := [...]string{
					"Removed temporarily increased destructive power.",
					"Removed temporarily increased magic power.",
					"Removed temporarily increased zen power.",
					"Removed temporarily increased hitting speed.",
					"Removed temporarily increased HP.",
					"Removed temporarily increased MP.",
				}
				for i := 0; i < len(temporaryAbilityNotices) && i < len(ch.ExtraAbilTimes); i++ {
					if ch.ExtraAbilTimes[i] > 0 && next.ExtraAbilTimes[i] == 0 {
						result.OrderedSpellEvents = append(result.OrderedSpellEvents, OrderedSpellEvent{Kind: OrderedSpellEventCharacterNotice, CharacterNotice: CharacterNotice{Character: ch, Text: temporaryAbilityNotices[i]}})
					}
				}
				ch = next
				updated[ch.ID] = ch
				abilityRefreshQueued = true
			}
			previousHP, previousMP := ch.HP, ch.MP
			poisonHealthChanged := false
			next, changed = w.applyCharacterPoisonTickLocked(ch, now)
			if changed {
				ch = next
				if ch.HP != previousHP || ch.MP != previousMP {
					poisonHealthChanged = true
				}
				if player.Character.HP > 0 && ch.HP <= 0 {
					ch.IncHealth = 0
					ch.IncSpell = 0
					ch.IncHealing = 0
					w.deferCharacterDeathLocked(ch)
				}
				updated[ch.ID] = ch
			}
			statusCharacter = ch
			statusCharacter.ShowHPUntil = 0
			if originalStatus != characterStatus(statusCharacter, now, true) {
				result.StatusRefreshCharacters = append(result.StatusRefreshCharacters, ch)
				result.OrderedSpellEvents = append(result.OrderedSpellEvents, OrderedSpellEvent{
					Kind:      OrderedSpellEventCharacterStatus,
					Character: ch,
				})
			}
			if abilityRefreshQueued {
				result.AbilityRefreshCharacters = append(result.AbilityRefreshCharacters, ch)
				result.OrderedSpellEvents = append(result.OrderedSpellEvents, OrderedSpellEvent{Kind: OrderedSpellEventCharacterAbility, Character: ch})
			}
			if poisonHealthChanged {
				result.OrderedHealthSpellChanges = append(result.OrderedHealthSpellChanges, ch)
				result.OrderedSpellEvents = append(result.OrderedSpellEvents, OrderedSpellEvent{Kind: OrderedSpellEventCharacterHealth, Character: ch})
			}
			if castleFreePKAreaChanged {
				result.NameColorCharacters = append(result.NameColorCharacters, ch)
				result.OrderedSpellEvents = append(result.OrderedSpellEvents, OrderedSpellEvent{Kind: OrderedSpellEventCharacterNameColor, Character: ch})
			}
			playersByID[ch.ID] = ch
			result.OrderedSpellEvents = append(result.OrderedSpellEvents, OrderedSpellEvent{Kind: OrderedSpellEventCharacterSnapshot, CharacterSnapshot: ch})
			result.RunCharacters = append(result.RunCharacters, ch)
			result.CharacterRuns = append(result.CharacterRuns, CharacterRunResult{
				Character:  ch,
				EventStart: eventStart,
				EventEnd:   len(result.OrderedSpellEvents),
			})
			if time.Since(characterProcessStarted) > processBudget {
				characterStageComplete = false
				w.characterProcessCursor = playerIndex + 1
				break
			}
		}
	}
	if characterStageComplete {
		w.characterProcessCursor = 0
	}
	if len(skippedCharacterIDs) > 0 {
		remainingDueAttacks := dueAttacks[:0]
		for _, pending := range dueAttacks {
			requeued := false
			if len(pending.Targets) > 0 {
				target := pending.Targets[0]
				if !target.Monster {
					_, requeued = skippedCharacterIDs[target.ID]
				}
			} else if len(pending.TargetIDs) > 0 {
				_, requeued = skippedCharacterIDs[pending.TargetIDs[0]]
			}
			if requeued {
				w.pendingMonsterAttacks = append(w.pendingMonsterAttacks, pending)
				continue
			}
			remainingDueAttacks = append(remainingDueAttacks, pending)
		}
		dueAttacks = remainingDueAttacks
	}
	for id := range w.pendingCharacterDeaths {
		if _, ok := playersByID[id]; !ok {
			delete(w.pendingCharacterDeaths, id)
		}
	}
	for id := range w.characterNextRunAt {
		if _, ok := playersByID[id]; !ok {
			delete(w.characterNextRunAt, id)
		}
	}
	for id := range w.characterDuplicateCheckAt {
		if _, ok := playersByID[id]; !ok {
			delete(w.characterDuplicateCheckAt, id)
		}
	}
	for id := range w.characterDuplicateAt {
		if _, ok := playersByID[id]; !ok {
			delete(w.characterDuplicateAt, id)
		}
	}
	monsterPlayers := make([]PlayerSnapshot, 0, len(players))
	for _, player := range players {
		if ch, ok := playersByID[player.Character.ID]; ok {
			player.Character = ch
		}
		monsterPlayers = append(monsterPlayers, player)
	}
	monsterStageReady := w.monsterProcessNextAt.IsZero() || now.After(w.monsterProcessNextAt)
	if monsterStageReady {
		w.monsterProcessNextAt = now.Add(monsterStageInterval)
		w.processMonsterLifecycleLocked(now, monsterPlayers, &result)
	}
	for _, player := range players {
		if ch, ok := playersByID[player.Character.ID]; ok {
			result.PreMonsterCharacters = append(result.PreMonsterCharacters, ch)
		}
	}
	if monsterStageReady {
		monsterProcessStarted := time.Now()
		processedMonsterIDs := make(map[string]struct{})
		processedMonsterEntry := false
		settleMonsterDeath := func(mon *Monster) error {
			killerID := mon.ExpHitterID
			if killerID == "" {
				killerID = mon.LastHitterID
			}
			killer, killerOK := w.monsterExperienceOwnerLocked(killerID, playersByID, now)
			if !killerOK {
				killer = storage.Character{}
			}
			if slave := w.monsters[killerID]; slave != nil && slave.MasterID != "" {
				if w.gainMonsterSlaveExpLocked(slave, mon.Level) {
					result.NameColorMonsters = append(result.NameColorMonsters, *slave)
					result.NameMonsters = append(result.NameMonsters, *slave)
					result.OrderedMonsterEvents = append(result.OrderedMonsterEvents,
						OrderedMonsterEvent{Kind: OrderedMonsterEventNameColor, Monster: *slave},
						OrderedMonsterEvent{Kind: OrderedMonsterEventUsername, Monster: *slave})
				}
			}
			death, experienceEvents, err := w.killMonsterWithDamageAtAndPlayersLocked(killer, mon, 0, now, playersByID)
			if err != nil {
				return err
			}
			result.MonsterDeaths = append(result.MonsterDeaths, death)
			if death.Character.ID != "" {
				playersByID[death.Character.ID] = death.Character
				updated[death.Character.ID] = death.Character
			}
			for _, experience := range experienceEvents {
				if experience.Character.ID != "" {
					playersByID[experience.Character.ID] = experience.Character
					updated[experience.Character.ID] = experience.Character
				}
				if experience.Experience > 0 || experience.LevelUp {
					result.SpellExperience = append(result.SpellExperience, experience)
					result.OrderedMonsterEvents = append(result.OrderedMonsterEvents, OrderedMonsterEvent{Kind: OrderedMonsterEventExperience, Experience: experience})
				}
			}
			result.OrderedMonsterEvents = append(result.OrderedMonsterEvents, OrderedMonsterEvent{Kind: OrderedMonsterEventDeath, Death: death})
			w.cleanupMonsterHitterRefsLocked(mon, playersByID, now)
			w.cleanupMonsterTargetAfterRunLocked(mon, playersByID, now)
			return nil
		}
		for {
			if processedMonsterEntry && time.Since(monsterProcessStarted) > monsterProcessBudget {
				break
			}
			entry, ok := w.nextMonsterProcessEntryLocked(processedMonsterIDs)
			if !ok {
				break
			}
			processedMonsterEntry = true
			processedMonsterIDs[entry.monster.ID] = struct{}{}
			w.monsterProcessGroupCursor = entry.nextGroup
			w.monsterProcessMemberCursor = entry.nextMember
			mon := entry.monster
			if mon.MasterID != "" {
				mon.NoItem = true
			}
			ghostAfterRun := !mon.Alive && !mon.CastleDoor && !mon.CastleWall && mon.RespawnAt.IsZero() && !mon.Ghost && !mon.DeathAt.IsZero() && now.After(mon.DeathAt.Add(3*time.Minute))
			markGhostAfterRun := func() {
				if ghostAfterRun && !mon.Alive {
					mon.Ghost = true
					mon.GhostAt = now
				}
			}
			if mon.Ghost {
				if !mon.GhostAt.IsZero() && now.After(mon.GhostAt.Add(5*time.Minute)) {
					w.unregisterMonsterLocked(mon.ID)
					w.unindexMonsterCellLocked(mon)
					delete(w.monsters, mon.ID)
				}
				continue
			}
			if mon.RunIntervalMS > 0 && !mon.NextRunAt.IsZero() && !now.After(mon.NextRunAt) {
				continue
			}
			if mon.RunIntervalMS > 0 {
				mon.NextRunAt = now.Add(time.Duration(mon.RunIntervalMS) * time.Millisecond)
			}
			if mon.PendingDeath {
				if err := settleMonsterDeath(mon); err != nil {
					return TickResult{}, err
				}
				w.processPendingMonsterSpawnsForParentLocked(mon, now, &result)
				continue
			}
			if mon.NextVisibleSearchAt.IsZero() || now.After(mon.NextVisibleSearchAt) {
				w.refreshMonsterVisibleActorsLocked(mon, playersByID)
			}
			if !w.monsterVisibleActiveLocked(mon) && mon.ProcessRunCount < monsterProcessInterval {
				mon.ProcessRunCount++
				continue
			}
			mon.ProcessRunCount = 0
			w.tryZilkinRebirthLocked(mon, now, monsterPlayers, &result)
			var err error
			wasAliveAtRun := mon.Alive
			if mon.CastleDoor {
				if mon.Alive && !mon.StoneMode {
					direction := castleDoorDirection(mon)
					if mon.Dir != direction && direction < 3 {
						mon.Dir = direction
						action := w.monsterActionLocked(mon, MonsterActionCastleTurn)
						result.MonsterActions = append(result.MonsterActions, action)
						result.OrderedMonsterEvents = append(result.OrderedMonsterEvents, OrderedMonsterEvent{Kind: OrderedMonsterEventAction, Action: action})
					}
				}
				if mon.Alive {
					mon.HealthTick = 0
				} else {
					mon.DeathAt = now
				}
				dueAttacks, err = w.applyPendingMonsterEventsForTargetLocked(&result, playersByID, updated, dueAttacks, mon.ID, now)
				if err != nil {
					return TickResult{}, err
				}
				w.applyMonsterRecoveryTickLocked(mon, now, &result)
				w.applyMonsterInheritedStateTickLocked(mon, now, &result)
				markGhostAfterRun()
				continue
			}
			if mon.CastleWall {
				w.castleWallSetMapFlagLocked(mon)
				if mon.Alive {
					mon.HealthTick = 0
				} else {
					mon.DeathAt = now
				}
				direction := castleWallDirection(mon)
				if mon.Dir != direction && direction < 5 {
					mon.Dir = direction
					action := w.monsterActionLocked(mon, MonsterActionCastleDig)
					result.MonsterActions = append(result.MonsterActions, action)
					result.OrderedMonsterEvents = append(result.OrderedMonsterEvents, OrderedMonsterEvent{Kind: OrderedMonsterEventAction, Action: action})
				}
				dueAttacks, err = w.applyPendingMonsterEventsForTargetLocked(&result, playersByID, updated, dueAttacks, mon.ID, now)
				if err != nil {
					return TickResult{}, err
				}
				w.applyMonsterRecoveryTickLocked(mon, now, &result)
				w.applyMonsterInheritedStateTickLocked(mon, now, &result)
				markGhostAfterRun()
				continue
			}
			if !mon.Alive {
				w.processPendingMonsterSpawnsForParentLocked(mon, now, &result)
				dueAttacks, err = w.applyPendingMonsterEventsForTargetLocked(&result, playersByID, updated, dueAttacks, mon.ID, now)
				if err != nil {
					return TickResult{}, err
				}
				w.cleanupMonsterHitterRefsLocked(mon, playersByID, now)
				w.cleanupMonsterTargetAfterRunLocked(mon, playersByID, now)
				w.finalizeSummonedOwnerStateBeforeInheritedLocked(mon, playersByID, now)
				w.applyMonsterInheritedStateTickLocked(mon, now, &result)
				markGhostAfterRun()
				continue
			}
			controlled := false
			paralyzedExpired := false
			if !mon.ParalyzedUntil.IsZero() {
				until, active, clockChanged := w.advanceMonsterStatusClockLocked(mon.ID, statusClockParalyzed, mon.ParalyzedUntil, now)
				if !active {
					mon.ParalyzedUntil = time.Time{}
					paralyzedExpired = true
					controlled = true
				} else if clockChanged {
					mon.ParalyzedUntil = until
					controlled = true
				}
				if active {
					controlled = true
				}
			}
			inheritedBeforeBehavior := w.monsterBaseRunBeforeBehaviorLocked(mon)
			pendingDeathAtStart := mon.PendingDeath
			if inheritedBeforeBehavior {
				w.applyMonsterInheritedStateTickLocked(mon, now, &result)
				w.applyMonsterRecoveryTickLocked(mon, now, &result)
			}
			wasAlive := mon.Alive
			slaveExpLevelBefore := mon.SlaveExpLevel
			var trace *MonsterTickTrace
			if w.monsterTraceEnabled {
				w.monsterTraceTick++
				trace = &MonsterTickTrace{Tick: w.monsterTraceTick, NowMS: now.UnixMilli(), MonsterID: mon.ID, StateBefore: monsterTraceState(mon)}
				w.monsterTraceCurrent = trace
			}
			var events []MonsterAction
			var hits []CharacterHit
			var chars []storage.Character
			if !controlled {
				events, hits, chars, err = w.tickMonsterBehaviorOnlyLocked(mon, playersByID, now)
			}
			if trace != nil {
				trace.StateAfter = monsterTraceState(mon)
				trace.Actions = append(trace.Actions, events...)
				trace.Decision = monsterTraceDecision(trace)
				result.MonsterTraces = append(result.MonsterTraces, *trace)
				w.monsterTraceCurrent = nil
			}
			if err != nil {
				return TickResult{}, err
			}
			result.MonsterActions = append(result.MonsterActions, events...)
			hitBeforeAction := len(events) == 1 && (events[0].Kind == MonsterActionHit || events[0].Kind == MonsterActionFlyAxe || events[0].Kind == MonsterActionLighting)
			if w.monsterIsSpitSpiderLocked(mon) || w.monsterIsGasAttackLocked(mon) || mon.Race == 94 || w.monsterIsCowKingLocked(mon) || w.monsterIsStoneLocked(mon) {
				hitBeforeAction = false
			}
			for _, hit := range w.monsterBehaviorGroundHits {
				result.OrderedMonsterEvents = append(result.OrderedMonsterEvents, OrderedMonsterEvent{Kind: OrderedMonsterEventHit, Hit: hit})
			}
			result.CharacterHits = append(result.CharacterHits, hits...)
			if hitBeforeAction {
				for _, hit := range hits {
					result.OrderedMonsterEvents = append(result.OrderedMonsterEvents, OrderedMonsterEvent{Kind: OrderedMonsterEventCharacterHit, CharacterHit: hit})
				}
				result.MonsterHits = append(result.MonsterHits, w.monsterBehaviorHits...)
				groundStart := w.monsterBehaviorGroundHitStart
				groundEnd := groundStart + len(w.monsterBehaviorGroundHits)
				for i, hit := range w.monsterBehaviorHits {
					if i >= groundStart && i < groundEnd {
						continue
					}
					result.OrderedMonsterEvents = append(result.OrderedMonsterEvents, OrderedMonsterEvent{Kind: OrderedMonsterEventHit, Hit: hit})
				}
			}
			for _, action := range events {
				result.OrderedMonsterEvents = append(result.OrderedMonsterEvents, OrderedMonsterEvent{Kind: OrderedMonsterEventAction, Action: action})
			}
			if !hitBeforeAction {
				for _, hit := range hits {
					result.OrderedMonsterEvents = append(result.OrderedMonsterEvents, OrderedMonsterEvent{Kind: OrderedMonsterEventCharacterHit, CharacterHit: hit})
				}
				result.MonsterHits = append(result.MonsterHits, w.monsterBehaviorHits...)
			}
			groundStart := w.monsterBehaviorGroundHitStart
			groundEnd := groundStart + len(w.monsterBehaviorGroundHits)
			if !hitBeforeAction {
				for i, hit := range w.monsterBehaviorHits {
					if i >= groundStart && i < groundEnd {
						continue
					}
					result.OrderedMonsterEvents = append(result.OrderedMonsterEvents, OrderedMonsterEvent{Kind: OrderedMonsterEventHit, Hit: hit})
				}
			}
			result.StatusRefreshMonsters = append(result.StatusRefreshMonsters, w.monsterBehaviorStatuses...)
			for _, monster := range w.monsterBehaviorStatuses {
				result.OrderedMonsterEvents = append(result.OrderedMonsterEvents, OrderedMonsterEvent{Kind: OrderedMonsterEventStatus, Monster: monster})
			}
			result.StatusRefreshCharacters = append(result.StatusRefreshCharacters, w.monsterBehaviorCharacterStatuses...)
			for _, character := range w.monsterBehaviorCharacterStatuses {
				result.OrderedMonsterEvents = append(result.OrderedMonsterEvents, OrderedMonsterEvent{Kind: OrderedMonsterEventCharacterStatus, Character: character})
			}
			result.PoisonNotifications = append(result.PoisonNotifications, w.monsterBehaviorPoisonNotifications...)
			for i := range w.monsterBehaviorCharacterStatuses {
				character := w.monsterBehaviorCharacterStatuses[i]
				result.OrderedStatusRefreshes = append(result.OrderedStatusRefreshes, StatusRefreshEvent{Character: &character})
			}
			for i := range w.monsterBehaviorPoisonNotifications {
				notification := w.monsterBehaviorPoisonNotifications[i]
				result.OrderedMonsterEvents = append(result.OrderedMonsterEvents, OrderedMonsterEvent{Kind: OrderedMonsterEventCharacterPoisonNotice, PoisonNotification: notification})
			}
			if mon.SlaveExpLevel != slaveExpLevelBefore {
				result.NameColorMonsters = append(result.NameColorMonsters, *mon)
				result.NameMonsters = append(result.NameMonsters, *mon)
				result.OrderedMonsterEvents = append(result.OrderedMonsterEvents,
					OrderedMonsterEvent{Kind: OrderedMonsterEventNameColor, Monster: *mon},
					OrderedMonsterEvent{Kind: OrderedMonsterEventUsername, Monster: *mon},
				)
			}
			slaveExpLevelBefore = mon.SlaveExpLevel
			w.monsterBehaviorHits = w.monsterBehaviorHits[:0]
			w.monsterBehaviorGroundHits = w.monsterBehaviorGroundHits[:0]
			w.monsterBehaviorGroundHitStart = 0
			w.monsterBehaviorStatuses = w.monsterBehaviorStatuses[:0]
			w.monsterBehaviorCharacterStatuses = w.monsterBehaviorCharacterStatuses[:0]
			w.monsterBehaviorPoisonNotifications = w.monsterBehaviorPoisonNotifications[:0]
			for _, ch := range chars {
				playersByID[ch.ID] = ch
				updated[ch.ID] = ch
			}
			pendingDeathBeforeEvents := mon.PendingDeath
			dueAttacks, err = w.applyPendingMonsterEventsForTargetLocked(&result, playersByID, updated, dueAttacks, mon.ID, now)
			if err != nil {
				return TickResult{}, err
			}
			if wasAliveAtRun && !pendingDeathBeforeEvents && (mon.PendingDeath || !mon.Alive) {
				w.cleanupMonsterHitterRefsLocked(mon, playersByID, now)
				w.cleanupMonsterTargetAfterRunLocked(mon, playersByID, now)
				w.finalizeSummonedOwnerStateAfterRunLocked(mon, playersByID, now)
				w.applyMonsterInheritedStateTickLocked(mon, now, &result)
				continue
			}
			pendingDeathAfterBehavior := mon.PendingDeath
			masterTickExpired := false
			if !inheritedBeforeBehavior {
				w.applyMonsterRecoveryTickLocked(mon, now, &result)
			}
			if !inheritedBeforeBehavior {
				w.cleanupMonsterHitterRefsLocked(mon, playersByID, now)
				w.cleanupMonsterTargetAfterRunLocked(mon, playersByID, now)
				w.finalizeSummonedOwnerStateBeforeInheritedLocked(mon, playersByID, now)
				if mon.SlaveExpLevel != slaveExpLevelBefore {
					result.NameColorMonsters = append(result.NameColorMonsters, *mon)
					result.NameMonsters = append(result.NameMonsters, *mon)
					result.OrderedMonsterEvents = append(result.OrderedMonsterEvents,
						OrderedMonsterEvent{Kind: OrderedMonsterEventNameColor, Monster: *mon},
						OrderedMonsterEvent{Kind: OrderedMonsterEventUsername, Monster: *mon},
					)
					slaveExpLevelBefore = mon.SlaveExpLevel
				}
			}
			if !inheritedBeforeBehavior {
				w.applyMonsterInheritedStateTickLocked(mon, now, &result)
			}
			if !inheritedBeforeBehavior && paralyzedExpired {
				result.StatusRefreshMonsters = append(result.StatusRefreshMonsters, *mon)
				result.PostActionMonsterStatusRefreshes = append(result.PostActionMonsterStatusRefreshes, *mon)
				result.OrderedMonsterEvents = append(result.OrderedMonsterEvents, OrderedMonsterEvent{Kind: OrderedMonsterEventStatus, Monster: *mon})
			}
			if mon.SlaveExpLevel != slaveExpLevelBefore {
				result.NameColorMonsters = append(result.NameColorMonsters, *mon)
				result.NameMonsters = append(result.NameMonsters, *mon)
				result.OrderedMonsterEvents = append(result.OrderedMonsterEvents,
					OrderedMonsterEvent{Kind: OrderedMonsterEventNameColor, Monster: *mon},
					OrderedMonsterEvent{Kind: OrderedMonsterEventUsername, Monster: *mon},
				)
			}
			if wasAlive && !mon.Alive {
				death := AttackResult{
					MonsterID: mon.ID, MonsterHP: 0, MonsterMaxHP: mon.MaxHP,
					MonsterRaceImg: mon.RaceImg, MonsterWeapon: mon.MonsterWeapon, MonsterAppr: mon.Appr,
					MonsterX: mon.X, MonsterY: mon.Y, MonsterMapID: mon.MapID, MonsterDir: mon.Dir,
					MonsterStatus: MonsterStatus(*mon, now), Dead: true,
				}
				result.MonsterDeaths = append(result.MonsterDeaths, death)
				result.OrderedMonsterEvents = append(result.OrderedMonsterEvents, OrderedMonsterEvent{Kind: OrderedMonsterEventDeath, Death: death})
			}
			if !mon.Alive {
				continue
			}
			killed, err := w.applyMonsterPoisonToTickResultLocked(mon, playersByID, now, &result)
			if err != nil {
				return TickResult{}, err
			}
			if w.releaseExpiredMonsterMasterLocked(mon, now) {
				result.NameMonsters = append(result.NameMonsters, *mon)
				result.OrderedMonsterEvents = append(result.OrderedMonsterEvents, OrderedMonsterEvent{Kind: OrderedMonsterEventUsername, Monster: *mon})
			}
			masterTickExpired = expireSummonedMonsterMasterTickLocked(mon, now)
			if !pendingDeathAtStart && mon.PendingDeath && mon.HP > 0 {
				mon.PendingDeath = false
				mon.DeathHitterID = ""
			}
			if !pendingDeathAtStart && pendingDeathAfterBehavior && mon.PendingDeath && mon.MasterID == "" {
				if err := settleMonsterDeath(mon); err != nil {
					return TickResult{}, err
				}
				continue
			}
			if masterTickExpired {
				if err := settleMonsterDeath(mon); err != nil {
					return TickResult{}, err
				}
				continue
			}
			if killed {
				continue
			}
		}
		if w.monsterProcessGroupCursor >= len(w.monsterProcessGroups) {
			w.monsterProcessGroupCursor = 0
			w.monsterProcessMemberCursor = 0
		}
	}
	if len(dueAttacks) > 0 {
		w.pendingMonsterAttacks = append(w.pendingMonsterAttacks, dueAttacks...)
	}
	w.reorderMonsterHealingRefreshesLocked(&result)
	fireMonsterHits, fireCharacterHits, fireUpdated, fireOrderedEvents, fireTrainerSpeeches, fireTrainerStruckHits, fireTrainerCombatEvents := w.applyFireWallTickLocked(playersByID, now)
	result.MonsterHits = append(result.MonsterHits, fireMonsterHits...)
	result.CharacterHits = append(result.CharacterHits, fireCharacterHits...)
	result.OrderedMonsterEvents = append(result.OrderedMonsterEvents, fireOrderedEvents...)
	for _, ch := range fireUpdated {
		playersByID[ch.ID] = ch
		updated[ch.ID] = ch
	}
	result.NPCSpeeches = append(result.NPCSpeeches, fireTrainerSpeeches...)
	result.NPCStruckHits = append(result.NPCStruckHits, fireTrainerStruckHits...)
	result.NPCCombatEvents = append(result.NPCCombatEvents, fireTrainerCombatEvents...)
	w.applyPendingTrainerSpellsLocked(&result, now)
	merchantSpeeches, merchantActions := w.runMerchantsLocked(now)
	npcSpeeches, npcActions := w.runNPCsLocked(now)
	result.NPCSpeeches = append(result.NPCSpeeches, merchantSpeeches...)
	result.NPCSpeeches = append(result.NPCSpeeches, npcSpeeches...)
	result.NPCActions = append(result.NPCActions, merchantActions...)
	result.NPCActions = append(result.NPCActions, npcActions...)
	w.finalizeGroundEventsLocked(now, &result)
	for _, player := range players {
		if character, ok := updated[player.Character.ID]; ok {
			result.Characters = append(result.Characters, character)
		}
	}
	return result, nil
}

func (w *World) processMonsterLifecycleLocked(now time.Time, players []PlayerSnapshot, result *TickResult) {
	w.replenishMonsterSpawnForPopulationLocked(now, result, len(players))
	w.cleanupCastleDefenseSlotsLocked()
	remainingSpawns := w.pendingMonsterSpawns[:0]
	for _, pending := range w.pendingMonsterSpawns {
		parent := w.monsters[pending.ParentID]
		if parent != nil && !parent.Ghost {
			remainingSpawns = append(remainingSpawns, pending)
		}
	}
	w.pendingMonsterSpawns = remainingSpawns
}

func (w *World) processPendingMonsterSpawnsForParentLocked(parent *Monster, now time.Time, result *TickResult) {
	if parent == nil {
		return
	}
	remainingSpawns := w.pendingMonsterSpawns[:0]
	for _, pending := range w.pendingMonsterSpawns {
		if pending.ParentID != parent.ID || now.Before(pending.DueAt) {
			remainingSpawns = append(remainingSpawns, pending)
			continue
		}
		if parent.Ghost {
			continue
		}
		child, spawned := w.spawnChildMonsterWithResultLocked(parent, pending.ChildName, now)
		if !spawned {
			continue
		}
		if child != nil {
			result.SpawnedMonsters = append(result.SpawnedMonsters, *child)
			result.OrderedMonsterEvents = append(result.OrderedMonsterEvents, OrderedMonsterEvent{Kind: OrderedMonsterEventSpawn, Monster: *child})
		}
	}
	w.pendingMonsterSpawns = remainingSpawns
}

func orderedPlayerSnapshots(players []PlayerSnapshot) []PlayerSnapshot {
	ordered := append([]PlayerSnapshot(nil), players...)
	hasProcessOrder := false
	for _, player := range ordered {
		if player.ProcessOrder != 0 {
			hasProcessOrder = true
			break
		}
	}
	if !hasProcessOrder {
		return ordered
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		left, right := ordered[i], ordered[j]
		if left.ProcessOrder != 0 || right.ProcessOrder != 0 {
			if left.ProcessOrder == 0 {
				return false
			}
			if right.ProcessOrder == 0 {
				return true
			}
			if left.ProcessOrder != right.ProcessOrder {
				return left.ProcessOrder < right.ProcessOrder
			}
		}
		if left.Character.ObjectOrder != 0 || right.Character.ObjectOrder != 0 {
			if left.Character.ObjectOrder == 0 {
				return false
			}
			if right.Character.ObjectOrder == 0 {
				return true
			}
			if left.Character.ObjectOrder != right.Character.ObjectOrder {
				return left.Character.ObjectOrder < right.Character.ObjectOrder
			}
		}
		if left.Character.ID == right.Character.ID {
			return left.Character.Name < right.Character.Name
		}
		return left.Character.ID < right.Character.ID
	})
	return ordered
}

func (w *World) applyDeferredMonsterAttacksLocked(result *TickResult, attacks []pendingMonsterAttack, now time.Time) error {
	for _, pending := range attacks {
		if err := w.applyDeferredMonsterAttackLocked(result, pending, now); err != nil {
			return err
		}
	}
	return nil
}

func (w *World) applyDeferredMonsterAttackLocked(result *TickResult, pending pendingMonsterAttack, now time.Time) error {
	mon := w.monsters[pending.MonsterID]
	if len(pending.MonsterTargets) == 0 {
		return nil
	}
	pendingTarget := pending.MonsterTargets[0]
	if mon == nil || (!pendingTarget.ReferenceDelay && !pendingTarget.DirectMagicStrike && !pendingTarget.StruckOnly && !monsterTargetAliveLocked(mon)) {
		return nil
	}
	target := w.monsters[pendingTarget.ID]
	if target == nil || target.ID == "" || (!pendingTarget.StruckOnly && !monsterTargetAliveLocked(target)) {
		return nil
	}
	if pendingTarget.StruckOnly {
		if pending.StruckCharacter != nil {
			*pending.StruckCharacter = w.monsterStruckByCharacterLocked(target, *pending.StruckCharacter, pending.StruckPlayers, now)
		} else {
			w.monsterStruckByMonsterLocked(target, mon, now)
		}
		return nil
	}
	if !pendingTarget.ReferenceDelay && !pendingTarget.DirectMagicStrike && !w.monsterCanAttackMonsterTargetLocked(mon, target) {
		return nil
	}
	caster := storage.Character{ID: mon.ID, MapID: mon.MapID, X: mon.X, Y: mon.Y}
	damagePower := pendingTarget.Damage
	if pendingTarget.ReferenceDelay && target.Race >= 50 {
		damagePower = referenceRound(float64(damagePower) / 1.2)
	}
	damage := w.monsterMagicDamageAfterDefenseLocked(target, damagePower)
	if damage <= 0 {
		return nil
	}
	if pendingTarget.ReferenceDelay {
		mon.TargetCharacterID = ""
		mon.TargetMonsterID = target.ID
		mon.TargetFocusAt = now
	}
	if pendingTarget.ReferenceDelay && pendingTarget.TargetRange >= 0 && (abs(target.X-pendingTarget.TargetX) > pendingTarget.TargetRange || abs(target.Y-pendingTarget.TargetY) > pendingTarget.TargetRange) {
		return nil
	}
	w.monsterMagicStruckLocked(target, now)
	hit, err := w.applyReferenceDeferredMonsterMagicDamageAtLocked(caster, target, damage, now)
	if err != nil {
		return err
	}
	if hit.Damage > 0 {
		w.monsterStruckByMonsterLocked(target, mon, now)
		result.MonsterHits = append(result.MonsterHits, hit)
		result.OrderedMonsterEvents = append(result.OrderedMonsterEvents, OrderedMonsterEvent{Kind: OrderedMonsterEventHit, Hit: hit})
	}
	return nil
}

func (w *World) applyOnePendingSpellForMonsterLocked(result *TickResult, players, updated map[string]storage.Character, now time.Time, index int) error {
	selected := w.pendingSpells[index]
	rest := make([]pendingSpell, 0, len(w.pendingSpells)-1)
	rest = append(rest, w.pendingSpells[:index]...)
	rest = append(rest, w.pendingSpells[index+1:]...)
	w.pendingSpells = []pendingSpell{selected}
	err := w.applyPendingSpellTicksForPhaseLocked(result, players, updated, now, pendingSpellPhaseMonster)
	processed := w.pendingSpells
	w.pendingSpells = append(rest, processed...)
	return err
}

func (w *World) pendingMonsterEventTargetID(pending pendingMonsterAttack) string {
	if len(pending.MonsterTargets) == 0 {
		return ""
	}
	return pending.MonsterTargets[0].ID
}

func (w *World) pendingMonsterEventBefore(leftTargetID, rightTargetID string) bool {
	if leftTargetID == rightTargetID {
		return false
	}
	left, leftOK := w.monsters[leftTargetID]
	right, rightOK := w.monsters[rightTargetID]
	if leftOK && left != nil && rightOK && right != nil {
		leftGroup, leftMember, leftObject, leftID := monsterProcessOrder(left)
		rightGroup, rightMember, rightObject, rightID := monsterProcessOrder(right)
		if leftGroup != rightGroup {
			return leftGroup < rightGroup
		}
		if leftMember != rightMember {
			return leftMember < rightMember
		}
		if leftObject != rightObject {
			return leftObject < rightObject
		}
		return leftID < rightID
	}
	if leftOK != rightOK {
		return leftOK
	}
	return leftTargetID < rightTargetID
}

func (w *World) applyPendingMonsterEventsLocked(result *TickResult, players, updated map[string]storage.Character, attacks []pendingMonsterAttack, now time.Time) ([]pendingMonsterAttack, error) {
	for {
		attackIndex := -1
		var attackOrder uint64
		attackTargetID := ""
		for i, pending := range attacks {
			if now.Before(pending.DueAt) || len(pending.MonsterTargets) == 0 {
				continue
			}
			targetID := w.pendingMonsterEventTargetID(pending)
			if attackIndex < 0 || w.pendingMonsterEventBefore(targetID, attackTargetID) ||
				(targetID == attackTargetID && pendingEventBefore(pending.EnqueueOrder, attackOrder, true)) {
				attackIndex = i
				attackOrder = pending.EnqueueOrder
				attackTargetID = targetID
			}
		}
		spellIndex := -1
		var spellOrder uint64
		spellTargetID := ""
		for i, pending := range w.pendingSpells {
			if pending.TargetMonsterID == "" || now.Before(pending.DueAt) {
				continue
			}
			if spellIndex < 0 || w.pendingMonsterEventBefore(pending.TargetMonsterID, spellTargetID) ||
				(pending.TargetMonsterID == spellTargetID && pendingEventBefore(pending.EnqueueOrder, spellOrder, false)) {
				spellIndex = i
				spellOrder = pending.EnqueueOrder
				spellTargetID = pending.TargetMonsterID
			}
		}
		if attackIndex < 0 && spellIndex < 0 {
			return attacks, nil
		}
		attackFirst := false
		switch {
		case attackIndex < 0:
			attackFirst = false
		case spellIndex < 0:
			attackFirst = true
		case attackTargetID != spellTargetID:
			attackFirst = w.pendingMonsterEventBefore(attackTargetID, spellTargetID)
		default:
			attackFirst = pendingAttackWins(attackIndex, spellIndex, attackOrder, spellOrder)
		}
		if attackFirst {
			if err := w.applyDeferredMonsterAttackLocked(result, attacks[attackIndex], now); err != nil {
				return nil, err
			}
			attacks = append(attacks[:attackIndex], attacks[attackIndex+1:]...)
			continue
		}
		pending := w.pendingSpells[spellIndex]
		if pending.Healing > 0 && pending.TargetMonsterID != "" {
			if _, exists := w.monsterHealingOrder[pending.TargetMonsterID]; !exists {
				w.monsterHealingOrder[pending.TargetMonsterID] = pending.EnqueueOrder
			}
		}
		if err := w.applyOnePendingSpellForMonsterLocked(result, players, updated, now, spellIndex); err != nil {
			return nil, err
		}
	}
}

func (w *World) applyPendingMonsterEventsForTargetLocked(result *TickResult, players, updated map[string]storage.Character, attacks []pendingMonsterAttack, targetID string, now time.Time) ([]pendingMonsterAttack, error) {
	for {
		spawnIndex := -1
		var spawnOrder uint64
		for i, pending := range w.pendingMonsterSpawns {
			if pending.ParentID != targetID || now.Before(pending.DueAt) {
				continue
			}
			if spawnIndex < 0 || pendingOrderBefore(pending.EnqueueOrder, spawnOrder) {
				spawnIndex = i
				spawnOrder = pending.EnqueueOrder
			}
		}
		attackIndex := -1
		var attackOrder uint64
		for i, pending := range attacks {
			if now.Before(pending.DueAt) || len(pending.MonsterTargets) == 0 || w.pendingMonsterEventTargetID(pending) != targetID {
				continue
			}
			if attackIndex < 0 || pendingEventBefore(pending.EnqueueOrder, attackOrder, true) {
				attackIndex = i
				attackOrder = pending.EnqueueOrder
			}
		}
		spellIndex := -1
		var spellOrder uint64
		for i, pending := range w.pendingSpells {
			if pending.TargetMonsterID != targetID || now.Before(pending.DueAt) {
				continue
			}
			if spellIndex < 0 || pendingEventBefore(pending.EnqueueOrder, spellOrder, false) {
				spellIndex = i
				spellOrder = pending.EnqueueOrder
			}
		}
		if spawnIndex < 0 && attackIndex < 0 && spellIndex < 0 {
			return attacks, nil
		}
		spawnFirst := spawnIndex >= 0 && (attackIndex < 0 || pendingOrderBefore(spawnOrder, attackOrder)) && (spellIndex < 0 || pendingOrderBefore(spawnOrder, spellOrder))
		if spawnFirst {
			pending := w.pendingMonsterSpawns[spawnIndex]
			w.pendingMonsterSpawns = append(w.pendingMonsterSpawns[:spawnIndex], w.pendingMonsterSpawns[spawnIndex+1:]...)
			parent := w.monsters[targetID]
			if parent != nil && !parent.Ghost {
				child, spawned := w.spawnChildMonsterWithResultLocked(parent, pending.ChildName, now)
				if spawned && child != nil {
					result.SpawnedMonsters = append(result.SpawnedMonsters, *child)
					result.OrderedMonsterEvents = append(result.OrderedMonsterEvents, OrderedMonsterEvent{Kind: OrderedMonsterEventSpawn, Monster: *child})
				}
			}
			continue
		}
		if pendingAttackWins(attackIndex, spellIndex, attackOrder, spellOrder) {
			if err := w.applyDeferredMonsterAttackLocked(result, attacks[attackIndex], now); err != nil {
				return nil, err
			}
			attacks = append(attacks[:attackIndex], attacks[attackIndex+1:]...)
			continue
		}
		pending := w.pendingSpells[spellIndex]
		if pending.Healing > 0 && pending.TargetMonsterID != "" {
			if _, exists := w.monsterHealingOrder[pending.TargetMonsterID]; !exists {
				w.monsterHealingOrder[pending.TargetMonsterID] = pending.EnqueueOrder
			}
		}
		if err := w.applyOnePendingSpellForMonsterLocked(result, players, updated, now, spellIndex); err != nil {
			return nil, err
		}
	}
}

func (w *World) reorderMonsterHealingRefreshesLocked(result *TickResult) {
	if len(w.monsterHealingOrder) == 0 {
		return
	}
	positions := make([]int, 0)
	refreshes := make([]Monster, 0)
	for i, mon := range result.PostActionMonsterHealthRefreshes {
		if _, ok := w.monsterHealingOrder[mon.ID]; !ok {
			continue
		}
		positions = append(positions, i)
		refreshes = append(refreshes, mon)
	}
	sort.SliceStable(refreshes, func(i, j int) bool {
		return w.monsterHealingOrder[refreshes[i].ID] < w.monsterHealingOrder[refreshes[j].ID]
	})
	for i, position := range positions {
		result.PostActionMonsterHealthRefreshes[position] = refreshes[i]
	}
	orderedPositions := make([]int, 0)
	orderedEvents := make([]OrderedMonsterEvent, 0)
	for i, event := range result.OrderedMonsterEvents {
		if event.Kind != OrderedMonsterEventHealth {
			continue
		}
		if _, ok := w.monsterHealingOrder[event.Monster.ID]; !ok {
			continue
		}
		orderedPositions = append(orderedPositions, i)
		orderedEvents = append(orderedEvents, event)
	}
	sort.SliceStable(orderedEvents, func(i, j int) bool {
		return w.monsterHealingOrder[orderedEvents[i].Monster.ID] < w.monsterHealingOrder[orderedEvents[j].Monster.ID]
	})
	for i, position := range orderedPositions {
		result.OrderedMonsterEvents[position] = orderedEvents[i]
	}
}

func (w *World) finalizeGroundEventsLocked(now time.Time, result *TickResult) {
	newlyClosed := make(map[int32]struct{})
	for id, event := range w.groundEvents {
		if now.After(event.StartAt.Add(event.Duration)) {
			delete(w.groundEvents, id)
			event.ClosedAt = now
			event.HideSent = false
			if w.closedGroundEvents == nil {
				w.closedGroundEvents = make(map[int32]SpellGroundEvent)
			}
			w.closedGroundEvents[id] = event
			newlyClosed[id] = struct{}{}
			continue
		}
		result.GroundEvents = append(result.GroundEvents, event)
	}
	for id, event := range w.closedGroundEvents {
		if _, justClosed := newlyClosed[id]; justClosed {
			continue
		}
		if !event.HideSent && !event.ClosedAt.IsZero() && now.After(event.ClosedAt) {
			event.HideSent = true
			w.closedGroundEvents[id] = event
			result.GroundEventHideDetails = append(result.GroundEventHideDetails, event)
		}
		if !event.ClosedAt.IsZero() && now.After(event.ClosedAt.Add(5*time.Minute)) {
			delete(w.closedGroundEvents, id)
		}
	}
	sort.SliceStable(result.GroundEventHideDetails, func(i, j int) bool {
		return groundEventBefore(result.GroundEventHideDetails[i], result.GroundEventHideDetails[j])
	})
	result.GroundEventHides = result.GroundEventHides[:0]
	for _, event := range result.GroundEventHideDetails {
		result.GroundEventHides = append(result.GroundEventHides, event.ID)
	}
	sort.Slice(result.GroundEvents, func(i, j int) bool {
		return groundEventBefore(result.GroundEvents[i], result.GroundEvents[j])
	})
}

func (w *World) monsterAttackerAliveLocked(id string, players map[string]storage.Character) bool {
	if attacker, ok := players[id]; ok {
		return !w.characterDeadLocked(attacker) && !attacker.Ghost
	}
	if attacker, ok := w.monsters[id]; ok {
		return attacker.Alive && !attacker.Ghost
	}
	return false
}

func (w *World) monsterExperienceOwnerLocked(attackerID string, players map[string]storage.Character, now time.Time) (storage.Character, bool) {
	if attacker, ok := players[attackerID]; ok {
		return attacker, true
	}
	attacker, ok := w.monsters[attackerID]
	if !ok || attacker.MasterID == "" {
		return storage.Character{}, false
	}
	owner, ok := players[attacker.MasterID]
	if !ok || w.characterDeadLocked(owner) {
		return storage.Character{}, false
	}
	if !attacker.MasterExpiresAt.IsZero() && now.After(attacker.MasterExpiresAt) {
		return storage.Character{}, false
	}
	return owner, true
}

func (w *World) applyCharacterNaturalSpellTickLocked(ch *storage.Character, now time.Time) bool {
	changed, _ := w.applyCharacterNaturalSpellTickTraceLocked(ch, now)
	return changed
}

func (w *World) advanceCharacterNaturalSpellClockLocked(ch *storage.Character, now time.Time) bool {
	if ch == nil {
		return false
	}
	changed := false
	if ch.HealthTickAt == 0 {
		ch.HealthTickAt = now.UnixMilli()
		changed = true
	}
	if ch.SpellTickAt == 0 {
		ch.SpellTickAt = now.UnixMilli()
		changed = true
	}
	nowMS := now.UnixMilli()
	healthElapsed := nowMS - ch.HealthTickAt
	spellElapsed := nowMS - ch.SpellTickAt
	if healthElapsed > 0 {
		ch.HealthTickAt = nowMS
		ch.HealthTick += int(healthElapsed / 20)
		changed = true
	}
	if spellElapsed > 0 {
		ch.SpellTickAt = nowMS
		ch.SpellTick += int(spellElapsed / 20)
		changed = true
	}
	return changed
}

func (w *World) applyCharacterNaturalSpellTickTraceLocked(ch *storage.Character, now time.Time) (bool, []storage.Character) {
	if ch == nil {
		return false, nil
	}
	w.advanceCharacterNaturalSpellClockLocked(ch, now)
	changed := false
	changes := make([]storage.Character, 0, 2)
	healthFillMS := w.gameplay.Recovery.HealthFillTimeMS
	if healthFillMS <= 0 {
		healthFillMS = 300
	}
	spellFillMS := w.gameplay.Recovery.SpellFillTimeMS
	if spellFillMS <= 0 {
		spellFillMS = 800
	}
	if ch.HP < ch.MaxHP && ch.HealthTick >= healthFillMS {
		ch.HealthTick = 0
		gain := ch.MaxHP/75 + 1
		if ch.HP+gain > ch.MaxHP {
			gain = ch.MaxHP - ch.HP
		}
		if gain > 0 {
			ch.HP += gain
			changed = true
			changes = append(changes, *ch)
		}
	} else if ch.HealthTick >= healthFillMS {
		ch.HealthTick = 0
	}
	if ch.MP < ch.MaxMP && ch.SpellTick >= spellFillMS {
		ch.SpellTick = 0
		gain := ch.MaxMP/18 + 1
		if ch.MP+gain > ch.MaxMP {
			gain = ch.MaxMP - ch.MP
		}
		if gain > 0 {
			ch.MP += gain
			changed = true
			changes = append(changes, *ch)
		}
	} else if ch.SpellTick >= spellFillMS {
		ch.SpellTick = 0
	}
	return changed, changes
}

func (w *World) applyCharacterNegativeHealthTickLocked(ch *storage.Character) bool {
	if ch == nil {
		return false
	}
	healthFillMS := w.gameplay.Recovery.HealthFillTimeMS
	if healthFillMS <= 0 {
		healthFillMS = 300
	}
	if ch.HealthTick < -healthFillMS && ch.HP > 1 {
		ch.HP--
		ch.HealthTick += healthFillMS
		return true
	}
	return false
}

func characterListFromMap(players map[string]storage.Character) []storage.Character {
	list := make([]storage.Character, 0, len(players))
	for _, player := range players {
		list = append(list, player)
	}
	return list
}

type pendingSpellPhase uint8

const (
	pendingSpellPhaseAll pendingSpellPhase = iota
	pendingSpellPhaseCharacter
	pendingSpellPhaseMonster
)

func (w *World) applyPendingSpellTicksLocked(result *TickResult, players, updated map[string]storage.Character, now time.Time) error {
	return w.applyPendingSpellTicksForPhaseAndCharacterLocked(result, players, updated, now, pendingSpellPhaseAll, "")
}

func (w *World) applyPendingSpellTicksForPhaseLocked(result *TickResult, players, updated map[string]storage.Character, now time.Time, phase pendingSpellPhase) (err error) {
	return w.applyPendingSpellTicksForPhaseAndCharacterLocked(result, players, updated, now, phase, "")
}

func (w *World) applyPendingSpellTicksForCharacterLocked(result *TickResult, players, updated map[string]storage.Character, now time.Time, characterID string) error {
	return w.applyPendingSpellTicksForPhaseAndCharacterLocked(result, players, updated, now, pendingSpellPhaseCharacter, characterID)
}

func (w *World) applyPendingSpellTicksForPhaseAndCharacterLocked(result *TickResult, players, updated map[string]storage.Character, now time.Time, phase pendingSpellPhase, characterID string) (err error) {
	if len(w.pendingSpells) == 0 {
		return nil
	}
	snapshotAllMonsters := phase == pendingSpellPhaseAll
	snapshotIDs := make(map[string]struct{})
	for _, pending := range w.pendingSpells {
		if pending.TransparentDuration > 0 || !pending.TransparentUntil.IsZero() {
			snapshotAllMonsters = true
		}
		if pending.TargetMonsterID != "" {
			snapshotIDs[pending.TargetMonsterID] = struct{}{}
		}
	}
	if snapshotAllMonsters {
		for id := range w.monsters {
			snapshotIDs[id] = struct{}{}
		}
	}
	originalMonsters := make(map[string]*Monster, len(snapshotIDs))
	originalMonsterValues := make(map[string]Monster, len(snapshotIDs))
	for id := range snapshotIDs {
		mon := w.monsters[id]
		if mon == nil {
			continue
		}
		originalMonsters[id] = mon
		originalMonsterValues[id] = *mon
	}
	rollbackMonsters := func() {
		for id := range snapshotIDs {
			original, ok := originalMonsters[id]
			if !ok {
				delete(w.monsters, id)
				continue
			}
			*original = originalMonsterValues[id]
			w.monsters[id] = original
		}
		for id, original := range originalMonsters {
			if _, ok := w.monsters[id]; !ok {
				w.monsters[id] = original
			}
		}
	}
	defer func() {
		if err != nil {
			rollbackMonsters()
		}
	}()
	appendMonsterPhaseEvent := func(event OrderedSpellEvent) {
		if phase != pendingSpellPhaseMonster {
			result.OrderedSpellEvents = append(result.OrderedSpellEvents, event)
			return
		}
		switch event.Kind {
		case OrderedSpellEventMonsterStatus:
			result.OrderedMonsterEvents = append(result.OrderedMonsterEvents, OrderedMonsterEvent{Kind: OrderedMonsterEventStatus, Monster: event.Monster})
		case OrderedSpellEventMonsterOpenHealth:
			result.OrderedMonsterEvents = append(result.OrderedMonsterEvents, OrderedMonsterEvent{Kind: OrderedMonsterEventOpenHealth, Monster: event.Monster})
		case OrderedSpellEventMonsterHit:
			result.OrderedMonsterEvents = append(result.OrderedMonsterEvents, OrderedMonsterEvent{Kind: OrderedMonsterEventHit, Hit: event.MonsterHit})
		case OrderedSpellEventCharacterExperience:
			result.OrderedMonsterEvents = append(result.OrderedMonsterEvents, OrderedMonsterEvent{Kind: OrderedMonsterEventExperience, Experience: event.Experience})
		default:
			result.OrderedSpellEvents = append(result.OrderedSpellEvents, event)
		}
	}
	sort.SliceStable(w.pendingSpells, func(i, j int) bool {
		left, right := w.pendingSpells[i], w.pendingSpells[j]
		leftTarget, rightTarget := pendingSpellTargetKey(left), pendingSpellTargetKey(right)
		if leftTarget == rightTarget {
			if left.EnqueueOrder != 0 && right.EnqueueOrder != 0 && left.EnqueueOrder != right.EnqueueOrder {
				return left.EnqueueOrder < right.EnqueueOrder
			}
			return false
		}
		leftClass, rightClass := pendingSpellTargetClass(left), pendingSpellTargetClass(right)
		if leftClass != rightClass {
			return leftClass < rightClass
		}
		leftOrder, leftKnown := w.pendingSpellTargetOrderLocked(left, players)
		rightOrder, rightKnown := w.pendingSpellTargetOrderLocked(right, players)
		if leftKnown && rightKnown && leftOrder != rightOrder {
			return leftOrder < rightOrder
		}
		if leftKnown != rightKnown {
			return leftKnown
		}
		return left.DueAt.Before(right.DueAt)
	})
	remaining := make([]pendingSpell, 0, len(w.pendingSpells))
	for _, pending := range w.pendingSpells {
		if phase != pendingSpellPhaseAll && pendingSpellPhaseForTarget(pending) != phase {
			remaining = append(remaining, pending)
			continue
		}
		if characterID != "" && pending.TargetCharacterID != characterID {
			remaining = append(remaining, pending)
			continue
		}
		if now.Before(pending.DueAt) {
			remaining = append(remaining, pending)
			continue
		}
		if pending.TargetCharacterID != "" {
			target, ok := players[pending.TargetCharacterID]
			if !ok || target.Ghost {
				continue
			}
		}
		if pending.TargetMonsterID != "" {
			mon, ok := w.monsters[pending.TargetMonsterID]
			if !ok || mon == nil || mon.Ghost || mon.PendingDeath {
				continue
			}
		}
		if pending.ShowHealthDuration > 0 {
			showUntil := pending.ShowHealthStartedAt.Add(pending.ShowHealthDuration).UnixNano()
			if pending.TargetCharacterID != "" {
				target, ok := players[pending.TargetCharacterID]
				if !ok {
					continue
				}
				target.ShowHPOpenAt = 0
				target.ShowHPDuration = 0
				target.ShowHPUntil = showUntil
				players[target.ID] = target
				updated[target.ID] = target
				result.ShowHPOpenedCharacters = append(result.ShowHPOpenedCharacters, target)
				result.OrderedSpellEvents = append(result.OrderedSpellEvents, OrderedSpellEvent{Kind: OrderedSpellEventCharacterOpenHealth, Character: target})
				continue
			}
			mon := w.monsters[pending.TargetMonsterID]
			if mon == nil {
				continue
			}
			mon.ShowHPOpenAt = 0
			mon.ShowHPDuration = 0
			mon.ShowHPUntil = showUntil
			result.ShowHPOpenedMonsters = append(result.ShowHPOpenedMonsters, *mon)
			appendMonsterPhaseEvent(OrderedSpellEvent{Kind: OrderedSpellEventMonsterOpenHealth, Monster: *mon})
			continue
		}
		if !pending.TransparentUntil.IsZero() || pending.TransparentDuration > 0 {
			transparentUntil := pending.TransparentUntil
			if pending.TransparentDuration > 0 {
				transparentUntil = now.Add(pending.TransparentDuration)
			}
			if pending.TargetCharacterID != "" {
				target, ok := players[pending.TargetCharacterID]
				if !ok {
					continue
				}
				if target.TransparentUntil > 0 {
					continue
				}
				if setCharacterTransparentLocked(&target, transparentUntil) {
					w.breakNearbyMonsterTargetsForStealthLocked(target)
					players[target.ID] = target
					updated[target.ID] = target
					result.StatusRefreshCharacters = append(result.StatusRefreshCharacters, target)
					result.OrderedStatusRefreshes = append(result.OrderedStatusRefreshes, StatusRefreshEvent{Character: &target})
					result.OrderedSpellEvents = append(result.OrderedSpellEvents, OrderedSpellEvent{Kind: OrderedSpellEventCharacterStatus, Character: target})
				}
				continue
			}
			mon := w.monsters[pending.TargetMonsterID]
			if mon == nil {
				continue
			}
			if mon.TransparentUntil.IsZero() {
				mon.TransparentUntil = transparentUntil
				w.breakNearbyMonsterTargetsForMonsterStealthLocked(mon)
				result.StatusRefreshMonsters = append(result.StatusRefreshMonsters, *mon)
				result.OrderedStatusRefreshes = append(result.OrderedStatusRefreshes, StatusRefreshEvent{Monster: mon})
				appendMonsterPhaseEvent(OrderedSpellEvent{Kind: OrderedSpellEventMonsterStatus, Monster: *mon})
			}
			continue
		}
		if pending.PoisonDuration > 0 || pending.PoisonHealth || pending.PoisonArmor || pending.ParalysisDuration > 0 {
			poisonUntil := pending.PoisonUntil
			if pending.PoisonDuration > 0 {
				poisonUntil = now.Add(pending.PoisonDuration)
			}
			if pending.TargetCharacterID != "" {
				target, ok := players[pending.TargetCharacterID]
				if !ok {
					continue
				}
				if caster, ok := players[pending.CasterID]; ok && caster.ID != target.ID && w.isProperCharacterTargetLocked(target, caster) {
					setCharacterTargetLocked(&target, caster.ID, now)
					w.setCharacterLastHitterAtLocked(&target, caster.ID, now)
					if w.canSetCharacterPKFlagLocked(target, caster) {
						caster.PKFlag = true
						caster.PKFlagUntil = now.Add(60 * time.Second).UnixNano()
						result.NameColorCharacters = append(result.NameColorCharacters, caster)
						result.OrderedSpellEvents = append(result.OrderedSpellEvents, OrderedSpellEvent{Kind: OrderedSpellEventCharacterNameColor, Character: caster})
					}
					players[caster.ID] = caster
					updated[caster.ID] = caster
				}
				previousStatus := characterStatus(target, now, false)
				if pending.PoisonHealth {
					setCharacterHealthPoisonLocked(&target, pending.PoisonHealthLevel, pending.PoisonPoint, poisonUntil, now)
				}
				if pending.PoisonArmor {
					setCharacterArmorPoisonLocked(&target, pending.PoisonPoint, poisonUntil, now)
				}
				if pending.ParalysisDuration > 0 && !w.characterCannotParalyzeLocked(target) {
					paralyzedUntil := now.Add(pending.ParalysisDuration).UnixNano()
					if paralyzedUntil > target.ParalyzedUntil {
						target.ParalyzedUntil = paralyzedUntil
					}
					w.resetCharacterStatusClockLocked(target.ID, statusClockParalyzed, target.ParalyzedUntil, now)
				}
				if characterStatus(target, now, false) != previousStatus {
					result.StatusRefreshCharacters = append(result.StatusRefreshCharacters, target)
					result.OrderedStatusRefreshes = append(result.OrderedStatusRefreshes, StatusRefreshEvent{Character: &target})
					result.OrderedSpellEvents = append(result.OrderedSpellEvents, OrderedSpellEvent{Kind: OrderedSpellEventCharacterStatus, Character: target})
				}
				if pending.PoisonHealth || pending.PoisonArmor || pending.PoisonNotification {
					seconds := 0
					if pending.PoisonDuration > 0 {
						seconds = int(pending.PoisonDuration / time.Second)
					} else if !poisonUntil.IsZero() {
						seconds = int(poisonUntil.Sub(now) / time.Second)
					}
					if pending.PoisonNotification && pending.ParalysisDuration > 0 {
						seconds = int(pending.ParalysisDuration / time.Second)
					}
					points := pending.PoisonPoint
					if points == 0 {
						points = int(pending.PoisonHealthLevel)
					}
					if pending.PoisonNotification {
						points = pending.PoisonPoint
					}
					notification := PoisonNotification{Character: target, Seconds: seconds, Points: points}
					result.PoisonNotifications = append(result.PoisonNotifications, notification)
					if pending.PoisonNotification {
						result.OrderedSpellEvents = append(result.OrderedSpellEvents, OrderedSpellEvent{Kind: OrderedSpellEventPoisonNotification, PoisonNotification: notification})
					}
				}
				players[target.ID] = target
				updated[target.ID] = target
				if pending.PoisonHealth || pending.PoisonArmor {
					result.AffectedCharacters = append(result.AffectedCharacters, target)
				}
				continue
			}
			mon := w.monsters[pending.TargetMonsterID]
			if mon == nil {
				continue
			}
			previousStatus := MonsterStatus(*mon, now)
			if caster, ok := players[pending.CasterID]; ok && w.isProperDelayedMonsterPoisonTargetLocked(caster, characterListFromMap(players), mon) {
				mon.TargetCharacterID = pending.CasterID
				w.setMonsterLastHitterAtLocked(mon, pending.CasterID, now)
			}
			if pending.PoisonHealth {
				setMonsterHealthPoisonLocked(mon, pending.PoisonHealthLevel, pending.PoisonPoint, poisonUntil, pending.CasterID, now)
			}
			if pending.PoisonArmor {
				setMonsterArmorPoisonLocked(mon, pending.PoisonPoint, poisonUntil, now)
			}
			if pending.ParalysisDuration > 0 {
				if caster, ok := players[pending.CasterID]; ok {
					target := *mon
					target.Alive = true
					if w.isProperMonsterTargetLocked(caster, characterListFromMap(players), &target) {
						w.setMonsterLastHitterAtLocked(mon, pending.CasterID, now)
					}
				}
				paralyzedUntil := now.Add(pending.ParalysisDuration)
				if paralyzedUntil.After(mon.ParalyzedUntil) {
					mon.ParalyzedUntil = paralyzedUntil
				}
				w.resetMonsterStatusClockLocked(mon.ID, statusClockParalyzed, mon.ParalyzedUntil, now)
			}
			if MonsterStatus(*mon, now) != previousStatus {
				result.StatusRefreshMonsters = append(result.StatusRefreshMonsters, *mon)
				result.OrderedStatusRefreshes = append(result.OrderedStatusRefreshes, StatusRefreshEvent{Monster: mon})
				appendMonsterPhaseEvent(OrderedSpellEvent{Kind: OrderedSpellEventMonsterStatus, Monster: *mon})
			}
			continue
		}
		if pending.Healing > 0 {
			if pending.TargetCharacterID != "" {
				target, ok := players[pending.TargetCharacterID]
				if !ok {
					continue
				}
				updatedTarget := target
				if updatedTarget.IncHealing+pending.Healing < 300 {
					updatedTarget.IncHealing += pending.Healing
					updatedTarget.PerHealing = 5
				} else {
					updatedTarget.IncHealing = 300
				}
				if updatedTarget.IncHealing != target.IncHealing {
					players[updatedTarget.ID] = updatedTarget
					updated[updatedTarget.ID] = updatedTarget
					result.HealingCharacters = append(result.HealingCharacters, updatedTarget.ID)
				}
				continue
			}
			mon := w.monsters[pending.TargetMonsterID]
			if mon == nil {
				continue
			}
			if mon.IncHealing+pending.Healing < 300 {
				mon.IncHealing += pending.Healing
				mon.PerHealing = 5
			} else {
				mon.IncHealing = 300
			}
			continue
		}
		if pending.CharacterPush {
			target, ok := players[pending.TargetCharacterID]
			if !ok {
				continue
			}
			playerList := make([]storage.Character, 0, len(players))
			for _, player := range players {
				playerList = append(playerList, player)
			}
			occupied := w.occupiedActorsLocked(playerList)
			updatedTarget, pushes, moved, err := w.pushCharacterInDirectionLocked(target, pending.PushCount, pending.PushDir, w.data.Maps[target.MapID], occupied)
			if err != nil {
				return err
			}
			if !moved {
				continue
			}
			players[target.ID] = updatedTarget
			updated[target.ID] = updatedTarget
			for _, push := range pushes {
				result.OrderedSpellEvents = append(result.OrderedSpellEvents, OrderedSpellEvent{Kind: OrderedSpellEventCharacterPush, CharacterPush: push})
			}
			continue
		}
		if pending.MonsterPush {
			mon := w.monsters[pending.TargetMonsterID]
			if mon == nil {
				continue
			}
			playerList := make([]storage.Character, 0, len(players))
			for _, player := range players {
				playerList = append(playerList, player)
			}
			occupied := w.occupiedActorsLocked(playerList)
			_, actions, moved := w.pushMonsterInDirectionLocked(mon, pending.PushCount, pending.PushDir, w.data.Maps[mon.MapID], occupied)
			if !moved {
				continue
			}
			result.MonsterActions = append(result.MonsterActions, actions...)
			continue
		}
		caster, ok := players[pending.CasterID]
		if !ok {
			continue
		}
		setCasterTarget := func(targetID string) error {
			if !pending.SetCasterTarget || targetID == "" {
				return nil
			}
			setCharacterTargetLocked(&caster, targetID, now)
			players[caster.ID] = caster
			updated[caster.ID] = caster
			return nil
		}
		if pending.CharacterDamage {
			target, ok := players[pending.TargetCharacterID]
			if !ok {
				continue
			}
			if pending.SingleMagicStrike {
				var damage int
				target, damage = w.prepareCharacterMagicDamageLocked(target, pending.Damage, now)
				if damage <= 0 {
					players[target.ID] = target
					updated[target.ID] = target
					continue
				}
				updatedTarget, hit, err := w.applyReferenceCharacterDamageAtLocked(caster, target, damage, now, true)
				if err != nil {
					return err
				}
				players[updatedTarget.ID] = updatedTarget
				updated[updatedTarget.ID] = updatedTarget
				if hit.Damage > 0 {
					result.CharacterHits = append(result.CharacterHits, hit)
					result.OrderedSpellEvents = append(result.OrderedSpellEvents, OrderedSpellEvent{Kind: OrderedSpellEventCharacterHit, CharacterHit: hit})
				}
				continue
			}
			if pending.CharacterBubbleAfter != 0 && target.BubbleDefenceUntil == pending.CharacterBubbleBefore {
				target.BubbleDefenceUntil = pending.CharacterBubbleAfter
				target.BubbleDefenceLevel = pending.CharacterBubbleLevel
			}
			precheckDamage := pending.Damage
			if !pending.CharacterDamagePrepared {
				precheckDamage = w.characterMagicDamageAfterDefenseLocked(target, pending.Damage, now)
				precheckDamage = applyCharacterMagicBubbleLocked(&target, precheckDamage, now)
			}
			if precheckDamage > 0 {
				if err := setCasterTarget(target.ID); err != nil {
					return err
				}
			}
			targetRange := pending.TargetRange
			if targetRange == 0 {
				targetRange = 2
			}
			if abs(target.X-pending.TargetX) > targetRange || abs(target.Y-pending.TargetY) > targetRange {
				players[target.ID] = target
				updated[target.ID] = target
				continue
			}
			if precheckDamage <= 0 {
				players[target.ID] = target
				updated[target.ID] = target
				continue
			}
			if !pending.CharacterDamagePrepared {
				var finalDamage int
				target, finalDamage = w.prepareCharacterMagicDamageLocked(target, pending.Damage, now)
				if finalDamage <= 0 {
					players[target.ID] = target
					updated[target.ID] = target
					continue
				}
				precheckDamage = finalDamage
			}
			updatedTarget, hit, err := w.applyReferenceCharacterDamageAtLocked(caster, target, precheckDamage, now, true)
			if err != nil {
				return err
			}
			players[updatedTarget.ID] = updatedTarget
			updated[updatedTarget.ID] = updatedTarget
			if hit.Damage <= 0 {
				continue
			}
			result.CharacterHits = append(result.CharacterHits, hit)
			result.OrderedSpellEvents = append(result.OrderedSpellEvents, OrderedSpellEvent{Kind: OrderedSpellEventCharacterHit, CharacterHit: hit})
			continue
		}
		mon := w.monsters[pending.TargetMonsterID]
		if mon == nil {
			continue
		}
		if pending.SingleMagicStrike {
			w.monsterMagicStruckLocked(mon, now)
			damage := w.monsterMagicDamageAfterDefenseLocked(mon, pending.Damage)
			if damage <= 0 {
				continue
			}
			hit, err := w.applyReferenceMonsterMagicDamageAtLocked(caster, mon, damage, now)
			if err != nil {
				return err
			}
			if hit.Damage > 0 {
				result.MonsterHits = append(result.MonsterHits, hit)
				appendMonsterPhaseEvent(OrderedSpellEvent{Kind: OrderedSpellEventMonsterHit, MonsterHit: hit})
			}
			continue
		}
		precheckDamage := pending.Damage
		precheckDamage = w.monsterMagicDamageAfterDefenseLocked(mon, pending.Damage)
		if mon.Undead > 0 {
			precheckDamage += w.combatStatsLocked(caster).Undead
		}
		if precheckDamage <= 0 {
			continue
		}
		if err := setCasterTarget(mon.ID); err != nil {
			return err
		}
		targetRange := pending.TargetRange
		if targetRange == 0 {
			targetRange = 2
		}
		if abs(mon.X-pending.TargetX) > targetRange || abs(mon.Y-pending.TargetY) > targetRange {
			continue
		}
		damage := pending.Damage
		if mon.Race >= 50 {
			damage = referenceRound(float64(pending.Damage) / 1.2)
		}
		w.monsterMagicStruckLocked(mon, now)
		damage = w.monsterMagicDamageAfterDefenseLocked(mon, damage)
		hit, err := w.applyReferenceMonsterMagicDamageAtLocked(caster, mon, damage, now)
		if err != nil {
			return err
		}
		if hit.Damage <= 0 {
			continue
		}
		result.MonsterHits = append(result.MonsterHits, hit)
		appendMonsterPhaseEvent(OrderedSpellEvent{Kind: OrderedSpellEventMonsterHit, MonsterHit: hit})
		players[hit.Character.ID] = hit.Character
		updated[hit.Character.ID] = hit.Character
		experience := SpellExperience{CharacterID: hit.Character.ID, Experience: hit.Experience, CurrentExp: hit.CurrentExp, LevelUp: hit.LevelUp, Character: hit.Character}
		result.SpellExperience = append(result.SpellExperience, experience)
		appendMonsterPhaseEvent(OrderedSpellEvent{Kind: OrderedSpellEventCharacterExperience, Experience: experience})
	}
	w.pendingSpells = remaining
	return nil
}

func pendingSpellPhaseForTarget(pending pendingSpell) pendingSpellPhase {
	if pending.TargetCharacterID != "" {
		return pendingSpellPhaseCharacter
	}
	if pending.TargetMonsterID != "" {
		return pendingSpellPhaseMonster
	}
	return pendingSpellPhaseAll
}

func pendingSpellTargetKey(pending pendingSpell) string {
	if pending.TargetCharacterID != "" {
		return "character:" + pending.TargetCharacterID
	}
	if pending.TargetMonsterID != "" {
		return "monster:" + pending.TargetMonsterID
	}
	return ""
}

func pendingSpellTargetClass(pending pendingSpell) int {
	if pending.TargetCharacterID != "" {
		return 0
	}
	if pending.TargetMonsterID != "" {
		return 1
	}
	return 2
}

func (w *World) pendingSpellTargetOrderLocked(pending pendingSpell, players map[string]storage.Character) (uint64, bool) {
	if pending.TargetCharacterID != "" {
		character, ok := players[pending.TargetCharacterID]
		if processOrder, known := w.tickCharacterProcessOrders[pending.TargetCharacterID]; known {
			return processOrder, processOrder != 0 && ok
		}
		return character.ObjectOrder, ok && character.ObjectOrder != 0
	}
	if pending.TargetMonsterID != "" {
		monster, ok := w.monsters[pending.TargetMonsterID]
		if !ok || monster == nil {
			return 0, false
		}
		return monster.ObjectOrder, monster.ObjectOrder != 0
	}
	return 0, false
}

func (w *World) applyMonsterNaturalSpellTickLocked(mon *Monster, now time.Time) []Monster {
	if mon == nil || !mon.Alive {
		return nil
	}
	nowMS := now.UnixMilli()
	if mon.HealthTickAt == 0 {
		mon.HealthTickAt = nowMS
	}
	if mon.SpellTickAt == 0 {
		mon.SpellTickAt = nowMS
	}
	healthElapsed := nowMS - mon.HealthTickAt
	spellElapsed := nowMS - mon.SpellTickAt
	if healthElapsed > 0 {
		mon.HealthTickAt = nowMS
		mon.HealthTick += int(healthElapsed / 20)
	}
	if spellElapsed > 0 {
		mon.SpellTickAt = nowMS
		mon.SpellTick += int(spellElapsed / 20)
	}
	healthFillMS := w.gameplay.Recovery.HealthFillTimeMS
	if healthFillMS <= 0 {
		healthFillMS = 300
	}
	spellFillMS := w.gameplay.Recovery.SpellFillTimeMS
	if spellFillMS <= 0 {
		spellFillMS = 800
	}
	changes := make([]Monster, 0, 2)
	if mon.HP < mon.MaxHP && mon.HealthTick >= healthFillMS {
		mon.HealthTick = 0
		gain := mon.MaxHP/75 + 1
		if mon.HP+gain > mon.MaxHP {
			gain = mon.MaxHP - mon.HP
		}
		if gain > 0 {
			mon.HP += gain
			changes = append(changes, *mon)
		}
	} else if mon.HealthTick >= healthFillMS {
		mon.HealthTick = 0
	}
	if mon.MP < mon.MaxMP && mon.SpellTick >= spellFillMS {
		mon.SpellTick = 0
		gain := mon.MaxMP/18 + 1
		if mon.MP+gain > mon.MaxMP {
			gain = mon.MaxMP - mon.MP
		}
		if gain > 0 {
			mon.MP += gain
			changes = append(changes, *mon)
		}
	} else if mon.SpellTick >= spellFillMS {
		mon.SpellTick = 0
	}
	return changes
}

func (w *World) applyMonsterNegativeHealthTickLocked(mon *Monster) bool {
	if mon == nil || !mon.Alive {
		return false
	}
	healthFillMS := w.gameplay.Recovery.HealthFillTimeMS
	if healthFillMS <= 0 {
		healthFillMS = 300
	}
	if mon.HealthTick < -healthFillMS && mon.HP > 1 {
		mon.HP--
		mon.HealthTick += healthFillMS
		return true
	}
	return false
}

func (w *World) applyMonsterHealingTickLocked(mon *Monster, now time.Time) bool {
	if mon == nil || !mon.Alive {
		return false
	}
	if mon.IncHealth <= 0 && mon.IncSpell <= 0 && mon.IncHealing <= 0 {
		mon.IncHealthSpellAt = now.UnixMilli()
		return false
	}
	interval := core.RecoveryInterval(mon.Level)
	nextAt := time.UnixMilli(mon.IncHealthSpellAt)
	if mon.IncHealthSpellAt != 0 && now.Before(nextAt.Add(interval)) {
		return false
	}
	overrun := now.Sub(nextAt) - interval
	if overrun < 0 {
		overrun = 0
	}
	if overrun > 200*time.Millisecond {
		overrun = 200 * time.Millisecond
	}
	perHealing := mon.PerHealing
	if perHealing <= 0 {
		perHealing = 1
	}
	perHealth := mon.PerHealth
	if perHealth <= 0 {
		perHealth = 1
	}
	perSpell := mon.PerSpell
	if perSpell <= 0 {
		perSpell = 1
	}
	queuedHealthBefore := mon.IncHealth
	queuedSpellBefore := mon.IncSpell
	queuedHealingBefore := mon.IncHealing
	healing := mon.IncHealing
	if healing > perHealing {
		healing = perHealing
	}
	hp := mon.IncHealth
	if hp > perHealth {
		hp = perHealth
	}
	mp := mon.IncSpell
	if mp > perSpell {
		mp = perSpell
	}
	healed := core.ApplyVitalDelta(storage.Character{HP: mon.HP, MaxHP: mon.MaxHP, MP: mon.MP, MaxMP: mon.MaxMP}, hp+healing, mp)
	mon.IncHealth -= hp
	mon.IncSpell -= mp
	mon.IncHealing -= healing
	mon.IncHealthSpellAt = now.Add(overrun).UnixMilli()
	mon.PerHealth = mon.Level/10 + 5
	mon.PerSpell = mon.Level/10 + 5
	mon.PerHealing = 5
	if healed.Changed {
		mon.HP = healed.Character.HP
		mon.MP = healed.Character.MP
	}
	if mon.HP >= mon.MaxHP {
		mon.IncHealth = 0
		mon.IncHealing = 0
	}
	if mon.MP >= mon.MaxMP {
		mon.IncSpell = 0
	}
	queuedChanged := mon.IncHealth != queuedHealthBefore || mon.IncSpell != queuedSpellBefore || mon.IncHealing != queuedHealingBefore
	return healed.Changed || queuedChanged
}

func (w *World) applyMonsterInheritedStateTickLocked(mon *Monster, now time.Time, result *TickResult) {
	paralyzedExpired := false
	transparentExpired := false
	statusChanged := false
	if !mon.HolySeizeUntil.IsZero() && now.After(mon.HolySeizeUntil) {
		mon.HolySeizeUntil = time.Time{}
		result.NameColorMonsters = append(result.NameColorMonsters, *mon)
		result.OrderedMonsterEvents = append(result.OrderedMonsterEvents, OrderedMonsterEvent{Kind: OrderedMonsterEventNameColor, Monster: *mon})
	}
	if !mon.CrazyUntil.IsZero() && now.After(mon.CrazyUntil) {
		mon.CrazyUntil = time.Time{}
		result.NameColorMonsters = append(result.NameColorMonsters, *mon)
		result.OrderedMonsterEvents = append(result.OrderedMonsterEvents, OrderedMonsterEvent{Kind: OrderedMonsterEventNameColor, Monster: *mon})
	}
	if w.applyMonsterShowHPOpenTickLocked(mon, now) {
		result.ShowHPOpenedMonsters = append(result.ShowHPOpenedMonsters, *mon)
		result.OrderedMonsterEvents = append(result.OrderedMonsterEvents, OrderedMonsterEvent{Kind: OrderedMonsterEventOpenHealth, Monster: *mon})
	}
	if w.applyMonsterShowHPTickLocked(mon, now) && mon.ShowHPUntil == 0 {
		result.ShowHPExpiredMonsters = append(result.ShowHPExpiredMonsters, *mon)
		result.PostActionMonsterCloseHealthRefreshes = append(result.PostActionMonsterCloseHealthRefreshes, *mon)
		result.OrderedMonsterEvents = append(result.OrderedMonsterEvents, OrderedMonsterEvent{Kind: OrderedMonsterEventCloseHealth, Monster: *mon})
	}
	if !mon.ParalyzedUntil.IsZero() {
		until, active, clockChanged := w.advanceMonsterStatusClockLocked(mon.ID, statusClockParalyzed, mon.ParalyzedUntil, now)
		if !active {
			mon.ParalyzedUntil = time.Time{}
			paralyzedExpired = true
			statusChanged = true
		} else if clockChanged {
			mon.ParalyzedUntil = until
		}
	}
	if !mon.TransparentUntil.IsZero() {
		until, active, clockChanged := w.advanceMonsterStatusClockLocked(mon.ID, statusClockTransparent, mon.TransparentUntil, now)
		if !active {
			mon.TransparentUntil = time.Time{}
			transparentExpired = true
			statusChanged = true
		} else if clockChanged {
			mon.TransparentUntil = until
		}
	}
	if transparentExpired {
		statusChanged = true
	}
	if w.applyMonsterProtectionTickLocked(mon, now) {
		statusChanged = true
	}
	if statusChanged || paralyzedExpired || transparentExpired {
		result.StatusRefreshMonsters = append(result.StatusRefreshMonsters, *mon)
		result.PostActionMonsterStatusRefreshes = append(result.PostActionMonsterStatusRefreshes, *mon)
		result.OrderedMonsterEvents = append(result.OrderedMonsterEvents, OrderedMonsterEvent{Kind: OrderedMonsterEventStatus, Monster: *mon})
	}
}

func (w *World) monsterBaseRunBeforeBehaviorLocked(mon *Monster) bool {
	return false
}

func (w *World) applyMonsterRecoveryTickLocked(mon *Monster, now time.Time, result *TickResult) {
	for _, changed := range w.applyMonsterNaturalSpellTickLocked(mon, now) {
		result.PostActionMonsterHealthRefreshes = append(result.PostActionMonsterHealthRefreshes, changed)
		result.OrderedMonsterEvents = append(result.OrderedMonsterEvents, OrderedMonsterEvent{Kind: OrderedMonsterEventHealth, Monster: changed})
	}
	if mon.PendingDeath && mon.HP <= 0 {
		return
	}
	if w.applyMonsterHealingTickLocked(mon, now) {
		result.PostActionMonsterHealthRefreshes = append(result.PostActionMonsterHealthRefreshes, *mon)
		result.OrderedMonsterEvents = append(result.OrderedMonsterEvents, OrderedMonsterEvent{Kind: OrderedMonsterEventHealth, Monster: *mon})
	}
	if w.applyMonsterNegativeHealthTickLocked(mon) {
		result.PostActionMonsterHealthRefreshes = append(result.PostActionMonsterHealthRefreshes, *mon)
		result.OrderedMonsterEvents = append(result.OrderedMonsterEvents, OrderedMonsterEvent{Kind: OrderedMonsterEventHealth, Monster: *mon})
	}
}

func (w *World) applyMonsterPoisonToTickResultLocked(mon *Monster, players map[string]storage.Character, now time.Time, result *TickResult) (bool, error) {
	previousStatus := MonsterStatus(*mon, now)
	poisonHits, killed, err := w.applyMonsterPoisonTickLocked(mon, players, now)
	if err != nil {
		return false, err
	}
	if MonsterStatus(*mon, now) != previousStatus {
		result.StatusRefreshMonsters = append(result.StatusRefreshMonsters, *mon)
		result.OrderedMonsterEvents = append(result.OrderedMonsterEvents, OrderedMonsterEvent{Kind: OrderedMonsterEventStatus, Monster: *mon})
	}
	result.MonsterHits = append(result.MonsterHits, poisonHits...)
	return killed, nil
}

func (w *World) tickMonsterLocked(mon *Monster, players map[string]storage.Character, now time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	if mon.RunIntervalMS > 0 && !mon.NextRunAt.IsZero() && !now.After(mon.NextRunAt) {
		return nil, nil, nil, nil
	}
	if mon.RunIntervalMS > 0 {
		mon.NextRunAt = now.Add(time.Duration(mon.RunIntervalMS) * time.Millisecond)
	}
	actions, hits, characters, err := w.tickMonsterBehaviorOnlyLocked(mon, players, now)
	w.cleanupMonsterTargetAfterRunLocked(mon, players, now)
	return actions, hits, characters, err
}

func (w *World) tickMonsterBehaviorOnlyLocked(mon *Monster, players map[string]storage.Character, now time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	return w.tickMonsterBehaviorLocked(mon, players, now)
}

func (w *World) releaseExpiredMonsterMasterLocked(mon *Monster, now time.Time) bool {
	if mon.MasterID == "" || mon.MasterExpiresAt.IsZero() || !now.After(mon.MasterExpiresAt) {
		return false
	}
	mon.MasterID = ""
	mon.MasterName = ""
	mon.MasterExpiresAt = time.Time{}
	mon.NoTame = false
	mon.HP /= 10
	return true
}

func (w *World) cleanupMonsterHitterRefsLocked(mon *Monster, players map[string]storage.Character, now time.Time) {
	if mon.LastHitterID != "" && !mon.LastHitterAt.IsZero() && (now.Sub(mon.LastHitterAt) > 30*time.Second || !w.monsterAttackerAliveLocked(mon.LastHitterID, players)) {
		mon.LastHitterID = ""
		mon.LastHitterAt = time.Time{}
	}
	if mon.ExpHitterID != "" && !mon.ExpHitterAt.IsZero() && (now.Sub(mon.ExpHitterAt) > 6*time.Second || !w.monsterAttackerAliveLocked(mon.ExpHitterID, players)) {
		mon.ExpHitterID = ""
		mon.ExpHitterAt = time.Time{}
	}
}

func (w *World) tickMonsterSpecialPreludeLocked(mon *Monster, players map[string]storage.Character, now time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	switch {
	case w.monsterIsBeeQueenLocked(mon):
		if !w.referenceSpecialWalkReadyInclusiveLocked(mon, now) {
			return nil, nil, nil, nil
		}
		actions, hits, chars, err := w.tickBeeQueenLocked(mon, players, now)
		if err != nil {
			return nil, nil, nil, err
		}
		w.cleanupTrackedMonsterChildrenLocked(mon)
		return actions, hits, chars, nil
	case w.monsterIsExplosionSpiderLocked(mon):
		return w.explosionSpiderLocked(mon, players, now)
	case w.monsterIsStoneLocked(mon):
		return w.tickStoneCustomLocked(mon, players, now, nil)
	case w.monsterIsDualAxeLocked(mon) || w.monsterIsThornDarkLocked(mon):
		return w.tickDualAxeMonsterLocked(mon, players, now)
	case w.monsterIsSpiderHouseLocked(mon):
		if !w.referenceSpecialWalkReadyInclusiveLocked(mon, now) {
			return nil, nil, nil, nil
		}
		actions, hits, chars, err := w.tickSpiderHouseLocked(mon, players, now)
		if err != nil {
			return nil, nil, nil, err
		}
		w.cleanupTrackedMonsterChildrenLocked(mon)
		return actions, hits, chars, nil
	case w.monsterIsBigHeartLocked(mon):
		return w.tickBigHeartLocked(mon, players, now)
	case w.monsterIsElectronicScorpionLocked(mon):
		return w.tickElectronicScorpionLocked(mon, players, now)
	case w.monsterIsCowKingLocked(mon):
		return w.tickCowKingMonsterLocked(mon, players, now, nil)
	case w.monsterIsMagicCowLocked(mon):
		return w.tickMagicCowMonsterLocked(mon, players, now)
	case w.monsterIsDigOutZombieLocked(mon):
		return w.tickDigOutZombieLocked(mon, players, now)
	case mon.Race == 94:
		return w.tickLightingZombieLocked(mon, players, now)
	case w.monsterIsGasAttackLocked(mon):
		if !w.gasMothSearchReadyLocked(mon, now) {
			return nil, nil, nil, nil
		}
		return w.tickGasAttackMonsterLocked(mon, players, now)
	case w.monsterIsSpitSpiderLocked(mon):
		return w.tickSpitSpiderLocked(mon, players, now)
	case w.monsterIsStickLocked(mon):
		if !w.referenceSpecialWalkReadyLocked(mon, now) {
			return nil, nil, nil, nil
		}
		return w.tickStickMonsterLocked(mon, players, now)
	case w.monsterIsCentipedeLocked(mon):
		return w.tickCentipedeMonsterLocked(mon, players, now)
	}
	return nil, nil, nil, nil
}

func (w *World) monsterUsesSpecialPreludeLocked(mon *Monster) bool {
	return w.monsterIsBeeQueenLocked(mon) || w.monsterIsExplosionSpiderLocked(mon) || w.monsterIsStoneLocked(mon) || w.monsterIsDualAxeLocked(mon) || w.monsterIsThornDarkLocked(mon) || w.monsterIsSpiderHouseLocked(mon) || w.monsterIsBigHeartLocked(mon) || w.monsterIsElectronicScorpionLocked(mon) || w.monsterIsCowKingLocked(mon) || w.monsterIsMagicCowLocked(mon) || w.monsterIsDigOutZombieLocked(mon) || mon.Race == 94 || w.monsterIsGasAttackLocked(mon) || w.monsterIsSpitSpiderLocked(mon) || w.monsterIsStickLocked(mon) || w.monsterIsCentipedeLocked(mon)
}

func (w *World) tickMonsterBehaviorLocked(mon *Monster, players map[string]storage.Character, now time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	if mon.CastleDoor {
		return nil, nil, nil, nil
	}
	actions := []MonsterAction{}
	if now.Year() >= 2000 && mon.Spawn.MapID != "" && mon.NextSearchAt.IsZero() {
		mon.NextSearchAt = now.Add(time.Duration(w.monsterInitialSearchDelayMSLocked(mon)) * time.Millisecond)
	}
	if w.monsterIsWhiteSkeletonLocked(mon) && mon.FirstRevealPending {
		mon.FirstRevealPending = false
		mon.Hidden = false
		mon.FixedHideMode = false
		mon.Dir = 5
		actions = append(actions, w.monsterActionLocked(mon, MonsterActionReveal))
		if mon.MasterID != "" {
			return w.finishMonsterTickLocked(mon, players, now, actions, w.tickSummonedMonsterBehaviorLocked)
		}
		return w.finishMonsterTickLocked(mon, players, now, actions, w.tickNormalMonsterLocked)
	}
	if mon.MasterID != "" && mon.FleeOnSight {
		if w.animalWalkDueLocked(mon, now) {
			w.selectFleeAnimalTargetLocked(mon, players, now)
		}
		if mon.RunAwayMode {
			return w.fleeAnimalActionLocked(mon, players, now)
		}
	}
	if mon.MasterID != "" {
		specialActions, specialHits, specialCharacters, err := w.tickMonsterSpecialPreludeLocked(mon, players, now)
		if err != nil {
			return nil, nil, nil, err
		}
		if w.monsterUsesAnimalObjectRunLocked(mon) || (w.monsterUsesSpecialPreludeLocked(mon) && (mon.FixedHideMode || mon.StoneMode)) {
			return append(actions, specialActions...), specialHits, specialCharacters, nil
		}
		moreActions, hits, characters, err := w.finishMonsterTickLocked(mon, players, now, specialActions, w.tickSummonedMonsterBehaviorLocked)
		if err != nil {
			return nil, nil, nil, err
		}
		return append(actions, moreActions...), append(specialHits, hits...), append(specialCharacters, characters...), nil
	}
	if mon.RunAwayMode && !mon.RunAwayUntil.IsZero() {
		if now.After(mon.RunAwayUntil) {
			mon.RunAwayMode = false
			mon.RunAwayUntil = time.Time{}
			mon.TargetX = -1
			mon.TargetY = -1
		} else if mon.TargetCharacterID != "" {
			return w.finishMonsterTickLocked(mon, players, now, actions, w.tickFleeAnimalMonsterLocked)
		}
	}
	if w.monsterIsBeeQueenLocked(mon) {
		return w.tickInclusiveCustomAndInheritedLocked(mon, players, now, actions, w.tickBeeQueenLocked)
	}
	if w.monsterIsExplosionSpiderLocked(mon) {
		return w.tickExplosionSpiderAndInheritedLocked(mon, players, now, actions)
	}
	if w.monsterIsStoneLocked(mon) {
		resultActions, hits, chars, err := w.tickStoneReferenceCustomLocked(mon, players, now, actions)
		if err != nil {
			return nil, nil, nil, err
		}
		if !mon.StoneMode {
			inheritedActions, inheritedHits, inheritedChars, err := w.finishMonsterTickLocked(mon, players, now, resultActions, w.tickStoneAttackLocked)
			if err != nil {
				return nil, nil, nil, err
			}
			return inheritedActions, append(hits, inheritedHits...), append(chars, inheritedChars...), nil
		}
		return resultActions, hits, chars, nil
	}
	if w.monsterIsDualAxeLocked(mon) {
		return w.tickAlwaysInheritedCustomLocked(mon, players, now, actions, w.tickDualAxeMonsterLocked)
	}
	if w.monsterIsThornDarkLocked(mon) {
		return w.tickAlwaysInheritedCustomLocked(mon, players, now, actions, w.tickDualAxeMonsterLocked)
	}
	if w.monsterIsSpiderHouseLocked(mon) {
		return w.tickInclusiveCustomAndInheritedLocked(mon, players, now, actions, w.tickSpiderHouseLocked)
	}
	if w.monsterIsBigHeartLocked(mon) {
		return w.tickBigHeartAndInheritedLocked(mon, players, now, actions)
	}
	if w.monsterIsElectronicScorpionLocked(mon) {
		return w.tickElectronicScorpionAndInheritedLocked(mon, players, now, actions)
	}
	if w.monsterIsCowKingLocked(mon) {
		return w.tickCowKingMonsterLocked(mon, players, now, actions)
	}
	if w.monsterIsMagicCowLocked(mon) {
		return w.tickAlwaysInheritedCustomLocked(mon, players, now, actions, w.tickMagicCowMonsterLocked)
	}
	if w.monsterIsDigOutZombieLocked(mon) {
		return w.tickDigOutAndInheritedLocked(mon, players, now, actions)
	}
	if mon.Race == 94 {
		return w.tickLightingZombieAndInheritedLocked(mon, players, now, actions)
	}
	if w.monsterIsGasAttackLocked(mon) {
		return w.tickGasAttackAndInheritedLocked(mon, players, now, actions)
	}
	if w.monsterIsSpitSpiderLocked(mon) {
		return w.tickSpitSpiderAndInheritedLocked(mon, players, now, actions)
	}
	if w.monsterIsArcherLocked(mon) {
		return w.tickArcherAndInheritedLocked(mon, players, now, actions)
	}
	if w.monsterIsStickLocked(mon) {
		return w.tickStickAndInheritedLocked(mon, players, now, actions)
	}
	if w.monsterIsCentipedeLocked(mon) {
		return w.tickCentipedeAndInheritedLocked(mon, players, now, actions)
	}
	if mon.FixedHideMode || mon.StoneMode {
		return actions, nil, nil, nil
	}
	if w.monsterIsAnimalLocked(mon) {
		if !mon.FleeOnSight {
			return w.finishMonsterTickLocked(mon, players, now, actions, w.tickPassiveMonsterLocked)
		}
		return w.finishAnimalMonsterTickLocked(mon, players, now, actions)
	}
	if w.monsterIsPassiveLocked(mon) {
		return w.finishMonsterTickLocked(mon, players, now, actions, w.tickPassiveMonsterLocked)
	}
	return w.finishMonsterTickLocked(mon, players, now, actions, w.tickNormalMonsterLocked)
}

func (w *World) finishAnimalMonsterTickLocked(mon *Monster, players map[string]storage.Character, now time.Time, prefix []MonsterAction) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	if !w.animalWalkDueLocked(mon, now) {
		if w.thinkMonsterLocked(mon, players, now) {
			return append(prefix, w.monsterActionLocked(mon, MonsterActionWalk)), nil, nil, nil
		}
		return prefix, nil, nil, nil
	}
	w.selectFleeAnimalTargetLocked(mon, players, now)
	prepared := w.prepareFleeAnimalTargetLocked(mon, players, now)
	if w.thinkMonsterLocked(mon, players, now) {
		return append(prefix, w.monsterActionLocked(mon, MonsterActionWalk)), nil, nil, nil
	}
	if !w.monsterWalkReadyInclusiveLocked(mon, now) {
		return prefix, nil, nil, nil
	}
	if !prepared {
		actions, hits, chars, err := w.tickPassiveMonsterLocked(mon, players, now)
		if err != nil {
			return nil, nil, nil, err
		}
		return append(prefix, actions...), hits, chars, nil
	}
	actions, hits, chars, err := w.moveFleeAnimalTargetLocked(mon, players, now)
	if err != nil {
		return nil, nil, nil, err
	}
	return append(prefix, actions...), hits, chars, nil
}

func (w *World) animalWalkDueLocked(mon *Monster, now time.Time) bool {
	if mon.LastWalkAt.IsZero() {
		mon.LastWalkAt = now.Add(-time.Duration(w.monsterWalkSpeedMSLocked(mon)) * time.Millisecond)
	} else if now.Before(mon.LastWalkAt) {
		return false
	}
	if mon.WalkWaitLocked {
		if mon.WalkWait <= 0 || now.Sub(mon.WalkWaitTick) > time.Duration(mon.WalkWait)*time.Millisecond {
			mon.WalkWaitLocked = false
		} else {
			return false
		}
	}
	return now.Sub(mon.LastWalkAt) >= time.Duration(w.monsterWalkSpeedMSLocked(mon))*time.Millisecond
}

func (w *World) tickSpitSpiderAndInheritedLocked(mon *Monster, players map[string]storage.Character, now time.Time, prefix []MonsterAction) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	w.updateReferencePeriodicSearchLocked(mon, players, now)
	actions, hits, chars, err := w.tickSpitSpiderTargetLocked(mon, players, now)
	if err != nil {
		return nil, nil, nil, err
	}
	combined := append(prefix, actions...)
	if w.thinkMonsterLocked(mon, players, now) {
		return append(combined, w.monsterActionLocked(mon, MonsterActionWalk)), hits, chars, nil
	}
	if len(actions) > 0 || mon.PendingDeath {
		return combined, hits, chars, nil
	}
	if target := w.monsters[mon.TargetMonsterID]; target != nil && w.monsterSpitMonsterTargetInDirectionLocked(mon, target, direction(mon.X, mon.Y, target.X, target.Y)) {
		return combined, hits, chars, nil
	}
	if target, ok := players[mon.TargetCharacterID]; ok && w.monsterSpitTargetInDirectionLocked(mon, target, direction(mon.X, mon.Y, target.X, target.Y)) {
		return combined, hits, chars, nil
	}
	inheritedActions, inheritedHits, inheritedChars, err := w.finishMonsterTickWithoutSearchLocked(mon, players, now, combined, w.tickNormalMonsterWithoutSearchLocked)
	if err != nil {
		return nil, nil, nil, err
	}
	return inheritedActions, append(hits, inheritedHits...), append(chars, inheritedChars...), nil
}

func (w *World) tickGasAttackAndInheritedLocked(mon *Monster, players map[string]storage.Character, now time.Time, prefix []MonsterAction) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	if !w.gasMothSearchReadyLocked(mon, now) {
		return w.finishMonsterTickWithoutSearchLocked(mon, players, now, prefix, w.tickNormalMonsterWithoutSearchLocked)
	}
	w.prepareGasAttackTargetLocked(mon, players, now)
	if w.thinkMonsterLocked(mon, players, now) {
		return append(prefix, w.monsterActionLocked(mon, MonsterActionWalk)), nil, nil, nil
	}
	actions, hits, chars, err := w.tickGasAttackTargetLocked(mon, players, now)
	if err != nil {
		return nil, nil, nil, err
	}
	combined := append(prefix, actions...)
	if len(actions) > 0 || mon.PendingDeath {
		return combined, hits, chars, nil
	}
	if target, ok := players[mon.TargetCharacterID]; ok && (mon.X != target.X || mon.Y != target.Y) && abs(mon.X-target.X) <= 1 && abs(mon.Y-target.Y) <= 1 {
		return combined, hits, chars, nil
	}
	if target := w.monsters[mon.TargetMonsterID]; target != nil && target.MapID == mon.MapID && (mon.X != target.X || mon.Y != target.Y) && abs(mon.X-target.X) <= 1 && abs(mon.Y-target.Y) <= 1 {
		return combined, hits, chars, nil
	}
	inheritedActions, inheritedHits, inheritedChars, err := w.finishMonsterTickWithoutSearchLocked(mon, players, now, combined, w.tickNormalMonsterWithoutSearchLocked)
	if err != nil {
		return nil, nil, nil, err
	}
	return inheritedActions, append(hits, inheritedHits...), append(chars, inheritedChars...), nil
}

func (w *World) updateATMonsterSearchLocked(mon *Monster, players map[string]storage.Character, now time.Time) {
	if mon.LastTargetSearchAt.IsZero() {
		mon.LastTargetSearchAt = now
		return
	}
	if now.Sub(mon.LastTargetSearchAt) > 8*time.Second || (mon.TargetCharacterID == "" && mon.TargetMonsterID == "" && now.Sub(mon.LastTargetSearchAt) > time.Second) {
		mon.LastTargetSearchAt = now
		w.searchMonsterTargetLocked(mon, players, now)
	}
}

func (w *World) tickAlwaysInheritedCustomLocked(mon *Monster, players map[string]storage.Character, now time.Time, prefix []MonsterAction, custom func(*Monster, map[string]storage.Character, time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error)) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	actions, hits, chars, err := custom(mon, players, now)
	if err != nil {
		return nil, nil, nil, err
	}
	combined := append(prefix, actions...)
	if mon.PendingDeath {
		return combined, hits, chars, nil
	}
	inheritedActions, inheritedHits, inheritedChars, err := w.finishMonsterTickWithoutSearchLocked(mon, players, now, combined, w.tickNormalMonsterWithoutSearchLocked)
	if err != nil {
		return nil, nil, nil, err
	}
	return inheritedActions, append(hits, inheritedHits...), append(chars, inheritedChars...), nil
}

func (w *World) tickLightingZombieAndInheritedLocked(mon *Monster, players map[string]storage.Character, now time.Time, prefix []MonsterAction) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	actions, hits, chars, err := w.tickLightingZombieLocked(mon, players, now)
	if err != nil {
		return nil, nil, nil, err
	}
	combined := append(prefix, actions...)
	if mon.TargetCharacterID != "" {
		if target, ok := players[mon.TargetCharacterID]; ok && mon.TargetX >= 0 && mon.TargetY >= 0 && (mon.TargetX != target.X || mon.TargetY != target.Y) && (mon.X != mon.TargetX || mon.Y != mon.TargetY) {
			if w.moveMonsterTowardPointLocked(mon, mon.TargetX, mon.TargetY, players) {
				mon.LastWalkAt = now
				return append(combined, w.monsterActionLocked(mon, MonsterActionWalk)), hits, chars, nil
			}
		}
	}
	if mon.TargetMonsterID != "" {
		if target := w.monsters[mon.TargetMonsterID]; target != nil && mon.TargetX >= 0 && mon.TargetY >= 0 && (mon.TargetX != target.X || mon.TargetY != target.Y) && (mon.X != mon.TargetX || mon.Y != mon.TargetY) {
			if w.moveMonsterTowardPointLocked(mon, mon.TargetX, mon.TargetY, players) {
				mon.LastWalkAt = now
				return append(combined, w.monsterActionLocked(mon, MonsterActionWalk)), hits, chars, nil
			}
		}
	}
	if mon.PendingDeath {
		return combined, hits, chars, nil
	}
	inheritedActions, inheritedHits, inheritedChars, err := w.finishMonsterTickWithoutSearchLocked(mon, players, now, combined, w.tickNormalMonsterWithoutSearchLocked)
	if err != nil {
		return nil, nil, nil, err
	}
	return inheritedActions, append(hits, inheritedHits...), append(chars, inheritedChars...), nil
}

func (w *World) tickElectronicScorpionAndInheritedLocked(mon *Monster, players map[string]storage.Character, now time.Time, prefix []MonsterAction) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	actions, hits, chars, err := w.tickElectronicScorpionLocked(mon, players, now)
	if err != nil {
		return nil, nil, nil, err
	}
	combined := append(prefix, actions...)
	if mon.TargetCharacterID == "" && mon.TargetMonsterID == "" {
		return combined, hits, chars, nil
	}
	if mon.PendingDeath {
		return combined, hits, chars, nil
	}
	inheritedActions, inheritedHits, inheritedChars, err := w.finishMonsterTickWithoutSearchLocked(mon, players, now, combined, w.tickNormalMonsterWithoutSearchLocked)
	if err != nil {
		return nil, nil, nil, err
	}
	return inheritedActions, append(hits, inheritedHits...), append(chars, inheritedChars...), nil
}

func (w *World) tickBigHeartLockedEntry(mon *Monster, players map[string]storage.Character, now time.Time, prefix []MonsterAction) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	actions, hits, chars, err := w.tickBigHeartLocked(mon, players, now)
	if err != nil {
		return nil, nil, nil, err
	}
	if len(prefix) == 0 {
		return actions, hits, chars, nil
	}
	return append(prefix, actions...), hits, chars, nil
}

func (w *World) tickBigHeartAndInheritedLocked(mon *Monster, players map[string]storage.Character, now time.Time, prefix []MonsterAction) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	actions, hits, chars, err := w.tickBigHeartLocked(mon, players, now)
	if err != nil {
		return nil, nil, nil, err
	}
	return append(append([]MonsterAction{}, prefix...), actions...), hits, chars, nil
}

func (w *World) tickStickAndInheritedLocked(mon *Monster, players map[string]storage.Character, now time.Time, prefix []MonsterAction) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	actions := append([]MonsterAction{}, prefix...)
	var hits []CharacterHit
	var chars []storage.Character
	if w.referenceSpecialWalkReadyLocked(mon, now) {
		customActions, customHits, customChars, err := w.tickStickMonsterLocked(mon, players, now)
		if err != nil {
			return nil, nil, nil, err
		}
		actions = append(actions, customActions...)
		hits = append(hits, customHits...)
		chars = append(chars, customChars...)
	}
	return actions, hits, chars, nil
}

func (w *World) tickCentipedeAndInheritedLocked(mon *Monster, players map[string]storage.Character, now time.Time, prefix []MonsterAction) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	actions := append([]MonsterAction{}, prefix...)
	var hits []CharacterHit
	var chars []storage.Character
	if w.referenceSpecialWalkReadyLocked(mon, now) {
		customActions, customHits, customChars, err := w.tickCentipedeMonsterLocked(mon, players, now)
		if err != nil {
			return nil, nil, nil, err
		}
		actions = append(actions, customActions...)
		hits = append(hits, customHits...)
		chars = append(chars, customChars...)
	}
	return actions, hits, chars, nil
}

func (w *World) tickDigOutAndInheritedLocked(mon *Monster, players map[string]storage.Character, now time.Time, prefix []MonsterAction) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	actions, hits, chars, err := w.tickDigOutZombieLocked(mon, players, now)
	if err != nil {
		return nil, nil, nil, err
	}
	combined := append(prefix, actions...)
	if mon.FixedHideMode {
		return combined, hits, chars, nil
	}
	inheritedActions, inheritedHits, inheritedChars, err := w.finishMonsterTickLocked(mon, players, now, combined, w.tickNormalMonsterLocked)
	if err != nil {
		return nil, nil, nil, err
	}
	return inheritedActions, append(hits, inheritedHits...), append(chars, inheritedChars...), nil
}

func (w *World) tickLightingZombieLocked(mon *Monster, players map[string]storage.Character, now time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	if mon.LastTargetSearchAt.IsZero() {
		mon.LastTargetSearchAt = now
	}
	if now.Sub(mon.LastTargetSearchAt) <= 8*time.Second {
		return nil, nil, nil, nil
	}
	if mon.TargetCharacterID == "" && mon.TargetMonsterID == "" {
		mon.LastTargetSearchAt = now
		w.searchMonsterTargetLocked(mon, players, now)
	}
	if mon.TargetCharacterID == "" && mon.TargetMonsterID == "" {
		return nil, nil, nil, nil
	}
	var target storage.Character
	var targetMonster *Monster
	if mon.TargetMonsterID != "" {
		targetMonster = w.monsters[mon.TargetMonsterID]
		if targetMonster == nil || targetMonster.ID == "" || targetMonster.ID == mon.ID {
			return nil, nil, nil, nil
		}
		target = storage.Character{ID: targetMonster.ID, MapID: targetMonster.MapID, X: targetMonster.X, Y: targetMonster.Y, HP: targetMonster.HP}
	} else {
		var ok bool
		target, ok = players[mon.TargetCharacterID]
		if !ok || target.ID == "" {
			return nil, nil, nil, nil
		}
	}
	if abs(mon.X-target.X) > 5 || abs(mon.Y-target.Y) > 5 {
		return nil, nil, nil, nil
	}
	walkReady := mon.LastWalkAt.IsZero() || now.Sub(mon.LastWalkAt) > time.Duration(w.monsterWalkSpeedMSLocked(mon))*time.Millisecond
	if walkReady {
		if abs(mon.X-target.X) <= 2 && abs(mon.Y-target.Y) <= 2 && w.rand.Intn(3) != 0 {
			return nil, nil, nil, nil
		}
		if abs(mon.X-target.X) <= 4 && abs(mon.Y-target.Y) <= 4 && mon.Dir >= 0 && mon.Dir < len(dirOffsets) {
			mon.TargetX = mon.X - dirOffsets[mon.Dir][0]
			mon.TargetY = mon.Y - dirOffsets[mon.Dir][1]
		}
	}
	if now.Sub(mon.LastAttackAt) <= time.Duration(w.monsterAttackIntervalMSLocked(mon))*time.Millisecond {
		return nil, nil, nil, nil
	}
	mon.LastAttackAt = now
	mon.TargetFocusAt = now
	mon.Dir = direction(mon.X, mon.Y, target.X, target.Y)
	action := w.monsterActionLocked(mon, MonsterActionLighting)
	if targetMonster != nil {
		action.TargetActor = MonsterActorID(*targetMonster)
	} else {
		action.TargetActor = CharacterActorID(target)
	}
	action.TargetX = target.X
	action.TargetY = target.Y
	power := mon.MinAttack
	if mon.MaxAttack > mon.MinAttack {
		power += w.rand.Intn(mon.MaxAttack - mon.MinAttack + 1)
	}
	mapData, ok := w.data.Maps[mon.MapID]
	if !ok {
		return []MonsterAction{action}, nil, nil, nil
	}
	if _, _, valid := spellLineNextPosition(mapData, mon.X, mon.Y, mon.Dir, 1); !valid {
		return []MonsterAction{action}, nil, nil, nil
	}
	mon.HolySeizeUntil = time.Time{}
	targetIDs := make([]string, 0)
	monsterTargets := make([]pendingMonsterTarget, 0)
	orderedTargets := make([]pendingMonsterTarget, 0)
	seen := map[string]struct{}{}
	playerList := make([]storage.Character, 0, len(players))
	for _, candidate := range players {
		playerList = append(playerList, candidate)
	}
	endX, endY, endValid := spellLineNextPosition(mapData, mon.X, mon.Y, mon.Dir, 9)
	if !endValid {
		endX, endY = mon.X, mon.Y
	}
	x, y := mon.X, mon.Y
	for step := 1; step <= 13; step++ {
		lineDir := mon.Dir
		if step > 1 {
			lineDir = direction(x, y, endX, endY)
		}
		nextX, nextY, valid := spellLineNextPosition(mapData, x, y, lineDir, 1)
		if !valid {
			break
		}
		x, y = nextX, nextY
		areaTarget := w.movingObjectAtPointForMonsterLocked(playerList, mon.MapID, x, y)
		if areaTarget.Character != nil {
			candidate := *areaTarget.Character
			if w.monsterCanAttackCharacterTargetLocked(mon, candidate) {
				if _, exists := seen[candidate.ID]; !exists && w.characterMagicHitAllowedLocked(candidate) {
					seen[candidate.ID] = struct{}{}
					power = referenceRound(float64(power) * 1.5)
					targetIDs = append(targetIDs, candidate.ID)
					orderedTargets = append(orderedTargets, pendingMonsterTarget{ID: candidate.ID, Damage: power, Order: candidate.ObjectOrder, TargetRange: -1, DirectMagicStrike: true})
				}
			}
		} else if areaTarget.Monster != nil {
			candidate := areaTarget.Monster
			if w.monsterCanLockedMonsterTargetAttackLocked(mon, candidate) && w.monsterMagicHitAllowedLocked(candidate) {
				if _, exists := seen[candidate.ID]; !exists {
					seen[candidate.ID] = struct{}{}
					power = referenceRound(float64(power) * 1.5)
					monsterTargets = append(monsterTargets, pendingMonsterTarget{ID: candidate.ID, Damage: power, TargetRange: -1, DirectMagicStrike: true})
					orderedTargets = append(orderedTargets, pendingMonsterTarget{ID: candidate.ID, Damage: power, Monster: true, Order: candidate.ObjectOrder, TargetRange: -1, DirectMagicStrike: true})
				}
			}
		}
		if x == endX && y == endY {
			break
		}
	}
	if len(targetIDs) > 0 || len(monsterTargets) > 0 {
		w.enqueuePendingMonsterAttackLocked(pendingMonsterAttack{DueAt: now.Add(600 * time.Millisecond), MonsterID: mon.ID, Targets: orderedTargets, TargetIDs: targetIDs, MonsterTargets: monsterTargets, Damage: power, ImpactDelay: 600 * time.Millisecond})
	}
	return []MonsterAction{action}, nil, nil, nil
}

func (w *World) monsterInitialSearchDelayMSLocked(mon *Monster) int {
	if mon.Race == 87 || mon.Race == 93 || mon.Race == 104 {
		return 3000
	}
	base, span := 3000, 2000
	switch mon.Race {
	case 110, 111, 112, 115, 120:
		base, span = 2000, 2000
	case 92:
		base, span = 500, 1500
	case 53, 81, 82, 83, 84, 86, 88, 89, 90, 91, 94, 97, 100, 101, 102, 105, 106, 118, 119, 130, 131, 133, 134, 200, 201, 206, 208, 209, 210, 214, 215:
		base, span = 1500, 1500
	case 85, 95, 96, 103, 107, 116, 117, 132:
		base, span = 2500, 1500
	}
	return base + w.monsterTraceIntn("search.initial_delay", span)
}

func (w *World) tickDigOutZombieLocked(mon *Monster, players map[string]storage.Character, now time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	if !w.referenceSpecialWalkReadyLocked(mon, now) {
		return nil, nil, nil, nil
	}
	if !mon.FixedHideMode {
		if mon.LastTargetSearchAt.IsZero() {
			mon.LastTargetSearchAt = now
			return nil, nil, nil, nil
		}
		searchInterval := 8 * time.Second
		if mon.TargetCharacterID == "" && mon.TargetMonsterID == "" {
			searchInterval = time.Second
		}
		if now.Sub(mon.LastTargetSearchAt) > searchInterval {
			mon.LastTargetSearchAt = now
			w.searchMonsterTargetLocked(mon, players, now)
			if mon.TargetCharacterID == "" && mon.TargetMonsterID == "" {
				mon.NextSearchAt = now.Add(time.Second)
			}
		}
		return nil, nil, nil, nil
	}
	for _, key := range w.visibleActorKeysLocked(mon.ID, mon.MapID, mon.X, mon.Y, 3, players) {
		if strings.HasPrefix(key, "character:") {
			target, ok := players[strings.TrimPrefix(key, "character:")]
			if !ok || !w.monsterCanTargetCharacterLocked(mon, target) || abs(mon.X-target.X) > 3 || abs(mon.Y-target.Y) > 3 {
				continue
			}
		} else if strings.HasPrefix(key, "monster:") {
			target := w.monsters[strings.TrimPrefix(key, "monster:")]
			if !monsterTargetAliveLocked(target) || target.ID == mon.ID || target.MasterID == "" || target.FixedHideMode || target.AdminMode || target.StoneMode || target.MapID != mon.MapID {
				continue
			}
			if target.Hidden && !monsterCanSeeTransparent(mon) {
				continue
			}
			if abs(mon.X-target.X) > 3 || abs(mon.Y-target.Y) > 3 {
				continue
			}
		} else {
			continue
		}
		eventID := w.nextGroundEventIDLocked()
		w.addGroundEventLocked(SpellGroundEvent{ID: eventID, MapID: mon.MapID, X: mon.X, Y: mon.Y, Type: 1, Duration: 5 * time.Minute, StartAt: now})
		mon.Hidden = false
		mon.FixedHideMode = false
		mon.LastWalkAt = now.Add(time.Second)
		return []MonsterAction{w.monsterActionLocked(mon, MonsterActionReveal)}, nil, nil, nil
	}
	return nil, nil, nil, nil
}

func (w *World) finishMonsterTickLocked(mon *Monster, players map[string]storage.Character, now time.Time, prefix []MonsterAction, tick func(*Monster, map[string]storage.Character, time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error)) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	w.updateReferencePeriodicSearchLocked(mon, players, now)
	return w.finishMonsterTickWithoutSearchLocked(mon, players, now, prefix, tick)
}

func (w *World) finishMonsterTickWithoutSearchLocked(mon *Monster, players map[string]storage.Character, now time.Time, prefix []MonsterAction, tick func(*Monster, map[string]storage.Character, time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error)) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	if w.thinkMonsterLocked(mon, players, now) {
		return append(prefix, w.monsterActionLocked(mon, MonsterActionWalk)), nil, nil, nil
	}
	if !w.monsterWalkReadyLocked(mon, now) {
		return prefix, nil, nil, nil
	}
	actions, hits, chars, err := tick(mon, players, now)
	if err != nil {
		return nil, nil, nil, err
	}
	if len(prefix) == 0 {
		return actions, hits, chars, nil
	}
	combined := make([]MonsterAction, 0, len(prefix)+len(actions))
	combined = append(combined, prefix...)
	combined = append(combined, actions...)
	return combined, hits, chars, nil
}

func (w *World) tickInclusiveCustomAndInheritedLocked(mon *Monster, players map[string]storage.Character, now time.Time, prefix []MonsterAction, custom func(*Monster, map[string]storage.Character, time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error)) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	actions := append([]MonsterAction{}, prefix...)
	var hits []CharacterHit
	var chars []storage.Character
	if mon.LastWalkAt.IsZero() {
		mon.LastWalkAt = now.Add(-time.Duration(w.monsterWalkSpeedMSLocked(mon)) * time.Millisecond)
	}
	if !now.Before(mon.LastWalkAt) && now.Sub(mon.LastWalkAt) >= time.Duration(w.monsterWalkSpeedMSLocked(mon))*time.Millisecond {
		mon.LastWalkAt = now
		customActions, customHits, customChars, err := custom(mon, players, now)
		if err != nil {
			return nil, nil, nil, err
		}
		w.cleanupTrackedMonsterChildrenLocked(mon)
		actions = append(actions, customActions...)
		hits = append(hits, customHits...)
		chars = append(chars, customChars...)
	}
	return actions, hits, chars, nil
}

func (w *World) tickCustomAndInheritedLocked(mon *Monster, players map[string]storage.Character, now time.Time, prefix []MonsterAction, custom func(*Monster, map[string]storage.Character, time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error)) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	actions, hits, chars, err := w.finishMonsterTickLocked(mon, players, now, prefix, custom)
	if err != nil {
		return nil, nil, nil, err
	}
	if len(actions) > 0 {
		switch actions[len(actions)-1].Kind {
		case MonsterActionHit, MonsterActionFlyAxe, MonsterActionLighting:
			return actions, hits, chars, nil
		}
	}
	inheritedActions, inheritedHits, inheritedChars, err := w.finishMonsterTickLocked(mon, players, now, actions, w.tickNormalMonsterLocked)
	if err != nil {
		return nil, nil, nil, err
	}
	return inheritedActions, append(hits, inheritedHits...), append(chars, inheritedChars...), nil
}

func (w *World) tickInclusiveCustomLocked(mon *Monster, players map[string]storage.Character, now time.Time, prefix []MonsterAction, custom func(*Monster, map[string]storage.Character, time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error)) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	if !w.referenceSpecialWalkReadyInclusiveLocked(mon, now) {
		return prefix, nil, nil, nil
	}
	actions, hits, chars, err := custom(mon, players, now)
	if err != nil {
		return nil, nil, nil, err
	}
	w.cleanupTrackedMonsterChildrenLocked(mon)
	if len(prefix) == 0 {
		return actions, hits, chars, nil
	}
	return append(prefix, actions...), hits, chars, nil
}

func (w *World) tickImmediateCustomAndInheritedLocked(mon *Monster, players map[string]storage.Character, now time.Time, prefix []MonsterAction, custom func(*Monster, map[string]storage.Character, time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error)) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	actions, hits, chars, err := custom(mon, players, now)
	if err != nil {
		return nil, nil, nil, err
	}
	if len(actions) > 0 {
		return append(prefix, actions...), hits, chars, nil
	}
	actions = prefix
	if mon.PendingDeath {
		return actions, hits, chars, nil
	}
	inheritedActions, inheritedHits, inheritedChars, err := w.finishMonsterTickLocked(mon, players, now, actions, w.tickNormalMonsterLocked)
	if err != nil {
		return nil, nil, nil, err
	}
	if len(hits) == 0 && len(chars) == 0 {
		return inheritedActions, inheritedHits, inheritedChars, nil
	}
	return inheritedActions, append(hits, inheritedHits...), append(chars, inheritedChars...), nil
}

func (w *World) tickExplosionSpiderAndInheritedLocked(mon *Monster, players map[string]storage.Character, now time.Time, prefix []MonsterAction) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	if mon.ExplosionStartAt.IsZero() {
		mon.ExplosionStartAt = now
	}
	actions := append([]MonsterAction{}, prefix...)
	var hits []CharacterHit
	var chars []storage.Character
	if now.Sub(mon.ExplosionStartAt) > 60*time.Second {
		mon.ExplosionStartAt = now
		detonationActions, detonationHits, detonationChars, err := w.explosionSpiderLocked(mon, players, now)
		if err != nil {
			return nil, nil, nil, err
		}
		actions = append(actions, detonationActions...)
		hits = append(hits, detonationHits...)
		chars = append(chars, detonationChars...)
		if mon.PendingDeath {
			return actions, hits, chars, nil
		}
	}
	inheritedActions, inheritedHits, inheritedChars, err := w.finishMonsterTickLocked(mon, players, now, actions, w.tickExplosionSpiderAttackLocked)
	if err != nil {
		return nil, nil, nil, err
	}
	return inheritedActions, append(hits, inheritedHits...), append(chars, inheritedChars...), nil
}

func (w *World) tickExplosionSpiderAttackLocked(mon *Monster, players map[string]storage.Character, now time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	actions, hits, chars, err := w.tickExplosionSpiderModeLocked(mon, players, now, false)
	if err != nil || len(actions) > 0 || len(hits) > 0 || len(chars) > 0 || mon.TargetCharacterID != "" || mon.TargetMonsterID != "" || !mon.TargetFocusAt.IsZero() {
		return actions, hits, chars, err
	}
	wanderActions, wanderHits, wanderChars, err := w.tickExplosionSpiderInheritedLocked(mon, players, now)
	return append(actions, wanderActions...), append(hits, wanderHits...), append(chars, wanderChars...), err
}

func (w *World) tickExplosionSpiderInheritedLocked(mon *Monster, players map[string]storage.Character, now time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	if mon.TargetCharacterID == "" && mon.TargetMonsterID == "" && mon.TargetFocusAt.IsZero() {
		if action, ok := w.wanderMonsterLocked(mon, players); ok {
			return []MonsterAction{action}, nil, nil, nil
		}
	}
	return nil, nil, nil, nil
}

func (w *World) tickNormalMonsterLocked(mon *Monster, players map[string]storage.Character, now time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	w.updateReferencePeriodicSearchLocked(mon, players, now)
	return w.tickNormalMonsterWithoutSearchLocked(mon, players, now)
}

func (w *World) tickNormalMonsterWithoutSearchLocked(mon *Monster, players map[string]storage.Character, now time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	if mon.TargetCharacterID == "" && mon.TargetMonsterID == "" {
		if mon.TargetFocusAt.IsZero() {
			if action, ok := w.wanderMonsterLocked(mon, players); ok {
				return []MonsterAction{action}, nil, nil, nil
			}
		}
		return nil, nil, nil, nil
	}
	return w.tickMonsterTargetLocked(mon, players, now)
}

func (w *World) updateReferencePeriodicSearchLocked(mon *Monster, players map[string]storage.Character, now time.Time) {
	if !w.monsterUsesReferencePeriodicSearchLocked(mon) {
		return
	}
	if mon.LastTargetSearchAt.IsZero() {
		mon.LastTargetSearchAt = now
		return
	}
	if now.Sub(mon.LastTargetSearchAt) > 8*time.Second || (mon.TargetCharacterID == "" && mon.TargetMonsterID == "" && now.Sub(mon.LastTargetSearchAt) > time.Second) {
		mon.LastTargetSearchAt = now
		w.searchMonsterTargetLocked(mon, players, now)
	}
}

func (w *World) tickMonsterTargetLocked(mon *Monster, players map[string]storage.Character, now time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	if mon.TargetCharacterID == "" && mon.TargetMonsterID != "" {
		return w.tickMonsterMonsterTargetLocked(mon, players, now)
	}
	if mon.TargetCharacterID != "" {
		target, ok := players[mon.TargetCharacterID]
		if ok && target.MapID != mon.MapID && (w.monsterIsDualAxeLocked(mon) || w.monsterIsThornDarkLocked(mon)) {
			if now.Sub(mon.LastAttackAt) <= time.Duration(w.monsterAttackIntervalMSLocked(mon))*time.Millisecond {
				return nil, nil, nil, nil
			}
			mon.TargetCharacterID = ""
			mon.TargetX, mon.TargetY = -1, -1
			mon.TargetFocusAt = time.Time{}
			return nil, nil, nil, nil
		}
		if !ok || target.MapID != mon.MapID {
			mon.TargetCharacterID = ""
			mon.TargetX, mon.TargetY = -1, -1
			mon.TargetFocusAt = time.Time{}
			mon.NextSearchAt = now.Add(time.Duration(w.monsterSearchNoTargetMSLocked(mon)) * time.Millisecond)
			return nil, nil, nil, nil
		}
		dir := walkDirection(mon.X, mon.Y, target.X, target.Y)
		if (mon.X != target.X || mon.Y != target.Y) && abs(mon.X-target.X) <= 1 && abs(mon.Y-target.Y) <= 1 {
			if now.Sub(mon.LastAttackAt) <= time.Duration(w.monsterAttackIntervalMSLocked(mon))*time.Millisecond {
				return nil, nil, nil, nil
			}
			mon.Dir = dir
			mon.LastAttackAt = now
			mon.TargetFocusAt = now
			mon.HolySeizeUntil = time.Time{}
			if mon.MasterID != "" {
				if master, ok := players[mon.MasterID]; ok && master.SlaveRelax {
					return []MonsterAction{w.monsterActionLocked(mon, MonsterActionHit)}, nil, nil, nil
				}
			}
			if !w.monsterCanAttackCharacterTargetLocked(mon, target) {
				return []MonsterAction{w.monsterActionLocked(mon, MonsterActionHit)}, nil, nil, nil
			}
			updated, hit, err := w.monsterAttackCharacterWithHitCheckAtLocked(mon, target, now)
			if err != nil {
				return nil, nil, nil, err
			}
			if hit.Damage <= 0 {
				return []MonsterAction{w.monsterActionLocked(mon, MonsterActionHit)}, nil, nil, nil
			}
			return []MonsterAction{w.monsterActionLocked(mon, MonsterActionHit)}, []CharacterHit{hit}, []storage.Character{updated}, nil
		}
		mon.TargetX, mon.TargetY = target.X, target.Y
		if mon.X == target.X && mon.Y == target.Y {
			return nil, nil, nil, nil
		}
		if !w.moveMonsterTowardLocked(mon, target, dir, players) {
			return nil, nil, nil, nil
		}
		mon.LastWalkAt = now
		return []MonsterAction{w.monsterActionLocked(mon, MonsterActionWalk)}, nil, nil, nil
	}
	return nil, nil, nil, nil
}

func (w *World) tickMonsterMonsterTargetLocked(mon *Monster, players map[string]storage.Character, now time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	target := w.monsters[mon.TargetMonsterID]
	if !monsterTargetAliveLocked(target) || target.ID == "" || target.ID == mon.ID || target.MapID != mon.MapID {
		mon.TargetMonsterID = ""
		mon.TargetX, mon.TargetY = -1, -1
		mon.TargetFocusAt = time.Time{}
		mon.NextSearchAt = now.Add(time.Duration(w.monsterSearchNoTargetMSLocked(mon)) * time.Millisecond)
		return nil, nil, nil, nil
	}
	dir := walkDirection(mon.X, mon.Y, target.X, target.Y)
	if (mon.X != target.X || mon.Y != target.Y) && abs(mon.X-target.X) <= 1 && abs(mon.Y-target.Y) <= 1 {
		if now.Sub(mon.LastAttackAt) <= time.Duration(w.monsterAttackIntervalMSLocked(mon))*time.Millisecond {
			return nil, nil, nil, nil
		}
		mon.Dir = dir
		mon.LastAttackAt = now
		mon.TargetFocusAt = now
		mon.HolySeizeUntil = time.Time{}
		if mon.MasterID != "" {
			if master, ok := players[mon.MasterID]; ok && master.SlaveRelax {
				return []MonsterAction{w.monsterActionLocked(mon, MonsterActionHit)}, nil, nil, nil
			}
		}
		power := mon.MinAttack
		if mon.MaxAttack > mon.MinAttack {
			power += w.rand.Intn(mon.MaxAttack - mon.MinAttack + 1)
		}
		if !w.monsterCanLockedMonsterTargetAttackLocked(mon, target) {
			return []MonsterAction{w.monsterActionLocked(mon, MonsterActionHit)}, nil, nil, nil
		}
		if power <= 0 {
			return []MonsterAction{w.monsterActionLocked(mon, MonsterActionHit)}, nil, nil, nil
		}
		if w.monsterHitPointLocked(mon) > 0 && w.monsterHitPointLocked(mon) < w.rand.Intn(w.monsterSpeedPointLocked(target)) {
			return []MonsterAction{w.monsterActionLocked(mon, MonsterActionHit)}, nil, nil, nil
		}
		damage := w.monsterPhysicalDamageAfterDefenseAtLocked(target, power, now)
		hit, err := w.applyReferenceMonsterVsMonsterDamageAtLocked(mon, target, damage, now)
		if err != nil {
			return nil, nil, nil, err
		}
		hit.Magic = false
		hit.ImpactDelay = 200 * time.Millisecond
		mon.TargetCharacterID = ""
		mon.TargetMonsterID = target.ID
		if hit.Damage > 0 {
			w.monsterBehaviorHits = append(w.monsterBehaviorHits, hit)
			w.enqueueReferenceMonsterStruckLocked(mon, target, now, 200*time.Millisecond)
		}
		return []MonsterAction{w.monsterActionLocked(mon, MonsterActionHit)}, nil, nil, nil
	}
	mon.TargetX, mon.TargetY = target.X, target.Y
	if mon.X == target.X && mon.Y == target.Y {
		return nil, nil, nil, nil
	}
	if !w.moveMonsterTowardPointLocked(mon, target.X, target.Y, players) {
		return nil, nil, nil, nil
	}
	mon.LastWalkAt = now
	return []MonsterAction{w.monsterActionLocked(mon, MonsterActionWalk)}, nil, nil, nil
}

func (w *World) monsterCanAttackCharacterTargetLocked(mon *Monster, ch storage.Character) bool {
	return mon != nil && ch.ID != "" && ch.MapID == mon.MapID && !w.characterDeadLocked(ch) && !ch.AdminMode && !ch.StoneMode && (mon.MasterID == "" || !w.isSafeZoneLocked(ch))
}

func (w *World) monsterCanAttackMonsterTargetLocked(mon, target *Monster) bool {
	return mon != nil && monsterTargetAliveLocked(target) && target.ID != mon.ID && target.MasterID != "" && !target.FixedHideMode && !target.AdminMode && !target.StoneMode && target.MapID == mon.MapID
}

func (w *World) monsterCanLockedMonsterTargetAttackLocked(mon, target *Monster) bool {
	return mon != nil && monsterTargetAliveLocked(target) && target.ID != mon.ID && target.MasterID != "" && !target.AdminMode && !target.StoneMode && target.MapID == mon.MapID
}

func (w *World) tickBeeQueenLocked(mon *Monster, players map[string]storage.Character, now time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	attackInterval := time.Duration(w.monsterAttackIntervalMSLocked(mon)) * time.Millisecond
	if mon.LastAttackAt.IsZero() || now.Before(mon.LastAttackAt) {
		mon.LastAttackAt = now.Add(-attackInterval)
	}
	if now.Sub(mon.LastAttackAt) < attackInterval {
		return nil, nil, nil, nil
	}
	mon.LastAttackAt = now
	w.searchMonsterTargetLocked(mon, players, now)
	if mon.TargetCharacterID == "" && mon.TargetMonsterID == "" {
		return nil, nil, nil, nil
	}
	if w.monsterChildLimitCountLocked(mon) >= 15 {
		return nil, nil, nil, nil
	}
	mon.LastAttackAt = now
	mon.TargetFocusAt = now
	childName := "蜜蜂"
	w.pendingMonsterSpawns = append(w.pendingMonsterSpawns, pendingMonsterSpawn{EnqueueOrder: w.nextPendingOrderLocked(), DueAt: now.Add(500 * time.Millisecond), ParentID: mon.ID, ChildName: childName})
	action := w.monsterActionLocked(mon, MonsterActionHit)
	return []MonsterAction{action}, nil, nil, nil
}

func (w *World) tickCentipedeMonsterLocked(mon *Monster, players map[string]storage.Character, now time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	if mon.FixedHideMode {
		if !mon.CentipedeHideAt.IsZero() && now.Sub(mon.CentipedeHideAt) <= 10*time.Second {
			return nil, nil, nil, nil
		}
		if w.centipedeHasTargetLocked(mon, players, w.monsterCentipedeComeOutRangeLocked(mon), true) {
			mon.Hidden = false
			mon.FixedHideMode = false
			mon.HP = mon.MaxHP
			mon.TargetFocusAt = now
			mon.CentipedeHideAt = now
			mon.CentipedeAttackAt = now.Add(3 * time.Second)
			return []MonsterAction{w.monsterActionLocked(mon, MonsterActionReveal)}, nil, nil, nil
		}
		return nil, nil, nil, nil
	}
	if mon.TargetCharacterID == "" {
		if now.Before(mon.CentipedeAttackAt) {
			return nil, nil, nil, nil
		}
		if !w.centipedeHasTargetLocked(mon, players, w.monsterCentipedeAttackRangeLocked(mon), false) {
			if now.Sub(mon.CentipedeHideAt) <= 10*time.Second {
				return nil, nil, nil, nil
			}
			mon.Hidden = true
			mon.FixedHideMode = true
			mon.TargetX = -1
			mon.TargetY = -1
			mon.TargetFocusAt = now
			mon.CentipedeAttackAt = time.Time{}
			return []MonsterAction{w.monsterActionLocked(mon, MonsterActionHide)}, nil, nil, nil
		}
	}
	if !mon.CentipedeAttackAt.IsZero() && now.Before(mon.CentipedeAttackAt) {
		return nil, nil, nil, nil
	}
	if !w.centipedeHasTargetLocked(mon, players, w.monsterCentipedeAttackRangeLocked(mon), false) {
		mon.TargetCharacterID = ""
		if now.Sub(mon.CentipedeHideAt) > 10*time.Second {
			mon.Hidden = true
			mon.FixedHideMode = true
			mon.TargetX = -1
			mon.TargetY = -1
			mon.CentipedeHideAt = now
			mon.CentipedeAttackAt = time.Time{}
			return []MonsterAction{w.monsterActionLocked(mon, MonsterActionHide)}, nil, nil, nil
		}
		return nil, nil, nil, nil
	}
	attackInterval := time.Duration(w.monsterAttackIntervalMSLocked(mon)) * time.Millisecond
	if attackInterval < 3*time.Second {
		attackInterval = 3 * time.Second
	}
	if now.Sub(mon.LastAttackAt) <= attackInterval {
		return nil, nil, nil, nil
	}
	mon.LastAttackAt = now
	mon.CentipedeAttackAt = now.Add(3 * time.Second)
	power := mon.MinAttack
	if mon.MaxAttack > mon.MinAttack {
		power += w.rand.Intn(mon.MaxAttack - mon.MinAttack + 1)
	}
	targetIDs := make([]string, 0)
	monsterTargets := make([]pendingMonsterTarget, 0)
	orderedTargets := make([]pendingMonsterTarget, 0)
	updatedCharacters := make([]storage.Character, 0)
	for _, key := range w.visibleActorKeysLocked(mon.ID, mon.MapID, mon.X, mon.Y, w.monsterViewRangeLocked(mon), players) {
		if strings.HasPrefix(key, "character:") {
			candidate, ok := players[strings.TrimPrefix(key, "character:")]
			if !ok || w.characterDeadLocked(candidate) || !w.monsterCanAttackCharacterTargetLocked(mon, candidate) || abs(mon.X-candidate.X) >= w.monsterViewRangeLocked(mon) || abs(mon.Y-candidate.Y) >= w.monsterViewRangeLocked(mon) {
				continue
			}
			mon.TargetFocusAt = now
			targetIDs = append(targetIDs, candidate.ID)
			orderedTargets = append(orderedTargets, pendingMonsterTarget{ID: candidate.ID, Damage: power, TargetX: candidate.X, TargetY: candidate.Y, TargetRange: 2, ReferenceDelay: true})
			if w.rand.Intn(4) == 0 {
				target := candidate
				if w.rand.Intn(3) != 0 {
					if setCharacterHealthPoisonLocked(&target, 3, 3, now.Add(60*time.Second), now) {
						players[target.ID] = target
						updatedCharacters = append(updatedCharacters, target)
						w.monsterBehaviorCharacterStatuses = append(w.monsterBehaviorCharacterStatuses, target)
					}
					mon.TargetCharacterID = target.ID
					mon.TargetMonsterID = ""
					w.monsterBehaviorPoisonNotifications = append(w.monsterBehaviorPoisonNotifications, PoisonNotification{Character: target, Seconds: 60, Points: 3})
				} else {
					if !w.characterCannotParalyzeLocked(target) {
						until := now.Add(5 * time.Second).UnixNano()
						if until > target.ParalyzedUntil {
							target.ParalyzedUntil = until
						}
						players[target.ID] = target
						updatedCharacters = append(updatedCharacters, target)
						w.monsterBehaviorCharacterStatuses = append(w.monsterBehaviorCharacterStatuses, target)
						w.monsterBehaviorPoisonNotifications = append(w.monsterBehaviorPoisonNotifications, PoisonNotification{Character: target, Seconds: 5})
					}
					mon.TargetCharacterID = target.ID
					mon.TargetMonsterID = ""
				}
			}
		}
		if !strings.HasPrefix(key, "monster:") {
			continue
		}
		candidate := w.monsters[strings.TrimPrefix(key, "monster:")]
		if !monsterTargetAliveLocked(candidate) || !w.monsterCanAttackMonsterTargetLocked(mon, candidate) || abs(mon.X-candidate.X) >= w.monsterViewRangeLocked(mon) || abs(mon.Y-candidate.Y) >= w.monsterViewRangeLocked(mon) {
			continue
		}
		mon.TargetFocusAt = now
		monsterTarget := pendingMonsterTarget{ID: candidate.ID, Damage: power, Monster: true, TargetX: candidate.X, TargetY: candidate.Y, TargetRange: 2, ReferenceDelay: true}
		monsterTargets = append(monsterTargets, monsterTarget)
		orderedTargets = append(orderedTargets, monsterTarget)
		if w.rand.Intn(4) == 0 {
			if w.rand.Intn(3) != 0 {
				if setMonsterHealthPoisonLocked(candidate, 3, 3, now.Add(60*time.Second), mon.ID, now) {
					w.monsterBehaviorStatuses = append(w.monsterBehaviorStatuses, *candidate)
				}
				mon.TargetCharacterID = ""
				mon.TargetMonsterID = candidate.ID
			} else {
				until := now.Add(5 * time.Second)
				if until.After(candidate.ParalyzedUntil) {
					candidate.ParalyzedUntil = until
				}
				w.resetMonsterStatusClockLocked(candidate.ID, statusClockParalyzed, candidate.ParalyzedUntil, now)
				w.monsterBehaviorStatuses = append(w.monsterBehaviorStatuses, *candidate)
				mon.TargetCharacterID = ""
				mon.TargetMonsterID = candidate.ID
			}
		}
	}
	if len(targetIDs) > 0 && len(monsterTargets) > 0 {
		w.enqueuePendingMonsterAttackLocked(pendingMonsterAttack{DueAt: now.Add(600 * time.Millisecond), MonsterID: mon.ID, Targets: orderedTargets, Damage: power, ImpactDelay: 600 * time.Millisecond})
	} else if len(targetIDs) > 0 {
		w.enqueuePendingMonsterAttackLocked(pendingMonsterAttack{DueAt: now.Add(600 * time.Millisecond), MonsterID: mon.ID, Targets: orderedTargets, Damage: power, ImpactDelay: 600 * time.Millisecond})
	} else if len(monsterTargets) > 0 {
		w.enqueuePendingMonsterAttackLocked(pendingMonsterAttack{DueAt: now.Add(600 * time.Millisecond), MonsterID: mon.ID, MonsterTargets: monsterTargets, Damage: power, ImpactDelay: 600 * time.Millisecond})
	}
	return []MonsterAction{w.monsterActionLocked(mon, MonsterActionHit)}, nil, updatedCharacters, nil
}

func (w *World) tickDualAxeMonsterLocked(mon *Monster, players map[string]storage.Character, now time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	if mon.LastTargetSearchAt.IsZero() {
		mon.LastTargetSearchAt = now
	}
	if now.Sub(mon.LastTargetSearchAt) >= 5*time.Second {
		mon.LastTargetSearchAt = now
		w.searchDualAxeTargetLocked(mon, players, now)
	}
	if mon.TargetCharacterID == "" && mon.TargetMonsterID == "" {
		return nil, nil, nil, nil
	}
	if mon.TargetMonsterID != "" {
		target := w.monsters[mon.TargetMonsterID]
		if target == nil || target.ID == "" || target.MapID != mon.MapID {
			mon.TargetMonsterID = ""
			mon.TargetFocusAt = time.Time{}
			return nil, nil, nil, nil
		}
		if abs(mon.X-target.X) <= 4 {
			if abs(mon.X-target.X) > 2 || w.rand.Intn(5) == 0 {
				if mon.Dir >= 0 && mon.Dir < len(dirOffsets) {
					mon.TargetX = mon.X - dirOffsets[mon.Dir][0]
					mon.TargetY = mon.Y - dirOffsets[mon.Dir][1]
				}
			}
		}
		if abs(mon.X-target.X) <= 7 {
			if now.Sub(mon.LastAttackAt) > time.Duration(w.monsterAttackIntervalMSLocked(mon))*time.Millisecond {
				mon.LastAttackAt = now
				mon.TargetFocusAt = now
				if mon.AttackMax == 0 {
					mon.AttackMax = 2
					if mon.Race == 93 {
						mon.AttackMax = 3
					}
					if mon.Race == 104 {
						mon.AttackMax = 6
					}
				}
				if mon.AttackCount < mon.AttackMax-1 {
					mon.AttackCount++
					if !w.monsterCanFlyToPointLocked(mon, target.X, target.Y) {
						return nil, nil, nil, nil
					}
					mon.Dir = direction(mon.X, mon.Y, target.X, target.Y)
					power := mon.MinAttack
					if mon.MaxAttack > mon.MinAttack {
						power += w.rand.Intn(mon.MaxAttack - mon.MinAttack + 1)
					}
					action := w.monsterActionLocked(mon, MonsterActionFlyAxe)
					action.TargetActor = MonsterActorID(*target)
					action.TargetX = target.X
					action.TargetY = target.Y
					_, err := w.dualAxeAttackMonsterLocked(mon, target, power)
					if err != nil {
						return nil, nil, nil, err
					}
					return []MonsterAction{action}, nil, nil, nil
				}
				if w.rand.Intn(5) == 0 {
					mon.AttackCount = 0
				}
			}
			return nil, nil, nil, nil
		}
		if now.Sub(mon.LastAttackAt) <= time.Duration(w.monsterAttackIntervalMSLocked(mon))*time.Millisecond {
			return nil, nil, nil, nil
		}
		mon.LastAttackAt = now
		return nil, nil, nil, nil
	}
	target, ok := players[mon.TargetCharacterID]
	if ok && target.MapID != mon.MapID {
		mon.TargetCharacterID = ""
		mon.TargetX, mon.TargetY = -1, -1
		mon.TargetFocusAt = time.Time{}
		return nil, nil, nil, nil
	}
	if !ok {
		mon.TargetCharacterID = ""
		mon.TargetX = -1
		mon.TargetY = -1
		mon.TargetFocusAt = time.Time{}
		mon.NextSearchAt = now.Add(time.Duration(w.monsterSearchNoTargetMSLocked(mon)) * time.Millisecond)
		return nil, nil, nil, nil
	}
	if abs(mon.X-target.X) <= 4 {
		if abs(mon.X-target.X) > 2 || w.rand.Intn(5) == 0 {
			if mon.Dir >= 0 && mon.Dir < len(dirOffsets) {
				mon.TargetX = mon.X - dirOffsets[mon.Dir][0]
				mon.TargetY = mon.Y - dirOffsets[mon.Dir][1]
			}
		}
	}
	if abs(mon.X-target.X) <= 7 {
		if now.Sub(mon.LastAttackAt) > time.Duration(w.monsterAttackIntervalMSLocked(mon))*time.Millisecond {
			mon.LastAttackAt = now
			mon.TargetFocusAt = now
			if mon.AttackMax == 0 {
				mon.AttackMax = 2
				if mon.Race == 93 {
					mon.AttackMax = 3
				}
				if mon.Race == 104 {
					mon.AttackMax = 6
				}
			}
			if mon.AttackCount < mon.AttackMax-1 {
				mon.AttackCount++
				if !w.monsterCanFlyLocked(mon, target) {
					return nil, nil, nil, nil
				}
				mon.Dir = direction(mon.X, mon.Y, target.X, target.Y)
				action := w.monsterActionLocked(mon, MonsterActionFlyAxe)
				action.TargetActor = CharacterActorID(target)
				action.TargetX = target.X
				action.TargetY = target.Y
				power := mon.MinAttack
				if mon.MaxAttack > mon.MinAttack {
					power += w.rand.Intn(mon.MaxAttack - mon.MinAttack + 1)
				}
				hits, chars, err := w.dualAxeAttackCharacterAtLocked(mon, target, power, now)
				if err != nil {
					return nil, nil, nil, err
				}
				return []MonsterAction{action}, hits, chars, nil
			}
			if w.rand.Intn(5) == 0 {
				mon.AttackCount = 0
			}
		}
		return nil, nil, nil, nil
	}
	if now.Sub(mon.LastAttackAt) <= time.Duration(w.monsterAttackIntervalMSLocked(mon))*time.Millisecond {
		return nil, nil, nil, nil
	}
	mon.LastAttackAt = now
	if abs(mon.X-target.X) <= 11 {
		dir := direction(mon.X, mon.Y, target.X, target.Y)
		if w.moveMonsterTowardLocked(mon, target, dir, players) {
			mon.TargetX, mon.TargetY = target.X, target.Y
			mon.LastWalkAt = now
			return []MonsterAction{w.monsterActionLocked(mon, MonsterActionWalk)}, nil, nil, nil
		}
	}
	return nil, nil, nil, nil
}

func (w *World) dualAxeAttackCharacterLocked(mon *Monster, target storage.Character, power int) ([]CharacterHit, []storage.Character, error) {
	return w.dualAxeAttackCharacterAtLocked(mon, target, power, time.Now())
}

func (w *World) dualAxeAttackCharacterAtLocked(mon *Monster, target storage.Character, power int, now time.Time) ([]CharacterHit, []storage.Character, error) {
	if power <= 0 || !w.monsterCanDualAxeCharacterTargetLocked(mon, target) {
		return nil, nil, nil
	}
	oldMagic := mon.UseMagic
	mon.UseMagic = false
	changed, hit, err := w.monsterReferenceCharacterDamageAtLocked(mon, target, power, now)
	mon.UseMagic = oldMagic
	if err != nil {
		return nil, nil, err
	}
	if hit.Damage <= 0 {
		return nil, nil, nil
	}
	hit.ImpactDelay = time.Duration(maxInt(abs(mon.X-target.X), abs(mon.Y-target.Y))*50+600) * time.Millisecond
	return []CharacterHit{hit}, []storage.Character{changed}, nil
}

func (w *World) monsterCanDualAxeCharacterTargetLocked(mon *Monster, ch storage.Character) bool {
	return mon != nil && ch.ID != "" && ch.MapID == mon.MapID
}

func (w *World) dualAxeAttackMonsterLocked(mon, target *Monster, power int) (int, error) {
	if power <= 0 || !w.monsterCanDualAxeMonsterTargetLocked(mon, target) {
		return 0, nil
	}
	targetCharacterID := target.TargetCharacterID
	targetMonsterID := target.TargetMonsterID
	targetFocusAt := target.TargetFocusAt
	nextSearchAt := target.NextSearchAt
	lastAttackAt := target.LastAttackAt
	lastHitterID := target.LastHitterID
	lastHitterAt := target.LastHitterAt
	expHitterID := target.ExpHitterID
	expHitterAt := target.ExpHitterAt
	healthTick := target.HealthTick
	spellTick := target.SpellTick
	perHealth := target.PerHealth
	perSpell := target.PerSpell
	meatQuality := target.MeatQuality
	holySeizeUntil := target.HolySeizeUntil
	pendingDeath := target.PendingDeath
	deathHitterID := target.DeathHitterID
	caster := storage.Character{ID: mon.ID, MapID: mon.MapID, X: mon.X, Y: mon.Y}
	damage := w.monsterPhysicalDamageAfterDefenseLocked(target, power)
	hit, err := w.attackMonsterDirectDamageModeForActorLocked(caster, target, damage, false, false, false)
	if err != nil {
		return 0, err
	}
	target.TargetCharacterID = targetCharacterID
	target.TargetMonsterID = targetMonsterID
	target.TargetFocusAt = targetFocusAt
	target.NextSearchAt = nextSearchAt
	target.LastAttackAt = lastAttackAt
	target.LastHitterID = lastHitterID
	target.LastHitterAt = lastHitterAt
	target.ExpHitterID = expHitterID
	target.ExpHitterAt = expHitterAt
	target.HealthTick = healthTick
	target.SpellTick = spellTick
	target.PerHealth = perHealth
	target.PerSpell = perSpell
	target.MeatQuality = meatQuality
	target.HolySeizeUntil = holySeizeUntil
	target.PendingDeath = pendingDeath
	target.DeathHitterID = deathHitterID
	if hit.Damage > 0 {
		hit.ImpactDelay = time.Duration(maxInt(abs(mon.X-target.X), abs(mon.Y-target.Y))*50+600) * time.Millisecond
		w.monsterBehaviorHits = append(w.monsterBehaviorHits, hit)
	}
	return hit.Damage, nil
}

func (w *World) monsterCanDualAxeMonsterTargetLocked(mon, target *Monster) bool {
	return mon != nil && target != nil && target.ID != "" && target.ID != mon.ID
}

func (w *World) monsterCanFlyLocked(mon *Monster, target storage.Character) bool {
	return w.monsterCanFlyToPointLocked(mon, target.X, target.Y)
}

func (w *World) monsterCanFlyToPointLocked(mon *Monster, targetX, targetY int) bool {
	mapData, ok := w.data.Maps[mon.MapID]
	if !ok {
		return false
	}
	stepX := float64(targetX-mon.X) / 10
	stepY := float64(targetY-targetX) / 10
	x := referenceRound(float64(mon.X) + stepX)
	y := referenceRound(float64(mon.Y) + stepY)
	for i := 0; i < 10; i++ {
		if !mapData.Walkable(x, y) {
			return false
		}
	}
	return true
}

func (w *World) tickSpitSpiderLocked(mon *Monster, players map[string]storage.Character, now time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	if w.monsterUsesReferencePeriodicSearchLocked(mon) {
		if mon.LastTargetSearchAt.IsZero() {
			mon.LastTargetSearchAt = now
		} else if now.Sub(mon.LastTargetSearchAt) > 8*time.Second || (mon.TargetCharacterID == "" && mon.TargetMonsterID == "" && now.Sub(mon.LastTargetSearchAt) > time.Second) {
			mon.LastTargetSearchAt = now
			w.searchMonsterTargetLocked(mon, players, now)
		}
	}
	return w.tickSpitSpiderTargetLocked(mon, players, now)
}

func (w *World) tickSpitSpiderTargetLocked(mon *Monster, players map[string]storage.Character, now time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	periodicSearch := w.monsterUsesReferencePeriodicSearchLocked(mon)
	if mon.TargetCharacterID == "" && mon.TargetMonsterID == "" && ((periodicSearch && now.After(mon.NextSearchAt)) || (!periodicSearch && !now.Before(mon.NextSearchAt))) {
		w.searchMonsterTargetLocked(mon, players, now)
	}
	if mon.TargetCharacterID == "" && mon.TargetMonsterID == "" {
		return nil, nil, nil, nil
	}
	var target storage.Character
	var targetMonster *Monster
	if mon.TargetMonsterID != "" {
		targetMonster = w.monsters[mon.TargetMonsterID]
		if targetMonster == nil || targetMonster.ID == "" {
			mon.TargetMonsterID = ""
			mon.TargetFocusAt = time.Time{}
			return nil, nil, nil, nil
		}
		target = storage.Character{ID: targetMonster.ID, MapID: targetMonster.MapID, X: targetMonster.X, Y: targetMonster.Y, HP: targetMonster.HP}
	} else {
		var ok bool
		target, ok = players[mon.TargetCharacterID]
		if !ok || target.ID == "" {
			mon.TargetCharacterID = ""
			mon.TargetX, mon.TargetY = -1, -1
			mon.TargetFocusAt = time.Time{}
			mon.NextSearchAt = now.Add(time.Duration(w.monsterSearchNoTargetMSLocked(mon)) * time.Millisecond)
			return nil, nil, nil, nil
		}
	}
	if abs(mon.X-target.X) <= 2 && abs(mon.Y-target.Y) <= 2 {
		dir := direction(mon.X, mon.Y, target.X, target.Y)
		inDirection := w.monsterSpitTargetInDirectionLocked(mon, target, dir)
		if targetMonster != nil {
			inDirection = w.monsterSpitMonsterTargetInDirectionLocked(mon, targetMonster, dir)
		}
		if !inDirection {
			if target.MapID == mon.MapID {
				mon.TargetX, mon.TargetY = target.X, target.Y
			} else if targetMonster != nil {
				mon.TargetMonsterID = ""
				mon.TargetFocusAt = time.Time{}
			} else {
				mon.TargetCharacterID = ""
				mon.TargetX, mon.TargetY = -1, -1
				mon.TargetFocusAt = time.Time{}
			}
			return nil, nil, nil, nil
		}
		if now.Sub(mon.LastAttackAt) <= time.Duration(w.monsterAttackIntervalMSLocked(mon))*time.Millisecond {
			return nil, nil, nil, nil
		}
		mon.LastAttackAt = now
		mon.TargetFocusAt = now
		mon.Dir = dir
		mon.HolySeizeUntil = time.Time{}
		power := mon.MinAttack
		if mon.MaxAttack > mon.MinAttack {
			power += w.rand.Intn(mon.MaxAttack - mon.MinAttack + 1)
		}
		if power < 1 {
			return nil, nil, nil, nil
		}
		hits := []CharacterHit{}
		updated := []storage.Character{}
		oldMagic := mon.UseMagic
		mon.UseMagic = true
		playerList := make([]storage.Character, 0, len(players))
		for _, candidate := range players {
			playerList = append(playerList, candidate)
		}
		for _, offset := range referenceSpitMap[mon.Dir] {
			areaTarget := w.movingObjectAtPointForMonsterLocked(playerList, mon.MapID, mon.X+offset[0], mon.Y+offset[1])
			if areaTarget.Character == nil && areaTarget.Monster == nil {
				continue
			}
			if areaTarget.Monster != nil {
				candidate := areaTarget.Monster
				if !w.monsterCanLockedMonsterTargetAttackLocked(mon, candidate) || w.monsterSpeedPointLocked(candidate) <= 0 || w.rand.Intn(w.monsterSpeedPointLocked(candidate)) >= w.monsterHitPointLocked(mon) {
					continue
				}
				if power <= 0 {
					break
				}
				damage := w.monsterMagicDamageAfterDefenseLocked(candidate, power)
				power = damage
				if damage <= 0 {
					continue
				}
				hit, err := w.applyReferenceMonsterVsMonsterDamageAtLocked(mon, candidate, damage, now)
				if err != nil {
					mon.UseMagic = oldMagic
					return nil, nil, nil, err
				}
				power = hit.Damage
				if hit.Damage > 0 {
					hit.Magic = false
					hit.ImpactDelay = 300 * time.Millisecond
					w.monsterBehaviorHits = append(w.monsterBehaviorHits, hit)
					w.enqueueReferenceMonsterStruckLocked(mon, candidate, now, 300*time.Millisecond)
					if (mon.Race == 82 || mon.Race == 119) && w.rand.Intn(maxInt(mon.AntiPoison, 0)+20) == 0 {
						if setMonsterHealthPoisonLocked(candidate, 1, 1, now.Add(30*time.Second), mon.ID, now) {
							w.monsterBehaviorStatuses = append(w.monsterBehaviorStatuses, *candidate)
						}
					}
				}
				continue
			}
			candidate := *areaTarget.Character
			if !w.monsterCanAttackCharacterTargetLocked(mon, candidate) || w.characterSpeedPointLocked(candidate) <= 0 || w.rand.Intn(w.characterSpeedPointLocked(candidate)) >= w.monsterHitPointLocked(mon) {
				continue
			}
			if power <= 0 {
				break
			}
			changed, hit, err := w.monsterReferenceCharacterDamageAtLocked(mon, candidate, power, now)
			if err != nil {
				mon.UseMagic = oldMagic
				return nil, nil, nil, err
			}
			power = hit.Damage
			hit.Magic = false
			hit.ImpactDelay = 300 * time.Millisecond
			if (mon.Race == 82 || mon.Race == 119) && w.rand.Intn(maxInt(mon.AntiPoison, 0)+20) == 0 && hit.Damage > 0 {
				if setCharacterHealthPoisonLocked(&changed, 1, 1, now.Add(30*time.Second), now) {
					w.monsterBehaviorCharacterStatuses = append(w.monsterBehaviorCharacterStatuses, changed)
				}
				w.monsterBehaviorPoisonNotifications = append(w.monsterBehaviorPoisonNotifications, PoisonNotification{Character: changed, Seconds: 30, Points: 1})
			}
			if changed.ID == candidate.ID && hit.Damage > 0 {
				players[changed.ID] = changed
				updated = append(updated, changed)
				hits = append(hits, hit)
			}
		}
		mon.UseMagic = oldMagic
		return []MonsterAction{w.monsterActionLocked(mon, MonsterActionHit)}, hits, updated, nil
	}
	return nil, nil, nil, nil
}

func (w *World) monsterSpitTargetInDirectionLocked(mon *Monster, ch storage.Character, dir int) bool {
	if mon == nil || dir < 0 || dir >= 8 {
		return false
	}
	dx, dy := ch.X-mon.X, ch.Y-mon.Y
	if abs(dx) > 2 || abs(dy) > 2 {
		return false
	}
	if (dx != 0 || dy != 0) && abs(dx) <= 1 && abs(dy) <= 1 {
		return true
	}
	patterns := [8][][2]int{
		{{0, -2}, {0, -1}}, {{2, -2}, {1, -1}}, {{1, 0}, {2, 0}}, {{1, 1}, {2, 2}},
		{{0, 1}, {0, 2}}, {{-1, 1}, {-2, 2}}, {{-1, 0}, {-2, 0}}, {{-1, -1}, {-2, -2}},
	}
	for _, offset := range patterns[dir] {
		if dx == offset[0] && dy == offset[1] {
			return true
		}
	}
	return false
}

func (w *World) monsterSpitMonsterTargetInDirectionLocked(mon, target *Monster, dir int) bool {
	if mon == nil || target == nil || target.ID == mon.ID || dir < 0 || dir >= 8 {
		return false
	}
	dx, dy := target.X-mon.X, target.Y-mon.Y
	if abs(dx) > 2 || abs(dy) > 2 {
		return false
	}
	if (dx != 0 || dy != 0) && abs(dx) <= 1 && abs(dy) <= 1 {
		return true
	}
	patterns := [8][][2]int{
		{{0, -2}, {0, -1}}, {{2, -2}, {1, -1}}, {{1, 0}, {2, 0}}, {{1, 1}, {2, 2}},
		{{0, 1}, {0, 2}}, {{-1, 1}, {-2, 2}}, {{-1, 0}, {-2, 0}}, {{-1, -1}, {-2, -2}},
	}
	for _, offset := range patterns[dir] {
		if dx == offset[0] && dy == offset[1] {
			return true
		}
	}
	return false
}

func (w *World) prepareGasAttackTargetLocked(mon *Monster, players map[string]storage.Character, now time.Time) {
	if mon.Race == 105 && !w.gasMothSearchReadyLocked(mon, now) {
		return
	}
	if mon.LastTargetSearchAt.IsZero() {
		mon.LastTargetSearchAt = now
	}
	searchInterval := 8 * time.Second
	if mon.TargetCharacterID == "" && mon.TargetMonsterID == "" {
		searchInterval = time.Second
	}
	searchReady := now.Sub(mon.LastTargetSearchAt) > searchInterval
	if searchReady {
		mon.LastTargetSearchAt = now
		w.searchMonsterTargetLocked(mon, players, now)
	}
}

func (w *World) gasMothSearchReadyLocked(mon *Monster, now time.Time) bool {
	if mon.Race != 105 {
		return true
	}
	if mon.LastWalkAt.IsZero() || now.Before(mon.LastWalkAt) {
		return true
	}
	return now.Sub(mon.LastWalkAt) >= time.Duration(w.monsterWalkSpeedMSLocked(mon))*time.Millisecond
}

func (w *World) tickGasAttackMonsterLocked(mon *Monster, players map[string]storage.Character, now time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	w.prepareGasAttackTargetLocked(mon, players, now)
	return w.tickGasAttackTargetLocked(mon, players, now)
}

func (w *World) tickGasAttackTargetLocked(mon *Monster, players map[string]storage.Character, now time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	if mon.TargetCharacterID == "" && mon.TargetMonsterID == "" {
		return nil, nil, nil, nil
	}
	var target storage.Character
	var targetMonster *Monster
	if mon.TargetMonsterID != "" {
		targetMonster = w.monsters[mon.TargetMonsterID]
		if targetMonster == nil || targetMonster.ID == "" {
			mon.TargetMonsterID = ""
			mon.TargetFocusAt = time.Time{}
			return nil, nil, nil, nil
		}
		target = storage.Character{ID: targetMonster.ID, MapID: targetMonster.MapID, X: targetMonster.X, Y: targetMonster.Y, HP: targetMonster.HP}
	} else {
		var ok bool
		target, ok = players[mon.TargetCharacterID]
		if !ok || target.ID == "" {
			mon.TargetCharacterID = ""
			mon.TargetX, mon.TargetY = -1, -1
			mon.TargetFocusAt = time.Time{}
			mon.NextSearchAt = now.Add(time.Duration(w.monsterSearchNoTargetMSLocked(mon)) * time.Millisecond)
			return nil, nil, nil, nil
		}
	}
	if !w.monsterWalkReadyLocked(mon, now) {
		return nil, nil, nil, nil
	}
	if mon.MapID != target.MapID {
		mon.TargetCharacterID = ""
		mon.TargetMonsterID = ""
		mon.TargetX, mon.TargetY = -1, -1
		mon.TargetFocusAt = time.Time{}
		return nil, nil, nil, nil
	}
	if mon.X == target.X && mon.Y == target.Y {
		mon.TargetX, mon.TargetY = target.X, target.Y
		return nil, nil, nil, nil
	}
	if abs(mon.X-target.X) > 1 || abs(mon.Y-target.Y) > 1 {
		mon.TargetX, mon.TargetY = target.X, target.Y
		dir := direction(mon.X, mon.Y, target.X, target.Y)
		if !w.moveMonsterTowardLocked(mon, target, dir, players) {
			return nil, nil, nil, nil
		}
		return []MonsterAction{w.monsterActionLocked(mon, MonsterActionWalk)}, nil, nil, nil
	}
	if now.Sub(mon.LastAttackAt) <= time.Duration(w.monsterAttackIntervalMSLocked(mon))*time.Millisecond {
		return nil, nil, nil, nil
	}
	mon.LastAttackAt = now
	mon.TargetFocusAt = now
	mon.Dir = direction(mon.X, mon.Y, target.X, target.Y)
	mon.HolySeizeUntil = time.Time{}
	power := mon.MinAttack
	if mon.MaxAttack > power {
		power += w.rand.Intn(mon.MaxAttack - power + 1)
	}
	if power <= 0 {
		return nil, nil, nil, nil
	}
	mapData, mapOK := w.data.Maps[mon.MapID]
	if !mapOK {
		return []MonsterAction{w.monsterActionLocked(mon, MonsterActionHit)}, nil, nil, nil
	}
	frontX, frontY, frontOK := spellLineNextPosition(mapData, mon.X, mon.Y, mon.Dir, 1)
	front := storage.Character{}
	var frontMonster *Monster
	if frontOK {
		playerList := make([]storage.Character, 0, len(players))
		for _, candidate := range players {
			playerList = append(playerList, candidate)
		}
		frontObject := w.movingObjectAtPointForMonsterLocked(playerList, mon.MapID, frontX, frontY)
		if frontObject.Character != nil {
			front = *frontObject.Character
		} else if frontObject.Monster != nil {
			frontMonster = frontObject.Monster
		}
	}
	if frontMonster != nil {
		if !w.monsterCanLockedMonsterTargetAttackLocked(mon, frontMonster) {
			return []MonsterAction{w.monsterActionLocked(mon, MonsterActionHit)}, nil, nil, nil
		}
		if w.monsterSpeedPointLocked(frontMonster) > 0 && w.rand.Intn(w.monsterSpeedPointLocked(frontMonster)) >= w.monsterHitPointLocked(mon) {
			return []MonsterAction{w.monsterActionLocked(mon, MonsterActionHit)}, nil, nil, nil
		}
		damage := w.monsterMagicDamageAfterDefenseLocked(frontMonster, power)
		if damage > 0 {
			hit, err := w.applyReferenceMonsterVsMonsterDamageAtLocked(mon, frontMonster, damage, now)
			if err != nil {
				return nil, nil, nil, err
			}
			hit.Magic = false
			hit.ImpactDelay = 300 * time.Millisecond
			w.monsterBehaviorHits = append(w.monsterBehaviorHits, hit)
			w.enqueueReferenceMonsterStruckLocked(mon, frontMonster, now, 300*time.Millisecond)
			if w.rand.Intn(maxInt(frontMonster.AntiPoison, 0)+20) == 0 {
				until := now.Add(5 * time.Second)
				if until.After(frontMonster.ParalyzedUntil) {
					frontMonster.ParalyzedUntil = until
					w.resetMonsterStatusClockLocked(frontMonster.ID, statusClockParalyzed, frontMonster.ParalyzedUntil, now)
					w.monsterBehaviorStatuses = append(w.monsterBehaviorStatuses, *frontMonster)
				}
			}
			hiddenTarget := frontMonster.Hidden || frontMonster.FixedHideMode
			if mon.Race == 105 && hiddenTarget && w.rand.Intn(3) == 0 {
				frontMonster.TransparentUntil = now.Add(time.Second)
				w.monsterBehaviorStatuses = append(w.monsterBehaviorStatuses, *frontMonster)
			}
		}
		return []MonsterAction{w.monsterActionLocked(mon, MonsterActionHit)}, nil, nil, nil
	}
	if front.ID == "" || !w.monsterCanAttackCharacterTargetLocked(mon, front) {
		return []MonsterAction{w.monsterActionLocked(mon, MonsterActionHit)}, nil, nil, nil
	}
	target = front
	if w.characterSpeedPointLocked(target) > 0 && w.rand.Intn(w.characterSpeedPointLocked(target)) >= w.monsterHitPointLocked(mon) {
		return []MonsterAction{w.monsterActionLocked(mon, MonsterActionHit)}, nil, nil, nil
	}
	oldMagic := mon.UseMagic
	mon.UseMagic = true
	updated, hit, err := w.monsterReferenceCharacterDamageAtLocked(mon, target, power, now)
	mon.UseMagic = oldMagic
	if err != nil {
		return nil, nil, nil, err
	}
	hit.Magic = false
	hit.ImpactDelay = 300 * time.Millisecond
	if hit.Damage <= 0 {
		return []MonsterAction{w.monsterActionLocked(mon, MonsterActionHit)}, nil, nil, nil
	}
	hiddenTarget := target.TransparentHideMode != nil && *target.TransparentHideMode
	if mon.Race == 105 && hiddenTarget && w.rand.Intn(3) == 0 {
		resetCharacterTransparentLocked(&updated, now.Add(time.Second))
	}
	if hit.Damage > 0 && w.rand.Intn(maxInt(target.AntiPoison, 0)+20) == 0 {
		until := now.Add(5 * time.Second).UnixNano()
		if until > updated.ParalyzedUntil {
			updated.ParalyzedUntil = until
			w.resetCharacterStatusClockLocked(updated.ID, statusClockParalyzed, updated.ParalyzedUntil, now)
			w.monsterBehaviorCharacterStatuses = append(w.monsterBehaviorCharacterStatuses, updated)
		}
		w.monsterBehaviorPoisonNotifications = append(w.monsterBehaviorPoisonNotifications, PoisonNotification{Character: updated, Seconds: 5})
	}
	players[updated.ID] = updated
	return []MonsterAction{w.monsterActionLocked(mon, MonsterActionHit)}, []CharacterHit{hit}, []storage.Character{updated}, nil
}

func (w *World) tickMagicCowMonsterLocked(mon *Monster, players map[string]storage.Character, now time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	if w.monsterIsCowKingLocked(mon) {
		phaseDue := !mon.CowKingMoveAt.IsZero() && now.Sub(mon.CowKingMoveAt) > 30*time.Second
		if action, moved := w.runMagicCowPeriodicMoveLocked(mon, players, now); moved {
			if action.Kind != 0 {
				return []MonsterAction{action}, nil, nil, nil
			}
		}
		if phaseDue {
			w.updateMagicCowPhaseLocked(mon, now)
		}
	}
	w.updateMagicCowTargetLocked(mon, players, now)
	return w.tickMagicCowAttackLocked(mon, players, now)
}

func (w *World) updateMagicCowTargetLocked(mon *Monster, players map[string]storage.Character, now time.Time) {
	if mon.LastTargetSearchAt.IsZero() {
		mon.LastTargetSearchAt = now
	} else if now.Sub(mon.LastTargetSearchAt) > 8*time.Second || (mon.TargetCharacterID == "" && mon.TargetMonsterID == "" && now.Sub(mon.LastTargetSearchAt) > time.Second) {
		mon.LastTargetSearchAt = now
		w.searchMonsterTargetLocked(mon, players, now)
	}
	if mon.TargetCharacterID == "" && mon.TargetMonsterID == "" && !now.Before(mon.NextSearchAt) {
		w.searchMonsterTargetLocked(mon, players, now)
	}
}

func (w *World) tickMagicCowAttackLocked(mon *Monster, players map[string]storage.Character, now time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	if mon.TargetCharacterID == "" && mon.TargetMonsterID == "" {
		return nil, nil, nil, nil
	}
	var target storage.Character
	var targetMonster *Monster
	if mon.TargetMonsterID != "" {
		targetMonster = w.monsters[mon.TargetMonsterID]
		if targetMonster == nil || targetMonster.MapID != mon.MapID {
			mon.TargetMonsterID = ""
			mon.TargetFocusAt = time.Time{}
			return nil, nil, nil, nil
		}
		target = storage.Character{ID: targetMonster.ID, MapID: targetMonster.MapID, X: targetMonster.X, Y: targetMonster.Y, HP: targetMonster.HP}
	} else {
		var ok bool
		target, ok = players[mon.TargetCharacterID]
		if !ok || target.ID == "" || target.MapID != mon.MapID {
			mon.TargetCharacterID = ""
			mon.TargetX, mon.TargetY = -1, -1
			mon.TargetFocusAt = time.Time{}
			mon.NextSearchAt = now.Add(time.Duration(w.monsterSearchNoTargetMSLocked(mon)) * time.Millisecond)
			return nil, nil, nil, nil
		}
	}
	if mon.X == target.X && mon.Y == target.Y {
		mon.TargetX, mon.TargetY = target.X, target.Y
		return nil, nil, nil, nil
	}
	if abs(mon.X-target.X) > 1 || abs(mon.Y-target.Y) > 1 {
		mon.TargetX, mon.TargetY = target.X, target.Y
		dir := direction(mon.X, mon.Y, target.X, target.Y)
		moved := false
		if targetMonster != nil {
			moved = w.moveMonsterTowardPointLocked(mon, target.X, target.Y, players)
		} else {
			moved = w.moveMonsterTowardLocked(mon, target, dir, players)
		}
		if !moved {
			return nil, nil, nil, nil
		}
		mon.LastWalkAt = now
		return []MonsterAction{w.monsterActionLocked(mon, MonsterActionWalk)}, nil, nil, nil
	}
	if now.Sub(mon.LastAttackAt) <= time.Duration(w.monsterAttackIntervalMSLocked(mon))*time.Millisecond {
		return nil, nil, nil, nil
	}
	mon.LastAttackAt = now
	mon.TargetFocusAt = now
	mon.Dir = direction(mon.X, mon.Y, target.X, target.Y)
	if !w.monsterIsCowKingLocked(mon) {
		mon.HolySeizeUntil = time.Time{}
	}
	power := 0
	if !w.monsterIsCowKingLocked(mon) {
		power = mon.MinAttack
		if mon.MaxAttack > power {
			power += w.rand.Intn(mon.MaxAttack - power + 1)
		}
	}
	if !w.monsterIsCowKingLocked(mon) {
		mapData, mapOK := w.data.Maps[mon.MapID]
		if !mapOK {
			return []MonsterAction{w.monsterActionLocked(mon, MonsterActionHit)}, nil, nil, nil
		}
		frontX, frontY, frontOK := spellLineNextPosition(mapData, mon.X, mon.Y, mon.Dir, 1)
		if !frontOK {
			return []MonsterAction{w.monsterActionLocked(mon, MonsterActionHit)}, nil, nil, nil
		}
		playerList := make([]storage.Character, 0, len(players))
		for _, candidate := range players {
			playerList = append(playerList, candidate)
		}
		frontObject := w.movingObjectAtPointForMonsterLocked(playerList, mon.MapID, frontX, frontY)
		if frontObject.Monster != nil {
			frontMonster := frontObject.Monster
			caster := storage.Character{ID: mon.ID, MapID: mon.MapID, X: mon.X, Y: mon.Y}
			if frontMonster.MasterID == "" || !w.isProperMonsterTargetLocked(caster, characterListFromMap(players), frontMonster) {
				return []MonsterAction{w.monsterActionLocked(mon, MonsterActionHit)}, nil, nil, nil
			}
			damage := w.monsterMagicDamageAfterDefenseLocked(frontMonster, power)
			if damage <= 0 {
				return []MonsterAction{w.monsterActionLocked(mon, MonsterActionHit)}, nil, nil, nil
			}
			hit, err := w.applyReferenceMonsterVsMonsterDamageAtLocked(mon, frontMonster, damage, now)
			if err != nil {
				return nil, nil, nil, err
			}
			hit.Magic = false
			hit.ImpactDelay = 300 * time.Millisecond
			w.monsterBehaviorHits = append(w.monsterBehaviorHits, hit)
			w.enqueueReferenceMonsterStruckLocked(mon, frontMonster, now, 300*time.Millisecond)
			return []MonsterAction{w.monsterActionLocked(mon, MonsterActionHit)}, nil, nil, nil
		}
		if frontObject.Character == nil || !w.monsterCanAttackCharacterTargetLocked(mon, *frontObject.Character) {
			return []MonsterAction{w.monsterActionLocked(mon, MonsterActionHit)}, nil, nil, nil
		}
		target = *frontObject.Character
	}
	if targetMonster != nil {
		if !w.monsterCanLockedMonsterTargetAttackLocked(mon, targetMonster) {
			return []MonsterAction{w.monsterActionLocked(mon, MonsterActionHit)}, nil, nil, nil
		}
		if w.monsterIsCowKingLocked(mon) {
			power = mon.MinAttack
			if mon.MaxAttack > power {
				power += w.rand.Intn(mon.MaxAttack - power + 1)
			}
		}
		damage := w.monsterMagicDamageAfterDefenseLocked(targetMonster, power)
		if w.monsterIsCowKingLocked(mon) {
			physical := w.monsterPhysicalDamageAfterDefenseAtLocked(targetMonster, power/2, now)
			magical := w.monsterMagicDamageAfterDefenseLocked(targetMonster, power/2)
			damage = physical + magical
		}
		if damage <= 0 {
			return []MonsterAction{w.monsterActionLocked(mon, MonsterActionHit)}, nil, nil, nil
		}
		hit, err := w.applyReferenceMonsterVsMonsterDamageAtLocked(mon, targetMonster, damage, now)
		if err != nil {
			return nil, nil, nil, err
		}
		hit.Magic = false
		hit.ImpactDelay = 200 * time.Millisecond
		if hit.Damage > 0 {
			w.monsterBehaviorHits = append(w.monsterBehaviorHits, hit)
			w.enqueueReferenceMonsterStruckLocked(mon, targetMonster, now, 200*time.Millisecond)
		}
		return []MonsterAction{w.monsterActionLocked(mon, MonsterActionHit)}, nil, nil, nil
	}
	attack := func(mon *Monster, target storage.Character) (storage.Character, CharacterHit, error) {
		oldMagic := mon.UseMagic
		mon.UseMagic = true
		updated, hit, err := w.monsterReferenceCharacterDamageAtLocked(mon, target, power, now)
		mon.UseMagic = oldMagic
		return updated, hit, err
	}
	if w.monsterIsCowKingLocked(mon) {
		if !w.monsterCanAttackCharacterTargetLocked(mon, target) {
			return []MonsterAction{w.monsterActionLocked(mon, MonsterActionHit)}, nil, nil, nil
		}
		attack = func(mon *Monster, target storage.Character) (storage.Character, CharacterHit, error) {
			return w.monsterMixedAttackCharacterAtLocked(mon, target, now)
		}
	}
	updated, hit, err := attack(mon, target)
	if err != nil {
		return nil, nil, nil, err
	}
	hit.AttackerID = mon.ID
	hit.AttackerRaceImg = mon.RaceImg
	hit.AttackerAppr = mon.Appr
	hit.AttackerX, hit.AttackerY = mon.X, mon.Y
	hit.ImpactDelay = 300 * time.Millisecond
	if w.monsterIsCowKingLocked(mon) {
		hit.ImpactDelay = 200 * time.Millisecond
	} else {
		hit.Magic = false
	}
	if hit.Damage <= 0 {
		return []MonsterAction{w.monsterActionLocked(mon, MonsterActionHit)}, nil, nil, nil
	}
	return []MonsterAction{w.monsterActionLocked(mon, MonsterActionHit)}, []CharacterHit{hit}, []storage.Character{updated}, nil
}

func (w *World) tickCowKingMonsterLocked(mon *Monster, players map[string]storage.Character, now time.Time, prefix []MonsterAction) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	phaseDue := !mon.CowKingMoveAt.IsZero() && now.Sub(mon.CowKingMoveAt) > 30*time.Second
	if action, moved := w.runMagicCowPeriodicMoveLocked(mon, players, now); moved {
		if action.Kind != 0 {
			return append(prefix, action), nil, nil, nil
		}
		return prefix, nil, nil, nil
	}
	if phaseDue {
		w.updateMagicCowPhaseLocked(mon, now)
	}
	w.updateMagicCowTargetLocked(mon, players, now)
	customActions, hits, chars, err := w.finishMonsterTickWithoutSearchLocked(mon, players, now, prefix, w.tickMagicCowAttackLocked)
	if err != nil {
		return nil, nil, nil, err
	}
	inheritedActions, inheritedHits, inheritedChars, err := w.finishMonsterTickWithoutSearchLocked(mon, players, now, customActions, w.tickNormalMonsterWithoutSearchLocked)
	if err != nil {
		return nil, nil, nil, err
	}
	return inheritedActions, append(hits, inheritedHits...), append(chars, inheritedChars...), nil
}

func (w *World) runMagicCowPeriodicMoveLocked(mon *Monster, players map[string]storage.Character, now time.Time) (MonsterAction, bool) {
	if mon.CowKingMoveAt.IsZero() {
		mon.CowKingMoveAt = now
		return MonsterAction{}, false
	}
	if !mon.CowKingMoveAt.IsZero() && now.Sub(mon.CowKingMoveAt) <= 30*time.Second {
		return MonsterAction{}, false
	}
	mon.CowKingMoveAt = now
	targetMapID := mon.MapID
	targetX, targetY, targetDir := 0, 0, 0
	if mon.TargetCharacterID != "" {
		target, ok := players[mon.TargetCharacterID]
		if !ok || target.ID == "" {
			return MonsterAction{}, false
		}
		targetMapID, targetX, targetY, targetDir = target.MapID, target.X, target.Y, target.Dir
	} else if mon.TargetMonsterID != "" {
		target := w.monsters[mon.TargetMonsterID]
		if target == nil || target.ID == "" {
			return MonsterAction{}, false
		}
		targetMapID, targetX, targetY, targetDir = target.MapID, target.X, target.Y, target.Dir
	} else {
		return MonsterAction{}, false
	}
	if targetMapID != mon.MapID || w.magicCowBlockedAroundLocked(targetMapID, targetX, targetY, players) < 5 {
		return MonsterAction{}, false
	}
	off := dirOffsets[targetDir%len(dirOffsets)]
	x, y := targetX-off[0], targetY-off[1]
	mp, exists := w.data.Maps[mon.MapID]
	if exists && mp.Walkable(x, y) && !w.movingObjectAtLocked(players, mon.MapID, x, y, mon.ID) {
		oldX, oldY := mon.X, mon.Y
		w.moveMonsterLocked(mon, x, y)
		action := w.monsterActionLocked(mon, MonsterActionSpaceMove)
		action.PreviousMapID, action.PreviousX, action.PreviousY = mon.MapID, oldX, oldY
		return action, true
	}
	if !exists || mp.Width < 4 || mp.Height < 4 {
		return MonsterAction{}, true
	}
	edge := 50
	if mp.Height < 150 {
		edge = 20
		if mp.Height < 30 {
			edge = 2
		}
	}
	if mp.Width <= edge+1 || mp.Height <= edge+1 {
		return MonsterAction{}, true
	}
	x = edge + w.rand.Intn(mp.Width-edge-1)
	y = edge + w.rand.Intn(mp.Height-edge-1)
	if w.findMagicCowSpaceMovePointLocked(mp, players, mon, &x, &y) {
		oldX, oldY := mon.X, mon.Y
		w.moveMonsterLocked(mon, x, y)
		action := w.monsterActionLocked(mon, MonsterActionSpaceMove)
		action.PreviousMapID, action.PreviousX, action.PreviousY = mon.MapID, oldX, oldY
		return action, true
	}
	return MonsterAction{}, true
}

func (w *World) findMagicCowSpaceMovePointLocked(mp data.StdMap, players map[string]storage.Character, mon *Monster, x, y *int) bool {
	step := 10
	if mp.Width < 80 {
		step = 3
	}
	endY := 50
	if mp.Height < 150 {
		endY = 15
		if mp.Height < 50 {
			endY = 2
		}
	}
	for attempt := 0; attempt < 201; attempt++ {
		if mp.Walkable(*x, *y) && !w.movingObjectAtLocked(players, mon.MapID, *x, *y, mon.ID) {
			return true
		}
		if *x < mp.Width-endY-1 {
			*x += step
			continue
		}
		*x = w.rand.Intn(mp.Width)
		if *y < mp.Height-endY-1 {
			*y += step
		} else {
			*y = w.rand.Intn(mp.Height)
		}
	}
	return false
}

func (w *World) magicCowBlockedAroundLocked(mapID string, x, y int, players map[string]storage.Character) int {
	mp, ok := w.data.Maps[mapID]
	if !ok {
		return 9
	}
	blocked := 0
	for dx := -1; dx <= 1; dx++ {
		for dy := -1; dy <= 1; dy++ {
			if (dx != 0 || dy != 0) && (!mp.Walkable(x+dx, y+dy) || w.movingObjectAtLocked(players, mapID, x+dx, y+dy, "")) {
				blocked++
			}
		}
	}
	return blocked
}

func (w *World) updateMagicCowPhaseLocked(mon *Monster, now time.Time) {
	if mon.MaxHP <= 0 {
		return
	}
	step := mon.MaxHP / 7
	if step <= 0 {
		return
	}
	phase := 7 - mon.HP/step
	if phase < 0 {
		phase = 0
	}
	if phase >= 2 && phase != mon.CowKingPhase {
		mon.CowKingPhase = phase
		mon.CowKingState = 1
		mon.CowKingPhaseAt = now
		mon.CowKingStoredAttack = mon.AttackIntervalMS
		mon.CowKingStoredWalk = mon.WalkSpeedMS
	}
	if mon.CowKingState == 1 {
		if now.Sub(mon.CowKingPhaseAt) < 8*time.Second {
			mon.AttackIntervalMS = 10000
			return
		}
		mon.CowKingState = 2
		mon.CowKingPhaseAt = now
	}
	if mon.CowKingState == 2 {
		if now.Sub(mon.CowKingPhaseAt) < 8*time.Second {
			mon.AttackIntervalMS = 500
			mon.WalkSpeedMS = 400
			return
		}
		mon.CowKingState = 0
		mon.AttackIntervalMS = mon.CowKingStoredAttack
		mon.WalkSpeedMS = mon.CowKingStoredWalk
	}
}

func (w *World) tickArcherMonsterLocked(mon *Monster, players map[string]storage.Character, now time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	if w.referenceSpecialWalkReadyInclusiveLocked(mon, now) {
		target, found := w.findClosestArcherTargetLocked(mon, players)
		if found && target.character != nil {
			mon.TargetCharacterID = target.character.ID
			mon.TargetMonsterID = ""
			mon.TargetFocusAt = now
		} else if found && target.monster != nil {
			mon.TargetCharacterID = ""
			mon.TargetMonsterID = target.monster.ID
			mon.TargetFocusAt = now
		} else {
			mon.TargetCharacterID = ""
			mon.TargetMonsterID = ""
			mon.TargetX, mon.TargetY = -1, -1
			mon.TargetFocusAt = time.Time{}
		}
	}
	if mon.TargetCharacterID == "" && mon.TargetMonsterID == "" {
		if mon.GuardDirection >= 0 && mon.Dir != mon.GuardDirection {
			mon.Dir = mon.GuardDirection
			return []MonsterAction{w.monsterActionLocked(mon, MonsterActionTurn)}, nil, nil, nil
		}
		return nil, nil, nil, nil
	}
	if mon.TargetMonsterID != "" {
		target := w.monsters[mon.TargetMonsterID]
		if target == nil || target.ID == "" || mon.Race == 112 && !monsterTargetAliveLocked(target) {
			mon.TargetMonsterID = ""
			mon.TargetX, mon.TargetY = -1, -1
			mon.TargetFocusAt = time.Time{}
			return nil, nil, nil, nil
		}
		if now.Sub(mon.LastAttackAt) < time.Duration(w.monsterAttackIntervalMSLocked(mon))*time.Millisecond {
			return nil, nil, nil, nil
		}
		mon.LastAttackAt = now
		mon.Dir = direction(mon.X, mon.Y, target.X, target.Y)
		power := mon.MinAttack
		if mon.MaxAttack > mon.MinAttack {
			power += w.rand.Intn(mon.MaxAttack - mon.MinAttack + 1)
		}
		hit, err := w.archerGuardAttackMonsterLocked(mon, target, power, now)
		if err != nil {
			return nil, nil, nil, err
		}
		hit.ImpactDelay = time.Duration(maxInt(abs(mon.X-target.X), abs(mon.Y-target.Y))*50+600) * time.Millisecond
		action := w.monsterActionLocked(mon, MonsterActionFlyAxe)
		action.TargetActor = MonsterActorID(*target)
		action.TargetX = target.X
		action.TargetY = target.Y
		if hit.Damage > 0 {
			w.monsterBehaviorHits = append(w.monsterBehaviorHits, hit)
		}
		return []MonsterAction{action}, nil, nil, nil
	}
	target, ok := players[mon.TargetCharacterID]
	if !ok || target.ID == "" {
		mon.TargetCharacterID = ""
		mon.TargetX, mon.TargetY = -1, -1
		mon.TargetFocusAt = time.Time{}
		return nil, nil, nil, nil
	}
	if now.Sub(mon.LastAttackAt) < time.Duration(w.monsterAttackIntervalMSLocked(mon))*time.Millisecond {
		return nil, nil, nil, nil
	}
	mon.LastAttackAt = now
	mon.Dir = direction(mon.X, mon.Y, target.X, target.Y)
	oldTargetCharacterID := mon.TargetCharacterID
	oldTargetMonsterID := mon.TargetMonsterID
	oldTargetFocusAt := mon.TargetFocusAt
	oldTargetX, oldTargetY := mon.TargetX, mon.TargetY
	oldNextSearchAt := mon.NextSearchAt
	power := mon.MinAttack
	if mon.MaxAttack > mon.MinAttack {
		power += w.rand.Intn(mon.MaxAttack - mon.MinAttack + 1)
	}
	updated, hit, err := w.monsterReferenceCharacterDamageAtLocked(mon, target, power, now)
	if err != nil {
		return nil, nil, nil, err
	}
	mon.TargetCharacterID = oldTargetCharacterID
	mon.TargetMonsterID = oldTargetMonsterID
	mon.TargetFocusAt = oldTargetFocusAt
	mon.TargetX, mon.TargetY = oldTargetX, oldTargetY
	mon.NextSearchAt = oldNextSearchAt
	if hit.Damage > 0 {
		updated.LastHitterID = mon.ID
		updated.LastHitterAt = now.UnixNano()
		updated.ExpHitterID = ""
		updated.ExpHitterAt = 0
	}
	if mon.Race == 112 {
		distance := maxInt(abs(mon.X-target.X), abs(mon.Y-target.Y))
		hit.ImpactDelay = time.Duration(distance*50+600) * time.Millisecond
	}
	actionKind := MonsterActionHit
	if mon.Race == 112 {
		actionKind = MonsterActionFlyAxe
	}
	action := w.monsterActionLocked(mon, actionKind)
	if actionKind == MonsterActionFlyAxe {
		action.TargetActor = CharacterActorID(target)
		action.TargetX = target.X
		action.TargetY = target.Y
	}
	if actionKind == MonsterActionFlyAxe && hit.Damage <= 0 {
		return []MonsterAction{action}, nil, nil, nil
	}
	return []MonsterAction{action}, []CharacterHit{hit}, []storage.Character{updated}, nil
}

func (w *World) tickArcherAndInheritedLocked(mon *Monster, players map[string]storage.Character, now time.Time, prefix []MonsterAction) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	actions, hits, chars, err := w.tickArcherMonsterLocked(mon, players, now)
	if err != nil {
		return nil, nil, nil, err
	}
	return append(append([]MonsterAction{}, prefix...), actions...), hits, chars, nil
}

func (w *World) tickStickMonsterLocked(mon *Monster, players map[string]storage.Character, now time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	if mon == nil || !mon.Alive || mon.Ghost {
		return nil, nil, nil, nil
	}
	if mon.FixedHideMode {
		comeOutRange := w.monsterStickComeOutRangeLocked(mon)
		found := false
		for _, key := range w.visibleActorKeysLocked(mon.ID, mon.MapID, mon.X, mon.Y, comeOutRange, players) {
			if strings.HasPrefix(key, "character:") {
				target, ok := players[strings.TrimPrefix(key, "character:")]
				found = ok && w.monsterCanTargetCharacterLocked(mon, target) && abs(mon.X-target.X) < comeOutRange && abs(mon.Y-target.Y) < comeOutRange
			} else if strings.HasPrefix(key, "monster:") {
				target := w.monsters[strings.TrimPrefix(key, "monster:")]
				found = monsterTargetAliveLocked(target) && target.ID != mon.ID && target.MasterID != "" && (mon.MasterID == "" || target.MasterID != mon.MasterID) && !target.FixedHideMode && !target.AdminMode && !target.StoneMode && target.MapID == mon.MapID && (!target.Hidden || monsterCanSeeTransparent(mon)) && abs(mon.X-target.X) <= comeOutRange-1 && abs(mon.Y-target.Y) <= comeOutRange-1
			}
			if found {
				break
			}
		}
		if found {
			mon.Hidden = false
			mon.FixedHideMode = false
			mon.NextSearchAt = now
			return []MonsterAction{w.monsterActionLocked(mon, MonsterActionReveal)}, nil, nil, nil
		}
		return nil, nil, nil, nil
	}
	if (mon.TargetCharacterID != "" || mon.TargetMonsterID != "") && now.Sub(mon.LastAttackAt) > time.Duration(w.monsterAttackIntervalMSLocked(mon))*time.Millisecond {
		w.searchMonsterTargetLocked(mon, players, now)
	}
	if mon.TargetCharacterID == "" && mon.TargetMonsterID == "" && !now.Before(mon.NextSearchAt) {
		w.searchMonsterTargetLocked(mon, players, now)
		if mon.TargetCharacterID == "" && mon.TargetMonsterID == "" {
			mon.Hidden = true
			mon.FixedHideMode = true
			mon.TargetX = -1
			mon.TargetY = -1
			mon.NextSearchAt = now.Add(time.Duration(w.monsterSearchNoTargetMSLocked(mon)) * time.Millisecond)
			return []MonsterAction{w.monsterActionLocked(mon, MonsterActionHide)}, nil, nil, nil
		}
	}
	if mon.TargetCharacterID == "" && mon.TargetMonsterID == "" {
		return nil, nil, nil, nil
	}
	if mon.TargetMonsterID != "" {
		target := w.monsters[mon.TargetMonsterID]
		if target == nil || target.ID == "" || target.MapID != mon.MapID {
			mon.TargetMonsterID = ""
			mon.TargetFocusAt = time.Time{}
			mon.Hidden = true
			mon.FixedHideMode = true
			mon.TargetX = -1
			mon.TargetY = -1
			mon.NextSearchAt = now.Add(time.Duration(w.monsterSearchNoTargetMSLocked(mon)) * time.Millisecond)
			return []MonsterAction{w.monsterActionLocked(mon, MonsterActionHide)}, nil, nil, nil
		}
		attackRange := w.monsterStickAttackRangeLocked(mon)
		if abs(mon.X-target.X) > attackRange || abs(mon.Y-target.Y) > attackRange {
			mon.Hidden = true
			mon.FixedHideMode = true
			mon.NextSearchAt = now.Add(time.Duration(w.monsterSearchNoTargetMSLocked(mon)) * time.Millisecond)
			return []MonsterAction{w.monsterActionLocked(mon, MonsterActionHide)}, nil, nil, nil
		}
		if (mon.X != target.X || mon.Y != target.Y) && abs(mon.X-target.X) <= 1 && abs(mon.Y-target.Y) <= 1 {
			if now.Sub(mon.LastAttackAt) <= time.Duration(w.monsterAttackIntervalMSLocked(mon))*time.Millisecond {
				return nil, nil, nil, nil
			}
			mon.Dir = direction(mon.X, mon.Y, target.X, target.Y)
			mon.LastAttackAt = now
			mon.TargetFocusAt = now
			mon.HolySeizeUntil = time.Time{}
			action := w.monsterActionLocked(mon, MonsterActionHit)
			action.TargetActor = MonsterActorID(*target)
			action.TargetX = target.X
			action.TargetY = target.Y
			if !w.monsterCanLockedMonsterTargetAttackLocked(mon, target) {
				return []MonsterAction{action}, nil, nil, nil
			}
			var hit AttackResult
			var err error
			if mon.UseMagic {
				power := mon.MagicAttack
				maxPower := mon.MagicAttackMax
				if power <= 0 {
					power = mon.MinAttack
					maxPower = mon.MaxAttack
				}
				if maxPower > power {
					power += w.rand.Intn(maxPower - power + 1)
				}
				w.monsterMagicStruckLocked(target, now)
				hit, err = w.applyMonsterMagicDamageAtLocked(storage.Character{ID: mon.ID, MapID: mon.MapID, X: mon.X, Y: mon.Y}, target, w.monsterMagicDamageAfterDefenseLocked(target, power), false, now)
				hit.Magic = true
				hit.ImpactDelay = 200 * time.Millisecond
			} else {
				power := mon.MinAttack
				if mon.MaxAttack > power {
					power += w.rand.Intn(mon.MaxAttack - power + 1)
				}
				hit, err = w.archerAttackMonsterLocked(mon, target, power)
			}
			if err != nil {
				return nil, nil, nil, err
			}
			if hit.Damage > 0 {
				w.monsterBehaviorHits = append(w.monsterBehaviorHits, hit)
			}
			return []MonsterAction{action}, nil, nil, nil
		}
		mon.TargetX, mon.TargetY = target.X, target.Y
		if mon.X == target.X && mon.Y == target.Y {
			return nil, nil, nil, nil
		}
		if w.moveMonsterTowardPointLocked(mon, target.X, target.Y, players) {
			mon.LastWalkAt = now
			return []MonsterAction{w.monsterActionLocked(mon, MonsterActionWalk)}, nil, nil, nil
		}
		return nil, nil, nil, nil
	}
	target, ok := players[mon.TargetCharacterID]
	if !ok {
		mon.TargetCharacterID = ""
		mon.TargetFocusAt = time.Time{}
		mon.Hidden = true
		mon.FixedHideMode = true
		mon.TargetX = -1
		mon.TargetY = -1
		mon.NextSearchAt = now.Add(time.Duration(w.monsterSearchNoTargetMSLocked(mon)) * time.Millisecond)
		return []MonsterAction{w.monsterActionLocked(mon, MonsterActionHide)}, nil, nil, nil
	}
	attackRange := w.monsterStickAttackRangeLocked(mon)
	if abs(mon.X-target.X) > attackRange || abs(mon.Y-target.Y) > attackRange {
		mon.Hidden = true
		mon.FixedHideMode = true
		mon.NextSearchAt = now.Add(time.Duration(w.monsterSearchNoTargetMSLocked(mon)) * time.Millisecond)
		return []MonsterAction{w.monsterActionLocked(mon, MonsterActionHide)}, nil, nil, nil
	}
	if (mon.X != target.X || mon.Y != target.Y) && abs(mon.X-target.X) <= 1 && abs(mon.Y-target.Y) <= 1 {
		if now.Sub(mon.LastAttackAt) <= time.Duration(w.monsterAttackIntervalMSLocked(mon))*time.Millisecond {
			return nil, nil, nil, nil
		}
		mon.Dir = direction(mon.X, mon.Y, target.X, target.Y)
		mon.LastAttackAt = now
		mon.TargetFocusAt = now
		mon.HolySeizeUntil = time.Time{}
		actionKind := MonsterActionHit
		if mon.UseMagic {
			actionKind = MonsterActionLighting
		}
		action := w.monsterActionLocked(mon, actionKind)
		action.TargetActor = CharacterActorID(target)
		action.TargetX = target.X
		action.TargetY = target.Y
		if !w.monsterCanAttackCharacterTargetLocked(mon, target) {
			return []MonsterAction{action}, nil, nil, nil
		}
		updated, hit, err := w.monsterAttackCharacterWithHitCheckAtLocked(mon, target, now)
		if err != nil {
			return nil, nil, nil, err
		}
		if hit.Damage <= 0 {
			return []MonsterAction{action}, nil, nil, nil
		}
		return []MonsterAction{action}, []CharacterHit{hit}, []storage.Character{updated}, nil
	}
	mon.TargetX, mon.TargetY = target.X, target.Y
	if mon.X == target.X && mon.Y == target.Y {
		return nil, nil, nil, nil
	}
	if w.moveMonsterTowardPointLocked(mon, target.X, target.Y, players) {
		mon.LastWalkAt = now
		return []MonsterAction{w.monsterActionLocked(mon, MonsterActionWalk)}, nil, nil, nil
	}
	return nil, nil, nil, nil
}

func (w *World) tickStoneMonsterLocked(mon *Monster, players map[string]storage.Character, now time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	return w.tickStoneMonsterModeLocked(mon, players, now, false, false)
}

func (w *World) tickStoneReferencePhaseLocked(mon *Monster, players map[string]storage.Character, now time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	return w.tickStoneMonsterModeLocked(mon, players, now, false, true)
}

func (w *World) tickStoneAttackLocked(mon *Monster, players map[string]storage.Character, now time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	return w.tickStoneMonsterModeLocked(mon, players, now, true, false)
}

func (w *World) tickStoneMonsterModeLocked(mon *Monster, players map[string]storage.Character, now time.Time, attackOnly, phaseOnly bool) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	if !attackOnly && mon.StoneMode {
		if w.stoneHasVisibleTargetLocked(mon, players, 2) {
			mon.StoneMode = false
			if mon.Race == 102 {
				eventID := w.nextGroundEventIDLocked()
				w.addGroundEventLocked(SpellGroundEvent{ID: eventID, MapID: mon.MapID, X: mon.X, Y: mon.Y, Type: 6, Duration: 5 * time.Minute, StartAt: now})
			}
			actions := []MonsterAction{w.monsterActionLocked(mon, MonsterActionReveal)}
			if mon.Race == 101 {
				nearby := make([]*Monster, 0)
				for _, areaTarget := range w.spellAreaTargetsWithLifeFilterLocked(nil, mon.MapID, mon.X, mon.Y, 7, true) {
					other := areaTarget.Monster
					if other == nil || other == mon || other.Race != 101 || !other.StoneMode {
						continue
					}
					other.StoneMode = false
					nearby = append(nearby, other)
				}
				for _, other := range nearby {
					actions = append(actions, w.monsterActionLocked(other, MonsterActionReveal))
				}
			}
			return actions, nil, nil, nil
		}
		return nil, nil, nil, nil
	}
	if !attackOnly && now.After(mon.NextSearchAt) {
		w.searchMonsterTargetLocked(mon, players, now)
		searchInterval := time.Second
		if mon.TargetCharacterID != "" || mon.TargetMonsterID != "" {
			searchInterval = 8 * time.Second
		}
		mon.NextSearchAt = now.Add(searchInterval)
		w.maybeCallStoneKingSlavesLocked(mon, now)
	}
	if phaseOnly {
		return nil, nil, nil, nil
	}
	if !attackOnly && w.thinkMonsterLocked(mon, players, now) {
		return []MonsterAction{w.monsterActionLocked(mon, MonsterActionWalk)}, nil, nil, nil
	}
	if mon.TargetMonsterID != "" {
		target := w.monsters[mon.TargetMonsterID]
		if target == nil || target.ID == "" || target.ID == mon.ID || target.MapID != mon.MapID {
			mon.TargetMonsterID = ""
			mon.TargetFocusAt = time.Time{}
			return nil, nil, nil, nil
		}
		if (mon.X != target.X || mon.Y != target.Y) && abs(mon.X-target.X) <= 1 && abs(mon.Y-target.Y) <= 1 && now.Sub(mon.LastAttackAt) > time.Duration(w.monsterAttackIntervalMSLocked(mon))*time.Millisecond {
			mon.Dir = direction(mon.X, mon.Y, target.X, target.Y)
			mon.LastAttackAt = now
			mon.TargetFocusAt = now
			mon.HolySeizeUntil = time.Time{}
			power := mon.MinAttack
			maxPower := mon.MaxAttack
			if mon.Race == 102 {
				power = mon.MagicAttack
				maxPower = mon.MagicAttackMax
				if power <= 0 {
					power = mon.MinAttack
					maxPower = mon.MaxAttack
				}
			}
			if !w.monsterCanLockedMonsterTargetAttackLocked(mon, target) {
				return []MonsterAction{w.monsterActionLocked(mon, MonsterActionHit)}, nil, nil, nil
			}
			if maxPower > power {
				power += w.rand.Intn(maxPower - power + 1)
			}
			var hit AttackResult
			var err error
			if mon.Race == 102 {
				damage := w.monsterMagicDamageAfterDefenseLocked(target, power)
				hit, err = w.applyMonsterMagicDamageAtLocked(storage.Character{ID: mon.ID, MapID: mon.MapID, X: mon.X, Y: mon.Y}, target, damage, false, now)
			} else {
				hit, err = w.archerAttackMonsterLocked(mon, target, power)
			}
			if err != nil {
				return nil, nil, nil, err
			}
			if mon.Race == 102 {
				hit.Magic = true
				hit.ImpactDelay = 200 * time.Millisecond
			}
			if hit.Damage > 0 {
				w.monsterBehaviorHits = append(w.monsterBehaviorHits, hit)
			}
			return []MonsterAction{w.monsterActionLocked(mon, MonsterActionHit)}, nil, nil, nil
		}
	}
	if mon.TargetCharacterID != "" {
		target, ok := players[mon.TargetCharacterID]
		if ok && target.MapID == mon.MapID && (mon.X != target.X || mon.Y != target.Y) && abs(mon.X-target.X) <= 1 && abs(mon.Y-target.Y) <= 1 && now.Sub(mon.LastAttackAt) > time.Duration(w.monsterAttackIntervalMSLocked(mon))*time.Millisecond {
			mon.Dir = direction(mon.X, mon.Y, target.X, target.Y)
			mon.LastAttackAt = now
			mon.TargetFocusAt = now
			mon.HolySeizeUntil = time.Time{}
			attack := func(mon *Monster, target storage.Character) (storage.Character, CharacterHit, error) {
				return w.monsterAttackCharacterAtLocked(mon, target, now)
			}
			if mon.Race == 102 {
				attack = func(mon *Monster, target storage.Character) (storage.Character, CharacterHit, error) {
					return w.monsterReferenceMagicCharacterAtLocked(mon, target, now)
				}
			}
			if !w.monsterCanAttackCharacterTargetLocked(mon, target) {
				return []MonsterAction{w.monsterActionLocked(mon, MonsterActionHit)}, nil, nil, nil
			}
			updated, hit, err := attack(mon, target)
			if err != nil {
				return nil, nil, nil, err
			}
			if hit.Damage <= 0 {
				return []MonsterAction{w.monsterActionLocked(mon, MonsterActionHit)}, nil, nil, nil
			}
			return []MonsterAction{w.monsterActionLocked(mon, MonsterActionHit)}, []CharacterHit{hit}, []storage.Character{updated}, nil
		}
	}
	return nil, nil, nil, nil
}

func (w *World) tickStoneCustomLocked(mon *Monster, players map[string]storage.Character, now time.Time, prefix []MonsterAction) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	if !w.referenceStoneRunReadyLocked(mon, now) {
		return prefix, nil, nil, nil
	}
	actions, hits, chars, err := w.tickStoneMonsterLocked(mon, players, now)
	if err != nil {
		return nil, nil, nil, err
	}
	w.cleanupTrackedMonsterChildrenLocked(mon)
	return append(prefix, actions...), hits, chars, nil
}

func (w *World) tickStoneReferenceCustomLocked(mon *Monster, players map[string]storage.Character, now time.Time, prefix []MonsterAction) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	if !w.referenceStoneRunReadyLocked(mon, now) {
		return prefix, nil, nil, nil
	}
	actions, hits, chars, err := w.tickStoneReferencePhaseLocked(mon, players, now)
	if err != nil {
		return nil, nil, nil, err
	}
	w.cleanupTrackedMonsterChildrenLocked(mon)
	return append(prefix, actions...), hits, chars, nil
}

func (w *World) maybeCallStoneKingSlavesLocked(mon *Monster, now time.Time) {
	if mon.Race != 102 {
		return
	}
	if mon.HP == mon.MaxHP {
		mon.StoneDangerLevel = 5
		return
	}
	if mon.StoneDangerLevel > 0 && mon.StoneDangerLevel*mon.MaxHP > mon.HP*5 {
		mon.StoneDangerLevel--
		w.callStoneKingSlavesLocked(mon)
	}
}

func (w *World) callStoneKingSlavesLocked(mon *Monster) {
	const maxSlaves = 30
	names := []string{"祖玛卫士", "祖玛雕像", "祖玛弓箭手", "楔蛾"}
	count := w.monsterTraceIntn("stone_king.slave_count", 6) + 6
	if remaining := maxSlaves - len(mon.ChildIDs); count > remaining {
		count = remaining
	}
	if count <= 0 || mon.Dir < 0 || mon.Dir >= len(dirOffsets) {
		return
	}
	off := dirOffsets[mon.Dir]
	x, y := mon.X+off[0], mon.Y+off[1]
	for i := 0; i < count; i++ {
		tpl, ok := w.monsterTemplateByIDLocked(names[w.monsterTraceIntn("stone_king.slave_kind", len(names))])
		if !ok {
			continue
		}
		child := w.createSpawnMonsterLocked(data.StdSpawn{MapID: mon.MapID, MonsterID: tpl.ID, X: x, Y: y, Count: 1}, tpl, x, y)
		if child == nil {
			continue
		}
		if mon.ChildIDs == nil {
			mon.ChildIDs = make(map[string]struct{})
		}
		mon.ChildIDs[child.ID] = struct{}{}
	}
}

func (w *World) tickPassiveMonsterLocked(mon *Monster, players map[string]storage.Character, now time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	if mon.TargetCharacterID != "" {
		return w.tickMonsterTargetLocked(mon, players, now)
	}
	if mon.TargetFocusAt.IsZero() {
		if action, ok := w.wanderMonsterLocked(mon, players); ok {
			return []MonsterAction{action}, nil, nil, nil
		}
	}
	return nil, nil, nil, nil
}

func (w *World) tickAnimalMonsterLocked(mon *Monster, players map[string]storage.Character, now time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	if mon.FleeOnSight {
		return w.tickFleeAnimalMonsterLocked(mon, players, now)
	}
	return w.tickPassiveMonsterLocked(mon, players, now)
}

func (w *World) tickFleeAnimalMonsterLocked(mon *Monster, players map[string]storage.Character, now time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	w.selectFleeAnimalTargetLocked(mon, players, now)
	return w.fleeAnimalActionLocked(mon, players, now)
}

func (w *World) selectFleeAnimalTargetLocked(mon *Monster, players map[string]storage.Character, now time.Time) {
	if target, ok := w.findClosestFleeTargetLocked(mon, players, mon.ViewRange); ok {
		mon.TargetCharacterID = ""
		mon.TargetMonsterID = ""
		if target.character != nil {
			mon.TargetCharacterID = target.character.ID
		} else {
			mon.TargetMonsterID = target.monster.ID
		}
		mon.TargetFocusAt = now
		mon.RunAwayMode = true
		mon.RunAwayUntil = time.Time{}
		mon.TargetX = -1
		mon.TargetY = -1
	} else {
		mon.TargetCharacterID = ""
		mon.TargetMonsterID = ""
		mon.TargetFocusAt = time.Time{}
		mon.RunAwayMode = false
		mon.RunAwayUntil = time.Time{}
		mon.TargetX = -1
		mon.TargetY = -1
		mon.NextSearchAt = now.Add(time.Duration(w.monsterSearchNoTargetMSLocked(mon)) * time.Millisecond)
	}
}

func (w *World) fleeAnimalActionLocked(mon *Monster, players map[string]storage.Character, now time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	if !w.prepareFleeAnimalTargetLocked(mon, players, now) {
		return w.tickPassiveMonsterLocked(mon, players, now)
	}
	return w.moveFleeAnimalTargetLocked(mon, players, now)
}

func (w *World) prepareFleeAnimalTargetLocked(mon *Monster, players map[string]storage.Character, now time.Time) bool {
	if mon.RunAwayMode && (mon.TargetCharacterID != "" || mon.TargetMonsterID != "") {
		var target storage.Character
		if mon.TargetCharacterID != "" {
			var ok bool
			target, ok = players[mon.TargetCharacterID]
			if !ok || target.ID == "" || !w.monsterCanKeepCharacterTargetLocked(mon, target) {
				mon.TargetCharacterID = ""
				mon.TargetFocusAt = time.Time{}
				mon.RunAwayMode = false
				mon.RunAwayUntil = time.Time{}
				mon.TargetX = -1
				mon.TargetY = -1
				mon.NextSearchAt = now.Add(time.Duration(w.monsterSearchNoTargetMSLocked(mon)) * time.Millisecond)
				return false
			}
		} else {
			candidate := w.monsters[mon.TargetMonsterID]
			if !monsterTargetAliveLocked(candidate) || candidate.ID == "" || candidate.FixedHideMode || candidate.MapID != mon.MapID || candidate.Hidden && !monsterCanSeeTransparent(mon) {
				mon.TargetMonsterID = ""
				mon.TargetFocusAt = time.Time{}
				mon.RunAwayMode = false
				mon.RunAwayUntil = time.Time{}
				mon.TargetX = -1
				mon.TargetY = -1
				mon.NextSearchAt = now.Add(time.Duration(w.monsterSearchNoTargetMSLocked(mon)) * time.Millisecond)
				return false
			}
			target = storage.Character{ID: candidate.ID, MapID: candidate.MapID, X: candidate.X, Y: candidate.Y, HP: candidate.HP}
		}
		gateX, _, gateOK := w.lastVisibleAnimalActorPointLocked(mon, players)
		if !gateOK {
			gateX = target.X
		}
		if abs(mon.X-gateX) <= 6 && abs(mon.X-gateX) <= 6 {
			mon.TargetX, mon.TargetY = w.referenceAnimalFleePointLocked(mon, target)
		}
		return mon.TargetX >= 0 && mon.TargetY >= 0
	}
	return false
}

func (w *World) moveFleeAnimalTargetLocked(mon *Monster, players map[string]storage.Character, now time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	if mon.X == mon.TargetX && mon.Y == mon.TargetY {
		mon.TargetX = -1
		mon.TargetY = -1
		return nil, nil, nil, nil
	}
	if w.moveMonsterTowardPointLocked(mon, mon.TargetX, mon.TargetY, players) {
		mon.LastWalkAt = now
		return []MonsterAction{w.monsterActionLocked(mon, MonsterActionWalk)}, nil, nil, nil
	}
	return nil, nil, nil, nil
}

func (w *World) explosionSpiderLocked(mon *Monster, players map[string]storage.Character, now time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	return w.explosionSpiderDamageLocked(mon, players, now, true)
}

func (w *World) explosionSpiderAttackLocked(mon *Monster, players map[string]storage.Character, now time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	return w.explosionSpiderDamageLocked(mon, players, now, true)
}

func (w *World) explosionSpiderDamageLocked(mon *Monster, players map[string]storage.Character, now time.Time, detonate bool) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	if detonate {
		mon.HP = core.ApplyHPDelta(mon.HP, mon.MaxHP, -mon.HP).HP
		mon.PendingDeath = true
		mon.DeathHitterID = mon.ExpHitterID
		if mon.DeathHitterID == "" {
			mon.DeathHitterID = mon.LastHitterID
		}
	}
	var hits []CharacterHit
	var updated []storage.Character
	power := mon.MinAttack
	if mon.MaxAttack > mon.MinAttack {
		power += w.rand.Intn(mon.MaxAttack - mon.MinAttack + 1)
	}
	visibleRange := mon.ViewRange
	if visibleRange < 1 {
		visibleRange = 1
	}
	for _, key := range w.visibleActorKeysLocked(mon.ID, mon.MapID, mon.X, mon.Y, visibleRange, players) {
		if strings.HasPrefix(key, "character:") {
			ch, ok := players[strings.TrimPrefix(key, "character:")]
			if !ok || w.characterDeadLocked(ch) || !w.monsterCanAttackCharacterTargetLocked(mon, ch) || abs(ch.X-mon.X) > 1 || abs(ch.Y-mon.Y) > 1 {
				continue
			}
			lastHitterID := ch.LastHitterID
			lastHitterAt := ch.LastHitterAt
			expHitterID := ch.ExpHitterID
			expHitterAt := ch.ExpHitterAt
			healthTick := ch.HealthTick
			spellTick := ch.SpellTick
			perHealth := ch.PerHealth
			perSpell := ch.PerSpell
			changed, hit, err := w.monsterMixedAttackCharacterWithPowerAtLocked(mon, ch, power, now)
			if err != nil {
				return nil, nil, nil, err
			}
			changed.LastHitterID = lastHitterID
			changed.LastHitterAt = lastHitterAt
			changed.ExpHitterID = expHitterID
			changed.ExpHitterAt = expHitterAt
			changed.HealthTick = healthTick
			changed.SpellTick = spellTick
			changed.PerHealth = perHealth
			changed.PerSpell = perSpell
			if hit.Damage > 0 {
				hit.ImpactDelay = 700 * time.Millisecond
				hits = append(hits, hit)
				updated = append(updated, changed)
			}
			continue
		}
		if !strings.HasPrefix(key, "monster:") {
			continue
		}
		target := w.monsters[strings.TrimPrefix(key, "monster:")]
		if !monsterTargetAliveLocked(target) || !w.monsterCanAttackMonsterTargetLocked(mon, target) || abs(target.X-mon.X) > 1 || abs(target.Y-mon.Y) > 1 {
			continue
		}
		damage := w.monsterPhysicalDamageAfterDefenseAtLocked(target, power/2, now) + w.monsterMagicDamageAfterDefenseLocked(target, power/2)
		if damage <= 0 {
			continue
		}
		hit, err := w.applyReferenceMonsterVsMonsterDamageAtLocked(mon, target, damage, now)
		if err != nil {
			return nil, nil, nil, err
		}
		if hit.Damage > 0 {
			hit.ImpactDelay = 700 * time.Millisecond
			w.monsterBehaviorHits = append(w.monsterBehaviorHits, hit)
			w.enqueueReferenceMonsterStruckLocked(mon, target, now, 700*time.Millisecond)
		}
	}
	return nil, hits, updated, nil
}

func (w *World) tickBigHeartLocked(mon *Monster, players map[string]storage.Character, now time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	if now.Sub(mon.LastAttackAt) <= time.Duration(w.monsterAttackIntervalMSLocked(mon))*time.Millisecond {
		return nil, nil, nil, nil
	}
	visibleKeys := w.visibleActorKeysLocked(mon.ID, mon.MapID, mon.X, mon.Y, mon.ViewRange, players)
	hasVisibleActor := false
	for _, key := range visibleKeys {
		if key != monsterVisibleActorKey(mon.ID) {
			hasVisibleActor = true
			break
		}
	}
	if !hasVisibleActor {
		return nil, nil, nil, nil
	}
	mon.LastAttackAt = now
	actions := []MonsterAction{w.monsterActionLocked(mon, MonsterActionHit)}
	power := mon.MinAttack
	if mon.MaxAttack > mon.MinAttack {
		power += w.rand.Intn(mon.MaxAttack - mon.MinAttack + 1)
	}
	targetIDs := make([]string, 0)
	monsterTargets := make([]pendingMonsterTarget, 0)
	orderedTargets := make([]pendingMonsterTarget, 0)
	for _, key := range visibleKeys {
		if strings.HasPrefix(key, "character:") {
			ch, ok := players[strings.TrimPrefix(key, "character:")]
			if !ok || w.characterDeadLocked(ch) || !w.monsterCanAttackCharacterTargetLocked(mon, ch) || abs(ch.X-mon.X) > mon.ViewRange || abs(ch.Y-mon.Y) > mon.ViewRange {
				continue
			}
			targetIDs = append(targetIDs, ch.ID)
			orderedTargets = append(orderedTargets, pendingMonsterTarget{ID: ch.ID, Damage: power, TargetX: ch.X, TargetY: ch.Y, TargetRange: 1, ReferenceDelay: true})
			actions[0].TargetEffects = append(actions[0].TargetEffects, MonsterTargetEffect{CharacterID: ch.ID, X: ch.X, Y: ch.Y, Type: 1})
			continue
		}
		if !strings.HasPrefix(key, "monster:") {
			continue
		}
		candidate := w.monsters[strings.TrimPrefix(key, "monster:")]
		if !monsterTargetAliveLocked(candidate) || !w.monsterCanAttackMonsterTargetLocked(mon, candidate) || abs(candidate.X-mon.X) > mon.ViewRange || abs(candidate.Y-mon.Y) > mon.ViewRange {
			continue
		}
		actions[0].TargetEffects = append(actions[0].TargetEffects, MonsterTargetEffect{CharacterID: candidate.ID, X: candidate.X, Y: candidate.Y, Type: 1})
		monsterTargets = append(monsterTargets, pendingMonsterTarget{ID: candidate.ID, Damage: power, Monster: true, TargetX: candidate.X, TargetY: candidate.Y, TargetRange: 1, ReferenceDelay: true})
		orderedTargets = append(orderedTargets, pendingMonsterTarget{ID: candidate.ID, Damage: power, Monster: true, TargetX: candidate.X, TargetY: candidate.Y, TargetRange: 1, ReferenceDelay: true})
	}
	if len(targetIDs) > 0 && len(monsterTargets) > 0 {
		w.enqueuePendingMonsterAttackLocked(pendingMonsterAttack{DueAt: now.Add(200 * time.Millisecond), MonsterID: mon.ID, Targets: orderedTargets, Damage: power, ImpactDelay: 200 * time.Millisecond})
	} else if len(targetIDs) > 0 {
		w.enqueuePendingMonsterAttackLocked(pendingMonsterAttack{DueAt: now.Add(200 * time.Millisecond), MonsterID: mon.ID, Targets: orderedTargets, Damage: power, ImpactDelay: 200 * time.Millisecond})
	} else if len(monsterTargets) > 0 {
		w.enqueuePendingMonsterAttackLocked(pendingMonsterAttack{DueAt: now.Add(200 * time.Millisecond), MonsterID: mon.ID, MonsterTargets: monsterTargets, Damage: power, ImpactDelay: 200 * time.Millisecond})
	}
	return actions, nil, nil, nil
}

func (w *World) tickElectronicScorpionLocked(mon *Monster, players map[string]storage.Character, now time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	mon.UseMagic = mon.HP < mon.MaxHP/2
	if mon.LastTargetSearchAt.IsZero() {
		mon.LastTargetSearchAt = now
	}
	if mon.TargetCharacterID == "" && mon.TargetMonsterID == "" && now.Sub(mon.LastTargetSearchAt) > 1*time.Second {
		mon.LastTargetSearchAt = now
		w.searchMonsterTargetLocked(mon, players, now)
	}
	if mon.TargetCharacterID == "" && mon.TargetMonsterID == "" {
		return nil, nil, nil, nil
	}
	if mon.TargetMonsterID != "" {
		target := w.monsters[mon.TargetMonsterID]
		if target == nil || target.ID == "" || target.MapID != mon.MapID {
			return nil, nil, nil, nil
		}
		nx := abs(mon.X - target.X)
		ny := abs(mon.Y - target.Y)
		if nx > 2 || ny > 2 {
			mon.TargetX, mon.TargetY = target.X, target.Y
			return nil, nil, nil, nil
		}
		if !mon.UseMagic && nx != 2 && ny != 2 {
			return nil, nil, nil, nil
		}
		if now.Sub(mon.LastAttackAt) <= time.Duration(w.monsterAttackIntervalMSLocked(mon))*time.Millisecond {
			return nil, nil, nil, nil
		}
		mon.LastAttackAt = now
		mon.TargetFocusAt = now
		mon.Dir = direction(mon.X, mon.Y, target.X, target.Y)
		power := mon.MagicAttack
		if power <= 0 {
			power = mon.MinAttack
		}
		if mon.MagicAttackMax > power {
			power += w.rand.Intn(mon.MagicAttackMax - power + 1)
		}
		damage := w.monsterMagicDamageAfterDefenseLocked(target, power)
		hit, err := w.applyReferenceMonsterVsMonsterDamageAtLocked(mon, target, damage, now)
		if err != nil {
			return nil, nil, nil, err
		}
		hit.Magic = false
		hit.ImpactDelay = 200 * time.Millisecond
		if hit.Damage > 0 && mon.MP&0xFF > 0 {
			mon.HP += hit.Damage / (mon.MP & 0xFF)
		}
		action := w.monsterActionLocked(mon, MonsterActionLighting)
		action.TargetActor = MonsterActorID(*target)
		action.TargetX = target.X
		action.TargetY = target.Y
		if hit.Damage > 0 {
			w.monsterBehaviorHits = append(w.monsterBehaviorHits, hit)
			w.enqueueReferenceMonsterStruckLocked(mon, target, now, 200*time.Millisecond)
		}
		return []MonsterAction{action}, nil, nil, nil
	}
	target, ok := players[mon.TargetCharacterID]
	if !ok || target.ID == "" {
		return nil, nil, nil, nil
	}
	nx := abs(mon.X - target.X)
	ny := abs(mon.Y - target.Y)
	if nx <= 2 && ny <= 2 {
		if !mon.UseMagic && nx != 2 && ny != 2 {
			return nil, nil, nil, nil
		}
		if now.Sub(mon.LastAttackAt) <= time.Duration(w.monsterAttackIntervalMSLocked(mon))*time.Millisecond {
			return nil, nil, nil, nil
		}
		mon.LastAttackAt = now
		mon.TargetFocusAt = now
		mon.Dir = direction(mon.X, mon.Y, target.X, target.Y)
		updated, hit, err := w.monsterReferenceMagicCharacterAtLocked(mon, target, now)
		if err != nil {
			return nil, nil, nil, err
		}
		hit.Magic = false
		hit.ImpactDelay = 200 * time.Millisecond
		if hit.Damage > 0 && mon.MP&0xFF > 0 {
			mon.HP += hit.Damage / (mon.MP & 0xFF)
		}
		action := w.monsterActionLocked(mon, MonsterActionLighting)
		action.TargetActor = CharacterActorID(target)
		action.TargetX = target.X
		action.TargetY = target.Y
		if hit.Damage <= 0 {
			return []MonsterAction{action}, nil, nil, nil
		}
		return []MonsterAction{action}, []CharacterHit{hit}, []storage.Character{updated}, nil
	}
	mon.TargetX, mon.TargetY = target.X, target.Y
	return nil, nil, nil, nil
}

func (w *World) tickExplosionSpiderLocked(mon *Monster, players map[string]storage.Character, now time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	return w.tickExplosionSpiderModeLocked(mon, players, now, true)
}

func (w *World) tickExplosionSpiderModeLocked(mon *Monster, players map[string]storage.Character, now time.Time, includeDetonation bool) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	if mon.ExplosionStartAt.IsZero() {
		mon.ExplosionStartAt = now
	}
	if includeDetonation && now.Sub(mon.ExplosionStartAt) > 60*time.Second {
		mon.ExplosionStartAt = now
		return w.explosionSpiderLocked(mon, players, now)
	}
	if mon.TargetCharacterID != "" {
		if target, ok := players[mon.TargetCharacterID]; ok {
			if target.MapID != mon.MapID {
				mon.TargetCharacterID = ""
				mon.TargetX, mon.TargetY = -1, -1
				mon.TargetFocusAt = time.Time{}
			} else if (mon.X != target.X || mon.Y != target.Y) && abs(mon.X-target.X) <= 1 && abs(mon.Y-target.Y) <= 1 {
				if now.Sub(mon.LastAttackAt) > time.Duration(w.monsterAttackIntervalMSLocked(mon))*time.Millisecond {
					mon.LastAttackAt = now
					mon.TargetFocusAt = now
					mon.Dir = direction(mon.X, mon.Y, target.X, target.Y)
					return w.explosionSpiderAttackLocked(mon, players, now)
				}
				return nil, nil, nil, nil
			} else {
				mon.TargetX, mon.TargetY = target.X, target.Y
				if w.moveMonsterTowardLocked(mon, target, direction(mon.X, mon.Y, target.X, target.Y), players) {
					mon.LastWalkAt = now
					return []MonsterAction{w.monsterActionLocked(mon, MonsterActionWalk)}, nil, nil, nil
				}
			}
		}
	}
	if mon.TargetMonsterID != "" {
		target := w.monsters[mon.TargetMonsterID]
		if target == nil || target.ID == "" || target.MapID != mon.MapID {
			mon.TargetMonsterID = ""
			mon.TargetX, mon.TargetY = -1, -1
			mon.TargetFocusAt = time.Time{}
		} else if (mon.X != target.X || mon.Y != target.Y) && abs(mon.X-target.X) <= 1 && abs(mon.Y-target.Y) <= 1 {
			if now.Sub(mon.LastAttackAt) > time.Duration(w.monsterAttackIntervalMSLocked(mon))*time.Millisecond {
				mon.LastAttackAt = now
				mon.TargetFocusAt = now
				mon.Dir = direction(mon.X, mon.Y, target.X, target.Y)
				return w.explosionSpiderAttackLocked(mon, players, now)
			}
			return nil, nil, nil, nil
		} else {
			mon.TargetX, mon.TargetY = target.X, target.Y
			if w.moveMonsterTowardPointLocked(mon, target.X, target.Y, players) {
				mon.LastWalkAt = now
				return []MonsterAction{w.monsterActionLocked(mon, MonsterActionWalk)}, nil, nil, nil
			}
		}
	}
	return nil, nil, nil, nil
}

func (w *World) tickSpiderHouseLocked(mon *Monster, players map[string]storage.Character, now time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	if now.Sub(mon.LastAttackAt) < time.Duration(w.monsterAttackIntervalMSLocked(mon))*time.Millisecond {
		return nil, nil, nil, nil
	}
	mon.LastAttackAt = now
	w.searchMonsterTargetLocked(mon, players, now)
	if mon.TargetMonsterID == "" && mon.TargetCharacterID == "" {
		return nil, nil, nil, nil
	}
	if w.monsterChildLimitCountLocked(mon) >= 15 {
		return nil, nil, nil, nil
	}
	childName := "爆裂蜘蛛"
	w.pendingMonsterSpawns = append(w.pendingMonsterSpawns, pendingMonsterSpawn{EnqueueOrder: w.nextPendingOrderLocked(), DueAt: now.Add(500 * time.Millisecond), ParentID: mon.ID, ChildName: childName})
	mon.TargetFocusAt = now
	return []MonsterAction{w.monsterActionLocked(mon, MonsterActionHit)}, nil, nil, nil
}

func (w *World) countMonsterChildrenLocked(parentID string) int {
	if parent := w.monsters[parentID]; parent != nil && len(parent.ChildIDs) > 0 {
		count := 0
		for childID := range parent.ChildIDs {
			child := w.monsters[childID]
			if child != nil && child.Alive && !child.Ghost {
				count++
			}
		}
		return count
	}
	count := 0
	for _, mon := range w.monsters {
		if mon.Alive && !mon.Ghost && mon.ParentID == parentID {
			count++
		}
	}
	return count
}

func (w *World) countMonsterGenerationEntriesLocked(parentID string) int {
	if parent := w.monsters[parentID]; parent != nil && len(parent.ChildIDs) > 0 {
		count := 0
		for childID := range parent.ChildIDs {
			child := w.monsters[childID]
			if child != nil && child.Alive && !child.Ghost {
				count++
			}
		}
		return count
	}
	count := 0
	for _, mon := range w.monsters {
		if mon.ParentID == parentID && mon.Alive && !mon.Ghost {
			count++
		}
	}
	return count
}

func (w *World) monsterChildLimitCountLocked(parent *Monster) int {
	if parent != nil && len(parent.ChildIDs) > 0 {
		return len(parent.ChildIDs)
	}
	if parent == nil {
		return 0
	}
	return w.countMonsterChildrenLocked(parent.ID)
}

func (w *World) cleanupTrackedMonsterChildrenLocked(parent *Monster) {
	if parent == nil || len(parent.ChildIDs) == 0 {
		return
	}
	for childID := range parent.ChildIDs {
		child := w.monsters[childID]
		if child == nil || !child.Alive || child.Ghost {
			delete(parent.ChildIDs, childID)
		}
	}
}

func (w *World) spawnChildMonsterLocked(parent *Monster, childName string, now time.Time) bool {
	_, ok := w.spawnChildMonsterWithResultLocked(parent, childName, now)
	return ok
}

func (w *World) spawnChildMonsterWithResultLocked(parent *Monster, childName string, now time.Time) (*Monster, bool) {
	childCount := w.monsterChildLimitCountLocked(parent)
	if childCount >= 15 {
		return nil, false
	}
	tpl, ok := w.monsterTemplateByIDLocked(childName)
	if !ok {
		return nil, false
	}
	childX, childY := parent.X, parent.Y+1
	if parent.Race == 103 {
		childX, childY = parent.X, parent.Y
	} else {
		mp, ok := w.data.Maps[parent.MapID]
		if !ok {
			return nil, false
		}
		if !mp.Walkable(childX, childY) {
			return nil, false
		}
	}
	child := w.createSpawnMonsterLocked(data.StdSpawn{MapID: parent.MapID, MonsterID: tpl.ID, X: childX, Y: childY, Count: 1, RespawnSeconds: 0}, tpl, childX, childY)
	if child == nil {
		return nil, false
	}
	if child.X != parent.X || child.Y != parent.Y {
		if _, occupied := w.occupied[monsterPosition{MapID: parent.MapID, X: childX, Y: childY}]; !occupied {
			w.occupyMonsterLocked(child)
		}
	}
	child.TargetCharacterID = parent.TargetCharacterID
	child.TargetMonsterID = parent.TargetMonsterID
	child.TargetFocusAt = now
	if parent.Race == 103 || parent.Race == 116 {
		if parent.ChildIDs == nil {
			parent.ChildIDs = make(map[string]struct{})
		}
		parent.ChildIDs[child.ID] = struct{}{}
	} else {
		child.ParentID = parent.ID
		if parent.ChildIDs == nil {
			parent.ChildIDs = make(map[string]struct{})
		}
		parent.ChildIDs[child.ID] = struct{}{}
	}
	return child, true
}

func (w *World) spawnChildMonsterAtLocked(parent *Monster, childName string, x, y int, now time.Time) bool {
	if w.countMonsterChildrenLocked(parent.ID) >= 15 {
		return false
	}
	tpl, ok := w.monsterTemplateByIDLocked(childName)
	if !ok {
		return false
	}
	if _, ok := w.data.Maps[parent.MapID]; !ok {
		return false
	}
	child := w.createSpawnMonsterLocked(data.StdSpawn{MapID: parent.MapID, MonsterID: tpl.ID, X: x, Y: y, Count: 1, RespawnSeconds: 0}, tpl, x, y)
	if child == nil {
		return false
	}
	if child.X != parent.X || child.Y != parent.Y {
		w.occupyMonsterLocked(child)
	}
	child.ParentID = parent.ID
	if parent.ChildIDs == nil {
		parent.ChildIDs = make(map[string]struct{})
	}
	parent.ChildIDs[child.ID] = struct{}{}
	child.TargetCharacterID = parent.TargetCharacterID
	child.TargetFocusAt = now
	return true
}
