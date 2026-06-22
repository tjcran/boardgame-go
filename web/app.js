// app.js — loads main.wasm, wires up the form, and renders the deterministic
// battle log produced by the Go `simulateBattle` global.

const statusEl = document.getElementById("status");
const runBtn = document.getElementById("run");
const outputEl = document.getElementById("output");
const summaryEl = document.getElementById("summary");
const logEl = document.getElementById("log");
const movesBody = document.querySelector("#moves tbody");

async function loadWasm() {
  // `Go` comes from wasm_exec.js (the official Go runtime glue).
  const go = new Go();
  try {
    let result;
    if (WebAssembly.instantiateStreaming) {
      result = await WebAssembly.instantiateStreaming(
        fetch("main.wasm"),
        go.importObject,
      );
    } else {
      // Fallback for servers that don't send application/wasm.
      const bytes = await fetch("main.wasm").then((r) => r.arrayBuffer());
      result = await WebAssembly.instantiate(bytes, go.importObject);
    }
    // go.run resolves only when main() returns; ours blocks forever (select{})
    // to keep the exported function alive, so we intentionally do not await it.
    go.run(result.instance);

    // Wait until main() has registered the global bridge.
    await waitFor(() => typeof globalThis.simulateBattle === "function");

    statusEl.textContent = "Module ready. Pick a seed and simulate.";
    runBtn.disabled = false;
  } catch (err) {
    statusEl.innerHTML =
      '<span class="err">Failed to load main.wasm: ' +
      String(err) +
      "</span>";
  }
}

function waitFor(pred, intervalMs = 20, timeoutMs = 5000) {
  return new Promise((resolve, reject) => {
    const start = Date.now();
    const tick = () => {
      if (pred()) return resolve();
      if (Date.now() - start > timeoutMs) {
        return reject(new Error("timed out waiting for wasm to initialise"));
      }
      setTimeout(tick, intervalMs);
    };
    tick();
  });
}

function run() {
  const seedRaw = document.getElementById("seed").value.trim();
  const matches = parseInt(document.getElementById("matches").value, 10) || 1;
  const maxMoves = parseInt(document.getElementById("maxMoves").value, 10) || 1000;

  // Pass the seed as a number when it looks numeric, otherwise as a string —
  // the Go side accepts both and hashes non-numeric strings deterministically.
  const seedArg = /^-?\d+$/.test(seedRaw) ? Number(seedRaw) : seedRaw;

  let res;
  try {
    res = globalThis.simulateBattle(seedArg, matches, maxMoves);
  } catch (err) {
    statusEl.innerHTML = '<span class="err">' + String(err) + "</span>";
    return;
  }

  if (!res || !res.ok) {
    statusEl.innerHTML =
      '<span class="err">' + (res ? res.error : "no result") + "</span>";
    outputEl.hidden = true;
    return;
  }

  render(res);
}

function render(res) {
  statusEl.textContent =
    "Done. Seed " + res.seed + ", " + res.matches + " match(es).";

  const r = res.result;
  const winLines = Object.keys(r.wins)
    .sort()
    .map((p) => "player " + p + ": " + r.wins[p])
    .join(", ");

  summaryEl.innerHTML = "";
  const cards = [
    ["Outcome (match 1)", res.outcome],
    ["Matches", r.matches],
    ["Wins", winLines || "0"],
    ["Draws", r.draws],
    ["Errors", r.errors],
    ["Avg moves", r.avgMoves.toFixed(2)],
    ["Avg turns", r.avgTurns.toFixed(2)],
  ];
  for (const [label, value] of cards) {
    const d = document.createElement("div");
    d.innerHTML = "<strong>" + label + "</strong><br>" + value;
    summaryEl.appendChild(d);
  }

  logEl.textContent = (res.log || []).join("\n");

  movesBody.innerHTML = "";
  for (const e of res.entries || []) {
    const tr = document.createElement("tr");
    const cells = [
      e.step,
      e.turn,
      e.player,
      e.move,
      JSON.stringify(e.args || []),
      '<span class="board">' + (e.board || "") + "</span>",
    ];
    tr.innerHTML = cells.map((c) => "<td>" + c + "</td>").join("");
    movesBody.appendChild(tr);
  }

  outputEl.hidden = false;
}

runBtn.addEventListener("click", run);
loadWasm();
