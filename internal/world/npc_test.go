package world

import (
	"path/filepath"
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
	if got := w.NPCLabelSelection("@main"); got != "@main" {
		t.Fatalf("NPCLabelSelection(@main) = %q, want @main", got)
	}
}
