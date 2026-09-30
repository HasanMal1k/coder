package handler

import "github.com/go-chi/chi/v5"

func (h *UserHandler) Routes() chi.Router {
	r := chi.NewRouter()

	r.Post("/register", h.Register)
	r.Post("/login", h.Login)

	r.Group(func(r chi.Router) {
		r.Use(h.RequireAuth)
		r.Get("/me", h.Me)
	})

	return r
}
