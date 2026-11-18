package main

import (
	"fmt"
	"log"
	"net/http"
)

func main() {
	http.HandleFunc("GET /health", health)

	err := http.ListenAndServe(":8000", nil)
	if err != nil {
		log.Fatalf("wallet api: %s", err)
	}
}

func health(w http.ResponseWriter, r *http.Request) {
	fmt.Fprint(w, "OK")
}
