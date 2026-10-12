import { Component, inject } from "@angular/core";
import {
  RouterLink,
  RouterLinkActive,
  RouterOutlet,
  Router,
} from "@angular/router";
import { Api } from "./core";
@Component({
  selector: "app-root",
  imports: [RouterLink, RouterLinkActive, RouterOutlet],
  templateUrl: "./app.html",
})
export class App {
  api = inject(Api);
  router = inject(Router);
  constructor() {
    void this.api.session();
  }
  async logout() {
    try {
      await this.api.logout();
      void this.router.navigateByUrl("/");
    } catch {
      alert("Uscita non riuscita. Riprova.");
    }
  }
}
