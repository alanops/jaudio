// sooperGUI.go
// Terminal UI front‑end for SooperLooper written in Go.
// Fixed version that compiles with the current tview (May 2025).
// Alan — v1.0

package main

import (
	"flag"
	"fmt"
	"log"
	"math"
	"net"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/hypebeast/go-osc/osc"
	"github.com/rivo/tview"
)

// --- Structs -----------------------------------------------------------------

type LoopState struct {
	State        int
	NextState    int
	LoopPos      float32
	InPeakMeter  float32
	OutPeakMeter float32
	Wet          float32
}

type ButtonState struct {
	OnStates       []int
	PendingOnCond  func(state, next int) bool
	PendingOffCond func(state, next int) bool
}

// --- Globals -----------------------------------------------------------------

var (
	stripGainPathRegex = regexp.MustCompile(`^/strip/Sooper(\d+)/Gain/Gain%20\(dB\)$`)

	oscHost     = "127.0.0.1"
	oscPort     = 9951
	refreshRate = 200

	loopCount  = 1
	loopStates = make(map[int]*LoopState)
	mu         sync.Mutex

	client     *osc.Client
	mockClient *osc.Client

	infoLog  = log.New(os.Stdout, "INFO: ", log.Ldate|log.Ltime)
	errorLog = log.New(os.Stderr, "ERROR: ", log.Ldate|log.Ltime)

	greenThreshold  float32 = 0.7
	yellowThreshold float32 = 0.9
	redThreshold    float32 = 1.0

	meterMinDB = -70.0
	meterMaxDB = 0.0

	debugFlag      *bool
	stateDebugFlag *bool
)

// --- main --------------------------------------------------------------------

func main() {
	flag.StringVar(&oscHost, "osc-host", oscHost, "OSC host")
	flag.IntVar(&oscPort, "osc-port", oscPort, "OSC UDP port")
	flag.IntVar(&refreshRate, "refresh-rate", refreshRate, "TUI refresh rate in ms")

	debugFlag = flag.Bool("debug", false, "Verbose logging")
	stateDebugFlag = flag.Bool("state-debug", false, "Show state column")

	help := flag.Bool("help", false, "Show help")
	flag.BoolVar(help, "h", false, "Show help (shorthand)")
	flag.Parse()

	if *help {
		fmt.Println(`Usage: sooperGUI [OPTIONS]
  --osc-host         OSC host (default 127.0.0.1)
  --osc-port         OSC UDP port (default 9951)
  --refresh-rate     TUI refresh rate ms (default 200)
  --debug            Verbose logging
  --state-debug      Add state debug column
  -h, --help         Show this help`)
		os.Exit(0)
	}

	// Relaunch in st only if st exists and env not set
	if os.Getenv("SOOPERGUI_XTERM") == "" {
		if _, err := exec.LookPath("st"); err == nil {
			self, err := os.Executable()
			if err != nil {
				errorLog.Fatalf("cannot find executable: %v", err)
			}
			args := os.Args[1:]
			env := append(os.Environ(), "SOOPERGUI_XTERM=1")
			cmd := exec.Command("st", "-f", "monospace:size=10", "-c", "sooperGUI", "-e", self)
			cmd.Args = append(cmd.Args, args...)
			cmd.Env = env
			cmd.Stdout, cmd.Stderr, cmd.Stdin = os.Stdout, os.Stderr, os.Stdin
			infoLog.Println("launching new st window…")
			if err := cmd.Start(); err != nil {
				errorLog.Fatalf("failed to launch st: %v", err)
			}
			go func() {
				time.Sleep(time.Second)
				if cmd.Process != nil {
					_ = cmd.Process.Signal(syscall.SIGWINCH)
				}
			}()
			cmd.Wait()
			os.Exit(0)
		}
	}

	if os.Getenv("SOOPERGUI_XTERM") != "" {
		fmt.Print("\033]10;#00FF00\007\033]11;#000000\007")
		ppid := os.Getppid()
		if parent, _ := os.OpenFile(fmt.Sprintf("/proc/%d/fd/1", ppid), os.O_WRONLY, 0); parent != nil {
			infoLog.SetOutput(parent)
		}
		if parent, _ := os.OpenFile(fmt.Sprintf("/proc/%d/fd/2", ppid), os.O_WRONLY, 0); parent != nil {
			errorLog.SetOutput(parent)
		}
	}

	listener, err := net.ListenPacket("udp", ":0")
	if err != nil {
		errorLog.Fatalf("udp listen: %v", err)
	}
	defer listener.Close()

	localPort := listener.LocalAddr().(*net.UDPAddr).Port
	returnIP := getLocalIP(oscHost)
	returnURL := fmt.Sprintf("osc.udp://%s:%d", returnIP, localPort)

	client = osc.NewClient(oscHost, oscPort)
	mockClient = osc.NewClient("127.0.0.1", 9090)

	dispatcher := osc.NewStandardDispatcher()
	dispatcher.AddMsgHandler("*", func(m *osc.Message) {
		if *debugFlag {
			infoLog.Printf("OSC IN %s %v", m.Address, m.Arguments)
		}
		handleOSC(m)
	})
	server := &osc.Server{Addr: fmt.Sprintf(":%d", localPort), Dispatcher: dispatcher}
	go func() {
		infoLog.Printf("OSC server listening on %s", returnURL)
		if err := server.Serve(listener); err != nil {
			errorLog.Fatalf("osc server: %v", err)
		}
	}()

	sendPing(client, returnURL)
	for i := 0; i < loopCount; i++ {
		registerAutoUpdate(client, i, "loop_pos", returnURL, debugFlag)
		registerAutoUpdate(client, i, "in_peak_meter", returnURL, debugFlag)
		registerAutoUpdate(client, i, "out_peak_meter", returnURL, debugFlag)
	}

	go func() {
		for {
			for i := 0; i < loopCount; i++ {
				pollControl(client, i, "state", returnURL, debugFlag)
				pollControl(client, i, "next_state", returnURL, debugFlag)
				if mockClient != nil {
					pollStripGain(mockClient, i+1, returnURL, debugFlag)
				}
			}
			time.Sleep(time.Duration(refreshRate) * time.Millisecond)
		}
	}()

	app := tview.NewApplication()
	table := tview.NewTable().SetBorders(true).SetFixed(1, 0)

	var screenWidth int = 80
	app.SetBeforeDrawFunc(func(s tcell.Screen) bool {
		w, _ := s.Size()
		screenWidth = w
		return false
	})

	app.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if ev.Key() == tcell.KeyCtrlC {
			return nil
		}
		return ev
	})

	buttonDefs := map[string]ButtonState{
		"RECORD": {
			OnStates:       []int{2, 3},
			PendingOnCond:  func(state, next int) bool { return state == 1 && (next == 4 || next == -1) },
			PendingOffCond: func(state, next int) bool { return (state == 2 || state == 3) && next == 4 },
		},
		"OVERDUB": {
			OnStates:       []int{5},
			PendingOnCond:  func(state, next int) bool { return state == 4 && next == 5 },
			PendingOffCond: func(state, next int) bool { return state == 5 && next == 4 },
		},
		"MUTE": {
			OnStates:       []int{10, 20},
			PendingOnCond:  func(state, next int) bool { return state == 4 && next == 10 },
			PendingOffCond: func(state, next int) bool { return (state == 10 || state == 20) && next == 4 },
		},
	}

	updateTable := func() {
		mu.Lock()
		defer mu.Unlock()

		headers := []string{"ID", "Rec", "Dub", "Mute", "Pos", "Meter In", "Meter Out", "Level"}
		fixedColWidths := []int{5, 8, 8, 8, 9}
		if *stateDebugFlag {
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
		meterWidth := screenWidth - fixedTotal - (numCols-1)*1
		if meterWidth < 3*len("Meter In") {
			meterWidth = 3 * len("Meter In")
		}
		meterWidthEach := meterWidth / meterCols
		if meterWidthEach < 1 {
			meterWidthEach = 1
		}

		bold := tcell.StyleDefault.Bold(true)
		for i, h := range headers {
			w := getColWidth(i, fixedColWidths, meterWidthEach)
			cell := tview.NewTableCell(" " + h + " ").SetSelectable(false).SetStyle(bold).SetMaxWidth(w).SetAlign(tview.AlignCenter)
			if i >= 5 && i <= 7 {
				cell.SetExpansion(1)
			}
			table.SetCell(0, i, cell)
		}

		for i := 0; i < loopCount; i++ {
			ls := loopStates[i]
			if ls == nil {
				ls = &LoopState{}
			}
			row := i + 1
			table.SetCell(row, 0, tview.NewTableCell(" "+strconv.Itoa(i+1)+" ").SetMaxWidth(fixedColWidths[0]).SetAlign(tview.AlignCenter))
			table.SetCell(row, 1, buttonStateCell(ls.State, ls.NextState, fixedColWidths[1], buttonDefs["RECORD"]))
			table.SetCell(row, 2, buttonStateCell(ls.State, ls.NextState, fixedColWidths[2], buttonDefs["OVERDUB"]))
			table.SetCell(row, 3, buttonStateCell(ls.State, ls.NextState, fixedColWidths[3], buttonDefs["MUTE"]))
			table.SetCell(row, 4, tview.NewTableCell(fmt.Sprintf(" %.2f ", ls.LoopPos)).SetMaxWidth(fixedColWidths[4]).SetAlign(tview.AlignCenter))
			table.SetCell(row, 5, meterBarCell(ls.InPeakMeter, meterWidthEach))
			table.SetCell(row, 6, meterBarCell(ls.OutPeakMeter, meterWidthEach))
			table.SetCell(row, 7, meterBarCell(ls.Wet, meterWidthEach))
			if *stateDebugFlag {
				table.SetCell(row, 8, tview.NewTableCell(fmt.Sprintf("S:%d N:%d", ls.State, ls.NextState)).SetAlign(tview.AlignCenter))
			}
		}
	}

	table.SetMouseCapture(func(action tview.MouseAction, ev *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
		if action != tview.MouseLeftClick && action != tview.MouseLeftDown && action != tview.MouseMove {
			return action, ev
		}
		x, y := ev.Position()
		row, col, ok := tableCoordinatesAt(table, x, y)
		if !ok || row == 0 {
			return action, ev
		}
		if col != 7 || row > loopCount {
			return action, ev
		}
		cellContentX, _, cellContentWidth, _ := table.GetCell(row, col).GetLastPosition()
		relX := x - cellContentX
		var fill float32
		if cellContentWidth > 0 {
			fill = float32(relX) / float32(cellContentWidth)
		}
		if fill < 0 {
			fill = 0
		}
		if fill > 1 {
			fill = 1
		}
		const maxWet = 0.921
		wet := fill * maxWet
		if wet > maxWet {
			wet = maxWet
		}
		mu.Lock()
		if loopStates[row-1] != nil {
			loopStates[row-1].Wet = wet
		}
		mu.Unlock()
		go func(loopID int, value float32) {
			addr := fmt.Sprintf("/strip/Sooper%d/Gain/Gain%%20(dB)", loopID)
			m := osc.NewMessage(addr)
			m.Append(value)
			if mockClient != nil {
				_ = mockClient.Send(m)
			}
		}(row, wet)
		return action, ev
	})

	go func() {
		for {
			app.QueueUpdateDraw(updateTable)
			time.Sleep(time.Duration(refreshRate) * time.Millisecond)
		}
	}()

	infoLog.Println("TUI running – press Ctrl+C (ignored) or close window to quit")
	if err := app.SetRoot(table, true).EnableMouse(true).Run(); err != nil {
		errorLog.Fatalf("tview: %v", err)
	}
}

// --- TUI helpers -------------------------------------------------------------

func meterBarCell(val float32, width int) *tview.TableCell {
	fill := amplitudeToMeterFill(val, meterMinDB, meterMaxDB)
	fullChars := int(math.Ceil(float64(fill) * float64(width)))
	if fullChars < 0 {
		fullChars = 0
	}
	if fullChars > width {
		fullChars = width
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

	bar := strings.Repeat("█", fullChars) + strings.Repeat(" ", width-fullChars)
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

func getColWidth(col int, fixed []int, meter int) int {
	if col == 5 || col == 6 || col == 7 {
		return meter
	}
	if col < len(fixed) {
		return fixed[col]
	}
	return 10
}

func tableCoordinatesAt(t *tview.Table, x, y int) (row, col int, ok bool) {
	ok = false
	t.ForEachCell(func(r, c int, cell *tview.TableCell) {
		cx, cy, cw, ch := cell.GetLastPosition()
		if x >= cx && x < cx+cw && y >= cy && y < cy+ch {
			row, col, ok = r, c, true
		}
	})
	return
}

func buttonStateCell(state, next, width int, def ButtonState) *tview.TableCell {
	label := "OFF"
	color := tcell.ColorRed

	switch {
	case def.PendingOnCond(state, next):
		label, color = "ON", tcell.ColorYellow
	case def.PendingOffCond(state, next):
		label, color = "OFF", tcell.ColorYellow
	case containsInt(def.OnStates, state):
		label, color = "ON", tcell.ColorGreen
	}

	return tview.NewTableCell(" " + label + " ").SetTextColor(color).SetAlign(tview.AlignCenter).SetMaxWidth(width)
}

func containsInt(arr []int, v int) bool {
	for _, x := range arr {
		if x == v {
			return true
		}
	}
	return false
}

// --- OSC helpers -------------------------------------------------------------

func getLocalIP(host string) string {
	if host == "127.0.0.1" || host == "localhost" {
		return "127.0.0.1"
	}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "127.0.0.1"
	}
	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok && !ipnet.IP.IsLoopback() && ipnet.IP.To4() != nil {
			return ipnet.IP.String()
		}
	}
	return "127.0.0.1"
}

func sendPing(c *osc.Client, returnURL string) {
	m := osc.NewMessage("/ping")
	m.Append(returnURL)
	m.Append("/pong")
	_ = c.Send(m)
}

func registerAutoUpdate(c *osc.Client, loop int, control, returnURL string, dbg *bool) {
	path := fmt.Sprintf("/sl/%d/register_auto_update", loop)
	m := osc.NewMessage(path)
	m.Append(control)
	m.Append(int32(100))
	m.Append(returnURL)
	m.Append(fmt.Sprintf("/sl/%d/update_%s", loop, control))
	if *dbg {
		infoLog.Printf("OSC OUT %s %v", path, m.Arguments)
	}
	_ = c.Send(m)
}

func pollControl(c *osc.Client, loop int, control, returnURL string, dbg *bool) {
	m := osc.NewMessage(fmt.Sprintf("/sl/%d/get", loop))
	m.Append(control)
	m.Append(returnURL)
	m.Append(fmt.Sprintf("/sl/%d/update_%s", loop, control))
	if *dbg {
		infoLog.Printf("OSC OUT poll %s", control)
	}
	_ = c.Send(m)
}

func pollStripGain(c *osc.Client, loopID int, returnURL string, dbg *bool) {
	if c == nil {
		return
	}
	m := osc.NewMessage("/get_strip_gain")
	m.Append(int32(loopID))
	m.Append(returnURL)
	m.Append(fmt.Sprintf("/strip/Sooper%d/Gain/Gain%%20(dB)", loopID))
	if *dbg {
		infoLog.Printf("OSC OUT poll strip gain %d", loopID)
	}
	_ = c.Send(m)
}

func handleOSC(msg *osc.Message) {
	mu.Lock()
	defer mu.Unlock()

	switch {
	case stripGainPathRegex.MatchString(msg.Address):
		matches := stripGainPathRegex.FindStringSubmatch(msg.Address)
		if len(matches) > 1 {
			id, _ := strconv.Atoi(matches[1])
			idx := id - 1
			if idx >= 0 && len(msg.Arguments) == 1 {
				switch v := msg.Arguments[0].(type) {
				case float32:
					getLoopState(idx).Wet = v
				case float64:
					getLoopState(idx).Wet = float32(v)
				}
			}
		}
	case msg.Address == "/pong":
		if len(msg.Arguments) >= 3 {
			if v, ok := msg.Arguments[2].(int32); ok {
				loopCount = int(v)
			}
		}
	case strings.Contains(msg.Address, "/update_state"):
		commonUpdate(msg, "state", func(ls *LoopState, v float32) { ls.State = int(v) })
	case strings.Contains(msg.Address, "/update_next_state"):
		commonUpdate(msg, "next_state", func(ls *LoopState, v float32) { ls.NextState = int(v) })
	case strings.Contains(msg.Address, "/update_loop_pos"):
		commonUpdate(msg, "loop_pos", func(ls *LoopState, v float32) { ls.LoopPos = v })
	case strings.Contains(msg.Address, "/update_in_peak_meter"):
		commonUpdate(msg, "in_peak_meter", func(ls *LoopState, v float32) { ls.InPeakMeter = v })
	case strings.Contains(msg.Address, "/update_out_peak_meter"):
		commonUpdate(msg, "out_peak_meter", func(ls *LoopState, v float32) { ls.OutPeakMeter = v })
	case strings.Contains(msg.Address, "/update_wet"):
		commonUpdate(msg, "wet", func(ls *LoopState, v float32) { ls.Wet = v })
	}
}

func commonUpdate(msg *osc.Message, ctrl string, apply func(*LoopState, float32)) {
	if len(msg.Arguments) < 3 {
		return
	}
	loopIdx := parseLoopIndex(msg.Address)
	if idx, ok := msg.Arguments[0].(int32); !ok || int(idx) != loopIdx {
		return
	}
	if c, ok := msg.Arguments[1].(string); !ok || c != ctrl {
		return
	}
	var val float32
	switch v := msg.Arguments[2].(type) {
	case float32:
		val = v
	case float64:
		val = float32(v)
	default:
		return
	}
	apply(getLoopState(loopIdx), val)
}

func parseLoopIndex(addr string) int {
	p := strings.Split(addr, "/")
	if len(p) > 2 {
		if i, err := strconv.Atoi(p[2]); err == nil {
			return i
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
