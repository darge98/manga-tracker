# Verifica funzionale

Questa tabella raccoglie evidenze osservate; “pending” non indica un test superato.

| Requisito | Evidenza | Stato |
| --- | --- | --- |
| Angular frontend e Go backend avviabili | Script `bash -n scripts/dev.sh` passa; Go compila con linker esterno, listener locale negato | Parziale, avvio live bloccato |
| Registrazione, login e logout | `server_test.go` e `independent_audit_test.go`: due account, credenziali errate, duplicato email normalizzata, logout invalida cookie B e mantiene sessione A | PASS handler HTTP |
| Dati multiutente isolati | Capitolo 1234.5 con note/voti diversi per A/B; dashboard A senza note B e viceversa; liste e base library separate | PASS handler HTTP |
| Dashboard ultimi letti | `library.go` filtra capitoli letti, ordina `readAt` discendente e mostra gli ultimi 12 manga distinti; test 3 eventi/2 manga seleziona ultimo capitolo per manga senza alterare il diario completo | PASS handler/test, browser pending |
| Manga popolari da API pubblica | `provider/jikan.go` usa `/top/manga?filter=bypopularity`; fake catalog solo test; DNS upstream negato | Schema verificato, live non provato |
| Ricerca paginata e dettagli | Mapping Jikan e fonti schema: query Unicode con caratteri riservati, `has_next_page`, immagini, ID, generi, autore, null capitoli, cache deduplicata; body HTTP 200 incompleto/ID errato rifiutato prima cache, retry valido accettato; test frontend retry pagina 2 conserva pagina 1 | PASS fixture + fonti schema; live pending |
| Capitoli letti/non letti | Capitolo decimal1234.5 letto poi non letto; `readAt` rimosso, note/voto aggiornati | PASS handler HTTP |
| Feedback personale per capitolo | Voti5 e2 su stesso capitolo A/B, note distinte; diario B riaperto da disco; JSON pubblico senza campi privati | PASS handler HTTP + disco |
| Da leggere e preferiti | Toggle true/false, recupero e rimozione; libreria B senza manga A | PASS handler HTTP |
| Liste custom CRUD e add/remove | Creazione, modifica nome, aggiunta idempotente, rimozione, eliminazione; scritture B sulla lista A negate | PASS handler HTTP |
| Liste pubbliche/private e share anon | Endpoint `/api/public/lists/:id` anonimo200 per pubblica,404 privata; nessun email/ownerId/note/voto/readAt nel JSON | PASS handler HTTP |
| Revoca visibilità | PATCH pubblico->privato, URL anonimo condiviso404, account B404 anche dopo riapertura store | PASS handler HTTP |
| Totale capitoli sconosciuto | Fixture `chapters:null` consente1234.5; UI mostra totale sconosciuto e campo positivo decimal con max100000 indipendente dai metadata; verificato fix in `detail.ts` | PASS backend + revisione UI; browser pending |
| Persistenza | Store chiuso e riaperto dallo stesso file temporaneo: capitoli/feedback/privacy/sessioni conservati | PASS riapertura archivio; processo restart pending |
| Browser desktop/mobile | Nessun listener consentito nella sandbox; prova browser app non eseguita | Non verificato |
| Build/check tecnici | Suite Go iniziale e vet PASS; secondo turno: 11 funzioni Test totali, 3 nuove prove mirate race PASS (`TestJikan`, `TestDashboardLatestDistinct`); frontend finale `npm run check`, `npm test` (11/11), `npm run build` PASS, initial 263.98 kB / transfer 71.61 kB | PASS Go + Angular |

## Confini dell'evidenza

I test API passano gli handler reali tramite `httptest.NewRecorder` con filesystem temporaneo e catalogo fixture dichiarato nei file test. Non aprono porte e non sostituiscono il provider di produzione. Riaprire lo store verifica serializzazione/persistenza; non è una prova del riavvio di un processo via HTTP reale.

Tentativi live: Go1.21 linker interno termina con `dyld: missing LC_UUID`; il linker esterno risolve il problema. Il server con `HOST=127.0.0.1 PORT=8081` termina per `bind: operation not permitted`. `curl https://api.jikan.moe/v4/manga/2` termina per DNS; tool web non recupera `/manga/2/full`, browser navigazione scade. Le fonti schema/termini citate in `research.md` sono una verifica documentale, non una risposta live del catalogo.

Nel secondo turno di prosecuzione, una nuova prova attuale del server su `127.0.0.1:8081` conferma il listener negato; una richiesta `curl` all'endpoint Jikan conferma ancora il DNS non disponibile. Il turno precedente aveva prodotto implementazione e test significativi: i turni consecutivi documentati con lo stesso blocco sono due, senza dedurre altri turni dal consumo di token. Nessuna escalation o apertura di porte tramite strumenti alternativi è stata tentata.

## Architettura

Backend separato in `api` (contratto HTTP, validazione e ownership), `auth` (password/sessioni), `provider` (Jikan, mapping, cache e limiti), `repository` (JSON nativo o PostgreSQL selezionato da `DATABASE_URL`). Frontend Angular con pagine dashboard/catalogo/dettaglio/liste/auth e servizio `Api` condiviso con segnali di sessione. La modalità JSON limita la distribuzione a un processo; Compose usa PostgreSQL con transazioni, vincoli e lock tra processi. Le pagine richiedono verifica browser dopo avvio sull'host.

## Docker Compose e PostgreSQL

| Requisito | Evidenza | Stato |
| --- | --- | --- |
| Stack frontend/API/database | `compose.yaml`, Dockerfile multi-stage Go con `-tags postgres`, Angular build/Nginx, PostgreSQL 18 | Configurato, immagini non costruite |
| Database realmente selezionato | Compose imposta `DATABASE_URL`; driver `lib/pq` compilato col tag, nessun fallback JSON con URL presente; health verifica Ping | Revisione source PASS; runtime pending |
| Volume e isolamento porte | Config JSON validata: volume `/var/lib/postgresql`, solo UI pubblicata su loopback, rete database interna | PASS configurazione |
| Origini e porta configurabile | Assert `APP_PORT=8090` coerente con `ALLOWED_ORIGINS`; proxy Nginx preserva URI e Host | PASS configurazione/revisione |
| Avvio ordinato | `db` healthy prima backend; backend healthy prima frontend | PASS configurazione; startup pending |
| Segreti e contesti build | `.env` assente, setup documentato da `.env.example`; `.env` ignorato e `.dockerignore` esclude dati e config; nessun valore privato stampato | PASS configurazione |
| Migrazione legacy e backup | Comando `import-json` read-only, target vuoto e transazione; guida dump/restore senza eliminazioni automatiche | Source/docs; prova PG pending |
| Integrazione PostgreSQL | Profilo `test` usa testdb separato su tmpfs e `TEST_DATABASE_URL`; package serializzati `-p 1` | Config PASS; test PG non eseguiti |
| Config Compose | `docker compose --env-file .env.example config --quiet` e `docker compose --env-file .env.example --profile test config --quiet`; anche configurazione temporanea con password casuale validata senza output sensibile | PASS |
| Build/up/container runtime | Docker CLI disponibile; dopo avvio OrbStack socket presente ma `docker info` permission denied sandbox | Bloccato, non verificato |

Il nuovo storage PostgreSQL non è stato provato contro un server reale in questa sessione. I test specifici devono essere eseguiti con `docker compose --profile test run --build --rm backend-test` da un host con accesso al daemon. Le prove JSON precedenti restano valide per la modalità nativa e non dimostrano persistenza PostgreSQL.

Revisione frontend: risolti dati residui su cambio ID per dettaglio/liste con reset e versionamento richieste; numero capitolo non vincolato a totale provider e ammette speciali decimali. Logout dashboard risolto con effect su identità, reset recent, controllo identità/versione della risposta e rendering condizionale all'utente. Session bootstrap deduplicato con promise in-flight e versione identità: una risposta startup 401 dopo login non annulla il nuovo utente, una risposta 200 dopo logout non ripristina l'account precedente. Undici test importano classi frontend reali con mock limitati a fetch/router. Oltre ai sei iniziali, coprono queste due race di sessione, salvataggio capitolo mentre cambia manga, rimozione item mentre cambia lista e retry paginato che conserva risultati precedenti. I primi quattro sono stati eseguiti anche dal revisore indipendente; il proprietario frontend conferma undici test PASS. Non sono test DOM/browser.
