# Deploying status-saver

This walks through a production Linux install under systemd, from a fresh
Ubuntu/Debian VPS. macOS (launchd) is covered in a separate section below.

## Prerequisites (Ubuntu / Debian)

```
sudo apt-get update
sudo apt-get install -y build-essential git curl

# Install Go 1.25+ (apt's golang-go is usually too old on LTS). Pick ONE:
# a) Snap (simplest):
sudo snap install go --classic
# b) Official tarball (most reliable):
curl -fsSL https://go.dev/dl/go1.25.9.linux-amd64.tar.gz | sudo tar -C /usr/local -xz
echo 'export PATH=$PATH:/usr/local/go/bin' | sudo tee /etc/profile.d/go.sh
source /etc/profile.d/go.sh

go version  # should report go1.25.x
```

`build-essential` installs `gcc`, which `github.com/mattn/go-sqlite3` needs
through CGO. Without it, `go build` fails with `C compiler "gcc" not found`.

## Build

All three binaries need the package path explicitly — the repo root has no
`main.go`, each command lives under `cmd/<name>/`:

```
git clone https://github.com/peetzweg/status-saver /root/git/status-saver
cd /root/git/status-saver

CGO_ENABLED=1 go build -o bin/status-saver        ./cmd/status-saver
CGO_ENABLED=1 go build -o bin/status-saver-pair   ./cmd/status-saver-pair
CGO_ENABLED=1 go build -o bin/status-saver-rotate ./cmd/status-saver-rotate
```

**Common error:** `no Go files in <dir>` means you forgot the trailing
`./cmd/<name>` argument — it's the package to build, not the output name.

Verify the binaries work:

```
./bin/status-saver --help     # prints the -config flag
```

## Install (systemd)

1. Create a dedicated unprivileged user:
   ```
   sudo useradd --system --home /var/lib/status-saver --shell /usr/sbin/nologin status-saver
   ```

2. Place binaries and config:
   ```
   sudo install -m 0755 bin/status-saver        /usr/local/bin/
   sudo install -m 0755 bin/status-saver-pair   /usr/local/bin/
   sudo install -m 0755 bin/status-saver-rotate /usr/local/bin/
   sudo mkdir -p /etc/status-saver
   sudo install -m 0640 config.example.yaml /etc/status-saver/config.yaml
   sudo chown root:status-saver /etc/status-saver/config.yaml
   ```

3. Install systemd units:
   ```
   sudo install -m 0644 deploy/systemd/status-saver.service        /etc/systemd/system/
   sudo install -m 0644 deploy/systemd/status-saver-rotate.service /etc/systemd/system/
   sudo install -m 0644 deploy/systemd/status-saver-rotate.timer   /etc/systemd/system/
   sudo systemctl daemon-reload
   ```

4. Pair the WhatsApp account (interactive, one-off):
   ```
   sudo -u status-saver status-saver-pair --config /etc/status-saver/config.yaml
   ```
   Scan the QR with the phone that owns the dedicated number
   (WhatsApp → Settings → Linked Devices → Link a Device). **Wait ~30
   seconds after the "pair-success received" log line** before Ctrl-C'ing
   or letting the binary exit — the phone app needs that window to
   complete its side of the handshake. The binary will exit on its own
   when safe.

5. Start the daemon and enable the rotation timer:
   ```
   sudo systemctl enable --now status-saver.service
   sudo systemctl enable --now status-saver-rotate.timer
   ```

## Observability

- Daemon logs: `journalctl -u status-saver -f`
- Rotation runs:  `journalctl -u status-saver-rotate -e`
- Next rotation:  `systemctl list-timers status-saver-rotate.timer`
- Archive on disk: `/var/lib/status-saver/data/<sender>/`
- Optional HTTP probes if `metrics_addr: "127.0.0.1:9090"` is set in
  config: `curl localhost:9090/health`, `curl localhost:9090/metrics`

## If the daemon keeps exiting

`status-saver` exits with status 1 when WhatsApp force-logs out the session
(device removed from the phone, account banned). The unit file sets
`RestartPreventExitStatus=1` so systemd does not loop — fix by re-running
`status-saver-pair`.

---

## macOS (launchd)

Untested end-to-end; treat this section as "best guess, please report back
if something breaks". Issue: https://github.com/peetzweg/status-saver/issues
— open a new one if you hit trouble.

### Prerequisites

```
brew install go     # Go 1.25+
xcode-select --install   # for cc
```

Xcode CLT gives you `cc`, which the CGO compile needs.

### Build

Same as Linux:

```
CGO_ENABLED=1 go build -o bin/status-saver        ./cmd/status-saver
CGO_ENABLED=1 go build -o bin/status-saver-pair   ./cmd/status-saver-pair
CGO_ENABLED=1 go build -o bin/status-saver-rotate ./cmd/status-saver-rotate
```

### Install as a launchd user agent

Running as a LaunchAgent (user-level) is simpler than LaunchDaemon
(system-wide) and fits the single-user nature of this tool.

1. Put binaries somewhere stable:
   ```
   mkdir -p ~/.local/bin ~/Library/Application\ Support/status-saver/data
   cp bin/status-saver{,-pair,-rotate} ~/.local/bin/
   cp config.example.yaml ~/Library/Application\ Support/status-saver/config.yaml
   ```

2. Edit `~/Library/Application Support/status-saver/config.yaml` to point
   at `~/Library/Application Support/status-saver/data` etc. (use absolute
   paths — `~` is not expanded by the daemon).

3. Pair first (interactive, terminal must stay open ~30s after pair-success):
   ```
   ~/.local/bin/status-saver-pair --config ~/Library/Application\ Support/status-saver/config.yaml
   ```

4. Create a LaunchAgent plist at
   `~/Library/LaunchAgents/com.github.peetzweg.status-saver.plist`
   (see `deploy/launchd/` in this repo for a template; substitute your
   username and real paths).

5. Load + start:
   ```
   launchctl load ~/Library/LaunchAgents/com.github.peetzweg.status-saver.plist
   launchctl start com.github.peetzweg.status-saver
   ```

6. Logs:
   ```
   tail -f ~/Library/Logs/status-saver.log
   ```

### Unload / stop

```
launchctl unload ~/Library/LaunchAgents/com.github.peetzweg.status-saver.plist
```

### Known caveat

macOS App Nap may idle the process when the Mac is locked. Set
`<key>LegacyTimers</key><true/>` in the plist or run as a LaunchDaemon
(requires root) if you hit this.
