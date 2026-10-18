package world

import (
	"testing"

	"openmir2/internal/npc"
	"openmir2/internal/storage"
)

func TestDisplayNamesUseReferenceRules(t *testing.T) {
	w := &World{}
	for _, test := range []struct {
		name string
		want string
	}{
		{name: "tester", want: "tester"},
		{name: "tester-1", want: "tester-1"},
		{name: "tester2", want: "tester2"},
		{name: "测试-1", want: "测试-1"},
	} {
		if got := w.CharacterDisplayName(storage.Character{Name: test.name}); got != test.want {
			t.Fatalf("CharacterDisplayName(%q) = %q, want %q", test.name, got, test.want)
		}
	}
	if got := MonsterDisplayName(Monster{Name: "鸡-1", MasterName: "master2"}); got != "鸡(master2)" {
		t.Fatalf("MonsterDisplayName() = %q, want 鸡(master2)", got)
	}
	if got := NPCDisplayName(npc.Entity{Name: "城门卫-01"}); got != "城门卫" {
		t.Fatalf("NPCDisplayName() = %q, want 城门卫", got)
	}
}

func TestMonsterActorIDRemainsStableForNonNumericRuntimeID(t *testing.T) {
	first := Monster{ID: "castle-door:one", ActorID: 500017, ObjectOrder: 17}
	second := Monster{ID: "castle-door:two", ActorID: 500018, ObjectOrder: 18}

	if got := MonsterActorID(first); got != 500017 {
		t.Fatalf("first non-numeric monster actor ID = %d, want 500017", got)
	}
	if got := MonsterActorID(second); got != 500018 {
		t.Fatalf("second non-numeric monster actor ID = %d, want 500018", got)
	}
	if got := MonsterActorID(Monster{ID: "mon-7", ObjectOrder: 17}); got != 100007 {
		t.Fatalf("numeric monster actor ID = %d, want 100007", got)
	}
	first.ObjectOrder = 91
	if got := MonsterActorID(first); got != 500017 {
		t.Fatalf("non-numeric monster actor ID after object-order refresh = %d, want 500017", got)
	}
}
