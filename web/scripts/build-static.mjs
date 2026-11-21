import { spawnSync } from "node:child_process";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { flattenRscAliases } from "./flatten-static-rsc.mjs";

const webRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const nextCLI = path.join(webRoot, "node_modules", "next", "dist", "bin", "next");
const build = spawnSync(process.execPath, [nextCLI, "build"], {
  cwd: webRoot,
  env: { ...process.env, NEXT_EXPORT: "1" },
  stdio: "inherit",
});
if (build.error) throw build.error;
if (build.status !== 0) process.exit(build.status ?? 1);

const aliases = flattenRscAliases(path.join(webRoot, "out"));
process.stdout.write(`Static RSC aliases created: ${aliases}\n`);
