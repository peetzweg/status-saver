# status-saver

Archiver daemon for WhatsApp Status posts (the 24-hour story feature) from
your contacts. Runs headless on a Linux server, pairs once via Multi-Device
against a dedicated secondary WhatsApp account, and from then on stores every
incoming status broadcast (photo, video, text + captions) to disk.

> Status: alpha, personal tool. Uses [whatsmeow](https://github.com/tulir/whatsmeow),
> an unofficial reimplementation of the WhatsApp Multi-Device protocol. ToS
> grey area — use a dedicated secondary number, never your primary.

> [!IMPORTANT]
> **Can't backfill statuses posted before the daemon was running.** We only
> capture live, while connected. If the daemon is offline for N minutes, you
> lose whatever arrives during those N minutes. See
> [Limitations](#limitations) for the protocol reason and upstream tracking
> issue [whatsmeow#1033](https://github.com/tulir/whatsmeow/discussions/1033).
> **Mitigation: run the daemon 24/7.**

---

## CLI

Single binary with subcommands:

```
status-saver run       [--config PATH]             # long-running daemon
status-saver pair      [--config PATH] [--force]   # QR pairing, one-off
status-saver rotate    [--config PATH]             # retention prune (cron)
status-saver version
status-saver help
```

Each subcommand accepts `--help` for its own flags.

## Install

Supported targets: **Linux with systemd** (production) and
**macOS with launchd** (development / personal use).

You can either build from source (needs Go 1.25+ and a C compiler) or
download a pre-built tarball from the
[Releases page](https://github.com/peetzweg/status-saver/releases)
(Linux amd64 only today — other platforms need a source build).

### Prerequisites

**Ubuntu / Debian:**

```bash
sudo apt-get update
sudo apt-get install -y build-essential git curl

# Go 1.25+ — apt's golang-go is usually too old on LTS. Pick one:
# a) Snap:
sudo snap install go --classic
# b) Official tarball:
curl -fsSL https://go.dev/dl/go1.25.9.linux-amd64.tar.gz | sudo tar -C /usr/local -xz
echo 'export PATH=$PATH:/usr/local/go/bin' | sudo tee /etc/profile.d/go.sh
source /etc/profile.d/go.sh

go version   # should report go1.25.x
```

`build-essential` provides `gcc`, required by `github.com/mattn/go-sqlite3`
through CGO. Without it, `go build` fails with `C compiler "gcc" not found`.

**macOS:**

```bash
brew install go           # Go 1.25+
xcode-select --install    # for cc (CGO needs it)
```

### Build

```bash
git clone https://github.com/peetzweg/status-saver
cd status-saver
make build                # produces ./bin/status-saver (single binary)
```

Under the hood: `CGO_ENABLED=1 go build -o bin/ ./cmd/...`. `make install`
puts the binary in `$GOBIN` (usually `~/go/bin`) instead — use it if you'd
rather not `install` into `/usr/local/bin` manually.

> Hit `no Go files in <dir>`? You skipped the package path. The repo root
> has no `main.go`; each command lives under `cmd/<name>/`. Either use
> `make build` or include the path: `go build -o bin/ ./cmd/...`.

Verify:

```bash
./bin/status-saver help
./bin/status-saver version
```

### Linux (systemd)

1. Create a dedicated unprivileged user:
   ```bash
   sudo useradd --system --home /var/lib/status-saver --shell /usr/sbin/nologin status-saver
   ```

2. Place the binary and config:
   ```bash
   sudo install -m 0755 bin/status-saver /usr/local/bin/
   sudo mkdir -p /etc/status-saver
   sudo install -m 0640 config.example.yaml /etc/status-saver/config.yaml
   sudo chown root:status-saver /etc/status-saver/config.yaml
   ```

3. Install systemd units:
   ```bash
   sudo install -m 0644 deploy/systemd/status-saver.service        /etc/systemd/system/
   sudo install -m 0644 deploy/systemd/status-saver-rotate.service /etc/systemd/system/
   sudo install -m 0644 deploy/systemd/status-saver-rotate.timer   /etc/systemd/system/
   sudo systemctl daemon-reload
   ```

4. Pair the WhatsApp account (interactive — see [Pairing](#pairing)):
   ```bash
   sudo -u status-saver status-saver pair --config /etc/status-saver/config.yaml
   ```

5. Start the daemon and enable the rotation timer:
   ```bash
   sudo systemctl enable --now status-saver.service
   sudo systemctl enable --now status-saver-rotate.timer
   ```

### macOS (launchd)

> **Untested end-to-end** — treat as "best guess, please report back". Open
> an issue at
> https://github.com/peetzweg/status-saver/issues if something breaks.

Running as a LaunchAgent (user-level) fits the single-user nature of this
tool better than a LaunchDaemon (system-wide).

1. Put the binary and config somewhere stable:
   ```bash
   mkdir -p ~/.local/bin "~/Library/Application Support/status-saver/data"
   cp bin/status-saver ~/.local/bin/
   cp config.example.yaml "~/Library/Application Support/status-saver/config.yaml"
   ```

2. Edit the config to point at absolute paths under
   `~/Library/Application Support/status-saver/`. The daemon does **not**
   expand `~`, so use the full path.

3. Pair (terminal must stay open ~30s after `pair-success` — see
   [Pairing](#pairing)):
   ```bash
   ~/.local/bin/status-saver pair --config "~/Library/Application Support/status-saver/config.yaml"
   ```

4. Install the LaunchAgent plist (template in `deploy/launchd/`; substitute
   your username and real paths):
   ```bash
   cp deploy/launchd/com.github.peetzweg.status-saver.plist \
      ~/Library/LaunchAgents/
   # edit it to replace YOURNAME with your macOS username
   ```

5. Load and start:
   ```bash
   launchctl load ~/Library/LaunchAgents/com.github.peetzweg.status-saver.plist
   launchctl start com.github.peetzweg.status-saver
   ```

6. Follow logs:
   ```bash
   tail -f ~/Library/Logs/status-saver.log
   ```

To stop / unload:
```bash
launchctl unload ~/Library/LaunchAgents/com.github.peetzweg.status-saver.plist
```

**Caveat: App Nap.** macOS may idle the process while the Mac is locked.
The bundled plist sets `LegacyTimers=true` to reduce this but doesn't
eliminate it. For a truly always-on service, run on a Linux server instead
or switch to a LaunchDaemon (requires root).

### Migration from v0.1.x

v0.2.0 consolidates the three binaries (`status-saver`, `status-saver-pair`,
`status-saver-rotate`) into a single `status-saver` with subcommands. To
upgrade:

1. Remove the old binaries:
   ```bash
   sudo rm -f /usr/local/bin/status-saver-pair /usr/local/bin/status-saver-rotate
   ```
2. Re-install the systemd unit files from `deploy/systemd/` — their
   `ExecStart=` lines now include the subcommand argument. If you have
   custom units, update them to `status-saver run` and `status-saver rotate`.
3. Reload: `sudo systemctl daemon-reload && sudo systemctl restart status-saver.service`.
4. CLI invocations take a subcommand now:
   `status-saver pair` / `status-saver rotate` instead of the separate binaries.

### Troubleshooting

- **`daemon keeps exiting with status 1`** — WhatsApp force-logged out the
  session (device removed from the phone, account banned). The unit sets
  `RestartPreventExitStatus=1` so systemd doesn't loop. Re-run
  `status-saver pair` manually.
- **Build fails with `C compiler "gcc" not found`** — missing
  `build-essential` on Ubuntu/Debian, or missing Xcode CLT on macOS.
- **`no Go files in <dir>`** — you ran `go build` without the package path.
  Use `make build` or pass `./cmd/...`.

## Configuration

```yaml
# /etc/status-saver/config.yaml
data_dir:       /var/lib/status-saver/data
session_db:     /var/lib/status-saver/session.db
index_db:       /var/lib/status-saver/index.db
retention_days: 90                  # 0 = keep forever
rotation_hour:  4                   # informational; actual schedule is in the timer
log_level:      info                # trace|debug|info|warn|error
alert_webhook:  ""                  # optional POST-on-LoggedOut (ntfy.sh / Slack)
metrics_addr:   ""                  # optional "127.0.0.1:9090" for /health + /metrics
```

All paths must be absolute. Parent directories are created automatically with
mode `0750`. Full example with comments: `config.example.yaml`.

## Pairing

```
status-saver pair --config /etc/status-saver/config.yaml
```

QR codes scroll by in the terminal. On the secondary phone:
**WhatsApp → Settings → Linked Devices → Link a Device** → scan.

After scanning, the binary logs `pair-success received; keeping connection
open for post-pair handshake` and stays connected for a **30-second grace
window**. Do not kill it during that window — the phone app needs that time
to complete app-state + contact sync. If interrupted too early the phone
gets stuck on "pairing…" and the link is effectively broken.

After the grace window it prints `pairing complete — session stored,
disconnecting` and exits.

Sessions remain valid for weeks to months. Only need to re-pair if WhatsApp
invalidates the session (device removed on the phone, account ban). The
daemon exits with status 1 on logout.

### Recovering from a half-broken pair

If an earlier pair attempt exited too early, `session.db` will look "paired"
but the phone never confirmed. Running `status-saver pair` again just prints
"already paired — pass --force to re-pair". Force a clean re-pair:

```
status-saver pair --config ./config.yaml --force
```

## On-disk data format

Flat layout, one folder per contact, date/time baked into each filename so
posts sort chronologically within a contact:

```
/var/lib/status-saver/data/
`-- Alice_49123456789/                          # <push_name>_<jid.user>
    |-- 2026-04-23_143012_3EB0A9B8C7D6E5F4.jpg
    |-- 2026-04-23_143012_3EB0A9B8C7D6E5F4.json
    |-- 2026-04-23_143155_3EB0F1E2D3C4B5A6.mp4
    |-- 2026-04-23_143155_3EB0F1E2D3C4B5A6.json
    |-- 2026-04-24_164820_3EB012AB34CD56EF.txt      # text-only status
    `-- 2026-04-24_164820_3EB012AB34CD56EF.json
```

File stem: `<YYYY-MM-DD>_<HHMMSS>_<msgid>`. Media (or `.txt`) and its `.json`
sidecar share a stem so they stay grouped when sorted.

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

### Retention

The `status-saver-rotate.timer` unit fires `status-saver rotate` daily at
04:00 local time. It deletes any file under `<dataDir>/<contact>/` whose
YYYY-MM-DD prefix is older than `retention_days`, removes contact folders
that end up empty, and prunes matching rows from `index.db`. Files that
don't start with `YYYY-MM-DD_` are left alone. `retention_days: 0` disables
pruning entirely.

Trigger manually: `sudo systemctl start status-saver-rotate.service`.

## Observability

```
journalctl -u status-saver -f              # live daemon log
journalctl -u status-saver-rotate -e       # last rotation run
systemctl list-timers status-saver-rotate  # next scheduled run
```

Interesting log messages (zerolog, with `mod=wa` / `mod=status` / ...):

- `status archived` — a new post just landed on disk
- `duplicate status, skipping` — dedup worked via index.db
- `whatsapp disconnected (will auto-reconnect)` — transient, no action
- `whatsapp logged out — session invalid` — terminal, exit 1, re-pair needed

### HTTP endpoints (optional)

Set `metrics_addr: "127.0.0.1:9090"` to expose:

- `GET /health` → `200 ok` when connected, `503 not connected` otherwise.
  Ideal for a systemd / uptime probe.
- `GET /metrics` → Prometheus text format with:
  - `statussaver_archived_total` (counter)
  - `statussaver_errors_total` (counter)
  - `statussaver_connected` (gauge 0/1)
  - `statussaver_last_archived_timestamp_seconds` (gauge)
  - `statussaver_uptime_seconds` (gauge)

Endpoints are **unauthenticated** — bind only to `127.0.0.1` (or behind an
auth proxy). Leave `metrics_addr` empty to disable (default).

## Architecture

```
 +-----------------------------------------------------+
 |  status-saver daemon (systemd, 24/7)                |
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
cmd/status-saver/          # single binary; dispatches to subcommands

internal/
|-- buildinfo/  # version metadata injected by goreleaser ldflags
|-- cli/
|   |-- daemon/   # `status-saver run`
|   |-- pair/     # `status-saver pair`
|   `-- rotate/   # `status-saver rotate`
|-- config/     # YAML loader + validation
|-- logging/    # zerolog console output
|-- wa/         # whatsmeow client wrapper + status@broadcast handler
|-- storage/    # on-disk path scheme + SQLite dedup index
|-- metrics/    # /health + /metrics recorder
`-- rotate/     # retention walker

deploy/systemd/ # .service + .timer units for Linux
deploy/launchd/ # macOS LaunchAgent plist template
```

### Data flow for one status post

1. whatsmeow delivers `*events.Message` with `Info.Chat == types.StatusBroadcastJID`.
2. `wa.StatusHandler.archive()` classifies and dedups against `index.db`.
3. Media is downloaded (`client.Download`), bytes are AES-decrypted, written
   atomically to `<dataDir>/<sender>/<stem>.<ext>`.
4. Text-only posts go to `<stem>.txt`.
5. Always: `<stem>.json` with sender, push name, timestamp, caption,
   mimetype.
6. A row in `index.db` records `(msg_id, sender_jid)` so restarts are
   idempotent.

## Limitations

Read this before deploying. These are real protocol / ecosystem constraints,
not bugs we haven't got around to.

### 1. No server-side backfill of prior statuses

When the daemon starts after downtime, statuses posted during that downtime
are **permanently lost** from our perspective. Concretely:

- Server's `<offline count=N>` replay empirically reports `count=0` for
  status@broadcast — the server doesn't queue statuses the way it queues
  direct messages.
- The phone's `HistorySync` push reliably fires only on first pairing
  (`INITIAL_STATUS_V3`), not on subsequent reconnects.
- The daemon fires an `HistorySyncOnDemand` peer request 5s after connect
  as a best-effort probe; the phone ACKs but usually returns nothing.
- There is no discovered IQ that asks the **server** for currently-active
  statuses. WhatsApp Desktop demonstrably does this, but nobody in the
  open-source ecosystem (whatsmeow, Baileys, whatsapp-web.js,
  mautrix-whatsapp) has reverse-engineered it yet. Tracked at
  [whatsmeow#1033](https://github.com/tulir/whatsmeow/discussions/1033)
  and locally at [issue #1](https://github.com/peetzweg/status-saver/issues/1).

Operational consequence: **run the daemon 24/7**, schedule deploys for
low-post hours, and accept that statuses expire 24h after posting — long
outages lose everything posted during them.

### 2. Phone must be online for first pairing and catch-up paths

- First pair (QR scan): needs the phone.
- Any `HistorySync` / on-demand catch-up: also needs the phone.
- Live capture itself does **not** need the phone once paired.

### 3. ToS and account risk

whatsmeow is an unofficial client; using it technically violates WhatsApp's
ToS. Passive read-only access from a dedicated secondary number has been
empirically low-risk historically, but there is no guarantee. A banned
session shows up as `events.LoggedOut`; the daemon exits 1 and systemd is
configured **not** to auto-restart — re-pairing is a manual step.

### 4. Content types / features not (yet) supported

- Voice-note statuses (audio) — classifier skips, tracked in
  [#3](https://github.com/peetzweg/status-saver/issues/3).
- Sticker statuses — same.
- Revoke tracking — we log and skip, tracked in
  [#4](https://github.com/peetzweg/status-saver/issues/4).
- Web UI for browsing — tracked in
  [#7](https://github.com/peetzweg/status-saver/issues/7).
- Multi-account — one secondary number per daemon instance. Run multiple
  instances with separate config + state dirs to archive multiple accounts.

### 5. Already-expired media can't be downloaded

Status media on the WhatsApp CDN is only retrievable during the 24h
lifetime. If the daemon receives an event late (e.g. a sluggish HistorySync
batch) and the media has already expired, `client.Download` fails — we log
it and the JSON sidecar lands with `media_path` empty.

### 6. Data at rest is plaintext

`session.db` is a credential (lets you send as the paired account). Captions
and sender attribution land in plaintext on disk. **Full-disk encryption on
the server is a hard requirement.** The systemd unit pins file permissions
to a dedicated user.

## Development

```
make build      # CGO_ENABLED=1 go build -o bin/ ./cmd/...
make test       # go test -race ./cmd/... ./internal/...
make lint       # gofmt + vet + golangci-lint (skipped if not installed)
make vuln       # govulncheck on module + transitive deps
make install    # go install ./cmd/...  (into $GOBIN)
make clean      # remove ./bin and ./dist
make help       # full target list
```

Test coverage as of v0.2.0:

| Package | Coverage |
|---|---:|
| `internal/config` | 96% |
| `internal/metrics` | 100% |
| `internal/rotate` | 74% |
| `internal/storage` | 33% |
| `internal/wa` | 19% (pure fns only — the handler IO path needs a fake whatsmeow, tracked in [#13](https://github.com/peetzweg/status-saver/issues/13)) |

### E2E smoke test

1. `status-saver pair --config ./config.yaml`, scan QR with the test phone.
2. `status-saver run --config ./config.yaml` — wait for
   `daemon started — awaiting status broadcasts`.
3. From a third account (whose number the test phone has saved) post a
   status — image with caption, then a video, then a text-only post.
4. Inside ~30s, `data/<poster>/` should gain files named
   `YYYY-MM-DD_HHMMSS_<msgid>.<ext>` plus their `.json` sidecars.
5. Kill and restart the daemon; nothing stored twice (dedup via `index.db`).
6. Briefly drop the network (`iptables -A OUTPUT -p tcp --dport 443 -j DROP`
   for 60s, then `-D`) → whatsmeow auto-reconnects.

## Releases & contributing

### Download a release

Tagged releases ship a single Linux amd64 tarball on the
[Releases page](https://github.com/peetzweg/status-saver/releases) containing
the `status-saver` binary, systemd units, example config, README, and
LICENSE. Verify with `checksums.txt`, extract, follow the
[Install](#install) steps above.

Other platforms (macOS, Linux arm64): build from source. Multi-arch
release builds are blocked on
[#12](https://github.com/peetzweg/status-saver/issues/12) (drop CGO).

### Cutting a release (maintainers)

Same flow as Changesets in the JS/TS world, adapted for Go:

1. Contributors open PRs with **conventional-commit titles**
   (`feat(wa): foo`, `fix(storage): bar`, `docs: baz`, …). The `PR title`
   workflow blocks merge if the title doesn't parse.
2. On push to `main`, `release-please` maintains a rolling Release PR
   titled `chore(main): release X.Y.Z` with the accumulating changelog.
3. Merging the Release PR creates the git tag + GitHub Release.
4. `goreleaser` runs on the same workflow and uploads
   `status-saver_X.Y.Z_linux_amd64.tar.gz` + `checksums.txt` as assets.

Version bumping in 0.x (configured in `release-please-config.json`):

| Commit type | Bump |
|---|---|
| `feat:` | patch (0.1.0 → 0.1.1) |
| `fix:` | patch (0.1.0 → 0.1.1) |
| `feat!:` / `BREAKING CHANGE:` footer | minor (0.1.0 → 0.2.0) |
| `chore:` / `docs:` / `test:` / `ci:` / `refactor:` | none (listed in changelog) |

Recognised commit types live in
[`.github/workflows/pr-title.yml`](./.github/workflows/pr-title.yml).

## License

Mozilla Public License 2.0 — see [`LICENSE`](./LICENSE).

Built on [whatsmeow](https://github.com/tulir/whatsmeow) (MPL-2.0); inspired
by [mautrix-whatsapp](https://github.com/mautrix/whatsapp) (AGPL-3.0, same
author). MPL-2.0 is the minimum required because of the whatsmeow
dependency.
