#!/usr/bin/env node
// -----------------------------------------------------------------------------
// Persistent babel-preset-solid transform worker.
// -----------------------------------------------------------------------------
// Protocol: newline-delimited JSON over stdio (NDJSON).
//   Request : {"id": <number>, "filename": <string>, "code": <string>,
//              "generate": "dom" | "ssr", "hydratable": <bool>}
//   Response: {"id": <number>, "ok": true, "code": <string>}
//         or  {"id": <number>, "ok": false, "error": <string>}
//
// One process handles many requests sequentially; babel and its preset are
// imported ONCE and stay warm. The Go side owns concurrency by running a pool
// of these; each worker is single-threaded and processes one request at a time.
//
// stdout carries ONLY protocol JSON (one object per line). Anything diagnostic
// goes to stderr so it can never corrupt the framing.

import { createInterface } from "node:readline";
import { transformAsync } from "@babel/core";
import solid from "babel-preset-solid";

function log(...args) {
  process.stderr.write("[transform-worker] " + args.join(" ") + "\n");
}

async function handle(req) {
  const generate = req.generate === "ssr" ? "ssr" : "dom";
  const res = await transformAsync(req.code, {
    presets: [[solid, { generate, hydratable: !!req.hydratable }]],
    filename: req.filename || "component.tsx",
    // babel-preset-solid does the JSX transform but NOT TypeScript stripping;
    // we still parse TS/JSX syntax so annotations don't break the parse.
    // esbuild does the actual type stripping downstream.
    parserOpts: { plugins: ["typescript", "jsx"] },
    // Keep it a pure syntactic transform: no config files, no env lookups.
    babelrc: false,
    configFile: false,
    sourceMaps: false,
  });
  return res.code;
}

const rl = createInterface({ input: process.stdin, crlfDelay: Infinity });

rl.on("line", async (line) => {
  line = line.trim();
  if (!line) return;
  let req;
  try {
    req = JSON.parse(line);
  } catch (err) {
    process.stdout.write(
      JSON.stringify({ id: -1, ok: false, error: "bad request JSON: " + err.message }) + "\n",
    );
    return;
  }
  try {
    const code = await handle(req);
    process.stdout.write(JSON.stringify({ id: req.id, ok: true, code }) + "\n");
  } catch (err) {
    process.stdout.write(
      JSON.stringify({ id: req.id, ok: false, error: String(err && err.stack ? err.stack : err) }) + "\n",
    );
  }
});

rl.on("close", () => process.exit(0));

// Signal readiness on stderr so the Go supervisor can wait for warmup.
log("ready");
