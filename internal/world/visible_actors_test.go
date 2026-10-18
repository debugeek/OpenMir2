package world

import (
	"reflect"
	"testing"
	"time"

	"openmir2/internal/storage"
)

func TestVisibleActorListKeepsExistingOrderAndAppendsNewActors(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	observer := &Monster{ID: "visible-observer", MapID: ch.MapID, X: ch.X, Y: ch.Y, ObjectOrder: 1, Alive: true}
	w.mu.Lock()
	for id := range w.monsters {
		delete(w.monsters, id)
	}
	w.monsters[observer.ID] = observer
	players := map[string]storage.Character{
		"actor-a":    {ID: "actor-a", MapID: ch.MapID, X: ch.X + 1, Y: ch.Y, ObjectOrder: 2},
		"actor-b":    {ID: "actor-b", MapID: ch.MapID, X: ch.X, Y: ch.Y + 1, ObjectOrder: 3},
		"actor-dead": {ID: "actor-dead", MapID: ch.MapID, X: ch.X + 1, Y: ch.Y + 1, HP: 0, ObjectOrder: 4},
	}
	first := w.visibleActorKeysLocked(observer.ID, ch.MapID, ch.X, ch.Y, 3, players)
	w.mu.Unlock()

	wantFirst := []string{monsterVisibleActorKey(observer.ID), characterVisibleActorKey("actor-b"), characterVisibleActorKey("actor-a"), characterVisibleActorKey("actor-dead")}
	if len(first) != len(wantFirst) {
		t.Fatalf("first visible list = %v, want %v", first, wantFirst)
	}
	for i := range wantFirst {
		if first[i] != wantFirst[i] {
			t.Fatalf("first visible list = %v, want %v", first, wantFirst)
		}
	}

	w.mu.Lock()
	players["actor-a"] = storage.Character{ID: "actor-a", MapID: ch.MapID, X: ch.X + 1, Y: ch.Y + 1, ObjectOrder: 100}
	players["actor-b"] = storage.Character{ID: "actor-b", MapID: ch.MapID, X: ch.X + 2, Y: ch.Y + 1, ObjectOrder: 3}
	players["actor-c"] = storage.Character{ID: "actor-c", MapID: ch.MapID, X: ch.X, Y: ch.Y + 2, ObjectOrder: 4}
	second := w.visibleActorKeysLocked(observer.ID, ch.MapID, ch.X, ch.Y, 3, players)
	w.mu.Unlock()

	wantSecond := []string{monsterVisibleActorKey(observer.ID), characterVisibleActorKey("actor-b"), characterVisibleActorKey("actor-a"), characterVisibleActorKey("actor-dead"), characterVisibleActorKey("actor-c")}
	if len(second) != len(wantSecond) {
		t.Fatalf("second visible list = %v, want %v", second, wantSecond)
	}
	for i := range wantSecond {
		if second[i] != wantSecond[i] {
			t.Fatalf("second visible list = %v, want %v", second, wantSecond)
		}
	}

	w.mu.Lock()
	players["actor-dead"] = storage.Character{ID: "actor-dead", MapID: ch.MapID, X: ch.X + 1, Y: ch.Y + 1, Ghost: true, ObjectOrder: 4}
	third := w.visibleActorKeysLocked(observer.ID, ch.MapID, ch.X, ch.Y, 3, players)
	w.mu.Unlock()
	wantThird := []string{monsterVisibleActorKey(observer.ID), characterVisibleActorKey("actor-b"), characterVisibleActorKey("actor-a"), characterVisibleActorKey("actor-c")}
	if !reflect.DeepEqual(third, wantThird) {
		t.Fatalf("ghost-filtered visible list = %v, want %v", third, wantThird)
	}

	w.mu.Lock()
	players["actor-a"] = storage.Character{ID: "actor-a", MapID: "other", X: ch.X + 1, Y: ch.Y, ObjectOrder: 2}
	leftMap := w.visibleActorKeysLocked(observer.ID, ch.MapID, ch.X, ch.Y, 3, players)
	w.mu.Unlock()
	wantLeftMap := []string{monsterVisibleActorKey(observer.ID), characterVisibleActorKey("actor-b"), characterVisibleActorKey("actor-c")}
	if !reflect.DeepEqual(leftMap, wantLeftMap) {
		t.Fatalf("cross-map character list = %v, want %v", leftMap, wantLeftMap)
	}
	players["actor-a"] = storage.Character{ID: "actor-a", MapID: ch.MapID, X: ch.X + 1, Y: ch.Y, ObjectOrder: 2}
	w.mu.Lock()
	returned := w.visibleActorKeysLocked(observer.ID, ch.MapID, ch.X, ch.Y, 3, players)
	w.mu.Unlock()
	wantReturned := []string{monsterVisibleActorKey(observer.ID), characterVisibleActorKey("actor-b"), characterVisibleActorKey("actor-c"), characterVisibleActorKey("actor-a")}
	if !reflect.DeepEqual(returned, wantReturned) {
		t.Fatalf("returned character list = %v, want %v", returned, wantReturned)
	}
}

func TestVisibleActorListRetainsDeadMonstersUntilTargetFiltering(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	observer := &Monster{ID: "visible-observer", MapID: ch.MapID, X: ch.X, Y: ch.Y, Alive: true}
	dead := &Monster{ID: "dead-visible", MapID: ch.MapID, X: ch.X + 1, Y: ch.Y, Alive: false}
	w.mu.Lock()
	for id := range w.monsters {
		delete(w.monsters, id)
	}
	w.monsters[observer.ID] = observer
	w.monsters[dead.ID] = dead
	keys := w.visibleActorKeysLocked(observer.ID, ch.MapID, ch.X, ch.Y, 3, nil)
	w.mu.Unlock()
	if !containsString(keys, monsterVisibleActorKey(dead.ID)) {
		t.Fatalf("visible actor keys = %v, want dead monster retained by SearchViewRange", keys)
	}
}

func TestVisibleActorListKeepsNonGhostCharacterDeathState(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	observer := &Monster{ID: "death-state-observer", MapID: ch.MapID, X: ch.X, Y: ch.Y, Alive: true}
	dead := storage.Character{ID: "death-state-character", MapID: ch.MapID, X: ch.X + 1, Y: ch.Y, HP: 0}
	w.mu.Lock()
	w.monsters[observer.ID] = observer
	w.finalizedCharacterDeaths[dead.ID] = struct{}{}
	keys := w.visibleActorKeysLocked(observer.ID, ch.MapID, ch.X, ch.Y, 3, map[string]storage.Character{dead.ID: dead})
	delete(w.finalizedCharacterDeaths, dead.ID)
	w.pendingCharacterDeaths[dead.ID] = struct{}{}
	pendingKeys := w.visibleActorKeysLocked(observer.ID, ch.MapID, ch.X, ch.Y, 3, map[string]storage.Character{dead.ID: dead})
	w.mu.Unlock()
	if !containsString(keys, characterVisibleActorKey(dead.ID)) || !containsString(pendingKeys, characterVisibleActorKey(dead.ID)) {
		t.Fatalf("dead character missing from finalized/pending states: finalized=%v pending=%v", keys, pendingKeys)
	}
}

func TestVisibleActorListExcludesObserverCharacters(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	observer := &Monster{ID: "observer-character-filter", MapID: ch.MapID, X: ch.X, Y: ch.Y, Alive: true}
	players := map[string]storage.Character{
		"normal-character":   {ID: "normal-character", MapID: ch.MapID, X: ch.X + 1, Y: ch.Y},
		"observer-character": {ID: "observer-character", MapID: ch.MapID, X: ch.X, Y: ch.Y + 1, ObserverMode: true},
	}
	w.mu.Lock()
	w.monsters[observer.ID] = observer
	keys := w.visibleActorKeysLocked(observer.ID, ch.MapID, ch.X, ch.Y, 3, players)
	w.mu.Unlock()

	if !containsString(keys, characterVisibleActorKey("normal-character")) {
		t.Fatalf("visible actor list = %v, want normal character", keys)
	}
	if containsString(keys, characterVisibleActorKey("observer-character")) {
		t.Fatalf("visible actor list = %v, did not want observer character", keys)
	}
}

func TestVisibleActorListMatchesReferenceMonsterMembership(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	ordinary := &Monster{ID: "ordinary", Race: 81, MapID: ch.MapID, X: ch.X + 1, Y: ch.Y, ObjectOrder: 2}
	summon := &Monster{ID: "summon", MasterID: ch.ID, MapID: ch.MapID, X: ch.X + 1, Y: ch.Y, ObjectOrder: 3}
	farSummon := &Monster{ID: "far-summon", MasterID: ch.ID, MapID: ch.MapID, X: ch.X + 5, Y: ch.Y, ObjectOrder: 7}
	dead := &Monster{ID: "dead", MapID: ch.MapID, X: ch.X + 2, Y: ch.Y, Alive: false, ObjectOrder: 5}
	fixedHidden := &Monster{ID: "fixed-hidden", MapID: ch.MapID, X: ch.X + 2, Y: ch.Y, FixedHideMode: true, ObjectOrder: 6}
	guard := &Monster{ID: "guard", Race: 112, WantRefMsg: true, MapID: ch.MapID, X: ch.X, Y: ch.Y, ObjectOrder: 4}
	crazy := &Monster{ID: "crazy", CrazyUntil: time.Unix(100, 0), MapID: ch.MapID, X: ch.X, Y: ch.Y + 1, ObjectOrder: 5}
	w.mu.Lock()
	for id := range w.monsters {
		delete(w.monsters, id)
	}
	w.monsters[ordinary.ID] = ordinary
	w.monsters[summon.ID] = summon
	w.monsters[farSummon.ID] = farSummon
	w.monsters[dead.ID] = dead
	w.monsters[fixedHidden.ID] = fixedHidden
	w.monsters[guard.ID] = guard
	w.monsters[crazy.ID] = crazy
	ordinaryKeys := w.visibleActorKeysLocked(ordinary.ID, ch.MapID, ch.X+1, ch.Y, 3, nil)
	summonKeys := w.visibleActorKeysLocked(summon.ID, ch.MapID, ch.X+1, ch.Y, 3, nil)
	guardKeys := w.visibleActorKeysLocked(guard.ID, ch.MapID, ch.X, ch.Y, 3, nil)
	crazyKeys := w.visibleActorKeysLocked(crazy.ID, ch.MapID, ch.X, ch.Y+1, 3, nil)
	w.mu.Unlock()

	if !containsString(ordinaryKeys, monsterVisibleActorKey(ordinary.ID)) || !containsString(ordinaryKeys, monsterVisibleActorKey(summon.ID)) || containsString(ordinaryKeys, monsterVisibleActorKey(farSummon.ID)) || containsString(ordinaryKeys, monsterVisibleActorKey(guard.ID)) {
		t.Fatalf("ordinary observer visible actors = %v, want near summon but no far summon or guard", ordinaryKeys)
	}
	if !containsString(summonKeys, monsterVisibleActorKey(ordinary.ID)) {
		t.Fatalf("summon observer visible actors = %v, want ordinary monster", summonKeys)
	}
	if !containsString(guardKeys, monsterVisibleActorKey(ordinary.ID)) || !containsString(guardKeys, monsterVisibleActorKey(dead.ID)) {
		t.Fatalf("guard visible actors = %v, want ordinary and non-ghost dead monsters", guardKeys)
	}
	if containsString(guardKeys, monsterVisibleActorKey(fixedHidden.ID)) {
		t.Fatalf("guard visible actors = %v, want fixed-hidden monster excluded", guardKeys)
	}
	w.mu.Lock()
	fixedHidden.FixedHideMode = false
	guardKeysAfterUnhide := w.visibleActorKeysLocked(guard.ID, ch.MapID, ch.X, ch.Y, 3, nil)
	w.mu.Unlock()
	if !containsString(guardKeysAfterUnhide, monsterVisibleActorKey(fixedHidden.ID)) {
		t.Fatalf("guard visible actors after fixed-hide transition = %v, want unhidden monster", guardKeysAfterUnhide)
	}
	w.mu.Lock()
	dead.Ghost = true
	guardKeysAfterGhost := w.visibleActorKeysLocked(guard.ID, ch.MapID, ch.X, ch.Y, 3, nil)
	w.mu.Unlock()
	if containsString(guardKeysAfterGhost, monsterVisibleActorKey(dead.ID)) || !containsString(guardKeysAfterGhost, monsterVisibleActorKey(ordinary.ID)) {
		t.Fatalf("guard visible actors after ghost transition = %v, want ordinary monster but no ghost", guardKeysAfterGhost)
	}
	w.mu.Lock()
	ordinary.MapID = "other"
	guardKeysAfterMap := w.visibleActorKeysLocked(guard.ID, ch.MapID, ch.X, ch.Y, 3, nil)
	w.mu.Unlock()
	if containsString(guardKeysAfterMap, monsterVisibleActorKey(ordinary.ID)) {
		t.Fatalf("guard visible actors after monster map change = %v, want ordinary removed", guardKeysAfterMap)
	}
	w.mu.Lock()
	ordinary.MapID = ch.MapID
	guardKeysReturned := w.visibleActorKeysLocked(guard.ID, ch.MapID, ch.X, ch.Y, 3, nil)
	w.mu.Unlock()
	if !containsString(guardKeysReturned, monsterVisibleActorKey(ordinary.ID)) {
		t.Fatalf("guard visible actors after monster return = %v, want ordinary monster", guardKeysReturned)
	}
	if !containsString(crazyKeys, monsterVisibleActorKey(ordinary.ID)) {
		t.Fatalf("crazy observer visible actors = %v, want ordinary monster", crazyKeys)
	}
	humanLike := &Monster{ID: "human-like", Race: 20, MapID: ch.MapID, X: ch.X, Y: ch.Y + 2, ObjectOrder: 8}
	w.mu.Lock()
	w.monsters[humanLike.ID] = humanLike
	humanLikeKeys := w.visibleActorKeysLocked(humanLike.ID, ch.MapID, humanLike.X, humanLike.Y, 3, nil)
	w.mu.Unlock()
	if !containsString(humanLikeKeys, monsterVisibleActorKey(ordinary.ID)) {
		t.Fatalf("human-like observer visible actors = %v, want unowned monster", humanLikeKeys)
	}
}

func TestVisibleActorListUsesInclusiveSquareRange(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	observer := &Monster{ID: "range-observer", MapID: ch.MapID, X: ch.X, Y: ch.Y, ViewRange: 3, Alive: true}
	w.mu.Lock()
	w.monsters[observer.ID] = observer
	keys := w.visibleActorKeysLocked(observer.ID, ch.MapID, ch.X, ch.Y, 3, map[string]storage.Character{
		"edge":   {ID: "edge", MapID: ch.MapID, X: ch.X + 3, Y: ch.Y},
		"corner": {ID: "corner", MapID: ch.MapID, X: ch.X + 3, Y: ch.Y + 3},
		"outer":  {ID: "outer", MapID: ch.MapID, X: ch.X + 4, Y: ch.Y},
	})
	w.mu.Unlock()
	if !containsString(keys, characterVisibleActorKey("edge")) || !containsString(keys, characterVisibleActorKey("corner")) || containsString(keys, characterVisibleActorKey("outer")) {
		t.Fatalf("range visible actors = %v, want edge/corner included and outer excluded", keys)
	}
}

func TestVisibleActorListRejectsNegativeRange(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	w.monsters["negative-range-observer"] = &Monster{ID: "negative-range-observer", MapID: ch.MapID, X: ch.X, Y: ch.Y, Race: 20}
	w.visibleActors["negative-range-observer"] = []string{characterVisibleActorKey("stale")}
	keys := w.visibleActorKeysLocked("negative-range-observer", ch.MapID, ch.X, ch.Y, -1, map[string]storage.Character{
		"nearby": {ID: "nearby", MapID: ch.MapID, X: ch.X, Y: ch.Y},
	})
	if len(keys) != 0 || len(w.visibleActors["negative-range-observer"]) != 0 {
		t.Fatalf("negative-range visible actors = %v, want empty", keys)
	}
}

func TestVisibleActorListExcludesNPCsFromMonsterSearch(t *testing.T) {
	w, _ := newTestWorldCharacter(t)
	w.monsters["observer"] = &Monster{ID: "observer", MapID: "0", X: 10, Y: 10, Race: 20, ViewRange: 3}
	keys := w.visibleActorKeysLocked("observer", "0", 10, 10, 3, nil)
	if containsString(keys, "npc:guide") || containsString(keys, "npc:merchant") {
		t.Fatalf("monster visible actors = %v, want moving objects only", keys)
	}
}

func TestVisibleActorListIncludesAdminModeMonsters(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	observer := &Monster{ID: "observer", MapID: ch.MapID, X: ch.X, Y: ch.Y, Race: 20, ViewRange: 3}
	admin := &Monster{ID: "admin", MapID: ch.MapID, X: ch.X + 1, Y: ch.Y, Race: 20, AdminMode: true}
	w.monsters[observer.ID] = observer
	w.monsters[admin.ID] = admin
	keys := w.visibleActorKeysLocked(observer.ID, ch.MapID, ch.X, ch.Y, 3, nil)
	if !containsString(keys, monsterVisibleActorKey(admin.ID)) {
		t.Fatalf("admin-mode monster visible actors = %v, want admin monster retained", keys)
	}
}

func TestArcherGuardKeepsFirstEqualDistanceVisibleTarget(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	guard := &Monster{ID: "guard-order", Race: 112, MapID: ch.MapID, X: ch.X, Y: ch.Y, ViewRange: 12}
	first := ch
	first.ID = "first-visible"
	first.X = ch.X
	first.Y = ch.Y + 1
	first.ObjectOrder = 20
	first.PKPoint = 200
	second := ch
	second.ID = "second-visible"
	second.X = ch.X + 1
	second.Y = ch.Y
	second.ObjectOrder = 10
	second.PKPoint = 200
	w.mu.Lock()
	for id := range w.monsters {
		delete(w.monsters, id)
	}
	w.monsters[guard.ID] = guard
	target, ok := w.findClosestArcherCharacterTargetLocked(guard, map[string]storage.Character{first.ID: first, second.ID: second}, 12)
	w.mu.Unlock()
	if !ok || target.ID != first.ID {
		t.Fatalf("equal-distance archer target = %+v, found=%t, want %q", target, ok, first.ID)
	}
}

func TestFleeTargetKeepsFirstEqualDistanceMixedActor(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	observer := &Monster{ID: "flee-order", MapID: ch.MapID, X: ch.X, Y: ch.Y, ViewRange: 5}
	first := ch
	first.ID = "first-flee"
	first.X = ch.X
	first.Y = ch.Y + 1
	first.ObjectOrder = 20
	summon := &Monster{ID: "second-flee", MasterID: ch.ID, MapID: ch.MapID, X: ch.X + 1, Y: ch.Y, Alive: true, ObjectOrder: 10}
	w.mu.Lock()
	for id := range w.monsters {
		delete(w.monsters, id)
	}
	w.monsters[observer.ID] = observer
	w.monsters[summon.ID] = summon
	target, ok := w.findClosestFleeTargetLocked(observer, map[string]storage.Character{first.ID: first}, 5)
	w.mu.Unlock()
	if !ok || target.character == nil || target.character.ID != first.ID {
		t.Fatalf("equal-distance flee target = %+v, found=%t, want character %q", target, ok, first.ID)
	}
}

func TestSummonedTargetKeepsFirstEqualDistanceVisibleMonster(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	observer := &Monster{ID: "summon-target-order", MapID: ch.MapID, X: ch.X, Y: ch.Y, ViewRange: 6}
	first := &Monster{ID: "first-summon-target", MasterID: ch.ID, MapID: ch.MapID, X: ch.X, Y: ch.Y + 1, Alive: true, ObjectOrder: 20}
	second := &Monster{ID: "second-summon-target", MasterID: ch.ID, MapID: ch.MapID, X: ch.X + 1, Y: ch.Y, Alive: true, ObjectOrder: 10}
	w.mu.Lock()
	for id := range w.monsters {
		delete(w.monsters, id)
	}
	w.monsters[observer.ID] = observer
	w.monsters[first.ID] = first
	w.monsters[second.ID] = second
	target, ok := w.findClosestSummonedMonsterTargetLocked(observer, nil, 6)
	w.mu.Unlock()
	if !ok || target.ID != first.ID {
		t.Fatalf("equal-distance summoned target = %+v, found=%t, want %q", target, ok, first.ID)
	}
}

func TestSummonedTargetSkipsPendingDeathMonster(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	observer := &Monster{ID: "summon-target-pending", MapID: ch.MapID, X: ch.X, Y: ch.Y, ViewRange: 6}
	target := &Monster{ID: "pending-summon-target", MasterID: ch.ID, MapID: ch.MapID, X: ch.X + 1, Y: ch.Y, Alive: true, PendingDeath: true}
	w.mu.Lock()
	for id := range w.monsters {
		delete(w.monsters, id)
	}
	w.monsters[observer.ID] = observer
	w.monsters[target.ID] = target
	_, ok := w.findClosestSummonedMonsterTargetLocked(observer, nil, 6)
	w.mu.Unlock()
	if ok {
		t.Fatal("pending-death monster was selected as a summoned target")
	}
}

func TestCentipedeTargetGateUsesVisibleMonsterMembership(t *testing.T) {
	w, ch := newTestWorldCharacter(t)
	observer := &Monster{ID: "centipede-membership", Race: 107, MapID: ch.MapID, X: ch.X, Y: ch.Y, ViewRange: 6}
	ordinary := &Monster{ID: "ordinary-centipede-target", MapID: ch.MapID, X: ch.X + 1, Y: ch.Y, Alive: true}
	summon := &Monster{ID: "summoned-centipede-target", MasterID: ch.ID, MapID: ch.MapID, X: ch.X + 1, Y: ch.Y, Alive: true}
	w.mu.Lock()
	for id := range w.monsters {
		delete(w.monsters, id)
	}
	w.monsters[observer.ID] = observer
	w.monsters[ordinary.ID] = ordinary
	if w.centipedeHasTargetLocked(observer, nil, 6, false) {
		w.mu.Unlock()
		t.Fatal("ordinary monster entered centipede visible target gate")
	}
	w.monsters[summon.ID] = summon
	if !w.centipedeHasTargetLocked(observer, nil, 6, false) {
		w.mu.Unlock()
		t.Fatal("summoned monster did not enter centipede visible target gate")
	}
	w.mu.Unlock()
}

func TestMonsterVisibleSearchDelayUsesReferenceConstructorRanges(t *testing.T) {
	w, _ := newTestWorldCharacter(t)
	tests := []struct {
		race     int
		min, max int
	}{
		{race: 53, min: 1500, max: 3000},
		{race: 81, min: 1500, max: 3000},
		{race: 83, min: 1500, max: 3000},
		{race: 84, min: 1500, max: 3000},
		{race: 85, min: 2500, max: 4000},
		{race: 107, min: 2500, max: 4000},
		{race: 87, min: 3000, max: 3001},
		{race: 93, min: 3000, max: 3001},
		{race: 104, min: 3000, max: 3001},
		{race: 103, min: 2500, max: 4000},
		{race: 116, min: 2500, max: 4000},
		{race: 117, min: 2500, max: 4000},
		{race: 112, min: 2000, max: 4000},
		{race: 110, min: 2000, max: 4000},
		{race: 111, min: 2000, max: 4000},
		{race: 115, min: 2000, max: 4000},
		{race: 120, min: 2000, max: 4000},
		{race: 100, min: 1500, max: 3000},
		{race: 130, min: 1500, max: 3000},
		{race: 131, min: 1500, max: 3000},
		{race: 132, min: 2500, max: 4000},
		{race: 133, min: 1500, max: 3000},
		{race: 134, min: 1500, max: 3000},
		{race: 201, min: 1500, max: 3000},
		{race: 206, min: 1500, max: 3000},
		{race: 208, min: 1500, max: 3000},
		{race: 209, min: 1500, max: 3000},
		{race: 210, min: 1500, max: 3000},
		{race: 214, min: 1500, max: 3000},
		{race: 215, min: 1500, max: 3000},
		{race: 96, min: 2500, max: 4000},
		{race: 94, min: 1500, max: 3000},
	}
	for _, test := range tests {
		got := w.monsterVisibleSearchDelayMSLocked(&Monster{Race: test.race})
		if got < test.min || got >= test.max {
			t.Fatalf("race %d visible search delay = %d, want [%d,%d)", test.race, got, test.min, test.max)
		}
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
