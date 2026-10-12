import { Component, inject, signal, effect } from "@angular/core";
import { RouterLink } from "@angular/router";
import { Api, Manga, readableError } from "./core";
import { MangaCard } from "./card";
@Component({
  imports: [RouterLink, MangaCard],
  template: `<div class="page-heading">
      <p class="eyebrow">IL PIACERE DI CONTINUARE</p>
      <h1>La prossima pagina<br />ti aspetta<span class="accent">.</span></h1>
      <p>Le tue storie, i tuoi progressi. Tutto al suo posto.</p>
    </div>
    <section class="welcome">
      <div>
        <span class="small-label">IL TUO DIARIO DI LETTURA</span>
        <h2>Non perdere il filo.</h2>
        <p>Segna i capitoli letti e conserva ciò che ti hanno lasciato.</p>
        <a
          class="button primary"
          [routerLink]="api.user() ? '/explore' : '/auth'"
          >{{ api.user() ? "Trova la prossima lettura" : "Crea il tuo spazio" }}
          <span aria-hidden="true">→</span></a
        >
      </div>
      <div class="welcome-art" aria-hidden="true">
        <span class="book-back"></span
        ><span class="book-front"
          ><span>MANGA<br />TRACK</span><b>読む</b
          ><small>IL TUO DIARIO</small></span
        >
      </div>
    </section>
    @if (error()) {
      <div class="notice error" role="alert">
        {{ error() }} <button (click)="load()">Riprova</button>
      </div>
    }
    <section>
      <div class="section-heading">
        <div>
          <h2>Riprendi da qui</h2>
          <p>Le tue letture più recenti.</p>
        </div>
        <a routerLink="/explore"
          >Esplora il catalogo <span aria-hidden="true">→</span></a
        >
      </div>
      @if (loading()) {
        <div class="loading" role="status">Caricamento delle letture…</div>
      } @else if (api.user() && recent().length) {
        <div class="recent-grid">
          @for (item of recent(); track item.manga.id + ":" + item.chapter) {
            <a class="recent-card" [routerLink]="['/manga', item.manga.id]"
              ><img
                [src]="item.manga.coverUrl"
                [alt]="'Copertina di ' + item.manga.title"
              />
              <div>
                <span class="small-label">IN LETTURA</span>
                <h3>{{ item.manga.title }}</h3>
                <p>Capitolo {{ item.chapter }} letto</p>
                <span class="continue"
                  >Continua <span aria-hidden="true">→</span></span
                >
              </div></a
            >
          }
        </div>
      } @else {
        <div class="empty compact">
          <h3>
            {{
              api.user()
                ? "Il primo capitolo è ancora da scrivere."
                : "La tua storia inizia qui."
            }}
          </h3>
          <p>
            {{
              api.user()
                ? "Apri un manga e segna un capitolo come letto. Lo ritroverai qui."
                : "Accedi per conservare le tue letture e ritrovarle su ogni dispositivo."
            }}
          </p>
          <a [routerLink]="api.user() ? '/explore' : '/auth'"
            >{{ api.user() ? "Scegli un manga" : "Accedi al tuo spazio" }} →</a
          >
        </div>
      }
    </section>
    <section>
      <div class="section-heading">
        <div>
          <h2>Storie che conquistano</h2>
          <p>I manga più popolari, da scoprire o riscoprire.</p>
        </div>
        <a routerLink="/explore">Vedi tutti →</a>
      </div>
      @if (loading()) {
        <div class="skeleton-grid" aria-hidden="true">
          @for (n of [1, 2, 3, 4, 5]; track n) {
            <div class="skeleton"></div>
          }
        </div>
      } @else {
        <div class="manga-grid">
          @for (manga of popular(); track manga.id) {
            <manga-card [manga]="manga" />
          }
        </div>
        @if (!popular().length && !error()) {
          <div class="empty">Nessun manga disponibile al momento.</div>
        }
      }
    </section>`,
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
        }>(
          "/dashboard",
        );
        if (version !== this.loadVersion || owner !== this.api.user()?.id)
          return;
        this.popular.set(d.popular);
        this.recent.set(d.recent);
        if (d.catalogWarning) this.error.set(d.catalogWarning);
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
