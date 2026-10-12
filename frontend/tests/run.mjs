import { build } from "esbuild";
import { spawnSync } from "node:child_process";
await build({
  entryPoints: ["tests/frontend.test.ts"],
  outfile: ".tmp-tests/frontend.test.mjs",
  bundle: true,
  platform: "node",
  format: "esm",
  packages: "external",
  tsconfig: "tsconfig.json",
});
const result = spawnSync(
  process.execPath,
  ["--test", ".tmp-tests/frontend.test.mjs"],
  { stdio: "inherit" },
);
process.exitCode = result.status ?? 1;
