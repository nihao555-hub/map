// Package geocode resolves a free text place name into a geographic bounding
// box using the OpenStreetMap Nominatim service.
package geocode

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gosom/google-maps-scraper/grid"
)

const (
	defaultEndpoint = "https://nominatim.openstreetmap.org/search"
	userAgent       = "google-maps-scraper/1.0 (full coverage grid)"
	requestTimeout  = 20 * time.Second
)

// Client looks up bounding boxes for place names.
type Client struct {
	Endpoint string
	HTTP     *http.Client
}

// New returns a client with sane defaults.
func New() *Client {
	return &Client{
		Endpoint: defaultEndpoint,
		HTTP:     &http.Client{Timeout: requestTimeout},
	}
}

type nominatimResult struct {
	BoundingBox []string `json:"boundingbox"`
}

// BoundingBox returns the bounding box of the best match for place.
func (c *Client) BoundingBox(ctx context.Context, place string) (grid.BoundingBox, error) {
	place = strings.TrimSpace(place)
	if place == "" {
		return grid.BoundingBox{}, fmt.Errorf("empty place")
	}

	endpoint := c.Endpoint
	if endpoint == "" {
		endpoint = defaultEndpoint
	}

	params := url.Values{}
	params.Set("q", place)
	params.Set("format", "json")
	params.Set("limit", "1")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+params.Encode(), nil)
	if err != nil {
		return grid.BoundingBox{}, err
	}

	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")

	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = &http.Client{Timeout: requestTimeout}
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return grid.BoundingBox{}, err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return grid.BoundingBox{}, fmt.Errorf("geocoding %q failed with status %d", place, resp.StatusCode)
	}

	var results []nominatimResult

	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return grid.BoundingBox{}, err
	}

	if len(results) == 0 || len(results[0].BoundingBox) != 4 {
		return grid.BoundingBox{}, fmt.Errorf("no geocoding result for %q", place)
	}

	// Nominatim returns [minLat, maxLat, minLon, maxLon] as strings.
	vals := make([]float64, 4)

	for i, raw := range results[0].BoundingBox {
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return grid.BoundingBox{}, fmt.Errorf("invalid bounding box value %q: %w", raw, err)
		}

		vals[i] = v
	}

	bbox := grid.BoundingBox{
		MinLat: vals[0],
		MaxLat: vals[1],
		MinLon: vals[2],
		MaxLon: vals[3],
	}

	if bbox.MinLat >= bbox.MaxLat || bbox.MinLon >= bbox.MaxLon {
		return grid.BoundingBox{}, fmt.Errorf("invalid bounding box for %q", place)
	}

	return bbox, nil
}
