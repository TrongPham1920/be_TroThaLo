package services

import (
	"errors"
	"fmt"
	"github.com/goccy/go-json"
	"io"
	"log"
	"net/http"
	"net/url"
)

type GeocodingFeature struct {
	PlaceName string     `json:"place_name"`
	Center    [2]float64 `json:"center"`
	Relevance float64    `json:"relevance"`
}
type GeocodingResponse struct {
	Features []GeocodingFeature `json:"features"`
}

func GetBestCoordinatesFromResponse(body io.Reader) (float64, float64, error) {
	var response GeocodingResponse
	if err := json.NewDecoder(body).Decode(&response); err != nil {
		return 0, 0, fmt.Errorf("failed to parse response: %w", err)
	}

	if len(response.Features) == 0 {
		return 0, 0, errors.New("no results found")
	}

	// Chọn kết quả có relevance cao nhất
	bestFeature := response.Features[0]
	for _, feature := range response.Features {
		if feature.Relevance > bestFeature.Relevance {
			bestFeature = feature
		}
	}

	return bestFeature.Center[0], bestFeature.Center[1], nil
}

// GetCoordinatesFromAddress gọi API và lấy tọa độ
func GetCoordinatesFromAddress(address, district, province, ward, mapboxAccessToken string) (float64, float64, error) {
	fullAddress := fmt.Sprintf("%s, %s, %s, %s", address, ward, district, province)
	encodedAddress := url.QueryEscape(fullAddress)
	log.Println("encodedAddress:", encodedAddress)
	apiURL := fmt.Sprintf(
		"https://api.mapbox.com/geocoding/v5/mapbox.places/%s.json?access_token=%s&country=VN",
		encodedAddress,
		mapboxAccessToken,
	)

	resp, err := http.Get(apiURL)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to send request: %w", err)
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {

		}
	}(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return 0, 0, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	// Lấy tọa độ tốt nhất
	return GetBestCoordinatesFromResponse(resp.Body)
}
