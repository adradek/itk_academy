//go:build integration

package wallet

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestRepositoryConcurrentBalanceOperations(t *testing.T) {
	tests := []struct {
		name             string
		initialBalance   int64
		deposits         int
		withdrawals      int
		wantSuccess      int
		wantInsufficient int
		wantBalance      int64
	}{
		{name: "deposits", deposits: 200, wantSuccess: 200, wantBalance: 200},
		{name: "withdrawals", initialBalance: 200, withdrawals: 200, wantSuccess: 200, wantBalance: 0},
		{name: "insufficient_funds", initialBalance: 100, withdrawals: 200, wantSuccess: 100, wantInsufficient: 100, wantBalance: 0},
		{name: "mixed_operations", initialBalance: 200, deposits: 100, withdrawals: 100, wantSuccess: 200, wantBalance: 200},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pool := newTestPool(t)
			if pool.Config().MaxConns < 2 {
				t.Fatal("concurrent test requires multiple database connections")
			}

			repo := NewRepository(pool)
			wlt := createTestWallet(t, pool, tc.initialBalance)

			workCtx, cancelWork := context.WithTimeout(context.Background(), timeout)
			defer cancelWork()

			// //
			// //
			// //
			// var method, directIO, fsyncSetting, syncCommit string

			// err := pool.QueryRow(workCtx, `
			// 	SELECT current_setting('wal_sync_method'),
			// 		current_setting('debug_io_direct'),
			// 		current_setting('fsync'),
			// 		current_setting('synchronous_commit')
			// `).Scan(&method, &directIO, &fsyncSetting, &syncCommit)
			// if err != nil {
			// 	t.Fatalf("read database settings: %v", err)
			// }

			// t.Logf(
			// 	"wal_sync_method=%s debug_io_direct=%q fsync=%s synchronous_commit=%s",
			// 	method, directIO, fsyncSetting, syncCommit,
			// )
			// //
			// //
			// //

			total := tc.deposits + tc.withdrawals
			start := make(chan struct{})
			results := make(chan error, total)
			var wg sync.WaitGroup

			for i := 0; i < total; i++ {
				wg.Add(1)
				go func(isDeposit bool) {
					defer wg.Done()
					<-start

					var err error
					if isDeposit {
						_, err = repo.Deposit(workCtx, wlt.ID, 1)
					} else {
						_, err = repo.Withdraw(workCtx, wlt.ID, 1)
					}

					results <- err
				}(i < tc.deposits)
			}

			// close(start)
			// wg.Wait()
			// close(results)
			// cancelWork()

			started := time.Now()
			close(start)
			wg.Wait()
			elapsed := time.Since(started)

			close(results)
			cancelWork()

			t.Logf(
				"======================= >>>>> %d operations in %s: %.0f operations/s",
				total,
				elapsed,
				float64(total)/elapsed.Seconds(),
			)

			var success, insufficient int
			for err := range results {
				switch {
				case err == nil:
					success++
				case errors.Is(err, ErrInsufficientFunds):
					insufficient++
				default:
					t.Errorf("unexpected operation error: %v", err)
				}
			}

			if success != tc.wantSuccess {
				t.Errorf("expected %d successes, got %d", tc.wantSuccess, success)
			}
			if insufficient != tc.wantInsufficient {
				t.Errorf("expected %d insufficient funds errors, got %d", tc.wantInsufficient, insufficient)
			}

			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()

			stored, err := repo.Find(ctx, wlt.ID)
			if err != nil {
				t.Fatalf("find wallet after concurrent operations: %v", err)
			}

			want := Wallet{ID: wlt.ID, Balance: tc.wantBalance}
			if stored != want {
				t.Errorf("expected stored wallet %+v, got %+v", want, stored)
			}
		})
	}
}
