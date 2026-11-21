import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";
import vm from "node:vm";
import ts from "typescript";

const web = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const compile = (file) => ts.transpileModule(fs.readFileSync(path.join(web, file), "utf8"), {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
}).outputText;
const authOutput = compile("src/lib/auth.ts");
const localeOutput = compile("src/i18n/index.ts");

function setup(initial = {}, mock = false) {
  const values = new Map(Object.entries(initial));
  const cookies = [];
  const exports = {};
  vm.runInNewContext(authOutput, {
    exports,
    window: {},
    localStorage: {
      getItem: (key) => values.get(key) ?? null,
      setItem: (key, value) => values.set(key, value),
      removeItem: (key) => values.delete(key),
    },
    document: { set cookie(value) { cookies.push(value); } },
    process: { env: { NEXT_PUBLIC_MOCK: mock ? "1" : "" } },
    atob,
  });
  return { auth: exports.auth, values, cookies };
}

test("stores, reads, and clears the ReconRivet session", () => {
  const payload = Buffer.from(JSON.stringify({ sub: "ReconRivet" })).toString("base64url");
  const token = `header.${payload}.signature`;
  const s = setup();
  s.auth.setToken(token);
  assert.equal(s.auth.getToken(), token);
  assert.equal(s.values.get("reconrivet_token"), token);
  assert.equal(s.auth.getCurrentUser().name, "ReconRivet");
  assert.match(s.cookies[0], /^reconrivet_token=/);
  s.auth.clearToken();
  assert.equal(s.auth.getToken(), null);
  assert.equal(s.values.size, 0);
  assert.match(s.cookies[1], /^reconrivet_token=; path=\/; max-age=0$/);
});

test("mock mode still permits direct UI entry", () => {
  assert.equal(setup({}, true).auth.getToken(), "mock-demo");
});

test("the saved ReconRivet locale takes precedence over the browser locale", () => {
  const exports = {};
  vm.runInNewContext(localeOutput, {
    exports,
    require: () => ({}),
    window: {
      localStorage: { getItem: (key) => key === "reconrivet.locale" ? "tr" : null },
      navigator: { languages: ["en-US"], language: "en-US" },
    },
    document: { documentElement: {} },
  });
  assert.equal(exports.locale, "tr");
});
