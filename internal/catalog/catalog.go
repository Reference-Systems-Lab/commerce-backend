// Package catalog is the catalog domain: the products customers browse. For now it lists them.
package catalog

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/Reference-Systems-Lab/commerce-backend/internal/catalog/store"
)

// Money is an amount in the currency's minor units (cents for USD), never a float.
type Money struct {
	Amount   int64  `json:"amount" minimum:"0" doc:"Amount in the currency's minor units, for example cents"`
	Currency string `json:"currency" pattern:"^[A-Z]{3}$" doc:"ISO 4217 currency code"`
}

// Product is a product as customers see it.
type Product struct {
	ID    int64  `json:"id"`
	Slug  string `json:"slug" doc:"Stable, URL-safe name"`
	Name  string `json:"name"`
	Price Money  `json:"price" doc:"The price the backend decided; clients display it, never compute it"`
}

// ProductPage is one page of products in id order.
type ProductPage struct {
	Items      []Product `json:"items" nullable:"false"`
	NextCursor *string   `json:"next_cursor" required:"true" nullable:"true" doc:"Pass as cursor for the next page; null on the last page"`
}

type listInput struct {
	Limit  int32  `query:"limit" minimum:"1" maximum:"100" default:"20" doc:"Products per page"`
	Cursor string `query:"cursor" maxLength:"64" doc:"next_cursor from the previous page; omit for the first page"`
}

type listOutput struct {
	Body ProductPage
}

// QueryTimeout bounds the list query, so a slow database fails requests instead of piling them up
// on the connection pool.
const QueryTimeout = 5 * time.Second

// Lister reads products; *store.Queries satisfies it.
type Lister interface {
	ListProducts(ctx context.Context, arg store.ListProductsParams) ([]store.ListProductsRow, error)
}

// Register adds GET /v1/products. q may be nil when only the OpenAPI document is wanted.
func Register(api huma.API, q Lister) {
	huma.Register(api, huma.Operation{
		OperationID: "listProducts",
		Method:      http.MethodGet,
		Path:        "/v1/products",
		Summary:     "List products",
		Description: "Products in id order, a page at a time. Follow next_cursor until it is null.",
		Tags:        []string{"Catalog"},
		Errors:      []int{http.StatusBadRequest},
	}, func(ctx context.Context, in *listInput) (*listOutput, error) {
		after, err := decodeCursor(in.Cursor)
		if err != nil {
			return nil, huma.Error400BadRequest("cursor is malformed")
		}
		ctx, cancel := context.WithTimeout(ctx, QueryTimeout)
		defer cancel()
		rows, err := q.ListProducts(ctx, store.ListProductsParams{
			AfterID:  after,
			RowLimit: in.Limit + 1, // one extra row says whether another page follows
		})
		if err != nil {
			return nil, err // Huma answers 500 without the error's text
		}
		limit := int(in.Limit)
		page := ProductPage{Items: make([]Product, 0, min(len(rows), limit))}
		for i, r := range rows {
			if i == limit {
				next := encodeCursor(page.Items[i-1].ID)
				page.NextCursor = &next
				break
			}
			page.Items = append(page.Items, Product{
				ID:    r.ID,
				Slug:  r.Slug,
				Name:  r.Name,
				Price: Money{Amount: r.PriceAmount, Currency: r.PriceCurrency},
			})
		}
		return &listOutput{Body: page}, nil
	})
}

// A cursor is opaque to clients: base64url of "p:" and the last id seen. The prefix lets a later
// format change reject old cursors cleanly.
const cursorPrefix = "p:"

var errCursor = errors.New("malformed cursor")

func encodeCursor(id int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(cursorPrefix + strconv.FormatInt(id, 10)))
}

func decodeCursor(c string) (int64, error) {
	if c == "" {
		return 0, nil
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(c) // Strict: one string per cursor
	if err != nil {
		return 0, errCursor
	}
	digits, ok := strings.CutPrefix(string(raw), cursorPrefix)
	if !ok {
		return 0, errCursor
	}
	id, err := strconv.ParseInt(digits, 10, 64)
	if err != nil || id < 1 || strconv.FormatInt(id, 10) != digits {
		return 0, errCursor
	}
	return id, nil
}
