# Cabinet mode

A cabinet is a room where the worker hands out the player slots, like an arcade machine in a shared space: the first
people to arrive play, everyone else watches the same screen and waits for their turn. The coordinator serves a
ready-made page for it, `cabinet.html`, and any other client can use cabinet mode too.

Rooms started without it behave as before: every user picks their own player index.

## How it behaves

- The user who creates the room sets the number of player slots, from 1 to 4. Later users can't change it.
- Each new user gets the lowest free slot (P1 first). When all slots are taken, they become a watcher: they get the
  video and sound, and their input (gamepad, keyboard and mouse) is ignored.
- When a player leaves, the user who has been watching the longest takes over their slot. With nobody watching, the
  slot stays free for the next arrival.
- A slot is kept while the user stays connected. Reloading the page makes a new user: if people were watching, the
  slot goes to the first of them.
- Changing player, saving, loading and recording are refused. A cabinet always starts from a fresh boot, and leaves
  no save behind for the next group.
- As for any room, the game stops when the last user leaves, and the worker is free again.

## The cabinet page

```
https://<your coordinator>/cabinet.html?cabinet=<name>&game=<game name>&players=<1 to 4>
```

| Parameter | Meaning |
|---|---|
| `cabinet` | The cabinet's name, e.g. `acme-lobby-left`. Everyone who opens the same name plays the same game, so pick a globally unique one. It can't contain `___`. Default: `cabinet` |
| `game` | The game: its ROM file name without the extension, as the worker lists it at start. URL-encode it: `Super%20Game%20(U)` |
| `players` | Number of player slots, 1 to 4. Default: 4 |

The room id is `<cabinet>___<game>`.

The page shows the game and nothing else:

- the video fills the frame with black bars, whatever its shape, with sharp pixels. It is never rotated (the main page
  turns itself sideways in a frame taller than it is wide), so it fits in an iframe of any size;
- a badge in the top-left corner: `P1` to `P4` in the player's colour, or `Watching · #2 in line`;
- "Click to play" while a player's page doesn't have the keyboard (in an iframe, until they click it), and "Click for
  sound" while the browser keeps the sound off;
- a `?` in the top-right corner that shows the keys.

Keys: arrows to move, `Z` `X` `C` `V` for A B X Y, `A` `S` for L R, `Enter` for Start, `Shift` for Select (and `;` `'`
`.` `/` for L2 R2 L3 R3). The shortcuts of the main page (quit, save, load, player choice, help...) do nothing here.
A gamepad works too, buttons by position (the bottom button is B, as on a SNES pad), and its left stick works as a
d-pad.

## Using it from your own client

Everything goes through the usual messages; cabinet mode only adds a field to the game start, and a message on the
data channel.

**Connect** to the coordinator with the room id: `/ws?room_id=<id>___<game name>`. Everyone at the same cabinet must
use the same id, so the coordinator sends them to the same worker. The part after `___` is the game name, as in the
deep links of the main page; the room id is refused without it.

**Start the game** with `players` set (packet type 104):

```json
{"t": 104, "p": {"room_id": "lobby___Super Game", "game_name": "Super Game", "players": 2}}
```

`players` only counts in the request that creates the room. Values above 4 are lowered to 4, and 0 (or no value)
makes an ordinary room.

**Read your slot** in the game start response:

```json
{"t": 104, "p": {"av": null, "roomId": "lobby___Super Game", "kb_mouse": false, "slot": {"index": 1}}}
```

- `index` is the player slot, from 0: `{"index": 1}` is P2.
- A watcher has `index` -1 and `queue`, their place in line from 1: `{"index": -1, "queue": 2}` is second in line.

**Follow the changes** on the `data` data channel. Whenever slots move (someone left), each user in the cabinet gets
their own slot again, as JSON bytes:

```json
{"t": 151, "p": {"index": 0}}
```

Send input as usual. There is no need to stop sending it while watching: the worker drops it.

**Refused requests**: changing player (108) gets no answer, and saving (106), loading (107) and recording (110) get
`"err"`.
