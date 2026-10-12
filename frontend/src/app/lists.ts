import { Component, inject, signal } from "@angular/core";
import { FormsModule } from "@angular/forms";
import { ActivatedRoute, RouterLink, Router } from "@angular/router";
import { Api, MangaList, Manga, readableError } from "./core";
import { MangaCard } from "./card";
@Component({
  imports: [FormsModule, RouterLink, MangaCard],
  template: `<div class="page-heading">
      <p class="eyebrow">LE STORIE, COME PIACCIONO A TE</p>
      <h1>
        {{
          publicView
            ? "Una selezione da scoprire."
            : id
              ? list()?.name || "La tua lista."
              : "Le tue liste."
        }}
      </h1>
      <p>
        {{
          publicView
            ? "Una raccolta condivisa da un lettore."
            : id
              ? list()?.description
              : "Metti insieme i tuoi manga. Tienili per te o condividili."
        }}
      </p>
    </div>
    @if (!publicView && !api.user()) {
      <div class="empty">
        <h2>Le tue selezioni hanno bisogno di un posto.</h2>
        <p>Accedi per creare liste pubbliche e private.</p>
        <a class="button primary" routerLink="/auth">Accedi</a>
      </div>
    } @else {
      @if (error()) {
        <div class="notice error" role="alert">
          {{ error() }} <button (click)="load()">Riprova</button>
        </div>
      }
      @if (message()) {
        <p class="notice success" role="status">{{ message() }}</p>
      }
      @if (loading()) {
        <div class="loading" role="status">Caricamento delle liste…</div>
      } @else if (id) {
        @if (list(); as l) {
          <div class="list-toolbar">
            <span class="status" [class.is-read]="l.isPublic">{{
              l.isPublic ? "Lista pubblica" : "Lista privata"
            }}</span
            ><span>{{ items().length }} manga</span>
            @if (!publicView) {
              <button class="button secondary" (click)="startEdit()">
                Modifica lista
              </button>
              @if (l.isPublic) {
                <button class="button secondary" (click)="copyLink()">
                  Copia link</button
                ><a class="external" [routerLink]="['/public/lists', l.id]"
                  >Apri pagina pubblica ↗</a
                >
              }
            }
          </div>
          @if (editing && !publicView) {
            <form class="list-form" (ngSubmit)="save()" #editForm="ngForm">
              <h2>Modifica la tua lista</h2>
              <label for="edit-name">Nome</label
              ><input
                id="edit-name"
                name="name"
                [(ngModel)]="name"
                required
                maxlength="100"
              /><label for="edit-description">Descrizione</label
              ><textarea
                id="edit-description"
                name="description"
                [(ngModel)]="description"
                maxlength="1000"
                rows="2"
              ></textarea
              ><label class="checkbox"
                ><input
                  type="checkbox"
                  name="isPublic"
                  [(ngModel)]="isPublic"
                />
                Pubblica, visibile a chi ha il link</label
              >
              <div class="actions">
                <button
                  class="button primary"
                  [disabled]="busy() || editForm.invalid"
                >
                  Salva modifiche</button
                ><button
                  type="button"
                  class="button secondary"
                  (click)="editing = false"
                >
                  Annulla</button
                ><button
                  type="button"
                  class="text-button danger"
                  [disabled]="busy()"
                  (click)="deleteList()"
                >
                  Elimina lista
                </button>
              </div>
            </form>
          }
          <div class="manga-grid">
            @for (m of items(); track m.id) {
              <div>
                <manga-card [manga]="m" />
                @if (!publicView) {
                  <button
                    class="remove-button"
                    [disabled]="busy()"
                    (click)="remove(m)"
                  >
                    Rimuovi dalla lista
                  </button>
                }
              </div>
            }
          </div>
          @if (!items().length) {
            <div class="empty">
              <h3>Questa selezione è ancora vuota.</h3>
              <p>
                {{
                  publicView
                    ? "Torna più tardi per scoprire i manga aggiunti."
                    : "Cerca un manga e aggiungilo alla lista dalla sua pagina."
                }}
              </p>
              @if (!publicView) {
                <a class="button primary" routerLink="/explore"
                  >Trova un manga</a
                >
              }
            </div>
          }
        }
      } @else {
        <form class="list-form" (ngSubmit)="save()" #createForm="ngForm">
          <h2>Una nuova selezione</h2>
          <div class="new-list-grid">
            <div>
              <label for="new-name">Nome della lista</label
              ><input
                id="new-name"
                name="name"
                [(ngModel)]="name"
                required
                maxlength="100"
                placeholder="Ad esempio: Storie da rileggere"
              />
            </div>
            <div>
              <label for="new-description">Descrizione (facoltativa)</label
              ><input
                id="new-description"
                name="description"
                [(ngModel)]="description"
                maxlength="1000"
                placeholder="Cosa rende speciale questa selezione?"
              />
            </div>
          </div>
          <div class="form-footer">
            <label class="checkbox"
              ><input type="checkbox" name="isPublic" [(ngModel)]="isPublic" />
              Pubblica, visibile a chi ha il link</label
            ><button
              class="button primary"
              [disabled]="busy() || createForm.invalid"
            >
              {{ busy() ? "Creazione…" : "Crea lista" }}
            </button>
          </div>
        </form>
        <div class="lists-grid">
          @for (l of lists(); track l.id) {
            <a class="list-card" [routerLink]="['/lists', l.id]"
              ><div class="list-card-top">
                <span class="list-symbol" aria-hidden="true">▤</span
                ><span class="status">{{
                  l.isPublic ? "Pubblica" : "Privata"
                }}</span>
              </div>
              <h2>{{ l.name }}</h2>
              <p>
                {{
                  l.description ||
                    "Una selezione personale, una storia dopo l’altra."
                }}
              </p>
              <div class="list-card-bottom">
                <span>{{ l.itemCount }} manga</span><span>Apri lista →</span>
              </div></a
            >
          }
        </div>
        @if (!lists().length) {
          <div class="empty compact">
            <h3>Un tema, un genere, un’emozione.</h3>
            <p>
              Crea la tua prima lista e raccogli i manga che vuoi tenere
              insieme.
            </p>
          </div>
        }
      }
    }`,
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
  async mutate(action: (current: () => boolean) => Promise<void>) {
    if (this.busy()) return;
    const version = this.loadVersion;
    const owner = this.api.user()?.id;
    const current = () =>
      version === this.loadVersion && owner === this.api.user()?.id;
    this.busy.set(true);
    this.error.set("");
    try {
      await action(current);
    } catch (e) {
      if (current()) this.error.set(readableError(e));
    } finally {
      if (version === this.loadVersion) this.busy.set(false);
    }
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
