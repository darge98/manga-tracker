import { Component, inject, signal, computed } from "@angular/core";
import { FormsModule } from "@angular/forms";
import { ActivatedRoute, RouterLink } from "@angular/router";
import { Api, Manga, MangaList, Chapter, Library, readableError } from "./core";
@Component({
  imports: [FormsModule, RouterLink],
  template: `<a class="back-link" routerLink="/explore">← Torna al catalogo</a>
    @if (loading()) {
      <div class="loading" role="status">Caricamento del manga…</div>
    }
    @if (error()) {
      <div class="notice error" role="alert">
        {{ error() }} <button (click)="load()">Riprova</button>
      </div>
    }
    @if (manga(); as m) {
      <section class="detail-hero">
        <div class="detail-cover">
          <img [src]="m.coverUrl" [alt]="'Copertina di ' + m.title" />
        </div>
        <div>
          <p class="eyebrow">{{ m.status }}</p>
          <h1>{{ m.title }}</h1>
          <p class="author">{{ m.author || "Autore non disponibile" }}</p>
          <div class="tags">
            @for (genre of m.genres; track genre) {
              <span>{{ genre }}</span>
            }
          </div>
          <dl class="manga-facts">
            <div>
              <dt>Capitoli</dt>
              <dd>{{ m.chapters || "Totale non disponibile" }}</dd>
            </div>
            <div>
              <dt>Valutazione catalogo</dt>
              <dd>{{ m.score ? m.score + " / 10" : "Non disponibile" }}</dd>
            </div>
          </dl>
          <div class="actions">
            @if (api.user()) {
              <button
                class="button"
                [class.primary]="library().toRead"
                [class.secondary]="!library().toRead"
                [disabled]="busy() || loading()"
                (click)="toggle('toRead')"
              >
                {{
                  library().toRead ? "In Da leggere" : "Aggiungi a Da leggere"
                }}</button
              ><button
                class="button secondary"
                [disabled]="busy() || loading()"
                (click)="toggle('favorite')"
              >
                {{
                  library().favorite
                    ? "Rimuovi dai preferiti"
                    : "Aggiungi ai preferiti"
                }}
              </button>
            } @else {
              <a class="button primary" routerLink="/auth"
                >Accedi per tenere il segno</a
              >
            }
            <a
              [href]="m.url"
              target="_blank"
              rel="noopener noreferrer"
              class="external"
              >Su MyAnimeList ↗</a
            >
          </div>
        </div>
      </section>
      <section class="synopsis">
        <h2>La storia</h2>
        <p>
          {{
            m.description ||
              "La descrizione non è disponibile per questo titolo."
          }}
        </p>
        <small class="muted"
          >Le descrizioni del catalogo possono essere in inglese.</small
        >
      </section>
      @if (api.user()) {
        <section>
          <div class="section-heading">
            <div>
              <h2>Il tuo diario dei capitoli</h2>
              <p>
                {{ readCount() }} capitoli letti{{
                  m.chapters ? " su " + m.chapters : ""
                }}.
                @if (!m.chapters) {
                  Il totale non è ancora noto.
                }
              </p>
            </div>
          </div>
          <form
            class="chapter-form"
            (ngSubmit)="saveChapter()"
            #chapterForm="ngForm"
          >
            <div class="chapter-form-head">
              <div>
                <label for="chapter-number">Capitolo</label
                ><input
                  id="chapter-number"
                  name="chapter"
                  type="number"
                  min="0.001"
                  max="100000"
                  step="any"
                  required
                  [disabled]="busy() || loading()"
                  [(ngModel)]="chapter"
                  (ngModelChange)="selectChapter()"
                />
              </div>
              <label class="checkbox"
                ><input
                  name="read"
                  type="checkbox"
                  [disabled]="busy() || loading()"
                  [(ngModel)]="read"
                />
                Segna come letto</label
              >
              <div>
                <label for="rating">Il tuo voto</label
                ><select
                  id="rating"
                  name="rating"
                  [disabled]="busy() || loading()"
                  [(ngModel)]="rating"
                >
                  <option [ngValue]="null">Nessun voto</option>
                  <option [ngValue]="1">1 / 5</option>
                  <option [ngValue]="2">2 / 5</option>
                  <option [ngValue]="3">3 / 5</option>
                  <option [ngValue]="4">4 / 5</option>
                  <option [ngValue]="5">5 / 5</option>
                </select>
              </div>
            </div>
            <label for="notes">Cosa ti ha lasciato questo capitolo?</label
            ><textarea
              id="notes"
              name="notes"
              [disabled]="busy() || loading()"
              [(ngModel)]="notes"
              maxlength="5000"
              rows="3"
              placeholder="Un pensiero, una scena, una teoria…"
            ></textarea>
            <div class="form-footer">
              <small>Le tue note e valutazioni restano private.</small
              ><button
                class="button primary"
                [disabled]="busy() || loading() || chapterForm.invalid"
              >
                {{ busy() ? "Salvataggio…" : "Salva capitolo" }}
              </button>
            </div>
          </form>
          @if (feedback()) {
            <p class="notice success" role="status">{{ feedback() }}</p>
          }
          @if (library().chapters.length) {
            <div class="chapter-history">
              @for (c of library().chapters; track c.chapter) {
                <button
                  class="chapter-row"
                  [disabled]="busy() || loading()"
                  (click)="edit(c)"
                >
                  <strong>Capitolo {{ c.chapter }}</strong
                  ><span class="status" [class.is-read]="c.read">{{
                    c.read ? "Letto" : "Da leggere"
                  }}</span
                  ><span>{{
                    c.rating ? c.rating + " / 5" : "Nessun voto"
                  }}</span
                  ><span class="chapter-note">{{
                    c.notes || "Nessuna nota"
                  }}</span
                  ><span>Modifica →</span>
                </button>
              }
            </div>
          } @else {
            <p class="muted">
              Non hai ancora registrato capitoli per questo manga.
            </p>
          }
        </section>
        <section class="add-list">
          <h2>Aggiungi a una tua lista</h2>
          @if (lists().length) {
            <form (ngSubmit)="addList()">
              <label class="sr-only" for="select-list">Scegli una lista</label
              ><select
                id="select-list"
                name="list"
                [(ngModel)]="selectedList"
                required
              >
                <option value="">Scegli una lista</option>
                @for (list of lists(); track list.id) {
                  <option [value]="list.id">
                    {{ list.name }} ·
                    {{ list.isPublic ? "Pubblica" : "Privata" }}
                  </option>
                }</select
              ><button
                class="button secondary"
                [disabled]="!selectedList || busy() || loading()"
              >
                Aggiungi
              </button>
            </form>
          } @else {
            <p>
              Raccogli le tue letture in una selezione personale.
              <a routerLink="/lists">Crea la prima lista →</a>
            </p>
          }
        </section>
      }
    }`,
})
export class Detail {
  api = inject(Api);
  route = inject(ActivatedRoute);
  manga = signal<Manga | null>(null);
  library = signal<Library>({
    mangaId: 0,
    chapters: [],
    toRead: false,
    favorite: false,
  });
  lists = signal<MangaList[]>([]);
  loading = signal(true);
  busy = signal(false);
  error = signal("");
  feedback = signal("");
  readCount = computed(
    () => this.library().chapters.filter((c) => c.read).length,
  );
  chapter = 1;
  read = true;
  rating: number | null = null;
  notes = "";
  selectedList = "";
  id = 0;
  loadVersion = 0;
  constructor() {
    this.route.paramMap.subscribe((p) => {
      this.id = Number(p.get("id"));
      void this.load();
    });
  }
  async load() {
    const version = ++this.loadVersion;
    this.busy.set(false);
    this.manga.set(null);
    this.library.set({
      mangaId: this.id,
      chapters: [],
      toRead: false,
      favorite: false,
    });
    this.lists.set([]);
    this.feedback.set("");
    this.loading.set(true);
    this.error.set("");
    try {
      const d = await this.api.request<{ manga: Manga }>("/manga/" + this.id);
      if (version !== this.loadVersion) return;
      this.manga.set(d.manga);
      if ((d as any).catalogWarning)
        this.feedback.set((d as any).catalogWarning);
      if (this.api.user()) {
        const [l, ls] = await Promise.all([
          this.api.request<Library>("/library/" + this.id),
          this.api.request<{ items: MangaList[] }>("/lists"),
        ]);
        if (version !== this.loadVersion) return;
        this.library.set({
          ...l,
          chapters: (l.chapters || []).sort(
            (a, b) => Number(a.chapter) - Number(b.chapter),
          ),
        });
        this.lists.set(ls.items);
        const max = Math.max(
          0,
          ...(l.chapters || [])
            .filter((c) => c.read)
            .map((c) => Number(c.chapter)),
        );
        this.chapter = max + 1;
        this.selectChapter();
      }
    } catch (e) {
      if (version === this.loadVersion) this.error.set(readableError(e));
    } finally {
      if (version === this.loadVersion) this.loading.set(false);
    }
  }
  selectChapter() {
    const c = this.library().chapters.find(
      (c) => Number(c.chapter) === this.chapter,
    );
    this.read = c?.read ?? true;
    this.rating = c?.rating ?? null;
    this.notes = c?.notes ?? "";
  }
  edit(c: Chapter) {
    this.chapter = Number(c.chapter);
    this.selectChapter();
    document.getElementById("chapter-number")?.focus();
  }
  async toggle(key: "toRead" | "favorite") {
    const id = this.id;
    const manga = this.manga();
    const next = !this.library()[key];
    await this.mutate(async (current) => {
      await this.api.request("/library/" + id, "PUT", { [key]: next, manga });
      if (!current()) return;
      this.library.update((l) => ({ ...l, [key]: next }));
      this.feedback.set(
        next ? "Aggiunto alla tua biblioteca." : "Rimosso dalla selezione.",
      );
    });
  }
  async saveChapter() {
    if (
      !Number.isFinite(this.chapter) ||
      this.chapter <= 0 ||
      this.chapter > 100000
    )
      return;
    const id = this.id;
    const chapter = this.chapter;
    const body = {
      read: this.read,
      rating: this.rating,
      notes: this.notes,
      manga: this.manga(),
    };
    await this.mutate(async (current) => {
      await this.api.request(
        "/library/" + id + "/chapters/" + chapter,
        "PUT",
        body,
      );
      if (!current()) return;
      const l = await this.api.request<Library>("/library/" + id);
      if (!current()) return;
      this.library.set({ ...l, chapters: l.chapters || [] });
      this.feedback.set("Capitolo " + chapter + " salvato nel tuo diario.");
    });
  }
  async addList() {
    const id = this.id;
    const list = this.selectedList;
    const manga = this.manga();
    await this.mutate(async (current) => {
      await this.api.request("/lists/" + list + "/items/" + id, "PUT", {
        manga,
      });
      if (current()) this.feedback.set("Manga aggiunto alla lista.");
    });
  }
  async mutate(action: (current: () => boolean) => Promise<void>) {
    if (this.busy()) return;
    const version = this.loadVersion;
    const owner = this.api.user()?.id;
    const current = () =>
      version === this.loadVersion && owner === this.api.user()?.id;
    this.busy.set(true);
    this.error.set("");
    this.feedback.set("");
    try {
      await action(current);
    } catch (e) {
      if (current()) this.error.set(readableError(e));
    } finally {
      if (version === this.loadVersion) this.busy.set(false);
    }
  }
}
