# Changelog

## [Date of Last Major Change - e.g., 2025-05-09] - OSC Control Restoration & ST Launch

### `mock_api.go`

*   **Protocol Reversion: HTTP to OSC Server**
    *   Converted the mock API entirely from an HTTP server back to an OSC server.
    *   Now listens for OSC messages on UDP port 9090 (e.g., `127.0.0.1:9090`).
    *   Specifically handles incoming OSC messages matching the address pattern `/strip/Sooper<ID>/Gain/Gain (dB)`.
    *   Extracts the SooperLooper ID (from the path) and a single float32 argument (gain value) from these messages.
    *   Logs the received OSC data to the console.
*   **Dependencies:**
    *   Removed HTTP-related packages (`io`, `net/http`).
    *   Added `github.com/hypebeast/go-osc/osc` for OSC server functionality.
    *   Kept `regexp` for path matching and `strconv` for ID conversion.

### `sooperGUI.go`

*   **Level Control Reverted to OSC (Send & Receive):**
    *   **Sending:** Mouse interactions on the "Level" column now send OSC messages to the target address `/strip/Sooper<ID>/Gain/Gain (dB)` (where `<ID>` is 1-based). The message contains a single `float32` argument representing the gain value.
    *   **Receiving:** Added logic to `handleOSC` to process incoming OSC messages that match the pattern `/strip/Sooper<ID>/Gain/Gain (dB)`. If such a message is received with a float argument, the GUI's internal "Wet" state (and thus the "Level" display) for the corresponding loop is updated. This allows for bidirectional updates if the target software sends messages on this path.
    *   Value capping for the level at `0.921` (and the corresponding `fill` value) remains in place.
*   **`st` Terminal Relaunch Restored:**
    *   The functionality to automatically relaunch `sooperGUI.go` within an `st` terminal upon startup has been restored (uncommented).
    *   The associated logic for setting terminal colors and redirecting logs when running inside `st` has also been restored.
    *   Ensured `os/exec` and `syscall` imports are active for this feature.
*   **Import Management:**
    *   Removed `net/http` and `bytes` imports as they are no longer needed for level control.
    *   Added `regexp` import for matching the custom OSC path for incoming level updates.
*   **Code Comments:**
    *   Added extensive, beginner-friendly comments throughout the codebase to explain Go constructs and application logic.

---
## [Previous Date] - HTTP Level Control & Initial Setup (Mistaken Direction)

### `mock_api.go`

*   **Enhanced HTTP Handling for `/strip/` routes:**
    *   The primary handler for `/strip/` now uses regular expressions to differentiate and manage specific path patterns.
*   **New Dynamic POST Endpoint:**
    *   Added a new endpoint: `POST /strip/Sooper<ID>/Gain/Gain (dB)`.
    *   This endpoint dynamically extracts an `<ID>` (e.g., "1", "2") from the URL path.
    *   It expects a plain text float value in the request body, representing the gain.
    *   Upon receiving a POST request, it logs the extracted `SooperID` and the received gain value.
    *   Responds with `HTTP 200 OK` and a confirmation message.
*   **Maintained Existing GET Endpoint:**
    *   The previous endpoint `GET /strip/Sooper1/Gain/Gain_dB` remains functional and returns "0.5".
*   **Improved Error Handling & Logging:**
    *   Added error handling for reading the request body.
    *   Logs errors if the request body cannot be read.
    *   Responds with `HTTP 405 Method Not Allowed` if a non-POST request is made to the dynamic gain path.
    *   Responds with `HTTP 404 Not Found` for unhandled paths under `/strip/`.
*   **Dependencies:**
    *   Added `io`, `regexp`, and `strings` to imports to support the new functionality.

### `sooperGUI.go`

*   **Level Column Control Rework (HTTP Integration):**
    *   The mouse interaction logic for the "Level" column (column 7 in the TUI) has been significantly changed.
    *   Instead of sending OSC messages, it now sends an HTTP POST request to the `mock_api.go` server.
    *   The target URL is dynamically constructed as `http://localhost:9090/strip/Sooper<LoopID>/Gain/Gain (dB)`, where `<LoopID>` is the 1-based index of the loop being controlled.
    *   The calculated `wet` value (as a float) is sent as a plain text string in the body of the POST request.
*   **Value Capping for Level Control:**
    *   The `wet` value derived from the mouse position in the "Level" column is now capped at a maximum of `0.921`.
    *   The internal `fill` variable (percentage of the bar clicked) is capped at `0.98978457` to achieve this `wet` value cap, based on the logarithmic conversion used.
*   **Go Module Initialization & Dependency Management:**
    *   The project was initialized as a Go module (`go mod init jaudio`).
    *   Dependencies (`github.com/gdamore/tcell/v2`, `github.com/hypebeast/go-osc/osc`, `github.com/rivo/tview`) were added using `go get` and are now managed in `go.mod` and `go.sum`.
*   **`st` Terminal Relaunch Bypass:**
    *   The functionality that attempted to relaunch `sooperGUI.go` within an `st` terminal has been commented out. This resolves issues where `st` is not installed or not found in the system's `$PATH`.
    *   The associated logic for redirecting logs from the (no longer created) `st` child process has also been commented out.
*   **Import Management:**
    *   Added `bytes` and `net/http` to the imports to support making HTTP POST requests.
    *   Removed previously imported `os/exec` and `syscall` packages as they are no longer used after commenting out the `st` relaunch functionality.
*   **Bug Fixes in Mouse Event Handling:**
    *   Corrected an assignment mismatch where `table.GetCell(...).GetLastPosition()` (returning 3 values) was incorrectly assigned to 4 variables.
    *   Resolved a variable redeclaration error for `fill` within the mouse event handler.
    *   Ensured consistent use of `cellContentWidth` for calculations, correcting previous mix-ups with a `cellWidth` variable.