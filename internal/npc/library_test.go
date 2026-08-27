package npc

import (
	"os"
	"path/filepath"
	"testing"
)

func writeJSON(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

func TestLoadLibraryReadsEntitiesAndScripts(t *testing.T) {
	dir := t.TempDir()
	writeJSON(t, filepath.Join(dir, "0", "guide.json"), `{
  "id": "guide",
  "name": "Guide",
  "kind": "merchant",
  "map_id": "0",
  "x": 10,
  "y": 11,
  "dir": 2,
  "merchant": {
    "price_rate": 120,
    "capabilities": {
      "buy": true,
      "sell": true
    },
    "item_types": ["consumable"],
    "stock": [
      {"item_id": "wood_sword", "count": 3}
    ]
  },
  "labels": {
    "@main": {
      "name": "@main",
      "ext_jump": true,
      "procedures": [
        {
          "say": "Welcome",
          "conditions": [{"op": "check_level", "args": ["1"]}],
          "actions": [{"op": "close"}]
        }
      ]
    }
  }
}`)

	lib, err := Load(dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	entity, ok := lib.Entities["guide"]
	if !ok {
		t.Fatal("expected guide entity")
	}
	if entity.Kind != KindMerchant {
		t.Fatalf("entity kind = %q, want %q", entity.Kind, KindMerchant)
	}
	if entity.Merchant.PriceRate != 120 {
		t.Fatalf("entity merchant price rate = %d, want 120", entity.Merchant.PriceRate)
	}
	if entity.ScriptID != "guide" {
		t.Fatalf("entity script id = %q, want guide", entity.ScriptID)
	}
	script, ok := lib.Scripts["guide"]
	if !ok {
		t.Fatal("expected guide script")
	}
	label, ok := script.Labels["@main"]
	if !ok {
		t.Fatal("expected @main label")
	}
	if !label.ExtJump || len(label.Procedures) != 1 {
		t.Fatalf("label = %+v", label)
	}
}

func TestConversationRendersDialogue(t *testing.T) {
	lib := Library{
		Entities: map[string]Entity{
			"guide": {
				ID:       "guide",
				Name:     "Guide",
				Kind:     KindMerchant,
				MapID:    "0",
				X:        1,
				Y:        2,
				Dir:      2,
				ScriptID: "guide_script",
			},
		},
		Scripts: map[string]Script{
			"guide_script": {
				ID: "guide_script",
				Labels: map[string]Label{
					"@main": {
						Name: "@main",
						Procedures: []Procedure{
							{Say: "Hello"},
						},
					},
				},
			},
		},
	}
	conversation, ok := lib.Conversation("guide", "@main", Context{})
	if !ok {
		t.Fatal("Conversation() returned ok=false")
	}
	if conversation.Text != "Hello" {
		t.Fatalf("Conversation() text = %q, want Hello", conversation.Text)
	}
}

func TestConversationRendersLineBreakControl(t *testing.T) {
	lib := Library{
		Entities: map[string]Entity{
			"guide": {
				ID:       "guide",
				Name:     "Guide",
				Kind:     KindMerchant,
				MapID:    "0",
				X:        1,
				Y:        2,
				Dir:      2,
				ScriptID: "guide_script",
			},
		},
		Scripts: map[string]Script{
			"guide_script": {
				ID: "guide_script",
				Labels: map[string]Label{
					"@main": {
						Name: "@main",
						Procedures: []Procedure{
							{Say: "Hello\\n<返回/@main>"},
						},
					},
				},
			},
		},
	}
	conversation, ok := lib.Conversation("guide", "@main", Context{})
	if !ok {
		t.Fatal("Conversation() returned ok=false")
	}
	if conversation.Text != "Hello\\<返回/@main>" {
		t.Fatalf("Conversation() text = %q, want line-break control", conversation.Text)
	}
}

func TestConversationFallsBackToMainAlias(t *testing.T) {
	lib := Library{
		Entities: map[string]Entity{
			"guide": {
				ID:       "guide",
				Name:     "Guide",
				Kind:     KindMerchant,
				MapID:    "0",
				X:        1,
				Y:        2,
				Dir:      2,
				ScriptID: "guide_script",
			},
		},
		Scripts: map[string]Script{
			"guide_script": {
				ID: "guide_script",
				Labels: map[string]Label{
					"@main": {
						Name: "@main",
						Procedures: []Procedure{
							{Say: "Main"},
						},
					},
				},
			},
		},
	}
	conversation, ok := lib.Conversation("guide", "@main-1", Context{})
	if !ok {
		t.Fatal("Conversation() returned ok=false")
	}
	if conversation.Text != "Main" {
		t.Fatalf("Conversation() text = %q, want Main", conversation.Text)
	}
}

func TestConversationFallsBackToFirstLabelWhenMainMissing(t *testing.T) {
	lib := Library{
		Entities: map[string]Entity{
			"teleporter": {
				ID:       "teleporter",
				Name:     "传送员",
				Kind:     KindMerchant,
				MapID:    "0",
				X:        1,
				Y:        2,
				Dir:      2,
				ScriptID: "teleporter_script",
			},
		},
		Scripts: map[string]Script{
			"teleporter_script": {
				ID: "teleporter_script",
				Labels: map[string]Label{
					"@传送员": {
						Name: "@传送员",
						Procedures: []Procedure{
							{Say: "欢迎光临"},
						},
					},
				},
			},
		},
	}
	conversation, ok := lib.Conversation("teleporter", "@main", Context{})
	if !ok {
		t.Fatal("Conversation() returned ok=false")
	}
	if conversation.Label != "@传送员" {
		t.Fatalf("Conversation() label = %q, want @传送员", conversation.Label)
	}
	if conversation.Text != "欢迎光临" {
		t.Fatalf("Conversation() text = %q, want 欢迎光临", conversation.Text)
	}
}

func TestRenderDialogueTextReplacesConfirmedVariables(t *testing.T) {
	got := renderDialogueText(
		"owner=<$OWNERGUILD> lord=<$LORD> gold=<$CASTLEGOLD> income=<$TODAYINCOME> door=<$CASTLEDOORSTATE> repair=<$REPAIRDOORGOLD>/<$REPAIRWALLGOLD> guard=<$GUARDFEE>/<$ARCHERFEE> upgrade=<$UPGRADEWEAPONFEE> weapon=<$USERWEAPON>",
		Context{
			OwnerGuild:       "测试行会",
			Lord:             "城主",
			CastleGold:       1234,
			TodayIncome:      56,
			CastleDoorState:  "关闭",
			RepairDoorGold:   2000,
			RepairWallGold:   500,
			GuardFee:         300,
			ArcherFee:        400,
			UpgradeWeaponFee: 10000,
			UserWeapon:       "木剑",
		},
		Entity{Name: "NPC"},
	)
	want := "owner=测试行会 lord=城主 gold=1234 income=56 door=关闭 repair=2000/500 guard=300/400 upgrade=10000 weapon=木剑"
	if got != want {
		t.Fatalf("renderDialogueText() = %q, want %q", got, want)
	}
}
