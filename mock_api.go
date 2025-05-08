package main

import (
	"fmt"
	"net/http"
)

func main() {
	http.HandleFunc("/strip/Sooper1/Gain/Gain (dB)", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			fmt.Println("Received POST with gain param")
		} else {
			fmt.Fprintln(w, "0.5") // mock response
		}
	})
	fmt.Println("Mock API running at http://localhost:9090")
	http.ListenAndServe(":9090", nil)
}
