#!/usr/bin/env bash
set -euo pipefail

task_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$task_root"

if [[ -f .env ]]; then
  set -a
  source .env
  set +a
fi

task_node_version="$(node --version 2>/dev/null || true)"
if [[ ! "$task_node_version" =~ ^v24\.([0-9]+)\.([0-9]+)$ ]] || (( 10#${BASH_REMATCH[1]:-0} < 15 )); then
  printf 'Node.js non compatibile: %s. Servono Node 24.15+ della serie 24 e npm. Esegui "nvm use" (.nvmrc) oppure correggi il PATH, poi riprova. Nessun servizio è stato avviato.\n' "${task_node_version:-non trovato}" >&2
  exit 1
fi
command -v npm >/dev/null || { echo 'npm non trovato nel PATH. Nessun servizio è stato avviato.' >&2; exit 1; }

command -v go >/dev/null || { echo 'Go non trovato nel PATH.' >&2; exit 1; }

if [[ ! -f frontend/node_modules/@angular/cli/bin/ng.js || ! -f frontend/node_modules/@angular/core/package.json ]]; then
  if [[ -f frontend/package-lock.json ]]; then
    (cd frontend && npm ci)
  else
    (cd frontend && npm install)
  fi
fi

backend_pid=''
frontend_pid=''
backend_build_tmp=''
cleanup() {
  trap - EXIT INT TERM
  [[ -z "$frontend_pid" ]] || kill "$frontend_pid" 2>/dev/null || true
  [[ -z "$backend_pid" ]] || kill "$backend_pid" 2>/dev/null || true
  wait 2>/dev/null || true
  [[ -z "$backend_build_tmp" ]] || rm -f "$backend_build_tmp"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

printf 'Runtime frontend: Node %s (%s)\n' "$task_node_version" "$(command -v node)"
mkdir -p .dev
export GOCACHE="${GOCACHE:-$task_root/.dev/go-cache}"
task_os="$(uname -s)"
if [[ "$task_os" == Darwin ]]; then
  command -v codesign >/dev/null || { echo 'codesign non trovato: servono gli strumenti sviluppatore macOS. Nessun servizio è stato avviato.' >&2; exit 1; }
fi
backend_build_tmp="$(mktemp "$task_root/.dev/manga-track-server.build.XXXXXX")"
if [[ "$task_os" == Darwin ]]; then
  (cd backend && go build -ldflags=-linkmode=external -o "$backend_build_tmp" ./cmd/server)
else
  (cd backend && go build -o "$backend_build_tmp" ./cmd/server)
fi
if [[ "$task_os" == Darwin ]]; then
  echo 'Verifica del binario backend macOS…'
  codesign --force --sign - "$backend_build_tmp"
  codesign --verify "$backend_build_tmp"
fi
mv -f "$backend_build_tmp" "$task_root/.dev/manga-track-server"
backend_build_tmp=''
(cd backend && exec ../.dev/manga-track-server) &
backend_pid=$!
(cd frontend && exec node node_modules/@angular/cli/bin/ng.js serve --host 127.0.0.1 --proxy-config proxy.conf.json) &
frontend_pid=$!

printf 'Avvio richiesto: frontend http://localhost:4200 · API http://%s:%s\n' "${HOST:-127.0.0.1}" "${PORT:-8080}"
report_exit() {
  local task_service="$1" task_status="$2"
  printf '%s si è arrestato (codice %s' "$task_service" "$task_status" >&2
  if (( task_status >= 128 )); then
    printf ', segnale %s' "$((task_status - 128))" >&2
  fi
  printf '). Chiusura degli altri processi.\n' >&2
}
while true; do
  if ! kill -0 "$backend_pid" 2>/dev/null; then
    task_status=0
    wait "$backend_pid" || task_status=$?
    backend_pid=''
    report_exit 'Il backend' "$task_status"
    (( task_status != 0 )) || task_status=1
    exit "$task_status"
  fi
  if ! kill -0 "$frontend_pid" 2>/dev/null; then
    task_status=0
    wait "$frontend_pid" || task_status=$?
    frontend_pid=''
    report_exit 'Il frontend' "$task_status"
    (( task_status != 0 )) || task_status=1
    exit "$task_status"
  fi
  sleep 1
done
