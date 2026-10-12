import { Routes } from "@angular/router";
import { inject } from "@angular/core";
import { Api } from "./core";
const ready = async () => {
  const api = inject(Api);
  if (!api.ready()) await api.session();
  return true;
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
    canActivate: [ready],
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
    canActivate: [ready],
    loadComponent: () => import("./lists").then((m) => m.Lists),
  },
  {
    path: "lists/:id",
    canActivate: [ready],
    loadComponent: () => import("./lists").then((m) => m.Lists),
  },
  {
    path: "public/lists/:id",
    data: { public: true },
    loadComponent: () => import("./lists").then((m) => m.Lists),
  },
  { path: "**", redirectTo: "" },
];
