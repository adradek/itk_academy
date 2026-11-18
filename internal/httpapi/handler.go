package httpapi

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
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

func NewHandler(repo *wallet.Repository) *Handler {
	return &Handler{repo: repo}
}

func (h *Handler) CreateWallet(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	wallet, err := h.repo.Create(ctx)
	w.Header().Set("Content-Type", "application/json")

	if err != nil {
		log.Printf("create wallet: %v", err)

		w.WriteHeader(http.StatusInternalServerError)

		responsePayload := errorResponse{
			Code:    "INTERNAL_ERROR",
			Message: "Internal server error",
		}
		if err := json.NewEncoder(w).Encode(responsePayload); err != nil {
			log.Printf("write wallet response: %v", err)
		}

		return
	}

	responsePayload := walletResponse{
		WalletID: wallet.ID,
		Balance:  wallet.Balance,
	}
	w.WriteHeader(http.StatusCreated)

	if err := json.NewEncoder(w).Encode(responsePayload); err != nil {
		log.Printf("write wallet response: %v", err)
	}
}
