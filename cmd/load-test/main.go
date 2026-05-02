package main

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"
)

func main() {
	totalRequests := 1000
	concurrency := 10
	apiKey := "ZLxJ9Y5gSO_B7Zfvxrrxj4AvtVL3OOYEofAPlBDo4no="

	models := []string{"gpt-4o", "deepseek-chat", "qwen-turbo"}
	messages := []string{
		"Hello, how are you?",
		"What is the weather today?",
		"Explain quantum computing",
		"Tell me a joke",
		"What is machine learning?",
		"Write a poem",
		"Explain AI",
		"What is blockchain?",
		"Help me write an email",
		"Tell me about history",
	}

	log.Printf("Starting load test with %d requests, %d concurrent workers\n", totalRequests, concurrency)

	var wg sync.WaitGroup
	sem := make(chan struct{}, concurrency)

	start := time.Now()
	successCount := 0
	failureCount := 0
	var mu sync.Mutex

	for i := 0; i < totalRequests; i++ {
		wg.Add(1)
		sem <- struct{}{}

		go func(requestID int) {
			defer wg.Done()
			defer func() { <-sem }()

			model := models[requestID%len(models)]
			message := messages[requestID%len(messages)]

			reqBody := map[string]interface{}{
				"model": model,
				"messages": []map[string]string{
					{"role": "user", "content": message},
				},
				"stream": false,
			}

			jsonBody, err := json.Marshal(reqBody)
			if err != nil {
				log.Printf("Request %d: Failed to marshal request: %v\n", requestID, err)
				mu.Lock()
				failureCount++
				mu.Unlock()
				return
			}

			req, err := http.NewRequest("POST", "http://localhost:8082/v1/chat/completions", bytes.NewBuffer(jsonBody))
			if err != nil {
				log.Printf("Request %d: Failed to create request: %v\n", requestID, err)
				mu.Lock()
				failureCount++
				mu.Unlock()
				return
			}

			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "ApiKey "+apiKey)

			client := &http.Client{Timeout: 30 * time.Second}
			resp, err := client.Do(req)
			if err != nil {
				log.Printf("Request %d: Failed to send request: %v\n", requestID, err)
				mu.Lock()
				failureCount++
				mu.Unlock()
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode == http.StatusOK {
				mu.Lock()
				successCount++
				mu.Unlock()
			} else {
				log.Printf("Request %d: Status code %d\n", requestID, resp.StatusCode)
				mu.Lock()
				failureCount++
				mu.Unlock()
			}

			if requestID%100 == 0 {
				mu.Lock()
				log.Printf("Progress: %d/%d requests completed\n", successCount+failureCount, totalRequests)
				mu.Unlock()
			}
		}(i)
	}

	wg.Wait()

	duration := time.Since(start)
	log.Printf("\nLoad test completed!\n")
	log.Printf("Total requests: %d\n", totalRequests)
	log.Printf("Success: %d (%.2f%%)\n", successCount, float64(successCount)/float64(totalRequests)*100)
	log.Printf("Failed: %d (%.2f%%)\n", failureCount, float64(failureCount)/float64(totalRequests)*100)
	log.Printf("Duration: %v\n", duration)
	log.Printf("Requests per second: %.2f\n", float64(totalRequests)/duration.Seconds())
}
