# Ricerca prodotto e catalogo

Verifica fonti: 11 ottobre 2026. Il workspace iniziale era vuoto: nessun provider o vincolo di repository preesistente.

## Provider scelto: Jikan v4

[Jikan](https://docs.api.jikan.moe/) espone metadata MyAnimeList tramite REST pubblico senza credenziali. Lo [schema ufficiale del progetto](https://raw.githubusercontent.com/jikan-me/jikan-rest/master/storage/api-docs/api-docs.json) documenta:

- ricerca: `GET /manga?q=...&page=1&limit=24`;
- dettagli: `GET /manga/{id}/full`;
- popolari: `GET /top/manga?filter=bypopularity`;
- ID stabile `mal_id`, copertine, titoli, sinossi, generi, stato, score e capitoli;
- limite 3 richieste/secondo e 60/minuto, possibili ulteriori limiti upstream;
- cache upstream 24 ore; valori sconosciuti `null`;
- API di sola lettura: niente autenticazione o aggiornamento delle liste MAL.

Di conseguenza Go gestisce proxy/cache e dati personali locali. L'interfaccia avvia la ricerca su invio esplicito; cache, richieste deduplicate e gestione degli errori 429 limitano il carico. Il catalogo non offre una lista autorevole dei singoli capitoli manga: il diario personale usa numeri inseriti dall'utente. Per serie in corso o totali sconosciuti, mostrare “totale sconosciuto” e consentire progressi positivi senza inventare una percentuale o un tetto. Metadata e copertine non sono i capitoli da leggere.

Jikan è indipendente da MAL e non offre garanzia di disponibilità. Il link [termini Jikan](https://jikan.moe/terms) riportato dallo schema restituisce “Page not found” nel browser ufficiale in questa verifica: non è una conferma di licenza sui contenuti.

## Riferimenti UX

AniList modella [stato, progressi, voto, riletture, priorità, note, privacy e appartenenza a liste custom](https://docs.anilist.co/reference/object/medialist). Idee adottabili: “Continua a leggere” ordinato per ultima lettura; incremento rapido del capitolo con annullamento; filtri per stato; preferiti indipendenti dal progresso; liste tematiche condivisibili. L'estensione distintiva qui è un diario privato per capitolo con voto e commento.

La visibilità appartiene alla lista: condividere una lista pubblica deve esporre solo nome, descrizione e manga, mai progresso o diario del proprietario. Dati del dashboard e ultimi letti restano personali. Il cambio pubblico/privato deve valere immediatamente sul link condiviso.

## Perché non AniList come catalogo autonomo

I [termini attuali AniList](https://docs.anilist.co/guide/terms-of-use) vietano l'uso in servizi concorrenti di tracking anime/manga, anche per soli metadata. Non è una base appropriata per questo tracker autonomo. Il [limite documentato](https://docs.anilist.co/guide/rate-limiting) è inoltre temporaneamente 30/minuto anziché 90/minuto. Lo [schema Media](https://docs.anilist.co/reference/object/media) definisce `chapters` come totale alla conclusione, non conteggio affidabile delle uscite correnti.

L'[API ufficiale MAL](https://myanimelist.net/apiconfig/references/api/v2) rimane un possibile futuro provider con registrazione applicazione; la pagina non era recuperabile in questa sessione, quindi non sono stati assunti dettagli di auth o limiti non verificati.

## Verifica richiesta

- Due account distinti: progresso, preferiti, note e liste private isolati.
- Liste pubbliche leggibili senza sessione; private negate anche conoscendo l'ID.
- Diario capitolo salvato e recuperabile dopo ricaricamento.
- Ricerca e popolari reali, con errore esplicito se provider indisponibile.
- Totali sconosciuti e pubblicazione in corso gestiti senza capitoli fittizi.
- Persistenza dopo riavvio del backend; logout invalida la sessione.
