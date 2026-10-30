# cable-clutter
Platform that helps people organize and make sense of their cables and wires. 

`cbl` is an offline-first CLI: log the cables and adapters you own, look up
what a device's ports do, and find out what you're missing to hook things up.

## Set it up from GitHub

Works on macOS and Linux. Everything stays on your computer: no account, no
server, no network after the download.

1. **Install Go 1.27 or newer** from [go.dev/dl](https://go.dev/dl/) (on a Mac,
   `brew install go` also works). Check with `go version`.
2. **Get the code and build it:**
   ```sh
   git clone https://github.com/sonia-sharapova/cable-clutter.git
   cd cable-clutter
   go install ./cmd/cbl
   ```
   This puts `cbl` in `~/go/bin`. If `cbl` isn't found afterwards, add that
   folder to your PATH: `echo 'export PATH="$HOME/go/bin:$PATH"' >> ~/.zshrc`
   (or `~/.bashrc`), then open a new terminal.
3. **Check it works:** `cbl doctor` prints the catalog size and where your
   library will live.
4. **Load the examples (optional):** `cbl demo` adds two ready-made setups, a
   Raspberry Pi desk (monitor, keyboard, mouse, charger) and a laptop desk
   (monitor, speaker), with the devices and cables they need. Running it again
   changes nothing. To try it without touching your own library, run
   `CBL_HOME=/tmp/cbl-try cbl demo` (and the same `CBL_HOME` for the next steps).
5. **Open the web page:** `cbl serve --open`. It runs at
   [127.0.0.1:7878](http://127.0.0.1:7878), only on this computer; stop it with
   Ctrl-C. The page and the commands share one library.
6. **Add your own things,** on the page (Your network → Inventory or Devices)
   or in the terminal:
   ```sh
   cbl add usb-a usb-c --qty 2               # cables, by their two ends
   cbl device add raspberry-pi-3b --name Pi  # devices, by catalog model
   cbl catalog devices                       # the models it knows
   cbl connect Pi "Dell monitor"             # how to hook two things up
   ```
7. **Back up** now and then: `cbl export -o backup.json`.
8. **Update** later with `git pull && go install ./cmd/cbl`. Your library is
   kept and upgraded in place.

Without cloning, `go install github.com/sonia-sharapova/cable-clutter/cmd/cbl@latest`
does step 2's build in one line.

## Develop

Requires Go 1.27+.

```sh
go build -o cbl ./cmd/cbl
go test ./...                                # includes the performance budgets
go test -v -run TestBudgets ./internal/perf  # print the budget report
go generate ./...                            # after editing catalog/*.yaml
```

The CLI loads `catalog/catalog.bin`, a snapshot compiled from the YAML, so it
never parses YAML at start-up. A test fails if the snapshot is out of date.

Releases: push a `v*` tag; GoReleaser builds macOS and Linux binaries and
updates the Homebrew tap (`.goreleaser.yaml`, `.github/workflows/release.yml`).

## Use

```sh
cbl add usb-a usb-c --qty 4                     # log cables by their ends...
cbl add ps2-avmulti 'rca*3' --for PS2 --where "drawer 2"
cbl add "aux cable"                             # ...or by name
cbl add 2-prong barrel --variant 8.5v           # pick a variant when ends are shared
cbl ls                                          # every cable with its status
cbl ls --free --type usb-c                      # filter by status or connector
cbl ls --summary                                # one line per type
cbl rm <id|name>
cbl adapter add|ls|rm ...                       # adapters have their own inventory
cbl show "grey ps2 av"                          # draw an item's ends side by side
cbl show usb-a usb-c                            # ...or any catalog type

cbl demo                                        # load the Pi desk and laptop desk examples
cbl device add ps2-slim --name PS2              # record a device you own
cbl device add generic-usb-mouse --name Mouse --owner Alex
                                                # generic types when you don't know the model
cbl device PS2                                  # its ports: what each does, what you
                                                #   can connect now, what you're missing
cbl device ls / rm
cbl catalog devices                             # known models

cbl system new "Retro corner"                   # a setup you run
cbl system add-link "Retro corner" "grey ps2 av" --from PS2 --to "CRT TV"
                                                # pin one of your cables (or name a
                                                #   type for any free one); ports are
                                                #   filled in when only one fits
cbl system up "Retro corner"                    # its cables become in use
cbl system up Lounge --switch                   # take conflicting systems down first
cbl system up Lounge --wish                     # or add what's missing to the wishlist
cbl system down "Retro corner"                  # disconnect: cables come back
cbl system ls / show / unlink / rm              # ls flags systems that can't be up together

cbl connect ps2 "crt tv"                        # cheapest chain for video, audio, data:
                                                #   free items first, then busy ones,
                                                #   then what to buy
cbl connect "mac mini" "crt tv" --wish          # add the missing parts to the wishlist
cbl connect pi crt --owned --max 2              # only what you own; shorter chains
cbl wish / wish add / wish done 1 / wish rm 1   # done = bought: it joins your inventory

cbl reach PS2                                   # everything it connects to, with what you own
cbl plan PS2 "CRT TV" "TE speaker"              # wire a chain; no cable gets two jobs
cbl plan PS2 "CRT TV" --save "Retro corner"     # ...and save it as a system
cbl best-buy                                    # purchases ranked by new connections
cbl spare                                       # what fits none of your devices; what's
                                                #   worth a second of
cbl graph [--format text|mermaid|dot] [--device PS2]

cbl serve --open                                # the same library as a web page, on
                                                #   this computer only (127.0.0.1:7878)

cbl find aux              # connectors, cables and adapters matching a name or alias
cbl find "usb c"
cbl catalog cables        # list connectors | cables | adapters | signals
cbl doctor                # catalog stats and library location
cbl find aux --timing     # print load and query times
cbl ls --json             # any command: one JSON document instead of text
cbl export -o backup.json # the whole library as JSON
cbl import backup.json    # into an empty library (--replace to overwrite)
```

## Layout

| Path | What |
| --- | --- |
| `catalog/*.yaml` | The bundled catalog: signals, connectors, cable and adapter types, glyphs, devices. |
| `catalog/catalog.bin` | Snapshot compiled from the YAML by `go generate` (`cmd/catalogc`); embedded in the binary. |
| `internal/catalog` | Loads and validates the catalog; builds the hashmap indexes and search. |
| `catalog/glyphs.yaml` | ASCII outline per connector (`#` = contacts: filled on plugs, blank on sockets). |
| `internal/outline` | Draws a part's ends side by side; golden tests in `testdata/` (`go test ./internal/outline -update` to refresh). |
| `catalog/devices.yaml` | Device models and their ports (connector, direction, signals, voltage). `confirmed: false` marks port lists resting on an assumption. |
| `internal/advise` | Per-port report: what you own that fits, what it lets you do, what to buy for the rest, and power supplies of the wrong voltage. |
| `internal/plan` | Which item each link uses when a system comes up (pinned, then dedicated, then any spare), and which systems conflict over cables or device ports. |
| `internal/route` | Dijkstra over (open connector, signal) states: one signal must survive the chain; converters (`in_signals`) change it; one-way parts (`source`) and send-only ports (`out_only`) are respected. |
| `internal/perf` | Performance budgets as tests, on a 5,000-type catalog and a 500-cable library. |
| `internal/smart` | Questions across all your gear: reach, chains (`plan`), best buys, unused parts and spares. |
| `internal/graph` | The gear graph (devices, connectors, owned parts) as text, Mermaid or Graphviz. |
| `internal/library` | The user's own data in SQLite (WAL), with numbered migrations. |
| `cmd/cbl` | The CLI. |

Cable and adapter types are keyed by a canonical string built from their sorted
ends, e.g. `rca*3:m|ps2-avmulti:m`, so end order never matters. An end is
`connector[*count]:gender`.

The library lives at `~/Library/Application Support/cable-clutter/library.db`
on macOS and `$XDG_DATA_HOME/cable-clutter/library.db` on Linux; set
`CBL_HOME` to put it elsewhere.
