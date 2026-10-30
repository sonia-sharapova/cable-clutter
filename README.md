# cable-clutter
Platform that helps people organize and make sense of their cables and wires. 

`cbl` is an offline-first CLI: log the cables and adapters you own, look up
what a device's ports do, and find out what you're missing to hook things up.

## Build

Requires Go 1.27+.

```sh
go build -o cbl ./cmd/cbl
go test ./...
go test -run x -bench . ./internal/catalog   # performance budgets
```

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

cbl find aux              # connectors, cables and adapters matching a name or alias
cbl find "usb c"
cbl catalog cables        # list connectors | cables | adapters | signals
cbl doctor                # catalog stats and library location
cbl find aux --timing     # print load and query times
```

## Layout

| Path | What |
| --- | --- |
| `catalog/*.yaml` | The bundled catalog: signals, connectors, cable and adapter types. Compiled into the binary. |
| `internal/catalog` | Loads and validates the catalog; builds the hashmap indexes and search. |
| `internal/library` | The user's own data in SQLite (WAL), with numbered migrations. |
| `cmd/cbl` | The CLI. |

Cable and adapter types are keyed by a canonical string built from their sorted
ends, e.g. `rca*3:m|ps2-avmulti:m`, so end order never matters. An end is
`connector[*count]:gender`.

The library lives at `~/Library/Application Support/cable-clutter/library.db`
on macOS and `$XDG_DATA_HOME/cable-clutter/library.db` on Linux; set
`CBL_HOME` to put it elsewhere.
