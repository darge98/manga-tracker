import { Component, inject, signal } from "@angular/core";
import { FormsModule } from "@angular/forms";
import { ActivatedRoute, RouterLink } from "@angular/router";
import { Api, Manga, Page, readableError } from "./core";
import { MangaCard } from "./card";
@Component({
  imports: [FormsModule, MangaCard, RouterLink],
  templateUrl: "./explore.html",
})
export class Explore {
  api = inject(Api);
  route = inject(ActivatedRoute);
  collection = false;
  kind = "";
  query = "";
  lastQuery = "";
  page = 1;
  items = signal<Manga[]>([]);
  hasNext = signal(false);
  loading = signal(false);
  error = signal("");
  loadVersion = 0;
  retryAppend = false;
  constructor() {
    this.route.paramMap.subscribe((p) => {
      this.kind = p.get("kind") || "";
      this.collection = !!this.kind;
      this.page = 1;
      this.items.set([]);
      void this.load();
    });
  }
  /** MyAnimeList rejects searches shorter than this; an empty search shows popular titles. */
  readonly minSearchLength = 3;
  tooShort() {
    const length = [...this.query.trim()].length;
    return length > 0 && length < this.minSearchLength;
  }
  search() {
    if (this.tooShort()) return;
    this.lastQuery = this.query.trim();
    this.page = 1;
    this.items.set([]);
    void this.load();
  }
  retry() {
    if (this.retryAppend) this.more();
    else void this.load();
  }
  more() {
    if (this.loading()) return;
    this.page++;
    void this.load(true);
  }
  async load(append = false) {
    const version = ++this.loadVersion;
    const owner = this.collection ? this.api.user()?.id : undefined;
    if (!append) this.hasNext.set(false);
    this.retryAppend = append;
    if (this.collection && !owner) {
      this.loading.set(false);
      return;
    }
    this.loading.set(true);
    this.error.set("");
    try {
      const path = this.collection
        ? "/library?kind=" + encodeURIComponent(this.kind)
        : this.lastQuery
          ? "/manga?q=" +
            encodeURIComponent(this.lastQuery) +
            "&page=" +
            this.page
          : "/manga/popular?page=" + this.page;
      const d = await this.api.request<Page>(path);
      if (
        version !== this.loadVersion ||
        (this.collection && owner !== this.api.user()?.id)
      )
        return;
      this.items.set(
        append
          ? [
              ...this.items(),
              ...d.items.filter(
                (x) => !this.items().some((i) => i.id === x.id),
              ),
            ]
          : d.items,
      );
      this.hasNext.set(!!d.hasNextPage);
    } catch (e) {
      if (version !== this.loadVersion) return;
      this.error.set(readableError(e));
      if (append) this.page--;
    } finally {
      if (version === this.loadVersion) this.loading.set(false);
    }
  }
}
