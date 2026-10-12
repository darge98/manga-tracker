import { Component, inject, signal, computed } from "@angular/core";
import { FormsModule } from "@angular/forms";
import { ActivatedRoute, RouterLink } from "@angular/router";
import {
  Api,
  Manga,
  MangaList,
  Chapter,
  Library,
  mutate,
  readableError,
} from "./core";
@Component({
  imports: [FormsModule, RouterLink],
  templateUrl: "./detail.html",
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
      const d = await this.api.request<{
        manga: Manga;
        catalogWarning: string;
      }>("/manga/" + this.id);
      if (version !== this.loadVersion) return;
      this.manga.set(d.manga);
      if (d.catalogWarning)
        this.feedback.set(
          "The catalog is unavailable: showing previously saved details.",
        );
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
    const next = !this.library()[key];
    await this.mutate(async (current) => {
      await this.api.request("/library/" + id, "PUT", { [key]: next });
      if (!current()) return;
      this.library.update((l) => ({ ...l, [key]: next }));
      this.feedback.set(
        next ? "Added to your library." : "Removed from your library.",
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
    };
    // An unread chapter without rating or notes is not stored: saving it removes it.
    const removed = !body.read && body.rating === null && !body.notes;
    await this.mutate(async (current) => {
      const l = await this.api.request<Library>(
        "/library/" + id + "/chapters/" + chapter,
        "PUT",
        body,
      );
      if (!current()) return;
      this.library.set({ ...l, chapters: l.chapters || [] });
      this.feedback.set(
        "Chapter " +
          chapter +
          (removed ? " removed from your journal." : " saved to your journal."),
      );
    });
  }
  isSaved() {
    return this.library().chapters.some(
      (c) => Number(c.chapter) === this.chapter,
    );
  }
  async deleteChapter() {
    const id = this.id;
    const chapter = this.chapter;
    if (
      this.busy() ||
      !confirm(
        "Delete chapter " +
          chapter +
          " from your journal? Its notes and rating will be lost.",
      )
    )
      return;
    await this.mutate(async (current) => {
      const l = await this.api.request<Library>(
        "/library/" + id + "/chapters/" + chapter,
        "DELETE",
      );
      if (!current()) return;
      this.library.set({ ...l, chapters: l.chapters || [] });
      this.selectChapter();
      this.feedback.set("Chapter " + chapter + " deleted from your journal.");
    });
  }
  async addList() {
    const id = this.id;
    const list = this.selectedList;
    await this.mutate(async (current) => {
      await this.api.request("/lists/" + list + "/items/" + id, "PUT", {});
      if (current()) this.feedback.set("Manga added to the list.");
    });
  }
  mutate(action: (current: () => boolean) => Promise<void>) {
    return mutate(this, action, () => this.feedback.set(""));
  }
}
