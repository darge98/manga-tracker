import { CanActivateFn, Router, Routes } from "@angular/router";
import { inject } from "@angular/core";
import { Api } from "./core";
/** Waits for the session check, so pages know whether a user is signed in. */
const ready: CanActivateFn = async () => {
  const api = inject(Api);
  if (!api.ready()) await api.session();
  return true;
};
/** Personal pages: anonymous visitors go to sign-in and come back afterwards. */
export const requireUser: CanActivateFn = async (_route, state) => {
  // inject() only works before the first await.
  const api = inject(Api);
  const router = inject(Router);
  if (!api.ready()) await api.session();
  return api.user()
    ? true
    : router.createUrlTree(["/auth"], {
        queryParams: { returnUrl: state.url },
      });
};
export const routes: Routes = [
  {
    path: "",
    canActivate: [ready],
    loadComponent: () => import("./dashboard").then((m) => m.Dashboard),
  },
  {
    path: "explore",
    canActivate: [ready],
    loadComponent: () => import("./explore").then((m) => m.Explore),
  },
  {
    path: "library/:kind",
    canActivate: [requireUser],
    loadComponent: () => import("./explore").then((m) => m.Explore),
  },
  {
    path: "manga/:id",
    canActivate: [ready],
    loadComponent: () => import("./detail").then((m) => m.Detail),
  },
  { path: "auth", loadComponent: () => import("./auth").then((m) => m.Auth) },
  {
    path: "lists",
    canActivate: [requireUser],
    loadComponent: () => import("./lists").then((m) => m.Lists),
  },
  {
    path: "lists/:id",
    canActivate: [requireUser],
    loadComponent: () => import("./lists").then((m) => m.Lists),
  },
  {
    path: "public/lists/:id",
    data: { public: true },
    loadComponent: () => import("./lists").then((m) => m.Lists),
  },
  { path: "**", redirectTo: "" },
];
