package main

import (
	"fmt"
	"net/http"
)

func main() {
	http.HandleFunc("/strip/Sooper1/Gain/Gain_dB", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			fmt.Println("Received POST with gain param")
			// Consider sending a response back to the client for POST requests
			// For example: w.WriteHeader(http.StatusOK) or fmt.Fprintln(w, "POST received")
		} else {
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprintln(w, "0.5") // mock response
		}
	})
	fmt.Println("Mock API running at http://localhost:9090/strip/Sooper1/Gain/Gain_dB")
	err := http.ListenAndServe(":9090", nil)
	if err != nil {
		fmt.Printf("Error starting server: %s\n", err)
	}
}
