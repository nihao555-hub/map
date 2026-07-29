package web

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
)

type JobProgress struct {
	Status         string   `json:"status"`
	Count          int      `json:"count"`
	StartedAt      string   `json:"started_at,omitempty"`
	ElapsedSeconds int64    `json:"elapsed_seconds"`
	LatestNames    []string `json:"latest_names"`
}

func formatElapsed(seconds int64) string {
	if seconds < 0 {
		seconds = 0
	}

	return fmt.Sprintf("%02d:%02d", seconds/60, seconds%60)
}

func countCSVResults(r io.Reader) (resultCount int, resultLatest []string, err error) {
	reader := csv.NewReader(r)
	reader.FieldsPerRecord = -1

	header, err := reader.Read()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return 0, nil, nil
		}

		return 0, nil, err
	}

	titleIndex := -1

	for i, name := range header {
		if name == "title" {
			titleIndex = i
			break
		}
	}

	resultLatest = make([]string, 0, 5)

	for {
		row, err := reader.Read()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}

			var parseErr *csv.ParseError
			if errors.As(err, &parseErr) {
				break
			}

			return resultCount, resultLatest, err
		}

		resultCount++

		if titleIndex >= 0 && titleIndex < len(row) && row[titleIndex] != "" {
			resultLatest = append(resultLatest, row[titleIndex])

			if len(resultLatest) > 5 {
				resultLatest = resultLatest[len(resultLatest)-5:]
			}
		}
	}

	return resultCount, resultLatest, nil
}
