import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

// Next's static export writes route-segment payloads under __next.!*/route/...
// while client navigation requests the same path with dots instead of slashes.
// Materialize those flat names so ordinary static hosts and the embedded Go UI
// serve the payload itself rather than a 404 or an HTML fallback.
export function flattenRscAliases(outDir) {
  let created = 0;

  function filesUnder(dir) {
    const files = [];
    for (const entry of fs.readdirSync(dir, { withFileTypes: true }).sort((a, b) => a.name.localeCompare(b.name))) {
      const full = path.join(dir, entry.name);
      if (entry.isDirectory()) files.push(...filesUnder(full));
      else if (entry.isFile()) files.push(full);
    }
    return files;
  }

  function visit(dir) {
    for (const entry of fs.readdirSync(dir, { withFileTypes: true }).sort((a, b) => a.name.localeCompare(b.name))) {
      if (!entry.isDirectory()) continue;
      const nestedDir = path.join(dir, entry.name);
      if (!entry.name.startsWith("__next.!")) {
        visit(nestedDir);
        continue;
      }
      for (const source of filesUnder(nestedDir)) {
        const relative = path.relative(nestedDir, source);
        const alias = path.join(dir, `${entry.name}.${relative.split(path.sep).join(".")}`);
        if (fs.existsSync(alias)) {
          if (!fs.readFileSync(alias).equals(fs.readFileSync(source))) {
            throw new Error(`Static RSC alias collision: ${alias}`);
          }
          continue;
        }
        fs.copyFileSync(source, alias);
        created++;
      }
    }
  }

  visit(outDir);
  return created;
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const outDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../out");
  if (!fs.statSync(outDir).isDirectory()) throw new Error(`Static export missing: ${outDir}`);
  const created = flattenRscAliases(outDir);
  process.stdout.write(`Static RSC aliases created: ${created}\n`);
}
