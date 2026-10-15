const reply = await (await fetch("/api/hello")).json();
document.getElementById("server").textContent = `server says ${reply.hello}`;
