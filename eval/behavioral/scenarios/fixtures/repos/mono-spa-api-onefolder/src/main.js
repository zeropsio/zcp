const list = document.getElementById("days");
const forecast = await (await fetch("/api/forecast")).json();
list.innerHTML = forecast.days.map((d) => `<li>${d.day}: ${d.tempC} °C</li>`).join("");
