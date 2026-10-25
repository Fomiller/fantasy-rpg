package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Fomiller/fantasy-rpg/src/internal/inventory"
	"github.com/Fomiller/fantasy-rpg/src/internal/jointoken"
)

func TestParseInstances(t *testing.T) {
	got, err := ParseInstances(" instance-0=http://a:8080/=ws://x/instances/instance-0/ws?a=b \n instance-1=http://b:8080=ws://y ")
	if err != nil {
		t.Fatal(err)
	}
	want := []Instance{
		{ID: "instance-0", Internal: "http://a:8080", Public: "ws://x/instances/instance-0/ws?a=b"},
		{ID: "instance-1", Internal: "http://b:8080", Public: "ws://y"},
	}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("instances = %+v, want %+v", got, want)
	}
	for _, bad := range []string{"instance-0", "instance-0=http://a", "=http://a=ws://b", "a=http://a=ws://a a=http://b=ws://b"} {
		if _, err := ParseInstances(bad); err == nil {
			t.Errorf("%q parsed", bad)
		}
	}
	if got, err := ParseInstances(""); err != nil || len(got) != 0 {
		t.Fatalf("empty = %v, %v", got, err)
	}
}

// directory makes a directory over the ids with statuses set by hand.
func directory(statuses ...InstanceStatus) *Directory {
	var list []Instance
	for _, st := range statuses {
		list = append(list, Instance{ID: st.ID, Internal: "http://" + st.ID, Public: "ws://" + st.ID})
	}
	d := NewDirectory(list)
	for _, st := range statuses {
		if st.Boot != "" {
			d.status[st.ID] = st
		}
	}
	return d
}

func TestReservePicksTheBusiestWithRoom(t *testing.T) {
	now := time.Now()
	d := directory(
		InstanceStatus{ID: "a", Boot: "1", Players: 1, Capacity: 8},
		InstanceStatus{ID: "b", Boot: "1", Players: 6, Capacity: 8},
		InstanceStatus{ID: "c", Boot: "1", Players: 8, Capacity: 8},
		// No boot means it did not answer its last poll.
		InstanceStatus{ID: "down", Players: 7, Capacity: 8},
	)
	if inst, st, ok := d.Reserve([]string{"r1"}, now); !ok || inst.ID != "b" || st.Boot != "1" {
		t.Fatalf("first pick = %v, %v, want b", inst, ok)
	}
	if inst, _, _ := d.Reserve([]string{"r2"}, now); inst.ID != "b" {
		t.Fatalf("second pick = %v, want b, which still has room", inst)
	}
	// b has 6 players and 2 tickets on the way, so it is full.
	if inst, _, _ := d.Reserve([]string{"r3"}, now); inst.ID != "a" {
		t.Fatalf("third pick = %v, want a", inst)
	}
}

// A party goes to one instance with room for all of them, or nowhere.
func TestReserveKeepsAPartyTogether(t *testing.T) {
	now := time.Now()
	d := directory(
		InstanceStatus{ID: "a", Boot: "1", Players: 1, Capacity: 8},
		InstanceStatus{ID: "b", Boot: "1", Players: 6, Capacity: 8},
	)
	if inst, _, ok := d.Reserve([]string{"p1", "p2", "p3"}, now); !ok || inst.ID != "a" {
		t.Fatalf("party of 3 = %v, %v, want a, the only one with room for all", inst, ok)
	}
	if _, _, ok := d.Reserve(make([]string, 9), now); ok {
		t.Fatal("a party bigger than any instance got a pick")
	}
}

// A ticket stops counting once its join shows in /status, or once its token
// has expired.
func TestReserveCountsTicketsOnce(t *testing.T) {
	now := time.Now()
	d := directory(InstanceStatus{ID: "a", Boot: "1", Capacity: 2})
	d.Reserve([]string{"r1"}, now)
	d.Reserve([]string{"r2"}, now)
	if _, _, ok := d.Reserve([]string{"r3"}, now); ok {
		t.Fatal("picked an instance with two tickets out and room for two")
	}
	d.status["a"] = InstanceStatus{ID: "a", Boot: "1", Players: 1, Capacity: 2, Runs: []string{"r1"}}
	if _, _, ok := d.Reserve([]string{"r3"}, now); ok {
		t.Fatal("r1 joined, r2 is on the way, so there is no room")
	}
	if _, _, ok := d.Reserve([]string{"r4"}, now.Add(jointoken.Lifetime)); !ok {
		t.Fatal("r2 and r3 expired, so there is room")
	}
}

func TestDeadLease(t *testing.T) {
	now := time.Now()
	d := directory(
		InstanceStatus{ID: "a", Boot: "1", Capacity: 8, Runs: []string{"live"}},
		InstanceStatus{ID: "down", Capacity: 8},
	)
	old, young := now.Add(-2*deadLeaseAge), now.Add(-time.Second)
	for _, tc := range []struct {
		name  string
		lease inventory.Lease
		dead  bool
	}{
		{"no lease", inventory.Lease{}, false},
		{"live run", inventory.Lease{RunID: "live", Instance: "a@1", Since: old}, false},
		{"restarted", inventory.Lease{RunID: "live", Instance: "a@0", Since: young}, true},
		{"run gone, token may still land", inventory.Lease{RunID: "gone", Instance: "a@1", Since: young}, false},
		{"run gone long ago", inventory.Lease{RunID: "gone", Instance: "a@1", Since: old}, true},
		{"instance not answering", inventory.Lease{RunID: "x", Instance: "down@1", Since: old}, false},
		{"instance not listed", inventory.Lease{RunID: "x", Instance: "gone@1", Since: young}, true},
	} {
		if got := d.DeadLease(tc.lease, now); got != tc.dead {
			t.Errorf("%s: dead = %v, want %v", tc.name, got, tc.dead)
		}
	}
	var none *Directory
	if none.DeadLease(inventory.Lease{RunID: "x", Instance: "a@0"}, now) {
		t.Error("no directory cleared a lease")
	}
}

// Poll keeps what each instance said, and forgets one that stops answering
// or answers as someone else.
func TestPoll(t *testing.T) {
	var answer InstanceStatus
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/status" || answer.ID == "" {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(answer)
	}))
	defer srv.Close()
	d := NewDirectory([]Instance{{ID: "a", Internal: srv.URL, Public: "ws://a"}})
	ctx := context.Background()

	answer = InstanceStatus{ID: "a", Boot: "1", Players: 3, Capacity: 8, Runs: []string{"r"}}
	d.Poll(ctx)
	if st := d.status["a"]; st.Boot != "1" || st.Players != 3 {
		t.Fatalf("status = %+v", st)
	}
	answer = InstanceStatus{ID: "b", Boot: "1"}
	d.Poll(ctx)
	if _, ok := d.status["a"]; ok {
		t.Fatal("kept a status from an instance answering as another")
	}
	answer = InstanceStatus{ID: "a", Boot: "2", Capacity: 8}
	d.Poll(ctx)
	answer = InstanceStatus{}
	d.Poll(ctx)
	if _, ok := d.status["a"]; ok {
		t.Fatal("kept a status from an instance that stopped answering")
	}
}
