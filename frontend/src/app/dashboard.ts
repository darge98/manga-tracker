import { Component, inject, signal, effect } from "@angular/core";
import { RouterLink } from "@angular/router";
import { Api, Manga, readableError } from "./core";
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
  recent = signal<any[]>([]);
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
    this.loading.set(true);
    this.error.set("");
    try {
      if (owner) {
        const d = await this.api.request<{
          popular: Manga[];
          recent: any[];
          catalogWarning: string;
        }>("/dashboard");
        if (version !== this.loadVersion || owner !== this.api.user()?.id)
          return;
        this.popular.set(d.popular);
        this.recent.set(d.recent);
        if (d.catalogWarning)
          this.error.set(
            "Catalogo momentaneamente non raggiungibile: la tua cronologia di lettura è comunque disponibile.",
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
}
