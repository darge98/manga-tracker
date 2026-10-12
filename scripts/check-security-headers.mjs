// The security headers exist twice: in Nginx (Docker Compose) and in
// vercel.json (production). Fail if the two copies drift apart.
import { readFileSync } from "node:fs";

const nginx = new Map(
  [...readFileSync("frontend/security-headers.conf", "utf8").matchAll(/^add_header\s+(\S+)\s+"([^"]*)"\s+always;/gm)].map(
    ([, key, value]) => [key.toLowerCase(), value],
  ),
);
// vercel.json applies them to every service through a top-level rule.
const rule = (JSON.parse(readFileSync("vercel.json", "utf8")).headers ?? []).find((h) => h.source === "/(.*)");
const vercel = new Map((rule?.headers ?? []).map(({ key, value }) => [key.toLowerCase(), value]));

const problems = [];
if (nginx.size === 0) problems.push("no headers found in frontend/security-headers.conf");
for (const key of new Set([...nginx.keys(), ...vercel.keys()])) {
  if (nginx.get(key) !== vercel.get(key)) {
    problems.push(`${key}\n  nginx:  ${nginx.get(key) ?? "(missing)"}\n  vercel: ${vercel.get(key) ?? "(missing)"}`);
  }
}
if (problems.length) {
  console.error("Security headers differ between Nginx and vercel.json:\n" + problems.join("\n"));
  process.exit(1);
}
console.log(`Security headers in sync (${nginx.size} headers).`);
