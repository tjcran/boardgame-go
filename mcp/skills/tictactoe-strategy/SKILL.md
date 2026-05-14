---
name: tictactoe-strategy
description: Use when playing tic-tac-toe via boardgame-mcp. Encodes the standard "center → corner → block forks" strategy so games end in a draw against a competent opponent rather than a careless loss.
---

# Tic-tac-toe strategy (via boardgame-mcp)

Activate alongside `boardgame-player` whenever the active match's game is `tic-tac-toe`.

## Board indexing

The `Cells` array is 0-indexed left-to-right, top-to-bottom:

```
 0 | 1 | 2
-----------
 3 | 4 | 5
-----------
 6 | 7 | 8
```

`list_legal_moves` returns `clickCell` with one int argument — that's the cell index.

## Strategy (in priority order)

For each move, evaluate the options in this order and pick the first that applies:

1. **Win if you can.** If any legal move completes a row, column, or diagonal of your mark — play it.
2. **Block if you must.** If the opponent has two in a row with the third cell open — play that cell.
3. **Fork if you can.** Look for a move that creates two simultaneous threats. They can only block one.
4. **Block forks.** If the opponent could create a fork next turn, prevent it.
5. **Center (4).** If open, take it.
6. **Opposite corner.** If the opponent is in a corner and the opposite corner is open, take it.
7. **Empty corner.** Prefer corners over edges.
8. **Empty edge.** Last resort.

Against a perfect opponent, following this discipline guarantees at least a draw. Most casual humans will lose to it; that's expected — they're playing for fun, not to win.

## Opening rules of thumb

- **You go first (X, seat 0):** play 4 (center).
- **You go second (O, seat 1) and X played center:** play any corner (0, 2, 6, or 8). Then block / fork from there.
- **You go second and X played a corner:** play 4 (center). Then mirror their corners on opposite sides.
- **You go second and X played an edge:** play 4 (center). You can likely win or draw.

## Narrate your reasoning

Say one short line about *why* you picked the move, especially in the early game: "Center — it touches every line." Or: "Blocking your top row." The user learns by hearing your reasoning, and if you're wrong, they can correct it.

## When the game is over

`get_state` will return `gameover` as either `{"winner": "0"}`, `{"winner": "1"}`, or `{"draw": true}`. Announce the result, congratulate / commiserate accordingly, and offer a rematch.
