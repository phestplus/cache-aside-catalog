package catalog

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/ekelestephen/cache-aside-catalog/internal/model"
	"github.com/ekelestephen/cache-aside-catalog/internal/store"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Register wires routes onto mux using Go 1.22's method+path pattern
// matching in net/http. A dedicated router library (chi/gorilla) wasn't
// needed for a handful of routes — the stdlib does this natively now.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /products", h.list)
	mux.HandleFunc("POST /products", h.create)
	mux.HandleFunc("GET /products/{id}", h.get)
	mux.HandleFunc("PUT /products/{id}", h.update)
	mux.HandleFunc("GET /healthz", h.healthz)
}

func (h *Handler) healthz(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	products, err := h.svc.ListProducts(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, products)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	p, err := h.svc.GetProduct(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, err)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var in model.CreateProductInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	p, err := h.svc.CreateProduct(r.Context(), in)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var in model.UpdateProductInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	p, err := h.svc.UpdateProduct(r.Context(), id, in)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, err)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}
