package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestChangeWalletBalanceInvalidRequest(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"invalid_json", "{"},
		{"empty_body", ""},
		{"unknown_field", `{"walletId":"123e4567-e89b-42d3-a456-426614174000","operationType":"DEPOSIT","amount":1000,"extra":true}`},
		{"missing_wallet_id", `{"operationType":"DEPOSIT","amount":1000}`},
		{"invalid_wallet_id", `{"walletId":"invalid","operationType":"DEPOSIT","amount":1000}`},
		{"missing_operation", `{"walletId":"123e4567-e89b-42d3-a456-426614174000","amount":1000}`},
		{"invalid_operation", `{"walletId":"123e4567-e89b-42d3-a456-426614174000","operationType":"TRANSFER","amount":1000}`},
		{"missing_amount", `{"walletId":"123e4567-e89b-42d3-a456-426614174000","operationType":"DEPOSIT"}`},
		{"zero_amount", `{"walletId":"123e4567-e89b-42d3-a456-426614174000","operationType":"DEPOSIT","amount":0}`},
		{"negative_amount", `{"walletId":"123e4567-e89b-42d3-a456-426614174000","operationType":"DEPOSIT","amount":-1}`},
		{"fractional_amount", `{"walletId":"123e4567-e89b-42d3-a456-426614174000","operationType":"DEPOSIT","amount":1.5}`},
		{"string_amount", `{"walletId":"123e4567-e89b-42d3-a456-426614174000","operationType":"DEPOSIT","amount":"1000"}`},
		{"overflow_amount", `{"walletId":"123e4567-e89b-42d3-a456-426614174000","operationType":"DEPOSIT","amount":9223372036854775808}`},
	}

	handler := NewHandler(nil)

	type responsePayload struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(
				http.MethodPost,
				"/api/v1/wallet",
				strings.NewReader(tc.body),
			)
			response := httptest.NewRecorder()
			handler.ChangeWalletBalance(response, request)

			if response.Code != http.StatusBadRequest {
				t.Fatalf("expected status %d, got %d",
					http.StatusBadRequest, response.Code)
			}

			if got := response.Header().Get("Content-Type"); got != "application/json" {
				t.Errorf("expected Content-Type application/json, got %q", got)
			}

			var payload responsePayload

			if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
				t.Fatalf("decode error response: %v", err)
			}
			if payload.Code != "INVALID_REQUEST" {
				t.Errorf("expected code INVALID_REQUEST, got %q", payload.Code)
			}
			if payload.Message == "" {
				t.Error("expected non-empty error message")
			}
		})
	}
}
