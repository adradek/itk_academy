package wallet

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("wallet not found")

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

func (r *Repository) Find(ctx context.Context, id string) (Wallet, error) {
	wallet := Wallet{}
	row := r.pool.QueryRow(ctx,
		"SELECT id::text, balance FROM wallets WHERE id = $1", id)

	err := row.Scan(&wallet.ID, &wallet.Balance)
	if errors.Is(err, pgx.ErrNoRows) {
		return Wallet{}, ErrNotFound
	}
	if err != nil {
		return Wallet{}, fmt.Errorf("wallet search: %w", err)
	}

	return wallet, nil
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
