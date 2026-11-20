import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";
import vm from "node:vm";
import ts from "typescript";

const web = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const output = ts.transpileModule(fs.readFileSync(path.join(web, "src/lib/auth.ts"), "utf8"), {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
}).outputText;
const storageOutput = ts.transpileModule(fs.readFileSync(path.join(web, "src/lib/local-storage.client.ts"), "utf8"), {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
}).outputText;
const localeOutput = ts.transpileModule(fs.readFileSync(path.join(web, "src/i18n/index.ts"), "utf8"), {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
}).outputText;

function setup(initial = {}, mock = false, writeFails = false) {
  const values = new Map(Object.entries(initial));
  const cookies = [];
  const exports = {};
  vm.runInNewContext(output, {
    exports,
    window: {},
    localStorage: {
      getItem: (key) => values.get(key) ?? null,
      setItem: (key, value) => {
        if (writeFails) throw new Error("storage write rejected");
        values.set(key, value);
      },
      removeItem: (key) => values.delete(key),
    },
    document: { set cookie(value) { cookies.push(value); } },
    process: { env: { NEXT_PUBLIC_MOCK: mock ? "1" : "" } },
    atob,
  });
  return { auth: exports.auth, values, cookies };
}

test("legacy token migrates to the new storage key and cookie", () => {
  const s = setup({ artex_token: "legacy-session" });
  assert.equal(s.auth.getToken(), "legacy-session");
  assert.equal(s.values.get("reconrivet_token"), "legacy-session");
  assert.equal(s.values.has("artex_token"), false);
  assert.match(s.cookies[0], /^reconrivet_token=legacy-session;/);
  assert.match(s.cookies[1], /^artex_token=; path=\/; max-age=0$/);
});

test("legacy token remains usable when storage migration fails", () => {
  const s = setup({ artex_token: "legacy-session" }, false, true);
  assert.equal(s.auth.getToken(), "legacy-session");
  assert.equal(s.values.get("artex_token"), "legacy-session");
  assert.equal(s.values.has("reconrivet_token"), false);
});

test("new token takes precedence and logout clears both names", () => {
  const s = setup({ reconrivet_token: "new-session", artex_token: "legacy-session" });
  assert.equal(s.auth.getToken(), "new-session");
  s.auth.clearToken();
  assert.equal(s.values.size, 0);
  assert.equal(s.cookies.length, 2);
});

test("legacy JWT subject displays the new administrator name", () => {
  const payload = Buffer.from(JSON.stringify({ sub: "ARTEX" })).toString("base64url");
  const s = setup({ reconrivet_token: `header.${payload}.signature` });
  assert.equal(s.auth.getCurrentUser().name, "ReconRivet");
});

test("mock mode still permits direct UI entry", () => {
  const s = setup({}, true);
  assert.equal(s.auth.getToken(), "mock-demo");
});

test("legacy browser preferences migrate without overwriting new preferences", () => {
  const values = new Map([
    ["artex_chat_send_mode", "ctrl-enter"],
    ["reconrivet_task_list_sort", "new-sort"],
    ["artex_task_list_sort", "old-sort"],
  ]);
  const exports = {};
  vm.runInNewContext(storageOutput, {
    exports,
    window: { localStorage: {
      getItem: (key) => values.get(key) ?? null,
      setItem: (key, value) => values.set(key, value),
      removeItem: (key) => values.delete(key),
    } },
  });
  assert.equal(exports.getLocalStorageValue("reconrivet_chat_send_mode"), "ctrl-enter");
  assert.equal(values.get("reconrivet_chat_send_mode"), "ctrl-enter");
  assert.equal(values.has("artex_chat_send_mode"), false);
  assert.equal(exports.getLocalStorageValue("reconrivet_task_list_sort"), "new-sort");
});

test("saved legacy locale survives a rejected migration write", () => {
  const values = new Map([["artex.locale", "tr"]]);
  const exports = {};
  vm.runInNewContext(localeOutput, {
    exports,
    require: () => ({}),
    window: {
      localStorage: {
        getItem: (key) => values.get(key) ?? null,
        setItem: () => { throw new Error("storage write rejected"); },
        removeItem: (key) => values.delete(key),
      },
      navigator: { languages: ["en-US"], language: "en-US" },
    },
    document: { documentElement: {} },
    process: { env: {} },
  });
  assert.equal(exports.locale, "tr");
  assert.equal(values.get("artex.locale"), "tr");
});
