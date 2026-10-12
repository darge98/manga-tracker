#!/usr/bin/env bash
set -euo pipefail
task_repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
task_fixture="$(mktemp -d "${TMPDIR:-/tmp}/manga-dev-test.XXXXXX")"
trap 'rm -rf "$task_fixture"' EXIT
mkdir -p "$task_fixture/scripts" "$task_fixture/backend" "$task_fixture/bin" "$task_fixture/frontend/node_modules/@angular/cli/bin" "$task_fixture/frontend/node_modules/@angular/core"
cp "$task_repo/scripts/dev.sh" "$task_fixture/scripts/dev.sh"
touch "$task_fixture/frontend/node_modules/@angular/cli/bin/ng.js" "$task_fixture/frontend/node_modules/@angular/core/package.json"
export MANGA_TEST_LOG="$task_fixture/events.log"
cat > "$task_fixture/bin/node" <<'STUB'
#!/usr/bin/env bash
if [[ "${1:-}" == --version ]]; then printf '%s\n' "$MANGA_TEST_NODE_VERSION"; exit 0; fi
echo frontend-start >> "$MANGA_TEST_LOG"
if [[ "$MANGA_TEST_FRONTEND_EXIT" != wait ]]; then exit "$MANGA_TEST_FRONTEND_EXIT"; fi
trap 'echo frontend-stop >> "$MANGA_TEST_LOG"; exit 0' TERM
while true; do sleep 0.1; done
STUB
cat > "$task_fixture/bin/npm" <<'STUB'
#!/usr/bin/env bash
echo unexpected-npm >> "$MANGA_TEST_LOG"
exit 99
STUB
cat > "$task_fixture/bin/uname" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' "$MANGA_TEST_OS"
STUB
cat > "$task_fixture/bin/codesign" <<'STUB'
#!/usr/bin/env bash
echo "codesign $1" >> "$MANGA_TEST_LOG"
if [[ "$1" == --verify ]]; then exit "$MANGA_TEST_VERIFY_EXIT"; fi
exit "$MANGA_TEST_SIGN_EXIT"
STUB
cat > "$task_fixture/bin/go" <<'STUB'
#!/usr/bin/env bash
echo "go $*" >> "$MANGA_TEST_LOG"
while [[ $# -gt 0 ]]; do
  if [[ "$1" == -o ]]; then shift; artifact="$1"; break; fi
  shift
done
cat > "$artifact" <<'SERVER'
#!/usr/bin/env bash
echo backend-start >> "$MANGA_TEST_LOG"
if [[ "$MANGA_TEST_BACKEND_EXIT" == kill9 ]]; then kill -KILL $$; fi
if [[ "$MANGA_TEST_BACKEND_EXIT" != wait ]]; then exit "$MANGA_TEST_BACKEND_EXIT"; fi
trap 'echo backend-stop >> "$MANGA_TEST_LOG"; exit 0' TERM
while true; do sleep 0.1; done
SERVER
chmod +x "$artifact"
STUB
chmod +x "$task_fixture/bin/"*
export PATH="$task_fixture/bin:/usr/bin:/bin"
export MANGA_TEST_OS=Linux MANGA_TEST_SIGN_EXIT=0 MANGA_TEST_VERIFY_EXIT=0 MANGA_TEST_FRONTEND_EXIT=42 MANGA_TEST_BACKEND_EXIT=wait
run_dev() {
  : > "$MANGA_TEST_LOG"
  task_status=0
  bash "$task_fixture/scripts/dev.sh" > "$task_fixture/output" 2>&1 || task_status=$?
  task_events="$(cat "$MANGA_TEST_LOG")"
  task_output="$(cat "$task_fixture/output")"
}
assert() { if ! "$@"; then printf 'FAIL: %s\nOutput: %s\nEvents: %s\n' "$*" "$task_output" "$task_events" >&2; exit 1; fi; }
export MANGA_TEST_NODE_VERSION=v22.14.0
run_dev
assert test "$task_status" -eq 1
assert test -z "$task_events"
assert test "${task_output#*Nessun servizio}" != "$task_output"
export MANGA_TEST_NODE_VERSION=v24.14.1
run_dev
assert test "$task_status" -eq 1
assert test -z "$task_events"
export MANGA_TEST_NODE_VERSION=v24.21.0
run_dev
assert test "$task_status" -eq 42
assert test "${task_events#*backend-start}" != "$task_events"
assert test "${task_events#*frontend-start}" != "$task_events"
assert test "${task_output#*Il frontend si è arrestato (codice 42}" != "$task_output"
export MANGA_TEST_OS=Darwin
run_dev
assert test "$task_status" -eq 42
assert test "${task_events#*codesign --force}" != "$task_events"
assert test "${task_events#*codesign --verify}" != "$task_events"
assert test "${task_events#*-ldflags=-linkmode=external}" != "$task_events"
export MANGA_TEST_SIGN_EXIT=73
run_dev
assert test "$task_status" -eq 73
assert test "${task_events#*backend-start}" = "$task_events"
assert test "${task_events#*frontend-start}" = "$task_events"
export MANGA_TEST_SIGN_EXIT=0 MANGA_TEST_VERIFY_EXIT=74
run_dev
assert test "$task_status" -eq 74
assert test "${task_events#*codesign --verify}" != "$task_events"
assert test "${task_events#*backend-start}" = "$task_events"
assert test "${task_events#*frontend-start}" = "$task_events"
export MANGA_TEST_SIGN_EXIT=0 MANGA_TEST_VERIFY_EXIT=0 MANGA_TEST_OS=Linux MANGA_TEST_BACKEND_EXIT=kill9 MANGA_TEST_FRONTEND_EXIT=wait
run_dev
assert test "$task_status" -eq 137
assert test "${task_output#*Il backend si è arrestato (codice 137, segnale 9)}" != "$task_output"
printf 'PASS: 7 scenari avvio (preflight, runtime PATH, arresto frontend, firma macOS, firma/verifica fallita, SIGKILL backend).\n'
