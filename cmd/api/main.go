package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"example.com/wallet/internal/httpapi"
	"example.com/wallet/internal/wallet"
)

func main() {
	port, err := strconv.Atoi(os.Getenv("SERVER_PORT"))
	if err != nil || port < 1 || port > 65535 {
		log.Fatal("wallet api: SERVER_PORT must be an integer between 1 and 65535")
	}

	config, err := parseConfig()
	if err != nil {
		log.Fatalf("wallet api: invalid PostgreSQL configuration (%s)", err)
	}

	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		log.Fatalf("wallet api: cannot create PostgreSQL pool (%s)", err)
	}
	defer pool.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = pool.Ping(ctx)
	if err != nil {
		log.Fatalf("wallet api: %s", err)
	}

	repo := wallet.NewRepository(pool)
	handler := httpapi.NewHandler(repo)

	http.HandleFunc("GET /api/v1/wallets/{WALLET_UUID}", handler.GetWallet)
	http.HandleFunc("POST /api/v1/wallets", handler.CreateWallet)
	http.HandleFunc("GET /health", health)

	err = http.ListenAndServe(fmt.Sprintf(":%d", port), nil)
	if err != nil {
		log.Fatalf("wallet api: %s", err)
	}
}

func parseConfig() (*pgxpool.Config, error) {
	envVars := []string{"POSTGRES_HOST", "POSTGRES_USER", "POSTGRES_PASSWORD", "POSTGRES_DB"}
	configItems := make(map[string]string)

	for _, envVar := range envVars {
		value := os.Getenv(envVar)
		if value == "" {
			return nil, fmt.Errorf("%s is required", envVar)
		}

		configItems[envVar] = value
	}

	config, err := pgxpool.ParseConfig("sslmode=disable")
	if err != nil {
		return nil, err
	}

	config.ConnConfig.Host = configItems["POSTGRES_HOST"]
	config.ConnConfig.Port = 5432
	config.ConnConfig.User = configItems["POSTGRES_USER"]
	config.ConnConfig.Password = configItems["POSTGRES_PASSWORD"]
	config.ConnConfig.Database = configItems["POSTGRES_DB"]

	return config, nil
}

func health(w http.ResponseWriter, r *http.Request) {
	fmt.Fprint(w, "OK")
}
