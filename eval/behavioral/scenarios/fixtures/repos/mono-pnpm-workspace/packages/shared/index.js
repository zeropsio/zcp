// Shared by web and api: the computed forecast (no network, no dependencies).
const days = ["Po", "Út", "St", "Čt", "Pá", "So", "Ne"];

export function forecast(city) {
  const seed = [...city].reduce((sum, ch) => sum + ch.charCodeAt(0), 0);
  return days.map((day, i) => ({ day, tempC: ((seed + i * 7) % 25) - 3 }));
}
