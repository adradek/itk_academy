package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
)

func main() {
	port, err := strconv.Atoi(os.Getenv("SERVER_PORT"))
	if err != nil || port < 1 || port > 65535 {
		log.Fatal("SERVER_PORT must be an integer between 1 and 65535")
	}

	http.HandleFunc("GET /health", health)

	err = http.ListenAndServe(fmt.Sprintf(":%d", port), nil)
	if err != nil {
		log.Fatalf("wallet api: %s", err)
	}
}

func health(w http.ResponseWriter, r *http.Request) {
	fmt.Fprint(w, "OK")
}
