/*
This is a Golang script written by perplexity.ai
It depends on stterm and sooperloopen.

It starts the sooperlooper engine and stterm with a custom TUI for it
Said TUI is primarily only status, the only controlling it does is for the output levels
*/

package main

import (
    "flag"
    "fmt"
    "log"
    "math"
    "net"
    "os"
    "os/exec"
    "strconv"
    "strings"
    "sync"
    "syscall"
    "time"

    "github.com/gdamore/tcell/v2"
    "github.com/hypebeast/go-osc/osc"
    "github.com/rivo/tview"
)

type LoopState struct {
    State        int
    NextState    int
    LoopPos      float32
    InPeakMeter  float32
    OutPeakMeter float32
    Wet          float32
}

type ButtonState struct {
    OnStates      []int
    PendingOnCond func(state, next int) bool
    PendingOffCond func(state, next int) bool
}

// --- Global configuration and state ---
var (
    oscHost     = "127.0.0.1"
    oscPort     = 9951
    refreshRate = 200

    loopCount  = 1 // Default to 1
    loopStates = make(map[int]*LoopState)
    mu         sync.Mutex
    client     *osc.Client

    infoLog  = log.New(os.Stdout, "INFO: ", log.Ldate|log.Ltime)
    errorLog = log.New(os.Stderr, "ERROR: ", log.Ldate|log.Ltime)

    greenThreshold  float32 = 0.7
    yellowThreshold float32 = 0.9
    redThreshold    float32 = 1.0

    meterMinDB  = -70.0
    meterMidDB  = -16.0 // For reference only
    meterMaxDB  = 0.0

    debugFlag      *bool // Set by --debug
    stateDebugFlag *bool // Set by --state-debug
)

func main() {
    // --- Parse command-line arguments ---
    flag.StringVar(&oscHost, "osc-host", oscHost, "OSC host (default: 127.0.0.1)")
    flag.IntVar(&oscPort, "osc-port", oscPort, "OSC UDP port (default: 9951)")
    flag.IntVar(&refreshRate, "refresh-rate", refreshRate, "TUI refresh rate in milliseconds (default: 200)")
    debugFlag = flag.Bool("debug", false, "Enable debug logging to parent terminal")
    stateDebugFlag = flag.Bool("state-debug", false, "Show state debug column in the TUI")
    help := flag.Bool("help", false, "Show help message")
    flag.BoolVar(help, "h", false, "Show help message (shorthand)")
    flag.Parse()

    if *help {
        fmt.Printf(`Usage: sooperGUI [OPTIONS]
Options:
  --osc-host         OSC host (default: 127.0.0.1)
  --osc-port         OSC UDP port (default: 9951)
  --refresh-rate     TUI refresh rate in milliseconds (default: 100)
  --debug            Enable debug logging to parent terminal
  --state-debug      Show state debug column in the TUI
  --help, -h         Show this help message
`)
        os.Exit(0)
    }

    // --- Relaunch in a new st terminal if not already in one ---
    if os.Getenv("SOOPERGUI_XTERM") == "" {
        self, err := os.Executable()
        if err != nil {
            errorLog.Fatalf("Cannot find executable: %v", err)
        }
        args := os.Args[1:]
        env := append(os.Environ(), "SOOPERGUI_XTERM=1")
        cmd := exec.Command("st", "-f", "monospace:size=10", "-c", "sooperGUI", "-e", self)
        cmd.Args = append(cmd.Args, args...)
        cmd.Env = env
        cmd.Stdout = os.Stdout
        cmd.Stderr = os.Stderr
        cmd.Stdin = os.Stdin
        infoLog.Println("Launching new st window for GUI...")
        if err := cmd.Start(); err != nil {
            errorLog.Fatalf("Failed to launch st: %v", err)
        }
        go func() {
            time.Sleep(1 * time.Second)
            if cmd.Process != nil {
                cmd.Process.Signal(syscall.SIGWINCH)
            }
        }()
        cmd.Wait()
        os.Exit(0)
    }

    // --- In the child process: set up logging and terminal colors ---
    if os.Getenv("SOOPERGUI_XTERM") != "" {
        fmt.Print("\033]10;#00FF00\007\033]11;#000000\007")
        ppid := os.Getppid()
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
    listener, err := net.ListenPacket("udp", ":0")
    if err != nil {
        errorLog.Fatalf("Failed to allocate UDP port: %v", err)
    }
    defer listener.Close()
    localAddr := listener.LocalAddr().(*net.UDPAddr)
    localPort := localAddr.Port
    returnIP := getLocalIP(oscHost)
    returnURL := fmt.Sprintf("osc.udp://%s:%d", returnIP, localPort)

    // --- Set up OSC client and server ---
    client = osc.NewClient(oscHost, oscPort)
    infoLog.Printf("Connecting to SooperLooper OSC at %s:%d", oscHost, oscPort)

    dispatcher := osc.NewStandardDispatcher()
    dispatcher.AddMsgHandler("*", func(msg *osc.Message) {
        if *debugFlag {
            infoLog.Printf("OSC IN: %s %v", msg.Address, msg.Arguments)
        }
        handleOSC(msg)
    })
    server := &osc.Server{
        Addr:       fmt.Sprintf(":%d", localPort),
        Dispatcher: dispatcher,
    }
    go func() {
        infoLog.Printf("OSC server listening on udp://%s:%d", returnIP, localPort)
        if err := server.Serve(listener); err != nil {
            errorLog.Fatalf("OSC server error: %v", err)
        }
    }()
    sendPing(client, returnURL)

    // --- Register for automatic updates for each loop and each control ---
    for i := 0; i < loopCount; i++ {
        registerAutoUpdate(client, i, "loop_pos", returnURL, debugFlag)
        registerAutoUpdate(client, i, "in_peak_meter", returnURL, debugFlag)
        registerAutoUpdate(client, i, "out_peak_meter", returnURL, debugFlag)
        registerAutoUpdate(client, i, "wet", returnURL, debugFlag)
        pollControl(client, i, "wet", returnURL, debugFlag)
    }

    // --- Poll for state and wet value at the user-configured refreshRate ---
    go func() {
        for {
            for i := 0; i < loopCount; i++ {
                pollControl(client, i, "state", returnURL, debugFlag)
                pollControl(client, i, "next_state", returnURL, debugFlag)
                pollControl(client, i, "wet", returnURL, debugFlag)
            }
            time.Sleep(time.Duration(refreshRate) * time.Millisecond)
        }
    }()

    // --- Set up the TUI using tview ---
    app := tview.NewApplication()
    table := tview.NewTable().SetBorders(true).SetFixed(1, 0)
    var screenWidth int = 80
    app.SetBeforeDrawFunc(func(screen tcell.Screen) bool {
        w, _ := screen.Size()
        screenWidth = w
        return false
    })

    app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
        if event.Key() == tcell.KeyCtrlC {
            return nil
        }
        return event
    })

    buttonDefs := map[string]ButtonState{
        "RECORD": {
            OnStates: []int{2, 3},
            PendingOnCond: func(state, next int) bool {
                return state == 1 && (next == 4 || next == -1)
            },
            PendingOffCond: func(state, next int) bool {
                return (state == 2 || state == 3) && next == 4
            },
        },
        "OVERDUB": {
            OnStates: []int{5},
            PendingOnCond: func(state, next int) bool {
                return state == 4 && next == 5
            },
            PendingOffCond: func(state, next int) bool {
                return state == 5 && next == 4
            },
        },
        "MUTE": {
            OnStates: []int{10, 20},
            PendingOnCond: func(state, next int) bool {
                return state == 4 && next == 10
            },
            PendingOffCond: func(state, next int) bool {
                return (state == 10 || state == 20) && next == 4
            },
        },
    }

    updateTable := func() {
        mu.Lock()
        defer mu.Unlock()
        headers := []string{
            "ID", "Rec", "Dub", "Mute", "Pos", "Meter In", "Meter Out", "Level",
        }
        fixedColWidths := []int{5, 8, 8, 8, 9}
        debugCol := *stateDebugFlag
        if debugCol {
            headers = append(headers, "State Debug")
            fixedColWidths = append(fixedColWidths, 14)
        }
        numCols := len(headers)
        table.Clear()
        fixedTotal := 0
        for i := 0; i < numCols; i++ {
            if i == 5 || i == 6 || i == 7 {
                continue
            }
            if i < len(fixedColWidths) {
                fixedTotal += fixedColWidths[i]
            }
        }
        meterCols := 3
        meterWidth := (screenWidth - fixedTotal - (numCols-1)*1)
        if meterWidth < 3*len("Meter In") {
            meterWidth = 3 * len("Meter In")
        }
        meterWidthEach := meterWidth / meterCols

        bold := tcell.StyleDefault.Bold(true)
        for i, h := range headers {
            var padded string
            align := tview.AlignCenter
            if i >= 5 && i <= 7 {
                padded = h
                align = tview.AlignCenter
            } else if i <= 4 {
                padded = " " + h + " "
            } else {
                padded = h
            }
            w := getColWidth(i, fixedColWidths, meterWidthEach)
            cell := tview.NewTableCell(padded).
                SetSelectable(false).
                SetStyle(bold).
                SetMaxWidth(w).
                SetAlign(align)
            if i >= 5 && i <= 7 {
                cell.SetExpansion(1)
            } else {
                cell.SetExpansion(0)
            }
            table.SetCell(0, i, cell)
        }
        for i := 0; i < loopCount; i++ {
            ls := loopStates[i]
            if ls == nil {
                ls = &LoopState{}
            }
            row := i + 1
            table.SetCell(row, 0, tview.NewTableCell(" "+strconv.Itoa(i+1)+" ").SetMaxWidth(fixedColWidths[0]).SetExpansion(0).SetAlign(tview.AlignCenter))
            table.SetCell(row, 1, buttonStateCell(ls.State, ls.NextState, fixedColWidths[1], buttonDefs["RECORD"]))
            table.SetCell(row, 2, buttonStateCell(ls.State, ls.NextState, fixedColWidths[2], buttonDefs["OVERDUB"]))
            table.SetCell(row, 3, buttonStateCell(ls.State, ls.NextState, fixedColWidths[3], buttonDefs["MUTE"]))
            table.SetCell(row, 4, tview.NewTableCell(fmt.Sprintf(" %.2f ", ls.LoopPos)).SetMaxWidth(fixedColWidths[4]).SetAlign(tview.AlignCenter).SetExpansion(0))
            table.SetCell(row, 5, meterBarCell(ls.InPeakMeter, meterWidthEach))
            table.SetCell(row, 6, meterBarCell(ls.OutPeakMeter, meterWidthEach))
            table.SetCell(row, 7, meterBarCell(ls.Wet, meterWidthEach))
            if debugCol {
                table.SetCell(row, 8, tview.NewTableCell(fmt.Sprintf("S:%d N:%d", ls.State, ls.NextState)).SetMaxWidth(fixedColWidths[5]).SetExpansion(0).SetAlign(tview.AlignCenter))
            }
        }
    }

    table.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey { return event })
    table.SetMouseCapture(func(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
        if action == tview.MouseLeftClick || action == tview.MouseLeftDown || action == tview.MouseMove {
            x, y := event.Position()
            row, col := table.CellAt(x, y)
            lX, _, width := table.GetCell(row,col).GetLastPosition()
            infoLog.Printf("Coords: %d, %d", x, y)
            if row > 0 && col == 7 && row <= loopCount {
                leftX := x - lX
                var fill float32
                fill = float32(leftX) / float32(width)
                var wet float32
                wet = float32(math.Pow(10, float64(fill)*(meterMaxDB-meterMinDB)/20.0 + meterMinDB/20.0))
                // wet = float32(math.Pow(10, float64(fill)))
                if *debugFlag {
                    // infoLog.Printf("DEBUG: Level cell width=%d, mouse x=%d, leftX=%d, rightX=%d, clampedX=%d, cellX=%d, fill=%.6f, wet=%.6f", width, x, leftX, rightX, clampedX, cellX, fill, wet)
                }
                infoLog.Printf("meterMaxDB = %f", meterMaxDB)
                infoLog.Printf("meterMinDB = %f", meterMinDB)
                infoLog.Printf("fill = %f  |  wet = %f", fill, wet)
                infoLog.Printf("x, y = (%d, %d)  |  Row, Col = (%d, %d)  |  leftX = %d", x, y, row, col, leftX)
                mu.Lock()
                loopStates[row-1].Wet = wet
                mu.Unlock()
                go func(loopIdx int, val float32) {
                    msg := osc.NewMessage(fmt.Sprintf("/sl/%d/set", loopIdx))
                    msg.Append("wet")
                    msg.Append(val)
                    if *debugFlag {
                        infoLog.Printf("OSC OUT: %s %v", msg.Address, msg.Arguments)
                    }
                    client := osc.NewClient(oscHost, oscPort)
                    _ = client.Send(msg)
                    if *debugFlag {
                        infoLog.Printf("Set wet for loop %d to %.6f", loopIdx, val)
                    }
                }(row-1, wet)
                return action, event
            }
        }
        return action, event
    })

    go func() {
        for {
            app.QueueUpdateDraw(updateTable)
            time.Sleep(time.Duration(refreshRate) * time.Millisecond)
        }
    }()
    infoLog.Println("TUI launched. Ctrl+C will do nothing as requested.")
    if err := app.SetRoot(table, true).EnableMouse(true).Run(); err != nil {
        errorLog.Fatalf("TUI error: %v", err)
    }
}

// --- Helper functions for rendering colored meter bars ---
func meterBarCell(val float32, width int) *tview.TableCell {
    fill := amplitudeToMeterFill(val, meterMinDB, meterMaxDB)
    full := int(math.Ceil(float64(fill) * float64(width)))
    if full > width {
        full = width
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
    bar := strings.Repeat("█", full) + strings.Repeat(" ", width-full)
    return tview.NewTableCell(bar).SetTextColor(color).SetAlign(tview.AlignLeft)
}

func levelBarCell(wet float32, width int) *tview.TableCell {
    if wet < 0.00001 {
        wet = 0.00001 // avoid log10(0)
    }
    db := 20.0 * math.Log10(float64(wet))
    fill := float32((db - meterMinDB) / (meterMaxDB - meterMinDB))
    if fill < 0 {
        fill = 0
    }
    if fill > 1 {
        fill = 1
    }
    full := int(fill * float32(width))
    if full > width {
        full = width
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
    handlePos := 0
    if width > 1 {
        handlePos = int(fill * float32(width-1) + 0.5)
    }
    bar := ""
    for i := 0; i < width; i++ {
        if i == handlePos {
            bar += "│"
        } else if i < full {
            bar += "█"
        } else {
            bar += " "
        }
    }
    return tview.NewTableCell(bar).SetTextColor(color).SetAlign(tview.AlignLeft)
}

func amplitudeToMeterFill(val float32, minDB, maxDB float64) float32 {
    if val < 0.00001 {
        return 0
    }
    db := 20.0 * math.Log10(float64(val))
    if db < minDB {
        db = minDB
    }
    if db > maxDB {
        db = maxDB
    }
    return float32((db - minDB) / (maxDB - minDB))
}

func getColWidth(col int, fixedColWidths []int, meterWidthEach int) int {
    if col == 5 || col == 6 || col == 7 {
        return meterWidthEach
    }
    if col < len(fixedColWidths) {
        return fixedColWidths[col]
    }
    return 10
}

// --- OSC/network helper functions ---

func getLocalIP(oscHost string) string {
    if oscHost == "127.0.0.1" || oscHost == "localhost" {
        return "127.0.0.1"
    }
    addrs, err := net.InterfaceAddrs()
    if err != nil {
        return "127.0.0.1"
    }
    for _, addr := range addrs {
        if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() && ipnet.IP.To4() != nil {
            return ipnet.IP.String()
        }
    }
    return "127.0.0.1"
}

func waitForEngine(client *osc.Client, returnURL string, timeout time.Duration) bool {
    msg := osc.NewMessage("/ping")
    msg.Append(returnURL)
    msg.Append("/pong")
    if *debugFlag {
        infoLog.Printf("OSC OUT: /ping %v", msg.Arguments)
    }
    _ = client.Send(msg)
    time.Sleep(timeout)
    return true
}

func sendPing(client *osc.Client, returnURL string) {
    msg := osc.NewMessage("/ping")
    msg.Append(returnURL)
    msg.Append("/pong")
    if *debugFlag {
        infoLog.Printf("OSC OUT: /ping %v", msg.Arguments)
    }
    _ = client.Send(msg)
}

func registerAutoUpdate(client *osc.Client, loop int, control string, returnURL string, debugFlag *bool) {
    retPath := fmt.Sprintf("/sl/%d/update_%s", loop, control)
    msg := osc.NewMessage(fmt.Sprintf("/sl/%d/register_auto_update", loop))
    msg.Append(control)
    msg.Append(int32(100))
    msg.Append(returnURL)
    msg.Append(retPath)
    if *debugFlag {
        infoLog.Printf("OSC OUT: %s %v", msg.Address, msg.Arguments)
    }
    _ = client.Send(msg)
}

func pollControl(client *osc.Client, loop int, control string, returnURL string, debugFlag *bool) {
    retPath := fmt.Sprintf("/sl/%d/update_%s", loop, control)
    msg := osc.NewMessage(fmt.Sprintf("/sl/%d/get", loop))
    msg.Append(control)
    msg.Append(returnURL)
    msg.Append(retPath)
    if *debugFlag {
        infoLog.Printf("OSC OUT: %s %v", msg.Address, msg.Arguments)
    }
    _ = client.Send(msg)
}

func handleOSC(msg *osc.Message) {
    mu.Lock()
    defer mu.Unlock()
    switch {
    case msg.Address == "/pong":
        if len(msg.Arguments) >= 3 {
            if c, ok := msg.Arguments[2].(int32); ok {
                loopCount = int(c)
            }
        }
    case strings.Contains(msg.Address, "/update_state"):
        loopIdx := parseLoopIndex(msg.Address)
        if len(msg.Arguments) >= 3 {
            if idx, ok := msg.Arguments[0].(int32); ok && int(idx) == loopIdx {
                if ctrl, ok := msg.Arguments[1].(string); ok && ctrl == "state" {
                    if val, ok := msg.Arguments[2].(float32); ok {
                        getLoopState(loopIdx).State = int(val)
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
                    switch val := msg.Arguments[2].(type) {
                    case float32:
                        getLoopState(loopIdx).Wet = val
                    case float64:
                        getLoopState(loopIdx).Wet = float32(val)
                    }
                }
            }
        }
    }
}

func parseLoopIndex(addr string) int {
    parts := strings.Split(addr, "/")
    if len(parts) > 2 {
        if idx, err := strconv.Atoi(parts[2]); err == nil {
            return idx
        }
    }
    return 0
}

func getLoopState(idx int) *LoopState {
    if loopStates[idx] == nil {
        loopStates[idx] = &LoopState{}
    }
    return loopStates[idx]
}

func buttonStateCell(state, nextState, width int, def ButtonState) *tview.TableCell {
    label := ""
    color := tcell.ColorWhite
    switch {
    case def.PendingOnCond(state, nextState):
        label = "ON"
        color = tcell.ColorYellow
    case def.PendingOffCond(state, nextState):
        label = "OFF"
        color = tcell.ColorYellow
    case containsInt(def.OnStates, state):
        label = "ON"
        color = tcell.ColorGreen
    default:
        label = "OFF"
        color = tcell.ColorRed
    }
    return tview.NewTableCell(" "+label+" ").
        SetTextColor(color).
        SetAlign(tview.AlignCenter).
        SetMaxWidth(width)
}

func containsInt(slice []int, val int) bool {
    for _, v := range slice {
        if v == val {
            return true
        }
    }
    return false
}
