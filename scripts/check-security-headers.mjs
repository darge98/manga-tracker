// The security headers exist twice: in Nginx (Docker Compose) and in
// vercel.json (production). Fail if the two copies drift apart.
import { readFileSync } from "node:fs";

const nginx = new Map(
  [...readFileSync("frontend/security-headers.conf", "utf8").matchAll(/^add_header\s+(\S+)\s+"([^"]*)"\s+always;/gm)].map(
    ([, key, value]) => [key.toLowerCase(), value],
  ),
);
// Vercel applies headers per service, so every service carries its own copy.
const services = Object.entries(JSON.parse(readFileSync("vercel.json", "utf8")).services ?? {});

const problems = [];
if (nginx.size === 0) problems.push("no headers found in frontend/security-headers.conf");
if (services.length === 0) problems.push("no services found in vercel.json");
for (const [name, service] of services) {
  const rule = (service.headers ?? []).find((h) => h.source === "/(.*)");
  const vercel = new Map((rule?.headers ?? []).map(({ key, value }) => [key.toLowerCase(), value]));
  for (const key of new Set([...nginx.keys(), ...vercel.keys()])) {
    if (nginx.get(key) !== vercel.get(key)) {
      problems.push(`${name}: ${key}\n  nginx:  ${nginx.get(key) ?? "(missing)"}\n  vercel: ${vercel.get(key) ?? "(missing)"}`);
    }
  }
}
if (problems.length) {
  console.error("Security headers differ between Nginx and vercel.json:\n" + problems.join("\n"));
  process.exit(1);
}
console.log(`Security headers in sync (${nginx.size} headers, ${services.length} Vercel services).`);
