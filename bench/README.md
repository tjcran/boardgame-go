# Benchmarks

Cross-match concurrency measurements that put a numeric floor under
`JS_LIMITATIONS.md` §1 (Single-threaded engine).

## Run

    go test -run='^$' -bench . -benchmem ./bench/

For longer / steadier numbers:

    go test -run='^$' -bench . -benchtime 5s -benchmem ./bench/

## Indicative numbers

On an AMD Ryzen 9 5900X (12 cores / 24 threads), `boardgame-go` at the
last commit on `main`:

    BenchmarkSingleMatchSerialMoves                     ~615 ns/op   3 allocs/op
    BenchmarkConcurrentMatchesViaManager/concurrency=1   ~3 μs/op   17 allocs/op
    BenchmarkConcurrentMatchesViaManager/concurrency=4   ~3 μs/op   17 allocs/op
    BenchmarkConcurrentMatchesViaManager/concurrency=16  ~3 μs/op   17 allocs/op
    BenchmarkConcurrentMatchesViaManager/concurrency=64  ~3 μs/op   17 allocs/op

Two readings worth highlighting:

1. **Pure-engine reducer overhead is ~615 ns / move.** That's the
   `core.Apply` floor — what every move pays for free.

2. **Manager overhead is ~3 µs / move and *flat* across 1→64
   concurrent matches.** This is the headline win: the per-match write
   lock + storage round-trip + broadcast cost a constant ~2.4 µs per
   move regardless of how many other matches the manager is hosting.

   BGIO can't structurally match this — Node.js serialises every
   match's move dispatch on one event loop, so concurrency=N
   effectively divides per-match throughput by N. Goroutines give us
   actual parallelism at no API cost.

## What the bench game is

A 2-player "noop" game whose only move increments a counter. No
plugins. No hooks. `DisableUndo: true` so snapshot copies don't drown
the engine signal in per-op log clones.

This is a floor, not a ceiling: real games will sit between
"as-fast-as-noop" and "dominated by your move logic." The point is
to bound the engine overhead, not predict game throughput.

## Honest caveats

- Storage is `Memory`. A Postgres or SQLite adapter adds its own
  latency floor; check `storage/<adapter>` for per-backend numbers.
- The benchmark plays one player exclusively (alice keeps making
  moves; turn never rotates). A turn-rotation scheme would add
  one OnEnd + one OnBegin per move; budget another ~0.5 µs.
- WebSocket broadcast cost isn't included; the bench drives the
  Manager API directly. With one subscriber per match attached the
  per-move number rises by ~1-2 µs.
