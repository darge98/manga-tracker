import "@angular/compiler";
import { test } from "node:test";
import assert from "node:assert/strict";
import {
  createEnvironmentInjector,
  runInInjectionContext,
  signal,
} from "@angular/core";
import { ActivatedRoute, convertToParamMap, Router } from "@angular/router";
import { Subject } from "rxjs";
import { Api } from "../src/app/core";
import { Detail } from "../src/app/detail";
import { Dashboard } from "../src/app/dashboard";
import { Explore } from "../src/app/explore";
import { Lists } from "../src/app/lists";
const manga = (id: number) => ({
  id,
  title: "Manga " + id,
  description: "",
  coverUrl: "",
  chapters: 2,
  status: "Publishing",
  genres: [],
  score: null,
  author: "",
  url: "",
});
const deferred = () => {
  let resolve!: (value: any) => void;
  let reject!: (value: any) => void;
  const promise = new Promise<any>((a, b) => {
    resolve = a;
    reject = b;
  });
  return { promise, resolve, reject };
};
const tick = () => new Promise((resolve) => setTimeout(resolve, 0));
function fixture(type: any) {
  const params = new Subject<any>();
  const api = new Api();
  api.user.set({ id: "1", username: "lettore", email: "a@example.org" });
  const router = { navigateByUrl: async () => true };
  const route = { paramMap: params, snapshot: { data: {} } };
  const injector = createEnvironmentInjector(
    [
      { provide: Api, useValue: api },
      { provide: ActivatedRoute, useValue: route },
      { provide: Router, useValue: router },
    ],
    null as any,
  );
  const component: any = runInInjectionContext(injector, () => new type());
  return { params, api, component, injector };
}
test("session uses HttpOnly credentials and clears expired identity", async () => {
  const original = globalThis.fetch;
  const api = new Api();
  let options: any;
  globalThis.fetch = async (_url: any, init: any) => {
    options = init;
    return new Response(
      JSON.stringify({
        user: { id: "1", username: "lettore", email: "a@example.org" },
      }),
      { status: 200 },
    );
  };
  await api.session();
  assert.equal(options.credentials, "include");
  assert.equal(api.user()?.username, "lettore");
  globalThis.fetch = async () =>
    new Response(JSON.stringify({ error: "Accesso richiesto" }), {
      status: 401,
    });
  await api.session();
  assert.equal(api.user(), null);
  assert.equal(api.ready(), true);
  globalThis.fetch = original;
});
test("detail ignores old manga response when route changes and clears failures", async () => {
  const f = fixture(Detail);
  const first = deferred(),
    second = deferred();
  f.api.request = ((p: string) =>
    p === "/manga/1" ? first.promise : second.promise) as any;
  f.params.next(convertToParamMap({ id: "1" }));
  f.params.next(convertToParamMap({ id: "2" }));
  second.reject(new Error("Non disponibile"));
  await tick();
  first.resolve({ manga: manga(1) });
  await tick();
  assert.equal(f.component.manga(), null);
  assert.equal(f.component.library().mangaId, 2);
  assert.equal(f.component.library().chapters.length, 0);
  assert.equal(f.component.error(), "Non disponibile");
  f.injector.destroy();
});
test("list navigation never keeps an old selection after access is denied", async () => {
  const f = fixture(Lists);
  f.api.request = async (p: string) => {
    if (p === "/lists/1")
      return {
        list: { id: "1", name: "Privata", isPublic: false },
        items: [manga(1)],
      };
    throw new Error("Lista non trovata");
  };
  f.params.next(convertToParamMap({ id: "1" }));
  await tick();
  assert.equal(f.component.items().length, 1);
  f.params.next(convertToParamMap({ id: "2" }));
  assert.equal(f.component.list(), null);
  assert.equal(f.component.items().length, 0);
  await tick();
  assert.equal(f.component.list(), null);
  assert.equal(f.component.error(), "Lista non trovata");
  f.injector.destroy();
});
test("chapter feedback supports unread decimal chapters independently of catalog count", async () => {
  const f = fixture(Detail);
  f.component.id = 1;
  f.component.manga.set(manga(1));
  f.component.chapter = 12.5;
  f.component.read = false;
  f.component.rating = 4;
  f.component.notes = "Una scena da ricordare";
  let payload: any;
  let path = "";
  f.api.request = async (p: string, method = "GET", body?: any) => {
    if (method === "PUT") {
      path = p;
      payload = body;
      return {};
    }
    return {
      mangaId: 1,
      chapters: [
        { chapter: "12.5", read: false, rating: 4, notes: body?.notes || "" },
      ],
      toRead: false,
      favorite: false,
    };
  };
  await f.component.saveChapter();
  assert.equal(path, "/library/1/chapters/12.5");
  assert.equal(payload.read, false);
  assert.equal(payload.rating, 4);
  assert.equal(payload.notes, "Una scena da ricordare");
  assert.equal(payload.manga.id, 1);
  f.injector.destroy();
});

test("concurrent startup session checks share one request", async () => {
  const original = globalThis.fetch;
  const response = deferred();
  let count = 0;
  globalThis.fetch = async () => {
    count++;
    return response.promise;
  };
  const api = new Api();
  const first = api.session();
  const second = api.session();
  assert.equal(first, second);
  assert.equal(count, 1);
  response.resolve(
    new Response(
      JSON.stringify({
        user: { id: "1", username: "lettore", email: "a@example.org" },
      }),
    ),
  );
  await Promise.all([first, second]);
  assert.equal(api.ready(), true);
  globalThis.fetch = original;
});
test("a private dashboard response received after logout cannot restore reading history", async () => {
  const pending = deferred();
  const api = new Api();
  api.user.set({ id: "1", username: "lettore", email: "a@example.org" });
  api.request = async () => pending.promise;
  const dashboard: any = Object.create(Dashboard.prototype);
  Object.assign(dashboard, {
    api,
    loadVersion: 0,
    recent: signal([{ manga: manga(1), chapter: "1" }]),
    popular: signal([]),
    loading: signal(false),
    error: signal(""),
  });
  const load = dashboard.load();
  assert.equal(dashboard.recent().length, 0);
  api.user.set(null);
  pending.resolve({
    recent: [{ manga: manga(1), chapter: "12" }],
    popular: [manga(1)],
  });
  await load;
  assert.deepEqual(dashboard.recent(), []);
  assert.deepEqual(dashboard.popular(), []);
});

test("a startup 401 received after login cannot clear the new identity", async () => {
  const original = globalThis.fetch;
  const pending = deferred();
  globalThis.fetch = async () => pending.promise;
  const api = new Api();
  const session = api.session();
  api.setUser({ id: "2", username: "nuovo", email: "b@example.org" });
  pending.resolve(
    new Response(JSON.stringify({ error: "Sessione scaduta" }), {
      status: 401,
    }),
  );
  await session;
  assert.equal(api.user()?.id, "2");
  globalThis.fetch = original;
});
test("a startup identity response after logout cannot restore the old account", async () => {
  const original = globalThis.fetch;
  const pending = deferred();
  globalThis.fetch = async (url: any) =>
    String(url).endsWith("/auth/me")
      ? pending.promise
      : new Response(JSON.stringify({ ok: true }));
  const api = new Api();
  const session = api.session();
  await api.logout();
  pending.resolve(
    new Response(
      JSON.stringify({
        user: { id: "1", username: "precedente", email: "a@example.org" },
      }),
    ),
  );
  await session;
  assert.equal(api.user(), null);
  globalThis.fetch = original;
});
test("saving a chapter while navigating never reloads or updates the next manga", async () => {
  const f = fixture(Detail);
  const pending = deferred();
  const gets: string[] = [];
  f.component.id = 1;
  f.component.manga.set(manga(1));
  f.component.chapter = 3;
  f.api.request = async (p: string, method = "GET") => {
    if (method === "PUT") return pending.promise;
    gets.push(p);
    if (p === "/manga/2") return { manga: manga(2) };
    if (p === "/lists") return { items: [] };
    return { mangaId: 2, chapters: [], favorite: false, toRead: false };
  };
  const save = f.component.saveChapter();
  f.params.next(convertToParamMap({ id: "2" }));
  await tick();
  pending.resolve({});
  await save;
  assert.equal(f.component.library().mangaId, 2);
  assert.equal(f.component.feedback(), "");
  assert.equal(gets.filter((p) => p === "/library/2").length, 1);
  assert.equal(gets.includes("/library/1"), false);
  f.injector.destroy();
});
test("removing a manga from one list cannot remove it from a newly opened list", async () => {
  const f = fixture(Lists);
  const pending = deferred();
  f.api.request = async (p: string, method = "GET") => {
    if (method === "DELETE") return pending.promise;
    return {
      list: { id: p.split("/").pop(), name: "Lista", isPublic: false },
      items: [manga(1)],
    };
  };
  f.params.next(convertToParamMap({ id: "1" }));
  await tick();
  const remove = f.component.remove(manga(1));
  f.params.next(convertToParamMap({ id: "2" }));
  await tick();
  pending.resolve({});
  await remove;
  assert.equal(f.component.list().id, "2");
  assert.equal(f.component.items().length, 1);
  assert.equal(f.component.message(), "");
  f.injector.destroy();
});

test("retrying a failed next search page preserves earlier results", async () => {
  const f = fixture(Explore);
  let fail = true;
  f.api.request = async (p: string) => {
    if (p.endsWith("page=2")) {
      if (fail) throw new Error("Catalogo temporaneamente non disponibile");
      return { items: [manga(2)], page: 2, hasNextPage: false };
    }
    return { items: [manga(1)], page: 1, hasNextPage: true };
  };
  f.params.next(convertToParamMap({}));
  await tick();
  f.component.more();
  await tick();
  assert.equal(f.component.items().length, 1);
  assert.equal(f.component.page, 1);
  fail = false;
  f.component.retry();
  await tick();
  assert.deepEqual(
    f.component.items().map((m: any) => m.id),
    [1, 2],
  );
  f.injector.destroy();
});
