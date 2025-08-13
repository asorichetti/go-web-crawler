package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"sync"
	"time"

	"golang.org/x/net/html"
	"golang.org/x/time/rate"
)

// Crawler manages the state of the web crawl
type Crawler struct {
	visited    map[string]bool //Tracks visited URL's to avoid duplicates
	mutex      sync.Mutex      //Protects visited map for concurrent access
	maxDepth   int             //Maximum crawl depth
	maxVisited int             //Maximum number of unique URL's to visit
	baseURL    *url.URL        //Base URL to restrict crawling to same host
	results    chan string     //Channel for collecting crawled URL's
	errors     chan error      //Channel for collecting errors
	wg         sync.WaitGroup  //WaitGroup to sync goroutines
	limiter    *rate.Limiter   //Rate limiter for HTTP requests
	client     *http.Client    //HTTP client for fetching URL's
}

// NewCrawler initializes a new Crawler with the given base URL, max depth, and max visited URL's.
func NewCrawler(baseURL string, maxDepth int, maxVisited int) (*Crawler, error) {
	parsedURL, err := url.Parse(baseURL) //Parse base URL
	if err != nil {                      //Check if the URL is invalid
		return nil, fmt.Errorf("invalid URL: %w", err)
	}
	//Create HTTP client for fetching URL's
	client := &http.Client{
		Timeout: 10 * time.Second, //Timeout after 10 seconds
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 20 { //Check if redirect limit is reached
				return fmt.Errorf("stopped after 20 redirects")
			}
			return nil
		},
	}
	return &Crawler{
		visited:    make(map[string]bool),
		maxDepth:   maxDepth,
		maxVisited: maxVisited,
		baseURL:    parsedURL,
		results:    make(chan string, 1000),                       //Channel for collecting crawled URL's
		errors:     make(chan error, 1000),                        //Channel for collecting errors
		limiter:    rate.NewLimiter(rate.Every(time.Second/5), 1), // 5 requests per second
		client:     client,
	}, nil
}

// Crawl starts the crawling process for a given URL up to max depth
func (c *Crawler) Crawl(startURL string, depth int) {
	defer c.wg.Done()

	// Stop if max depth is reached
	if depth > c.maxDepth {
		return
	}

	// Normalize URL
	parsedURL, err := url.Parse(startURL)
	//Check if parsing failed
	if err != nil {
		c.errors <- fmt.Errorf("error parsing URL %s: %v", startURL, err)
		return
	}
	//Check if the URL is on a different host than the base URL
	if parsedURL.Host != c.baseURL.Host {
		return // Skip external URL's
	}
	normalizedURL := parsedURL.String()

	// Check if already visited or max limit is reached
	c.mutex.Lock()
	if c.visited[normalizedURL] || len(c.visited) >= c.maxVisited {
		c.mutex.Unlock()
		return
	}
	c.visited[normalizedURL] = true
	c.mutex.Unlock()

	//Wait for rate limiter to allow the request
	if err := c.limiter.Wait(context.Background()); err != nil {
		c.errors <- fmt.Errorf("rate limit error for %s: %v", normalizedURL, err)
		return
	}

	// Fetch the page
	req, err := http.NewRequest("GET", normalizedURL, nil)
	//Check if request creation failed
	if err != nil {
		c.errors <- fmt.Errorf("error creating request for %s: %v", normalizedURL, err)
		return
	}
	//Set headers for fetching URL's
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.5")
	req.Header.Set("Referer", c.baseURL.String())
	resp, err := c.client.Do(req)
	//Check if HTTP request failed
	if err != nil {
		c.errors <- fmt.Errorf("error fetching %s: %v", normalizedURL, err)
		return
	}
	defer resp.Body.Close()

	//Check if the HTTP response status is not OK (200)
	if resp.StatusCode != http.StatusOK {
		c.errors <- fmt.Errorf("non-OK status for %s: %s", normalizedURL, resp.Status)
		return
	}

	// Parse HTML and extract links
	links, err := extractLinks(resp.Body, c.baseURL)
	//Check if HTML parsing failed
	if err != nil {
		c.errors <- fmt.Errorf("error parsing %s: %v", normalizedURL, err)
		return
	}

	//Send crawled URL to results channel
	select {
	case c.results <- normalizedURL:
	default:
		// Skip if channel is full to avoid blocking
	}

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
		switch tt {
		case html.ErrorToken:
			//Check if the tokenizer reached the end of the input
			if tokenizer.Err() == io.EOF {
				return links, nil
			}
			return nil, fmt.Errorf("error parsing HTML: %w", tokenizer.Err())
		case html.StartTagToken, html.SelfClosingTagToken:
			token := tokenizer.Token()
			//Check if the token is an anchor tag
			if token.Data == "a" {
				for _, attr := range token.Attr {
					if attr.Key == "href" {
						link, err := normalizeURL(attr.Val, baseURL)
						//Check if the URL normalization succeeded and the link is non-empty
						if err == nil && link != "" {
							links = append(links, link)
						}
					}
				}
			}
		}
	}
}

// normalizeURL converts relative URLs to absolute and validates
func normalizeURL(link string, baseURL *url.URL) (string, error) {
	//Parse the input link
	parsedLink, err := url.Parse(link)
	//Check if the link parsing failed
	if err != nil {
		return "", err
	}
	absoluteURL := baseURL.ResolveReference(parsedLink)
	//Check if the URL scheme is HTTP or HTTPS
	if absoluteURL.Scheme != "http" && absoluteURL.Scheme != "https" {
		return "", nil // Skip non-HTTP(S) links
	}
	return absoluteURL.String(), nil
}

// main parses command-line arguments and coordinates the web crawling process
func main() {
	//Check if the minimum required arguments are provided
	if len(os.Args) < 2 {
		fmt.Println("Usage: web_crawler <url> [max_depth] [max_visited]")
		os.Exit(1)
	}

	startURL := os.Args[1]
	maxDepth := 2     // Default depth
	maxVisited := 100 // Default max visited URL's
	//Check if max depth is provided
	if len(os.Args) > 2 {
		//Check if the max depth argument is a valid non-negative integer
		if d, err := strconv.Atoi(os.Args[2]); err == nil && d >= 0 {
			maxDepth = d
		}
	}
	//Check if max visited is provided
	if len(os.Args) > 3 {
		//Check if the max visited argument is a valid positive integer
		if v, err := strconv.Atoi(os.Args[3]); err == nil && v > 0 {
			maxVisited = v
		}
	}

	//Initialize the crawler
	crawler, err := NewCrawler(startURL, maxDepth, maxVisited)
	//Check if the crawler initialization failed
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Start crawling
	crawler.wg.Add(1)
	go crawler.Crawl(startURL, 1)

	// Collect results and errors
	go func() {
		crawler.wg.Wait()
		close(crawler.results)
		close(crawler.errors)
	}()

	// Print results
	for url := range crawler.results {
		fmt.Println(url)
	}

	//Aggregate and print errors
	var aggregatedErrors []error
	for err := range crawler.errors {
		aggregatedErrors = append(aggregatedErrors, err)
	}
	//Check if any errors were collected
	if len(aggregatedErrors) > 0 {
		fmt.Fprintf(os.Stderr, "\nAggregated Errors:\n")
		for _, err := range aggregatedErrors {
			fmt.Fprintf(os.Stderr, "%v\n", err)
		}
	}
}
