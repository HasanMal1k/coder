package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/HasanMal1k/coder/internal/auth/service"
)

type UserHandler struct {
	svc *service.UserService
}

type RegisterRequest struct {
	Email        string `json:"email"`
	Password     string `json:"password"`
	DisplayName  string `json:"display_name"`
	Organization string `json:"organization"`
}

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type AuthResponse struct {
	Token string       `json:"token"`
	User  UserResponse `json:"user"`
}

type UserResponse struct {
	ID           string `json:"id"`
	Email        string `json:"email"`
	DisplayName  string `json:"display_name"`
	Organization string `json:"organization"`
	Subscription string `json:"subscription"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

func NewUserHandler(svc *service.UserService) (h *UserHandler) {
	return &UserHandler{
		svc: svc,
	}
}

func (h *UserHandler) Register(w http.ResponseWriter, r *http.Request) {
	var userData RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&userData); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid request body"})
		return
	}

	token, user, err := h.svc.Register(r.Context(), userData.Email, userData.DisplayName, userData.Password, userData.Organization)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrEmailTaken):
			writeJSON(w, http.StatusConflict, ErrorResponse{Error: "email already registered"})
		default:
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "internal server error"})
		}
		return
	}

	writeJSON(w, http.StatusCreated, AuthResponse{
		Token: token,
		User: UserResponse{
			ID:           user.ID,
			Email:        user.Email,
			DisplayName:  user.DisplayName,
			Organization: user.Organization,
			Subscription: user.Subscription,
		},
	})
}

func (h *UserHandler) Login(w http.ResponseWriter, r *http.Request) {
	var userData LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&userData); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid request body"})
		return
	}

	token, user, err := h.svc.Login(r.Context(), userData.Email, userData.Password)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidCredentials):
			writeJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "invalid email or password"})
		default:
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "internal server error"})
		}

		return
	}

	writeJSON(w, http.StatusOK, AuthResponse{
		Token: token,
		User: UserResponse{
			ID:           user.ID,
			Email:        user.Email,
			DisplayName:  user.DisplayName,
			Organization: user.Organization,
			Subscription: user.Subscription,
		},
	})

}

func (h *UserHandler) Me(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "not authenticated"})
		return
	}

	writeJSON(w, http.StatusOK, UserResponse{
		ID:           user.ID,
		Email:        user.Email,
		DisplayName:  user.DisplayName,
		Organization: user.Organization,
		Subscription: user.Subscription,
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
