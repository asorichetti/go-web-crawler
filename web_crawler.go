package main

import (
	"fmt"
	"net/url"
	"sync"
)

// Crawler manages the state of the web crawl
type Crawler struct {
	visited  map[string]bool
	mutex    sync.Mutex
	maxDepth int
	baseURL  *url.URL
	results  chan string
	wg       sync.WaitGroup
}

// NewCrawler initializes a new Crawler
func NewCrawler(baseURL string, maxDepth int) (*Crawler, error) {
	parsedURL, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}
	return &Crawler{
		visited:  make(map[string]bool),
		maxDepth: maxDepth,
		baseURL:  parsedURL,
		results:  make(chan string, 100),
	}, nil
}
