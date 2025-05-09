package main

import (
	"fmt"
	"log"
	"regexp"
	"strconv"

	"github.com/hypebeast/go-osc/osc"
)

func main() {
	// Define the OSC address pattern we are interested in.
	// This regex will capture the numeric ID part of "Sooper<ID>".
	// Example: /strip/Sooper1/Gain/Gain (dB) -> ID will be "1"
	// Note: OSC addresses don't typically contain spaces or parentheses,
	// but we are matching the specific string provided.
	// The path in OSC is usually a forward-slash separated string.
	// The regex needs to match the literal characters, including parentheses and the literal "%20".
	// We use raw string literals (backticks) for the regex pattern
	// to avoid needing to double-escape backslashes for special characters like (.
	// The "%" also doesn't need escaping in a raw string literal for regex.
	pathRegex := regexp.MustCompile(`^/strip/Sooper(\d+)/Gain/Gain%20\(dB\)$`)

	// Create a new OSC dispatcher.
	// The dispatcher is responsible for routing incoming OSC messages
	// to the correct handler functions based on their address.
	dispatcher := osc.NewStandardDispatcher()

	// Add a handler function for any OSC message that matches the regex.
	// We use a generic "*" handler and then filter by regex inside,
	// as the go-osc dispatcher doesn't directly support regex matching for handlers.
	// Alternatively, one could register a handler for "/strip/*" if the library supports wildcards,
	// or iterate through expected paths if IDs are known.
	// For this specific case, we'll check the address inside a global handler.
	err := dispatcher.AddMsgHandler("*", func(msg *osc.Message) {
		fmt.Printf("Received OSC message: %s %v\n", msg.Address, msg.Arguments)

		matches := pathRegex.FindStringSubmatch(msg.Address)
		// matches[0] is the full string, matches[1] is the first capture group (the ID).
		if matches != nil && len(matches) > 1 {
			idStr := matches[1]
			id, err := strconv.Atoi(idStr)
			if err != nil {
				log.Printf("Error converting ID '%s' to int: %v\n", idStr, err)
				return
			}

			// Expecting one argument: a float32 for the gain value.
			if len(msg.Arguments) == 1 {
				// Type assertion to get the float32 value.
				// The 'ok' variable will be true if the assertion succeeds.
				if gainValue, ok := msg.Arguments[0].(float32); ok {
					fmt.Printf("Mock OSC: Received Gain for SooperID %d (path: %s) with value: %f\n", id, msg.Address, gainValue)
				} else {
					log.Printf("Mock OSC: Received message for SooperID %d but argument is not a float32: %T\n", id, msg.Arguments[0])
				}
			} else {
				log.Printf("Mock OSC: Received message for SooperID %d but expected 1 argument, got %d\n", id, len(msg.Arguments))
			}
		}
		// If the address doesn't match our specific pattern, it will just be logged by the initial Printf.
	})
	if err != nil {
		log.Fatalf("Error adding OSC message handler: %v", err)
	}

	// Define the address and port for the OSC server to listen on.
	// Port 9090 as previously used for the mock.
	serverAddr := "127.0.0.1:9090"

	// Create the OSC server.
	server := &osc.Server{
		Addr:       serverAddr,
		Dispatcher: dispatcher,
	}

	fmt.Printf("Mock OSC Server running and listening on udp://%s\n", serverAddr)
	fmt.Printf("Expecting messages to pattern: /strip/Sooper<ID>/Gain/Gain%%20(dB)\n") // %% for literal % in Printf

	// Start listening for OSC messages.
	// This is a blocking call, so the program will stay running here.
	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("Error starting OSC server: %v", err)
	}
}
