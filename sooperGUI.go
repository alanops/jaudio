/*
This is a Golang script.
It creates a Terminal User Interface (TUI) for SooperLooper, a live looping audio sampler.
The TUI displays status information and allows some control, primarily output levels.
*/

// The 'package main' declaration indicates that this file will compile into an executable program,
// rather than a library to be used by other programs. Every Go executable must have a 'main' package
// and a 'main()' function.
package main

// The 'import' keyword is used to include packages that provide additional functionality.
// Go's standard library offers many useful packages. Third-party packages can also be imported.
import (
	"flag"  // Provides support for command-line flag parsing.
	"fmt"   // Implements formatted I/O (like C's printf and scanf).
	"log"   // Implements simple logging.
	"math"  // Provides basic mathematical constants and functions.
	"net"   // Provides a portable interface for network I/O, including TCP/IP, UDP, domain name resolution, and Unix domain sockets.
	"os"    // Provides a platform-independent interface to operating system functionality.
	"os/exec" // Provides functions for running external commands.
	"regexp" // Added for regular expression matching of OSC paths.
	"strconv" // Implements conversions to and from string representations of basic data types.
	"strings" // Implements simple functions to manipulate UTF-8 encoded strings.
	"sync"    // Provides basic synchronization primitives such as mutual exclusion locks (mutexes).
	"syscall" // Contains an interface to the low-level operating system primitives.
	"time" // Provides time-related functionality.

	// Third-party packages:
	// These are not part of Go's standard library and need to be fetched (e.g., using 'go get').
	// The paths here usually refer to their location on a code hosting platform like GitHub.

	// tcell is a TUI library for Go, providing a cell-based view of the terminal.
	// It handles low-level terminal interactions, input events, and screen drawing.
	"github.com/gdamore/tcell/v2"
	// go-osc is a library for Open Sound Control (OSC) communication in Go.
	// OSC is a protocol for communication among computers, sound synthesizers, and other multimedia devices.
	"github.com/hypebeast/go-osc/osc"
	// tview is a rich interactive widget library for terminal-based user interfaces, built on top of tcell.
	// It provides higher-level components like tables, forms, lists, etc.
	"github.com/rivo/tview"
)

// --- Struct Definitions ---
// A 'struct' is a composite data type that groups together zero or more named values (fields) of arbitrary types.
// It's similar to a class or struct in other languages, but Go structs only contain data, not methods directly (methods are defined separately with receivers).

// LoopState holds the current state and meter information for a single SooperLooper loop.
type LoopState struct {
	State        int     // Current state of the loop (e.g., playing, recording, muted). SooperLooper uses integer codes for states.
	NextState    int     // The state the loop will transition to next (e.g., if a state change is pending).
	LoopPos      float32 // Current playback/recording position within the loop (often a normalized value 0.0 to 1.0).
	InPeakMeter  float32 // Peak level of the audio input for this loop.
	OutPeakMeter float32 // Peak level of the audio output for this loop.
	Wet          float32 // The output level (volume) of the loop, often called "wet" signal.
}

// ButtonState defines the visual state and conditions for TUI buttons (like Record, Overdub, Mute).
type ButtonState struct {
	OnStates      []int // A slice (dynamically-sized array) of SooperLooper state codes that mean this button should appear "ON".
	// PendingOnCond is a function that determines if the button should appear as "pending ON" (e.g., yellow).
	// It takes the current loop state and next loop state as arguments and returns true if the condition is met.
	// Functions can be types in Go, allowing them to be stored in struct fields or passed as arguments.
	PendingOnCond func(state, next int) bool
	// PendingOffCond is a function that determines if the button should appear as "pending OFF".
	PendingOffCond func(state, next int) bool
}

// --- Global Configuration and State Variables ---
// The 'var' keyword declares variables. This block declares global variables for the application.
// Global variables are generally discouraged for complex state, but can be acceptable for configuration
// or truly global singletons if managed carefully (e.g., with mutexes for concurrent access).
var (
	// Regex for parsing the custom strip gain path.
	// It captures the numeric ID from "/strip/Sooper<ID>/Gain/Gain (dB)".
	stripGainPathRegex = regexp.MustCompile(`^/strip/Sooper(\d+)/Gain/Gain \(dB\)$`)

	// oscHost is the IP address or hostname of the SooperLooper OSC server.
	// Default is "127.0.0.1" (localhost), meaning SooperLooper is expected to be running on the same machine.
	oscHost = "127.0.0.1"
	// oscPort is the UDP port SooperLooper is listening on for OSC messages.
	oscPort = 9951
	// refreshRate is how often the TUI will update its display, in milliseconds.
	refreshRate = 200

	// loopCount is the number of loops SooperLooper reports it has.
	// It's initialized to 1 and updated when a '/pong' message is received from SooperLooper.
	loopCount = 1
	// loopStates is a map (Go's hash table or dictionary type) that stores the LoopState for each loop.
	// The key is the loop index (int), and the value is a pointer to a LoopState struct (*LoopState).
	// 'make' is a built-in function to initialize maps, slices, and channels.
	loopStates = make(map[int]*LoopState)
	// mu is a 'sync.Mutex', a mutual exclusion lock.
	// It's used to protect shared data (like loopStates) from concurrent access by multiple goroutines,
	// preventing race conditions.
	mu sync.Mutex
	// client is the OSC client used to send messages to SooperLooper.
	// It's a pointer to an osc.Client struct.
	client *osc.Client

	// infoLog and errorLog are custom loggers.
	// log.New creates a new logger. os.Stdout and os.Stderr are standard output and standard error file descriptors.
	// The prefix ("INFO: ", "ERROR: ") and flags (log.Ldate|log.Ltime for date and time) configure the log output.
	infoLog  = log.New(os.Stdout, "INFO: ", log.Ldate|log.Ltime)
	errorLog = log.New(os.Stderr, "ERROR: ", log.Ldate|log.Ltime)

	// Thresholds for coloring meter bars in the TUI.
	greenThreshold  float32 = 0.7 // Values below this are green.
	yellowThreshold float32 = 0.9 // Values below this (but >= greenThreshold) are yellow.
	redThreshold    float32 = 1.0 // Values at or above yellowThreshold (effectively) are red.

	// Meter range in decibels (dB) for display calculations.
	meterMinDB = -70.0 // The lowest dB value the meter can show.
	meterMidDB = -16.0 // A reference mid-point, not directly used in bar calculation but good for context.
	meterMaxDB = 0.0   // The highest dB value (0dBFS is typically clipping).

	// Pointers to boolean values that will be set by command-line flags.
	// Using pointers allows the flag package to modify these variables directly.
	// The actual boolean values are allocated by the flag package.
	debugFlag      *bool // Enables verbose debug logging if true.
	stateDebugFlag *bool // Shows an extra state debugging column in the TUI if true.
)

// main is the entry point of the application. When the program is run, the main function is executed.
func main() {
	// --- Parse command-line arguments ---
	// The 'flag' package is used to define and parse command-line options (flags).

	// flag.StringVar defines a string flag.
	// It takes a pointer to the variable to store the flag's value (&oscHost),
	// the flag name ("osc-host"), the default value (oscHost, its current global value),
	// and a help message.
	flag.StringVar(&oscHost, "osc-host", oscHost, "OSC host (default: 127.0.0.1)")
	// flag.IntVar defines an integer flag.
	flag.IntVar(&oscPort, "osc-port", oscPort, "OSC UDP port (default: 9951)")
	flag.IntVar(&refreshRate, "refresh-rate", refreshRate, "TUI refresh rate in milliseconds (default: 200)")

	// flag.Bool defines a boolean flag. It returns a pointer to a boolean.
	// This is why debugFlag and stateDebugFlag are declared as *bool.
	debugFlag = flag.Bool("debug", false, "Enable debug logging to parent terminal")
	stateDebugFlag = flag.Bool("state-debug", false, "Show state debug column in the TUI")

	// Defines a boolean flag for showing help.
	help := flag.Bool("help", false, "Show help message")
	// Defines a shorthand version "-h" for the help flag.
	flag.BoolVar(help, "h", false, "Show help message (shorthand)")

	// flag.Parse() parses the command-line arguments from os.Args[1:]
	// and sets the values of the defined flags.
	flag.Parse()

	// If the help flag was provided (*help dereferences the pointer to get the boolean value).
	if *help {
		// fmt.Printf prints a formatted string to standard output.
		// The backticks ` ` create a raw string literal, preserving newlines and special characters.
		fmt.Printf(`Usage: sooperGUI [OPTIONS]
Options:
  --osc-host         OSC host (default: 127.0.0.1)
  --osc-port         OSC UDP port (default: 9951)
  --refresh-rate     TUI refresh rate in milliseconds (default: 100)
  --debug            Enable debug logging to parent terminal
  --state-debug      Show state debug column in the TUI
  --help, -h         Show this help message
`)
		// os.Exit(0) terminates the program with a success status code.
		os.Exit(0)
	}

	// --- Relaunch in a new st terminal if not already in one ---
	if os.Getenv("SOOPERGUI_XTERM") == "" { // Check an environment variable
		self, err := os.Executable() // Get path to current executable
		if err != nil {
			errorLog.Fatalf("Cannot find executable: %v", err) // Log error and exit
		}
		args := os.Args[1:] // Get original command-line arguments
		env := append(os.Environ(), "SOOPERGUI_XTERM=1") // Add env var for the new process
		// Prepare to run 'st' terminal
		cmd := exec.Command("st", "-f", "monospace:size=10", "-c", "sooperGUI", "-e", self)
		cmd.Args = append(cmd.Args, args...)
		cmd.Env = env
		cmd.Stdout = os.Stdout // Redirect standard streams
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin
		infoLog.Println("Launching new st window for GUI...")
		if err := cmd.Start(); err != nil { // Start the command
			errorLog.Fatalf("Failed to launch st: %v", err)
		}
		// Goroutine to send a SIGWINCH signal, possibly to fix terminal sizing.
		go func() {
			time.Sleep(1 * time.Second)
			if cmd.Process != nil {
				cmd.Process.Signal(syscall.SIGWINCH)
			}
		}()
		cmd.Wait() // Wait for the 'st' process to exit
		os.Exit(0) // Exit this parent process
	}

	// --- In the child process: set up logging and terminal colors ---
	// This section is for when the program was relaunched inside 'st'.
	// It sets terminal colors and redirects logging back to the original parent terminal.
	if os.Getenv("SOOPERGUI_XTERM") != "" {
		fmt.Print("\033]10;#00FF00\007\033]11;#000000\007") // ANSI escape codes for colors
		ppid := os.Getppid() // Get parent process ID
		// Try to open parent's stdout/stderr via /proc filesystem (Linux-specific)
		parentStdout, _ := os.OpenFile(fmt.Sprintf("/proc/%d/fd/1", ppid), os.O_WRONLY, 0)
		parentStderr, _ := os.OpenFile(fmt.Sprintf("/proc/%d/fd/2", ppid), os.O_WRONLY, 0)
		if parentStdout != nil {
			infoLog.SetOutput(parentStdout)
		}
		if parentStderr != nil {
			errorLog.SetOutput(parentStderr)
		}
	}

	// --- Allocate a UDP port for OSC replies ---
	// SooperLooper needs a return address to send updates back to this GUI.
	// We listen on a dynamically allocated UDP port for these replies.
	// net.ListenPacket is used for connectionless protocols like UDP.
	// ":0" means "listen on any available local IP address, on any available port".
	listener, err := net.ListenPacket("udp", ":0")
	if err != nil {
		// errorLog.Fatalf logs the message and then calls os.Exit(1).
		errorLog.Fatalf("Failed to allocate UDP port: %v", err)
	}
	// 'defer' schedules a function call (listener.Close()) to be run just before the surrounding function (main) returns.
	// This is a common Go idiom for ensuring resources are cleaned up.
	defer listener.Close()

	// Get the local address and port that was actually allocated.
	// listener.LocalAddr() returns a net.Addr. We need to assert its type to *net.UDPAddr to get the port.
	// This is a type assertion: value.(TypeName). It panics if the assertion fails.
	localAddr := listener.LocalAddr().(*net.UDPAddr)
	localPort := localAddr.Port // The dynamically allocated port number.

	// Determine the IP address to use for the return URL.
	returnIP := getLocalIP(oscHost)
	// Construct the OSC return URL string (e.g., "osc.udp://192.168.1.10:12345").
	// fmt.Sprintf formats a string according to a format specifier and returns the resulting string.
	returnURL := fmt.Sprintf("osc.udp://%s:%d", returnIP, localPort)

	// --- Set up OSC client and server ---
	// Create an OSC client to send messages to SooperLooper.
	client = osc.NewClient(oscHost, oscPort)
	infoLog.Printf("Connecting to SooperLooper OSC at %s:%d", oscHost, oscPort)

	// Create an OSC dispatcher. A dispatcher routes incoming OSC messages to handler functions
	// based on their OSC address patterns.
	dispatcher := osc.NewStandardDispatcher()
	// Add a message handler for all incoming OSC messages ("*").
	// The handler is an anonymous function (a closure) that takes an *osc.Message.
	dispatcher.AddMsgHandler("*", func(msg *osc.Message) {
		// If the debug flag is enabled, log the incoming message.
		// *debugFlag dereferences the pointer to get the boolean value.
		if *debugFlag {
			infoLog.Printf("OSC IN: %s %v", msg.Address, msg.Arguments)
		}
		// Pass the message to the handleOSC function for processing.
		handleOSC(msg)
	})

	// Create an OSC server to listen for messages from SooperLooper.
	server := &osc.Server{
		Addr:       fmt.Sprintf(":%d", localPort), // Listen on our dynamically allocated port.
		Dispatcher: dispatcher,                   // Use the dispatcher we configured.
	}

	// Start the OSC server in a new goroutine.
	// A goroutine is a lightweight thread managed by the Go runtime.
	// The 'go' keyword starts a function call in a new goroutine.
	// This allows the OSC server to listen for messages concurrently without blocking the main thread.
	go func() {
		infoLog.Printf("OSC server listening on udp://%s:%d", returnIP, localPort)
		// server.Serve takes the net.PacketConn (our listener) and starts serving.
		// This is a blocking call, so it runs in its own goroutine.
		if err := server.Serve(listener); err != nil {
			errorLog.Fatalf("OSC server error: %v", err)
		}
	}()

	// Send an initial ping to SooperLooper to establish communication and get loop count.
	sendPing(client, returnURL)

	// --- Register for automatic updates for each loop and each control ---
	// This loop initially runs for loopCount=1. The actual loopCount is updated
	// later when SooperLooper responds to the ping.
	// A more robust approach might wait for the actual loopCount before this.
	for i := 0; i < loopCount; i++ {
		registerAutoUpdate(client, i, "loop_pos", returnURL, debugFlag)
		registerAutoUpdate(client, i, "in_peak_meter", returnURL, debugFlag)
		registerAutoUpdate(client, i, "out_peak_meter", returnURL, debugFlag)
		// registerAutoUpdate(client, i, "wet", returnURL, debugFlag) // Removed: "wet" is now handled by /strip/... path
		// Also poll for the initial 'wet' value.
		// pollControl(client, i, "wet", returnURL, debugFlag) // Removed: "wet" is now handled by /strip/... path
	}

	// --- Poll for state and wet value at the user-configured refreshRate ---
	// This goroutine periodically asks SooperLooper for certain values.
	// This is a fallback or supplement to the auto-update mechanism.
	go func() {
		// 'for {}' is an infinite loop in Go.
		for {
			for i := 0; i < loopCount; i++ {
				pollControl(client, i, "state", returnURL, debugFlag)
				pollControl(client, i, "next_state", returnURL, debugFlag)
				// pollControl(client, i, "wet", returnURL, debugFlag) // Removed: "wet" is now handled by /strip/... path
			}
			// time.Sleep pauses the current goroutine for at least the specified duration.
			time.Sleep(time.Duration(refreshRate) * time.Millisecond)
		}
	}()

	// --- Set up the TUI using tview ---
	// Create a new tview application. This is the root of the TUI.
	app := tview.NewApplication()
	// Create a new table widget. SetBorders(true) draws borders around cells.
	// SetFixed(1, 0) fixes the first row (header) and zero columns during scrolling.
	table := tview.NewTable().SetBorders(true).SetFixed(1, 0)

	// screenWidth will store the width of the terminal screen.
	var screenWidth int = 80 // Default value.
	// SetBeforeDrawFunc registers a function to be called before the screen is drawn.
	// This can be used to get screen dimensions or make last-minute adjustments.
	// It receives a tcell.Screen object.
	app.SetBeforeDrawFunc(func(screen tcell.Screen) bool {
		w, _ := screen.Size() // Get current screen width and height.
		screenWidth = w       // Update our global screenWidth.
		return false          // Return false to indicate no changes were made that require a redraw.
	})

	// SetInputCapture registers a function to intercept all keyboard input events.
	// This is used here to handle Ctrl+C gracefully (by doing nothing, as per user request).
	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyCtrlC { // Check if Ctrl+C was pressed.
			// app.Stop() // This would normally be used to stop the application.
			return nil // Returning nil consumes the event, so tview doesn't process it further (e.g., by quitting).
		}
		return event // Return the event to allow tview to process it normally.
	})

	// buttonDefs defines the properties for different control buttons in the TUI.
	// It's a map where the key is the button name (string) and the value is a ButtonState struct.
	buttonDefs := map[string]ButtonState{
		"RECORD": { // Configuration for the "RECORD" button.
			OnStates: []int{2, 3}, // Loop states where "RECORD" is considered ON.
			PendingOnCond: func(state, next int) bool { // Condition for "pending ON" (e.g., about to record).
				return state == 1 && (next == 4 || next == -1) // State 1 is 'Waiting', 4 is 'Playing', -1 might be 'Cancel Pending'.
			},
			PendingOffCond: func(state, next int) bool { // Condition for "pending OFF" (e.g., about to stop recording).
				return (state == 2 || state == 3) && next == 4 // States 2,3 are Rec/Overdub, 4 is Play.
			},
		},
		"OVERDUB": {
			OnStates:      []int{5}, // State 5 is 'Overdubbing'.
			PendingOnCond: func(state, next int) bool { return state == 4 && next == 5 },
			PendingOffCond: func(state, next int) bool { return state == 5 && next == 4 },
		},
		"MUTE": {
			OnStates:      []int{10, 20}, // States 10, 20 are Mute variations.
			PendingOnCond: func(state, next int) bool { return state == 4 && next == 10 },
			PendingOffCond: func(state, next int) bool { return (state == 10 || state == 20) && next == 4 },
		},
	}

	// updateTable is a function (closure) responsible for redrawing the entire TUI table.
	// It's called periodically and when OSC messages update the state.
	updateTable := func() {
		// Lock the mutex to ensure exclusive access to shared data (loopStates, loopCount, screenWidth).
		// This prevents race conditions if another goroutine tries to modify these while the table is being updated.
		mu.Lock()
		// 'defer mu.Unlock()' ensures the mutex is unlocked when updateTable returns, even if a panic occurs.
		defer mu.Unlock()

		// Define table headers.
		headers := []string{
			"ID", "Rec", "Dub", "Mute", "Pos", "Meter In", "Meter Out", "Level",
		}
		// Define fixed widths for some columns.
		fixedColWidths := []int{5, 8, 8, 8, 9} // Corresponds to ID, Rec, Dub, Mute, Pos.

		debugCol := *stateDebugFlag // Check if the state debug column should be shown.
		if debugCol {
			headers = append(headers, "State Debug")        // Add header for debug column.
			fixedColWidths = append(fixedColWidths, 14) // Add fixed width for debug column.
		}
		numCols := len(headers)

		table.Clear() // Clear all existing cells from the table.

		// Calculate widths for the meter columns (Meter In, Meter Out, Level).
		// These columns will share the remaining screen space.
		fixedTotal := 0 // Sum of widths of columns that are not meters.
		for i := 0; i < numCols; i++ {
			// Columns 5, 6, 7 are meter columns (0-indexed).
			if i == 5 || i == 6 || i == 7 {
				continue // Skip meter columns for fixedTotal calculation.
			}
			if i < len(fixedColWidths) {
				fixedTotal += fixedColWidths[i]
			}
		}
		meterCols := 3 // Number of meter columns.
		// Calculate total width available for all meter columns.
		// (screenWidth - fixedTotal width - (numCols-1)*1 for borders between columns)
		meterWidth := (screenWidth - fixedTotal - (numCols-1)*1)
		if meterWidth < 3*len("Meter In") { // Ensure a minimum width.
			meterWidth = 3 * len("Meter In")
		}
		meterWidthEach := meterWidth / meterCols // Distribute width equally among meter columns.

		// Style for header cells (bold).
		bold := tcell.StyleDefault.Bold(true)
		// Populate header row.
		for i, h := range headers {
			var padded string
			align := tview.AlignCenter
			if i >= 5 && i <= 7 { // Meter columns
				padded = h
				align = tview.AlignCenter
			} else if i <= 4 { // Fixed width columns before meters
				padded = " " + h + " " // Add padding.
			} else { // Debug column
				padded = h
			}

			w := getColWidth(i, fixedColWidths, meterWidthEach) // Get specific width for this column.
			cell := tview.NewTableCell(padded).
				SetSelectable(false). // Headers are not selectable.
				SetStyle(bold).
				SetMaxWidth(w). // Important for layout.
				SetAlign(align)

			if i >= 5 && i <= 7 { // Meter columns
				cell.SetExpansion(1) // Allow meter columns to expand to fill space.
			} else {
				cell.SetExpansion(0) // Other columns have fixed expansion.
			}
			table.SetCell(0, i, cell) // Add cell to table at row 0, column i.
		}

		// Populate data rows for each loop.
		for i := 0; i < loopCount; i++ {
			ls := loopStates[i] // Get the state for the current loop.
			if ls == nil {      // If no state exists yet (e.g., before first OSC update).
				ls = &LoopState{} // Use an empty LoopState to avoid nil pointer errors.
			}
			row := i + 1 // Table data rows start from 1 (row 0 is header).

			// Column 0: Loop ID
			table.SetCell(row, 0, tview.NewTableCell(" "+strconv.Itoa(i+1)+" ").SetMaxWidth(fixedColWidths[0]).SetExpansion(0).SetAlign(tview.AlignCenter))
			// Column 1: Record Button State
			table.SetCell(row, 1, buttonStateCell(ls.State, ls.NextState, fixedColWidths[1], buttonDefs["RECORD"]))
			// Column 2: Overdub Button State
			table.SetCell(row, 2, buttonStateCell(ls.State, ls.NextState, fixedColWidths[2], buttonDefs["OVERDUB"]))
			// Column 3: Mute Button State
			table.SetCell(row, 3, buttonStateCell(ls.State, ls.NextState, fixedColWidths[3], buttonDefs["MUTE"]))
			// Column 4: Loop Position
			table.SetCell(row, 4, tview.NewTableCell(fmt.Sprintf(" %.2f ", ls.LoopPos)).SetMaxWidth(fixedColWidths[4]).SetAlign(tview.AlignCenter).SetExpansion(0))
			// Column 5: Input Peak Meter
			table.SetCell(row, 5, meterBarCell(ls.InPeakMeter, meterWidthEach))
			// Column 6: Output Peak Meter
			table.SetCell(row, 6, meterBarCell(ls.OutPeakMeter, meterWidthEach))
			// Column 7: Wet Level Meter
			table.SetCell(row, 7, meterBarCell(ls.Wet, meterWidthEach)) // Using meterBarCell for consistency, could be levelBarCell if different style needed

			// Column 8 (Optional): State Debug
			if debugCol {
				table.SetCell(row, 8, tview.NewTableCell(fmt.Sprintf("S:%d N:%d", ls.State, ls.NextState)).SetMaxWidth(fixedColWidths[5]).SetExpansion(0).SetAlign(tview.AlignCenter))
			}
		}
	}

	// SetInputCapture for the table (currently just returns the event, can be used for table-specific keybindings).
	table.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey { return event })
	// SetMouseCapture for the table to handle mouse clicks, specifically for the "Level" column.
	table.SetMouseCapture(func(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
		// Check if the action is a left click, mouse down, or mouse move (for dragging).
		if action == tview.MouseLeftClick || action == tview.MouseLeftDown || action == tview.MouseMove {
			x, y := event.Position()      // Get mouse coordinates relative to the screen.
			row, col := table.CellAt(x, y) // Get the table cell (row, col) at these coordinates.

			if row == 0 { // Clicked on header row.
				return action, event // Do nothing with header clicks for now.
			}

			// GetLastPosition returns the x, y coordinates of the cell's content area (top-left)
			// and the width of the content area. This might be smaller than the full cell
			// if there's padding or if the cell text doesn't fill the MaxWidth.
			cellContentX, _, cellContentWidth := table.GetCell(row, col).GetLastPosition()

			// Check if the click is on the "Level" column (column 7) and within a valid loop row.
			if row > 0 && col == 7 && row <= loopCount {
				// mouseXrelative is the click position relative to the start of the cell's content area.
				mouseXrelative := x - cellContentX
				var fill float32 // 'fill' represents the proportion of the bar clicked (0.0 to 1.0).
				if cellContentWidth > 0 {
					fill = float32(mouseXrelative) / float32(cellContentWidth)
				} else {
					fill = 0 // Avoid division by zero if cell width is 0.
				}

				// Cap 'fill' to ensure the resulting 'wet' value doesn't exceed 0.921.
				// This maxFill value is derived from the logarithmic conversion formula used for 'wet'.
				// max_fill_for_0_921_wet = (20*log10(0.921) - meterMinDB) / (meterMaxDB - meterMinDB)
				// This evaluates to approx 0.98978457.
				const maxFill = 0.98978457
				if fill > maxFill {
					fill = maxFill
				}
				if fill < 0 { // Ensure fill is not negative.
					fill = 0
				}

				var wet float32 // The calculated wet level (amplitude, 0.0 to ~0.921).
				// Convert the linear 'fill' value to a logarithmic 'wet' value (amplitude).
				// This formula maps the fill (0-1 range, effectively capped by maxFill)
				// to an amplitude scale based on meterMinDB and meterMaxDB.
				wet = float32(math.Pow(10, (float64(fill)*(meterMaxDB-meterMinDB)+meterMinDB)/20.0))

				// Ensure 'wet' is strictly capped at 0.921 due to potential floating point inaccuracies.
				const maxWet = 0.921
				if wet > maxWet {
					wet = maxWet
				}
				if wet < 0 { // Should not happen if fill is non-negative.
					wet = 0
				}

				if *debugFlag {
					infoLog.Printf("Mouse: x=%d, y=%d | Cell: r=%d, c=%d | RelX=%d, cellContentW=%d | Fill=%.4f, Wet=%.4f", x, y, row, col, mouseXrelative, cellContentWidth, fill, wet)
				}

				// Update the local state for immediate TUI feedback.
				mu.Lock() // Lock mutex before accessing shared loopStates.
				if loopStates[row-1] != nil { // loopStates is 0-indexed, table 'row' is 1-indexed.
					loopStates[row-1].Wet = wet
				}
				mu.Unlock() // Unlock mutex.

				// Send OSC message for level control.
				go func(loopID int, valueToSend float32) {
					// Construct the OSC address. Note: SooperLooper loop IDs are typically 0-indexed in OSC paths like /sl/0/set
					// However, the new endpoint is specified as /strip/Sooper<ID>/Gain/Gain (dB) where ID is 1-based.
					// We use 'row' which is 1-based from the table.
					oscAddress := fmt.Sprintf("/strip/Sooper%d/Gain/Gain (dB)", loopID)
					msg := osc.NewMessage(oscAddress)
					msg.Append(valueToSend) // Append the float value.

					// Use the global OSC client.
					// Ensure 'client' is initialized and available.
					// The client is configured with oscHost and oscPort from flags/defaults.
					if client != nil {
						err := client.Send(msg)
						if err != nil {
							errorLog.Printf("Error sending OSC message to %s for loop %d: %v", oscAddress, loopID, err)
						} else if *debugFlag {
							infoLog.Printf("OSC OUT to %s with value %.4f", oscAddress, valueToSend)
						}
					} else {
						errorLog.Println("OSC client is not initialized. Cannot send level update.")
					}
				}(row, wet) // Pass current 'row' (1-based loopID) and 'wet' value to the goroutine.
				return action, event // Event handled.
			}
		}
		return action, event // Event not handled by this specific logic, pass it on.
	})

	// Goroutine to periodically request a redraw of the TUI.
	// app.QueueUpdateDraw is a thread-safe way to tell tview to redraw the UI.
	go func() {
		for {
			app.QueueUpdateDraw(updateTable) // Schedule updateTable to be run in the main tview goroutine.
			time.Sleep(time.Duration(refreshRate) * time.Millisecond)
		}
	}()

	infoLog.Println("TUI launched. Ctrl+C will do nothing as requested by input capture.")
	// Set the table as the root widget of the application and run the TUI event loop.
	// EnableMouse(true) allows tview to process mouse events.
	// Run() is a blocking call; it will only return when the application quits (e.g., via app.Stop()).
	if err := app.SetRoot(table, true).EnableMouse(true).Run(); err != nil {
		errorLog.Fatalf("TUI error: %v", err)
	}
} // End of main function

// --- Helper functions for rendering TUI elements ---

// meterBarCell creates a tview.TableCell representing a colored meter bar.
// 'val' is the current amplitude (0.0-1.0 range).
// 'width' is the desired character width of the bar in the TUI.
func meterBarCell(val float32, width int) *tview.TableCell {
	// Convert amplitude to a fill percentage for the meter (0.0-1.0).
	fill := amplitudeToMeterFill(val, meterMinDB, meterMaxDB)
	// Calculate how many characters of the bar should be "full".
	// math.Ceil rounds up to ensure even small values show at least one block if width allows.
	fullChars := int(math.Ceil(float64(fill) * float64(width)))
	if fullChars > width { // Cap at the maximum width.
		fullChars = width
	}
	if fullChars < 0 { // Ensure it's not negative.
		fullChars = 0
	}


	var color tcell.Color // tcell.Color defines terminal colors.
	// Determine color based on fill percentage and predefined thresholds.
	switch {
	case fill < greenThreshold:
		color = tcell.ColorGreen
	case fill < yellowThreshold:
		color = tcell.ColorYellow
	default: // fill >= yellowThreshold
		color = tcell.ColorRed
	}

	// Create the bar string using block characters (█) and spaces.
	// strings.Repeat repeats a string n times.
	bar := strings.Repeat("█", fullChars) + strings.Repeat(" ", width-fullChars)
	// Create and return a new table cell with the bar, color, and alignment.
	return tview.NewTableCell(bar).SetTextColor(color).SetAlign(tview.AlignLeft)
}

// levelBarCell creates a tview.TableCell for a level control, showing a bar and a handle.
// This function was originally distinct but now meterBarCell is used for level display too.
// It could be adapted if a different visual style (e.g., with a handle '│') is desired for level.
func levelBarCell(wet float32, width int) *tview.TableCell {
	if wet < 0.00001 { // Avoid log10(0) or log10 of very small numbers.
		wet = 0.00001
	}
	// Convert wet amplitude to dB.
	db := 20.0 * math.Log10(float64(wet))
	// Normalize dB value to a 0-1 fill range based on meterMinDB and meterMaxDB.
	fill := float32((db - meterMinDB) / (meterMaxDB - meterMinDB))
	if fill < 0 {
		fill = 0
	}
	if fill > 1 {
		fill = 1
	}

	fullChars := int(fill * float32(width)) // Number of '█' characters.
	if fullChars > width {
		fullChars = width
	}
	if fullChars < 0 {
		fullChars = 0
	}

	var color tcell.Color
	switch {
	case fill < greenThreshold:
		color = tcell.ColorGreen
	case fill < yellowThreshold:
		color = tcell.ColorYellow
	default:
		color = tcell.ColorRed
	}

	// Logic for placing a 'handle' character ('│') in the bar.
	handlePos := 0
	if width > 1 {
		// Position handle based on fill, ensuring it's within the bar width.
		handlePos = int(fill*float32(width-1) + 0.5) // +0.5 for rounding.
	}
	if handlePos >= width {
		handlePos = width -1
	}
	if handlePos < 0 {
		handlePos = 0
	}


	barChars := make([]rune, width) // Use a slice of runes for building the bar string.
	for i := 0; i < width; i++ {
		if i == handlePos {
			barChars[i] = '│' // Handle character.
		} else if i < fullChars {
			barChars[i] = '█' // Full part of the bar.
		} else {
			barChars[i] = ' ' // Empty part of the bar.
		}
	}
	return tview.NewTableCell(string(barChars)).SetTextColor(color).SetAlign(tview.AlignLeft)
}

// amplitudeToMeterFill converts an amplitude value (typically 0.0-1.0+) to a normalized fill value (0.0-1.0)
// for meter display, based on a dB scale.
func amplitudeToMeterFill(val float32, minDB, maxDB float64) float32 {
	if val < 0.00001 { // Treat very small amplitudes as zero fill to avoid log(0).
		return 0
	}
	// Convert linear amplitude to decibels (dB). 20*log10(amplitude).
	db := 20.0 * math.Log10(float64(val))

	// Clamp dB value to the defined meter range [minDB, maxDB].
	if db < minDB {
		db = minDB
	}
	if db > maxDB {
		db = maxDB
	}
	// Normalize the clamped dB value to a 0.0-1.0 range.
	// This represents the "fill percentage" of the meter.
	return float32((db - minDB) / (maxDB - minDB))
}

// getColWidth determines the width for a given table column.
// It uses fixed widths for some columns and distributes remaining space for meter columns.
func getColWidth(col int, fixedColWidths []int, meterWidthEach int) int {
	// Columns 5, 6, 7 are meter columns.
	if col == 5 || col == 6 || col == 7 {
		return meterWidthEach
	}
	// Check if the column index is within the bounds of fixedColWidths.
	if col < len(fixedColWidths) {
		return fixedColWidths[col]
	}
	return 10 // Default width for any other columns (e.g., debug column if not in fixedColWidths).
}

// --- OSC/network helper functions ---

// getLocalIP attempts to find a suitable local IP address for OSC return messages.
// If oscHost is localhost, it returns 127.0.0.1. Otherwise, it tries to find a non-loopback IPv4 address.
func getLocalIP(oscHost string) string {
	if oscHost == "127.0.0.1" || oscHost == "localhost" {
		return "127.0.0.1"
	}
	// net.InterfaceAddrs() returns a list of the system's network interface addresses.
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "127.0.0.1" // Fallback on error.
	}
	// Iterate over all network interface addresses.
	for _, addr := range addrs {
		// Type assert the address to *net.IPNet to access IP details.
		if ipnet, ok := addr.(*net.IPNet); ok && // Check if assertion is successful.
			!ipnet.IP.IsLoopback() && // Check if it's not a loopback address (e.g., 127.0.0.1).
			ipnet.IP.To4() != nil { // Check if it's an IPv4 address.
			return ipnet.IP.String() // Return the first suitable IPv4 address found.
		}
	}
	return "127.0.0.1" // Fallback if no suitable address is found.
}

// waitForEngine sends a ping and waits, but doesn't actually check for a response.
// This function seems incomplete or was intended for a different purpose.
// Currently, it just sends a ping and sleeps.
func waitForEngine(client *osc.Client, returnURL string, timeout time.Duration) bool {
	msg := osc.NewMessage("/ping") // Create a new OSC message with address "/ping".
	msg.Append(returnURL)          // Append the return URL for SooperLooper to reply to.
	msg.Append("/pong")            // Append the OSC address SooperLooper should use in its reply.
	if *debugFlag {
		infoLog.Printf("OSC OUT: /ping %v", msg.Arguments)
	}
	_ = client.Send(msg) // Send the message. The error is ignored here.
	time.Sleep(timeout)  // Pause for the specified timeout duration.
	return true          // Always returns true, doesn't confirm engine readiness.
}

// sendPing sends a "/ping" OSC message to SooperLooper.
func sendPing(client *osc.Client, returnURL string) {
	msg := osc.NewMessage("/ping")
	msg.Append(returnURL)
	msg.Append("/pong") // SooperLooper is expected to reply with a message to "/pong" at our returnURL.
	if *debugFlag {
		infoLog.Printf("OSC OUT: /ping %v", msg.Arguments)
	}
	_ = client.Send(msg) // Error ignored.
}

// registerAutoUpdate sends an OSC message to SooperLooper to request automatic updates for a specific control.
// 'loop' is the loop index, 'control' is the name of the parameter (e.g., "loop_pos").
// 'returnURL' is where SooperLooper should send updates.
// 'debugFlag' is a pointer to the global debug flag.
func registerAutoUpdate(client *osc.Client, loop int, control string, returnURL string, debugFlag *bool) {
	// retPath is the OSC address pattern SooperLooper will use for updates for this specific control.
	retPath := fmt.Sprintf("/sl/%d/update_%s", loop, control)
	// Message to tell SooperLooper to register for auto-updates.
	msg := osc.NewMessage(fmt.Sprintf("/sl/%d/register_auto_update", loop))
	msg.Append(control)    // The control name.
	msg.Append(int32(100)) // Update interval in milliseconds (e.g., 100ms).
	msg.Append(returnURL)  // Our OSC server URL.
	msg.Append(retPath)    // The path for updates.
	if *debugFlag {        // Check debug flag value by dereferencing the pointer.
		infoLog.Printf("OSC OUT: %s %v", msg.Address, msg.Arguments)
	}
	_ = client.Send(msg) // Error ignored.
}

// pollControl sends an OSC message to SooperLooper to request the current value of a specific control.
// This is used for one-time polling, as opposed to continuous auto-updates.
func pollControl(client *osc.Client, loop int, control string, returnURL string, debugFlag *bool) {
	retPath := fmt.Sprintf("/sl/%d/update_%s", loop, control) // Path for the reply.
	// Message to get a control's value.
	msg := osc.NewMessage(fmt.Sprintf("/sl/%d/get", loop))
	msg.Append(control)
	msg.Append(returnURL)
	msg.Append(retPath)
	if *debugFlag {
		infoLog.Printf("OSC OUT: %s %v", msg.Address, msg.Arguments)
	}
	_ = client.Send(msg) // Error ignored.
}

// handleOSC is the main handler for all incoming OSC messages from SooperLooper.
// It uses a switch statement to process messages based on their OSC address.
func handleOSC(msg *osc.Message) {
	// Lock the mutex to protect shared state (loopStates, loopCount) during updates.
	mu.Lock()
	defer mu.Unlock() // Ensure mutex is unlocked when the function returns.

	// A 'switch' statement without an expression is an alternative way to write if-else-if chains.
	// Each 'case' contains a boolean expression.
	switch {
	// Handle updates for the custom /strip/Sooper<ID>/Gain/Gain (dB) path
	case stripGainPathRegex.MatchString(msg.Address):
		matches := stripGainPathRegex.FindStringSubmatch(msg.Address)
		if len(matches) > 1 {
			idStr := matches[1]
			loopID_1based, err := strconv.Atoi(idStr)
			if err == nil {
				loopIdx_0based := loopID_1based - 1 // Convert 1-based ID to 0-based for map key
				if loopIdx_0based >= 0 {
					if len(msg.Arguments) == 1 {
						if val, ok := msg.Arguments[0].(float32); ok {
							ls := getLoopState(loopIdx_0based)
							ls.Wet = val // Assuming this path controls what we display as "Wet"
							if *debugFlag {
								infoLog.Printf("OSC IN (StripGain): Loop %d, Address %s, Wet set to %.4f", loopIdx_0based, msg.Address, val)
							}
						} else {
							if *debugFlag {
								errorLog.Printf("OSC IN (StripGain): Loop %d, Address %s, Arg not float32: %T", loopIdx_0based, msg.Address, msg.Arguments[0])
							}
						}
					} else {
						if *debugFlag {
							errorLog.Printf("OSC IN (StripGain): Loop %d, Address %s, Expected 1 arg, got %d", loopIdx_0based, msg.Address, len(msg.Arguments))
						}
					}
				}
			}
		}

	case msg.Address == "/pong": // Reply to our initial ping.
		// SooperLooper's /pong message arguments: [our_return_url, our_reply_path, loop_count, version_string, ...]
		if len(msg.Arguments) >= 3 {
			// Type assertion: try to convert the 3rd argument (index 2) to an int32.
			// The 'ok' variable will be true if the assertion succeeds.
			if c, ok := msg.Arguments[2].(int32); ok {
				loopCount = int(c) // Update the global loopCount.
				// Potentially, we might need to re-register auto-updates or poll controls
				// here if the loopCount has changed or was not 1 initially.
			}
		}
	// Cases for various update messages from SooperLooper.
	// strings.Contains checks if a substring exists within the message address.
	case strings.Contains(msg.Address, "/update_state"):
		loopIdx := parseLoopIndex(msg.Address) // Extract loop index from the address.
		// OSC update messages typically have arguments: [loop_index, control_name, value].
		if len(msg.Arguments) >= 3 {
			if idx, ok := msg.Arguments[0].(int32); ok && int(idx) == loopIdx { // Verify loop index.
				if ctrl, ok := msg.Arguments[1].(string); ok && ctrl == "state" { // Verify control name.
					if val, ok := msg.Arguments[2].(float32); ok { // Get state value.
						getLoopState(loopIdx).State = int(val) // Update local state.
					}
				}
			}
		}
	case strings.Contains(msg.Address, "/update_next_state"):
		loopIdx := parseLoopIndex(msg.Address)
		if len(msg.Arguments) >= 3 {
			if idx, ok := msg.Arguments[0].(int32); ok && int(idx) == loopIdx {
				if ctrl, ok := msg.Arguments[1].(string); ok && ctrl == "next_state" {
					if val, ok := msg.Arguments[2].(float32); ok {
						getLoopState(loopIdx).NextState = int(val)
					}
				}
			}
		}
	case strings.Contains(msg.Address, "/update_loop_pos"):
		loopIdx := parseLoopIndex(msg.Address)
		if len(msg.Arguments) >= 3 {
			if idx, ok := msg.Arguments[0].(int32); ok && int(idx) == loopIdx {
				if ctrl, ok := msg.Arguments[1].(string); ok && ctrl == "loop_pos" {
					if val, ok := msg.Arguments[2].(float32); ok {
						getLoopState(loopIdx).LoopPos = val
					}
				}
			}
		}
	case strings.Contains(msg.Address, "/update_in_peak_meter"):
		loopIdx := parseLoopIndex(msg.Address)
		if len(msg.Arguments) >= 3 {
			if idx, ok := msg.Arguments[0].(int32); ok && int(idx) == loopIdx {
				if ctrl, ok := msg.Arguments[1].(string); ok && ctrl == "in_peak_meter" {
					if val, ok := msg.Arguments[2].(float32); ok {
						getLoopState(loopIdx).InPeakMeter = val
					}
				}
			}
		}
	case strings.Contains(msg.Address, "/update_out_peak_meter"):
		loopIdx := parseLoopIndex(msg.Address)
		if len(msg.Arguments) >= 3 {
			if idx, ok := msg.Arguments[0].(int32); ok && int(idx) == loopIdx {
				if ctrl, ok := msg.Arguments[1].(string); ok && ctrl == "out_peak_meter" {
					if val, ok := msg.Arguments[2].(float32); ok {
						getLoopState(loopIdx).OutPeakMeter = val
					}
				}
			}
		}
	case strings.Contains(msg.Address, "/update_wet"):
		loopIdx := parseLoopIndex(msg.Address)
		if len(msg.Arguments) >= 3 {
			if idx, ok := msg.Arguments[0].(int32); ok && int(idx) == loopIdx {
				if ctrl, ok := msg.Arguments[1].(string); ok && ctrl == "wet" {
					// The 'wet' value might come as float32 or float64 from different OSC sources/versions.
					// A type switch handles different possible types for the value.
					switch val := msg.Arguments[2].(type) {
					case float32:
						getLoopState(loopIdx).Wet = val
					case float64: // If it's float64, convert to float32.
						getLoopState(loopIdx).Wet = float32(val)
					}
				}
			}
		}
	}
}

// parseLoopIndex extracts the loop index (integer) from an OSC address string.
// Example address: "/sl/0/update_state" -> extracts 0.
func parseLoopIndex(addr string) int {
	// strings.Split splits the string by "/" into a slice of substrings.
	parts := strings.Split(addr, "/") // E.g., ["", "sl", "0", "update_state"]
	if len(parts) > 2 {               // Ensure there are enough parts (at least "/sl/INDEX/...").
		// strconv.Atoi converts a string to an integer.
		// It returns the integer and an error. We check if err == nil.
		if idx, err := strconv.Atoi(parts[2]); err == nil {
			return idx // Return the parsed index.
		}
	}
	return 0 // Default or fallback index if parsing fails. Could be problematic if 0 is a valid loop.
}

// getLoopState retrieves the LoopState struct for a given loop index from the global 'loopStates' map.
// If a LoopState for the index doesn't exist yet, it creates and stores a new one (lazy initialization).
func getLoopState(idx int) *LoopState {
	if loopStates[idx] == nil { // Check if the map entry is nil (doesn't exist).
		loopStates[idx] = &LoopState{} // Create a new LoopState struct and store its pointer.
	}
	return loopStates[idx] // Return the pointer to the LoopState.
}

// buttonStateCell creates a tview.TableCell representing a button (e.g., Record, Mute).
// Its appearance (label "ON"/"OFF" and color) depends on the current and next loop states
// and the button's definition (def ButtonState).
func buttonStateCell(state, nextState, width int, def ButtonState) *tview.TableCell {
	label := ""
	color := tcell.ColorWhite // Default color.

	// Determine label and color based on conditions defined in ButtonState.
	switch {
	case def.PendingOnCond(state, nextState): // Check if "pending ON" condition is met.
		label = "ON"
		color = tcell.ColorYellow // Yellow for pending states.
	case def.PendingOffCond(state, nextState): // Check if "pending OFF" condition is met.
		label = "OFF"
		color = tcell.ColorYellow
	case containsInt(def.OnStates, state): // Check if current state is one of the "ON" states.
		label = "ON"
		color = tcell.ColorGreen // Green for active ON state.
	default: // Otherwise, the button is considered "OFF".
		label = "OFF"
		color = tcell.ColorRed // Red for OFF state.
	}
	// Create and return the table cell.
	return tview.NewTableCell(" "+label+" "). // Add padding to label.
						SetTextColor(color).
						SetAlign(tview.AlignCenter).
						SetMaxWidth(width)
}

// containsInt is a simple helper function to check if an integer 'val' exists in a slice of integers 'slice'.
func containsInt(slice []int, val int) bool {
	// 'for _, v := range slice' is Go's way to iterate over elements of a slice (or map, array, string).
	// '_' is the blank identifier, used when we don't need the index. 'v' gets the value of each element.
	for _, v := range slice {
		if v == val {
			return true // Value found.
		}
	}
	return false // Value not found after checking all elements.
}
