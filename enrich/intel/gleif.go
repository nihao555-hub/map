package intel

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/gosom/google-maps-scraper/enrich"
)

var gleifBaseURL = "https://api.gleif.org/api/v1"

// SetGLEIFBaseURL overrides the GLEIF API host. Intended for tests.
func SetGLEIFBaseURL(base string) {
	if base != "" {
		gleifBaseURL = strings.TrimRight(base, "/")
	}
}

// lookupGLEIF resolves a company name to a LEI record and, when reported,
// its direct and ultimate parents. GLEIF data is CC0 — no API key required.
func lookupGLEIF(ctx context.Context, client *http.Client, name string) (
	entity, direct, ultimate *enrich.LegalEntity,
) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, nil, nil
	}

	entity = searchGLEIF(ctx, client, name)
	if entity == nil {
		return nil, nil, nil
	}

	direct = fetchGLEIFRelated(ctx, client, entity.LEI, "direct-parent")
	ultimate = fetchGLEIFRelated(ctx, client, entity.LEI, "ultimate-parent")

	return entity, direct, ultimate
}

func searchGLEIF(ctx context.Context, client *http.Client, name string) *enrich.LegalEntity {
	endpoint := gleifBaseURL + "/lei-records?" + url.Values{
		"filter[entity.legalName]": {name},
		"page[size]":               {"5"},
	}.Encode()

	body, err := gleifGET(ctx, client, endpoint)
	if err != nil {
		return nil
	}

	var payload gleifListResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil
	}

	if len(payload.Data) == 0 {
		// Fall back to full-text search when the exact legal-name filter misses.
		endpoint = gleifBaseURL + "/lei-records?" + url.Values{
			"filter[fulltext]": {name},
			"page[size]":       {"5"},
		}.Encode()

		body, err = gleifGET(ctx, client, endpoint)
		if err != nil {
			return nil
		}

		if err := json.Unmarshal(body, &payload); err != nil || len(payload.Data) == 0 {
			return nil
		}
	}

	best := pickBestGLEIF(payload.Data, name)

	return best.toLegalEntity()
}

func fetchGLEIFRelated(ctx context.Context, client *http.Client, lei, relation string) *enrich.LegalEntity {
	endpoint := fmt.Sprintf("%s/lei-records/%s/%s", gleifBaseURL, url.PathEscape(lei), relation)

	body, err := gleifGET(ctx, client, endpoint)
	if err != nil {
		return nil
	}

	// Parent endpoints return either a single record or a list; try both.
	var single gleifSingleResponse
	if err := json.Unmarshal(body, &single); err == nil && single.Data.ID != "" {
		return single.Data.toLegalEntity()
	}

	var list gleifListResponse
	if err := json.Unmarshal(body, &list); err == nil && len(list.Data) > 0 {
		return list.Data[0].toLegalEntity()
	}

	return nil
}

func gleifGET(ctx context.Context, client *http.Client, endpoint string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/vnd.api+json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("gleif: not found")
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("gleif: status %d", resp.StatusCode)
	}

	return io.ReadAll(io.LimitReader(resp.Body, 2<<20))
}

func pickBestGLEIF(records []gleifRecord, name string) gleifRecord {
	lower := strings.ToLower(name)

	for _, r := range records {
		if strings.EqualFold(r.Attributes.Entity.LegalName.Name, name) {
			return r
		}
	}

	for _, r := range records {
		if strings.Contains(strings.ToLower(r.Attributes.Entity.LegalName.Name), lower) {
			return r
		}
	}

	return records[0]
}

type gleifListResponse struct {
	Data []gleifRecord `json:"data"`
}

type gleifSingleResponse struct {
	Data gleifRecord `json:"data"`
}

type gleifRecord struct {
	ID         string `json:"id"`
	Attributes struct {
		LEI    string `json:"lei"`
		Entity struct {
			LegalName struct {
				Name string `json:"name"`
			} `json:"legalName"`
			Status              string       `json:"status"`
			Jurisdiction        string       `json:"jurisdiction"`
			Category            string       `json:"category"`
			CreationDate        string       `json:"creationDate"`
			LegalAddress        gleifAddress `json:"legalAddress"`
			HeadquartersAddress gleifAddress `json:"headquartersAddress"`
		} `json:"entity"`
	} `json:"attributes"`
}

type gleifAddress struct {
	City    string `json:"city"`
	Country string `json:"country"`
}

func (r gleifRecord) toLegalEntity() *enrich.LegalEntity {
	lei := r.Attributes.LEI
	if lei == "" {
		lei = r.ID
	}

	if lei == "" {
		return nil
	}

	city := r.Attributes.Entity.HeadquartersAddress.City
	if city == "" {
		city = r.Attributes.Entity.LegalAddress.City
	}

	country := r.Attributes.Entity.HeadquartersAddress.Country
	if country == "" {
		country = r.Attributes.Entity.LegalAddress.Country
	}

	creation := r.Attributes.Entity.CreationDate
	if len(creation) >= 10 {
		creation = creation[:10]
	}

	return &enrich.LegalEntity{
		LEI:          lei,
		LegalName:    r.Attributes.Entity.LegalName.Name,
		Status:       r.Attributes.Entity.Status,
		Jurisdiction: r.Attributes.Entity.Jurisdiction,
		Country:      country,
		City:         city,
		Category:     r.Attributes.Entity.Category,
		CreationDate: creation,
	}
}
