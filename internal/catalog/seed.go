package catalog

import (
	"context"
	"fmt"

	"github.com/Reference-Systems-Lab/commerce-backend/internal/catalog/store"
)

// seedProducts are the fixed development products. Prices are in cents.
var seedProducts = []store.InsertProductIfMissingParams{
	{Slug: "canvas-tote", Name: "Canvas Tote", PriceAmount: 2400, PriceCurrency: "USD"},
	{Slug: "ceramic-mug", Name: "Ceramic Mug", PriceAmount: 1800, PriceCurrency: "USD"},
	{Slug: "linen-apron", Name: "Linen Apron", PriceAmount: 3600, PriceCurrency: "USD"},
	{Slug: "oak-cutting-board", Name: "Oak Cutting Board", PriceAmount: 5200, PriceCurrency: "USD"},
	{Slug: "wool-throw", Name: "Wool Throw", PriceAmount: 8900, PriceCurrency: "USD"},
	{Slug: "beeswax-candle", Name: "Beeswax Candle", PriceAmount: 1500, PriceCurrency: "USD"},
}

// Seeder writes products; *store.Queries satisfies it.
type Seeder interface {
	UpdateProduct(ctx context.Context, arg store.UpdateProductParams) (int64, error)
	InsertProductIfMissing(ctx context.Context, arg store.InsertProductIfMissingParams) error
}

// Seed upserts the development products by slug. Running it again writes nothing and uses up no ids.
func Seed(ctx context.Context, q Seeder) error {
	for _, p := range seedProducts {
		if _, err := q.UpdateProduct(ctx, store.UpdateProductParams{
			Name: p.Name, PriceAmount: p.PriceAmount, PriceCurrency: p.PriceCurrency, Slug: p.Slug,
		}); err != nil {
			return fmt.Errorf("seed %s: %w", p.Slug, err)
		}
		if err := q.InsertProductIfMissing(ctx, p); err != nil {
			return fmt.Errorf("seed %s: %w", p.Slug, err)
		}
	}
	return nil
}
