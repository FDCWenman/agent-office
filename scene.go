package main

import (
	"hash/fnv"
	"strconv"
	"time"
)

// Office geometry, in pixels. Each agent gets a station: a bubble area, the
// character in a chair behind a desk, a monitor, and a name plate.
const (
	stationW = 28
	stationH = 26
	wallH    = 10
	sceneTop = 12 // y of the first row of stations

	// Terminal rows needed to show one row of desks.
	minOfficeRows = (sceneTop + stationH) / 2

	doneBubbleFor = 20 * time.Second
)

// Station layout, relative to the station's top-left corner.
const (
	bubbleX, bubbleY   = 7, 0
	personX, personY   = 8, 7
	deskX, deskY       = 1, 16
	deskW              = 24
	monitorX, monitorY = 17, 9
	helperX, helperY   = 2, 12
	plateY             = 24
)

var stateRGB = map[State]rgb{
	StateIdle:       {160, 160, 160},
	StateThinking:   {236, 120, 220},
	StateTyping:     {80, 210, 120},
	StateReading:    {80, 170, 250},
	StateRunning:    {245, 205, 70},
	StateDelegating: {175, 140, 250},
	StateWorking:    {70, 200, 200},
	StateWaiting:    {250, 95, 85},
	StateDone:       {150, 200, 110},
	StateFailed:     {240, 60, 60},
	StateEnded:      {110, 110, 110},
}

var (
	colWall      = rgb{52, 58, 82}
	colWallTrim  = rgb{36, 40, 58}
	colWindow    = rgb{196, 200, 214}
	colGlass     = rgb{128, 186, 228}
	colGlint     = rgb{200, 230, 248}
	colFloor     = rgb{120, 84, 58}
	colFloorSeam = rgb{100, 70, 48}
	colDeskTop   = rgb{184, 132, 86}
	colDeskFront = rgb{138, 94, 60}
	colDeskDark  = rgb{94, 62, 40}
	colChair     = rgb{58, 46, 66}
	colMonitor   = rgb{48, 50, 58}
	colScreenOff = rgb{24, 26, 34}
	colKeyboard  = rgb{196, 198, 206}
	colBubble    = rgb{244, 244, 240}
	colInk       = rgb{40, 40, 48}
	colPlate     = rgb{34, 28, 26}
	colLabel     = rgb{232, 232, 232}
	colLabelDim  = rgb{150, 150, 150}
	colMouth     = rgb{120, 40, 40}
)

// Body without hair: head rows 0-5, torso rows 6-9 (hidden below the desk top).
var bodyArt = []string{
	"........",
	"..ssss..",
	".ssssss.",
	".sesses.",
	".ssssss.",
	"..ssss..",
	".tttttt.",
	"tttttttt",
	"tdttttdt",
	"tdttttdt",
}

var hairStyles = [][]string{
	{ // short
		"..hhhh..",
		".hhhhhh.",
		".h....h.",
	},
	{ // long
		"..hhhh..",
		".hhhhhh.",
		"hh....hh",
		"h......h",
		"h......h",
		"hh....hh",
	},
	{ // spiky
		"h.h..h.h",
		".hhhhhh.",
		".hhhhhh.",
	},
	{ // cap
		"..cccc..",
		".cccccc.",
		"cccccccc",
	},
}

var plantArt = []string{
	".g.g..",
	"gGgGg.",
	".gGgGg",
	"gGgGg.",
	".gggg.",
	".pppp.",
	".pppp.",
	"..pp..",
}

var bubbleArt = []string{
	".bbbbbbb.",
	"bbbbbbbbb",
	"bbbbbbbbb",
	"bbbbbbbbb",
	"bbbbbbbbb",
	".bbbbbbb.",
	"...bb....",
}

var (
	hairColors  = []rgb{{44, 32, 26}, {92, 60, 36}, {206, 164, 86}, {24, 24, 30}, {168, 74, 42}, {140, 140, 150}}
	skinColors  = []rgb{{244, 204, 164}, {226, 176, 132}, {180, 126, 88}, {124, 84, 58}}
	shirtColors = []rgb{{70, 136, 244}, {232, 92, 92}, {92, 190, 120}, {240, 182, 64}, {162, 104, 222}, {62, 190, 190}, {240, 132, 64}}
)

// look is an agent's appearance, fixed by its session id.
type look struct {
	hair, skin, shirt rgb
	style, blink      int
	seed              uint32
}

func lookFor(id string) look {
	h := fnv.New32a()
	h.Write([]byte(id))
	v := h.Sum32()
	return look{
		hair:  hairColors[v%6],
		skin:  skinColors[(v/6)%4],
		shirt: shirtColors[(v/24)%7],
		style: int((v / 168) % 4),
		blink: int((v / 672) % 25),
		seed:  v,
	}
}

func perRow(w int) int { return max(1, (w-2)/stationW) }

// sceneRows is the terminal height of the whole office for n agents.
func sceneRows(n, w int) int {
	slots := max(n, 1)
	return (sceneTop + (slots+perRow(w)-1)/perRow(w)*stationH) / 2
}

// officeLines draws the office w cells wide, cut to at most maxRows rows.
func (m model) officeLines(w, maxRows int) []string {
	agents := m.office.Agents()
	per := perRow(w)
	slots := len(agents)
	if slots == 0 {
		slots = min(2, per) // an empty office still has desks
	}
	rows := (slots + per - 1) / per
	c := newCanvas(w, sceneTop+rows*stationH, colFloor)
	margin := (w - per*stationW) / 2
	drawRoom(c, margin)

	type label struct {
		x, row int
		parts  []textPart
	}
	var labels []label
	for i := 0; i < slots; i++ {
		x0 := margin + (i%per)*stationW
		y0 := sceneTop + (i/per)*stationH
		var a *Agent
		if i < len(agents) {
			a = agents[i]
		}
		drawStation(c, x0, y0, a, m.frame, m.now)

		parts := m.plate(a, i)
		if i > 0 && a == nil {
			parts = nil
		}
		if len(parts) == 0 {
			continue
		}
		lw := 0
		for _, p := range parts {
			lw += len([]rune(p.s))
		}
		lx := x0 + (stationW-lw)/2
		c.rect(lx-1, y0+plateY, lw+2, 2, colPlate)
		labels = append(labels, label{x: lx, row: (y0 + plateY) / 2, parts: parts})
	}

	cells := c.cells()
	for _, l := range labels {
		x := l.x
		for _, p := range l.parts {
			x = putText(cells[l.row], x, p.s, p.fg, p.bold)
		}
	}
	if len(cells) > maxRows {
		cells = cells[:maxRows]
	}
	out := make([]string, len(cells))
	for i, row := range cells {
		out[i] = renderRow(row)
	}
	return out
}

type textPart struct {
	s    string
	fg   rgb
	bold bool
}

// plate is the name plate under station i.
func (m model) plate(a *Agent, i int) []textPart {
	if a == nil {
		return []textPart{{s: "waiting for agents", fg: colLabelDim}}
	}
	name := truncate(m.office.Name(a), stationW-8)
	var parts []textPart
	selected := i == m.selected
	if selected {
		parts = append(parts, textPart{s: "▶ ", fg: colLabel, bold: true})
	}
	parts = append(parts, textPart{s: "● ", fg: stateRGB[a.State]})
	switch {
	case a.State == StateEnded:
		parts = append(parts, textPart{s: name + " (ended)", fg: colLabelDim})
	default:
		parts = append(parts, textPart{s: name, fg: colLabel, bold: selected})
	}
	if a.Subagents > 2 {
		parts = append(parts, textPart{s: " +" + strconv.Itoa(a.Subagents), fg: stateRGB[StateDelegating]})
	}
	return parts
}

func drawRoom(c *canvas, margin int) {
	for y := wallH; y < c.h; y++ { // wooden planks
		for x := 0; x < c.w; x++ {
			if y%4 == 1 || (x+(y/4%2)*9)%18 == 0 {
				c.set(x, y, colFloorSeam)
			}
		}
	}
	c.rect(0, 0, c.w, wallH, colWall)
	c.rect(0, wallH-2, c.w, 2, colWallTrim)
	for x := 6; x+11 <= c.w-4; x += 34 {
		c.rect(x, 1, 11, 6, colWindow)
		c.rect(x+1, 2, 4, 4, colGlass)
		c.rect(x+6, 2, 4, 4, colGlass)
		c.set(x+2, 3, colGlint)
		c.set(x+7, 3, colGlint)
	}
	if margin >= 8 {
		pal := map[byte]rgb{'g': {60, 150, 70}, 'G': {40, 118, 54}, 'p': {170, 92, 60}}
		c.sprite(1, wallH-4, plantArt, pal)
		c.sprite(c.w-7, wallH-4, plantArt, pal)
	}
}

// drawStation draws one desk. a == nil is an empty desk.
func drawStation(c *canvas, x0, y0 int, a *Agent, frame int, now time.Time) {
	present := a != nil && a.State != StateEnded
	var lk look
	if a != nil {
		lk = lookFor(a.SessionID)
	}

	c.rect(x0+deskX, y0+deskY+6, deskW, 2, colFloorSeam) // shadow under the desk
	c.rect(x0+personX, y0+12, 8, 4, colChair)
	c.rect(x0+personX-1, y0+13, 10, 3, colChair)
	if present {
		drawPerson(c, x0+personX, y0+personY, a, lk, frame)
	}

	c.rect(x0+deskX, y0+deskY, deskW, 2, colDeskTop)
	c.rect(x0+deskX, y0+deskY+2, deskW, 4, colDeskFront)
	c.rect(x0+deskX, y0+deskY+2, deskW, 1, colDeskDark)
	c.rect(x0+deskX+10, y0+deskY+4, 4, 1, colDeskDark) // drawer handle
	c.rect(x0+deskX+1, y0+deskY+6, 1, 2, colDeskDark)  // legs
	c.rect(x0+deskX+deskW-2, y0+deskY+6, 1, 2, colDeskDark)
	c.rect(x0+personX+2, y0+deskY, 4, 1, colKeyboard)

	if present {
		drawHands(c, x0+personX, y0+deskY, a.State, lk, frame)
	}
	drawMonitor(c, x0+monitorX, y0+monitorY, a, lk, frame)
	if present {
		drawHelpers(c, x0+helperX, y0+helperY, a.Subagents, lk, frame)
		drawBubble(c, x0+bubbleX, y0+bubbleY, a, frame, now)
	}
}

func drawPerson(c *canvas, x, y int, a *Agent, lk look, frame int) {
	pal := map[byte]rgb{
		's': lk.skin, 'e': colInk, 'h': lk.hair, 'M': colMouth,
		't': lk.shirt, 'd': lk.shirt.shade(0.75), 'c': lk.shirt.shade(0.6),
	}
	c.sprite(x, y, bodyArt, pal)

	eyes := ".sesses."
	switch {
	case (frame+lk.blink)%25 == 0:
		eyes = ".ssssss."
	case a.State == StateReading && (frame/5)%2 == 1:
		eyes = ".ssesse." // glancing at the screen
	}
	c.sprite(x, y+3, []string{eyes}, pal)
	c.sprite(x, y, hairStyles[lk.style], pal)

	if a.State == StateWaiting { // open mouth, waving arm
		c.sprite(x, y+4, []string{"...MM..."}, pal)
		wave := (frame / 2) % 2
		c.rect(x+8, y+6, 1, 2, lk.shirt)
		c.set(x+8+wave, y+5, lk.shirt)
		c.set(x+8+wave, y+4, lk.skin)
	}
}

func drawHands(c *canvas, x, deskTop int, s State, lk look, frame int) {
	l, r := deskTop, deskTop
	if s == StateTyping || s == StateRunning || s == StateWorking {
		if frame%2 == 0 {
			l--
		} else {
			r--
		}
	}
	c.set(x+1, l, lk.skin)
	c.set(x+6, r, lk.skin)
}

func drawMonitor(c *canvas, x, y int, a *Agent, lk look, frame int) {
	c.rect(x, y, 9, 6, colMonitor)
	c.set(x+4, y+6, colMonitor)
	c.rect(x+3, y+7, 3, 1, colMonitor)
	s := StateIdle
	if a != nil {
		s = a.State
	}
	drawScreen(c, x+1, y+1, s, lk.seed, frame)
}

// drawScreen fills the 7×4 monitor screen to show what the agent is doing.
func drawScreen(c *canvas, x, y int, s State, seed uint32, frame int) {
	const w, h = 7, 4
	lineLen := func(i int) int { return int(mix(seed, i)%5) + 2 }
	switch s {
	case StateTyping: // code scrolling up, the last line being typed
		c.rect(x, y, w, h, rgb{28, 32, 44})
		cols := []rgb{{120, 200, 255}, {240, 160, 90}, {150, 220, 130}, {220, 130, 220}}
		step := frame / 2
		for i := 0; i < h-1; i++ {
			c.rect(x, y+i, lineLen(step+i), 1, cols[(step+i)%len(cols)])
		}
		c.rect(x, y+h-1, frame%6+1, 1, cols[(step+h-1)%len(cols)])
	case StateReading: // a page with a moving highlight
		c.rect(x, y, w, h, rgb{226, 226, 216})
		for i := 0; i < h; i++ {
			col := rgb{150, 150, 160}
			if i == (frame/3)%h {
				col = rgb{250, 214, 90}
			}
			c.rect(x, y+i, lineLen(i)+1, 1, col)
		}
	case StateRunning: // terminal output with a blinking cursor
		c.rect(x, y, w, h, rgb{12, 14, 12})
		n := (frame / 3) % (h + 1)
		for i := 0; i < n && i < h; i++ {
			c.rect(x, y+i, lineLen(i), 1, rgb{90, 220, 110})
		}
		if frame%2 == 0 && n < h {
			c.set(x, y+n, rgb{200, 255, 200})
		}
	case StateThinking:
		c.rect(x, y, w, h, rgb{44, 32, 64})
		c.rect(x+frame%w, y+1, 1, 2, stateRGB[StateThinking])
	case StateDelegating: // a little tree of tasks
		c.rect(x, y, w, h, rgb{44, 30, 72})
		col := stateRGB[StateDelegating]
		c.rect(x, y, 3, 1, col)
		for i := 1; i < h; i++ {
			c.set(x+1, y+i, col.shade(0.6))
			if (frame/2)%h >= i {
				c.rect(x+2, y+i, 3, 1, col)
			}
		}
	case StateWorking:
		c.rect(x, y, w, h, rgb{18, 44, 50})
		for i := 0; i < h; i++ {
			c.rect(x, y+i, lineLen(frame/3+i), 1, stateRGB[StateWorking])
		}
	case StateWaiting: // flashing "!"
		bg := rgb{210, 64, 52}
		if (frame/3)%2 == 1 {
			bg = rgb{130, 36, 30}
		}
		c.rect(x, y, w, h, bg)
		c.rect(x+3, y, 1, 2, colBubble)
		c.set(x+3, y+3, colBubble)
	case StateDone:
		c.rect(x, y, w, h, rgb{26, 62, 38})
		drawCheck(c, x, y-1, rgb{130, 230, 120})
	case StateFailed:
		c.rect(x, y, w, h, rgb{96, 20, 20})
		drawCross(c, x, y, rgb{250, 120, 120})
	default: // idle, ended, empty desk
		c.rect(x, y, w, h, colScreenOff)
		c.set(x, y, colScreenOff.shade(1.8))
	}
}

func drawCheck(c *canvas, x, y int, col rgb) {
	for _, p := range [][2]int{{1, 3}, {2, 4}, {3, 3}, {4, 2}, {5, 1}} {
		c.set(x+p[0], y+p[1], col)
	}
}

func drawCross(c *canvas, x, y int, col rgb) {
	for _, p := range [][2]int{{2, 0}, {4, 0}, {3, 1}, {2, 2}, {4, 2}} {
		c.set(x+p[0], y+p[1], col)
	}
}

// drawHelpers shows up to two running subagents as tiny figures on the desk.
func drawHelpers(c *canvas, x, y, n int, lk look, frame int) {
	pal := map[byte]rgb{'h': lk.hair.shade(1.3), 's': lk.skin, 't': lk.shirt.shade(1.25)}
	for i := 0; i < n && i < 2; i++ {
		bob := (frame/2 + i) % 2
		c.sprite(x+i*3, y-bob, []string{"hh", "ss", "tt", "tt"}, pal)
	}
}

func drawBubble(c *canvas, x, y int, a *Agent, frame int, now time.Time) {
	bubble := func(col rgb) { c.sprite(x, y, bubbleArt, map[byte]rgb{'b': col}) }
	switch a.State {
	case StateThinking: // "..." filling in
		bubble(colBubble)
		for i := 0; i < (frame/3)%4; i++ {
			c.rect(x+2+i*2, y+2, 1, 2, colInk)
		}
	case StateWaiting: // flashing "!"
		bg, fg := colBubble, stateRGB[StateWaiting]
		if (frame/3)%2 == 1 {
			bg, fg = fg, colBubble
		}
		bubble(bg)
		c.rect(x+4, y+1, 1, 2, fg)
		c.set(x+4, y+4, fg)
	case StateDone:
		if now.Sub(a.Since) < doneBubbleFor {
			bubble(colBubble)
			drawCheck(c, x+1, y, rgb{60, 170, 70})
		}
	case StateFailed:
		bubble(colBubble)
		drawCross(c, x+1, y+1, stateRGB[StateFailed])
	}
}

// mix is a small integer hash for stable pseudo-random line lengths.
func mix(seed uint32, i int) uint32 {
	v := seed ^ uint32(i)*2654435761
	v ^= v >> 15
	v *= 2246822519
	v ^= v >> 13
	return v
}
