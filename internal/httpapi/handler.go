package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"time"

	"example.com/wallet/internal/wallet"
)

type walletResponse struct {
	WalletID string `json:"walletId"`
	Balance  int64  `json:"balance"`
}

type walletOperationRequest struct {
	WalletID      string `json:"walletId"`
	OperationType string `json:"operationType"`
	Amount        int64  `json:"amount"`
}

type errorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Handler struct {
	repo *wallet.Repository
}

const timeout = time.Second * 5

var uuidPattern = regexp.MustCompile(
	`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`,
)

func NewHandler(repo *wallet.Repository) *Handler {
	return &Handler{repo: repo}
}

func (h *Handler) CreateWallet(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	created, err := h.repo.Create(ctx)
	w.Header().Set("Content-Type", "application/json")

	if err != nil {
		log.Printf("create wallet: %v", err)
		message := "Internal server error"
		respondWithError(w, message, "INTERNAL_ERROR")
		return
	}

	responsePayload := walletResponse{
		WalletID: created.ID,
		Balance:  created.Balance,
	}
	w.WriteHeader(http.StatusCreated)

	if err := json.NewEncoder(w).Encode(responsePayload); err != nil {
		log.Printf("write wallet response: %v", err)
	}
}

func (h *Handler) GetWallet(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	w.Header().Set("Content-Type", "application/json")

	uuid := r.PathValue("WALLET_UUID")
	if !validUUID(uuid) {
		message := fmt.Sprintf("getting wallet: wrong uuid format (%q)", uuid)
		respondWithError(w, message, "INVALID_REQUEST")
		return
	}

	found, err := h.repo.Find(ctx, uuid)
	if errors.Is(err, wallet.ErrNotFound) {
		message := fmt.Sprintf("getting wallet: %v", err)
		respondWithError(w, message, "WALLET_NOT_FOUND")
		return
	}

	if err != nil {
		log.Printf("getting wallet: %v", err)
		message := "Internal server error"
		respondWithError(w, message, "INTERNAL_ERROR")
		return
	}

	responsePayload := walletResponse{
		WalletID: found.ID,
		Balance:  found.Balance,
	}
	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(responsePayload); err != nil {
		log.Printf("write wallet response: %v", err)
	}
}

func (h *Handler) ChangeWalletBalance(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var request walletOperationRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		respondWithError(w, "Invalid JSON request", "INVALID_REQUEST")
		return
	}
	if !validUUID(request.WalletID) {
		respondWithError(w, "Invalid wallet UUID", "INVALID_REQUEST")
		return
	}
	if request.Amount <= 0 {
		respondWithError(w, "Amount must be positive", "INVALID_REQUEST")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	var updated wallet.Wallet
	var err error
	switch request.OperationType {
	case "DEPOSIT":
		updated, err = h.repo.Deposit(ctx, request.WalletID, request.Amount)
	case "WITHDRAW":
		updated, err = h.repo.Withdraw(ctx, request.WalletID, request.Amount)
	default:
		respondWithError(w, "Operation type must be DEPOSIT or WITHDRAW", "INVALID_REQUEST")
		return
	}

	if err != nil {
		switch {
		case errors.Is(err, wallet.ErrNotFound):
			respondWithError(w, "Wallet not found", "WALLET_NOT_FOUND")
		case errors.Is(err, wallet.ErrInvalidAmount):
			respondWithError(w, "Amount must be positive", "INVALID_REQUEST")
		case errors.Is(err, wallet.ErrInsufficientFunds):
			respondWithError(w, "Insufficient funds", "INSUFFICIENT_FUNDS")
		case errors.Is(err, wallet.ErrBalanceLimitExceeded):
			respondWithError(w, "Balance limit exceeded", "BALANCE_LIMIT_EXCEEDED")
		default:
			log.Printf("change wallet balance: %v", err)
			respondWithError(w, "Internal server error", "INTERNAL_ERROR")
		}
		return
	}

	responsePayload := walletResponse{
		WalletID: updated.ID,
		Balance:  updated.Balance,
	}
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(responsePayload); err != nil {
		log.Printf("write wallet response: %v", err)
	}
}

func validUUID(uuid string) bool {
	return uuidPattern.MatchString(uuid)
}

func respondWithError(w http.ResponseWriter, message string, code string) {
	log.Print(message)

	switch code {
	case "INVALID_REQUEST":
		w.WriteHeader(http.StatusBadRequest)
	case "WALLET_NOT_FOUND":
		w.WriteHeader(http.StatusNotFound)
	case "INSUFFICIENT_FUNDS", "BALANCE_LIMIT_EXCEEDED":
		w.WriteHeader(http.StatusConflict)
	case "INTERNAL_ERROR":
		w.WriteHeader(http.StatusInternalServerError)
	}

	responsePayload := errorResponse{
		Code:    code,
		Message: message,
	}

	if err := json.NewEncoder(w).Encode(responsePayload); err != nil {
		log.Printf("write wallet response: %v", err)
	}
}
