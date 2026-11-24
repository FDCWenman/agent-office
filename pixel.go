package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// rgb is a 24-bit colour. The office is drawn in truecolor.
type rgb struct{ R, G, B uint8 }

// shade scales the colour: f < 1 darkens, f > 1 lightens.
func (c rgb) shade(f float64) rgb {
	ch := func(v uint8) uint8 {
		x := float64(v) * f
		if x > 255 {
			x = 255
		}
		return uint8(x)
	}
	return rgb{ch(c.R), ch(c.G), ch(c.B)}
}

func (c rgb) hex() string { return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B) }

// canvas is a pixel buffer. Two pixel rows make one terminal row: each cell
// is drawn as "▀" with the top pixel as foreground and the bottom one as
// background, so pixels come out roughly square.
type canvas struct {
	w, h int
	px   []rgb
}

func newCanvas(w, h int, fill rgb) *canvas {
	h += h % 2 // whole terminal rows only
	c := &canvas{w: w, h: h, px: make([]rgb, w*h)}
	for i := range c.px {
		c.px[i] = fill
	}
	return c
}

func (c *canvas) set(x, y int, col rgb) {
	if x >= 0 && y >= 0 && x < c.w && y < c.h {
		c.px[y*c.w+x] = col
	}
}

func (c *canvas) at(x, y int) rgb { return c.px[y*c.w+x] }

func (c *canvas) rect(x, y, w, h int, col rgb) {
	for j := y; j < y+h; j++ {
		for i := x; i < x+w; i++ {
			c.set(i, j, col)
		}
	}
}

// sprite draws art at (x, y). Each byte is looked up in pal; '.' and bytes
// missing from pal are transparent.
func (c *canvas) sprite(x, y int, art []string, pal map[byte]rgb) {
	for j, row := range art {
		for i := 0; i < len(row); i++ {
			if col, ok := pal[row[i]]; ok {
				c.set(x+i, y+j, col)
			}
		}
	}
}

type cell struct {
	ch     rune
	fg, bg rgb
	bold   bool
}

func (c *canvas) cells() [][]cell {
	rows := make([][]cell, c.h/2)
	for r := range rows {
		row := make([]cell, c.w)
		for x := range row {
			top, bot := c.at(x, 2*r), c.at(x, 2*r+1)
			if top == bot {
				row[x] = cell{ch: ' ', bg: top}
			} else {
				row[x] = cell{ch: '▀', fg: top, bg: bot}
			}
		}
		rows[r] = row
	}
	return rows
}

// putText writes s into row from column x over the picture, keeping the
// colour behind each cell as its background. It returns the next column.
func putText(row []cell, x int, s string, fg rgb, bold bool) int {
	for _, ch := range s {
		if x >= len(row) {
			break
		}
		if ansi.StringWidth(string(ch)) != 1 {
			ch = '?' // keep one rune per cell
		}
		if x >= 0 {
			bg := row[x].bg
			if row[x].ch == '▀' {
				bg = row[x].fg
			}
			row[x] = cell{ch: ch, fg: fg, bg: bg, bold: bold}
		}
		x++
	}
	return x
}

// renderRow turns cells into a string with truecolor escapes, emitting a new
// escape only when the colours change.
func renderRow(row []cell) string {
	var b strings.Builder
	var cur cell
	started := false
	for _, c := range row {
		if c.ch == ' ' {
			c.fg = cur.fg // a space shows no foreground; don't switch for it
		}
		if !started || c.fg != cur.fg || c.bg != cur.bg || c.bold != cur.bold {
			b.WriteString("\x1b[0")
			if c.bold {
				b.WriteString(";1")
			}
			fmt.Fprintf(&b, ";38;2;%d;%d;%d;48;2;%d;%d;%dm", c.fg.R, c.fg.G, c.fg.B, c.bg.R, c.bg.G, c.bg.B)
			cur, started = c, true
		}
		b.WriteRune(c.ch)
	}
	b.WriteString("\x1b[0m")
	return b.String()
}
