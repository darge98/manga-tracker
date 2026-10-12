import { Injectable, WritableSignal, signal } from "@angular/core";
import { ApiError, readableError } from "./errors";
export { ApiError, readableError } from "./errors";
export interface Manga {
  id: number;
  title: string;
  description: string;
  coverUrl: string;
  chapters: number | null;
  status: string;
  genres: string[];
  score: number | null;
  author: string;
  url: string;
}
export interface User {
  id: string;
  username: string;
  email: string;
}
export interface Chapter {
  chapter: string;
  read: boolean;
  readAt: string | null;
  rating: number | null;
  notes: string;
}
export interface Library {
  mangaId: number;
  chapters: Chapter[];
  toRead: boolean;
  favorite: boolean;
}
export interface RecentRead {
  manga: Manga;
  chapter: string;
  readAt: string;
  rating: number | null;
  notes: string;
}
export interface Recommendation {
  manga: Manga;
  /** The liked manga that contributed most to this suggestion. */
  because: { id: number; title: string };
}
export interface MangaList {
  id: string;
  name: string;
  description: string;
  isPublic: boolean;
  itemCount: number;
  createdAt: string;
}
export interface Page {
  items: Manga[];
  page: number;
  hasNextPage: boolean;
}
@Injectable({ providedIn: "root" })
export class Api {
  user = signal<User | null>(null);
  ready = signal(false);
  private identityVersion = 0;
  setUser(user: User | null) {
    this.identityVersion++;
    this.user.set(user);
  }
  private sessionPending: Promise<void> | null = null;
  async request<T>(path: string, method = "GET", body?: unknown): Promise<T> {
    const identityVersion = this.identityVersion;
    let response: Response;
    try {
      response = await fetch("/api" + path, {
        method,
        credentials: "include",
        headers: body ? { "Content-Type": "application/json" } : {},
        body: body === undefined ? undefined : JSON.stringify(body),
      });
    } catch {
      throw new ApiError(0, "network");
    }
    let data: unknown;
    try {
      data = await response.json();
    } catch {
      data = {};
    }
    if (!response.ok) {
      if (response.status === 401 && identityVersion === this.identityVersion)
        this.setUser(null);
      throw new ApiError(
        response.status,
        (data as { code?: string } | null)?.code ?? "",
      );
    }
    return data as T;
  }
  session(): Promise<void> {
    if (this.sessionPending) return this.sessionPending;
    const identityVersion = this.identityVersion;
    this.sessionPending = (async () => {
      try {
        const data = await this.request<{ user: User }>("/auth/me");
        if (identityVersion === this.identityVersion) this.setUser(data.user);
      } catch {
        if (identityVersion === this.identityVersion) this.setUser(null);
      } finally {
        this.ready.set(true);
        this.sessionPending = null;
      }
    })();
    return this.sessionPending;
  }
  async logout() {
    await this.request("/auth/logout", "POST");
    this.setUser(null);
  }
}
/** State shared by pages that run user actions while route changes may
 * replace the content being edited. */
export interface MutationHost {
  api: Api;
  busy: WritableSignal<boolean>;
  error: WritableSignal<string>;
  loadVersion: number;
}

/** Runs one user action at a time. `current()` turns false when the page
 * loads other content or the account changes, so stale results are dropped. */
export async function mutate(
  host: MutationHost,
  action: (current: () => boolean) => Promise<void>,
  onStart?: () => void,
) {
  if (host.busy()) return;
  const version = host.loadVersion;
  const owner = host.api.user()?.id;
  const current = () =>
    version === host.loadVersion && owner === host.api.user()?.id;
  host.busy.set(true);
  host.error.set("");
  onStart?.();
  try {
    await action(current);
  } catch (e) {
    if (current()) host.error.set(readableError(e));
  } finally {
    if (version === host.loadVersion) host.busy.set(false);
  }
}
