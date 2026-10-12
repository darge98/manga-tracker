# Manga Track

Applicazione Angular + Go per tenere il diario delle proprie letture manga: dashboard, ricerca catalogo, progresso, feedback per capitolo, preferiti e liste pubbliche o private. Ogni account mantiene la propria libreria.

## Tutto l'ambiente con Docker

Con Docker Engine attivo e Docker Compose, dal root:

```bash
cp .env.example .env
docker compose up --build -d
docker compose ps
```

Aprire `http://localhost:8080`. Compose avvia frontend Angular/Nginx, API Go e **PostgreSQL 18 realmente usato dal backend**, con volume persistente. Solo la UI pubblica una porta host; database e API restano interni. L'esempio `.env` contiene una password per sviluppo locale: cambiarla prima della prima inizializzazione per altri ambienti. `APP_PORT` configura la porta UI.

`docker compose logs -f` mostra i log; `docker compose down` arresta l'ambiente e conserva il database. Vedere [guida Docker](docs/docker.md) per backup, ripristino, importazione del JSON precedente e configurazione HTTPS. Non serve installare Go o Node sull'host per questo avvio.

## Avvio locale

Servono Go 1.21+ e Node.js 24.15+ (serie 24), con npm. Angular 22.2.1 e TypeScript 6.0.3 sono fissati nel frontend. Dal root del progetto:

```bash
bash scripts/dev.sh
```

Lo script controlla il Node presente nel `PATH` prima di installare dipendenze, compilare Go o avviare servizi. Richiede Node 24.15+ della serie 24 con npm; non modifica la versione predefinita e non installa runtime o pacchetti globali. Poi installa le dipendenze mancanti, compila il backend e avvia entrambi i servizi. Aprire `http://localhost:4200`; Angular inoltra `/api` al backend su `http://localhost:8080`. `Ctrl+C` arresta i servizi.

Con nvm, dalla cartella del progetto, selezionare la versione indicata da `.nvmrc` (Node 24.21.0):

```bash
nvm install # solo se la versione non è già installata
nvm use
bash scripts/dev.sh
```

Per i comandi npm manuali, usare lo stesso runtime. Il banner indica gli indirizzi di avvio richiesti; la disponibilità effettiva è confermata dai log dei servizi. In caso di arresto, lo script identifica il servizio e riporta il codice di uscita, compreso il numero di segnale quando applicabile.

Avvio separato, utile durante lo sviluppo (per Go 1.21 su macOS usare lo script oppure la procedura compilazione/firma descritta sotto):

```bash
cd backend
go run ./cmd/server
```

In un altro terminale:

```bash
cd frontend
npm install
npm start
```

Registrare un account dall'interfaccia per iniziare. Non sono richieste credenziali MyAnimeList o AniList.

Configurazione facoltativa: copiare `.env.example` in `.env`, caricato dallo script. `HOST` vale `127.0.0.1`, `PORT` vale `8080`, `DATA_FILE` vale `data/manga-track.json` relativo alla directory backend e `ALLOWED_ORIGINS` ammette i due indirizzi locali del frontend. `COOKIE_SECURE=true` richiede HTTPS. Cambiando la porta backend va aggiornata anche `frontend/proxy.conf.json`.

L'avvio nativo senza `DATABASE_URL` conserva i dati nel file JSON `backend/data/manga-track.json`, con scrittura atomica. Questa modalità supporta una singola istanza Go; non avviare processi multipli sullo stesso file. Per salvare i dati, eseguire un backup del file dopo aver arrestato il server. Docker Compose usa invece PostgreSQL; non cambia né importa automaticamente questo archivio locale.

Per produzione, `npm --prefix frontend run build` genera gli asset Angular da servire con un hosting statico che rimanda le rotte SPA a `index.html`. Un reverse proxy dello stesso dominio deve inoltrare `/api` al processo Go; il server Go non serve gli asset Angular. Configurare HTTPS, `COOKIE_SECURE=true`, origini consentite e percorso dati persistente. Il repository fornisce l'avvio locale, non un deployment già pubblicato.

## Catalogo e dati personali

Il catalogo usa [Jikan v4](https://docs.api.jikan.moe/), API pubblica indipendente che espone metadata MyAnimeList. Tracking, autenticazione, note e liste sono gestiti dall'applicazione. Non è un lettore di scansioni né una sincronizzazione delle liste MAL.

I totali capitoli possono essere sconosciuti, soprattutto per serie in corso: il progresso resta personale e non implica un conteggio aggiornato dei capitoli pubblicati. [Ricerca e motivazioni](docs/research.md) includono fonti e idee UX da AniList; i suoi termini escludono questo uso come tracker autonomo.

Le liste pubbliche sono condivisibili; il diario per capitolo e il progresso personale non fanno parte del contenuto condiviso. I file locali contenenti account e sessioni sono esclusi dal versionamento con `.gitignore`.

## Verifica

Per verificare preflight e gestione degli arresti dello script senza avviare server reali:

```bash
bash scripts/test-dev.sh
```

```bash
cd backend
go test ./...
go vet ./...
```

```bash
cd frontend
npm test
npm run check
npm run build
```

Vedere [contratto API](docs/api.md) e [evidenze della verifica](docs/verification.md).

Su macOS recente con Go 1.21, il linker interno può produrre un binario che `dyld` rifiuta per `missing LC_UUID`. Lo script usa il linker esterno macOS (richiede gli strumenti Xcode/clang). Dopo la compilazione applica una firma ad hoc con `codesign --force --sign -` al proprio binario, la verifica e solo allora lo avvia. Questo corregge anche gli arresti `Killed: 9` dovuti a `Code Signature Invalid` osservati con Go 1.21 su macOS recente, senza modificare Gatekeeper o impostazioni di sicurezza. Per l'avvio manuale su macOS con Go 1.21, compilare e firmare il binario prima di eseguirlo, dal root:

```bash
mkdir -p .dev
(cd backend && go build -ldflags=-linkmode=external -o ../.dev/manga-track-server ./cmd/server)
codesign --force --sign - .dev/manga-track-server
codesign --verify .dev/manga-track-server
(cd backend && ../.dev/manga-track-server)
```

Per i test Go, `go test -ldflags=-linkmode=external ./...` evita l'errore `missing LC_UUID`.

Nell'ambiente di sviluppo gestito di questa sessione, l'apertura di porte locali è negata (`bind: operation not permitted`) e il processo shell non risolve `api.jikan.moe`. I test Go usano gli handler HTTP reali, un archivio temporaneo e un catalogo di test dichiarato; questo non dimostra un flusso browser completo né il provider live. Avviare lo script da un normale terminale sul proprio host per usare l'applicazione e completare la prova dal browser.
