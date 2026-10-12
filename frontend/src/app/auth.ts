import { Component, inject, signal } from "@angular/core";
import { FormsModule } from "@angular/forms";
import { ActivatedRoute, Router } from "@angular/router";
import { Api, User, readableError } from "./core";
@Component({
  imports: [FormsModule],
  templateUrl: "./auth.html",
})
export class Auth {
  api = inject(Api);
  router = inject(Router);
  route = inject(ActivatedRoute);
  register = false;
  username = "";
  email = "";
  password = "";
  busy = signal(false);
  error = signal("");
  async submit() {
    this.busy.set(true);
    this.error.set("");
    try {
      const d = await this.api.request<{ user: User }>(
        "/auth/" + (this.register ? "register" : "login"),
        "POST",
        { username: this.username, email: this.email, password: this.password },
      );
      this.api.setUser(d.user);
      void this.router.navigateByUrl(this.returnUrl());
    } catch (e) {
      this.error.set(readableError(e));
    } finally {
      this.busy.set(false);
    }
  }
  /** Only same-app paths: "//host" or absolute URLs would leave the app. */
  returnUrl() {
    const url = this.route.snapshot.queryParamMap.get("returnUrl") ?? "";
    return /^\/(?![/\\])/.test(url) ? url : "/";
  }
}
