// Italian messages for the stable error codes returned by the API.
// The API's own `error` text is English and meant for API consumers.
const messages: Record<string, string> = {
  network: "Connessione al server non riuscita. Controlla la rete e riprova.",
  unauthenticated: "Accedi per continuare.",
  invalid_credentials: "Email o password non corrette.",
  invalid_registration:
    "Inserisci un'email valida, un nome utente di 2–50 caratteri e una password di almeno 10 caratteri.",
  password_too_long: "La password è troppo lunga.",
  email_taken: "Esiste già un account con questa email.",
  rate_limited: "Troppe richieste. Riprova tra un minuto.",
  search_too_long: "La ricerca è troppo lunga.",
  invalid_manga_id: "Manga non valido.",
  manga_not_found: "Manga non trovato.",
  invalid_chapter:
    "Il capitolo deve essere un numero positivo, anche con decimali.",
  invalid_chapter_feedback:
    "Indica se il capitolo è letto, un voto da 1 a 5 e note fino a 5000 caratteri.",
  invalid_kind: "Raccolta non valida.",
  list_not_found: "Lista non trovata.",
  invalid_list:
    "Il nome della lista è obbligatorio (massimo 100 caratteri), la descrizione al massimo 1000.",
  invalid_list_name: "Il nome della lista deve avere da 1 a 100 caratteri.",
  description_too_long: "La descrizione è troppo lunga.",
  catalog_unavailable:
    "Il catalogo non è raggiungibile in questo momento. Riprova tra poco.",
  database_unavailable:
    "Il servizio non è disponibile in questo momento. Riprova tra poco.",
  not_found: "Elemento non trovato.",
};

const byStatus: Record<number, string> = {
  401: messages["unauthenticated"],
  404: messages["not_found"],
  429: messages["rate_limited"],
  503: messages["database_unavailable"],
};

const fallback = "Non è stato possibile completare la richiesta. Riprova.";

export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
  ) {
    super(messages[code] ?? byStatus[status] ?? fallback);
    this.name = "ApiError";
  }
}

export function readableError(e: unknown) {
  return e instanceof Error && e.message ? e.message : fallback;
}
