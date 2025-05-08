package main

import (
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
)

func main() {
	// Handler for the new dynamic POST endpoint and the old GET endpoint
	http.HandleFunc("/strip/", func(w http.ResponseWriter, r *http.Request) {
		// Regex to capture the ID from the path for the new POST pattern
		// Path: /strip/Sooper<ID>/Gain/Gain (dB)
		postPathRegex := regexp.MustCompile(`^/strip/Sooper(\d+)/Gain/Gain \(dB\)$`)
		postMatches := postPathRegex.FindStringSubmatch(r.URL.Path)

		if postMatches != nil { // Matches the new POST pattern
			if r.Method == http.MethodPost {
				idStr := postMatches[1]
				bodyBytes, err := io.ReadAll(r.Body)
				if err != nil {
					http.Error(w, "Error reading request body", http.StatusInternalServerError)
					fmt.Printf("Error reading body for SooperID %s (path: %s): %v\n", idStr, r.URL.Path, err)
					return
				}
				defer r.Body.Close() // Important to close the body

				valueStr := strings.TrimSpace(string(bodyBytes))
				fmt.Printf("Received POST for SooperID %s (path: %s) with gain value: %s\n", idStr, r.URL.Path, valueStr)
				w.WriteHeader(http.StatusOK)
				fmt.Fprintln(w, "POST received for SooperID "+idStr)
			} else {
				// Path matched POST pattern, but method is not POST
				http.Error(w, "Method not allowed. Expected POST for this path pattern.", http.StatusMethodNotAllowed)
			}
			return // Handled
		}
		
		// Check for the old GET endpoint if it didn't match the new POST pattern
		if r.URL.Path == "/strip/Sooper1/Gain/Gain_dB" && r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprintln(w, "0.5") // mock response for old GET
			return // Handled
		}

		// If no pattern under /strip/ matched
		http.NotFound(w, r)
		fmt.Printf("Path not handled under /strip/: %s by this handler\n", r.URL.Path)
	})
	
	fmt.Println("Mock API running at http://localhost:9090")
	fmt.Println("  Handles GET /strip/Sooper1/Gain/Gain_dB")
	fmt.Println("  Handles POST /strip/Sooper<ID>/Gain/Gain (dB)")

	err := http.ListenAndServe(":9090", nil)
	if err != nil {
		fmt.Printf("Error starting server: %s\n", err)
	}
}
