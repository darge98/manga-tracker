import { Component, inject, signal } from "@angular/core";
import { FormsModule } from "@angular/forms";
import { ActivatedRoute, RouterLink, Router } from "@angular/router";
import { Api, MangaList, Manga, mutate, readableError } from "./core";
import { MangaCard } from "./card";
@Component({
  imports: [FormsModule, RouterLink, MangaCard],
  templateUrl: "./lists.html",
})
export class Lists {
  api = inject(Api);
  route = inject(ActivatedRoute);
  router = inject(Router);
  id = "";
  publicView = false;
  list = signal<MangaList | null>(null);
  lists = signal<MangaList[]>([]);
  items = signal<Manga[]>([]);
  loading = signal(true);
  busy = signal(false);
  error = signal("");
  message = signal("");
  name = "";
  description = "";
  isPublic = false;
  editing = false;
  loadVersion = 0;
  constructor() {
    this.route.paramMap.subscribe((p) => {
      this.id = p.get("id") || "";
      this.publicView = this.route.snapshot.data["public"] === true;
      this.editing = false;
      void this.load();
    });
  }
  async load() {
    const version = ++this.loadVersion;
    this.busy.set(false);
    this.list.set(null);
    this.items.set([]);
    this.message.set("");
    this.loading.set(true);
    this.error.set("");
    if (!this.publicView && !this.api.user()) {
      this.loading.set(false);
      return;
    }
    try {
      if (this.id) {
        const d = await this.api.request<{ list: MangaList; items: Manga[] }>(
          (this.publicView ? "/public" : "") + "/lists/" + this.id,
        );
        if (version !== this.loadVersion) return;
        this.list.set(d.list);
        this.items.set(d.items);
      } else {
        const d = await this.api.request<{ items: MangaList[] }>("/lists");
        if (version !== this.loadVersion) return;
        this.lists.set(d.items);
      }
    } catch (e) {
      if (version === this.loadVersion) this.error.set(readableError(e));
    } finally {
      if (version === this.loadVersion) this.loading.set(false);
    }
  }
  startEdit() {
    const l = this.list();
    if (!l) return;
    this.name = l.name;
    this.description = l.description;
    this.isPublic = l.isPublic;
    this.editing = true;
  }
  async save() {
    const id = this.id;
    const version = this.loadVersion;
    const body = {
      name: this.name.trim(),
      description: this.description,
      isPublic: this.isPublic,
    };
    await this.mutate(async (current) => {
      await this.api.request(
        "/lists" + (id ? "/" + id : ""),
        id ? "PATCH" : "POST",
        body,
      );
      if (!current()) return;
      this.editing = false;
      this.name = "";
      this.description = "";
      this.isPublic = false;
      await this.load();
      if (this.loadVersion === version + 1 && this.id === id)
        this.message.set(
          id ? "Lista aggiornata." : "La tua nuova lista è pronta.",
        );
    });
  }
  async remove(m: Manga) {
    const id = this.id;
    await this.mutate(async (current) => {
      await this.api.request("/lists/" + id + "/items/" + m.id, "DELETE");
      if (!current()) return;
      this.items.update((items) => items.filter((x) => x.id !== m.id));
      this.message.set("Manga rimosso dalla lista.");
    });
  }
  async deleteList() {
    if (
      this.busy() ||
      !confirm(
        "Eliminare questa lista? I tuoi progressi di lettura resteranno salvati.",
      )
    )
      return;
    const id = this.id;
    await this.mutate(async (current) => {
      await this.api.request("/lists/" + id, "DELETE");
      if (current()) void this.router.navigateByUrl("/lists");
    });
  }
  mutate(action: (current: () => boolean) => Promise<void>) {
    return mutate(this, action);
  }

  async copyLink() {
    const link = window.location.origin + "/public/lists/" + this.id;
    try {
      await navigator.clipboard.writeText(link);
      this.message.set("Link pubblico copiato.");
    } catch {
      this.message.set("Link pubblico: " + link);
    }
  }
}
