package wallet

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrInvalidAmount = errors.New("invalid amount")
var ErrNotFound = errors.New("wallet not found")
var ErrInsufficientFunds = errors.New("insufficient funds")
var ErrBalanceLimitExceeded = errors.New("balance limit exceeded")

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
	wlt := Wallet{}
	row := r.pool.QueryRow(ctx,
		"SELECT id::text, balance FROM wallets WHERE id = $1", id)

	err := row.Scan(&wlt.ID, &wlt.Balance)
	if errors.Is(err, pgx.ErrNoRows) {
		return Wallet{}, ErrNotFound
	}
	if err != nil {
		return Wallet{}, fmt.Errorf("wlt search: %w", err)
	}

	return wlt, nil
}

func (r *Repository) List(ctx context.Context) ([]Wallet, error) {
	rows, err := r.pool.Query(ctx,
		"SELECT id::text, balance FROM wallets ORDER BY id")
	if err != nil {
		return nil, fmt.Errorf("wallet list query: %w", err)
	}
	defer rows.Close()

	wlts := make([]Wallet, 0)
	for rows.Next() {
		wlt := Wallet{}
		if err := rows.Scan(&wlt.ID, &wlt.Balance); err != nil {
			return nil, fmt.Errorf("wallet list scan: %w", err)
		}
		wlts = append(wlts, wlt)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("wallet list read: %w", err)
	}

	return wlts, nil
}

func (r *Repository) Create(ctx context.Context) (Wallet, error) {
	wlt := Wallet{}
	row := r.pool.QueryRow(ctx,
		"INSERT INTO wallets (id) VALUES (gen_random_uuid()) RETURNING id::text, balance")

	err := row.Scan(&wlt.ID, &wlt.Balance)
	if err != nil {
		return Wallet{}, fmt.Errorf("wallet creation: %w", err)
	}

	return wlt, nil
}

func (r *Repository) Deposit(ctx context.Context, id string, amountCents int64) (Wallet, error) {
	if amountCents <= 0 {
		return Wallet{}, fmt.Errorf("wallet deposit: %w (%d)", ErrInvalidAmount, amountCents)
	}

	wlt := Wallet{}
	row := r.pool.QueryRow(ctx,
		"UPDATE wallets SET balance = balance + $2 WHERE id = $1 RETURNING id::text, balance", id, amountCents)

	err := row.Scan(&wlt.ID, &wlt.Balance)
	if errors.Is(err, pgx.ErrNoRows) {
		return Wallet{}, ErrNotFound
	}
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "22003" {
			return Wallet{}, fmt.Errorf("wallet deposit: %w", ErrBalanceLimitExceeded)
		}
		return Wallet{}, fmt.Errorf("wallet deposit: %w", err)
	}

	return wlt, nil
}

func (r *Repository) Withdraw(ctx context.Context, id string, amountCents int64) (Wallet, error) {
	if amountCents <= 0 {
		return Wallet{}, fmt.Errorf("wallet withdraw: %w (%d)", ErrInvalidAmount, amountCents)
	}

	wlt := Wallet{}
	row := r.pool.QueryRow(ctx,
		"UPDATE wallets SET balance = balance - $2 WHERE id = $1 AND balance >= $2 RETURNING id::text, balance", id, amountCents)

	err := row.Scan(&wlt.ID, &wlt.Balance)
	if errors.Is(err, pgx.ErrNoRows) {
		_, findErr := r.Find(ctx, id)
		if errors.Is(findErr, ErrNotFound) {
			return Wallet{}, ErrNotFound
		}
		if findErr != nil {
			return Wallet{}, fmt.Errorf("wallet withdraw: %w", findErr)
		}
		return Wallet{}, ErrInsufficientFunds
	}
	if err != nil {
		return Wallet{}, fmt.Errorf("wallet withdraw: %w", err)
	}

	return wlt, nil
}
