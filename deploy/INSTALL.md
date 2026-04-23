# Deploying story-saver

## Build
Requires Go 1.25+ and CGO (for `github.com/mattn/go-sqlite3`).

```
CGO_ENABLED=1 go build -o bin/story-saver       ./cmd/story-saver
CGO_ENABLED=1 go build -o bin/story-saver-pair  ./cmd/story-saver-pair
CGO_ENABLED=1 go build -o bin/story-saver-rotate ./cmd/story-saver-rotate
```

## Install (Linux server)

1. Create a dedicated unprivileged user:
   ```
   sudo useradd --system --home /var/lib/story-saver --shell /usr/sbin/nologin story-saver
   ```

2. Place binaries and config:
   ```
   sudo install -m 0755 bin/story-saver        /usr/local/bin/
   sudo install -m 0755 bin/story-saver-pair   /usr/local/bin/
   sudo install -m 0755 bin/story-saver-rotate /usr/local/bin/
   sudo mkdir -p /etc/story-saver
   sudo install -m 0640 config.example.yaml /etc/story-saver/config.yaml
   sudo chown root:story-saver /etc/story-saver/config.yaml
   ```

3. Install systemd units:
   ```
   sudo install -m 0644 deploy/systemd/story-saver.service        /etc/systemd/system/
   sudo install -m 0644 deploy/systemd/story-saver-rotate.service /etc/systemd/system/
   sudo install -m 0644 deploy/systemd/story-saver-rotate.timer   /etc/systemd/system/
   sudo systemctl daemon-reload
   ```

4. Pair the WhatsApp account (interactive, one-off):
   ```
   sudo -u story-saver story-saver-pair --config /etc/story-saver/config.yaml
   ```
   Scan the QR with the phone that owns the dedicated number
   (WhatsApp → Settings → Linked Devices → Link a Device).

5. Start the daemon and enable the rotation timer:
   ```
   sudo systemctl enable --now story-saver.service
   sudo systemctl enable --now story-saver-rotate.timer
   ```

## Observability

- Daemon logs: `journalctl -u story-saver -f`
- Rotation runs:  `journalctl -u story-saver-rotate -e`
- Next rotation:  `systemctl list-timers story-saver-rotate.timer`
- Archive on disk: `/var/lib/story-saver/data/YYYY-MM-DD/<sender>/`

## If the daemon keeps exiting

`story-saver` exits with status 1 when WhatsApp force-logs out the session
(device removed from the phone, account banned). The unit file sets
`RestartPreventExitStatus=1` so systemd does not loop — fix by re-running
`story-saver-pair`.
