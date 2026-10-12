// User-facing messages for the stable error codes returned by the API. The
// API's own `error` text is meant for API consumers and may be more technical.
const messages: Record<string, string> = {
  network: "Could not reach the server. Check your connection and try again.",
  unauthenticated: "Please sign in to continue.",
  invalid_credentials: "Incorrect email or password.",
  invalid_registration:
    "Enter a valid email, a username of 2–50 characters and a password of at least 10 characters.",
  password_too_long: "The password is too long.",
  wrong_password: "Your current password is incorrect.",
  invalid_new_password: "The new password must be 10–256 characters long.",
  email_taken: "An account with this email already exists.",
  rate_limited: "Too many requests. Please try again in a minute.",
  search_too_long: "The search is too long.",
  search_too_short: "Type at least 3 characters to search.",
  invalid_search:
    "The catalog did not accept this search. Try at least 3 characters.",
  invalid_manga_id: "Invalid manga.",
  manga_not_found: "Manga not found.",
  invalid_chapter:
    "The chapter must be a positive number; decimals are allowed.",
  invalid_chapter_feedback:
    "Say whether the chapter is read, with an optional 1–5 rating and notes up to 5000 characters.",
  invalid_kind: "Invalid collection.",
  list_not_found: "List not found.",
  invalid_list:
    "A list name is required (up to 100 characters); the description can be up to 1000.",
  invalid_list_name: "The list name must be 1–100 characters long.",
  description_too_long: "The description is too long.",
  catalog_unavailable:
    "The catalog is unavailable right now. Please try again shortly.",
  database_unavailable:
    "The service is unavailable right now. Please try again shortly.",
  not_found: "Not found.",
};

const byStatus: Record<number, string> = {
  401: messages["unauthenticated"],
  404: messages["not_found"],
  429: messages["rate_limited"],
  503: messages["database_unavailable"],
};

const fallback = "The request could not be completed. Please try again.";

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
