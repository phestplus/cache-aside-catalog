// Package model holds the domain types shared between the store and
// catalog packages. It exists purely to avoid an import cycle: store needs
// Product to define its interface, and catalog needs both store and Product.
package model

import "time"

type Product struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	PriceCents  int64     `json:"price_cents"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type CreateProductInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	PriceCents  int64  `json:"price_cents"`
}

type UpdateProductInput struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
	PriceCents  *int64  `json:"price_cents,omitempty"`
}
