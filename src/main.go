package main

import (
	"fmt"
	"github.com/kellydunn/go-opc"
	"golang.org/x/net/websocket"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type action struct {
	startTime int64
	whatToDo  string
}

type Instrument interface {
	init()
	play(currentTime int64, duration int64, params []string)
	tick(currentTime int64)
}

var globals struct {
	playbackChannel chan string
	actionsQueue    []action
	instTbl         map[string]Instrument
}

// some utility functions

func checkError(err error) {
	if err != nil {
		fmt.Println("Error: ", err)
		panic("checkError found an error")
	}
}

func strToInt64(s string) int64 {
	i, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		fmt.Println(err)
		panic("String to int64 conversion failed")
	}
	return i
}

func int64ToStr(i int64) string {
	return strconv.FormatInt(i, 10)
}

func strToInt(s string) int {
	rv, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		fmt.Println(err)
		panic("Can't parse a string into a regular integer")
	}
	return int(rv)
}

const HUEDEGREE = 512

func min(a int, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a int, b int) int {
	if a > b {
		return a
	}
	return b
}

func min3(a int, b int, c int) int {
	return min(a, min(b, c))
}

func max3(a int, b int, c int) int {
	return max(a, max(b, c))
}

func RGBToHSV(r int, g int, b int) (int, int, int) {
	var m, M, delta int
	m = min3(r, g, b)
	M = max3(r, g, b)
	delta = M - m

	var hue, saturation, value int

	if delta == 0 {
		// Achromatic case (i.e. grayscale)
		hue = -1 // undefined
		saturation = 0
	} else {
		var h int

		if r == M {
			h = ((g - b) * 60 * HUEDEGREE) / delta
		} else if g == M {
			h = ((b-r)*60*HUEDEGREE)/delta + 120*HUEDEGREE
		} else { // if (b == M)
			h = ((r-g)*60*HUEDEGREE)/delta + 240*HUEDEGREE
		}
		if h < 0 {
			h += 360 * HUEDEGREE
		}

		hue = h

		// The constatnt 8 is tuned to statistically cause as little
		// tolerated mismatches as possible in RGB -> HSV -> RGB conversion.
		// (See the unit test at the bottom of this file.)
		//
		saturation = (256*delta - 8) / M
	}
	value = M
	return hue, saturation, value
}

func HSVToRGB(hue int, saturation int, value int) (int, int, int) {
	var r, g, b int

	if saturation == 0 {
		r = value
		g = value
		b = value
	} else {
		var h, s, v int
		var i, p int
		h = hue
		s = saturation
		v = value
		i = h / (60 * HUEDEGREE)
		p = (256*v - s*v) / 256

		if (i & 1) != 0 {
			var q int
			q = ((256 * 60 * HUEDEGREE * v) - (h * s * v) + (60 * HUEDEGREE * s * v * i)) / (256 * 60 * HUEDEGREE)
			switch i {
			case 1:
				r = q
				g = v
				b = p
			case 3:
				r = p
				g = q
				b = v
			case 5:
				r = v
				g = p
				b = q
			}
		} else {
			var t int
			t = ((256 * 60 * HUEDEGREE * v) + (h * s * v) - (60 * HUEDEGREE * s * v * (i + 1))) / (256 * 60 * HUEDEGREE)
			switch i {
			case 0:
				r = v
				g = t
				b = p
			case 2:
				r = p
				g = v
				b = t
			case 4:
				r = t
				g = p
				b = v
			}
		}
	}

	return r, g, b
}

func diff(a int, b int) int {
	if a >= b {
		return a - b
	}
	return b - a
}

// --------------------------------------------------------------------------------------------------------------------------------
// Dan Light code starts here!
// --------------------------------------------------------------------------------------------------------------------------------

func minFloat(a float64, b float64, c float64) float64 {
	if a < b {
		if a < c {
			return a
		}
	} else {
		if b < c {
			return b
		}
	}
	return c
}

func maxFloat(a float64, b float64, c float64) float64 {
	if a > b {
		if a > c {
			return a
		}
	} else {
		if b > c {
			return b
		}
	}
	return c
}

func danLightsChange(zone int, unitNum int, hueStart int, hueStop int, saturation int, brightness int, fade int) {
	serverAddr, err := net.ResolveUDPAddr("udp", "192.168.10.223:9000")
	// serverAddr, err := net.ResolveUDPAddr("udp", "127.0.0.1:9000")
	checkError(err)
	conn, err := net.DialUDP("udp", nil, serverAddr)
	checkError(err)
	defer conn.Close()
	buf := make([]byte, 9)
	// turn wall light red
	if saturation == 255 {
		saturation = 254 // bug in Dan's lights
	}
	buf[0] = byte(4)          // 2 == SetRGBunit
	buf[1] = byte(zone)       // Z -- zone address
	buf[2] = byte(unitNum)    // U -- unit address
	buf[3] = byte(14)         // FL -- HSV Flags
	buf[4] = byte(hueStart)   // H -- hue
	buf[5] = byte(saturation) // S -- saturation
	buf[6] = byte(brightness) // V -- value
	buf[7] = byte(0)          // FTh -- fade time, high bits
	buf[8] = byte(0)          // FTl -- fade time, low bits
	fmt.Println("Sending: ", buf)
	_, err = conn.Write(buf)
	if err != nil {
		fmt.Println(buf, err)
		panic("Write to connection")
	}
	if fade > 0 {
		FTl := fade & 255
		FTh := (fade - FTl) >> 8
		buf[0] = byte(4)          // 2 == SetRGBunit
		buf[1] = byte(zone)       // Z -- zone address
		buf[2] = byte(unitNum)    // U -- unit address
		buf[3] = byte(14)         // U -- unit address
		buf[4] = byte(hueStop)    // H -- hue
		buf[5] = byte(saturation) // S -- saturation
		buf[6] = byte(0)          // V -- value
		buf[7] = byte(FTh)        // FTh -- fade time, high bits
		buf[8] = byte(FTl)        // FTl -- fade time, low bits
		fmt.Println("Sending: ", buf)
		_, err = conn.Write(buf)
		if err != nil {
			fmt.Println(buf, err)
			panic("Write to connection")
		}
	}
}

type instDanLights struct {
	title string
}

func (self *instDanLights) init() {
	// nothing to init
}

func (self *instDanLights) play(currentTime int64, duration int64, params []string) {
	// such as params [22.6 0.66 lights 64 254 128]
	bank := params[3]
	unitOffset := strToInt(params[4])
	zone := 200
	offsetStart := 200
	switch bank {
	case "lobbywall":
		zone = 101
		offsetStart = 100
	case "lobbylanterns":
		zone = 101
		offsetStart = 106
	case "baywhite":
		zone = 200
		offsetStart = 200
	case "baycolor":
		zone = 201
		offsetStart = 200
	}
	var unitNum int
	if unitOffset == 0 {
		unitNum = unitOffset
	} else {
		unitNum = offsetStart + unitOffset
	}
	hueStart := strToInt(params[5])
	saturation := strToInt(params[6])
	brightness := 254
	hueStop := strToInt(params[7])
	fade := int(duration / 100000000)
	danLightsChange(zone, unitNum, hueStart, hueStop, saturation, brightness, fade)
}

func (self *instDanLights) tick(currentTime int64) {
	// do nothing
}

// --------------------------------------------------------------------------------------------------------------------------------
// end Dan Light code
// --------------------------------------------------------------------------------------------------------------------------------

// --------------------------------------------------------------------------------------------------------------------------------
// Fadecandy Code Starts Here!
// --------------------------------------------------------------------------------------------------------------------------------

type fadeCandyPlayingItem struct {
	startTime     int64
	stopTime      int64
	startHue      byte
	stopHue       byte
	saturation    byte
	skip          int
	direction     int
	currentOffset int
}

type fadeCandyNextNoteInfo struct {
	skip          int
	direction     int
	currentOffset int
}

type instFadeCandy struct {
	title                 string
	fadeCandyOC           *opc.Client
	fadeCandyOpcMessage   *opc.Message
	fadeCandyNumLEDs      int
	fadeCandyAddBuffer    []uint8
	fadeCandyNextNoteInfo map[int]*fadeCandyNextNoteInfo
	fadeCandyPlayingList  map[int]*fadeCandyPlayingItem
}

func (self *instFadeCandy) init() {
	self.fadeCandyOC = opc.NewClient()
	self.fadeCandyNumLEDs = 60
	// err := self.fadeCandyOC.Connect("tcp", "localhost:7890")
	// if err != nil {
	//	fmt.Println(err)
	//	panic("fadeCandyOC.Connect error: " + err.Error())
	// }
	self.fadeCandyOpcMessage = opc.NewMessage(0)
	self.fadeCandyOpcMessage.SetLength(uint16(self.fadeCandyNumLEDs * 3))
	self.fadeCandyAddBuffer = make([]uint8, self.fadeCandyNumLEDs*3)
	self.fadeCandyNextNoteInfo = make(map[int]*fadeCandyNextNoteInfo, 0)
	self.fadeCandyPlayingList = make(map[int]*fadeCandyPlayingItem, 0)
}

func (self *instFadeCandy) play(currentTime int64, duration int64, params []string) {
	fmt.Println("Fadecandy Play: currentTime", currentTime, "duration", duration, "params", params)
	// params [66 0.66 fadeCandy 1241 113 254 113 2 1]
	remoteVoiceNum := strToInt(params[3])
	fmt.Println("remoteVoiceNum", remoteVoiceNum)
	startHue := strToInt(params[4])
	fmt.Println("startHue", startHue)
	saturation := strToInt(params[5])
	fmt.Println("saturation", saturation)
	stopHue := strToInt(params[6])
	fmt.Println("stopHue", stopHue)
	skip := strToInt(params[7])
	fmt.Println("skip", skip)
	direction := strToInt(params[8])
	fmt.Println("direction", direction)
	_, ok := self.fadeCandyNextNoteInfo[remoteVoiceNum]
	if !ok {
		// new remoteVoiceNum
		temp := fadeCandyNextNoteInfo{skip, direction, 0}
		self.fadeCandyNextNoteInfo[remoteVoiceNum] = &temp
	} else {
		// update to existing entry
		self.fadeCandyNextNoteInfo[remoteVoiceNum].skip = skip
		self.fadeCandyNextNoteInfo[remoteVoiceNum].direction = direction
		if direction > 0 {
			self.fadeCandyNextNoteInfo[remoteVoiceNum].currentOffset++
			if self.fadeCandyNextNoteInfo[remoteVoiceNum].currentOffset >= self.fadeCandyNextNoteInfo[remoteVoiceNum].skip {
				self.fadeCandyNextNoteInfo[remoteVoiceNum].currentOffset = 0
			}
		} else {
			if self.fadeCandyNextNoteInfo[remoteVoiceNum].currentOffset == 0 {
				self.fadeCandyNextNoteInfo[remoteVoiceNum].currentOffset = self.fadeCandyNextNoteInfo[remoteVoiceNum].skip - 1
			}
			self.fadeCandyNextNoteInfo[remoteVoiceNum].currentOffset--
		}
	}
	currentOffset := self.fadeCandyNextNoteInfo[remoteVoiceNum].currentOffset
	_, ok = self.fadeCandyPlayingList[remoteVoiceNum]
	if !ok {
		// new remoteVoiceNum
		temp := fadeCandyPlayingItem{currentTime, currentTime + duration, byte(startHue), byte(stopHue), byte(saturation), skip, direction, currentOffset}
		self.fadeCandyPlayingList[remoteVoiceNum] = &temp
	} else {
		// update to existing entry
		self.fadeCandyPlayingList[remoteVoiceNum].startTime = currentTime
		self.fadeCandyPlayingList[remoteVoiceNum].stopTime = currentTime + duration
		self.fadeCandyPlayingList[remoteVoiceNum].startHue = byte(startHue)
		self.fadeCandyPlayingList[remoteVoiceNum].stopHue = byte(stopHue)
		self.fadeCandyPlayingList[remoteVoiceNum].saturation = byte(saturation)
		self.fadeCandyPlayingList[remoteVoiceNum].skip = skip
		self.fadeCandyPlayingList[remoteVoiceNum].direction = direction
		self.fadeCandyPlayingList[remoteVoiceNum].currentOffset = currentOffset
	}
	fmt.Println("self.fadeCandyPlayingList", self.fadeCandyPlayingList)
}

func (self *instFadeCandy) tick(currentTime int64) {
	// fmt.Println("tick ", currentTime)
	// garbage collect notes no longer playing
	listToRemove := make([]int, 0)
	for voicenum, playingEntry := range self.fadeCandyPlayingList {
		if playingEntry.stopTime < currentTime {
			listToRemove = append(listToRemove, voicenum)
		}
	}
	// fmt.Println("listToRemove",listToRemove)
	for _, voicenum := range listToRemove {
		delete(self.fadeCandyPlayingList, voicenum)
	}
	// clear add buffer
	lightcount := self.fadeCandyNumLEDs
	bufsize := self.fadeCandyNumLEDs * 3
	for i := 0; i < bufsize; i++ {
		self.fadeCandyAddBuffer[i] = 0
	}
	// // fmt.Println("self", self)
	// fmt.Println("self.title", self.title)
	// fmt.Println("self.fadeCandyOC", self.fadeCandyOC)
	// // fmt.Println("self.fadeCandyOpcMessage", self.fadeCandyOpcMessage)
	// fmt.Println("self.fadeCandyNumLEDs", self.fadeCandyNumLEDs)
	// fmt.Println("self.fadeCandyAddBuffer", self.fadeCandyAddBuffer)
	// fmt.Println("self.fadeCandyPlayingList", self.fadeCandyPlayingList)
	// fmt.Println("self.fadeCandyNextNoteInfo", self.fadeCandyNextNoteInfo)
	// go through all the notes and figure out how far we are through them
	for voicenum, playingEntry := range self.fadeCandyPlayingList {
		percent := float64(currentTime-playingEntry.startTime) / float64(playingEntry.stopTime-playingEntry.startTime)
		fmt.Println("voice", voicenum, "percent", percent, "playingEntry.currentOffset", playingEntry.currentOffset, "startHue", playingEntry.startHue)
		// figure out our hue
		var currentHue byte
		if playingEntry.stopHue == playingEntry.startHue {
			currentHue = playingEntry.startHue
		} else {
			// currentHue = ((playingEntry.stopHue - playingEntry.startHue) * percent) + playingEntry.startHue
			currentHue = byte((float64(playingEntry.stopHue-playingEntry.startHue))*percent) + playingEntry.startHue
		}
		saturation := playingEntry.saturation
		value := byte(percent * 255)
		// convert to RGB
		// r, g, b := HSVToRGB(int(currentHue), int(saturation), int(value))
		// we're going
		var colors [3]int
		colors[0], colors[1], colors[2] = HSVToRGB(int(currentHue), int(saturation), int(value))
		// distribute to lights
		for i := playingEntry.currentOffset; i < lightcount; i += playingEntry.skip {
			z := i * 3
			// loop through color channels
			for j := 0; j < 3; j++ {
				// add with max out at 255
				x := self.fadeCandyAddBuffer[z+j]
				y := x + uint8(colors[j])
				if y < x {
					y = 255
				}
				self.fadeCandyAddBuffer[z+j] = y
			}
		}
	}
	// // os.Exit(1)
}

// --------------------------------------------------------------------------------------------------------------------------------
// end Fadecandy code
// --------------------------------------------------------------------------------------------------------------------------------

// --------------------------------------------------------------------------------------------------------------------------------
// Pure Data Code Starts Here!
// --------------------------------------------------------------------------------------------------------------------------------

type instPureData struct {
	title string
}

func (self *instPureData) init() {
	fmt.Println("Testing UDP messages.")
	serverAddr, err := net.ResolveUDPAddr("udp", "127.0.0.1:13003")
	checkError(err)
	//
	conn, err := net.DialUDP("udp", nil, serverAddr)
	checkError(err)
	//
	defer conn.Close()
	// buf := make([]byte, 8)
	//
	message := "Hello world." + "\n"
	//
	buf := []byte(message)
	//
	fmt.Println("Sending", buf)
	_, err = conn.Write(buf)
	if err != nil {
		fmt.Println(buf, err)
	}
}

func (self *instPureData) play(currentTime int64, duration int64, params []string) {
}

func (self *instPureData) tick(currentTime int64) {
}

// --------------------------------------------------------------------------------------------------------------------------------
// end Pure Data code
// --------------------------------------------------------------------------------------------------------------------------------

// Code for the web sockets server and the note queue that all the input from
// the web sockets server goes into

func websocketsServer(ws *websocket.Conn) {
	var message string
	keepGoing := true
	for keepGoing {
		buf := make([]byte, 256)
		numRead, err := ws.Read(buf)
		if err == nil {
			message = string(buf[0:numRead])
			io.WriteString(ws, "hello from line 23.")
			fmt.Println("Message is: ", message)
			globals.playbackChannel <- message
		} else {
			if err == io.EOF {
				keepGoing = false
			} else {
				fmt.Println(err)
				panic("websockets buffer problem.")
			}
		}
	}
}

func addToQueue(msg string) {
	i := strings.Index(msg, ",")
	currentTimeStr := msg[0:i]
	currentTimeFloat, err := strconv.ParseFloat(currentTimeStr, 64)
	if err != nil {
		fmt.Println(err)
		panic("string to float conversion on current time failed.")
	}
	currentTimeInt := int64(currentTimeFloat * 1000000000)
	msg = msg[i+1:]
	i = strings.Index(msg, ",")
	startTimeStr := msg[0:i]
	startTimeFloat, err := strconv.ParseFloat(startTimeStr, 64)
	if err != nil {
		fmt.Println(err)
		panic("string to float conversion on start time failed.")
	}
	startTimeInt := int64(startTimeFloat * 1000000000)
	serverTime := time.Now().UnixNano()
	timeDiff := currentTimeInt - serverTime
	var newQueEntry action
	newQueEntry.startTime = timeDiff + startTimeInt
	newQueEntry.whatToDo = msg
	globals.actionsQueue = append(globals.actionsQueue, newQueEntry)
	// ok, now we have to sort!
	num := len(globals.actionsQueue)
	if num > 1 {
		if globals.actionsQueue[num-2].startTime > globals.actionsQueue[num-1].startTime {
			i := num - 2
			keepGoing := true
			for keepGoing {
				x := globals.actionsQueue[i].startTime
				globals.actionsQueue[i].startTime = globals.actionsQueue[i+1].startTime
				globals.actionsQueue[i+1].startTime = x
				y := globals.actionsQueue[i].whatToDo
				globals.actionsQueue[i].whatToDo = globals.actionsQueue[i+1].whatToDo
				globals.actionsQueue[i+1].whatToDo = y
				i--
				if i < 0 {
					keepGoing = false
				} else {
					if globals.actionsQueue[i].startTime <= globals.actionsQueue[i+1].startTime {
						keepGoing = false
					}
				}
			}
		}
	}
}

func takeAction(currentTime int64) {
	params := strings.Split(globals.actionsQueue[0].whatToDo, ",")
	// params [1.5 2.5 lights 1 255 0 0]
	durafloat, err := strconv.ParseFloat(params[1], 64)
	if err != nil {
		fmt.Println(err)
		panic("string to float conversion on duration failed.")
	}
	duration := int64(durafloat * 1000000000)
	instrumentname := params[2]
	globals.instTbl[instrumentname].play(currentTime, duration, params)
	// this whacky statement deletes the first element in the list
	i := 0
	globals.actionsQueue = append(globals.actionsQueue[:i], globals.actionsQueue[i+1:]...)
}

func musicPlaybackLoop(ch chan string) {
	keepGoing := true
	for keepGoing {
		// time.Sleep(1000 * time.Millisecond)
		select {
		case msg := <-ch:
			addToQueue(msg)
		default:
			currentTime := time.Now().UnixNano()
			if len(globals.actionsQueue) != 0 {
				if globals.actionsQueue[0].startTime <= currentTime {
					takeAction(currentTime)
				}
			}
			for _, instInf := range globals.instTbl {
				instInf.tick(currentTime)
			}
		}
	}
}

func main() {
	// inintialize instrument lookup table
	globals.instTbl = make(map[string]Instrument)
	var x instDanLights
	x.title = "Dan Lights"
	globals.instTbl["danLights"] = &x
	x.init()
	var y instFadeCandy
	y.title = "Fadecandy"
	globals.instTbl["fadeCandy"] = &y
	y.init()
	var z instPureData
	z.title = "Pure Data"
	globals.instTbl["puredata"] = &z
	z.init()
	//
	// initialize channel for websockets
	ch := make(chan string)
	globals.playbackChannel = ch
	globals.actionsQueue = make([]action, 0)
	// spawn the playback loop and websocket listener
	go musicPlaybackLoop(ch)
	http.Handle("/umtlocal", websocket.Handler(websocketsServer))
	fmt.Println("Websocket Server Running")
	err := http.ListenAndServe(":46398", nil)
	if err != nil {
		fmt.Println(err)
		panic("ListenAndServe: " + err.Error())
	}
}
