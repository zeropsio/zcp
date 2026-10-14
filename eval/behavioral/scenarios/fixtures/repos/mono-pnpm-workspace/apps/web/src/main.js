// Renders the shared forecast; with VITE_API_URL set (built per level from a
// project variable) it also shows what the api answers.
import { forecast } from "@mono/shared";

const city = new URLSearchParams(location.search).get("city") ?? "Praha";
document.querySelector("#days").innerHTML = forecast(city)
  .map((d) => `<li>${d.day}: ${d.tempC} °C</li>`)
  .join("");

const api = import.meta.env.VITE_API_URL;
if (api) {
  fetch(`${api}/forecast?city=${encodeURIComponent(city)}`)
    .then((r) => r.json())
    .then((body) => (document.querySelector("#api").textContent = `api: ${body.days.length} dní pro ${body.city}`))
    .catch(() => (document.querySelector("#api").textContent = "api unreachable"));
}
