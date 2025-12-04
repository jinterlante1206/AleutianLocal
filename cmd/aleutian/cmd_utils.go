// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

type ConvertResponse struct {
	Status     string `json:"status"`
	Message    string `json:"message"`
	OutputPath string `json:"output_path"`
	Logs       string `json:"logs"`
}

type ModelListJSON struct {
	ModelList []struct {
		Name            string `json:"name"`
		HuggingFaceLink string `json:"huggingface_link"`
	} `json:"model_list"`
}

func runCacheAll(cmd *cobra.Command, args []string) {
	jsonPath := args[0]
	byteValue, err := os.ReadFile(jsonPath)
	if err != nil {
		log.Fatalf("Failed to read JSON file: %v", err)
	}

	var list ModelListJSON
	if err := json.Unmarshal(byteValue, &list); err != nil {
		log.Fatalf("Failed to parse JSON: %v", err)
	}

	// Regex to extract URL from Markdown links [link](url)
	re := regexp.MustCompile(`\((https://huggingface\.co/[^)]+)\)`)

	for _, item := range list.ModelList {
		matches := re.FindStringSubmatch(item.HuggingFaceLink)
		if len(matches) < 2 {
			continue
		}
		fullURL := matches[1]
		if strings.Contains(fullURL, "/papers/") {
			continue // Skip papers
		}

		// Clean ID: https://huggingface.co/org/repo -> org/repo
		repoID := strings.TrimPrefix(fullURL, "https://huggingface.co/")
		repoID = strings.Split(repoID, "?")[0]
		repoID = strings.TrimSuffix(repoID, "/")

		fmt.Printf("Processing %s (%s)...\n", item.Name, repoID)
		triggerDownload(repoID)
	}
}

func runConvertCommand(cmd *cobra.Command, args []string) {
	modelId := args[0]
	converterPort := 12140
	converterHost := "localhost"
	converterURL := fmt.Sprintf("http://%s:%d/convert", converterHost, converterPort)
	payload, _ := json.Marshal(map[string]interface{}{
		"model_id":      modelId,
		"quantize_type": quantizeType,
		"is_local_path": isLocalPath,
	})
	fmt.Printf("Sending the conversion request for %s (type: %s). This may take some time.\n",
		modelId, quantizeType)
	client := &http.Client{Timeout: 45 * time.Minute}
	resp, err := client.Post(converterURL, "application/json", bytes.NewBuffer(payload))
	if err != nil {
		log.Fatalf("Failed to call the GGUF converter service: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Fatalf("The GGUF Converter service failed and returned an error (%s)", string(body))
	}
	var convertResp ConvertResponse
	if err := json.NewDecoder(resp.Body).Decode(&convertResp); err != nil {
		log.Fatalf("Failed to parse response from converter: %v", err)
	}

	register, _ := cmd.Flags().GetBool("register")
	if register {
		fmt.Println("Registering the gguf model file with ollama")
		cwd, err := os.Getwd()
		if err != nil {
			log.Fatalf("Failed to get current directory: %v", err)
		}
		hostGgufPath := filepath.Join(cwd, convertResp.OutputPath)
		if _, err := os.Stat(hostGgufPath); os.IsNotExist(err) {
			log.Fatalf("Could not find converted GGUF file on host at %s: %v", hostGgufPath, err)
		}
		modelFileContent := fmt.Sprintf("FROM %s", hostGgufPath)
		modelFileContent += fmt.Sprintf("\nPARAMETER stop %q", "</answer>") // Use %q for quoting
		modelFileContent += fmt.Sprintf("\nPARAMETER stop %q", "</s>")      // Common EOS token, good to include
		modelFileContent += fmt.Sprintf("\nPARAMETER stop %q", "Done")
		modelFileContent += fmt.Sprintf("\nPARAMETER stop %q", "End")
		modelFileContent += fmt.Sprintf("\nPARAMETER stop %q", "Response complete")

		osTmpFile, err := os.CreateTemp("", "Modelfile-*")
		if err != nil {
			log.Fatalf("Failed to create the temporary modelfile: %v", err)
		}
		defer osTmpFile.Close()
		defer os.Remove(osTmpFile.Name())
		_, err = osTmpFile.WriteString(modelFileContent)
		if err != nil {
			log.Fatalf("Failed to write to the tmpfile %v", err)
		}
		osTmpFile.Name()
		ollamaCreate := exec.Command("ollama", "create", modelId+"_local", "-f", osTmpFile.Name())
		ollamaCreate.Stdout = os.Stdout
		ollamaCreate.Stderr = os.Stderr
		if err = ollamaCreate.Run(); err != nil {
			log.Fatalf("Ollama failed to register your gguf model %s: %v", modelId, err)
		}
	}

	fmt.Printf("\n🎉 %s\n", convertResp.Message)
	fmt.Printf("   Output File: %s\n", convertResp.OutputPath)
	if register {
		fmt.Println("Registered the output file with Ollama")
	}
	fmt.Println("--- Conversion Logs ---")
	fmt.Println(convertResp.Logs)
	fmt.Println("-----------------------")
	fmt.Println("\nCheck the converter logs for full details: podman logs -f aleutian-gguf-converter")
}

func runPullModel(cmd *cobra.Command, args []string) {
	modelID := args[0]
	triggerDownload(modelID)
}

func triggerDownload(modelID string) {
	// Load config to find Orchestrator URL
	baseURL := getOrchestratorBaseURL()
	url := fmt.Sprintf("%s/v1/models/pull", baseURL)

	payload := map[string]string{"model_id": modelID}
	jsonBody, _ := json.Marshal(payload)

	fmt.Printf("Requesting download for %s... ", modelID)
	client := &http.Client{Timeout: 30 * time.Minute} // Long timeout for downloads
	resp, err := client.Post(url, "application/json", bytes.NewBuffer(jsonBody))

	if err != nil {
		fmt.Printf("Connection Failed: %v\n", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == 200 {
		fmt.Println("Success")
	} else {
		body, _ := io.ReadAll(resp.Body)
		fmt.Printf("Failed (Status %d): %s\n", resp.StatusCode, string(body))
	}
}
