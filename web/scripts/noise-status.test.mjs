import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";
import vm from "node:vm";
import ts from "typescript";

const web = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const source = fs.readFileSync(path.join(web, "src/app/(main)/system/settings/noise-profile-card.tsx"), "utf8");
const output = ts.transpileModule(source, {
  compilerOptions: { module: ts.ModuleKind.CommonJS, jsx: ts.JsxEmit.ReactJSX, target: ts.ScriptTarget.ES2022 },
}).outputText;
const exports = {};
vm.runInNewContext(output, {
  exports,
  require: (name) => name === "@/i18n" ? { t: (value) => value } : {},
});
const { isNoiseStatus } = exports;

test("accepts the backend noise-status contract", () => {
  assert.equal(isNoiseStatus({ profile: "off", hosts: [] }), true);
  assert.equal(isNoiseStatus({
    profile: "quiet",
    hosts: [{ host: "example.test", rpm: 1, peak_rpm: 2, inflight: 0, peak_inflight: 1, duplicates_avoided: 3 }],
  }), true);
});

test("rejects incomplete and malformed noise-status responses before rendering", () => {
  for (const value of [null, {}, { profile: "off" }, { profile: "invalid", hosts: [] }, { profile: "off", hosts: [null] }, { profile: "off", hosts: [{ host: "example.test" }] }]) {
    assert.equal(isNoiseStatus(value), false);
  }
});
