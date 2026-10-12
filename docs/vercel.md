# Deploy su Vercel + Neon

In produzione l'app gira su Vercel come un unico progetto con due *Services* definiti in `vercel.json`:

- `web`: il frontend Angular, servito come file statici (fallback su `index.html` per le rotte client);
- `api`: il backend Go (`backend/cmd/server`), eseguito come Vercel Function sul runtime Go.

Entrambi stanno sullo stesso dominio: `/api/*` va al backend con il percorso invariato, tutto il resto al frontend. Cookie di sessione, controllo dell'Origin e CSP funzionano senza configurazione aggiuntiva, su qualsiasi dominio (produzione, anteprime, dominio personalizzato). Il database è Neon, collegato dal Marketplace di Vercel.

Docker Compose resta l'ambiente di sviluppo locale e di CI.

## Primo deploy

1. **Repository**: pubblica il progetto su un repository GitHub *personale*. Sul piano Hobby Vercel non si collega a repository di organizzazioni GitHub.
2. **Progetto Vercel**: *Add New → Project*, importa il repository e lascia la *Root Directory* sulla radice, dove si trova `vercel.json`. Non scegliere un framework preset: i servizi sono definiti in `vercel.json`.
3. **Database**: nella scheda *Storage* (o dal Marketplace) crea un database **Neon** e collegalo al progetto per Production, Preview e Development. L'integrazione imposta `DATABASE_URL`, che passa dal pooler di Neon ed è quella usata dal backend. Le tabelle vengono create al primo avvio dalle migrazioni.
4. **Variabili d'ambiente** (*Settings → Environment Variables*):
   - `MAL_CLIENT_ID`: il Client ID dell'API di MyAnimeList (Production e Preview);
   - lascia attiva l'opzione *Enable access to System Environment Variables* (attiva di default). Dalla variabile `VERCEL` il backend capisce di essere su Vercel e attiva cookie `Secure`, lettura dell'IP reale e ascolto su `0.0.0.0`. In alternativa imposta esplicitamente `COOKIE_SECURE=true`, `TRUST_PROXY=true` e `HOST=0.0.0.0`.
5. **Deploy**: ogni push su `main` va in produzione, ogni altro branch o pull request crea un'anteprima. Dopo il primo deploy verifica `https://<dominio>/api/health`: deve rispondere `{"provider":"myanimelist","status":"ok","storage":"postgres"}`.
6. **Dominio personalizzato** (facoltativo): *Settings → Domains*. HTTPS e HSTS sono gestiti da Vercel; non serve aggiornare `ALLOWED_ORIGINS`.

## Attenzione alle anteprime

Se non configuri diversamente l'integrazione Neon, le deployment di anteprima usano **lo stesso database della produzione**. Prima di testare modifiche allo schema o dati di prova in anteprima, attiva nell'integrazione la creazione di un branch Neon per ogni anteprima, oppure assegna all'ambiente Preview un database separato.

## Limiti del piano Hobby (ottobre 2026)

- Uso solo **non commerciale**: niente pubblicità né pagamenti; le donazioni sono ammesse. La licenza dell'API MyAnimeList pone un vincolo analogo.
- 1.000.000 di invocazioni e 4 ore di CPU attiva al mese; 100 GB di traffico.
- Log di runtime conservati per 1 ora.
- Neon free: 0,5 GB di spazio e un monte ore di calcolo mensile; il database si sospende dopo alcuni minuti di inattività e si riattiva alla richiesta successiva.

Se i limiti vengono superati Vercel può mettere in pausa il progetto: tieni d'occhio *Usage* nella dashboard.

## Differenze rispetto a Docker Compose

| | Docker Compose | Vercel |
|---|---|---|
| Header di sicurezza | `frontend/security-headers.conf` (Nginx) | `headers` al livello principale di `vercel.json` |
| IP del client | `X-Real-IP` impostato da Nginx | `X-Real-IP` sovrascritto da Vercel |
| Processo del backend | sempre attivo | istanze avviate su richiesta |

I due insiemi di header devono restare identici: la CI lo controlla con `node scripts/check-security-headers.mjs`.

Rate limit, cache e coda verso MyAnimeList vivono in memoria e quindi valgono per singola istanza del backend. Con traffico basso non è un problema; se il traffico cresce conviene spostarli nel database. Le sessioni scadute vengono eliminate a ogni login, perché su Vercel non c'è un processo sempre attivo che possa farlo periodicamente.

## Provare la configurazione in locale

Con la CLI di Vercel, senza account:

```bash
DATABASE_URL='postgres://…' MAL_CLIENT_ID='…' npx vercel dev -L
```

`vercel dev` usa il dev server di Angular per il frontend e non applica gli `headers` del livello principale (in produzione vengono applicati a entrambi i servizi); la build di produzione si genera con `npx vercel build` (richiede un progetto collegato, `vercel link`).

## Cambiare database in futuro

Il backend usa solo PostgreSQL standard tramite `DATABASE_URL`. Per passare a un altro provider: esporta i dati con `pg_dump`, importali nel nuovo database, poi sostituisci `DATABASE_URL` su Vercel (scollegando l'integrazione Neon). Il pooler deve supportare i prepared statement a livello di protocollo (PgBouncer ≥ 1.22) oppure va usata una connessione diretta. Si può aggiungere `pool_max_conns=N` all'URL per limitare le connessioni per istanza.
