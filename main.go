package main

import (
	"strconv"
	"sync"

	"fmt"

	"github.com/PuerkitoBio/goquery"
	"github.com/gin-gonic/gin"
)

const url = "https://www.dmi.dk/vejr/sundhedsvejr/pollen/"

type forecast struct {
	CityName     string          `json:"city_name"`
	ForecastText string          `json:"forecast_text"`
	Values       []forecastValue `json:"values"`
}

type forecastValue struct {
	Name  string `json:"name"`
	Value int    `json:"value"`
}

var cacheMutex sync.Mutex
var cache []forecast

func init() {
	cacheMutex = sync.Mutex{}
}
func main() {
	r := gin.Default()
	r.GET("/", func(c *gin.Context) {
		output, err := fetchParseAndJsonify()
		if err != nil {
			c.AbortWithError(500, err)
			return
		}
		c.JSON(200, output)
	})
	r.Run(":8000")
}

func fetchParseAndJsonify() ([]forecast, error) {
	// Check for cache before starting.
	cacheMutex.Lock()
	if cache != nil {
		cacheMutex.Unlock()
		return cache, nil
	}
	cacheMutex.Unlock()

	doc, err := goquery.NewDocument(url)
	if err != nil {
		return nil, fmt.Errorf("error fetching from URL: %v", err)
	}
	forecasts := make([]forecast, 0, 0)

	var outerErr error
	doc.Find("div.tx-dmi-data-store table table").Each(func(index int, selection *goquery.Selection) {
		// Skip if any of the previous iterations encountered an error.
		if outerErr != nil {
			return
		}

		// Parse varies fields.
		cityName := selection.Find("tr").First().Text()
		forecastText := selection.Find("tr").Last().Text()
		forecastValues := make([]forecastValue, 0, 0)

		// Iterate on each pollen type.
		var err error
		selection.Find("tr").Each(func(index int, sel *goquery.Selection) {
			// Skip known offenders or if we encountered an error.
			if err != nil || sel.Find("td").Length() != 2 {
				return
			}

			// Try to parse the value as an integer, setting 0 in case of failure.
			strValue := sel.Find("td").Last().Text()
			value := 0
			if strValue != "-" {
				// Otherwise try to parse int, returning an error if we fail.
				var innerErr error
				value, innerErr = strconv.Atoi(strValue)
				if innerErr != nil {
					err = fmt.Errorf("unable to parse pollen value for \"%s\": %v", strValue, innerErr)
				}
			}

			// Success, append and proceed.
			forecastValues = append(forecastValues, forecastValue{
				Name:  sel.Find("td").First().Text(),
				Value: value,
			})
		})
		// If any errors happened while we iterated, propagate it now.
		if err != nil {
			outerErr = err
			return
		}

		forecasts = append(forecasts, forecast{
			CityName:     cityName,
			ForecastText: forecastText,
			Values:       forecastValues,
		})
	})
	if outerErr != nil {
		return nil, outerErr
	}

	// Set the cache for the future.
	cacheMutex.Lock()
	cache = forecasts
	cacheMutex.Unlock()
	return forecasts, nil
}
