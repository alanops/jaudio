package main

import (
	"fmt"
	"log"
	"net"
	"regexp"
	"strconv"
	"strings"

	"github.com/hypebeast/go-osc/osc"
)

// Placeholder for current gain values, if we want the mock to have some state.
// For now, it will just send a fixed value back.
// var mockStripGains = make(map[int]float32)

func main() {
	// Regex for matching the path where gain values are SET or where UPDATES for gain values arrive.
	// Example: /strip/Sooper1/Gain/Gain%20(dB)
	stripGainPathRegex := regexp.MustCompile(`^/strip/Sooper(\d+)/Gain/Gain%20\(dB\)$`)

	dispatcher := osc.NewStandardDispatcher()

	// Handler for SETTING the gain value (e.g., from sooperGUI's mouse drag)
	// This handler also implicitly handles incoming updates if the target sends them on this path.
	err := dispatcher.AddMsgHandler("*", func(msg *osc.Message) {
		// Log all messages first for general debugging
		// msg.Sender() is not available directly on osc.Message with this library's dispatcher.
		// The return address for /get_strip_gain comes from message arguments.
		fmt.Printf("Received OSC message: Address: %s, Arguments: %v\n", msg.Address, msg.Arguments)

		// Check if it's a message to set/update the strip gain
		matches := stripGainPathRegex.FindStringSubmatch(msg.Address)
		if matches != nil && len(matches) > 1 {
			idStr := matches[1]
			id_1based, err := strconv.Atoi(idStr)
			if err != nil {
				log.Printf("Error converting ID '%s' from path %s to int: %v\n", idStr, msg.Address, err)
				return
			}

			if len(msg.Arguments) == 1 {
				if gainValue, ok := msg.Arguments[0].(float32); ok {
					fmt.Printf("Mock OSC: Received/Set Gain for SooperID %d (path: %s) with value: %f\n", id_1based, msg.Address, gainValue)
					// If we wanted the mock to have state:
					// mockStripGains[id_1based] = gainValue
				} else {
					log.Printf("Mock OSC: Received message for SooperID %d (path: %s) but argument is not a float32: %T\n", id_1based, msg.Address, msg.Arguments[0])
				}
			} else {
				log.Printf("Mock OSC: Received message for SooperID %d (path: %s) but expected 1 argument, got %d\n", id_1based, msg.Address, len(msg.Arguments))
			}
			return // Message handled (or attempted)
		}

		// Handler for GETTING the strip gain value (e.g., from sooperGUI's polling)
		// Expects: /get_strip_gain <loopID_1based_int32> <return_url_string> <reply_path_string>
		if msg.Address == "/get_strip_gain" {
			if len(msg.Arguments) == 3 {
				loopID_1based, okLoopID := msg.Arguments[0].(int32)
				returnURL, okReturnURL := msg.Arguments[1].(string)
				replyPath, okReplyPath := msg.Arguments[2].(string)

				if okLoopID && okReturnURL && okReplyPath {
					fmt.Printf("Mock OSC: Received /get_strip_gain for LoopID %d. Will reply to %s on path %s\n", loopID_1based, returnURL, replyPath)

					// Extract host and port from returnURL (e.g., "osc.udp://127.0.0.1:9951")
					// The go-osc client needs "host:port" format.
					parsedReturnURL := strings.TrimPrefix(returnURL, "osc.udp://")
					host, portStr, err := net.SplitHostPort(parsedReturnURL)
					if err != nil {
						log.Printf("Mock OSC: Error parsing returnURL '%s': %v\n", returnURL, err)
						return
					}
					port, err := strconv.Atoi(portStr)
					if err != nil {
						log.Printf("Mock OSC: Error converting port '%s' from returnURL to int: %v\n", portStr, err)
						return
					}

					// Create a temporary client to send the reply.
					replyClient := osc.NewClient(host, port)
					replyMsg := osc.NewMessage(replyPath)
					
					// Placeholder value. If mockStripGains was used, retrieve from there.
					var valueToReturn float32 = 0.75 
					// if val, exists := mockStripGains[int(loopID_1based)]; exists {
					// 	valueToReturn = val
					// }
					replyMsg.Append(valueToReturn)

					err = replyClient.Send(replyMsg)
					if err != nil {
						log.Printf("Mock OSC: Error sending reply to %s on path %s: %v\n", returnURL, replyPath, err)
					} else {
						fmt.Printf("Mock OSC: Sent reply to %s path %s with value %f for loop %d\n", returnURL, replyPath, valueToReturn, loopID_1based)
					}

				} else {
					log.Printf("Mock OSC: Received /get_strip_gain with incorrect argument types: %T, %T, %T\n", msg.Arguments[0], msg.Arguments[1], msg.Arguments[2])
				}
			} else {
				log.Printf("Mock OSC: Received /get_strip_gain with incorrect number of arguments: expected 3, got %d\n", len(msg.Arguments))
			}
			return // Message handled
		}
	})
	if err != nil {
		log.Fatalf("Error adding OSC message handler: %v", err)
	}

	serverAddr := "127.0.0.1:9090"
	server := &osc.Server{
		Addr:       serverAddr,
		Dispatcher: dispatcher,
	}

	fmt.Printf("Mock OSC Server running and listening on udp://%s\n", serverAddr)
	fmt.Printf("Handles SET/UPDATE on: /strip/Sooper<ID>/Gain/Gain%%20(dB) <float32_value>\n")
	fmt.Printf("Handles GET on: /get_strip_gain <int32_loopID_1based> <string_returnURL> <string_replyPath>\n")

	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("Error starting OSC server: %v", err)
	}
}
