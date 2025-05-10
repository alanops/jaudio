# SooperLooper TUI and Mock API

This project consists of a Terminal User Interface (TUI) for the SooperLooper live looping sampler, and a mock HTTP API used by the TUI for level control.

## Components

*   **`sooperGUI.go`**:
    *   A Go application that provides a TUI to monitor and interact with a SooperLooper instance.
    *   Communicates with SooperLooper via OSC (Open Sound Control) for status updates (loop state, position, meters).
    *   Features mouse-driven level control for loops, which sends commands to the `mock_api.go` server via HTTP POST.
*   **`mock_api.go`**:
    *   A simple Go HTTP server that acts as a mock backend for the level control feature in `sooperGUI.go`.
    *   Listens for POST requests on `/strip/Sooper<ID>/Gain/Gain (dB)` to simulate setting gain levels.
    *   Also provides a GET endpoint `/strip/Sooper1/Gain/Gain_dB` for basic testing.
*   **`3track-sooper.slsess`**:
    *   A SooperLooper session file, likely containing a pre-configured 3-track looping setup. This can be loaded into SooperLooper to be controlled by `sooperGUI.go`.
*   **`CHANGELOG.md`**:
    *   Contains a log of significant changes made to the project. See [`CHANGELOG.md`](CHANGELOG.md:0) for details.

## Prerequisites

*   **Go**: Version 1.18 or higher recommended.
*   **SooperLooper**:
    *   **Requirement**: This application (`sooperGUI.go`) is a client for SooperLooper. **SooperLooper itself must be separately installed on your system and accessible via the command line (i.e., in your system's PATH).**
    *   **How to Install**:
        *   **Package Manager (Recommended)**: The easiest way is usually through your Linux distribution's package manager.
            *   Debian/Ubuntu: `sudo apt install sooperlooper`
            *   Fedora: `sudo dnf install sooperlooper`
            *   Arch Linux: `sudo pacman -S sooperlooper`
            *   For other distributions, search their repositories.
        *   **From Source**: If not available via package manager or for the latest versions, you may need to compile from source. This typically involves downloading the source code, installing build dependencies (like a C++ compiler, JACK, liblo, etc.), and then running `configure`, `make`, and `make install`.
        *   **Official Documentation**: **Always refer to the official SooperLooper website and documentation for the most accurate and up-to-date installation instructions for your system.** Its installation is beyond the scope of this project's setup.
    *   **Verification**: After installation, try running `sooperlooper --version` or `sooperlooper --help` in your terminal to ensure it's installed correctly and in your PATH.
    *   **JACK Audio Server**: SooperLooper relies heavily on the JACK Audio Connection Kit. **You must have JACK installed and correctly configured for your audio hardware.** Issues like "cannot connect to jack", "Cannot initialize driver", or "Failed to open server" often point to problems with JACK setup or permissions (e.g., real-time scheduling, memory locking). Tools like `qjackctl` can help manage and troubleshoot JACK. Resolving JACK issues is system-specific and often involves configuring audio groups, system limits, and JACK's own settings to match your sound card.

## Setup

1.  **Clone the repository** (if you haven't already).
2.  **Initialize Go Modules & Dependencies**:
    If you haven't already, or to ensure all dependencies are present:
    ```bash
    go mod tidy
    ```
    This will download the necessary packages defined in `go.mod` (e.g., `tview`, `osc`).

## Running the Applications

You'll typically run SooperLooper, `mock_api.go`, and `sooperGUI.go` in separate terminals.

### 0. SooperLooper (Example with Session File)

To use `sooperGUI.go` effectively, SooperLooper should be running and listening for OSC messages. You can load the provided session file:

*   **Command (from the project root directory):**
    ```bash
    sooperlooper --load-session ./3track-sooper.slsess --osc-port 9951
    ```
*   **Important Note:** This command assumes `sooperlooper` is installed and can be found in your system's PATH. If you see a "command not found" error, you need to install SooperLooper first. The `--osc-port 9951` ensures it listens on the port `sooperGUI.go` defaults to.

### 1. `mock_api.go`

This server provides the HTTP endpoints that `sooperGUI.go` uses for level control.

*   **Command:**
    ```bash
    go run mock_api.go
    ```
*   **Description:**
    *   Starts the mock HTTP server, typically listening on `http://localhost:9090`.
    *   It will log received POST requests for gain changes to the console.

### 2. `sooperGUI.go`

This is the main TUI application.

*   **Build (Recommended):**
    First, build the executable:
    ```bash
    go build sooperGUI.go
    ```
    This creates an executable file named `sooperGUI` in the current directory.

*   **Run:**
    To run the TUI directly in your current terminal (avoiding issues with new window creation in some environments):
    ```bash
    SOOPERGUI_XTERM=1 ./sooperGUI [FLAGS]
    ```
    Alternatively, you can run it directly without building first (though building is recommended for repeated use):
    ```bash
    SOOPERGUI_XTERM=1 go run sooperGUI.go [FLAGS]
    ```
*   **Why `SOOPERGUI_XTERM=1`?**
    *   By default, `sooperGUI.go` attempts to launch itself in a new `st` terminal window. If `st` is not installed or if you're in an environment without a display server (like a headless server or some CI systems), this can cause a "can't open display" error.
    *   Setting the `SOOPERGUI_XTERM=1` environment variable tells the application to skip launching a new window and instead run the TUI within the current terminal session.
*   **Description:**
    *   Starts the Terminal User Interface.
    *   Attempts to connect to a SooperLooper instance via OSC (defaults to `127.0.0.1:9951`). Ensure SooperLooper is running and configured to listen for OSC on this address and port.
    *   The "Level" column in the TUI sends HTTP POST requests to the `mock_api.go` server (at `http://localhost:9090`) when interacted with.
*   **Available Flags:**
    *   `--osc-host <host>`: OSC host for SooperLooper (default: `127.0.0.1`).
    *   `--osc-port <port>`: OSC UDP port for SooperLooper (default: `9951`).
    *   `--refresh-rate <ms>`: TUI refresh rate in milliseconds (default: `200`).
    *   `--debug`: Enable debug logging to the console.
    *   `--state-debug`: Show an extra state debug column in the TUI.
    *   `--help` or `-h`: Show the help message.

## Key Features of `sooperGUI.go`

*   Real-time display of SooperLooper loop states (Record, Overdub, Mute, etc.), loop position, and I/O peak meters.
*   OSC communication for receiving updates from and sending basic pings to SooperLooper.
*   Interactive mouse-driven control for loop "Level" faders, now integrated with the `mock_api.go` via HTTP.
*   Configurable connection parameters and refresh rate.
*   Recent fixes ensure compatibility with current `tview` library versions (as of May 2025) and address issues with cell coordinate detection and mouse event handling.