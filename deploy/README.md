# deploy: reference files for the service and the nightly snapshot

These files are a **reference**, not the installed copies. The owner's machine has its own copies of the unit files
and of the script. Before you replace an installed file, compare it with the file here (for example with `diff`),
keep the settings that are only on the machine (the user, the paths), and then copy.

| file | installed as | what it does |
|---|---|---|
| `lifelog.service` | `/etc/systemd/system/lifelog.service` | runs `lifelog serve` with `--with web` (the browser; `--with whatsapp` adds the agent on WhatsApp beside it); `KillSignal=SIGTERM` and `TimeoutStopSec=30` let the server drain its requests |
| `lifelog-snapshot` | `/usr/local/bin/lifelog-snapshot` | runs `lifelog snapshot`, tests its exit status, prunes to the newest 14 good snapshots |
| `lifelog-snapshot.service` | `/etc/systemd/system/lifelog-snapshot.service` | runs the script once; `TimeoutStartSec=30min`; `OnFailure=lifelog-snapshot-failed.service` |
| `lifelog-snapshot.timer` | `/etc/systemd/system/lifelog-snapshot.timer` | starts the service every night |
| `lifelog-snapshot-failed.service` | `/etc/systemd/system/lifelog-snapshot-failed.service` | writes the failure to the journal |

`OWNER` and the paths in the units are placeholders.

```mermaid
flowchart LR
    timer["lifelog-snapshot.timer<br/>every night"] --> service["lifelog-snapshot.service"]
    service --> script["lifelog-snapshot"]
    script --> cmd["lifelog snapshot --snapshots DIR"]
    cmd -->|"exit 0"| prune["prune: keep the 14 newest<br/>good snapshots"]
    cmd -->|"exit 1"| fail["the script exits 1<br/>no prune"]
    fail --> failed["lifelog-snapshot-failed.service<br/>one line in the journal"]
```

## The snapshot names

[D25](../docs/decisions/D25-snapshots.md) gives the names. A snapshot that passes its restore check is
`life-YYYY-MM-DD.db`, or `life-YYYY-MM-DDTHHMMSS.db` when the day has one already. A snapshot that fails the check is
renamed to `life-YYYY-MM-DD.failed.db` (or `life-YYYY-MM-DDTHHMMSS.failed.db`). The script counts and prunes only the
good names. It never removes a `.failed.db` file: the owner reads it and removes it.

## The script

Settings come from the environment: `LIFELOG_SNAPSHOTS` (required), `LIFELOG_BIN`, `LIFELOG_DB` and `LIFELOG_KEEP`.
The script exits 1 when `lifelog snapshot` exits with any other status than 0. systemd then starts
`lifelog-snapshot-failed.service`. To read the failures:

```
journalctl -p err -t lifelog-snapshot-failed
journalctl -u lifelog-snapshot.service
```

## Ship a change

The service runs the installed program, not the one in the repo folder.

1. `go build -o <install path> ./cmd/lifelog`, and the extensions beside it: `go build -C extensions/web -o <install
   folder>/lifelog-web ./cmd/lifelog-web`, `go build -C extensions/mcp -o <install folder>/lifelog-mcp
   ./cmd/lifelog-mcp`, `go build -C extensions/agentic -o <install folder>/lifelog-agentic-init
   ./cmd/lifelog-agentic-init` and `go build -C extensions/whatsapp -o <install folder>/lifelog-whatsapp
   ./cmd/lifelog-whatsapp` ([extensions](../extensions/README.md)). A unit with `--with web` or `--with whatsapp` needs
   the program (`lifelog-web`, `lifelog-whatsapp`) beside `lifelog`: `serve` refuses to start without it, before the file
   opens. The local proxy points to `LIFELOG_WEB_LISTEN` (`127.0.0.1:7780`), and not to the core
   ([install](../extensions/web/README.md#install)).
2. `systemctl daemon-reload` after a change to a unit file.
3. `systemctl restart lifelog.service`.
4. Read the service's log lines (`journalctl -u lifelog.service -n 20`): the first names the version and the
   `user_version`, and the lines of `with web` say that the browser started.
