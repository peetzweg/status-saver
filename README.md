# story-saver

Archiver-Daemon für WhatsApp-Status-Posts (24-h-Stories) von Kontakten.
Läuft headless auf einem Linux-Server, loggt sich via Multi-Device einmalig
mit einer dedizierten Zweitnummer ein und speichert danach jeden eingehenden
Status-Broadcast (Foto, Video, Text + Captions) nach Disk.

> Status: alpha, persönliches Tool. Verwendet [whatsmeow](https://github.com/tulir/whatsmeow) —
> eine inoffizielle Reimplementierung des WhatsApp-Multi-Device-Protokolls.
> Das ist ein ToS-Grenzgänger: rein passives Lesen mit einer Zweitnummer ist
> historisch niedrig-riskant, aber keine Garantie. Nicht mit deiner Primärnummer
> verwenden.

---

## Was macht das Tool

WhatsApp sendet Status-Posts von Kontakten an alle Linked Devices der
Beobachter:innen als normale verschlüsselte Messages — adressiert an die
spezielle JID `status@broadcast`. `story-saver` hält eine dauerhafte
Multi-Device-Session offen, filtert alle Nachrichten auf genau diese JID,
entschlüsselt die Media-Attachments (AES-CBC/HMAC via whatsmeow) und
schreibt sie nach Disk inkl. Metadaten-JSON.

Der persistente Daemon ist bewusst gewählt statt "1× am Tag um 4 Uhr
connecten": WhatsApp garantiert nicht dokumentiert, wie lang Status-Offline-
Backfill für ein gerade wieder verbundenes Gerät ist. Ein 24/7-Client fängt
alles ab. Ein separater Timer um 04:00 kümmert sich nur ums Aufräumen.

## Was das Tool **nicht** macht

- Keine eigenen Status posten (wir sind reiner Leser).
- Keine Chat-Nachrichten oder Gruppen-Messages archivieren — nur `status@broadcast`.
- Kein Multi-Account (aktuell genau eine Zweitnummer).
- Keine Web-UI zum Durchstöbern — Dateien liegen roh auf Disk.
- Kein offizieller WhatsApp-Support (die Cloud-API kann Status gar nicht lesen).

## Architektur

```
 ┌─────────────────────────────────────────────────────┐
 │  story-saver daemon (systemd, 24/7)                 │
 │  ┌───────────────┐   ┌───────────────────────────┐  │
 │  │ whatsmeow     │──▶│ status message handler    │  │
 │  │ Client (MD)   │   │ (filter status@broadcast) │  │
 │  └───────┬───────┘   └──────────┬────────────────┘  │
 │          │                      ▼                    │
 │   session.db             downloader + writer        │
 │  (SQLite, whatsmeow)    ├─ media → disk             │
 │                         └─ metadata → index.db      │
 └─────────────────────────────────────────────────────┘
 ┌─────────────────────────────────────────────────────┐
 │  systemd timer 04:00 daily                          │
 │  └─ rotate: prune >retention_days, clean index      │
 └─────────────────────────────────────────────────────┘
```

### Paketlayout

```
cmd/
├── story-saver/          # langlebiger Daemon
├── story-saver-pair/     # interaktives QR-Pairing, läuft genau einmal
└── story-saver-rotate/   # one-shot, vom systemd-Timer getriggert

internal/
├── config/     # YAML-Loader + Validation
├── logging/    # zerolog-Konsolenausgabe
├── wa/         # whatsmeow-Client-Wrapper + status@broadcast-Handler
├── storage/    # Disk-Pfad-Schema + SQLite-Dedup-Index
└── rotate/     # Retention-Walker

deploy/systemd/ # .service + .timer Units, siehe deploy/INSTALL.md
```

### Datenfluss für einen Status-Post

1. whatsmeow liefert `*events.Message` mit `Info.Chat == types.StatusBroadcastJID`.
2. `wa.StatusHandler.archive()` prüft `index.db` auf Duplikat — Restarts sind idempotent.
3. Für Media (Image/Video): `client.Download(ctx, msg)` → AES-entschlüsselt, schreibt in `<dataDir>/YYYY-MM-DD/<sender>/<HHMMSS>_<msgid>.<ext>`.
4. Für Text-only: `<...>.txt` mit dem Plaintext.
5. Immer: `<...>.json` mit `sender_jid`, `push_name`, `received_at`, `caption`, `mimetype`.
6. `(msg_id, sender_jid)` als Row in `index.db` — verhindert Doppelverarbeitung.

## Voraussetzungen

- Linux-Server (getestet auf Ubuntu/Debian; systemd).
- Go 1.25+
- C-Compiler (für `github.com/mattn/go-sqlite3` via CGO).
- Ein WhatsApp-Zweit-Account auf einem Handy (zum Scannen des QR-Codes einmalig).

## Bauen

```
CGO_ENABLED=1 go build -o bin/story-saver        ./cmd/story-saver
CGO_ENABLED=1 go build -o bin/story-saver-pair   ./cmd/story-saver-pair
CGO_ENABLED=1 go build -o bin/story-saver-rotate ./cmd/story-saver-rotate
```

Warum CGO: der SQLite-Driver ist ein C-Binding. Alternativ `modernc.org/sqlite`
(pure Go) — nicht eingebaut, leicht austauschbar falls CGO auf dem Zielsystem
nicht geht.

## Konfigurieren

```yaml
# /etc/story-saver/config.yaml
data_dir:       /var/lib/story-saver/data
session_db:     /var/lib/story-saver/session.db
index_db:       /var/lib/story-saver/index.db
retention_days: 90        # 0 = nie löschen
rotation_hour:  4         # informativ; Zeitplan steht im systemd-Timer
log_level:      info      # trace|debug|info|warn|error
alert_webhook:  ""        # optional: POST bei LoggedOut (ntfy.sh, Slack webhook, …)
```

Alle Pfade absolut. Parent-Ordner werden automatisch mit `0750` angelegt.

## Pairing (einmalig)

Der Daemon startet nicht, solange kein Device gepaart ist. Pairing ist
interaktiv (braucht dein Handy):

```
sudo -u story-saver story-saver-pair --config /etc/story-saver/config.yaml
```

Im Terminal erscheinen fortlaufend QR-Codes. Auf dem Zweit-Handy:
**WhatsApp → Einstellungen → Verknüpfte Geräte → Gerät verknüpfen** → scannen.
Nach erfolgreichem Scan beendet sich das Binary; `session.db` enthält danach
die Keys.

Sessions bleiben Wochen bis Monate gültig. Erst bei Logout (du entfernst das
Gerät in der App, Account-Ban, etc.) muss neu gepaart werden — der Daemon
beendet sich dann mit Exit-Code 1.

## Betrieb

### Via systemd (empfohlen)

Siehe **`deploy/INSTALL.md`** — legt User, Binaries, Config, Service und Timer
an. Kurzfassung:

```
sudo systemctl enable --now story-saver.service
sudo systemctl enable --now story-saver-rotate.timer
```

### Manuell (Debugging)

```
./bin/story-saver --config ./config.yaml
```

Stop mit `Ctrl-C`. Shutdown ist sauber (whatsmeow disconnect, SQLite flush).

## Datenformat auf Disk

```
/var/lib/story-saver/data/
└── 2026-04-23/
    └── PhilipP_4915112345678/          # <push_name>_<jid.user>
        ├── 143012_3EB0A9B8C7D6E5F4.jpg
        ├── 143012_3EB0A9B8C7D6E5F4.json
        ├── 143155_3EB0F1E2D3C4B5A6.mp4
        ├── 143155_3EB0F1E2D3C4B5A6.json
        └── 164820_3EB012AB34CD56EF.txt   # text-only Status
            164820_3EB012AB34CD56EF.json
```

JSON-Sidecar-Schema (alle Felder optional außer `msg_id`, `sender_jid`, `received_at`):

```json
{
  "msg_id": "3EB0A9B8C7D6E5F4",
  "sender_jid": "4915112345678@s.whatsapp.net",
  "push_name": "PhilipP",
  "received_at": "2026-04-23T14:30:12+02:00",
  "media_path": "/var/lib/.../143012_3EB0A9B8C7D6E5F4.jpg",
  "mimetype": "image/jpeg",
  "caption": "Optional picture caption"
}
```

## Retention

Der Timer `story-saver-rotate.timer` ruft täglich 04:00 `story-saver-rotate`
auf:

1. Tagesordner mit `Name < heute - retention_days` werden komplett gelöscht.
2. `seen_messages`-Rows mit `received_at < cutoff` werden aus `index.db` entfernt.

`retention_days: 0` schaltet das Pruning komplett aus.

Manuell auslösen:

```
sudo systemctl start story-saver-rotate.service
```

## Observability

```
journalctl -u story-saver -f              # Live-Log des Daemons
journalctl -u story-saver-rotate -e       # letzte Rotation
systemctl list-timers story-saver-rotate  # nächster Lauf
```

Interessante Log-Felder (zerolog, Format `mod=wa` / `mod=status` / …):

- `"status archived"` — ein neuer Post ist auf Disk
- `"duplicate status, skipping"` — idempotent durch index.db
- `"whatsapp disconnected (will auto-reconnect)"` — transient, kein Problem
- `"whatsapp logged out — session invalid"` — terminal, Exit 1, Re-Pair nötig

## Entwicklung

```
go test ./...           # Storage- und Rotation-Unit-Tests
go vet ./...
go build ./...
```

Die `wa/`-Pakete haben keine Unit-Tests — das würde einen Fake-WhatsApp-Server
voraussetzen. Stattdessen E2E gegen eine echte Zweitnummer (siehe Plan).

### E2E-Smoketest

1. `story-saver-pair` → QR scannen mit Test-Handy.
2. `story-saver` starten; warten bis `daemon started — awaiting status broadcasts`.
3. Von einem dritten Account (dessen Nummer das Test-Handy kennt) einen Status posten — Foto mit Caption, Video, Text-only.
4. In `data/<heute>/<poster>/` sollten innerhalb von ~30 s Media + `.json` erscheinen.
5. Daemon killen, neu starten, nichts doppelt speichern (Dedup via `index.db`).
6. Netzwerk kurz trennen (`iptables -A OUTPUT -p tcp --dport 443 -j DROP` für 60 s, dann `-D`) → whatsmeow reconnected automatisch.

## Sicherheits- und Betriebshinweise

- **Dedizierte Zweitnummer verwenden.** Passive Status-Reads sind historisch
  unauffällig, aber jede Multi-Device-Session auf einem Server erhöht das
  Ban-Risiko leicht.
- `session.db` enthält die Geräte-Identität. Kompromittierung erlaubt
  WhatsApp-Nachrichten im Namen des Zweit-Accounts zu senden. Rechte streng:
  unit-File setzt `0640` und eigenen User.
- Captions und Absenderzuordnung landen im Klartext auf Disk. Disk-Encryption
  auf dem Server ist Pflicht.
- Bei `events.LoggedOut` exit 1 — systemd restartet **nicht** (siehe
  `RestartPreventExitStatus=1`). Re-Pair ist ein bewusster Schritt.

## Lizenz / Herkunft

Code basiert auf dem [whatsmeow](https://github.com/tulir/whatsmeow)-Client
(MPL-2.0). Status-Broadcast-Handling ist inspiriert von
[mautrix-whatsapp](https://github.com/mautrix/whatsapp) (gleicher Autor, AGPL-3.0).
Eigene Lizenz noch nicht gesetzt — bei Veröffentlichung mindestens MPL-2.0
erforderlich wegen whatsmeow-Dependency.
