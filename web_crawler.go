package main

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
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

// Crawl starts the crawling process
func (c *Crawler) Crawl(startURL string, depth int){
	defer c.wg.Done()

	//Stop if max depth is reached
	if depth > c.maxDepth{
		return
	}

	// Normalize URL
	parsedURL, err := url.Parse(startURL)
	if err != nil{
		fmt.Fprintf(os.Stderr, "Error parsing URL %s: %v\n", startURL, err)
		return
	}
	if parsedURL.Host != c.baseURL.Host{
		return //Skip external URLs
	}
	normalizedURL := parsedURL.String()

	//Check if already visited
	c.mutex.Lock()
	if c.visited[normalizedURL]{
		c.mutex.Unlock()
		return
	}
	c.visited[normalizedURL] = true
	c.mutex.Unlock()

	// Fetch the page
	resp, err := http.Get(normalizedURL)
	if err != nil{
		fmt.Fprintf(os.Stderr, "Error fetching %s: %v\n", normalizedURL, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderrm "Non-OK status for %s: %s\n", normalizedURL, resp.Status)
		return
	}

	// Parse HTML and extract links
	links, err := extractLinks(resp.Body, c.baseURL)
	if err != nil{
		fmt.Fprintf(os.Stderr, "Error parsing %s: %v\n", normalizedURL, err)
		return
	}

	// Send URL to results channel
	c.results <- normalizedURL

	// Spawn goroutines for each link
	for _, link := range links {
		c.wg.Add(1)
		go c.Crawl(link, depth+1)
	}
}

// extractLinks parses HTML and returns valid links
func extractLinks(body io.Reader, baseURL *url.URL) ([]string, error) {
	var links []string
	tokenizer := html.NewTokenizer(body)

	for {
		tt := tokenizer.Next()
		switch tt{
		case html.ErrorToken:
			if tokenizer.Err() == io.EOF{
				return
			}
			return nil, fmt.Errorf("error parsing HTML: %w", tokenizer.Err())
		case html.StartTagToken, html.SelfClosingTagToken:
			token := tokenizer.Token()
			if tok.Date == "a"{
				for _, attr := range token.Attr {
					if attr.Key == "href"{
						link, err := normalizeURL(attr.Val, baseURL)
						if err == nil && link != ""{
							links = append(links, link)
						}
					}
				}
			}
		}
	}
}
