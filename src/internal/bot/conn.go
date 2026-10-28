package bot

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/proto"

	gamev1 "github.com/Fomiller/fantasy-rpg/gen/go/game/v1"
	"github.com/Fomiller/fantasy-rpg/src/internal/protocol"
)

// conn is one socket, read on its own goroutine so the bot can tick while
// it waits.
type conn struct {
	ws    *websocket.Conn
	where string
	m     *Metrics
	msgs  chan *gamev1.ServerMessage
	// err is why msgs closed. Read it only after msgs is drained.
	err error
}

func dial(ctx context.Context, rawURL, via, where string, m *Metrics) (*conn, error) {
	u, host, err := rewrite(rawURL, via)
	if err != nil {
		return nil, err
	}
	ws, _, err := websocket.Dial(ctx, u, &websocket.DialOptions{Host: host})
	if err != nil {
		return nil, err
	}
	// Town snapshots carry every player, so they outgrow the 32 KiB default.
	ws.SetReadLimit(1 << 22)
	m.connects.WithLabelValues(where).Inc()
	c := &conn{ws: ws, where: where, m: m, msgs: make(chan *gamev1.ServerMessage, 64)}
	go c.read()
	return c, nil
}

func (c *conn) read() {
	defer close(c.msgs)
	for {
		_, b, err := c.ws.Read(context.Background())
		if err != nil {
			c.err = err
			return
		}
		var msg gamev1.ServerMessage
		if err := proto.Unmarshal(b, &msg); err != nil {
			c.err = fmt.Errorf("decode server message: %w", err)
			c.ws.Close(websocket.StatusUnsupportedData, "bad message")
			return
		}
		c.m.messages.WithLabelValues(c.where, "received").Inc()
		c.msgs <- &msg
	}
}

func (c *conn) send(ctx context.Context, msg *gamev1.ClientMessage) error {
	msg.ProtocolVersion = protocol.Version
	b, err := proto.Marshal(msg)
	if err != nil {
		return err
	}
	if err := c.ws.Write(ctx, websocket.MessageBinary, b); err != nil {
		return err
	}
	c.m.messages.WithLabelValues(c.where, "sent").Inc()
	return nil
}

// close closes from this side, as the client does on a ticket or a run's
// end, and waits for the reader to stop.
func (c *conn) close(reason string) {
	c.ws.Close(websocket.StatusNormalClosure, reason)
	for range c.msgs {
	}
}

// rewrite points url at via's scheme and host, and returns the Host header
// that keeps the original routing.
func rewrite(raw, via string) (string, string, error) {
	if via == "" {
		return raw, "", nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", "", err
	}
	v, err := url.Parse(via)
	if err != nil {
		return "", "", err
	}
	host := u.Host
	u.Scheme = strings.NewReplacer("https", "wss", "http", "ws").Replace(v.Scheme)
	u.Host = v.Host
	return u.String(), host, nil
}
