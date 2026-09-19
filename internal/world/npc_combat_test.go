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

func TestNPCTrainingCollectsPhysicalAndMagicDamageUntilQuiet(t *testing.T) {
	store, err := storage.Open(t.TempDir() + "/test.json")
	if err != nil {
		t.Fatal(err)
	}
	bundle := data.StdBundle{}
	bundle.NPCs.Entities = map[string]npc.Entity{
		"trainer": {ID: "trainer", Name: "Trainer", Kind: npc.KindTrainer, MapID: "0"},
	}
	w := New(bundle, store)
	now := time.Unix(100, 0)
	physical, err := w.ApplyNPCTrainingHit("trainer", "player-1", 12, false, now)
	if err != nil || physical.Total != 12 || physical.Magic || !physical.HitAt.Equal(now) || physical.Attacker != "player-1" {
		t.Fatalf("physical hit = %+v, err = %v", physical, err)
	}
	magic, err := w.ApplyNPCTrainingHit("trainer", "player-1", 8, true, now.Add(time.Second))
	if err != nil || magic.Total != 20 || magic.HitCount != 2 || !magic.Magic {
		t.Fatalf("magic hit = %+v, err = %v", magic, err)
	}
	if _, ok := w.FlushNPCTraining("trainer", now.Add(3*time.Second)); ok {
		t.Fatal("training summary flushed before the quiet interval elapsed")
	}
	summary, ok := w.FlushNPCTraining("trainer", now.Add(5*time.Second))
	if !ok || summary.Total != 20 || summary.HitCount != 2 || !summary.Summary {
		t.Fatalf("summary = %+v, ok = %t", summary, ok)
	}
}

func TestTickFlushesNPCTrainingSummaryInOrder(t *testing.T) {
	store, err := storage.Open(t.TempDir() + "/test.json")
	if err != nil {
		t.Fatal(err)
	}
	bundle := data.StdBundle{}
	bundle.NPCs.Entities = map[string]npc.Entity{
		"trainer": {ID: "trainer", Name: "Trainer", Kind: npc.KindTrainer, MapID: "0"},
	}
	w := New(bundle, store)
	now := time.Unix(100, 0)
	if _, err := w.ApplyNPCTrainingHit("trainer", "player-1", 20, false, now); err != nil {
		t.Fatal(err)
	}
	if _, err := w.ApplyNPCTrainingHit("trainer", "player-1", 10, true, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	result, err := w.Tick(nil, now.Add(5*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.NPCTrainingHits) != 1 || !result.NPCTrainingHits[0].Summary || result.NPCTrainingHits[0].Total != 30 || result.NPCTrainingHits[0].HitCount != 2 {
		t.Fatalf("tick summary = %+v", result.NPCTrainingHits)
	}
	if len(result.OrderedSpellEvents) == 0 || result.OrderedSpellEvents[len(result.OrderedSpellEvents)-1].Kind != OrderedSpellEventNPCTraining {
		t.Fatalf("ordered tick events = %+v", result.OrderedSpellEvents)
	}
}

func TestNPCTrainingRejectsNonTrainerAndInvalidHits(t *testing.T) {
	store, err := storage.Open(t.TempDir() + "/test.json")
	if err != nil {
		t.Fatal(err)
	}
	bundle := data.StdBundle{}
	bundle.NPCs.Entities = map[string]npc.Entity{
		"normal":  {ID: "normal", Kind: npc.KindNormal, MapID: "0"},
		"hidden":  {ID: "hidden", Kind: npc.KindTrainer, MapID: "0", Hidden: true},
		"trainer": {ID: "trainer", Kind: npc.KindTrainer, MapID: "0"},
	}
	w := New(bundle, store)
	now := time.Unix(100, 0)
	for _, test := range []struct {
		name  string
		npcID string
		dmg   int
		want  bool
	}{
		{name: "ordinary npc", npcID: "normal", dmg: 10},
		{name: "hidden trainer", npcID: "hidden", dmg: 10},
		{name: "zero damage", npcID: "trainer", dmg: 0},
		{name: "unknown npc", npcID: "missing", dmg: 10},
	} {
		t.Run(test.name, func(t *testing.T) {
			hit, err := w.ApplyNPCTrainingHit(test.npcID, "player-1", test.dmg, false, now)
			if test.npcID == "missing" || test.npcID == "normal" {
				if err == nil {
					t.Fatal("expected invalid trainer error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if (hit.Damage > 0) != test.want {
				t.Fatalf("hit = %+v, want damage = %t", hit, test.want)
			}
		})
	}
}

func TestFireWallTickRecordsTrainerMagicHit(t *testing.T) {
	store, err := storage.Open(t.TempDir() + "/test.json")
	if err != nil {
		t.Fatal(err)
	}
	bundle := data.StdBundle{}
	bundle.NPCs.Entities = map[string]npc.Entity{
		"trainer": {ID: "trainer", Name: "Trainer", Kind: npc.KindTrainer, MapID: "0", X: 4, Y: 4},
	}
	now := time.Unix(100, 0)
	w := &World{
		data:         bundle,
		store:        store,
		npcTraining:  map[string]NPCTrainingState{},
		fireFields:   map[fireFieldKey]fireField{},
		groundEvents: map[int32]SpellGroundEvent{},
		monsters:     map[string]*Monster{},
		rand:         rand.New(rand.NewSource(1)),
	}
	w.fireFields[fireFieldKey{MapID: "0", X: 4, Y: 4}] = fireField{
		MapID: "0", X: 4, Y: 4, OwnerID: "player-1", Damage: 17,
		ExpiresAt: now.Add(time.Minute), NextTick: now.Add(-time.Second),
	}
	owner := storage.Character{ID: "player-1", MapID: "0", X: 3, Y: 4, HP: 100}
	_, _, _ = w.applyFireWallTickLocked(map[string]storage.Character{owner.ID: owner}, now)
	if len(w.pendingNPCTraining) != 1 {
		t.Fatalf("pending trainer hits = %d, want 1", len(w.pendingNPCTraining))
	}
	hit := w.pendingNPCTraining[0]
	if hit.Damage != 17 || !hit.Magic || hit.Attacker != owner.ID || !hit.HitAt.Equal(now) {
		t.Fatalf("trainer fire wall hit = %+v", hit)
	}
}

func TestPendingTrainerMagicDamageRechecksTargetRange(t *testing.T) {
	store, err := storage.Open(t.TempDir() + "/test.json")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(100, 0)
	makeWorld := func(x int) *World {
		bundle := data.StdBundle{}
		bundle.NPCs.Entities = map[string]npc.Entity{
			"trainer": {ID: "trainer", Name: "Trainer", Kind: npc.KindTrainer, MapID: "0", X: x, Y: 4},
		}
		bundle.Maps = map[string]data.StdMap{"0": {ID: "0", Width: 10, Height: 10}}
		return &World{
			data:        bundle,
			store:       store,
			npcTraining: map[string]NPCTrainingState{},
			monsters:    map[string]*Monster{},
			pendingSpells: []pendingSpell{{
				DueAt: now.Add(-time.Second), CasterID: "player-1", TargetNPCID: "trainer",
				TargetX: 4, TargetY: 4, TargetRange: 1, Damage: 13,
			}},
		}
	}
	caster := storage.Character{ID: "player-1", MapID: "0", X: 3, Y: 4, HP: 100}
	for _, test := range []struct {
		name       string
		trainerX   int
		wantEvents int
	}{
		{name: "target remains in range", trainerX: 4, wantEvents: 1},
		{name: "target moved out of range", trainerX: 6, wantEvents: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			w := makeWorld(test.trainerX)
			result := TickResult{}
			players := map[string]storage.Character{caster.ID: caster}
			if err := w.applyPendingSpellTicksLocked(&result, players, map[string]storage.Character{}, now); err != nil {
				t.Fatal(err)
			}
			if len(result.NPCTrainingHits) != test.wantEvents {
				t.Fatalf("training hits = %d, want %d", len(result.NPCTrainingHits), test.wantEvents)
			}
			ordered := 0
			for _, event := range result.OrderedSpellEvents {
				if event.Kind == OrderedSpellEventNPCTraining {
					ordered++
				}
			}
			if ordered != test.wantEvents {
				t.Fatalf("ordered trainer events = %d, want %d", ordered, test.wantEvents)
			}
		})
	}
}

func TestHitWithIdentRoutesMeleeDamageToTrainer(t *testing.T) {
	store, err := storage.Open(t.TempDir() + "/test.json")
	if err != nil {
		t.Fatal(err)
	}
	bundle := data.StdBundle{}
	bundle.Maps = map[string]data.StdMap{"0": {ID: "0", Width: 10, Height: 10}}
	bundle.NPCs.Entities = map[string]npc.Entity{
		"trainer": {ID: "trainer", Name: "Trainer", Kind: npc.KindTrainer, MapID: "0", X: 4, Y: 4},
	}
	character := storage.Character{ID: "player-1", Name: "Player", Class: "warrior", Level: 10, MapID: "0", X: 3, Y: 4, HP: 100, MaxHP: 100}
	if _, err := store.InsertCharacter(character); err != nil {
		t.Fatal(err)
	}
	w := New(bundle, store)
	w.rand = rand.New(rand.NewSource(1))
	result, err := w.HitWithIdent(character, character.X, character.Y, 2, mir176.CMHit)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.NPCTrainingHits) != 1 {
		t.Fatalf("trainer hits = %d, want 1", len(result.NPCTrainingHits))
	}
	hit := result.NPCTrainingHits[0]
	if hit.Damage <= 0 || hit.Magic || hit.Attacker != character.ID || hit.NPC.ID != "trainer" {
		t.Fatalf("trainer melee hit = %+v", hit)
	}
}

func TestHitWithIdentRejectsOrdinaryNPC(t *testing.T) {
	store, err := storage.Open(t.TempDir() + "/test.json")
	if err != nil {
		t.Fatal(err)
	}
	bundle := data.StdBundle{
		Maps: map[string]data.StdMap{"0": {ID: "0", Width: 10, Height: 10}},
		NPCs: npc.Library{Entities: map[string]npc.Entity{
			"ordinary": {ID: "ordinary", Name: "Ordinary", Kind: npc.KindNormal, MapID: "0", X: 4, Y: 4},
		}},
	}
	character := storage.Character{ID: "player-1", Class: "warrior", Level: 10, MapID: "0", X: 3, Y: 4, HP: 100, MaxHP: 100}
	if _, err := store.InsertCharacter(character); err != nil {
		t.Fatal(err)
	}
	w := New(bundle, store)
	w.rand = rand.New(rand.NewSource(1))
	result, err := w.HitWithIdent(character, character.X, character.Y, 2, mir176.CMHit)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.NPCTrainingHits) != 0 || result.MonsterID != "" || len(result.CharacterHits) != 0 {
		t.Fatalf("ordinary NPC was attacked: %+v", result)
	}
}

func TestDoSpellResolvesTrainerAsSingleTarget(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	caster.Skills = storage.SkillStates{{ID: "火球术", Level: 0}}
	caster.MP = 100
	trainer := npc.Entity{ID: "trainer", Name: "Trainer", Kind: npc.KindTrainer, MapID: caster.MapID, X: caster.X + 1, Y: caster.Y}
	w.mu.Lock()
	w.data.NPCs.Entities[trainer.ID] = trainer
	w.mu.Unlock()

	result, err := w.DoSpell(caster, "火球术", trainer.X, trainer.Y, w.NPCActorID(trainer.ID), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.TargetIDResolved || result.MagicTargetID != w.NPCActorID(trainer.ID) {
		t.Fatalf("trainer target result = %+v", result)
	}
	if len(w.pendingSpells) != 1 || w.pendingSpells[0].TargetNPCID != trainer.ID {
		t.Fatalf("pending trainer spell = %+v", w.pendingSpells)
	}
	tick, err := w.Tick([]PlayerSnapshot{{Character: result.Character}}, time.Now().Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if len(tick.NPCTrainingHits) != 1 || tick.NPCTrainingHits[0].Damage <= 0 || !tick.NPCTrainingHits[0].Magic {
		t.Fatalf("consumed trainer spell = %+v", tick.NPCTrainingHits)
	}
}

func TestDoChargeStrikesAndPushesTrainer(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	caster.Skills = storage.SkillStates{{ID: "野蛮冲撞", Level: 0}}
	caster.MP = 100
	trainer := npc.Entity{ID: "trainer", Name: "Trainer", Kind: npc.KindTrainer, MapID: caster.MapID, X: caster.X + 1, Y: caster.Y}
	w.mu.Lock()
	w.monsters = map[string]*Monster{}
	w.occupied = map[monsterPosition]string{}
	w.data.NPCs.Entities[trainer.ID] = trainer
	w.mu.Unlock()

	result, err := w.CastSkill(caster, "野蛮冲撞", trainer.X, trainer.Y, w.NPCActorID(trainer.ID))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.NPCTrainingHits) != 1 || result.NPCTrainingHits[0].Damage <= 0 {
		t.Fatalf("trainer charge hits = %+v", result.NPCTrainingHits)
	}
	if got := w.data.NPCs.Entities[trainer.ID]; got.X != trainer.X+1 || got.Y != trainer.Y {
		t.Fatalf("trainer position = (%d,%d), want (%d,%d)", got.X, got.Y, trainer.X+1, trainer.Y)
	}
}

func TestAreaAndLinearSpellRoutesToTrainer(t *testing.T) {
	w, caster := newTestWorldCharacter(t)
	w.mu.Lock()
	w.monsters = map[string]*Monster{}
	w.occupied = map[monsterPosition]string{}
	trainer := npc.Entity{ID: "trainer", Name: "Trainer", Kind: npc.KindTrainer, MapID: caster.MapID, X: caster.X + 1, Y: caster.Y}
	w.data.NPCs.Entities[trainer.ID] = trainer
	explosion := w.data.Skills["爆裂火焰"]
	linear := w.data.Skills["疾光电影"]
	state := storage.SkillState{ID: explosion.ID, Level: 0}
	result := SkillCastResult{}
	_, valid, err := w.castExplosionSkillLocked(&result, caster, explosion, state, trainer.X, trainer.Y, nil)
	if err != nil || !valid || len(result.NPCTrainingHits) != 1 {
		w.mu.Unlock()
		t.Fatalf("explosion trainer result = valid:%t hits:%+v err:%v", valid, result.NPCTrainingHits, err)
	}
	linearState := storage.SkillState{ID: linear.ID, Level: 0}
	linearResult := SkillCastResult{}
	_, hit, err := w.castLightningLineSkillLocked(&linearResult, caster, linear, linearState, nil, int32(trainer.X), int32(trainer.Y), 0, skillLightningRange, true, time.Now())
	w.mu.Unlock()
	if err != nil || !hit || len(linearResult.NPCTrainingHits) != 0 || len(w.pendingSpells) != 1 || w.pendingSpells[0].TargetNPCID != trainer.ID {
		t.Fatalf("linear trainer result = hit:%t hits:%+v pending:%+v err:%v", hit, linearResult.NPCTrainingHits, w.pendingSpells, err)
	}
}

func TestSpecialMonsterFlightChecksEveryPathTile(t *testing.T) {
	store, err := storage.Open(t.TempDir() + "/test.json")
	if err != nil {
		t.Fatal(err)
	}
	bundle := data.StdBundle{Maps: map[string]data.StdMap{
		"map": {ID: "map", Width: 10, Height: 10, Blocked: []data.StdPoint{{X: 2, Y: 4}}},
	}}
	w := New(bundle, store)
	mon := &Monster{MapID: "map", X: 2, Y: 2}
	blocked := storage.Character{MapID: "map", X: 2, Y: 6}
	if w.monsterCanFlyLocked(mon, blocked) {
		t.Fatal("special monster flight crossed a blocked intermediate tile")
	}
	clear := storage.Character{MapID: "map", X: 4, Y: 6}
	if !w.monsterCanFlyLocked(mon, clear) {
		t.Fatal("special monster flight rejected a clear path")
	}
}
