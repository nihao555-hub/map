package intel_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gosom/google-maps-scraper/enrich"
	"github.com/gosom/google-maps-scraper/enrich/intel"
)

func TestGLEIFLookupViaMockAPI(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/lei-records") && r.URL.RawQuery != "":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{{
					"id": "HWUPKR0MPOU8FGXBT394",
					"attributes": map[string]any{
						"lei": "HWUPKR0MPOU8FGXBT394",
						"entity": map[string]any{
							"legalName":    map[string]any{"name": "Apple Inc."},
							"status":       "ACTIVE",
							"jurisdiction": "US-CA",
							"category":     "GENERAL",
							"creationDate": "1977-01-03T00:00:00Z",
							"legalAddress": map[string]any{"city": "Cupertino", "country": "US"},
						},
					},
				}},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	intel.SetGLEIFBaseURL(srv.URL + "/api/v1")
	t.Cleanup(func() { intel.SetGLEIFBaseURL("https://api.gleif.org/api/v1") })

	opts := intel.DefaultOptions()
	opts.DomainIntel = false
	opts.VerifyEmails = false
	opts.NormalizePhones = false
	opts.TechFingerprint = false
	opts.HTTPClient = srv.Client()

	profile := &enrich.CompanyProfile{LegalName: "Apple Inc."}
	intel.New(opts).Enrich(context.Background(), profile, nil)

	require.NotNil(t, profile.LegalEntity)
	assert.Equal(t, "HWUPKR0MPOU8FGXBT394", profile.LEI)
	assert.Equal(t, "Apple Inc.", profile.LegalEntity.LegalName)
	assert.Equal(t, "ACTIVE", profile.LegalEntity.Status)
	assert.Equal(t, "US-CA", profile.LegalEntity.Jurisdiction)
}
