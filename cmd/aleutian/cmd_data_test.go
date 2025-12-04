package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
)

func TestFileWorker(t *testing.T) {
	// 1. Create a dummy file to ingest
	tmpFile, err := os.CreateTemp("", "test_ingest_*.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name()) // Clean up
	tmpFile.WriteString("Dummy content for ingestion")
	tmpFile.Close()

	// 2. Create Mock Orchestrator
	var receivedContent string
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/documents" {
			t.Errorf("Worker hit wrong endpoint: %s", r.URL.Path)
		}

		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		receivedContent = body["content"] // Capture for assertion

		// Send success response
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":           "success",
			"source":           body["source"],
			"chunks_processed": 5,
		})
	}))
	defer mockServer.Close()

	// 3. Setup Worker Inputs
	var wg sync.WaitGroup
	jobs := make(chan string, 1)

	// 4. Run the Worker
	wg.Add(1)
	// We pass the mockServer.URL + endpoint directly
	go fileWorker(1, &wg, jobs, mockServer.URL+"/v1/documents", "default", "v1")

	// 5. Send Job
	jobs <- tmpFile.Name()
	close(jobs)
	wg.Wait()

	// 6. Assertions
	if receivedContent != "Dummy content for ingestion" {
		t.Errorf("Worker failed to send correct content. Got: %s", receivedContent)
	}
}
