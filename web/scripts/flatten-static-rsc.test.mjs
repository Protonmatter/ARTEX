import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { test } from "node:test";

import { flattenRscAliases } from "./flatten-static-rsc.mjs";

function removeFixture(out) {
  const resolved = fs.realpathSync(out);
  const tempRoot = fs.realpathSync(os.tmpdir());
  if (path.dirname(resolved) !== tempRoot || !path.basename(resolved).startsWith("reconrivet-rsc-")) {
    throw new Error(`Refusing to remove unexpected fixture path: ${resolved}`);
  }
  fs.rmSync(resolved, { recursive: true, force: true });
}

test("creates route and root aliases without changing the source payload", () => {
  const out = fs.mkdtempSync(path.join(os.tmpdir(), "reconrivet-rsc-"));
  try {
    const route = path.join(out, "function", "tasks", "__next.!hash", "function", "tasks", "__PAGE__.txt");
    const root = path.join(out, "__next.!root", "__PAGE__.txt");
    fs.mkdirSync(path.dirname(route), { recursive: true });
    fs.mkdirSync(path.dirname(root), { recursive: true });
    fs.writeFileSync(route, "route payload");
    fs.writeFileSync(root, "root payload");
    assert.equal(flattenRscAliases(out), 2);
    assert.equal(fs.readFileSync(path.join(out, "function", "tasks", "__next.!hash.function.tasks.__PAGE__.txt"), "utf8"), "route payload");
    assert.equal(fs.readFileSync(path.join(out, "__next.!root.__PAGE__.txt"), "utf8"), "root payload");
    assert.equal(flattenRscAliases(out), 0);
    assert.equal(fs.readFileSync(route, "utf8"), "route payload");
  } finally {
    removeFixture(out);
  }
});

test("refuses to overwrite a different existing alias", () => {
  const out = fs.mkdtempSync(path.join(os.tmpdir(), "reconrivet-rsc-"));
  try {
    const source = path.join(out, "tasks", "__next.!hash", "tasks", "__PAGE__.txt");
    const alias = path.join(out, "tasks", "__next.!hash.tasks.__PAGE__.txt");
    fs.mkdirSync(path.dirname(source), { recursive: true });
    fs.writeFileSync(source, "expected");
    fs.writeFileSync(alias, "different");
    assert.throws(() => flattenRscAliases(out), /collision/);
    assert.equal(fs.readFileSync(alias, "utf8"), "different");
  } finally {
    removeFixture(out);
  }
});
