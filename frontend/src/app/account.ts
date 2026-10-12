import { Component, inject, signal } from "@angular/core";
import { FormsModule } from "@angular/forms";
import { Router } from "@angular/router";
import { Api, mutate } from "./core";
type Section = "password" | "delete";
@Component({
  imports: [FormsModule],
  templateUrl: "./account.html",
})
export class Account {
  api = inject(Api);
  router = inject(Router);
  busy = signal(false);
  error = signal("");
  message = signal("");
  /** Which form the current error or message belongs to. */
  section = signal<Section>("password");
  loadVersion = 0;
  currentPassword = "";
  newPassword = "";
  confirmPassword = "";
  deletePassword = "";
  async changePassword() {
    this.section.set("password");
    this.message.set("");
    if (this.newPassword !== this.confirmPassword) {
      this.error.set("Le due password nuove non coincidono.");
      return;
    }
    const body = {
      currentPassword: this.currentPassword,
      newPassword: this.newPassword,
    };
    await mutate(this, async (current) => {
      await this.api.request("/auth/password", "PUT", body);
      if (!current()) return;
      this.currentPassword = this.newPassword = this.confirmPassword = "";
      this.message.set(
        "Password aggiornata. Le sessioni aperte su altri dispositivi sono state chiuse.",
      );
    });
  }
  async deleteAccount() {
    this.section.set("delete");
    this.message.set("");
    if (
      this.busy() ||
      !confirm(
        "Eliminare definitivamente l'account? Diario, progressi e liste, anche quelle pubbliche, andranno persi. L'operazione non si può annullare.",
      )
    )
      return;
    const password = this.deletePassword;
    await mutate(this, async () => {
      await this.api.request("/auth/account", "DELETE", { password });
      this.api.setUser(null);
      void this.router.navigateByUrl("/");
    });
  }
}
