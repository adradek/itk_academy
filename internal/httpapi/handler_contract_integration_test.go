//go:build integration

package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"example.com/wallet/internal/wallet"
	"github.com/jackc/pgx/v5/pgxpool"
)

type contractWallet struct {
	WalletID string `json:"walletId"`
	Balance  int64  `json:"balance"`
}

const contractTestTimeout = 5 * time.Second

func TestCreateWalletContract(t *testing.T) {
	pool := newContractTestPool(t)
	mux := newContractTestMux(pool)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/wallets", nil))

	var created contractWallet
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created wallet: %v", err)
	}
	if created.WalletID == "" {
		t.Fatal("expected non-empty wallet ID")
	}
	t.Cleanup(func() { deleteContractWallet(t, pool, created.WalletID) })

	want := contractWallet{WalletID: created.WalletID, Balance: 0}
	assertContractResponse(t, response, http.StatusCreated, want)
	assertContractStoredWallet(t, pool, want)
	assertContractWalletCount(t, pool, 1)
}

func TestGetWalletContract(t *testing.T) {
	pool := newContractTestPool(t)
	mux := newContractTestMux(pool)
	for _, balance := range []int64{0, 1000, math.MaxInt64} {
		t.Run(fmt.Sprintf("balance_%d", balance), func(t *testing.T) {
			want := createContractWallet(t, pool, balance)
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/api/v1/wallets/"+want.WalletID, nil)
			mux.ServeHTTP(response, request)

			assertContractResponse(t, response, http.StatusOK, want)
			assertContractStoredWallet(t, pool, want)
		})
	}
}

func TestGetWalletInvalidUUIDContract(t *testing.T) {
	mux := newContractTestMux(nil)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/wallets/not-a-uuid", nil)
	mux.ServeHTTP(response, request)

	assertContractError(t, response, http.StatusBadRequest, "INVALID_REQUEST")
}

func TestGetWalletsListContract(t *testing.T) {
	pool := newContractTestPool(t)
	mux := newContractTestMux(pool)
	tests := []struct {
		name     string
		balances []int64
	}{
		{name: "empty"},
		{name: "several_wallets", balances: []int64{0, 250, math.MaxInt64}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			want := make([]contractWallet, 0, len(tc.balances))
			for _, balance := range tc.balances {
				want = append(want, createContractWallet(t, pool, balance))
			}
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/wallets", nil))

			var got []contractWallet
			if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
				t.Fatalf("decode wallets list: %v", err)
			}
			if got == nil {
				t.Fatal("expected JSON array, including [] for an empty list, got null")
			}

			assertContractResponse(t, response, http.StatusOK, got)
			sort.Slice(got, func(i, j int) bool { return got[i].WalletID < got[j].WalletID })
			sort.Slice(want, func(i, j int) bool { return want[i].WalletID < want[j].WalletID })
			if !reflect.DeepEqual(got, want) {
				t.Errorf("expected wallets %+v, got %+v", want, got)
			}
		})
	}
}

func TestChangeWalletBalanceContract(t *testing.T) {
	pool := newContractTestPool(t)
	mux := newContractTestMux(pool)
	tests := []struct {
		name           string
		operation      string
		initialBalance int64
		amount         int64
		wantBalance    int64
		wantCode       string
	}{
		{name: "deposit", operation: "DEPOSIT", initialBalance: 1000, amount: 500, wantBalance: 1500},
		{name: "withdraw", operation: "WITHDRAW", initialBalance: 1000, amount: 400, wantBalance: 600},
		{name: "withdraw_all", operation: "WITHDRAW", initialBalance: 1000, amount: 1000, wantBalance: 0},
		{name: "deposit_max_amount", operation: "DEPOSIT", amount: math.MaxInt64, wantBalance: math.MaxInt64},
		{name: "deposit_to_limit", operation: "DEPOSIT", initialBalance: math.MaxInt64 - 1, amount: 1, wantBalance: math.MaxInt64},
		{name: "insufficient_funds", operation: "WITHDRAW", initialBalance: 1000, amount: 1001, wantBalance: 1000, wantCode: "INSUFFICIENT_FUNDS"},
		{name: "withdraw_from_empty_wallet", operation: "WITHDRAW", amount: 1, wantBalance: 0, wantCode: "INSUFFICIENT_FUNDS"},
		{name: "balance_overflow", operation: "DEPOSIT", initialBalance: math.MaxInt64, amount: 1, wantBalance: math.MaxInt64, wantCode: "BALANCE_LIMIT_EXCEEDED"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			wlt := createContractWallet(t, pool, tc.initialBalance)
			body := fmt.Sprintf(`{"walletId":%q,"operationType":%q,"amount":%d}`,
				wlt.WalletID, tc.operation, tc.amount)
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/api/v1/wallet", strings.NewReader(body))
			request.Header.Set("Content-Type", "application/json")
			mux.ServeHTTP(response, request)

			want := contractWallet{WalletID: wlt.WalletID, Balance: tc.wantBalance}
			if tc.wantCode == "" {
				assertContractResponse(t, response, http.StatusOK, want)
			} else {
				assertContractError(t, response, http.StatusConflict, tc.wantCode)
			}
			assertContractStoredWallet(t, pool, want)
		})
	}
}

func TestWalletNotFoundContract(t *testing.T) {
	pool := newContractTestPool(t)
	mux := newContractTestMux(pool)
	untouched := createContractWallet(t, pool, 1000)
	const missingID = "123e4567-e89b-42d3-a456-426614174000"
	tests := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{name: "get", method: http.MethodGet, path: "/api/v1/wallets/" + missingID},
		{name: "deposit", method: http.MethodPost, path: "/api/v1/wallet",
			body: `{"walletId":"` + missingID + `","operationType":"DEPOSIT","amount":1}`},
		{name: "withdraw", method: http.MethodPost, path: "/api/v1/wallet",
			body: `{"walletId":"` + missingID + `","operationType":"WITHDRAW","amount":1}`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			request := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			request.Header.Set("Content-Type", "application/json")
			mux.ServeHTTP(response, request)

			assertContractError(t, response, http.StatusNotFound, "WALLET_NOT_FOUND")
			assertContractStoredWallet(t, pool, untouched)
			assertContractWalletCount(t, pool, 1)
		})
	}
}

func TestWalletInternalErrorContract(t *testing.T) {
	pool := newContractTestPool(t)
	untouched := createContractWallet(t, pool, 1000)

	failedPool, err := pgxpool.NewWithConfig(context.Background(), pool.Config())
	if err != nil {
		t.Fatalf("create failing repository pool: %v", err)
	}
	failedPool.Close()
	mux := newContractTestMux(failedPool)
	tests := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{name: "create", method: http.MethodPost, path: "/api/v1/wallets"},
		{name: "get", method: http.MethodGet, path: "/api/v1/wallets/" + untouched.WalletID},
		{name: "list", method: http.MethodGet, path: "/api/v1/wallets"},
		{name: "deposit", method: http.MethodPost, path: "/api/v1/wallet",
			body: fmt.Sprintf(`{"walletId":%q,"operationType":"DEPOSIT","amount":1}`, untouched.WalletID)},
		{name: "withdraw", method: http.MethodPost, path: "/api/v1/wallet",
			body: fmt.Sprintf(`{"walletId":%q,"operationType":"WITHDRAW","amount":1}`, untouched.WalletID)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			request := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			request.Header.Set("Content-Type", "application/json")
			mux.ServeHTTP(response, request)

			assertContractResponse(t, response, http.StatusInternalServerError, map[string]string{
				"code": "INTERNAL_ERROR", "message": "Internal server error",
			})
			assertContractStoredWallet(t, pool, untouched)
			assertContractWalletCount(t, pool, 1)
		})
	}
}

func newContractTestMux(pool *pgxpool.Pool) *http.ServeMux {
	handler := NewHandler(wallet.NewRepository(pool))
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/wallets", handler.CreateWallet)
	mux.HandleFunc("GET /api/v1/wallets/{WALLET_UUID}", handler.GetWallet)
	mux.HandleFunc("GET /api/v1/wallets", handler.GetWalletsList)
	mux.HandleFunc("POST /api/v1/wallet", handler.ChangeWalletBalance)
	return mux
}

func assertContractResponse(t *testing.T, response *httptest.ResponseRecorder, status int, want any) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("expected status %d, got %d: %s", status, response.Code, response.Body.String())
	}
	if got := response.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", got)
	}
	if !json.Valid(response.Body.Bytes()) {
		t.Fatalf("invalid JSON response: %s", response.Body.String())
	}
	expected, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("encode expected JSON: %v", err)
	}

	var gotJSON, wantJSON any
	for _, item := range []struct {
		body   string
		target *any
	}{
		{response.Body.String(), &gotJSON},
		{string(expected), &wantJSON},
	} {
		decoder := json.NewDecoder(strings.NewReader(item.body))
		decoder.UseNumber()
		if err := decoder.Decode(item.target); err != nil {
			t.Fatalf("decode JSON: %v", err)
		}
	}
	if !reflect.DeepEqual(gotJSON, wantJSON) {
		t.Errorf("expected JSON %s, got %s", expected, response.Body.String())
	}
}

func assertContractError(t *testing.T, response *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	var got struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if strings.TrimSpace(got.Message) == "" {
		t.Error("expected non-empty error message")
	}
	assertContractResponse(t, response, status, map[string]string{"code": code, "message": got.Message})
}

func newContractTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	for _, name := range []string{
		"POSTGRES_HOST", "POSTGRES_USER", "POSTGRES_PASSWORD", "POSTGRES_DB", "POSTGRES_TEST_DB",
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
		t.Fatalf("parse test database config: %v", err)
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
	ctx, cancel := context.WithTimeout(context.Background(), contractTestTimeout)
	defer cancel()
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("connect to test database: %v", err)
	}

	if _, err := pool.Exec(ctx, "DELETE FROM wallets"); err != nil {
		t.Fatalf("clean test wallets: %v", err)
	}
	return pool
}

func createContractWallet(t *testing.T, pool *pgxpool.Pool, balance int64) contractWallet {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), contractTestTimeout)
	defer cancel()
	var wlt contractWallet
	err := pool.QueryRow(ctx,
		"INSERT INTO wallets (id, balance) VALUES (gen_random_uuid(), $1) RETURNING id::text, balance",
		balance,
	).Scan(&wlt.WalletID, &wlt.Balance)
	if err != nil {
		t.Fatalf("prepare test wallet: %v", err)
	}
	t.Cleanup(func() { deleteContractWallet(t, pool, wlt.WalletID) })
	return wlt
}

func deleteContractWallet(t *testing.T, pool *pgxpool.Pool, id string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), contractTestTimeout)
	defer cancel()
	if _, err := pool.Exec(ctx, "DELETE FROM wallets WHERE id = $1", id); err != nil {
		t.Errorf("delete test wallet: %v", err)
	}
}

func assertContractStoredWallet(t *testing.T, pool *pgxpool.Pool, want contractWallet) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), contractTestTimeout)
	defer cancel()
	var balance int64
	if err := pool.QueryRow(ctx, "SELECT balance FROM wallets WHERE id = $1", want.WalletID).Scan(&balance); err != nil {
		t.Fatalf("read stored wallet: %v", err)
	}
	if balance != want.Balance {
		t.Errorf("expected stored balance %d, got %d", want.Balance, balance)
	}
}

func assertContractWalletCount(t *testing.T, pool *pgxpool.Pool, want int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), contractTestTimeout)
	defer cancel()
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM wallets").Scan(&count); err != nil {
		t.Fatalf("count stored wallets: %v", err)
	}
	if count != want {
		t.Errorf("expected %d stored wallets, got %d", want, count)
	}
}
