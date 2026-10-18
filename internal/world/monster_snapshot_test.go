package world

import (
	"testing"
	"time"

	"openmir2/internal/data"
	"openmir2/internal/storage"
)

func TestSnapshotAroundUsesReferenceMapScanOrder(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	w.mu.Lock()
	for id := range w.monsters {
		delete(w.monsters, id)
	}
	w.monsters["later-cell"] = &Monster{ID: "later-cell", MapID: ch.MapID, X: ch.X + 1, Y: ch.Y, ObjectOrder: 2, Alive: true}
	w.monsters["first-cell"] = &Monster{ID: "first-cell", MapID: ch.MapID, X: ch.X, Y: ch.Y + 1, ObjectOrder: 3, Alive: true}
	w.monsters["known-order"] = &Monster{ID: "known-order", MapID: ch.MapID, X: ch.X, Y: ch.Y, ObjectOrder: 1, Alive: true}
	w.monsters["unknown-order"] = &Monster{ID: "unknown-order", MapID: ch.MapID, X: ch.X, Y: ch.Y, Alive: true}
	w.drops["drop-later-cell"] = GroundDrop{ID: "drop-later-cell", MapID: ch.MapID, X: ch.X + 1, Y: ch.Y, ObjectOrder: 4}
	w.drops["drop-first-cell"] = GroundDrop{ID: "drop-first-cell", MapID: ch.MapID, X: ch.X, Y: ch.Y + 1, ObjectOrder: 5}
	w.drops["drop-known-order"] = GroundDrop{ID: "drop-known-order", MapID: ch.MapID, X: ch.X, Y: ch.Y, ObjectOrder: 1}
	w.drops["drop-unknown-order"] = GroundDrop{ID: "drop-unknown-order", MapID: ch.MapID, X: ch.X, Y: ch.Y}
	w.mu.Unlock()

	monsters, drops := w.SnapshotAround(ch.MapID, ch.X, ch.Y, 2)
	if len(monsters) != 4 {
		t.Fatalf("SnapshotAround() returned %d monsters, want 4", len(monsters))
	}
	if monsters[0].ID != "known-order" || monsters[1].ID != "unknown-order" || monsters[2].ID != "first-cell" || monsters[3].ID != "later-cell" {
		t.Fatalf("SnapshotAround() order = %v, want [known-order unknown-order first-cell later-cell]", monsters)
	}
	if len(drops) != 4 || drops[0].ID != "drop-known-order" || drops[1].ID != "drop-unknown-order" || drops[2].ID != "drop-first-cell" || drops[3].ID != "drop-later-cell" {
		t.Fatalf("SnapshotAround() drop order = %v, want [drop-known-order drop-unknown-order drop-first-cell drop-later-cell]", drops)
	}
}

func TestSnapshotAroundForVisibilityIncludesDeadNonGhostMonsters(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	w.mu.Lock()
	w.monsters["dead-visible"] = &Monster{ID: "dead-visible", MapID: ch.MapID, X: ch.X, Y: ch.Y, Alive: false, Hidden: true}
	w.mu.Unlock()
	if monsters, _ := w.SnapshotAround(ch.MapID, ch.X, ch.Y, 0); len(monsters) != 0 {
		t.Fatalf("SnapshotAround() returned %v, want no dead monster", monsters)
	}
	monsters, _ := w.SnapshotAroundForVisibility(ch.MapID, ch.X, ch.Y, 0)
	if len(monsters) != 1 || monsters[0].ID != "dead-visible" {
		t.Fatalf("SnapshotAroundForVisibility() = %v, want dead-visible", monsters)
	}
	w.mu.Lock()
	w.monsters["dead-visible"].Ghost = true
	w.mu.Unlock()
	if monsters, _ := w.SnapshotAroundForVisibility(ch.MapID, ch.X, ch.Y, 0); len(monsters) != 0 {
		t.Fatalf("Ghost SnapshotAroundForVisibility() = %v, want no monsters", monsters)
	}
}

func TestMonsterSnapshotForDelayedMessageKeepsDeadNonGhostAndRejectsGhost(t *testing.T) {
	w := &World{monsters: map[string]*Monster{
		"dead":  {ID: "dead", Alive: false, HP: 0},
		"ghost": {ID: "ghost", Alive: false, Ghost: true},
	}}

	dead, ok := w.MonsterSnapshotForDelayedMessage("dead")
	if !ok || dead.ID != "dead" {
		t.Fatalf("dead non-Ghost delayed snapshot = %+v, %t; want the object", dead, ok)
	}
	if _, ok := w.MonsterSnapshotForDelayedMessage("ghost"); ok {
		t.Fatal("Ghost delayed snapshot returned an object")
	}
}

func TestSnapshotAroundForVisibilityDoesNotRespawnMonsters(t *testing.T) {
	w := lifecycleWorld()
	mon := &Monster{
		ID: "visibility-respawn", TemplateID: "rat", MapID: "m", X: 5, Y: 5,
		MaxHP: 10, HP: 0, RespawnAt: time.Unix(1, 0), Spawn: data.StdSpawn{
			MapID: "m", MonsterID: "rat", X: 5, Y: 5, Range: 0,
		},
	}
	w.mu.Lock()
	w.monsters = map[string]*Monster{mon.ID: mon}
	w.mu.Unlock()
	w.SnapshotAroundForVisibility("m", 5, 5, 0)
	w.mu.Lock()
	respawned := w.monsters[mon.ID].Alive
	w.mu.Unlock()
	if respawned {
		t.Fatal("visibility snapshot should not respawn a monster")
	}
}

func TestSnapshotAroundDoesNotRespawnMonsters(t *testing.T) {
	w := lifecycleWorld()
	mon := &Monster{
		ID: "query-respawn", TemplateID: "rat", MapID: "m", X: 5, Y: 5,
		MaxHP: 10, HP: 0, RespawnAt: time.Unix(1, 0), Spawn: data.StdSpawn{
			MapID: "m", MonsterID: "rat", X: 5, Y: 5, Range: 0,
		},
	}
	w.mu.Lock()
	w.monsters = map[string]*Monster{mon.ID: mon}
	w.mu.Unlock()
	w.SnapshotAround("m", 5, 5, 0)
	w.mu.Lock()
	respawned := w.monsters[mon.ID].Alive
	w.mu.Unlock()
	if respawned {
		t.Fatal("query snapshot should not respawn a monster")
	}
}

func TestSnapshotAroundClearsExpiredDropOwnership(t *testing.T) {
	w := lifecycleWorld()
	w.drops["expired-owner"] = GroundDrop{
		ID: "expired-owner", MapID: "m", X: 5, Y: 5, OwnerID: "owner", OwnerGroupID: "group",
		PickupAt: time.Now().Add(-time.Second),
	}
	w.SnapshotAround("m", 5, 5, 0)
	got := w.drops["expired-owner"]
	if got.OwnerID != "" || got.OwnerGroupID != "" {
		t.Fatalf("expired drop ownership = %q/%q, want cleared", got.OwnerID, got.OwnerGroupID)
	}
}

func TestSnapshotAroundForVisibilityUsesInclusiveSquareRange(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	w.mu.Lock()
	for id := range w.monsters {
		delete(w.monsters, id)
	}
	w.monsters["edge"] = &Monster{ID: "edge", MapID: ch.MapID, X: ch.X + 2, Y: ch.Y, Alive: true}
	w.monsters["corner"] = &Monster{ID: "corner", MapID: ch.MapID, X: ch.X + 2, Y: ch.Y + 2, Alive: true}
	w.monsters["outer"] = &Monster{ID: "outer", MapID: ch.MapID, X: ch.X + 3, Y: ch.Y, Alive: true}
	w.monsters["other-map"] = &Monster{ID: "other-map", MapID: "other", X: ch.X, Y: ch.Y, Alive: true}
	w.drops["drop-edge"] = GroundDrop{ID: "drop-edge", MapID: ch.MapID, X: ch.X + 2, Y: ch.Y}
	w.drops["drop-corner"] = GroundDrop{ID: "drop-corner", MapID: ch.MapID, X: ch.X + 2, Y: ch.Y + 2}
	w.drops["drop-outer"] = GroundDrop{ID: "drop-outer", MapID: ch.MapID, X: ch.X + 3, Y: ch.Y}
	w.drops["drop-other-map"] = GroundDrop{ID: "drop-other-map", MapID: "other", X: ch.X, Y: ch.Y}
	w.mu.Unlock()
	monsters, drops := w.SnapshotAroundForVisibility(ch.MapID, ch.X, ch.Y, 2)
	seen := make(map[string]bool, len(monsters))
	for _, mon := range monsters {
		seen[mon.ID] = true
	}
	if !seen["edge"] || !seen["corner"] || seen["outer"] || seen["other-map"] {
		t.Fatalf("square-range monsters = %v, want edge/corner included and outer/other-map excluded", monsters)
	}
	seenDrops := make(map[string]bool, len(drops))
	for _, drop := range drops {
		seenDrops[drop.ID] = true
	}
	if !seenDrops["drop-edge"] || !seenDrops["drop-corner"] || seenDrops["drop-outer"] || seenDrops["drop-other-map"] {
		t.Fatalf("square-range drops = %v, want edge/corner included and outer/other-map excluded", drops)
	}
}

func TestSnapshotAroundForVisibilityRemovesExpiredDrops(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	w.mu.Lock()
	w.drops["expired"] = GroundDrop{ID: "expired", MapID: ch.MapID, X: ch.X, Y: ch.Y, CreatedAt: time.Now().Add(-2 * time.Hour)}
	w.drops["current"] = GroundDrop{ID: "current", MapID: ch.MapID, X: ch.X, Y: ch.Y, CreatedAt: time.Now()}
	w.mu.Unlock()
	_, drops := w.SnapshotAroundForVisibility(ch.MapID, ch.X, ch.Y, 0)
	if len(drops) != 1 || drops[0].ID != "current" {
		t.Fatalf("drop visibility snapshot = %v, want current only", drops)
	}
	w.mu.Lock()
	_, expiredStillPresent := w.drops["expired"]
	_, currentStillPresent := w.drops["current"]
	w.mu.Unlock()
	if expiredStillPresent || !currentStillPresent {
		t.Fatalf("drop store after snapshot = expired:%t current:%t, want expired:false current:true", expiredStillPresent, currentStillPresent)
	}
}

func TestSnapshotAroundForVisibilityClearsGhostDropOwners(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	owner := storage.Character{ID: "ghost-drop-owner", MapID: ch.MapID, X: ch.X, Y: ch.Y}
	groupOwner := storage.Character{ID: "ghost-drop-group", MapID: ch.MapID, X: ch.X, Y: ch.Y}
	w.HandleCharacterDisconnect(owner, time.Now())
	w.HandleCharacterDisconnect(groupOwner, time.Now())
	w.mu.Lock()
	w.drops["owner-drop"] = GroundDrop{ID: "owner-drop", MapID: ch.MapID, X: ch.X, Y: ch.Y, OwnerID: owner.ID}
	w.drops["group-drop"] = GroundDrop{ID: "group-drop", MapID: ch.MapID, X: ch.X, Y: ch.Y, OwnerGroupID: groupOwner.ID}
	w.mu.Unlock()
	_, drops := w.SnapshotAroundForVisibility(ch.MapID, ch.X, ch.Y, 0)
	if len(drops) != 2 {
		t.Fatalf("drop visibility snapshot = %v, want both drops", drops)
	}
	for _, drop := range drops {
		if drop.OwnerID != "" || drop.OwnerGroupID != "" {
			t.Fatalf("ghost-owned drop = %+v, want ownership cleared", drop)
		}
	}
}

func TestSnapshotAroundForVisibilityRejectsNegativeRange(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	w.mu.Lock()
	w.monsters["nearby"] = &Monster{ID: "nearby", MapID: ch.MapID, X: ch.X, Y: ch.Y, Alive: true}
	w.drops["nearby-drop"] = GroundDrop{ID: "nearby-drop", MapID: ch.MapID, X: ch.X, Y: ch.Y}
	w.mu.Unlock()
	monsters, drops := w.SnapshotAroundForVisibility(ch.MapID, ch.X, ch.Y, -1)
	if len(monsters) != 0 || len(drops) != 0 {
		t.Fatalf("negative-range visibility snapshot = monsters:%v drops:%v, want empty", monsters, drops)
	}
}

func TestSnapshotAroundForVisibilityKeepsHiddenButNotFixedHiddenMonsters(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	w.mu.Lock()
	w.monsters["hidden-visible"] = &Monster{ID: "hidden-visible", MapID: ch.MapID, X: ch.X, Y: ch.Y, ObjectOrder: 1, Alive: true, Hidden: true}
	w.monsters["fixed-hidden"] = &Monster{ID: "fixed-hidden", MapID: ch.MapID, X: ch.X, Y: ch.Y, Alive: true, Hidden: true, FixedHideMode: true}
	w.monsters["admin-hidden"] = &Monster{ID: "admin-hidden", MapID: ch.MapID, X: ch.X, Y: ch.Y, ObjectOrder: 3, Alive: true, AdminMode: true}
	w.mu.Unlock()
	monsters, _ := w.SnapshotAroundForVisibility(ch.MapID, ch.X, ch.Y, 0)
	if len(monsters) != 2 || monsters[0].ID != "hidden-visible" || monsters[1].ID != "admin-hidden" {
		t.Fatalf("SnapshotAroundForVisibility() = %v, want hidden-visible and admin-hidden", monsters)
	}
	w.mu.Lock()
	w.monsters["admin-hidden"].AdminMode = false
	w.mu.Unlock()
	monsters, _ = w.SnapshotAroundForVisibility(ch.MapID, ch.X, ch.Y, 0)
	if len(monsters) != 2 {
		t.Fatalf("admin-cleared SnapshotAroundForVisibility() = %v, want two monsters", monsters)
	}
	w.mu.Lock()
	w.monsters["admin-hidden"].Ghost = true
	w.monsters["fixed-hidden"].FixedHideMode = false
	w.mu.Unlock()
	monsters, _ = w.SnapshotAroundForVisibility(ch.MapID, ch.X, ch.Y, 0)
	if len(monsters) != 2 {
		t.Fatalf("unfixed SnapshotAroundForVisibility() = %v, want both monsters", monsters)
	}
	w.mu.Lock()
	w.monsters["fixed-hidden"].Ghost = true
	w.mu.Unlock()
	monsters, _ = w.SnapshotAroundForVisibility(ch.MapID, ch.X, ch.Y, 0)
	if len(monsters) != 1 || monsters[0].ID != "hidden-visible" {
		t.Fatalf("Ghost SnapshotAroundForVisibility() = %v, want hidden-visible only", monsters)
	}
}

func TestMonsterSnapshotBeforeUsesObjectOrderBeforeID(t *testing.T) {
	if !monsterSnapshotBefore(Monster{ID: "z", ObjectOrder: 1}, Monster{ID: "a", ObjectOrder: 2}) {
		t.Fatal("monsterSnapshotBefore() did not prefer the earlier object order")
	}
	if monsterSnapshotBefore(Monster{ID: "z"}, Monster{ID: "a"}) {
		t.Fatal("monsterSnapshotBefore() did not use ID for unknown object order")
	}
	if !monsterSnapshotBefore(Monster{ID: "z", ObjectOrder: 1}, Monster{ID: "a"}) {
		t.Fatal("monsterSnapshotBefore() did not prefer a known object order")
	}
	if monsterSnapshotBefore(Monster{ID: "a"}, Monster{ID: "z", ObjectOrder: 1}) {
		t.Fatal("monsterSnapshotBefore() did not place an unknown object order after a known one")
	}
	if !monsterObjectOrderBefore(&Monster{ID: "z", ObjectOrder: 1}, &Monster{ID: "a"}) {
		t.Fatal("monsterObjectOrderBefore() did not prefer a known object order")
	}
	if monsterObjectOrderBefore(&Monster{ID: "a"}, &Monster{ID: "z", ObjectOrder: 1}) {
		t.Fatal("monsterObjectOrderBefore() did not place an unknown object order after a known one")
	}
}

func TestMonsterActorTargetSelectionUsesObjectOrder(t *testing.T) {
	w := &World{monsters: map[string]*Monster{
		"later":   {ID: "later", MapID: "map", X: 10, Y: 10, ObjectOrder: 20, Alive: true},
		"earlier": {ID: "earlier", MapID: "map", X: 10, Y: 10, ObjectOrder: 10, Alive: true},
	}}
	actorID := MonsterActorID(*w.monsters["earlier"])
	if got := w.monsterTargetLocked("map", 10, 10, actorID, 1); got == nil || got.ID != "earlier" {
		t.Fatalf("monsterTargetLocked() = %+v, want earlier object", got)
	}
	if got := w.spellMonsterTargetNearLocked("map", 10, 10, actorID, 1); got == nil || got.ID != "earlier" {
		t.Fatalf("spellMonsterTargetNearLocked() = %+v, want earlier object", got)
	}
}

func TestSpellMonsterTargetNearSkipsDeadMonster(t *testing.T) {
	mon := &Monster{ID: "dead", ActorID: 500001, MapID: "map", X: 10, Y: 10, Alive: false}
	w := &World{monsters: map[string]*Monster{mon.ID: mon}}
	if got := w.spellMonsterTargetNearLocked("map", 10, 10, MonsterActorID(*mon), 1); got != nil {
		t.Fatalf("spellMonsterTargetNearLocked() = %+v, want nil for dead monster", got)
	}
}

func TestMonsterAtExactPointUsesObjectOrder(t *testing.T) {
	w := &World{monsters: map[string]*Monster{
		"fixed-hidden": {ID: "fixed-hidden", MapID: "map", X: 10, Y: 10, ObjectOrder: 1, Alive: true, FixedHideMode: true},
		"later":        {ID: "later", MapID: "map", X: 10, Y: 10, ObjectOrder: 20, Alive: true},
		"earlier":      {ID: "earlier", MapID: "map", X: 10, Y: 10, ObjectOrder: 10, Alive: true},
	}}
	if got := w.monsterAtExactPointLocked("map", 10, 10); got == nil || got.ID != "earlier" {
		t.Fatalf("monsterAtExactPointLocked() = %+v, want earlier object", got)
	}
}

func TestMonsterAtLockedKeepsAdminMonsterAsBlocker(t *testing.T) {
	mon := &Monster{ID: "admin-blocker", MapID: "map", X: 10, Y: 10, Alive: true, AdminMode: true}
	w := &World{monsters: map[string]*Monster{mon.ID: mon}, occupied: map[monsterPosition]string{{MapID: "map", X: 10, Y: 10}: mon.ID}}
	if !w.monsterAtLockedWithRun("map", 10, 10, "", false) {
		t.Fatal("admin monster did not remain a movement blocker")
	}
}

func TestSkillActorTargetResolutionUsesObjectOrder(t *testing.T) {
	w := &World{monsters: map[string]*Monster{
		"later":   {ID: "later", MapID: "map", X: 10, Y: 10, ObjectOrder: 20, Alive: true},
		"earlier": {ID: "earlier", MapID: "map", X: 11, Y: 10, ObjectOrder: 10, Alive: true},
	}}
	if got := w.monsterNearActorLocked("map", 10, 10, MonsterActorID(*w.monsters["earlier"])); got == nil || got.ID != "earlier" {
		t.Fatalf("monsterNearActorLocked() = %+v, want earlier object", got)
	}
}

func TestMonstersInRadiusUsesObjectOrderForEqualDistance(t *testing.T) {
	w := &World{monsters: map[string]*Monster{
		"later":   {ID: "later", MapID: "map", X: 10, Y: 11, ObjectOrder: 20, Alive: true},
		"earlier": {ID: "earlier", MapID: "map", X: 11, Y: 10, ObjectOrder: 10, Alive: true},
	}}
	got := w.monstersInRadiusLocked("map", 10, 10, 1)
	if len(got) != 2 || got[0].ID != "earlier" {
		t.Fatalf("monstersInRadiusLocked() = %v, want earlier object first", got)
	}
}

func TestMonstersInRadiusRejectsNegativeRadius(t *testing.T) {
	w := &World{monsters: map[string]*Monster{
		"target": {ID: "target", MapID: "map", X: 10, Y: 10, Alive: true},
	}}
	if got := w.monstersInRadiusLocked("map", 10, 10, -1); len(got) != 0 {
		t.Fatalf("negative-radius monsters = %v, want none", got)
	}
}

func TestMonsterTargetRejectsNegativeRadius(t *testing.T) {
	mon := &Monster{ID: "target", MapID: "map", X: 10, Y: 10, Alive: true}
	w := &World{monsters: map[string]*Monster{mon.ID: mon}}
	if got := w.monsterTargetLocked("map", 10, 10, MonsterActorID(*mon), -1); got != nil {
		t.Fatalf("negative-radius monster target = %+v, want nil", got)
	}
	if got := w.monsterTargetLocked("map", 10, 10, 0, -1); got != nil {
		t.Fatalf("negative-radius point target = %+v, want nil", got)
	}
	if got := w.spellMonsterTargetNearLocked("map", 10, 10, MonsterActorID(*mon), -1); got != nil {
		t.Fatalf("negative-radius spell monster target = %+v, want nil", got)
	}
	if got := w.explicitMonsterTargetLocked("map", 10, 10, MonsterActorID(*mon), -1); got != nil {
		t.Fatalf("negative-radius explicit monster target = %+v, want nil", got)
	}
}

func TestMonsterCharacterTargetRejectsAnonymousCharacter(t *testing.T) {
	mon := &Monster{ID: "monster", MapID: "map"}
	anonymous := storage.Character{MapID: "map", HP: 10}
	if w := (&World{}).monsterCanTargetCharacterLocked(mon, anonymous); w {
		t.Fatal("monsterCanTargetCharacterLocked() accepted anonymous character")
	}
	if w := (&World{}).monsterCanKeepCharacterTargetLocked(mon, anonymous); w {
		t.Fatal("monsterCanKeepCharacterTargetLocked() accepted anonymous character")
	}
	if w := (&World{}).monsterCanAttackCharacterTargetLocked(mon, anonymous); w {
		t.Fatal("monsterCanAttackCharacterTargetLocked() accepted anonymous character")
	}
}

func TestButcherTargetRequiresClickedCell(t *testing.T) {
	w := &World{monsters: map[string]*Monster{
		"later":   {ID: "later", MapID: "map", X: 10, Y: 10, ObjectOrder: 20, Animal: true},
		"earlier": {ID: "earlier", MapID: "map", X: 11, Y: 10, ObjectOrder: 10, Animal: true},
	}}
	actorID := MonsterActorID(*w.monsters["earlier"])
	if got := w.butcherTargetLocked("map", actorID, 10, 10); got != nil {
		t.Fatalf("butcherTargetLocked() = %+v, want nil for an offset target", got)
	}
}

func TestGroundDropBeforePrefersKnownObjectOrder(t *testing.T) {
	if !groundDropBefore(GroundDrop{ID: "z", ObjectOrder: 1}, GroundDrop{ID: "a"}) {
		t.Fatal("groundDropBefore() did not prefer a known object order")
	}
	if groundDropBefore(GroundDrop{ID: "a"}, GroundDrop{ID: "z", ObjectOrder: 1}) {
		t.Fatal("groundDropBefore() did not place an unknown object order after a known one")
	}
}

func TestActiveSummonedMonsterByTemplateUsesObjectOrder(t *testing.T) {
	w := &World{monsters: map[string]*Monster{
		"later":   {ID: "later", MasterID: "owner", TemplateID: "wolf", ObjectOrder: 20, Alive: true},
		"earlier": {ID: "earlier", MasterID: "owner", TemplateID: "wolf", ObjectOrder: 10, Alive: true},
	}}
	if got := w.activeSummonedMonsterByTemplateLocked("owner", "wolf", time.Time{}); got == nil || got.ID != "earlier" {
		t.Fatalf("activeSummonedMonsterByTemplateLocked() = %+v, want earlier object", got)
	}
}
