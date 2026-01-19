package main

import (
	"bufio"
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

const chromeUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

type Result struct {
	URL        string
	StatusCode int
	Error      error
	Duration   time.Duration
}

func main() {
	inputFile := flag.String("i", "", "Input file containing URLs (required)")
	proxyAddr := flag.String("p", "", "Proxy address as ip:port (required)")
	flag.Parse()

	if *inputFile == "" || *proxyAddr == "" {
		fmt.Println("Error: Both -i (input file) and -p (proxy) parameters are required")
		flag.Usage()
		os.Exit(1)
	}

	// Read URLs from file
	urls, err := readURLsFromFile(*inputFile)
	if err != nil {
		fmt.Printf("Error reading file: %v\n", err)
		os.Exit(1)
	}

	if len(urls) == 0 {
		fmt.Println("No URLs found in input file")
		os.Exit(0)
	}

	fmt.Printf("Found %d URLs to request\n", len(urls))
	fmt.Printf("Using proxy: %s\n", *proxyAddr)
	fmt.Println(strings.Repeat("-", 50))

	// Create HTTP client with proxy
	client, err := createProxyClient(*proxyAddr)
	if err != nil {
		fmt.Printf("Error creating proxy client: %v\n", err)
		os.Exit(1)
	}

	// Process URLs with goroutines
	results := processURLs(client, urls)

	// Print summary
	printSummary(results)
}

func readURLsFromFile(filename string) ([]string, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	var urls []string
	seen := make(map[string]bool)
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Basic URL validation
		if strings.HasPrefix(line, "http://") || strings.HasPrefix(line, "https://") {
			if !seen[line] {
				urls = append(urls, line)
				seen[line] = true
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading file: %w", err)
	}

	return urls, nil
}

func createProxyClient(proxyAddr string) (*http.Client, error) {
	// Add http:// scheme if not present
	if !strings.HasPrefix(proxyAddr, "http://") && !strings.HasPrefix(proxyAddr, "https://") {
		proxyAddr = "http://" + proxyAddr
	}

	proxyURL, err := url.Parse(proxyAddr)
	if err != nil {
		return nil, fmt.Errorf("invalid proxy address: %w", err)
	}

	transport := &http.Transport{
		Proxy: http.ProxyURL(proxyURL),
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
	}

	return client, nil
}

func processURLs(client *http.Client, urls []string) []Result {
	var wg sync.WaitGroup
	results := make([]Result, len(urls))
	
	for i, urlStr := range urls {
		wg.Add(1)
		go func(index int, u string) {
			defer wg.Done()
			results[index] = makeRequest(client, u)
		}(i, urlStr)
	}

	wg.Wait()
	return results
}

func makeRequest(client *http.Client, urlStr string) Result {
	start := time.Now()
	result := Result{URL: urlStr}

	fmt.Printf("Requesting: %s\n", urlStr)

	// Create request with Chrome user agent
	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		result.Error = err
		result.Duration = time.Since(start)
		fmt.Printf("  ✗ Error creating request: %v\n", err)
		return result
	}

	req.Header.Set("User-Agent", chromeUserAgent)

	// Execute request
	resp, err := client.Do(req)
	result.Duration = time.Since(start)

	if err != nil {
		result.Error = err
		fmt.Printf("  ✗ Error: %v (%.2fs)\n", err, result.Duration.Seconds())
		return result
	}
	defer resp.Body.Close()

	result.StatusCode = resp.StatusCode

	// Read and discard body to allow connection reuse
	io.Copy(io.Discard, resp.Body)

	fmt.Printf("  ✓ Status: %d (%.2fs)\n", resp.StatusCode, result.Duration.Seconds())

	return result
}

func printSummary(results []Result) {
	fmt.Println(strings.Repeat("-", 50))
	fmt.Println("Summary:")

	successCount := 0
	errorCount := 0
	totalDuration := time.Duration(0)
	statusCodes := make(map[int]int)

	for _, result := range results {
		totalDuration += result.Duration

		if result.Error != nil {
			errorCount++
		} else {
			successCount++
			statusCodes[result.StatusCode]++
		}
	}

	fmt.Printf("  Total URLs: %d\n", len(results))
	fmt.Printf("  Successful: %d\n", successCount)
	fmt.Printf("  Errors: %d\n", errorCount)

	if len(statusCodes) > 0 {
		fmt.Println("\n  Status codes:")
		for code, count := range statusCodes {
			fmt.Printf("    %d: %d\n", code, count)
		}
	}

	if len(results) > 0 {
		avgDuration := totalDuration / time.Duration(len(results))
		fmt.Printf("\n  Average duration: %.2fs\n", avgDuration.Seconds())
		fmt.Printf("  Total time: %.2fs\n", totalDuration.Seconds())
	}
}
