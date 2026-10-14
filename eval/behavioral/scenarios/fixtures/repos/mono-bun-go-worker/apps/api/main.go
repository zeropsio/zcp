// Počasí api: the forecast as JSON, standard library only.
package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
)

type day struct {
	Day   string `json:"day"`
	TempC int    `json:"tempC"`
}

var days = []string{"Po", "Út", "St", "Čt", "Pá", "So", "Ne"}

func forecast(city string) []day {
	seed := 0
	for _, r := range city {
		seed += int(r)
	}
	out := make([]day, len(days))
	for i, d := range days {
		out[i] = day{Day: d, TempC: (seed+i*7)%25 - 3}
	}
	return out
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	http.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
	http.HandleFunc("/forecast", func(w http.ResponseWriter, r *http.Request) {
		city := r.URL.Query().Get("city")
		if city == "" {
			city = "Praha"
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"city": city, "days": forecast(city)})
	})
	log.Printf("api listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
