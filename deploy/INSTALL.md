# Deploying status-saver

## Build
Requires Go 1.25+ and CGO (for `github.com/mattn/go-sqlite3`).

```
CGO_ENABLED=1 go build -o bin/status-saver       ./cmd/status-saver
CGO_ENABLED=1 go build -o bin/status-saver-pair  ./cmd/status-saver-pair
CGO_ENABLED=1 go build -o bin/status-saver-rotate ./cmd/status-saver-rotate
```

## Install (Linux server)

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
   (WhatsApp → Settings → Linked Devices → Link a Device).

5. Start the daemon and enable the rotation timer:
   ```
   sudo systemctl enable --now status-saver.service
   sudo systemctl enable --now status-saver-rotate.timer
   ```

## Observability

- Daemon logs: `journalctl -u status-saver -f`
- Rotation runs:  `journalctl -u status-saver-rotate -e`
- Next rotation:  `systemctl list-timers status-saver-rotate.timer`
- Archive on disk: `/var/lib/status-saver/data/YYYY-MM-DD/<sender>/`

## If the daemon keeps exiting

`status-saver` exits with status 1 when WhatsApp force-logs out the session
(device removed from the phone, account banned). The unit file sets
`RestartPreventExitStatus=1` so systemd does not loop — fix by re-running
`status-saver-pair`.
