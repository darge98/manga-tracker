import { Pipe, PipeTransform } from "@angular/core";
// The API keeps MyAnimeList's English publishing status; this is the UI label.
const labels: Record<string, string> = {
  Publishing: "In corso",
  Finished: "Concluso",
  "Not yet published": "Non ancora pubblicato",
  "On hiatus": "In pausa",
  Discontinued: "Interrotto",
};
@Pipe({ name: "status" })
export class StatusPipe implements PipeTransform {
  transform(status: string) {
    return labels[status] ?? status;
  }
}
