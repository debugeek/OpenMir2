package world

import (
	"math/rand"
	"testing"
	"time"

	"openmir2/internal/data"
	"openmir2/internal/npc"
	"openmir2/internal/protocol/mir176"
	"openmir2/internal/storage"
)

func TestFireWallClearsGhostOwnerAfterCurrentTick(t *testing.T) {
	w, target := newTestWorldCharacter(t)
	now := time.Unix(20, 0)
	owner := storage.Character{ID: "ghost-fire-owner", MapID: target.MapID, X: target.X + 3, Y: target.Y, HP: 100, MaxHP: 100, AttackMode: 2, Ghost: true}
	w.mu.Lock()
	w.fireFields[fireFieldKey{MapID: target.MapID, X: target.X, Y: target.Y}] = fireField{
		MapID: target.MapID, X: target.X, Y: target.Y, OwnerID: owner.ID, Owner: owner,
		Damage: 10, ExpiresAt: now.Add(time.Minute), NextTick: now.Add(-time.Second),
	}
	w.mu.Unlock()

	first, err := w.Tick([]PlayerSnapshot{{Character: owner}, {Character: target}}, now)
	if err != nil {
		t.Fatalf("first Tick() error = %v", err)
	}
	if len(first.CharacterHits) != 1 {
		t.Fatalf("first CharacterHits = %d, want 1", len(first.CharacterHits))
	}
	if _, ok := w.fireFields[fireFieldKey{MapID: target.MapID, X: target.X, Y: target.Y}]; !ok {
		t.Fatal("fire field disappeared after owner ghost cleanup")
	}

	second, err := w.Tick([]PlayerSnapshot{{Character: owner}, {Character: target}}, now.Add(fireWallTickInterval+time.Second))
	if err != nil {
		t.Fatalf("second Tick() error = %v", err)
	}
	if len(second.CharacterHits) != 0 {
		t.Fatalf("second CharacterHits = %d, want 0", len(second.CharacterHits))
	}
}

func TestFireWallStrikesTrainerThroughMineMessage(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	now := time.Unix(20, 0)
	trainer := npc.Entity{ID: "trainer", Kind: npc.KindTrainer, MapID: caster.MapID, X: caster.X + 1, Y: caster.Y}
	w.mu.Lock()
	w.gameplay.Combat.NonPKServer = true
	w.data.NPCs.Entities = map[string]npc.Entity{trainer.ID: trainer}
	w.fireFields[fireFieldKey{MapID: trainer.MapID, X: trainer.X, Y: trainer.Y}] = fireField{
		MapID: trainer.MapID, X: trainer.X, Y: trainer.Y, OwnerID: caster.ID, Owner: caster,
		Damage: 10, ExpiresAt: now.Add(time.Minute), NextTick: now.Add(-time.Second),
	}
	speeches := []NPCSpeech{}
	var hits []NPCStruckHit
	var events []NPCCombatEvent
	_, _, _, _, speeches, hits, events = w.applyFireWallTickLocked(map[string]storage.Character{caster.ID: caster}, now)
	w.mu.Unlock()
	if len(speeches) != 0 {
		t.Fatalf("firewall trainer speeches = %+v, want none for RM_MAGSTRUCK_MINE", speeches)
	}
	if len(w.pendingSpells) != 0 {
		t.Fatalf("firewall trainer pending spells = %+v, want none", w.pendingSpells)
	}
	if len(hits) != 0 {
		t.Fatalf("firewall trainer struck hits = %+v, want none for RM_MAGSTRUCK_MINE", hits)
	}
	if len(events) != 0 {
		t.Fatalf("firewall trainer combat events = %+v, want none for RM_MAGSTRUCK_MINE", events)
	}
	if state := w.npcCombat[trainer.ID]; state != nil {
		t.Fatalf("firewall trainer state = %+v, want no recorded strike", state)
	}
}

func TestNPCIdleActionsUseReferenceMerchantAndGuildOfficialBranches(t *testing.T) {
	w, _ := newTestWorldCharacter(t)
	w.mu.Lock()
	w.rand = rand.New(&seqSource{vals: []int64{0, 0, 0, 0, 0, 0}})
	w.data.NPCs.Entities = map[string]npc.Entity{
		"merchant": {ID: "merchant", Kind: npc.KindMerchant, MapID: testMapID, X: 10, Y: 10, Dir: 3},
		"guild":    {ID: "guild", GuildOfficial: true, MapID: testMapID, X: 11, Y: 10, Dir: 4},
	}
	w.npcNextRunAt["merchant"] = time.Time{}
	w.npcNextRunAt["guild"] = time.Time{}
	now := time.Now().Add(time.Second)
	_, actions := w.runMerchantsLocked(now)
	_, npcActions := w.runNPCsLocked(now)
	actions = append(actions, npcActions...)
	w.mu.Unlock()
	if len(actions) != 2 {
		t.Fatalf("NPC idle actions = %+v, want one turn for each official", actions)
	}
	for _, action := range actions {
		if action.Kind != NPCActionTurn || action.NPC.Dir != 0 {
			t.Fatalf("NPC idle action = %+v, want reference turn action with direction 0", action)
		}
	}
}

func TestCastleOfficialUsesReferenceWarHideAndRevealActions(t *testing.T) {
	w, _ := newTestWorldCharacter(t)
	w.mu.Lock()
	w.data.Castle.ID = "castle"
	w.data.NPCs.Entities = map[string]npc.Entity{
		"official": {ID: "official", CastleOfficial: true, MapID: testMapID, X: 10, Y: 10, Dir: 3},
	}
	w.castleRuntime["castle"] = storage.Castle{ID: "castle", UnderWar: true}
	w.rand = rand.New(&seqSource{vals: []int64{1 << 32, 1 << 32, 1 << 32, 1 << 32}})
	w.npcNextRunAt["official"] = time.Time{}
	_, hide := w.runMerchantsLocked(time.Now().Add(time.Second))
	if len(hide) != 1 || hide[0].Kind != NPCActionDisappear {
		t.Fatalf("castle war actions = %+v, want one disappear", hide)
	}
	if got := w.NPCsInMap(testMapID); len(got) != 0 {
		t.Fatalf("castle official during war = %+v, want hidden", got)
	}
	w.castleRuntime["castle"] = storage.Castle{ID: "castle"}
	w.npcNextRunAt["official"] = time.Time{}
	_, show := w.runMerchantsLocked(time.Now().Add(2 * time.Second))
	w.mu.Unlock()
	if len(show) != 1 || show[0].Kind != NPCActionReveal {
		t.Fatalf("castle peace actions = %+v, want one reveal", show)
	}
}

func TestNPCSchedulerUsesReferenceRunInterval(t *testing.T) {
	w, _ := newTestWorldCharacter(t)
	w.mu.Lock()
	w.data.NPCs.Entities = map[string]npc.Entity{
		"merchant": {ID: "merchant", Kind: npc.KindMerchant, MapID: testMapID, X: 10, Y: 10},
	}
	w.npcNextRunAt["merchant"] = time.Time{}
	w.rand = rand.New(&seqSource{vals: []int64{0, 0, 0, 0}})
	now := time.Unix(100, 0)
	_, first := w.runMerchantsLocked(now)
	_, early := w.runMerchantsLocked(now.Add(100 * time.Millisecond))
	w.npcNextRunAt["merchant"] = now.Add(250 * time.Millisecond)
	_, boundary := w.runMerchantsLocked(now.Add(251 * time.Millisecond))
	w.mu.Unlock()
	if len(first) != 1 || first[0].Kind != NPCActionTurn {
		t.Fatalf("first NPC run = %+v, want one turn", first)
	}
	if len(early) != 0 {
		t.Fatalf("early NPC run = %+v, want no action before 250ms", early)
	}
	if len(boundary) != 1 || boundary[0].Kind != NPCActionTurn {
		t.Fatalf("boundary NPC run = %+v, want one turn after 250ms", boundary)
	}
}

func TestHiddenNPCStillRunsReferenceScheduler(t *testing.T) {
	w, _ := newTestWorldCharacter(t)
	w.mu.Lock()
	w.data.NPCs.Entities = map[string]npc.Entity{
		"hidden-merchant": {ID: "hidden-merchant", Kind: npc.KindMerchant, Hidden: true, MapID: testMapID, X: 10, Y: 10},
	}
	w.npcNextRunAt["hidden-merchant"] = time.Time{}
	w.rand = rand.New(&seqSource{vals: []int64{0, 0}})
	_, actions := w.runMerchantsLocked(time.Unix(100, 0))
	w.mu.Unlock()
	if len(actions) != 1 || actions[0].Kind != NPCActionTurn {
		t.Fatalf("hidden merchant actions = %+v, want one scheduled turn", actions)
	}
}

func TestTrainerDelayedSpeechRunsWithNPCScheduler(t *testing.T) {
	w, _ := newTestWorldCharacter(t)
	w.mu.Lock()
	trainer := npc.Entity{ID: "trainer", Kind: npc.KindTrainer, MapID: testMapID, X: 10, Y: 10}
	w.data.NPCs.Entities = map[string]npc.Entity{trainer.ID: trainer}
	w.npcNextRunAt[trainer.ID] = time.Time{}
	w.npcCombat[trainer.ID] = &NPCCombatState{DamageSum: 30, HitCount: 2, LastHitAt: time.Unix(100, 0)}
	speeches, actions := w.runNPCsLocked(time.Unix(104, 0))
	w.mu.Unlock()
	if len(actions) != 0 {
		t.Fatalf("trainer actions = %+v, want none", actions)
	}
	if len(speeches) != 1 || speeches[0].Text != "总破坏力30, 平均破坏力15" {
		t.Fatalf("trainer speeches = %+v, want one delayed summary", speeches)
	}
	state := w.npcCombat[trainer.ID]
	if state.DamageSum != 0 || state.HitCount != 0 || !state.LastHitAt.IsZero() {
		t.Fatalf("trainer state after speech = %+v, want counters cleared", state)
	}
}

func TestTrainerStrikeUsesReferenceMovingObjectInput(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	ch.EquippedItems = map[int]storage.UserItem{
		SlotWeapon: {ItemID: testWeaponID, Dura: 1000, DuraMax: 1000},
	}
	trainer := npc.Entity{ID: "trainer", Kind: npc.KindTrainer, MapID: ch.MapID, X: ch.X + 1, Y: ch.Y}
	w.mu.Lock()
	w.rand = rand.New(zeroSource{})
	w.gameplay.Combat.NonPKServer = true
	w.data.NPCs.Entities = map[string]npc.Entity{trainer.ID: trainer}
	w.mu.Unlock()

	result, err := w.HitWithIdent(ch, ch.X, ch.Y, 2, mir176.CMHit)
	if err != nil {
		t.Fatalf("HitWithIdent() error = %v", err)
	}
	if !result.Connected || result.Damage <= 0 {
		t.Fatalf("trainer result = %+v, want connected positive strike", result)
	}
	if len(result.NPCSpeeches) != 1 || result.NPCSpeeches[0].Text == "" {
		t.Fatalf("trainer speeches = %+v, want immediate strike speech", result.NPCSpeeches)
	}
	if len(result.NPCStruckHits) != 1 || result.NPCStruckHits[0].NPC.ID != trainer.ID || result.NPCStruckHits[0].Damage != result.Damage || result.NPCStruckHits[0].Magic {
		t.Fatalf("trainer struck hits = %+v, want one physical strike frame", result.NPCStruckHits)
	}
	state := w.npcCombat[trainer.ID]
	if state == nil || state.HitCount != 1 || state.DamageSum != result.Damage {
		t.Fatalf("trainer state = %+v, want one recorded strike", state)
	}
	if got := result.Character.EquippedItems[SlotWeapon].Dura; got != 998 {
		t.Fatalf("trainer strike weapon durability = %d, want 998", got)
	}
}

func TestTrainerLongHitContinuesThroughPrimaryTarget(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	ch.Skills = storage.SkillStates{{ID: "刺杀剑术", Level: 0}}
	ch.EquippedItems = map[int]storage.UserItem{
		SlotWeapon: {ItemID: testWeaponID, Dura: 1000, DuraMax: 1000},
	}
	farTrainer := npc.Entity{ID: "far-trainer", Kind: npc.KindTrainer, MapID: ch.MapID, X: ch.X + 2, Y: ch.Y}
	nearTrainer := npc.Entity{ID: "near-trainer", Kind: npc.KindTrainer, MapID: ch.MapID, X: ch.X + 1, Y: ch.Y}
	w.mu.Lock()
	w.gameplay.Combat.NonPKServer = true
	w.data.NPCs.Entities = map[string]npc.Entity{farTrainer.ID: farTrainer, nearTrainer.ID: nearTrainer}
	w.monsters = map[string]*Monster{}
	w.monsterCells = map[monsterPosition]map[string]struct{}{}
	w.monsterCellByID = map[string]monsterPosition{}
	w.rand = rand.New(zeroSource{})
	if target := w.movingObjectAtPointLocked(nil, ch.MapID, farTrainer.X, farTrainer.Y); target.Trainer == nil || target.Trainer.ID != farTrainer.ID {
		t.Fatalf("far trainer lookup = %+v, want %s", target, farTrainer.ID)
	}
	w.mu.Unlock()

	result, err := w.HitWithIdent(ch, ch.X, ch.Y, 2, mir176.CMLongHit)
	if err != nil {
		t.Fatalf("HitWithIdent() error = %v", err)
	}
	if len(result.NPCStruckHits) != 2 || result.NPCStruckHits[0].NPC.ID != farTrainer.ID || result.NPCStruckHits[1].NPC.ID != nearTrainer.ID {
		t.Fatalf("trainer long-hit targets = %+v, want far then primary trainer", result.NPCStruckHits)
	}
	if got := result.Character.EquippedItems[SlotWeapon].Dura; got != 998 {
		t.Fatalf("trainer long-hit weapon durability = %d, want one reference loss", got)
	}
}

func TestTrainerMagicStrikeUsesReferenceRMStruckChain(t *testing.T) {
	w, _ := newTestWorldCharacter(t)
	trainer := npc.Entity{ID: "trainer", Kind: npc.KindTrainer, MapID: testMapID, X: 10, Y: 10}
	w.mu.Lock()
	w.gameplay.Combat.NonPKServer = true
	w.data.NPCs.Entities = map[string]npc.Entity{trainer.ID: trainer}
	w.pendingSpells = []pendingSpell{{DueAt: time.Unix(100, 0), TargetTrainerID: trainer.ID, Damage: 12}}
	result := TickResult{}
	w.applyPendingTrainerSpellsLocked(&result, time.Unix(101, 0))
	w.mu.Unlock()
	if len(result.NPCSpeeches) != 1 || result.NPCSpeeches[0].Text != "破坏力12, 平均值12" {
		t.Fatalf("trainer magic speeches = %+v, want one reference magic strike speech", result.NPCSpeeches)
	}
	if len(result.NPCStruckHits) != 1 || result.NPCStruckHits[0].NPC.ID != trainer.ID || result.NPCStruckHits[0].Damage != 12 || !result.NPCStruckHits[0].Magic {
		t.Fatalf("trainer magic struck hits = %+v, want one magic struck event", result.NPCStruckHits)
	}
	if len(result.NPCCombatEvents) != 1 || !result.NPCCombatEvents[0].HasSpeech || !result.NPCCombatEvents[0].HasStruck || !result.NPCCombatEvents[0].Struck.Magic {
		t.Fatalf("trainer magic combat events = %+v, want one magic event", result.NPCCombatEvents)
	}
	if state := w.npcCombat[trainer.ID]; state == nil || state.DamageSum != 12 || state.HitCount != 1 {
		t.Fatalf("trainer immediate magic state = %+v, want one recorded strike", state)
	}
}

func TestTrainerDelayedMagicDoesNotApplyReferenceAnimalReduction(t *testing.T) {
	w, _ := newTestWorldCharacter(t)
	trainer := npc.Entity{ID: "trainer", Kind: npc.KindTrainer, MapID: testMapID, X: 10, Y: 10}
	w.mu.Lock()
	w.data.NPCs.Entities = map[string]npc.Entity{trainer.ID: trainer}
	w.pendingSpells = []pendingSpell{{DueAt: time.Unix(100, 0), TargetTrainerID: trainer.ID, TargetX: 10, TargetY: 10, TargetRange: 1, Damage: 12}}

	w.applyPendingTrainerSpellsLocked(&TickResult{}, time.Unix(101, 0))
	w.mu.Unlock()
	if state := w.npcCombat[trainer.ID]; state == nil || state.DamageSum != 12 || state.HitCount != 1 {
		t.Fatalf("trainer delayed magic state = %+v, want one reference magic strike", state)
	}
}

func TestTrainerDelayedMagicDropsMovedTarget(t *testing.T) {
	w, _ := newTestWorldCharacter(t)
	trainer := npc.Entity{ID: "trainer", Kind: npc.KindTrainer, MapID: testMapID, X: 10, Y: 10}
	w.mu.Lock()
	w.data.NPCs.Entities = map[string]npc.Entity{trainer.ID: trainer}
	w.pendingSpells = []pendingSpell{{DueAt: time.Unix(100, 0), TargetTrainerID: trainer.ID, TargetX: 10, TargetY: 10, TargetRange: 1, Damage: 12}}
	moved := trainer
	moved.X = 12
	w.data.NPCs.Entities[trainer.ID] = moved
	result := TickResult{}
	w.applyPendingTrainerSpellsLocked(&result, time.Unix(101, 0))
	w.mu.Unlock()
	if len(result.NPCSpeeches) != 0 || w.npcCombat[trainer.ID] != nil {
		t.Fatalf("moved trainer delayed magic = speeches:%+v state:%+v, want dropped", result.NPCSpeeches, w.npcCombat[trainer.ID])
	}
}

func TestTrainerDirectSpellUsesReferenceTargetChain(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	caster.Class = "wizard"
	caster.MP = 100
	caster.Skills = storage.SkillStates{{ID: "火球术", Level: 0}}
	trainer := npc.Entity{ID: "trainer", Kind: npc.KindTrainer, MapID: caster.MapID, X: caster.X + 1, Y: caster.Y + 1}
	w.mu.Lock()
	w.gameplay.Combat.NonPKServer = true
	w.data.NPCs.Entities = map[string]npc.Entity{trainer.ID: trainer}
	w.mu.Unlock()

	cast, err := w.DoSpell(caster, "火球术", trainer.X, trainer.Y, 0, nil)
	if err != nil {
		t.Fatalf("DoSpell() error = %v", err)
	}
	if cast.MagicTargetID == 0 || len(w.pendingSpells) != 1 || w.pendingSpells[0].TargetTrainerID != trainer.ID {
		t.Fatalf("trainer direct spell = %+v pending=%+v, want trainer target", cast, w.pendingSpells)
	}
	result := TickResult{}
	w.mu.Lock()
	w.applyPendingTrainerSpellsLocked(&result, time.Now().Add(time.Second))
	w.mu.Unlock()
	if len(result.NPCSpeeches) != 1 {
		t.Fatalf("trainer direct spell speeches = %+v, want one magic strike speech", result.NPCSpeeches)
	}
}

func TestTrainerPlayerTargetRequiresReferenceNonPKMode(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	caster.Class = "wizard"
	caster.MP = 100
	caster.Skills = storage.SkillStates{{ID: "火球术", Level: 0}}
	trainer := npc.Entity{ID: "trainer", Kind: npc.KindTrainer, MapID: caster.MapID, X: caster.X + 1, Y: caster.Y + 1}
	w.mu.Lock()
	w.data.NPCs.Entities = map[string]npc.Entity{trainer.ID: trainer}
	w.mu.Unlock()

	cast, err := w.DoSpell(caster, "火球术", trainer.X, trainer.Y, 0, nil)
	if err != nil {
		t.Fatalf("DoSpell() error = %v", err)
	}
	if cast.MagicTargetID != 0 || len(w.pendingSpells) != 0 {
		t.Fatalf("trainer target in normal mode = %+v pending=%+v, want no target", cast, w.pendingSpells)
	}
}

func TestTrainerSpellTargetRequiresReferenceExactCoordinate(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	caster.Class = "wizard"
	caster.MP = 100
	caster.Skills = storage.SkillStates{{ID: "火球术", Level: 0}}
	trainer := npc.Entity{ID: "trainer", Kind: npc.KindTrainer, MapID: caster.MapID, X: caster.X + 1, Y: caster.Y}
	w.mu.Lock()
	w.gameplay.Combat.NonPKServer = true
	w.data.NPCs.Entities = map[string]npc.Entity{trainer.ID: trainer}
	w.mu.Unlock()

	cast, err := w.DoSpell(caster, "火球术", trainer.X+1, trainer.Y, 0, nil)
	if err != nil {
		t.Fatalf("DoSpell() error = %v", err)
	}
	if cast.MagicTargetID != 0 || len(w.pendingSpells) != 0 {
		t.Fatalf("adjacent trainer target = %+v pending=%+v, want no target", cast, w.pendingSpells)
	}
}

func TestMonsterTargetSelectionSkipsTrainerObjects(t *testing.T) {
	w := &World{
		monsters: map[string]*Monster{
			"monster": {ID: "monster", MapID: testMapID, X: 10, Y: 10, Alive: true, ObjectOrder: 2},
		},
		data: data.StdBundle{NPCs: npc.Library{Entities: map[string]npc.Entity{
			"trainer": {ID: "trainer", Kind: npc.KindTrainer, MapID: testMapID, X: 10, Y: 10, ReferenceOrder: 1},
		}}},
	}
	selected := w.movingObjectAtPointForMonsterLocked(nil, testMapID, 10, 10)
	if selected.Trainer != nil || selected.Monster == nil || selected.Monster.ID != "monster" {
		t.Fatalf("monster target selection = %+v, want monster without trainer", selected)
	}
}

func TestSpecialMonsterFlightMatchesReferenceFirstTileCheck(t *testing.T) {
	store, err := storage.Open(t.TempDir() + "/test.json")
	if err != nil {
		t.Fatal(err)
	}
	bundle := data.StdBundle{Maps: map[string]data.StdMap{
		"map": {ID: "map", Width: 10, Height: 10, Blocked: []data.StdPoint{{X: 2, Y: 4}}},
	}}
	w := New(bundle, store)
	mon := &Monster{MapID: "map", X: 2, Y: 2}
	blocked := storage.Character{MapID: "map", X: 4, Y: 20}
	if w.monsterCanFlyLocked(mon, blocked) {
		t.Fatal("special monster flight crossed a blocked intermediate tile")
	}
	clear := storage.Character{MapID: "map", X: 4, Y: 30}
	if !w.monsterCanFlyLocked(mon, clear) {
		t.Fatal("special monster flight rejected a clear path")
	}
}
