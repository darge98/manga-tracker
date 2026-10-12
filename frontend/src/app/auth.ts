import { Component, inject, signal } from "@angular/core";
import { FormsModule } from "@angular/forms";
import { Router } from "@angular/router";
import { Api, User, readableError } from "./core";
@Component({
  imports: [FormsModule],
  template: `<div class="auth-layout">
    <div class="auth-intro">
      <p class="eyebrow">IL TUO DIARIO, SEMPRE CON TE</p>
      <h1>Ogni capitolo<br />lascia un segno<span class="accent">.</span></h1>
      <p>
        Ritrova i manga che ami, conserva le tue impressioni e condividi le tue
        selezioni.
      </p>
      <div class="auth-rule"></div>
      <p class="muted">
        Il catalogo è libero da esplorare. Il tuo diario è personale.
      </p>
    </div>
    <section class="auth-panel">
      <div class="tabs" role="group" aria-label="Accesso account">
        <button [class.selected]="!register" (click)="register = false">
          Accedi</button
        ><button [class.selected]="register" (click)="register = true">
          Registrati
        </button>
      </div>
      <h2>{{ register ? "Fai spazio alle tue storie." : "Bentornato." }}</h2>
      <p>
        {{
          register
            ? "Crea il tuo account e comincia a tenere il segno."
            : "Accedi per riprendere le tue letture."
        }}
      </p>
      <form (ngSubmit)="submit()" #form="ngForm">
        @if (register) {
          <label for="username">Nome utente</label
          ><input
            id="username"
            name="username"
            [(ngModel)]="username"
            required
            minlength="2"
            maxlength="50"
            autocomplete="username"
          />
        }
        <label for="email">Email</label
        ><input
          id="email"
          type="email"
          name="email"
          [(ngModel)]="email"
          required
          email
          autocomplete="email"
        /><label for="password">Password</label
        ><input
          id="password"
          type="password"
          name="password"
          [(ngModel)]="password"
          required
          [minlength]="register ? 10 : 1"
          [attr.autocomplete]="register ? 'new-password' : 'current-password'"
        />
        @if (register) {
          <small>Almeno 10 caratteri.</small>
        }
        @if (error()) {
          <p class="notice error" role="alert">{{ error() }}</p>
        }
        <button
          class="button primary full"
          type="submit"
          [disabled]="form.invalid || busy()"
        >
          {{ busy() ? "Attendi…" : register ? "Crea account" : "Accedi" }}
        </button>
      </form>
    </section>
  </div>`,
})
export class Auth {
  api = inject(Api);
  router = inject(Router);
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
      void this.router.navigateByUrl("/");
    } catch (e) {
      this.error.set(readableError(e));
    } finally {
      this.busy.set(false);
    }
  }
}
