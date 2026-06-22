# WebAssembly battle simulator

A static, server-less demo that runs the boardgame-go engine in the browser
via WebAssembly. It plays an in-memory tic-tac-toe battle between two seeded
`bots.RandomBot` players and renders a deterministic battle log: the same seed
always produces the same moves (see `JS_LIMITATIONS.md` §3).

The Go entrypoint lives at `cmd/wasm/main.go`. It binds the `core` + `bots`
packages with `syscall/js` and exposes one global function, `simulateBattle`,
mirroring the in-memory loop of `bots.Simulate` while additionally capturing a
per-move log.

## Build

```sh
GOOS=js GOARCH=wasm go build -o web/main.wasm ./cmd/wasm
```

`web/wasm_exec.js` is the official Go runtime glue for Go 1.23.x (copied from
`$(go env GOROOT)/misc/wasm/wasm_exec.js`). Refresh it after a Go upgrade.

## Run

Serve the `web/` directory over HTTP (WASM cannot be loaded from `file://`):

```sh
cd web && python3 -m http.server 8080
# then open http://localhost:8080
```

## JS API

```js
simulateBattle(seed)                  // seed (number or string)
simulateBattle(seed, matches)         // + number of matches
simulateBattle(seed, matches, maxMoves)
```

Returns `{ ok, seed, matches, result, log, entries, outcome }`, or
`{ ok:false, error }` on bad input.
