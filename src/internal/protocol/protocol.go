package protocol

import (
	"fmt"

	"google.golang.org/protobuf/proto"

	gamev1 "github.com/Fomiller/fantasy-rpg/gen/go/game/v1"
)

const Version = 1

func DecodeClient(b []byte) (*gamev1.ClientMessage, error) {
	var msg gamev1.ClientMessage
	if err := proto.Unmarshal(b, &msg); err != nil {
		return nil, fmt.Errorf("decode client message: %w", err)
	}
	if msg.GetProtocolVersion() != Version {
		return nil, fmt.Errorf("unsupported protocol version %d, want %d", msg.GetProtocolVersion(), Version)
	}
	return &msg, nil
}

func EncodeServer(msg *gamev1.ServerMessage) ([]byte, error) {
	msg.ProtocolVersion = Version
	b, err := proto.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("encode server message: %w", err)
	}
	return b, nil
}
