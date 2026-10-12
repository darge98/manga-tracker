# Ambiente Docker Compose

## Avvio

Servono Docker Engine attivo e Docker Compose. Non servono Go, Node.js o PostgreSQL installati sull'host. Dalla cartella del progetto:

```bash
cp .env.example .env
chmod 600 .env
docker compose up --build -d
docker compose ps
```

Aprire `http://localhost:8080`. `APP_PORT` cambia la porta pubblicata; `APP_BIND_ADDRESS` è `127.0.0.1` per l'ambiente locale. La password nell'esempio è solo per sviluppo locale: sostituire `POSTGRES_PASSWORD` in `.env` prima della prima inizializzazione, usando un valore URL-safe (ad esempio il risultato di `openssl rand -hex 24`). Non aggiungere `.env` al repository. `docker compose config --quiet` valida la configurazione senza stampare credenziali.

Lo stack comprende Angular compilato e servito da Nginx non-root, API Go non-root e PostgreSQL 18. Solo la UI espone una porta host; Nginx inoltra `/api` al backend, preservando URI e `Host`. Backend e database non pubblicano porte. La rete del database è interna; il backend dispone anche della rete applicativa per contattare l'API di MyAnimeList. Compose attende PostgreSQL healthy, poi l'API healthy, prima di avviare la UI. Le immagini si costruiscono in più stage e i contesti escludono dipendenze locali, file dati e configurazioni private.

`DATABASE_URL` viene costruito da Compose. All'avvio il backend applica le migrazioni mancanti; `/api/health` verifica il database e risponde 503 se non è raggiungibile. Il backend è raggiungibile solo tramite Nginx, per questo Compose imposta `TRUST_PROXY=true`: i limiti di frequenza usano l'IP reale del client (`X-Real-IP`) invece di quello di Nginx. `COOKIE_SECURE=false` serve per HTTP locale; per HTTPS impostarlo a `true` e configurare `DOCKER_ALLOWED_ORIGINS` con gli indirizzi pubblici effettivi.

## Operazioni quotidiane

```bash
docker compose logs -f frontend backend db
docker compose ps
docker compose down
```

`down` ferma l'ambiente e conserva il volume `postgres_data`. Il riavvio con `docker compose up -d` recupera utenti, sessioni, capitoli, feedback e liste dal database. Non usare `down --volumes` per un normale arresto: elimina i dati persistenti. Cambiare le variabili di inizializzazione PostgreSQL dopo aver creato il volume non modifica automaticamente account o password esistenti.

## Backup e ripristino

Creare un dump del database in esecuzione:

```bash
mkdir -p backups
docker compose exec -T db sh -c 'pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Fc' > backups/manga-track.dump
```

Il dump contiene dati personali e deve rimanere privato. Ripristinarlo in un nuovo ambiente con database vuoto, senza tabelle applicative: avviare soltanto PostgreSQL prima del ripristino, evitando che il backend crei lo schema:

```bash
docker compose up -d --wait db
docker compose exec -T db sh -c 'pg_restore -U "$POSTGRES_USER" -d "$POSTGRES_DB" --exit-on-error --no-owner --no-acl' < backups/manga-track.dump
docker compose up -d
```

Se il database contiene già dati, non applicare il dump sopra di essi: ripristinare prima in un nuovo ambiente o database vuoto. Il comando non elimina automaticamente i dati esistenti.

## Diagnostica e verifica

`docker compose exec backend wget -qO- http://127.0.0.1:8080/api/health` deve mostrare stato `ok` e storage `postgres`. Visitare la UI, registrare due account e verificare l'isolamento di diario e liste; dopo `down`/`up -d`, le letture devono essere ancora presenti. I test in-process non sostituiscono questa prova.

Per eseguire i test Go e di integrazione PostgreSQL senza Go sull'host:

```bash
docker compose --profile test run --build --rm backend-test
docker compose --profile test stop testdb
```

Il profilo usa `TEST_DATABASE_URL` verso un database dedicato `manga_track_test`, su una rete separata e con i dati in memoria. Non usa il volume applicativo. Ogni test crea uno schema proprio e lo elimina alla fine.

Fonti primarie: [ordine di avvio Compose](https://docs.docker.com/compose/how-tos/startup-order/), [immagine PostgreSQL ufficiale](https://hub.docker.com/_/postgres), [Nginx non-root](https://github.com/nginx/docker-nginx-unprivileged). PostgreSQL 18 usa il volume in `/var/lib/postgresql`, che questo Compose monta nel percorso corretto.
