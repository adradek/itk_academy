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
