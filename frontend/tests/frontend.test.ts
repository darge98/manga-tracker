import { test } from "node:test";
import assert from "node:assert/strict";
import {
  createEnvironmentInjector,
  runInInjectionContext,
  signal,
} from "@angular/core";
import { ActivatedRoute, convertToParamMap, Router } from "@angular/router";
import { Subject } from "rxjs";
import { Api, ApiError } from "../src/app/core";
import { Detail } from "../src/app/detail";
import { Dashboard } from "../src/app/dashboard";
import { Explore } from "../src/app/explore";
import { Lists } from "../src/app/lists";
import { Auth } from "../src/app/auth";
import { Account } from "../src/app/account";
import { requireUser } from "../src/app/app.routes";
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
  api.user.set({ id: "1", username: "reader", email: "a@example.org" });
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
        user: { id: "1", username: "reader", email: "a@example.org" },
      }),
      { status: 200 },
    );
  };
  await api.session();
  assert.equal(options.credentials, "include");
  assert.equal(api.user()?.username, "reader");
  globalThis.fetch = async () =>
    new Response(JSON.stringify({ error: "Sign in required" }), {
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
  second.reject(new Error("Unavailable"));
  await tick();
  first.resolve({ manga: manga(1) });
  await tick();
  assert.equal(f.component.manga(), null);
  assert.equal(f.component.library().mangaId, 2);
  assert.equal(f.component.library().chapters.length, 0);
  assert.equal(f.component.error(), "Unavailable");
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
    throw new Error("List not found");
  };
  f.params.next(convertToParamMap({ id: "1" }));
  await tick();
  assert.equal(f.component.items().length, 1);
  f.params.next(convertToParamMap({ id: "2" }));
  assert.equal(f.component.list(), null);
  assert.equal(f.component.items().length, 0);
  await tick();
  assert.equal(f.component.list(), null);
  assert.equal(f.component.error(), "List not found");
  f.injector.destroy();
});
test("chapter feedback supports unread decimal chapters independently of catalog count", async () => {
  const f = fixture(Detail);
  f.component.id = 1;
  f.component.manga.set(manga(1));
  f.component.chapter = 12.5;
  f.component.read = false;
  f.component.rating = 4;
  f.component.notes = "A scene to remember";
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
  assert.equal(payload.notes, "A scene to remember");
  assert.equal(payload.manga, undefined);
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
        user: { id: "1", username: "reader", email: "a@example.org" },
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
  api.user.set({ id: "1", username: "reader", email: "a@example.org" });
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
    new Response(JSON.stringify({ error: "Session expired" }), {
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
      list: { id: p.split("/").pop(), name: "List", isPublic: false },
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
      if (fail) throw new Error("Catalog temporarily unavailable");
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

test("deleting a chapter calls DELETE and refreshes the diary", async () => {
  const f = fixture(Detail);
  f.component.id = 1;
  f.component.manga.set(manga(1));
  f.component.library.set({
    mangaId: 1,
    chapters: [
      { chapter: "3", read: true, readAt: null, rating: 4, notes: "x" },
    ],
    toRead: false,
    favorite: false,
  });
  f.component.chapter = 3;
  assert.equal(f.component.isSaved(), true);
  const originalConfirm = globalThis.confirm;
  globalThis.confirm = () => true;
  let call = "";
  f.api.request = async (p: string, method = "GET") => {
    call = method + " " + p;
    return { mangaId: 1, chapters: [], toRead: false, favorite: false };
  };
  await f.component.deleteChapter();
  globalThis.confirm = originalConfirm;
  assert.equal(call, "DELETE /library/1/chapters/3");
  assert.equal(f.component.library().chapters.length, 0);
  assert.equal(f.component.isSaved(), false);
  assert.equal(f.component.notes, "");
  f.injector.destroy();
});

test("API errors are shown from their stable code", async () => {
  const original = globalThis.fetch;
  const api = new Api();
  globalThis.fetch = async () =>
    new Response(
      JSON.stringify({
        code: "email_taken",
        error: "An account with this email already exists",
      }),
      { status: 409 },
    );
  await assert.rejects(api.request("/auth/register", "POST", {}), {
    message: "An account with this email already exists.",
  });
  globalThis.fetch = async () =>
    new Response(JSON.stringify({ error: "Too many requests" }), {
      status: 429,
    });
  await assert.rejects(api.request("/manga"), {
    message: "Too many requests. Please try again in a minute.",
  });
  globalThis.fetch = async () => {
    throw new TypeError("Failed to fetch");
  };
  await assert.rejects(api.request("/manga"), {
    message: "Could not reach the server. Check your connection and try again.",
  });
  globalThis.fetch = original;
});

test("searches shorter than 3 characters never reach the API", async () => {
  const f = fixture(Explore);
  let calls = 0;
  f.api.request = async () => {
    calls++;
    return { items: [], page: 1, hasNextPage: false };
  };
  f.component.query = " ab ";
  assert.equal(f.component.tooShort(), true);
  f.component.search();
  await tick();
  assert.equal(calls, 0);
  f.component.query = "";
  assert.equal(f.component.tooShort(), false);
  f.component.query = "abc";
  f.component.search();
  await tick();
  assert.equal(calls, 1);
  f.injector.destroy();
});

test("personal pages send anonymous visitors to sign-in and back", async () => {
  const api = new Api();
  // Session not checked yet: the guard must await it, as on a fresh page load.
  let sessionChecks = 0;
  api.session = async () => {
    sessionChecks++;
    await tick();
    api.ready.set(true);
  };
  const router = {
    createUrlTree: (commands: string[], extras: any) => ({ commands, extras }),
  };
  const injector = createEnvironmentInjector(
    [
      { provide: Api, useValue: api },
      { provide: Router, useValue: router },
    ],
    null as any,
  );
  const run = () =>
    runInInjectionContext(injector, () =>
      requireUser({} as any, { url: "/lists/abc" } as any),
    );
  assert.deepEqual(await run(), {
    commands: ["/auth"],
    extras: { queryParams: { returnUrl: "/lists/abc" } },
  });
  assert.equal(sessionChecks, 1);
  api.user.set({ id: "1", username: "reader", email: "a@example.org" });
  assert.equal(await run(), true);
  injector.destroy();
});

test("sign-in only returns to paths inside the app", () => {
  const returnUrlFor = (value: string | null) => {
    const injector = createEnvironmentInjector(
      [
        { provide: Api, useValue: new Api() },
        { provide: Router, useValue: {} },
        {
          provide: ActivatedRoute,
          useValue: {
            snapshot: {
              queryParamMap: convertToParamMap(
                value === null ? {} : { returnUrl: value },
              ),
            },
          },
        },
      ],
      null as any,
    );
    const auth: any = runInInjectionContext(injector, () => new Auth());
    const url = auth.returnUrl();
    injector.destroy();
    return url;
  };
  assert.equal(returnUrlFor("/manga/2"), "/manga/2");
  assert.equal(returnUrlFor(null), "/");
  for (const unsafe of [
    "//evil.example",
    "/\\evil.example",
    "https://evil.example",
    "javascript:alert(1)",
  ])
    assert.equal(returnUrlFor(unsafe), "/", unsafe);
});

test("account: password change checks confirmation and reports the result", async () => {
  const f = fixture(Account);
  const calls: string[] = [];
  f.api.request = async (p: string, method = "GET", body?: any) => {
    calls.push(method + " " + p + " " + JSON.stringify(body));
    if (body.currentPassword === "wrong")
      throw new ApiError(403, "wrong_password");
    return { ok: true };
  };
  f.component.currentPassword = "old-password";
  f.component.newPassword = "new-password-1";
  f.component.confirmPassword = "different-password";
  await f.component.changePassword();
  assert.equal(calls.length, 0);
  assert.equal(f.component.error(), "The two new passwords do not match.");

  f.component.currentPassword = "wrong";
  f.component.confirmPassword = "new-password-1";
  await f.component.changePassword();
  assert.equal(f.component.error(), "Your current password is incorrect.");
  assert.equal(f.api.user()?.id, "1"); // a wrong password keeps the user signed in

  f.component.currentPassword = "old-password";
  await f.component.changePassword();
  assert.equal(
    calls.at(-1),
    'PUT /auth/password {"currentPassword":"old-password","newPassword":"new-password-1"}',
  );
  assert.equal(f.component.error(), "");
  assert.match(f.component.message(), /Password updated/);
  assert.equal(f.component.newPassword, "");
  f.injector.destroy();
});

test("account: deletion asks for confirmation, then signs out", async () => {
  const f = fixture(Account);
  let call = "";
  let navigated = "";
  f.api.request = async (p: string, method = "GET", body?: any) => {
    call = method + " " + p + " " + JSON.stringify(body);
    return { ok: true };
  };
  f.component.router.navigateByUrl = async (url: string) => {
    navigated = url;
    return true;
  };
  f.component.deletePassword = "password-123";
  const originalConfirm = globalThis.confirm;
  globalThis.confirm = () => false;
  await f.component.deleteAccount();
  assert.equal(call, "");
  globalThis.confirm = () => true;
  await f.component.deleteAccount();
  globalThis.confirm = originalConfirm;
  assert.equal(call, 'DELETE /auth/account {"password":"password-123"}');
  assert.equal(f.api.user(), null);
  assert.equal(navigated, "/");
  f.injector.destroy();
});
