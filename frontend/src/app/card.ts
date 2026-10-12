import { Component, input } from "@angular/core";
import { RouterLink } from "@angular/router";
import { Manga } from "./core";
@Component({
  selector: "manga-card",
  imports: [RouterLink],
  template: `<a class="manga-card" [routerLink]="['/manga', manga().id]"
    ><div class="cover">
      <img
        [src]="manga().coverUrl"
        [alt]="'Cover of ' + manga().title"
        loading="lazy"
        (error)="hideImage($event)"
      />
      @if (manga().score) {
        <span class="score">{{ manga().score }} / 10</span>
      }
    </div>
    <h3>{{ manga().title }}</h3>
    <p>
      {{ manga().genres.slice(0, 2).join(" · ") || manga().status }}
    </p></a
  >`,
})
export class MangaCard {
  manga = input.required<Manga>();
  hideImage(event: Event) {
    (event.target as HTMLImageElement).style.display = "none";
  }
}
