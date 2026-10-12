import { Component, inject, signal } from "@angular/core";
import { FormsModule } from "@angular/forms";
import { ActivatedRoute, RouterLink } from "@angular/router";
import { Api, Manga, Page, readableError } from "./core";
import { MangaCard } from "./card";
@Component({
  imports: [FormsModule, MangaCard, RouterLink],
  template: `<div class="page-heading">
      <p class="eyebrow">TROVA LA TUA PROSSIMA STORIA</p>
      <h1>
        {{
          collection
            ? kind === "favorites"
              ? "I tuoi preferiti."
              : "Da leggere."
            : "Lasciati incuriosire."
        }}
      </h1>
      <p>
        {{
          collection
            ? "Una selezione tutta tua, pronta per il prossimo capitolo."
            : "Cerca un titolo o parti dai manga più amati."
        }}
      </p>
    </div>
    @if (!collection) {
      <form class="search-form" (ngSubmit)="search()">
        <label class="sr-only" for="manga-search">Titolo del manga</label
        ><input
          id="manga-search"
          name="query"
          [(ngModel)]="query"
          placeholder="Cerca un manga, ad esempio Berserk"
          autocomplete="off"
        /><button class="button primary" type="submit" [disabled]="loading()">
          Cerca
        </button>
      </form>
    }
    @if (collection && !api.user()) {
      <div class="empty">
        <h2>Una biblioteca solo tua.</h2>
        <p>Accedi per salvare i manga che vuoi leggere e i tuoi preferiti.</p>
        <a class="button primary" routerLink="/auth">Accedi</a>
      </div>
    } @else {
      @if (error()) {
        <div class="notice error" role="alert">
          {{ error() }} <button (click)="retry()">Riprova</button>
        </div>
      }
      <div class="results-heading">
        <h2>
          {{
            collection
              ? "La tua selezione"
              : lastQuery
                ? "Risultati per “" + lastQuery + "”"
                : "Popolari nel catalogo"
          }}
        </h2>
        <span
          >{{ items().length }} titoli{{ hasNext() ? " e altri" : "" }}</span
        >
      </div>
      @if (loading() && !items().length) {
        <div class="loading" role="status">Stiamo cercando le storie…</div>
      }
      <div class="manga-grid">
        @for (manga of items(); track manga.id) {
          <manga-card [manga]="manga" />
        }
      </div>
      @if (!loading() && !items().length && !error()) {
        <div class="empty">
          <h3>
            {{
              collection
                ? "La tua lista è ancora vuota."
                : "Nessun manga trovato."
            }}
          </h3>
          <p>
            {{
              collection
                ? "Apri un manga e aggiungilo alla tua biblioteca."
                : "Prova un titolo diverso o una parola più breve."
            }}
          </p>
          @if (collection) {
            <a class="button primary" routerLink="/explore">Esplora manga</a>
          }
        </div>
      }
      @if (hasNext()) {
        <div class="load-more">
          <button
            class="button secondary"
            (click)="more()"
            [disabled]="loading()"
          >
            {{ loading() ? "Caricamento…" : "Mostra altri manga" }}
          </button>
        </div>
      }
    }`,
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
  search() {
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
