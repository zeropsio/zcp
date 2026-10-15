"""Počasí api: the forecast as JSON, standard library only."""
import json
import os
from http.server import BaseHTTPRequestHandler, HTTPServer

DAYS = ["Po", "Út", "St", "Čt", "Pá", "So", "Ne"]


def forecast(city: str):
    seed = sum(ord(ch) for ch in city)
    return [{"day": d, "tempC": (seed + i * 7) % 25 - 3} for i, d in enumerate(DAYS)]


class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path == "/health":
            body, kind = b"ok", "text/plain"
        elif self.path.startswith("/forecast"):
            body, kind = json.dumps({"city": "Praha", "days": forecast("Praha")}).encode(), "application/json"
        else:
            self.send_response(404)
            self.end_headers()
            return
        self.send_response(200)
        self.send_header("content-type", kind)
        self.end_headers()
        self.wfile.write(body)


if __name__ == "__main__":
    port = int(os.environ.get("PORT", "8080"))
    HTTPServer(("0.0.0.0", port), Handler).serve_forever()
