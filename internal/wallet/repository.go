package wallet

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Wallet struct {
	ID      string
	Balance int64
}

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) Create(ctx context.Context) (Wallet, error) {
	wallet := Wallet{}
	row := r.pool.QueryRow(ctx,
		"INSERT INTO wallets (id) VALUES (gen_random_uuid()) RETURNING id::text, balance")

	err := row.Scan(&wallet.ID, &wallet.Balance)
	if err != nil {
		return Wallet{}, fmt.Errorf("wallet creation: %w", err)
	}

	return wallet, nil
}
