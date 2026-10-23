package protocol

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"google.golang.org/protobuf/proto"

	gamev1 "github.com/Fomiller/fantasy-rpg/gen/go/game/v1"
)

// The Godot client test reads and writes the same fixtures, so these check
// that Go and GDScript agree on the wire format.
var update = flag.Bool("update", false, "rewrite the Go-owned fixtures")

var testdata = filepath.Join("..", "..", "..", "proto", "testdata")

func TestServerSnapshotFixture(t *testing.T) {
	got, err := EncodeServer(&gamev1.ServerMessage{
		Payload: &gamev1.ServerMessage_Snapshot{Snapshot: &gamev1.WorldSnapshot{
			Tick: 42,
			Entities: []*gamev1.EntityState{
				{Id: 7, Kind: gamev1.EntityKind_ENTITY_KIND_PLAYER, Position: &gamev1.Vec2{X: 1.5, Y: -2}, Health: 100},
				{
					Id: 9, Kind: gamev1.EntityKind_ENTITY_KIND_ENEMY, Position: &gamev1.Vec2{X: 10, Y: 4}, Health: 30, MaxHealth: 90,
					Archetype: "elite", EnemyState: gamev1.EnemyState_ENEMY_STATE_CHARGE, Aim: &gamev1.Vec2{X: 0, Y: -1},
				},
				{Id: 12, Kind: gamev1.EntityKind_ENTITY_KIND_PROJECTILE, Position: &gamev1.Vec2{X: 3, Y: 3}, Velocity: &gamev1.Vec2{X: 320}},
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(testdata, "server_snapshot.bin")
	if *update {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture (run go test -update to create it): %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("server_snapshot.bin is stale, run go test -update")
	}
}

func TestDecodeClientMoveFixture(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(testdata, "client_move.bin"))
	if err != nil {
		t.Fatalf("read fixture (written by just client-test-update): %v", err)
	}
	msg, err := DecodeClient(b)
	if err != nil {
		t.Fatal(err)
	}
	move := msg.GetMove()
	if move == nil {
		t.Fatalf("payload is %T, want move", msg.GetPayload())
	}
	if move.GetSeq() != 3 || move.GetDirection().GetX() != 1 || move.GetDirection().GetY() != -0.5 {
		t.Fatalf("got %v, want seq 3 direction (1, -0.5)", move)
	}
}

func TestDecodeClientRejectsWrongVersion(t *testing.T) {
	b, err := proto.Marshal(&gamev1.ClientMessage{ProtocolVersion: Version + 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeClient(b); err == nil {
		t.Fatal("want error for wrong protocol version")
	}
}
