package world

import (
	"testing"

	"openmir2/internal/storage"
)

type pickupSyncCall struct {
	name string
	item storage.UserItem
}

type pickupSyncRecorder struct {
	calls []pickupSyncCall
}

func (r *pickupSyncRecorder) UpdateClient(storage.Character) {
	r.calls = append(r.calls, pickupSyncCall{name: "update"})
}

func (r *pickupSyncRecorder) BroadcastDropHide(storage.Character, string) {
	r.calls = append(r.calls, pickupSyncCall{name: "broadcast-hide"})
}

func (r *pickupSyncRecorder) SendDropHide(storage.Character, string) {
	r.calls = append(r.calls, pickupSyncCall{name: "self-hide"})
}

func (r *pickupSyncRecorder) SendGoldChanged(storage.Character, int) {
	r.calls = append(r.calls, pickupSyncCall{name: "gold"})
}

func (r *pickupSyncRecorder) SendBagAddItem(_ storage.Character, item storage.UserItem) {
	r.calls = append(r.calls, pickupSyncCall{name: "bag", item: item})
}

func (r *pickupSyncRecorder) SendWeightChanged(storage.Character) {
	r.calls = append(r.calls, pickupSyncCall{name: "weight"})
}

func TestApplyPickupSyncOrdersItemPickupNotifications(t *testing.T) {
	recorder := &pickupSyncRecorder{}
	item := storage.UserItem{ItemID: "药品", MakeIndex: 7}
	ApplyPickupSync(recorder, PickupResult{
		Character:  storage.Character{ID: "picker"},
		Drop:       GroundDrop{ID: "drop-1"},
		AddedItems: []storage.UserItem{item},
	})

	want := []string{"update", "weight", "self-hide", "bag"}
	if len(recorder.calls) != len(want) {
		t.Fatalf("notification calls = %+v, want %v", recorder.calls, want)
	}
	for i, call := range recorder.calls {
		if call.name != want[i] {
			t.Fatalf("notification call %d = %q, want %q", i, call.name, want[i])
		}
	}
	if recorder.calls[3].item != item {
		t.Fatalf("bag item = %+v, want %+v", recorder.calls[3].item, item)
	}
}
