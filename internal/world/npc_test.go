package world

import (
	"path/filepath"
	"strings"
	"testing"

	"openmir2/internal/data"
	"openmir2/internal/npc"
	"openmir2/internal/storage"
)

func TestWorldNPCQueries(t *testing.T) {
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	bundle := data.StdBundle{
		Maps: map[string]data.StdMap{
			"0": {
				ID:     "0",
				Width:  100,
				Height: 100,
			},
		},
		NPCs: npc.Library{
			Entities: map[string]npc.Entity{
				"guide": {
					ID:       "guide",
					Name:     "Guide",
					Kind:     npc.KindMerchant,
					MapID:    "0",
					X:        11,
					Y:        12,
					Dir:      2,
					ScriptID: "guide_script",
				},
			},
			Scripts: map[string]npc.Script{
				"guide_script": {
					ID: "guide_script",
					Labels: map[string]npc.Label{
						"@main": {
							Name: "@main",
						},
					},
				},
			},
		},
	}
	w := New(bundle, store)
	entity, ok := w.NPCByID("guide")
	if !ok {
		t.Fatal("NPCByID() did not find guide")
	}
	if entity.Name != "Guide" {
		t.Fatalf("NPCByID() = %+v, want Guide", entity)
	}
	inMap := w.NPCsInMap("0")
	if len(inMap) != 1 || inMap[0].ID != "guide" {
		t.Fatalf("NPCsInMap() = %+v, want one guide", inMap)
	}
	script := w.data.NPCs.Scripts["guide_script"]
	if script.ID != "guide_script" {
		t.Fatalf("script = %+v, want guide_script", script)
	}
}

func TestNPCConversationUsesCastleOwnerAndLord(t *testing.T) {
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := store.SaveGuild(storage.Guild{ID: "guild-a", Ranks: []storage.GuildRank{{Number: 1, Members: []string{"城主甲"}}}}); err != nil {
		t.Fatalf("SaveGuild() error = %v", err)
	}
	w := New(data.StdBundle{
		Castle: data.CastleConfig{ID: "castle", PalaceMap: "0150"},
		NPCs: npc.Library{
			Entities: map[string]npc.Entity{"castle-official": {ID: "castle-official", MapID: "0150", CastleOfficial: true, ScriptID: "castle-script"}},
			Scripts: map[string]npc.Script{"castle-script": {ID: "castle-script", Labels: map[string]npc.Label{
				"@main": {Name: "@main", Procedures: []npc.Procedure{{Say: "<$OWNERGUILD>/<$LORD>/<$CASTLEDOORSTATE>"}}},
			}}},
		},
	}, store)
	w.RestoreCastleRuntime(storage.Castle{ID: "castle", OwnerGuildID: "guild-a", MainDoorHP: 0, MainDoorOpen: true})
	conversation, ok := w.NPCConversation(storage.Character{}, "castle-official", "@main")
	if !ok {
		t.Fatal("NPCConversation() returned ok=false")
	}
	if conversation.Text != "guild-a/城主甲/损坏" {
		t.Fatalf("conversation text = %q, want owner guild and lord context", conversation.Text)
	}
}

func TestNPCConversationUsesFirstGuildRankAsReferenceLord(t *testing.T) {
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := store.SaveGuild(storage.Guild{ID: "guild-a", Ranks: []storage.GuildRank{
		{Number: 2, Members: []string{"首个职位成员"}},
		{Number: 1, Members: []string{"会长职位成员"}},
	}}); err != nil {
		t.Fatalf("SaveGuild() error = %v", err)
	}
	w := New(data.StdBundle{
		Castle: data.CastleConfig{ID: "castle", PalaceMap: "0150"},
		NPCs: npc.Library{
			Entities: map[string]npc.Entity{"castle-official": {ID: "castle-official", MapID: "0150", CastleOfficial: true, ScriptID: "castle-script"}},
			Scripts: map[string]npc.Script{"castle-script": {ID: "castle-script", Labels: map[string]npc.Label{
				"@main": {Name: "@main", Procedures: []npc.Procedure{{Say: "<$LORD>"}}},
			}}},
		},
	}, store)
	w.RestoreCastleRuntime(storage.Castle{ID: "castle", OwnerGuildID: "guild-a"})
	conversation, ok := w.NPCConversation(storage.Character{}, "castle-official", "@main")
	if !ok || conversation.Text != "首个职位成员" {
		t.Fatalf("conversation = %+v, want first guild rank member", conversation)
	}
}

func TestNPCConversationUsesReferenceDefaultUserWeaponText(t *testing.T) {
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	w := New(data.StdBundle{
		NPCs: npc.Library{
			Entities: map[string]npc.Entity{
				"weapon-upgrader": {ID: "weapon-upgrader", MapID: "0", ScriptID: "weapon-script"},
			},
			Scripts: map[string]npc.Script{
				"weapon-script": {ID: "weapon-script", Labels: map[string]npc.Label{
					"@main": {Name: "@main", Procedures: []npc.Procedure{{Say: "<$USERWEAPON>"}}},
				}},
			},
		},
	}, store)
	conversation, ok := w.NPCConversation(storage.Character{}, "weapon-upgrader", "@main")
	if !ok || conversation.Text != "Weapon" {
		t.Fatalf("conversation = %+v, want default Weapon text", conversation)
	}
}

func TestNPCConversationDoesNotInjectCastleContextOutsideCastleArea(t *testing.T) {
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	w := New(data.StdBundle{
		Castle: data.CastleConfig{ID: "castle", CastleMap: "3", PalaceMap: "0150", SecretMap: "D701", HomeX: 644, HomeY: 290, WarRangeX: 100, WarRangeY: 100},
		NPCs: npc.Library{
			Entities: map[string]npc.Entity{"guide": {ID: "guide", MapID: "0", X: 10, Y: 10, CastleOfficial: true, ScriptID: "guide-script"}},
			Scripts: map[string]npc.Script{"guide-script": {ID: "guide-script", Labels: map[string]npc.Label{
				"@main": {Name: "@main", Procedures: []npc.Procedure{{Say: "<$OWNERGUILD>/<$LORD>/<$CASTLEDOORSTATE>"}}},
			}}},
		},
	}, store)
	w.RestoreCastleRuntime(storage.Castle{ID: "castle", OwnerGuildID: "guild-a", MainDoorHP: 2000, MainDoorOpen: true})
	conversation, ok := w.NPCConversation(storage.Character{}, "guide", "@main")
	if !ok {
		t.Fatal("NPCConversation() returned ok=false")
	}
	if conversation.Text != "????/????/????" {
		t.Fatalf("conversation text = %q, want no castle context", conversation.Text)
	}
}

func TestNPCConversationInjectsCastleContextForReferenceCastleMaps(t *testing.T) {
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	w := New(data.StdBundle{
		Castle: data.CastleConfig{ID: "castle", PalaceMap: "0150", SecretMap: "D701", WarMaps: []string{"0151"}},
		NPCs: npc.Library{
			Entities: map[string]npc.Entity{"castle-shop": {ID: "castle-shop", MapID: "0151", CastleOfficial: true, ScriptID: "castle-script"}},
			Scripts: map[string]npc.Script{"castle-script": {ID: "castle-script", Labels: map[string]npc.Label{
				"@main": {Name: "@main", Procedures: []npc.Procedure{{Say: "<$OWNERGUILD>/<$LORD>"}}},
			}}},
		},
	}, store)
	w.RestoreCastleRuntime(storage.Castle{ID: "castle", OwnerGuildID: "guild-a", MainDoorHP: 2000})

	conversation, ok := w.NPCConversation(storage.Character{}, "castle-shop", "@main")
	if !ok {
		t.Fatal("NPCConversation() returned ok=false")
	}
	if !strings.Contains(conversation.Text, "guild-a") || !strings.Contains(conversation.Text, "管理员") {
		t.Fatalf("conversation text = %q, want castle context on reference castle map", conversation.Text)
	}
}

func TestNPCConversationInitializesReferenceOpenCastleDoor(t *testing.T) {
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	w := New(data.StdBundle{
		Castle: data.CastleConfig{ID: "castle", PalaceMap: "0150", WarMaps: []string{"0151"}, MainDoor: data.CastleDefenseSlot{HP: 2000}},
		NPCs: npc.Library{
			Entities: map[string]npc.Entity{"castle-shop": {ID: "castle-shop", MapID: "0151", CastleOfficial: true, ScriptID: "castle-script"}},
			Scripts:  map[string]npc.Script{"castle-script": {ID: "castle-script", Labels: map[string]npc.Label{"@main": {Name: "@main", Procedures: []npc.Procedure{{Say: "<$CASTLEDOORSTATE>"}}}}}},
		},
	}, store)

	conversation, ok := w.NPCConversation(storage.Character{}, "castle-shop", "@main")
	if !ok || conversation.Text != "打开" {
		t.Fatalf("conversation = %+v, want initially open castle door", conversation)
	}
	castle, ok := store.Castle("castle")
	if !ok || !castle.MainDoorOpen {
		t.Fatalf("initialized castle = %+v, want open main door", castle)
	}
}

func TestWorldNPCsInMapExcludesHiddenNPCs(t *testing.T) {
	w := New(data.StdBundle{NPCs: npc.Library{Entities: map[string]npc.Entity{
		"visible": {ID: "visible", MapID: "0"},
		"hidden":  {ID: "hidden", MapID: "0", Hidden: true},
	}}}, nil)
	entities := w.NPCsInMap("0")
	if len(entities) != 1 || entities[0].ID != "visible" {
		t.Fatalf("NPCsInMap() = %+v, want only visible NPC", entities)
	}
}

func TestOccupiedActorsIncludesVisibleNonTrainerNPCs(t *testing.T) {
	w := New(data.StdBundle{NPCs: npc.Library{Entities: map[string]npc.Entity{
		"merchant": {ID: "merchant", Kind: npc.KindMerchant, MapID: "0", X: 4, Y: 5},
		"hidden":   {ID: "hidden", Kind: npc.KindMerchant, MapID: "0", X: 6, Y: 5, Hidden: true},
	}}}, nil)
	occupied := w.occupiedActorsLocked(nil)
	if got := occupied[monsterPosition{MapID: "0", X: 4, Y: 5}]; got != "merchant" {
		t.Fatalf("visible merchant occupant = %q, want merchant", got)
	}
	if _, ok := occupied[monsterPosition{MapID: "0", X: 6, Y: 5}]; ok {
		t.Fatal("hidden merchant occupied a movement tile")
	}
}

func TestWorldNPCsInMapUsesReferenceMapScanOrder(t *testing.T) {
	w := New(data.StdBundle{NPCs: npc.Library{Entities: map[string]npc.Entity{
		"later-cell":      {ID: "later-cell", MapID: "0", X: 11, Y: 1},
		"first-cell":      {ID: "first-cell", MapID: "0", X: 10, Y: 2},
		"same-cell-later": {ID: "same-cell-later", MapID: "0", X: 10, Y: 2},
	}}}, nil)
	got := w.NPCsInMap("0")
	if len(got) != 3 {
		t.Fatalf("NPCsInMap() length = %d, want 3", len(got))
	}
	want := []string{"first-cell", "same-cell-later", "later-cell"}
	for i, entity := range got {
		if entity.ID != want[i] {
			t.Fatalf("NPCsInMap() order[%d] = %q, want %q", i, entity.ID, want[i])
		}
	}
}

func TestWorldNPCsInMapUsesReferenceOrderWithinSameCell(t *testing.T) {
	w := New(data.StdBundle{NPCs: npc.Library{Entities: map[string]npc.Entity{
		"fixed":    {ID: "fixed", MapID: "0", X: 10, Y: 10, ReferenceOrder: 143},
		"merchant": {ID: "merchant", MapID: "0", X: 10, Y: 10, ReferenceOrder: 12},
		"unmapped": {ID: "unmapped", MapID: "0", X: 10, Y: 10},
	}}}, nil)
	got := w.NPCsInMap("0")
	want := []string{"merchant", "fixed", "unmapped"}
	for i, entity := range got {
		if entity.ID != want[i] {
			t.Fatalf("NPCsInMap() order[%d] = %q, want %q", i, entity.ID, want[i])
		}
	}
}

func TestNPCLabelSelectionNormalizesSpecialPrefixes(t *testing.T) {
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	w := New(data.StdBundle{}, store)
	if got := w.NPCLabelSelection("@@guildwar"); got != "@@guildwar" {
		t.Fatalf("NPCLabelSelection(@@guildwar) = %q, want @@guildwar", got)
	}
	if got := w.NPCLabelSelection("@@withdrawal"); got != "@@withdrawal" {
		t.Fatalf("NPCLabelSelection(@@withdrawal) = %q, want @@withdrawal", got)
	}
	if got := w.NPCLabelSelection("@@receipts"); got != "@@receipts" {
		t.Fatalf("NPCLabelSelection(@@receipts) = %q, want @@receipts", got)
	}
	if got := w.NPCLabelSelection("@@dealgold"); got != "@@dealgold" {
		t.Fatalf("NPCLabelSelection(@@dealgold) = %q, want @@dealgold", got)
	}
	if got := w.NPCLabelSelection("@@InPutString3"); got != "@@InPutString3" {
		t.Fatalf("NPCLabelSelection(@@InPutString3) = %q, want @@InPutString3", got)
	}
	if got := w.NPCLabelSelection("@@unknown"); got != "@@unknown" {
		t.Fatalf("NPCLabelSelection(@@unknown) = %q, want @@unknown", got)
	}
	if got := w.NPCLabelSelection("@main"); got != "@main" {
		t.Fatalf("NPCLabelSelection(@main) = %q, want @main", got)
	}
}
