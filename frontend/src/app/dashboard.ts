import { Component, inject, signal, effect } from "@angular/core";
import { RouterLink } from "@angular/router";
import { Api, Manga, RecentRead, Recommendation, readableError } from "./core";
import { MangaCard } from "./card";
@Component({
  imports: [RouterLink, MangaCard],
  templateUrl: "./dashboard.html",
})
export class Dashboard {
  api = inject(Api);
  loading = signal(true);
  error = signal("");
  popular = signal<Manga[]>([]);
  recent = signal<RecentRead[]>([]);
  recommendations = signal<Recommendation[]>([]);
  recommendationsState = signal<"idle" | "loading" | "ready" | "error">("idle");
  /** How many liked manga the recommendations are based on. */
  basedOn = signal(0);
  loadVersion = 0;
  constructor() {
    effect(() => {
      this.api.user();
      void this.load();
    });
  }
  async load() {
    const version = ++this.loadVersion;
    const owner = this.api.user()?.id;
    this.recent.set([]);
    this.recommendations.set([]);
    this.recommendationsState.set("idle");
    this.loading.set(true);
    this.error.set("");
    try {
      if (owner) {
        const d = await this.api.request<{
          popular: Manga[];
          recent: RecentRead[];
          catalogWarning: string;
        }>("/dashboard");
        if (version !== this.loadVersion || owner !== this.api.user()?.id)
          return;
        this.popular.set(d.popular);
        this.recent.set(d.recent);
        // Loaded separately: it may wait on several catalog calls.
        void this.loadRecommendations(version, owner);
        if (d.catalogWarning)
          this.error.set(
            "The catalog is temporarily unavailable. Your reading history is still here.",
          );
      } else {
        const d = await this.api.request<{ items: Manga[] }>("/manga/popular");
        if (version !== this.loadVersion) return;
        this.popular.set(d.items);
      }
    } catch (e) {
      if (version === this.loadVersion) this.error.set(readableError(e));
    } finally {
      if (version === this.loadVersion) this.loading.set(false);
    }
  }
  async loadRecommendations(version: number, owner: string) {
    this.recommendationsState.set("loading");
    try {
      const d = await this.api.request<{
        items: Recommendation[];
        basedOn: number;
        catalogWarning: string;
      }>("/recommendations");
      if (version !== this.loadVersion || owner !== this.api.user()?.id) return;
      this.recommendations.set(d.items);
      this.basedOn.set(d.basedOn);
      this.recommendationsState.set(
        d.catalogWarning && !d.items.length ? "error" : "ready",
      );
    } catch {
      if (version === this.loadVersion) this.recommendationsState.set("error");
    }
  }
}
