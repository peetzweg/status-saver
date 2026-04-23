# status-saver

Archiver daemon for WhatsApp Status posts (the 24-hour story feature) from
your contacts. Runs headless on a Linux server, pairs once via Multi-Device
against a dedicated secondary WhatsApp account, and from then on stores every
incoming status broadcast (photo, video, text + captions) to disk.

> Status: alpha, personal tool. Uses [whatsmeow](https://github.com/tulir/whatsmeow),
> an unofficial reimplementation of the WhatsApp Multi-Device protocol. This is
> a ToS grey area: passive read-only access from a secondary number has
> historically been low-risk, but there is no guarantee. Do not use your
> primary number with this.

> [!IMPORTANT]
> **There is no reliable way to fetch statuses posted before the daemon
> started.** We only capture statuses live, while connected. If the daemon
> is offline for N minutes, expect to lose every status posted in those N
> minutes. WhatsApp Desktop *can* backfill prior statuses on reopen, which
> proves a server-side mechanism exists, but it is not reverse-engineered
> in any open-source WhatsApp library (whatsmeow, Baileys, whatsapp-web.js).
> See **[Limitations](#limitations)** below and upstream discussion
> [whatsmeow#1033](https://github.com/tulir/whatsmeow/discussions/1033).
> The only mitigation is **24/7 daemon uptime**.

---

## Limitations

Read this before deploying. These are not bugs — they are real constraints
imposed by the WhatsApp protocol and the state of open-source tooling.

### 1. No server-side backfill of prior statuses

When the daemon starts after a period of downtime, statuses posted during
that downtime are **permanently lost** (from our perspective). Concretely:

- The server's `<offline count=N>` replay mechanism empirically reports
  `count=0` for status@broadcast. Statuses are not queued for offline
  replay the way direct messages are.
- The phone's `HistorySync` push only reliably fires on first pairing
  (type `INITIAL_STATUS_V3`), not on subsequent reconnects.
- On-demand `HistorySyncOnDemand` peer requests to the phone usually get
  ACKed but return no data. The code still fires it at startup as a
  best-effort probe, but don't count on it.
- There is no discovered IQ or notification that asks the **server** for
  the list of currently-active statuses. WhatsApp Desktop sends one, but
  it has not been reverse-engineered into any open library. Upstream
  tracking issue: [whatsmeow#1033](https://github.com/tulir/whatsmeow/discussions/1033).

Operational consequence: **minimize daemon downtime.** Plan deploys during
low-post hours. Any multi-hour outage loses posts from that window, and
statuses expire 24h after posting, so nothing recovers them after expiry.

### 2. Phone must be online for first pairing and for fallback paths

- First pairing (QR scan) requires the phone to be online.
- The opportunistic `HistorySync` / `HistorySyncOnDemand` paths also require
  the phone to be online.
- Live capture itself does **not** require the phone. Once paired, the
  daemon receives statuses directly from WhatsApp's server.

### 3. ToS and account risk

- whatsmeow is an unofficial client. Using it technically violates
  WhatsApp's Terms of Service.
- Passive read-only use from a dedicated secondary number has historically
  been low-risk, but there is no guarantee. Do not pair your primary number.
- A banned session shows up as `events.LoggedOut`; the daemon exits with
  status 1 and systemd is configured to **not** auto-restart (requires
  manual re-pair).

### 4. Status types and features we don't handle yet

- Voice-note statuses (audio messages posted as status): skipped in the
  classifier (no handler, falls through to `kindNone`). Add when needed.
- Revoke/delete notifications for previously captured statuses: we log and
  skip them. The original file stays on disk as a historical record.
- Multi-account: one paired secondary number per daemon instance. To
  archive multiple accounts, run separate daemon instances with separate
  config and state directories.

### 5. No decryption of already-expired media

Status media on WhatsApp's CDN is only retrievable while the status is
still active (within its 24h window). If the daemon receives a message
event late (e.g. through a sluggish HistorySync batch) and the media has
already expired server-side, `client.Download` will fail. We log the
error and skip the post; the JSON sidecar still lands on disk with
`media_path` empty.

If you have information about how WhatsApp Desktop fetches prior statuses
and want to help close this gap, comment on
[issue #1](https://github.com/peetzweg/status-saver/issues/1) — it tracks
this limitation with repro details and everything we've already tried.

---

## What it does

When a contact posts a status, WhatsApp delivers it to every linked device of
every viewer as a normal end-to-end encrypted message — addressed to the
special JID `status@broadcast`. `status-saver` keeps a persistent Multi-Device
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
 |  status-saver daemon (systemd, 24/7)                 |
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
|-- status-saver/          # long-running daemon
|-- status-saver-pair/     # interactive QR pairing, runs exactly once
`-- status-saver-rotate/   # oneshot, triggered by the systemd timer

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

**Bottom line: the only reliable way to archive every status is to never be
offline when one is posted.** Every "catch-up" path below is best-effort and
each has been empirically observed to fail. If the daemon is down for N
minutes, expect to lose whatever statuses arrived in those N minutes.

Four opportunistic delivery paths feed the archive pipeline:

1. **Live events** — status arrives over the open WebSocket while the
   daemon is connected. Archived immediately. The **only path that works
   deterministically.**
2. **Server offline replay** (`events.OfflineSyncPreview` / `OfflineSyncCompleted`)
   — during short disconnects the server buffers events for us and replays
   them on reconnect. Works for direct/group messages. Empirically
   **observed to report `count=0` for status@broadcast** — the server
   doesn't seem to queue statuses for offline replay.
3. **Phone-pushed history sync** (`events.HistorySync`) — the phone may
   proactively push historical conversation batches on first-pair or
   subsequent reconnects. `INITIAL_STATUS_V3` history-sync type covers
   statuses. In practice the phone only sends this on first pairing, not
   on subsequent daemon restarts.
4. **On-demand peer request** — 5 seconds after connect, the daemon sends
   a `HistorySyncOnDemand` peer message to the phone asking for the last
   50 status posts. The phone ACKs the peer message but frequently sends
   no response. Requires the phone to be online and willing.

**Why this is such a mess:** WhatsApp Desktop can fully quit, have statuses
post during its downtime, then reopen and display them — so a server-driven
mechanism exists. But none of the open-source WhatsApp libraries
(whatsmeow, Baileys, whatsapp-web.js, mautrix-whatsapp) have reverse-engineered
it. The gap is tracked at
[whatsmeow Discussion #1033](https://github.com/tulir/whatsmeow/discussions/1033)
("Full History Sync on reconnection") — filed and unanswered by maintainers.
Fixing this would take packet-capturing WhatsApp Web/Desktop traffic, which
is a substantial side project.

**Operational consequences:**

- **Run the daemon 24/7.** Every other recommendation is downstream of this.
- Minimize restart windows. Plan deploys to coincide with times your
  contacts don't typically post (very early morning).
- Statuses expire 24h after posting; a long outage (hours) loses everything
  posted in that window with no recovery path.
- The on-demand peer request is logged as best-effort. Don't count on it.

If you discover the actual IQ / notification that WhatsApp Web uses,
please open an issue or PR — both against this repo and upstream at
whatsmeow. Would be a genuinely useful contribution to the ecosystem.

## Requirements

- Linux server (tested on Ubuntu/Debian; systemd).
- Go 1.25+
- C compiler (required by `github.com/mattn/go-sqlite3` through CGO).
- One dedicated secondary WhatsApp account on a phone (needed once to scan
  the QR code).

## Building

```
make build      # all three binaries into ./bin
make test       # unit tests with race detector
make lint       # gofmt + vet + golangci-lint (if installed)
make help       # list all targets
```

Under the hood `make build` runs `CGO_ENABLED=1 go build -o bin/ ./cmd/...`
which compiles every binary in one go. If you prefer raw Go commands or
don't have `make` available, that one-liner works directly from the repo
root — no need to invoke `go build` three times.

Why CGO: the SQLite driver is a C binding (`mattn/go-sqlite3`). On
Debian/Ubuntu, `apt install build-essential` gives you the `gcc` it needs.
If CGO is unavailable on the target host, swapping in `modernc.org/sqlite`
(pure Go) is a small patch — tracked at
[issue #12](https://github.com/peetzweg/status-saver/issues/12).

## Configuring

```yaml
# /etc/status-saver/config.yaml
data_dir:       /var/lib/status-saver/data
session_db:     /var/lib/status-saver/session.db
index_db:       /var/lib/status-saver/index.db
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
sudo -u status-saver status-saver-pair --config /etc/status-saver/config.yaml
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
the phone never confirmed. Running `status-saver-pair` again will just
print "already paired — pass --force to delete the session and re-pair".

Force a fresh pair:

```
status-saver-pair --config ./config.yaml --force
```

`--force` deletes `session.db` and starts over. Scan the QR again.

## Running

### Via systemd (recommended)

See **`deploy/INSTALL.md`** for the full setup: user, binaries, config,
service, and timer. Short version:

```
sudo systemctl enable --now status-saver.service
sudo systemctl enable --now status-saver-rotate.timer
```

### Manually (for debugging)

```
./bin/status-saver --config ./config.yaml
```

Stop with `Ctrl-C`. Shutdown is clean (whatsmeow disconnect, SQLite flush).

## On-disk data format

Flat layout — one folder per contact, with the date/time baked into each
filename so posts still sort chronologically within a contact.

```
/var/lib/status-saver/data/
`-- PhilipP_4915112345678/                 # <push_name>_<jid.user>
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
  "sender_jid": "4915112345678@s.whatsapp.net",
  "push_name": "PhilipP",
  "received_at": "2026-04-23T14:30:12+02:00",
  "media_path": "/var/lib/.../PhilipP_4915112345678/2026-04-23_143012_3EB0A9B8C7D6E5F4.jpg",
  "mimetype": "image/jpeg",
  "caption": "Optional picture caption"
}
```

## Retention

The `status-saver-rotate.timer` unit calls `status-saver-rotate` daily at 04:00:

1. Any file under `<dataDir>/<contact>/` whose name starts with a date prefix
   older than `retention_days` is deleted.
2. Contact folders that end up empty afterwards are removed.
3. `seen_messages` rows with `received_at < cutoff` are pruned from `index.db`.

Files that don't start with `YYYY-MM-DD_` are left alone, so hand-dropped
notes or manual archives inside a contact folder are safe.

`retention_days: 0` disables pruning completely.

Trigger a run manually:

```
sudo systemctl start status-saver-rotate.service
```

## Observability

```
journalctl -u status-saver -f              # live daemon log
journalctl -u status-saver-rotate -e       # last rotation run
systemctl list-timers status-saver-rotate  # next scheduled run
```

Interesting log fields (zerolog, with `mod=wa` / `mod=status` / ...):

- `"status archived"` — a new post just landed on disk
- `"duplicate status, skipping"` — dedup worked via index.db
- `"whatsapp disconnected (will auto-reconnect)"` — transient, no action needed
- `"whatsapp logged out — session invalid"` — terminal, exit 1, re-pair required

### HTTP endpoints (optional)

Set `metrics_addr: "127.0.0.1:9090"` in the config to expose:

- `GET /health` → `200 ok` when the WhatsApp session is connected,
  `503 not connected` otherwise. Ideal for a systemd / uptime probe.
- `GET /metrics` → Prometheus text exposition format with:
  - `statussaver_archived_total` (counter)
  - `statussaver_errors_total` (counter)
  - `statussaver_connected` (gauge, 0/1)
  - `statussaver_last_archived_timestamp_seconds` (gauge, unix time)
  - `statussaver_uptime_seconds` (gauge)

Endpoints are **unauthenticated** — bind only to `127.0.0.1` (or behind
an auth proxy). Leave `metrics_addr` empty to disable the listener
entirely (default).

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

1. `status-saver-pair` → scan QR with the test phone.
2. Start `status-saver`; wait for `daemon started — awaiting status broadcasts`.
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

## Releases & contributing

### Installing a release

Tagged releases ship as a single tarball on the GitHub Releases page
containing all three binaries plus the systemd units, example config,
INSTALL.md, and LICENSE. Download `status-saver_<version>_linux_amd64.tar.gz`,
verify against `checksums.txt`, extract, and follow `deploy/INSTALL.md`.

Only Linux amd64 is built for 0.x releases. For other platforms (macOS,
Linux arm64), build from source as described above — `go build` handles
both natively when CGO is available.

### How releases are cut (for maintainers)

This repo uses the same bot-driven flow as Changesets in the TS world,
adapted to Go's ecosystem:

1. **Contributors open PRs with conventional-commit titles**:
   `feat(wa): add foo`, `fix(storage): bar`, `docs: baz`, etc.
   The `PR title` check blocks merge if the title doesn't parse.
2. **On every push to `main`**, `release-please` maintains a rolling
   **Release PR** titled `chore(main): release X.Y.Z`. Its diff shows
   the CHANGELOG entries that would ship and the version bump.
3. **Merge the Release PR** when you're ready to cut. release-please
   then creates a git tag (`vX.Y.Z`) + empty GitHub Release.
4. **goreleaser fires on the same workflow run**, builds the binaries,
   packages the tarball, and uploads it + a `checksums.txt` as release
   assets.

Version bumping follows semver with pre-1.0 conventions:

- `feat:` → minor bump (0.1.0 → 0.2.0)
- `fix:` → patch bump (0.1.0 → 0.1.1)
- `chore:` / `docs:` / `test:` / `ci:` / `refactor:` → no bump, included
  in the changelog "Other changes" section

### Commit message reference

Format: `<type>(<optional-scope>): <short subject>`. Body is optional.
A `BREAKING CHANGE:` footer forces a major bump (once we're past 1.0.0).
Recognized types are defined in
[`.github/workflows/pr-title.yml`](./.github/workflows/pr-title.yml).

## License

Mozilla Public License 2.0 — see [`LICENSE`](./LICENSE).

This repo uses [whatsmeow](https://github.com/tulir/whatsmeow) (MPL-2.0)
and is inspired by [mautrix-whatsapp](https://github.com/mautrix/whatsapp)
(same author, AGPL-3.0). MPL-2.0 is the minimum required because of the
whatsmeow dependency.
