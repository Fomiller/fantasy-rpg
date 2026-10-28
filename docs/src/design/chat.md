# Chat

Players talk in the town and in caves. Say and yell stay inside one town
shard or one cave. General, Trade and LookingForGroup reach every shard and
cave. A whisper reaches one player anywhere. FOM-262 builds it. It is the
first use of the cluster's NATS from [Scaling](scaling.md).

## Rules that do not bend

- Delivery is at most once. Nothing is kept, and there is no history.
- The server sets the sender's name, from the connection. The client sends
  only the text it typed.
- A line is cleaned on the server. Control and format characters are
  removed, the ends are trimmed, and it is cut at 255 characters. A line
  that is empty after that is dropped.
- The rate limit is per player, on the server that receives the line. Over
  it, the player gets a system message. Nobody is kicked.
- Chat works the same in the town and in a cave.
- NATS carries messages, never state. Nothing here decides who holds an
  item.
- There is no party channel. Parties do not exist.

## Channels

| Command | Channel | Who hears it | Transport |
| -- | -- | -- | -- |
| `/s` or no command | Say | Players within 384 units (6 tiles), same town shard or cave | None |
| `/y` | Yell | Everyone in the same town shard or cave | None |
| `/1` | General | Everyone who has it joined, every shard and cave | `frpg.chat.channel.general` |
| `/2` | Trade | Same | `frpg.chat.channel.trade` |
| `/3` | LookingForGroup | Same | `frpg.chat.channel.lfg` |
| `/w name text` | Whisper | One player, any shard or cave | Request to `frpg.player.<name>` |
| `/r text` | Reply | The last player who whispered you | Same as whisper |

- `/join <channel>` and `/leave <channel>` take `general`, `trade` or
  `lfg`, or `1`, `2` and `3`. All three are joined at the start.
- Speaking in a channel you left gets a system message, and nothing is sent.
- A name is matched without regard to case. A name with a space in it
  cannot be whispered, since the command ends the name at the first space.

## Where the code is

- `src/internal/chat` has the rules: reading a line, cleaning it, the rate
  limit, and the NATS bus. It knows nothing about connections.
- `src/internal/server/chat.go` delivers. It runs on the same loop goroutine
  as the rest of the server, so a client's chat state needs no lock.
- `Lobby` is the world server, and `Server` is the cave. Both embed the same
  chat code. The only difference is who is "here": the town's players or the
  cave's.

## Messages

- `ClientMessage.chat` is `ChatSend { text }`. The server reads the slash
  command, so the Godot client and the bot need no parser.
- `ServerMessage.chat` is `ChatMessage { channel, sender, text }`. The
  channel is one of `say`, `yell`, `general`, `trade`, `lfg`,
  `whisper_from`, `whisper_to` and `system`. For `whisper_to`, the sender is
  the player it went to.
- `ServerMessage.chat_channels` is the channels the player is in. It follows
  the join, and comes again after `/join` and `/leave`.

## Shared channels

- Each server subscribes to the three channel subjects once, when it
  starts.
- A line goes to NATS and comes back through the server's own subscription,
  so the speaker hears it in the same order as everyone else.
- The channel of a message comes from the subject it arrived on, not from
  the payload.
- If NATS is down, publishing fails at once. The speaker gets "Chat is
  unavailable right now." The client does not buffer, and the server does
  not retry. The connection is built with no reconnect buffer, so nothing
  old is sent after a reconnect.

## Whispers

- A player who has a body on a server listens on `frpg.player.<name>`. They
  stop when they leave it for a ticket, a run's end or a disconnect.
- `/w` first looks for the name on its own server. Otherwise it sends a NATS
  request and waits up to 2 s.
- The listener answers `ok` once the player's loop has taken the message.
  No listener means NATS answers "no responders" at once, and the speaker
  sees "name is not online."
- The name is escaped into the subject. A dot, space, `*` or `>` in a name
  becomes `%xx`, so one name cannot listen on another's subject.
- Between leaving one server and joining the next, a player is not
  listening. A whisper in that gap says "not online". It is a matter of
  seconds.

## Joined channels across moves

A player can move four ways: town shard to town shard (`TownTicket`), town
to cave (`InstanceTicket`), cave to town, and a restart of the client. The
state is three booleans, and the choice was where to keep them.

- **In the token and the client.** Chosen.
  - The client keeps the last `ChatChannels` it was sent. It sends the
    channels it left as `JoinRequest.chat_left` on every join. An empty list
    means all three are joined, so a new session and an old client get the
    defaults.
  - The town puts the same set in the join token (`chat_left`). The token is
    signed, so the cave cannot be given a different set than the town had.
  - The cave sends `ChatChannels` on join. The client keeps it, and sends
    it back to the next town.
- **Valkey keyed by name.** Not chosen. The caves have no Valkey today, so
  it would add a second dependency to them for three booleans. It would
  also need a key lifetime and a fail-closed rule of its own.
- **NATS.** Not chosen. Core NATS keeps nothing.

The client is not trusted with anything that matters. The set only chooses
which public channels a player hears, and `/join` does the same. A client
restart forgets it, and the player is back to all three joined.

## Fail closed

Chat follows the store ([Persistence](persistence.md)): a missing setting is
an error, never a quiet fallback.

- `NATS_URL` is read by `world-server` and `instance-server`. Without it
  they refuse to start with "no chat bus".
- `CHAT=local` allows a start with no NATS, the way `STORE=memory` does. Say,
  yell and whispers between players on the same server still work. The
  shared channels say chat is unavailable. The k3d manifest and any local
  run without Compose use it.
- A bad `CHAT` value is an error.
- A NATS that is unreachable at start is retried for 15 s, then the server
  exits.
- After the start, a NATS that goes away only breaks the shared channels and
  cross-server whispers. Say and yell keep working.

## Ops

- The chart sets `NATS_URL` from `nats.url` on the world StatefulSet and the
  instance Fleet. It defaults to `nats://nats.nats.svc:4222`: core NATS, no
  auth, shared with other apps. Every subject starts with `frpg.`.
- `just deps` starts NATS in Compose, and `just dev` and `just instance`
  pass `NATS_URL`. The `full` profile sets it for every server.
- `just net-test` starts a throwaway NATS.
- `chat_messages_total{channel}` counts lines sent through a server, with
  whispers under `whisper`.
- `chat_drops_total{reason}` counts lines not delivered. Reasons:
  `rate_limited`, `unavailable`, `not_online`, `not_joined`, `bad_command`,
  `no_reply`, `empty`.
- The bot leaves all three channels on join, so a load test does not send
  every player's chat to every bot. Bots ignore chat.

## Rate limit

A token bucket per player: a burst of 5 lines, then one more per second.
It counts say, yell, the shared channels, whispers and replies. `/join`,
`/leave` and bad commands are free. The numbers are a guess, in
`src/internal/chat/chat.go`.

## Client

- The chat box is at the bottom left, above the ability bar. Each channel has
  a color, and the box scrolls.
- Enter opens the input. Enter sends. Escape closes it, through the same
  `windows` group that closes the other windows.
- While the input is open, the character does not move and abilities and
  hotkeys do nothing. The keys are polled in Godot, so `main.gd` and
  `player.gd` check `ChatBox.typing`.
- Clicking a sender's name puts `/w name ` in the input.
- Errors and the rate limit show as system lines.

## Not built

- Party channel, until parties exist.
- History, and any delivery guarantee. JetStream would give both, and the
  shared NATS has no JetStream.
- Ignore lists, mutes and profanity filters.
- Names with spaces in `/w`.
