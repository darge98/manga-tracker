# Manga Track

Applicazione Angular + Go per tenere il diario delle proprie letture manga: dashboard, ricerca nel catalogo, progresso, feedback per capitolo, preferiti e liste pubbliche o private. Ogni account ha la propria libreria.

## Avvio

Serve solo Docker con Docker Compose. Dalla cartella del progetto:

```bash
cp .env.example .env   # poi cambia POSTGRES_PASSWORD
docker compose up --build -d
```

Aprire `http://localhost:8080`. Compose avvia il frontend Angular servito da Nginx, l'API Go e PostgreSQL 18 con un volume persistente. Solo la UI pubblica una porta sull'host; Nginx inoltra `/api` al backend. `docker compose logs -f` mostra i log, `docker compose down` arresta l'ambiente conservando i dati.

Backup, ripristino e HTTPS: vedere la [guida Docker](docs/docker.md).

### Sviluppo del frontend con hot reload

Con lo stack avviato, `npm start` nella cartella `frontend` (Node 24, vedi `.nvmrc`) serve l'app su `http://localhost:4200` e inoltra `/api` allo stack Docker su `http://127.0.0.1:8080`.

## Test

```bash
# Backend: PostgreSQL di test isolato, dati in memoria
docker compose --profile test run --build --rm backend-test
docker compose --profile test stop testdb

# Frontend
cd frontend && npm ci && npm test && npm run check && npm run build
```

I test del backend girano contro un PostgreSQL reale: ogni test crea il proprio schema e lo elimina alla fine, quindi i test possono girare in parallelo. Con Go 1.26 installato si possono lanciare anche dall'host, indicando un database di test:

```bash
cd backend
TEST_DATABASE_URL='postgres://USER:PASSWORD@127.0.0.1:5432/DB?sslmode=disable' go test -race ./...
```

Senza `TEST_DATABASE_URL` i test che usano il database vengono saltati, e il motivo è indicato nell'output.

## Architettura

- `backend/internal/api`: handler HTTP (`net/http.ServeMux` con pattern), validazione, ownership, rate limit.
- `backend/internal/repository`: accesso a PostgreSQL tramite `pgx` con query mirate, e migrazioni numerate in `migrations/`, applicate all'avvio. Per cambiare lo schema si aggiunge un nuovo file `NNN_descrizione.sql`; i file già applicati non si modificano.
- `backend/internal/provider`: client [Jikan v4](https://docs.api.jikan.moe/) con cache e limite di 60 richieste al minuto verso l'upstream.
- `backend/internal/auth`: hash delle password (PBKDF2) e token di sessione.
- `frontend/`: Angular 22 con componenti standalone.

Contratto completo: [docs/api.md](docs/api.md). [Ricerca e motivazioni](docs/research.md) spiegano la scelta del catalogo.

## Catalogo e dati personali

Il catalogo usa Jikan, un'API pubblica che espone i metadata di MyAnimeList. Tracking, autenticazione, note e liste sono gestiti dall'applicazione. Non è un lettore di scansioni né una sincronizzazione delle liste MAL. Il totale dei capitoli può essere sconosciuto, soprattutto per le serie in corso. Le liste pubbliche sono condivisibili; il diario per capitolo e il progresso personale non vengono mai condivisi.
