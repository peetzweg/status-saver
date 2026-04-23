# story-saver

Archiver daemon for WhatsApp Status posts (the 24-hour story feature) from
your contacts. Runs headless on a Linux server, pairs once via Multi-Device
against a dedicated secondary WhatsApp account, and from then on stores every
incoming status broadcast (photo, video, text + captions) to disk.

> Status: alpha, personal tool. Uses [whatsmeow](https://github.com/tulir/whatsmeow),
> an unofficial reimplementation of the WhatsApp Multi-Device protocol. This is
> a ToS grey area: passive read-only access from a secondary number has
> historically been low-risk, but there is no guarantee. Do not use your
> primary number with this.

---

## What it does

When a contact posts a status, WhatsApp delivers it to every linked device of
every viewer as a normal end-to-end encrypted message — addressed to the
special JID `status@broadcast`. `story-saver` keeps a persistent Multi-Device
session open, filters every incoming message on that JID, decrypts any media
attachments (AES-CBC/HMAC via whatsmeow), and writes everything to disk along
with a metadata sidecar.

The persistent-daemon model is deliberate instead of "connect once a day at
04:00": WhatsApp does not document how much status backfill a freshly
reconnected client receives. A 24/7 client catches everything. A separate
timer at 04:00 only does housekeeping.

## What it does not do

- It does not post your own statuses (purely a reader).
- It does not archive chat messages or group messages — only `status@broadcast`.
- No multi-account support (exactly one secondary number for now).
- No web UI for browsing — files sit raw on disk.
- No official WhatsApp support (the Cloud API can't read statuses at all).

## Architecture

```
 +-----------------------------------------------------+
 |  story-saver daemon (systemd, 24/7)                 |
 |  +---------------+   +---------------------------+  |
 |  | whatsmeow     |-->| status message handler    |  |
 |  | Client (MD)   |   | (filter status@broadcast) |  |
 |  +-------+-------+   +-------------+-------------+  |
 |          |                         v                |
 |   session.db              downloader + writer       |
 |  (SQLite, whatsmeow)      |- media -> disk          |
 |                           `- metadata -> index.db   |
 +-----------------------------------------------------+
 +-----------------------------------------------------+
 |  systemd timer 04:00 daily                          |
 |  `- rotate: prune >retention_days, clean index      |
 +-----------------------------------------------------+
```

### Package layout

```
cmd/
|-- story-saver/          # long-running daemon
|-- story-saver-pair/     # interactive QR pairing, runs exactly once
`-- story-saver-rotate/   # oneshot, triggered by the systemd timer

internal/
|-- config/     # YAML loader + validation
|-- logging/    # zerolog console output
|-- wa/         # whatsmeow client wrapper + status@broadcast handler
|-- storage/    # on-disk path scheme + SQLite dedup index
`-- rotate/     # retention walker

deploy/systemd/ # .service + .timer units, see deploy/INSTALL.md
```

### Data flow for one status post

1. whatsmeow delivers `*events.Message` with `Info.Chat == types.StatusBroadcastJID`.
2. `wa.StatusHandler.archive()` checks `index.db` for a duplicate — restarts are idempotent.
3. For media (image/video): `client.Download(ctx, msg)` -> AES-decrypted bytes, written to `<dataDir>/<sender>/<YYYY-MM-DD_HHMMSS>_<msgid>.<ext>`.
4. For text-only: `<...>.txt` with the plaintext.
5. Always: `<...>.json` with `sender_jid`, `push_name`, `received_at`, `caption`, `mimetype`.
6. `(msg_id, sender_jid)` gets a row in `index.db` — prevents double-processing.

### Catching up on missed statuses

Four delivery paths feed the same archive pipeline:

1. **Live events** — a status post arrives over the open WebSocket while the
   daemon is connected. Archived immediately. Primary path; cover it well and
   the other three stop mattering.
2. **Server offline replay** (`events.OfflineSyncPreview` / `OfflineSyncCompleted`)
   — during short disconnects, the WhatsApp server buffers events and replays
   them on reconnect. whatsmeow surfaces these through the normal
   `events.Message` pipeline, so the StatusHandler picks them up with no
   extra code. Watch for `server will replay events missed during downtime
   messages=N`.
3. **On-demand history request** (new) — 5 seconds after startup the daemon
   sends a peer-message `HistorySyncOnDemand` request to the phone, asking
   for the last 50 status posts. Response arrives as `events.HistorySync`
   with `type=ON_DEMAND`. This catches statuses posted while the daemon
   was down. Requires the phone to be online.
4. **Phone-pushed history sync** (`events.HistorySync`) — the paired phone
   also proactively sends batches of historical conversation data at its
   own discretion, typically on first-pair or occasional reconnects. Same
   handler, same archive path, dedup via `index.db` keeps it idempotent.

**Hard limits** — statuses can still be lost despite all of the above:

- **Statuses expire 24h after posting.** If a post is older than 24h when
  the daemon asks for it, it's gone. Nothing can recover it.
- **Phone has to be online** for paths 3 and 4. The on-demand request is a
  peer message to your device; if the phone is powered off or without
  network, it can't respond.
- **Server offline replay is short-term.** Empirically, disconnects measured
  in minutes get replayed; hours may not.
- **No synchronous "active statuses" API.** The on-demand request asks the
  phone, not the server — so you're bounded by what the phone has in its
  local cache, and timing depends on how quickly it responds.

Operational consequence: keep the daemon running 24/7. The catch-up paths
are insurance that works best for short gaps (minutes to a few hours).
Long downtimes will still lose data.

## Requirements

- Linux server (tested on Ubuntu/Debian; systemd).
- Go 1.25+
- C compiler (required by `github.com/mattn/go-sqlite3` through CGO).
- One dedicated secondary WhatsApp account on a phone (needed once to scan
  the QR code).

## Building

```
CGO_ENABLED=1 go build -o bin/story-saver        ./cmd/story-saver
CGO_ENABLED=1 go build -o bin/story-saver-pair   ./cmd/story-saver-pair
CGO_ENABLED=1 go build -o bin/story-saver-rotate ./cmd/story-saver-rotate
```

Why CGO: the SQLite driver is a C binding. If CGO is unavailable on the
target host, swapping in `modernc.org/sqlite` (pure Go) is a small patch.

## Configuring

```yaml
# /etc/story-saver/config.yaml
data_dir:       /var/lib/story-saver/data
session_db:     /var/lib/story-saver/session.db
index_db:       /var/lib/story-saver/index.db
retention_days: 90        # 0 = never delete
rotation_hour:  4         # informational; the actual schedule lives in the timer
log_level:      info      # trace|debug|info|warn|error
alert_webhook:  ""        # optional: POST on LoggedOut (ntfy.sh, Slack webhook, ...)
```

All paths must be absolute. Parent directories are created automatically
with mode `0750`.

## Pairing (one-off)

The daemon will not start unless a device is paired. Pairing is interactive
and needs your phone:

```
sudo -u story-saver story-saver-pair --config /etc/story-saver/config.yaml
```

QR codes scroll by in the terminal. On the secondary phone:
**WhatsApp -> Settings -> Linked Devices -> Link a Device** -> scan.

After the scan the binary logs
`pair-success received; keeping connection open for post-pair handshake`
and stays connected for a 30-second grace period. **Do not kill it during
that window** — the phone app needs that time to complete app-state key
sync and contact sync. If the binary exits earlier, the phone gets stuck
on "pairing…" and the link is effectively broken.

After the grace period it logs `pairing complete — session stored,
disconnecting` and exits.

Sessions remain valid for weeks to months. You only need to re-pair if
WhatsApp invalidates the session (you remove the device from the app,
account ban, etc.) — the daemon exits with status 1 in that case.

### Recovering from a half-broken pair

If an earlier pair attempt exited too early (pre-fix, or network glitch,
or the grace period was cut short), `session.db` will look "paired" but
the phone never confirmed. Running `story-saver-pair` again will just
print "already paired — pass --force to delete the session and re-pair".

Force a fresh pair:

```
story-saver-pair --config ./config.yaml --force
```

`--force` deletes `session.db` and starts over. Scan the QR again.

## Running

### Via systemd (recommended)

See **`deploy/INSTALL.md`** for the full setup: user, binaries, config,
service, and timer. Short version:

```
sudo systemctl enable --now story-saver.service
sudo systemctl enable --now story-saver-rotate.timer
```

### Manually (for debugging)

```
./bin/story-saver --config ./config.yaml
```

Stop with `Ctrl-C`. Shutdown is clean (whatsmeow disconnect, SQLite flush).

## On-disk data format

Flat layout — one folder per contact, with the date/time baked into each
filename so posts still sort chronologically within a contact.

```
/var/lib/story-saver/data/
`-- Alice_49123456789/                 # <push_name>_<jid.user>
    |-- 2026-04-23_143012_3EB0A9B8C7D6E5F4.jpg
    |-- 2026-04-23_143012_3EB0A9B8C7D6E5F4.json
    |-- 2026-04-23_143155_3EB0F1E2D3C4B5A6.mp4
    |-- 2026-04-23_143155_3EB0F1E2D3C4B5A6.json
    |-- 2026-04-24_164820_3EB012AB34CD56EF.txt   # text-only status
    `-- 2026-04-24_164820_3EB012AB34CD56EF.json
```

File stem: `<YYYY-MM-DD>_<HHMMSS>_<msgid>`. Same stem for the media (or
`.txt`) and the `.json` sidecar, so they're always grouped when sorted
alphabetically.

JSON sidecar schema (all fields optional except `msg_id`, `sender_jid`,
`received_at`):

```json
{
  "msg_id": "3EB0A9B8C7D6E5F4",
  "sender_jid": "49123456789@s.whatsapp.net",
  "push_name": "Alice",
  "received_at": "2026-04-23T14:30:12+02:00",
  "media_path": "/var/lib/.../Alice_49123456789/2026-04-23_143012_3EB0A9B8C7D6E5F4.jpg",
  "mimetype": "image/jpeg",
  "caption": "Optional picture caption"
}
```

## Retention

The `story-saver-rotate.timer` unit calls `story-saver-rotate` daily at 04:00:

1. Any file under `<dataDir>/<contact>/` whose name starts with a date prefix
   older than `retention_days` is deleted.
2. Contact folders that end up empty afterwards are removed.
3. `seen_messages` rows with `received_at < cutoff` are pruned from `index.db`.

Files that don't start with `YYYY-MM-DD_` are left alone, so hand-dropped
notes or manual archives inside a contact folder are safe.

`retention_days: 0` disables pruning completely.

Trigger a run manually:

```
sudo systemctl start story-saver-rotate.service
```

## Observability

```
journalctl -u story-saver -f              # live daemon log
journalctl -u story-saver-rotate -e       # last rotation run
systemctl list-timers story-saver-rotate  # next scheduled run
```

Interesting log fields (zerolog, with `mod=wa` / `mod=status` / ...):

- `"status archived"` — a new post just landed on disk
- `"duplicate status, skipping"` — dedup worked via index.db
- `"whatsapp disconnected (will auto-reconnect)"` — transient, no action needed
- `"whatsapp logged out — session invalid"` — terminal, exit 1, re-pair required

## Development

```
go test ./...           # storage and rotation unit tests
go vet ./...
go build ./...
```

The `wa/` package has no unit tests — testing it would require a fake
WhatsApp server. Instead, validate end-to-end against a real secondary
account (see the smoke test below).

### E2E smoke test

1. `story-saver-pair` → scan QR with the test phone.
2. Start `story-saver`; wait for `daemon started — awaiting status broadcasts`.
3. From a third account (whose number the test phone has saved) post a
   status — an image with a caption, then a video, then a text-only post.
4. Inside ~30s, `data/<poster>/` should contain files named
   `YYYY-MM-DD_HHMMSS_<msgid>.<ext>` plus their `.json` sidecars.
5. Kill and restart the daemon; nothing gets stored twice (dedup via
   `index.db`).
6. Briefly drop the network (`iptables -A OUTPUT -p tcp --dport 443 -j DROP`
   for 60s, then `-D`) → whatsmeow auto-reconnects.

## Security and operational notes

- **Use a dedicated secondary number.** Passive status reads have been
  historically quiet, but every Multi-Device session on a server raises
  ban risk slightly.
- `session.db` holds the device identity. A compromised file lets an
  attacker send WhatsApp messages as your secondary account. The systemd
  unit pins permissions to the dedicated user; keep the file mode tight.
- Captions and sender attribution land in plaintext on disk. Full-disk
  encryption on the server is a hard requirement.
- On `events.LoggedOut` the daemon exits with status 1. The unit sets
  `RestartPreventExitStatus=1`, so systemd will **not** restart the
  daemon — re-pairing is a deliberate manual step.

## License / provenance

Built on the [whatsmeow](https://github.com/tulir/whatsmeow) client
(MPL-2.0). Status broadcast handling is inspired by
[mautrix-whatsapp](https://github.com/mautrix/whatsapp) (same author,
AGPL-3.0). Project licence not yet set — at minimum MPL-2.0 is required
for any public release because of the whatsmeow dependency.
