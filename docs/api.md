# Manga Track API

Go 1.21+, base URL `http://localhost:8080/api`. JSON camelCase. Errors: `{"error":"Human readable message"}`. All request bodies use `Content-Type: application/json`. Browser requests use `credentials: 'include'` (Angular `withCredentials: true`). No bearer token.

## Authentication

| Method | Path | Request | Response |
|---|---|---|---|
| POST | `/auth/register` | `{username,email,password}` | `{user}` and session cookie |
| POST | `/auth/login` | `{email,password}` | `{user}` and session cookie |
| GET | `/auth/me` | — | `{user}` or 401 |
| POST | `/auth/logout` | `{}` | `{ok:true}`, session revoked |

User: `{id,username,email}`. Password 10–256 bytes; username 2–50 bytes; valid unique email normalized to lowercase. Registration also signs in. Session cookie `manga_session`: HttpOnly, SameSite=Lax, 30 day lifetime, Secure when `COOKIE_SECURE=true`. Only SHA256 token digests persist. Passwords use PBKDF2-HMAC-SHA256, 600,000 iterations, independent 128-bit random salts. Auth attempts are limited to 15/minute per connecting IP (no trust of forwarded headers).

Mutating requests validate allowed Origin whenever supplied. Cross-site browser requests without Origin are denied. JSON-only writes prevent simple form submission. Allowed origins default to `http://localhost:4200,http://127.0.0.1:4200`; explicitly configure production origins. Anonymous callers get 401 for private endpoints. Inaccessible lists return 404.

## Catalog

Live metadata from [Jikan v4](https://docs.api.jikan.moe/), IDs are MyAnimeList manga IDs. There is no runtime fake catalog or credential requirement. Uncached upstream requests are serialized at <60/minute with a 30 minute in-process response cache. Upstream outages/rate limits become 503 with an actionable message; missing manga becomes 404. Known manga detail pages fall back to the persisted metadata with a `catalogWarning` when the provider is unavailable.

Manga:

```ts
interface Manga {
  id: number;
  title: string;
  description: string;
  coverUrl: string;
  chapters: number | null;
  status: string;
  genres: string[];
  score: number | null;
  author: string;
  url: string; // original MAL page
}
```

`chapters` is nullable metadata, never a verified list of individual chapters. Jikan supplies cover images and metadata, not manga reading pages or chapter image catalogs. UI users enter chapter numbers manually, including decimals such as 12.5; textual special chapter labels are not supported in this MVP.

| Method | Path | Response |
|---|---|---|
| GET | `/health` | `{status:"ok",provider:"jikan"}` |
| GET | `/manga?q=...&page=1` | `{items:Manga[],page,hasNextPage}` |
| GET | `/manga/popular?page=1` | same page shape |
| GET | `/manga/:id` | `{manga:Manga,catalogWarning:string}` |

Page size 24. Search includes `sfw=true`; query max 200 bytes. Catalog endpoints are public. Catalog metadata is persisted once fetched, allowing existing library/list content to remain available during an upstream outage.

## Personal library and dashboard

```ts
interface ChapterProgress {
  chapter: string; // normalized numeric text, e.g. "1", "12.5"
  read: boolean;
  readAt: string | null; // ISO8601, preserved when feedback changes
  rating: number | null; // 1 through 5
  notes: string;         // max 5000 bytes
}
interface Library {
  mangaId: number;
  chapters: ChapterProgress[];
  toRead: boolean;
  favorite: boolean;
}
```

| Method | Path | Request | Response |
|---|---|---|---|
| GET | `/library/:id` | — | `Library` (empty default for unseen manga) |
| PUT | `/library/:id` | `{toRead?,favorite?,manga?}` | updated `Library` |
| PUT | `/library/:id/chapters/:chapter` | `{read,rating?,notes?,manga?}` | updated `Library` |
| GET | `/library?kind=favorites` | — | `{items:Manga[]}` |
| GET | `/library?kind=to-read` | — | `{items:Manga[]}` |
| GET | `/dashboard` | — | `{recent,popular,catalogWarning}` |

Chapter input must be numeric >0 and <=100000, with optional decimal. Progress is isolated by authenticated user and manga. Chapter PUT replaces feedback for that chapter: omitted rating becomes null and omitted notes becomes empty. Setting read=false clears readAt. Unread chapter feedback may still be retained. Re-marking read does not alter prior readAt unless the chapter was made unread.

The optional `manga` field is accepted for frontend compatibility but never trusted as a catalog source. Metadata comes exclusively from Jikan or its persisted server cache. Normally browsing has already persisted the ID. If no persisted manga exists, the provider resolves it before mutation; an unavailable provider returns 503 without changing the library/list.

`recent` contains at most 12 distinct manga entries `{manga,chapter,readAt,rating,notes}`, newest read first. Each manga appears once with its latest reading event; the complete chapter history remains in `/library/:id`. `popular` is a Manga array. `catalogWarning` is an empty string normally, or a provider warning while local history still returns 200. Personal feedback is never included in public lists.

## Custom lists

```ts
interface List {
  id: string;
  name: string;
  description: string;
  isPublic: boolean;
  itemCount: number;
  createdAt: string;
}
```

| Method | Path | Request | Response |
|---|---|---|---|
| GET | `/lists` | — | `{items:List[]}` owned by caller |
| POST | `/lists` | `{name,description,isPublic}` | 201 `{list,items:[]}` |
| GET | `/lists/:id` | — | `{list,items:Manga[]}` owner only |
| PATCH | `/lists/:id` | `{name?,description?,isPublic?}` | `{list}` |
| DELETE | `/lists/:id` | — | `{ok:true}` |
| PUT | `/lists/:id/items/:mangaId` | `{manga?}` or `{}` | `{list}` |
| DELETE | `/lists/:id/items/:mangaId` | — | `{list}` |
| GET | `/public/lists/:id` | — | `{list,items:Manga[]}` if public, else 404 |

Name 1–100 bytes, description max 1000 bytes. Membership is unique. Public reads do not require login and disclose no email, owner ID, chapter progress or feedback. Setting isPublic=false immediately revokes public access. Public access never grants write access.

## Storage and execution

Run `bash scripts/dev.sh` from the project root. With a compatible Go toolchain, backend-only development also supports `cd backend && go run ./cmd/server`. SIGINT/SIGTERM stop new connections and allow active requests up to 15 seconds before closing the archive lock. Environment: optional `DATABASE_URL` selects PostgreSQL; when absent `DATA_FILE` selects the JSON archive. `HOST=127.0.0.1`, `PORT=8080`, `DATA_FILE=data/manga-track.json`, `ALLOWED_ORIGINS=http://localhost:4200,http://127.0.0.1:4200`, `COOKIE_SECURE=false`.

Native JSON builds use only the Go standard library. PostgreSQL builds use the pinned standard `github.com/lib/pq v1.10.9` driver and the `postgres` build tag. The JSON repository remains available for existing native development data and is protected by an in-process mutex and a Unix advisory file lock. A second process opening the same DATA_FILE is refused. Mutations clone state and persist through a 0600 temporary file, fsync and atomic rename before publication; failures do not expose unsaved mutations. Invalid/corrupt files fail startup instead of silently resetting users. Directory is created as 0700. Database, lock and temporary files stay outside version control.

Supported server platforms: macOS and Linux. JSON storage requires a single process and local persistent filesystem; it is not suitable for shared network filesystems. PostgreSQL provides actual database persistence with transaction-level coordination between backend processes. Use HTTPS, secure cookie, explicit origins and backups for production. The current low-volume repository still reads complete state snapshots; it is not a claim of unrestricted scalability.

Tests: `cd backend && go test -race ./...`. On this macOS with Go 1.21, the internal linker causes `dyld: missing LC_UUID`; the validated test workaround is `go test -race -ldflags=-linkmode=external ./...`. The externally linked server also reproduced an invalid ad-hoc signature: macOS crash reports explicitly identified `SIGKILL (Code Signature Invalid)` / `CODESIGNING: Invalid Page`. Signing the final generated binary using `codesign --force --sign -` and verifying it with `codesign --verify` resolved that startup failure in two consecutive rebuilds. This signs only our local artifact and does not change machine security settings. The development script applies the macOS build/sign/verify sequence before launch. The test catalog/HTTP transport fixtures exist only in tests. Tests cover authentication, cookie flags, session revocation and persistence, cross-user privacy, public list transitions, chapter feedback/decimals, persistence rollback/concurrency/file lock, PBKDF2 vectors and provider field mapping/cache/error handling.

For manual backend startup on this Go 1.21/macOS combination, compile and sign the artifact before execution:

```sh
cd backend
GOCACHE="$PWD/../.dev/go-cache" go build -ldflags=-linkmode=external -o ../.dev/manga-track-server ./cmd/server
codesign --force --sign - ../.dev/manga-track-server
codesign --verify --verbose=4 ../.dev/manga-track-server
../.dev/manga-track-server
```

The successful signed artifact reaches the listener initialization. This managed environment still denies binding local sockets; that separate sandbox limitation is not the cause of the host's earlier signature SIGKILL.


## PostgreSQL and Docker storage

[Docker instructions](docker.md) start PostgreSQL 18, the Go API and the Angular/Nginx frontend. Only the frontend port is published. The backend image includes the real Go PostgreSQL driver:

```sh
cd backend
go build -tags postgres -o server ./cmd/server
DATABASE_URL='postgres://USER:PASSWORD@HOST:5432/DATABASE?sslmode=require' ./server
```

Set the URL in the environment using private configuration; do not commit a real password. Local Compose uses `sslmode=disable` only between services on its internal Docker database network. Native builds without `-tags postgres` reject a configured `DATABASE_URL` with an actionable rebuild message; a connection error never silently switches to JSON. JSON files are not automatically migrated or changed.

Startup checks the database and applies embedded migration `internal/repository/migrations/001_initial.sql` atomically. The schema stores users, hashed sessions, canonical manga metadata, per-user library flags, numeric chapter progress/feedback, owned lists and ordered membership. Foreign keys, unique email/membership and checks for rating 1–5, read timestamps, chapter range and note length enforce the existing API invariants. `/api/health` returns `storage: postgres` after a database readiness check and returns 503 if the database is unavailable; errors omit the connection URL/password.

Repository reads use independent repeatable-read transaction snapshots. No shared PostgreSQL session cache can reintroduce a revoked session through a late read. Mutations acquire a PostgreSQL transaction advisory lock, reload current state, run the existing ownership/business callback and commit row diffs. They update only changed users, sessions, manga, flags, chapters or lists. Membership is replaced only for a changed list to preserve its ordering. The database is not rewritten on each update. The callback interface intentionally favors compatibility with the current low-volume app: each read loads the normalized state and is not yet a collection of optimized per-user SQL queries.

Legacy import is explicit, atomic and restricted to an empty target database:

```sh
# After building the PostgreSQL image and starting only the db service:
docker compose run --rm --no-deps -v "$PWD/path-to-private-copy.json:/legacy.json:ro" backend import-json /legacy.json
```

Follow `docs/docker.md` to prepare a readable private copy for the non-root container. Stop the native backend before copying the JSON. The source is only read; passwords remain hashed, session digests and all chapter/list data are preserved. Invalid/orphaned legacy records or a nonempty database cause refusal and transaction rollback. The import does not delete or replace any existing PostgreSQL data.

PostgreSQL integration tests require a dedicated initially empty test database. They refuse an existing dataset and clean only their fixtures. Packages must run sequentially because API and repository tests share that database:

```sh
# Isolated Docker profile: ephemeral testdb, separate from application volume.
docker compose --profile test run --build --rm backend-test
# Native alternative, using a dedicated PostgreSQL test database only:
cd backend
TEST_DATABASE_URL='postgres://USER:PASSWORD@HOST:5432/manga_track_test?sslmode=require' go test -tags postgres -p 1 -count=1 ./...
```

Tests cover PostgreSQL persistence/reconnect, concurrent writers, constraints/rollback, cross-process session revocation, auth/feedback isolation, public/private lists and read-only legacy import/refusal. These live integration tests remain unverified in the managed environment: the driver download is blocked by shell DNS and the now-present OrbStack socket is denied by sandbox permissions. Default JSON/API tests, the SQL code (always compiled) and `go vet` pass. A skipped or blocked integration command is not a passing PostgreSQL proof.
