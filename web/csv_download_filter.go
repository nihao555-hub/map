package web

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
)

// filterCSVBytesForJob rewrites a scraped CSV so the download matches the API:
// relevance + radius + StablePlaceKey dedup. Original columns are preserved;
// when duplicates collapse, the richer contact row wins.
func filterCSVBytesForJob(raw []byte, data JobData) ([]byte, error) {
	raw = bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF})
	reader := csv.NewReader(bytes.NewReader(raw))
	reader.FieldsPerRecord = -1
	reader.LazyQuotes = true

	header, err := reader.Read()
	if err != nil {
		if err == io.EOF {
			return raw, nil
		}
		return nil, err
	}

	col := make(map[string]int, len(header))
	for i, name := range header {
		col[name] = i
	}
	get := func(row []string, name string) string {
		idx, ok := col[name]
		if !ok || idx >= len(row) {
			return ""
		}
		return row[idx]
	}

	type rowSlot struct {
		row   []string
		place Place
	}
	var slots []rowSlot
	for {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		slots = append(slots, rowSlot{row: row, place: placeFromCSVRow(get, row)})
	}

	places := make([]Place, len(slots))
	for i := range slots {
		places[i] = slots[i].place
	}
	kept := FilterPlacesForJob(places, data)
	keepKey := make(map[string]struct{}, len(kept))
	for _, p := range kept {
		keepKey[StablePlaceKey(p)] = struct{}{}
	}

	best := make(map[string][]string, len(keepKey))
	order := make([]string, 0, len(keepKey))
	for _, slot := range slots {
		key := StablePlaceKey(slot.place)
		if _, ok := keepKey[key]; !ok {
			continue
		}
		prev, seen := best[key]
		if !seen {
			best[key] = slot.row
			order = append(order, key)
			continue
		}
		if placeBetter(slot.place, placeFromCSVRow(get, prev)) {
			best[key] = slot.row
		}
	}

	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	if err := w.Write(header); err != nil {
		return nil, err
	}
	for _, key := range order {
		row := best[key]
		out := make([]string, len(header))
		copy(out, row)
		if err := w.Write(out); err != nil {
			return nil, err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func placeFromCSVRow(get func([]string, string) string, row []string) Place {
	lat, _ := strconv.ParseFloat(get(row, "latitude"), 64)
	lon, _ := strconv.ParseFloat(get(row, "longitude"), 64)
	if lon == 0 {
		if alt, err := strconv.ParseFloat(get(row, "longtitude"), 64); err == nil {
			lon = alt
		}
	}
	rating, _ := strconv.ParseFloat(get(row, "review_rating"), 64)
	reviewCount, _ := strconv.Atoi(get(row, "review_count"))
	p := Place{
		Title:           get(row, "title"),
		Category:        get(row, "category"),
		Address:         get(row, "address"),
		CompleteAddress: get(row, "complete_address"),
		Latitude:        lat,
		Longitude:       lon,
		Link:            get(row, "link"),
		Phone:           get(row, "phone"),
		Website:         get(row, "website"),
		ReviewRating:    rating,
		ReviewCount:     reviewCount,
		Emails:          get(row, "emails"),
		WhatsApp:        get(row, "whatsapp"),
		Facebook:        get(row, "facebook"),
		Instagram:       get(row, "instagram"),
		LinkedIn:        get(row, "linkedin"),
		Descriptions:    get(row, "descriptions"),
		About:           get(row, "about"),
		PlaceID:         get(row, "place_id"),
		Cid:             get(row, "cid"),
		DataID:          get(row, "data_id"),
	}
	ensurePlaceKey(&p)
	return p
}

func writeFilteredCSVDownload(w io.Writer, raw []byte, data JobData) error {
	filtered, err := filterCSVBytesForJob(raw, data)
	if err != nil {
		return fmt.Errorf("filter csv: %w", err)
	}
	if _, err := w.Write([]byte{0xEF, 0xBB, 0xBF}); err != nil {
		return err
	}
	_, err = w.Write(filtered)
	return err
}
