package main

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

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

var cacheMutex sync.RWMutex
var cache []forecast
var cacheTimestamp time.Time

func init() {
	// Print date/time when logging using the default logger.
	log.SetFlags(log.LstdFlags)
}

func main() {
	// Start the goroutine for refreshing the cache periodically.
	go refreshCacheJob()

	r := gin.Default()
	r.LoadHTMLGlob("templates/*.html")
	r.Static("/static", "static")
	r.GET("/", func(c *gin.Context) {
		output, err := fetchCache()

		// If we're unable to fetch anything from the cache, tell the client.
		if err != nil {
			c.String(http.StatusInternalServerError, err.Error())
			return
		}

		c.HTML(http.StatusOK, "index.html", gin.H{
			"output":         output,
			"cacheTimestamp": cacheTimestamp.Format("15:04:05"),
		})
	})
	r.GET("/api", func(c *gin.Context) {
		output, err := fetchCache()

		// If we're unable to fetch anything from the cache, tell the client.
		if err != nil {
			c.String(http.StatusInternalServerError, err.Error())
			return
		}

		// Tell the use about the freshness of the cache.
		c.Header("X-Cache-Timestamp", cacheTimestamp.Format(time.RFC3339))

		// Otherwise, return what was in the cache.
		c.JSON(http.StatusOK, output)
	})
	listenAddr, ok := os.LookupEnv("LISTEN_ADDR")
	if !ok {
		listenAddr = ":8080"
	}
	r.Run(listenAddr)
}

func fetchCache() ([]forecast, error) {
	// Grab a read lock.
	cacheMutex.RLock()
	defer cacheMutex.RUnlock()

	// Check if the cache is empty, if it rebuild cache and return an error.
	if cache == nil {
		go rebuildCache()
		return nil, errors.New("Cache is empty, try again in a few seconds")
	}

	return cache, nil
}

// Handles refreshing the cache every so often.
func refreshCacheJob() {
	for {
		// Rebuild the cache.
		err := rebuildCache()

		// Log whatever happened.
		if err != nil {
			log.Fatalln("Error rebuilding cache:", err)
		} else {
			log.Println("Cache rebuild successful")
		}

		<-time.Tick(10 * time.Minute)
	}
}

func rebuildCache() error {
	// Check for cache before starting.
	cacheMutex.Lock()
	defer cacheMutex.Unlock()

	doc, err := goquery.NewDocument(url)
	if err != nil {
		return fmt.Errorf("error fetching from URL: %v", err)
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
		return outerErr
	}

	// Set the cache for the future.
	cache = forecasts
	cacheTimestamp = time.Now().UTC()
	return nil
}
