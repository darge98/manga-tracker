import { Injectable, signal } from "@angular/core";
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
    const response = await fetch("/api" + path, {
      method,
      credentials: "include",
      headers: body ? { "Content-Type": "application/json" } : {},
      body: body === undefined ? undefined : JSON.stringify(body),
    });
    let data: any;
    try {
      data = await response.json();
    } catch {
      data = {};
    }
    if (!response.ok) {
      if (response.status === 401 && identityVersion === this.identityVersion)
        this.setUser(null);
      throw new Error(
        data.error?.message ||
          data.error ||
          data.message ||
          `Richiesta non riuscita (${response.status}).`,
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
export function readableError(e: unknown) {
  return e instanceof Error
    ? e.message
    : "Non è stato possibile completare la richiesta. Riprova.";
}
