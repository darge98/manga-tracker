# Manga Track API

Go 1.26, base URL `http://localhost:8080/api` (through the Nginx frontend). JSON camelCase. Errors: `{"code":"email_taken","error":"Human readable message"}`. `code` is stable and meant for clients (the frontend translates it); `error` is English text for API consumers. All request bodies use `Content-Type: application/json`. Browser requests use `credentials: 'include'` (Angular `withCredentials: true`). No bearer token.

## Authentication

| Method | Path | Request | Response |
|---|---|---|---|
| POST | `/auth/register` | `{username,email,password}` | `{user}` and session cookie |
| POST | `/auth/login` | `{email,password}` | `{user}` and session cookie |
| GET | `/auth/me` | — | `{user}` or 401 |
| POST | `/auth/logout` | `{}` | `{ok:true}`, session revoked |

User: `{id,username,email}`. Password 10–256 bytes; username 2–50 bytes; valid unique email normalized to lowercase. Registration also signs in. Session cookie `manga_session`: HttpOnly, SameSite=Lax, 30 day lifetime, Secure when `COOKIE_SECURE=true`. Only SHA256 token digests persist. Passwords use PBKDF2-HMAC-SHA256, 600,000 iterations, independent 128-bit random salts. Register/login attempts are limited to 15/minute per client IP. Behind the bundled Nginx (`TRUST_PROXY=true`) the client IP comes from `X-Real-IP`; otherwise the connecting address is used and forwarded headers are ignored.

Mutating requests validate allowed Origin whenever supplied. Cross-site browser requests without Origin are denied. JSON-only writes prevent simple form submission. Allowed origins come from `ALLOWED_ORIGINS` (Compose sets the UI address and the Angular dev server); explicitly configure production origins. Anonymous callers get 401 for private endpoints. Inaccessible lists return 404.

## Catalog

Live metadata from the official [MyAnimeList API v2](https://myanimelist.net/apiconfig/references/api/v2), authenticated with the `MAL_CLIENT_ID` client ID (no user login). IDs are MyAnimeList manga IDs. Without a client ID the catalog endpoints return 503. Search and popular titles exclude NSFW works; an empty search returns popular titles; a search shorter than 3 characters returns 400 `search_too_short` without calling MAL; any other search MAL rejects returns 400 `invalid_search`. Responses are cached in process for 30 minutes (500 entries, evicting the entry closest to expiry). Cached responses are served immediately; uncached upstream requests are spaced to stay under 60/minute, and a request that would wait more than 10 seconds in that queue fails fast with 503. Public catalog endpoints and public lists are limited to 60 requests/minute per client IP (429 beyond that). Upstream outages/rate limits become 503 with an actionable message; missing manga becomes 404. Known manga detail pages fall back to the persisted metadata with a `catalogWarning` when the provider is unavailable.

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

`chapters` is nullable metadata, never a verified list of individual chapters. MyAnimeList supplies cover images and metadata, not manga reading pages or chapter image catalogs. UI users enter chapter numbers manually, including decimals such as 12.5; textual special chapter labels are not supported in this MVP.

| Method | Path | Response |
|---|---|---|
| GET | `/health` | `{status:"ok",provider:"myanimelist",storage:"postgres"}`, 503 if the database is unreachable |
| GET | `/manga?q=...&page=1` | `{items:Manga[],page,hasNextPage}` |
| GET | `/manga/popular?page=1` | same page shape |
| GET | `/manga/:id` | `{manga:Manga,catalogWarning:string}` |

Page size 24. Search includes `sfw=true`; query max 200 bytes. Catalog endpoints are public. Detail pages persist the fetched metadata, so known manga stay available during an upstream outage.

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
| PUT | `/library/:id` | `{toRead?,favorite?}` | updated `Library`; omitted flags are unchanged |
| PUT | `/library/:id/chapters/:chapter` | `{read,rating?,notes?}` | updated `Library` |
| DELETE | `/library/:id/chapters/:chapter` | — | updated `Library`; deleting a missing chapter is not an error |
| GET | `/library?kind=favorites` | — | `{items:Manga[]}` |
| GET | `/library?kind=to-read` | — | `{items:Manga[]}` |
| GET | `/dashboard` | — | `{recent,popular,catalogWarning}` |

Chapter input must be numeric >0 and <=100000, with optional decimal. Progress is isolated by authenticated user and manga. Chapter PUT replaces feedback for that chapter: omitted rating becomes null and omitted notes becomes empty. Setting read=false clears readAt. Unread chapter feedback may still be retained. Re-marking read does not alter prior readAt unless the chapter was made unread.

Clients never send manga metadata (unknown fields are rejected with 400). Metadata comes exclusively from MyAnimeList or the copy saved in the database. If the manga is not saved yet, the server fetches it before the mutation; an unavailable provider returns 503 without changing the library or list.

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
| PUT | `/lists/:id/items/:mangaId` | `{}` | `{list}`; adding an existing item is a no-op |
| DELETE | `/lists/:id/items/:mangaId` | — | `{list}` |
| GET | `/public/lists/:id` | — | `{list,items:Manga[]}` if public, else 404 |

Name 1–100 bytes, description max 1000 bytes. Membership is unique. Public reads do not require login and disclose no email, owner ID, chapter progress or feedback. Setting isPublic=false immediately revokes public access. Public access never grants write access.

## Storage and execution

The backend requires `DATABASE_URL` (PostgreSQL); Docker Compose sets it. Other settings: `HOST`, `PORT`, `ALLOWED_ORIGINS`, `COOKIE_SECURE=true` for HTTPS, `TRUST_PROXY=true` when the API is reachable only through the reverse proxy. SIGINT/SIGTERM stop new connections and allow active requests up to 15 seconds.

The repository (`internal/repository`) uses `pgx` with one targeted query or short transaction per operation: request cost depends on the rows involved, not on the total amount of data. Concurrent writers from several backend processes are coordinated by PostgreSQL (row locks for list ordering, upserts for chapters and flags). Expired sessions are deleted at startup and hourly.

Startup applies, under an advisory lock, every `migrations/NNN_name.sql` not yet listed in `schema_migrations`. Foreign keys, unique email/membership and checks for rating 1–5, read timestamps, chapter range and note length enforce the API invariants in the database too. Errors never include the connection URL.

Tests run against real PostgreSQL; `internal/testdb` gives each test its own schema. See the README for commands.
