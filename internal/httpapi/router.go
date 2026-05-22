package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/daniil-oliynyk/go-api/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

type ReadinessChecker interface {
	Ping(ctx context.Context) error
}

type MetadataProvider interface {
	GetMetadata(ctx context.Context) (store.Metadata, error)
}

type ListingProvider interface {
	GetListing(ctx context.Context, id string) (store.Listing, error)
}

type Dependencies struct {
	ReadinessChecker ReadinessChecker
	MetadataProvider MetadataProvider
	ListingProvider  ListingProvider
}

func NewRouter(deps Dependencies) http.Handler {
	router := chi.NewRouter()

	router.Use(middleware.RequestID)
	router.Use(middleware.RealIP)
	router.Use(middleware.Recoverer)

	router.Get("/healthz", healthzHandler)
	router.Get("/readyz", readyzHandler(deps.ReadinessChecker))
	router.Get("/api/meta", metaHandler(deps.MetadataProvider))
	router.Get("/api/listings/map", mapListingsHandler)
	router.Get("/api/listings/{id}", listingDetailHandler(deps.ListingProvider))

	return router
}

func healthzHandler(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func readyzHandler(checker ReadinessChecker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if checker == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"status": "unavailable",
				"error":  "readiness checker is not configured",
			})
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		if err := checker.Ping(ctx); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"status": "unavailable",
				"error":  "database is not reachable",
			})
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

type metadataResponse struct {
	TotalListings             int        `json:"totalListings"`
	PropertyTypes             []string   `json:"propertyTypes"`
	LastSuccessfulIngestionAt *time.Time `json:"lastSuccessfulIngestionAt"`
}

func metaHandler(provider MetadataProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if provider == nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{
				"error": "metadata provider is not configured",
			})
			return
		}

		metadata, err := provider.GetMetadata(r.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{
				"error": "failed to load metadata",
			})
			return
		}

		writeJSON(w, http.StatusOK, metadataResponse{
			TotalListings:             metadata.TotalListings,
			PropertyTypes:             metadata.PropertyTypes,
			LastSuccessfulIngestionAt: metadata.LastSuccessfulIngestionAt,
		})
	}
}

type listingResponse struct {
	ID              string     `json:"id"`
	Address         string     `json:"address"`
	PostalCode      string     `json:"postalCode"`
	PropertyType    *string    `json:"propertyType"`
	WardNumber      *string    `json:"wardNumber"`
	WardName        *string    `json:"wardName"`
	Latitude        float64    `json:"latitude"`
	Longitude       float64    `json:"longitude"`
	SourceUpdatedAt *time.Time `json:"sourceUpdatedAt"`
	IngestedAt      time.Time  `json:"ingestedAt"`
	IngestionRunID  string     `json:"ingestionRunId"`
}

func listingDetailHandler(provider ListingProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if provider == nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{
				"error": "listing provider is not configured",
			})
			return
		}

		id := chi.URLParam(r, "id")
		if id == "" {
			writeJSON(w, http.StatusNotFound, map[string]string{
				"error": "listing not found",
			})
			return
		}

		listing, err := provider.GetListing(r.Context(), id)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				writeJSON(w, http.StatusNotFound, map[string]string{
					"error": "listing not found",
				})
				return
			}

			writeJSON(w, http.StatusInternalServerError, map[string]string{
				"error": "failed to load listing",
			})
			return
		}

		writeJSON(w, http.StatusOK, listingResponse{
			ID:              listing.ID,
			Address:         listing.Address,
			PostalCode:      listing.PostalCode,
			PropertyType:    listing.PropertyType,
			WardNumber:      listing.WardNumber,
			WardName:        listing.WardName,
			Latitude:        listing.Latitude,
			Longitude:       listing.Longitude,
			SourceUpdatedAt: listing.SourceUpdatedAt,
			IngestedAt:      listing.IngestedAt,
			IngestionRunID:  listing.IngestionRunID,
		})
	}
}

func mapListingsHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := parseMapQuery(r.URL.Query()); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"type":     "FeatureCollection",
		"features": []any{},
	})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
