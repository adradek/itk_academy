//go:build integration

package wallet

import (
	"context"
	"errors"
	"math"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const timeout = 5 * time.Second

func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	for _, name := range []string{
		"POSTGRES_HOST",
		"POSTGRES_USER",
		"POSTGRES_PASSWORD",
		"POSTGRES_DB",
		"POSTGRES_TEST_DB",
	} {
		if os.Getenv(name) == "" {
			t.Fatalf("%s is required", name)
		}
	}

	testDatabase := os.Getenv("POSTGRES_TEST_DB")
	if testDatabase == os.Getenv("POSTGRES_DB") {
		t.Fatal("test database must differ from development database")
	}

	config, err := pgxpool.ParseConfig("sslmode=disable")
	if err != nil {
		t.Fatalf("parse database config: %v", err)
	}

	config.ConnConfig.Host = os.Getenv("POSTGRES_HOST")
	config.ConnConfig.Port = 5432
	config.ConnConfig.User = os.Getenv("POSTGRES_USER")
	config.ConnConfig.Password = os.Getenv("POSTGRES_PASSWORD")
	config.ConnConfig.Database = testDatabase

	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		t.Fatalf("create test pool: %v", err)
	}
	t.Cleanup(pool.Close)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("connect to test database: %v", err)
	}

	// Tests sharing this database must run sequentially.
	if _, err := pool.Exec(ctx, "DELETE FROM wallets"); err != nil {
		t.Fatalf("clean test wallets: %v", err)
	}

	return pool
}

func createTestWallet(t *testing.T, pool *pgxpool.Pool, balance int64) Wallet {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var wlt Wallet
	err := pool.QueryRow(ctx,
		`INSERT INTO wallets (id, balance)
		 VALUES (gen_random_uuid(), $1)
		 RETURNING id::text, balance`,
		balance,
	).Scan(&wlt.ID, &wlt.Balance)
	if err != nil {
		t.Fatalf("prepare test wallet: %v", err)
	}

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()

		if _, err := pool.Exec(ctx,
			"DELETE FROM wallets WHERE id = $1", wlt.ID,
		); err != nil {
			t.Errorf("delete test wallet: %v", err)
		}
	})

	return wlt
}

func TestRepositoryCreateAndFind(t *testing.T) {
	pool := newTestPool(t)
	repo := NewRepository(pool)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	created, err := repo.Create(ctx)
	if err != nil {
		t.Fatalf("create wallet: %v", err)
	}

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()

		_, err := pool.Exec(ctx, "DELETE FROM wallets WHERE id = $1", created.ID)
		if err != nil {
			t.Errorf("delete test wallet: %v", err)
		}
	})

	if created.ID == "" {
		t.Fatal("expected non-empty wallet ID")
	}
	if created.Balance != 0 {
		t.Fatalf("expected initial balance 0, got %d", created.Balance)
	}

	found, err := repo.Find(ctx, created.ID)
	if err != nil {
		t.Fatalf("find wallet: %v", err)
	}

	if found != created {
		t.Errorf("expected wallet %+v, got %+v", created, found)
	}
}

func TestRepositoryBalanceOperations(t *testing.T) {
	pool := newTestPool(t)
	repo := NewRepository(pool)

	tests := []struct {
		name           string
		run            func(context.Context, string, int64) (Wallet, error)
		initialBalance int64
		amount         int64
		wantBalance    int64
		wantErr        error
	}{
		{name: "deposit", run: repo.Deposit, initialBalance: 1000, amount: 500, wantBalance: 1500},
		{name: "withdraw", run: repo.Withdraw, initialBalance: 1000, amount: 400, wantBalance: 600},
		{name: "withdraw_all", run: repo.Withdraw, initialBalance: 1000, amount: 1000, wantBalance: 0},
		{name: "insufficient_funds", run: repo.Withdraw, initialBalance: 1000, amount: 2000, wantBalance: 1000, wantErr: ErrInsufficientFunds},
		{name: "withdraw_from_empty_wallet", run: repo.Withdraw, initialBalance: 0, amount: 1000, wantBalance: 0, wantErr: ErrInsufficientFunds},
		{name: "zero_deposit", run: repo.Deposit, initialBalance: 1000, amount: 0, wantBalance: 1000, wantErr: ErrInvalidAmount},
		{name: "negative_deposit", run: repo.Deposit, initialBalance: 1000, amount: -100, wantBalance: 1000, wantErr: ErrInvalidAmount},
		{name: "zero_withdraw", run: repo.Withdraw, initialBalance: 1000, amount: 0, wantBalance: 1000, wantErr: ErrInvalidAmount},
		{name: "negative_withdraw", run: repo.Withdraw, initialBalance: 1000, amount: -100, wantBalance: 1000, wantErr: ErrInvalidAmount},
		{name: "deposit_to_limit", run: repo.Deposit, initialBalance: math.MaxInt64 - 1, amount: 1, wantBalance: math.MaxInt64},
		{name: "deposit_overflow", run: repo.Deposit, initialBalance: math.MaxInt64, amount: 1, wantBalance: math.MaxInt64, wantErr: ErrBalanceLimitExceeded},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			wlt := createTestWallet(t, pool, tc.initialBalance)
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()

			got, err := tc.run(ctx, wlt.ID, tc.amount)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("expected error %v, got %v", tc.wantErr, err)
			}

			want := Wallet{ID: wlt.ID, Balance: tc.wantBalance}

			if tc.wantErr == nil && got != want {
				t.Errorf("expected returned wallet %+v, got %+v", want, got)
			}

			stored, err := repo.Find(ctx, wlt.ID)
			if err != nil {
				t.Fatalf("find wallet after operation: %v", err)
			}
			if stored != want {
				t.Errorf("expected stored wallet %+v, got %+v", want, stored)
			}
		})
	}
}

func TestRepositoryNotFound(t *testing.T) {
	pool := newTestPool(t)
	repo := NewRepository(pool)

	wlt := createTestWallet(t, pool, 0)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := pool.Exec(ctx,
		"DELETE FROM wallets WHERE id = $1", wlt.ID,
	); err != nil {
		t.Fatalf("prepare missing wallet: %v", err)
	}

	tests := []struct {
		name string
		run  func(context.Context, string) (Wallet, error)
	}{
		{
			name: "find",
			run:  repo.Find,
		},
		{
			name: "deposit",
			run: func(ctx context.Context, id string) (Wallet, error) {
				return repo.Deposit(ctx, id, 1)
			},
		},
		{
			name: "withdraw",
			run: func(ctx context.Context, id string) (Wallet, error) {
				return repo.Withdraw(ctx, id, 1)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.run(ctx, wlt.ID)
			if !errors.Is(err, ErrNotFound) {
				t.Fatalf("expected ErrNotFound, got %v", err)
			}
		})
	}
}

func TestRepositoryList(t *testing.T) {
	pool := newTestPool(t)
	repo := NewRepository(pool)

	tests := []struct {
		name     string
		balances []int64
	}{
		{name: "empty"},
		{name: "several_wallets", balances: []int64{0, 250, 1000}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()

			want := make([]Wallet, 0, len(tc.balances))
			for _, balance := range tc.balances {
				want = append(want, createTestWallet(t, pool, balance))
			}

			sort.Slice(want, func(i, j int) bool {
				return want[i].ID < want[j].ID
			})

			got, err := repo.List(ctx)
			if err != nil {
				t.Fatalf("list wallets: %v", err)
			}
			if got == nil {
				t.Fatal("expected non-nil slice")
			}
			if len(got) != len(want) {
				t.Fatalf("expected %d wallets, got %d", len(want), len(got))
			}

			for i, wlt := range got {
				if wlt != want[i] {
					t.Errorf("wallet %d: expected %+v, got %+v", i, want[i], wlt)
				}
			}
		})
	}
}
