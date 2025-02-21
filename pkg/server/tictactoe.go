package server

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Fekinox/socket-server-test/pkg/grid"
)

type TTTStatus int

type TTTGrid = grid.Grid[int]

type ExpandDirection int

const (
	ExpandUp ExpandDirection = iota
	ExpandDown
	ExpandLeft
	ExpandRight
)

const (
	NotFinished TTTStatus = iota
	P1Win
	P2Win
	Tie
)

const (
	TTT_K           = 4
	TTT_INIT_WIDTH  = 4
	TTT_INIT_HEIGHT = 4
)

// Tic Tac Toe game state.
type TTTState struct {
	Grid         *TTTGrid
	Turn         int
	Status       TTTStatus
	WinningMove  grid.Pos
	WinningTiles []grid.Pos
}

func InitialTTTState() *TTTState {
	return &TTTState{
		Grid: grid.NewGrid(int(TTT_INIT_WIDTH), int(TTT_INIT_HEIGHT), 0),
		Turn: 1,
	}
}

func findKInARowAt(g *TTTGrid, x, y, dx, dy, k int) ([]grid.Pos, bool) {
	if !g.InBounds(x, y) {
		return nil, false
	}
	first := g.MustGet(x, y)
	if first == 0 {
		return nil, false
	}
	xx, yy := x, y
	row := []grid.Pos{{
		X: x,
		Y: y,
	}}
	for range k - 1 {
		xx, yy = xx+dx, yy+dy
		if v, ok := g.Get(xx, yy); !ok || v != first {
			return nil, false
		}
		row = append(row, grid.Pos{
			X: xx,
			Y: yy,
		})
	}

	return row, true
}

func allKInARowsAt(g *TTTGrid, x, y, k int) [][]grid.Pos {
	var rows [][]grid.Pos
	for xx := range 3 {
		dx := xx - 1
		for yy := range 3 {
			dy := yy - 1
			if dx == 0 && dy == 0 {
				continue
			}
			row, ok := findKInARowAt(g, x, y, dx, dy, k)
			if ok {
				rows = append(rows, row)
			}
		}
	}

	return rows
}

func expand(g *TTTGrid, dir ExpandDirection) *TTTGrid {
	var ox, oy int
	nw, nh := g.Width(), g.Height()
	switch dir {
	case ExpandUp:
		nh = nh + 1
		oy = 1
	case ExpandDown:
		nh = nh + 1
	case ExpandLeft:
		nw = nw + 1
		ox = 1
	case ExpandRight:
		nw = nw + 1
	}

	return grid.NewGridWith(nw, nh, func(x, y int) int {
		if v, ok := g.Get(x-ox, y-oy); ok {
			return v
		} else {
			return 0
		}
	})
}

func NextMove(ts *TTTState, move Move) (*TTTState, error) {
	nextState := &TTTState{
		Turn:   2 - ts.Turn + 1,
		Status: ts.Status,
	}

	switch m := move.(type) {
	case Mark:
		xx, yy := m.X-1, m.Y-1
		v, ok := ts.Grid.Get(xx, yy)
		if !ok {
			return nil, fmt.Errorf("Position %d %d is out of bounds", m.X, m.Y)
		}
		if v != 0 {
			return nil, errors.New("Grid cell is occupied")
		}
		nextState.Grid = grid.NewGridWith(ts.Grid.Width(), ts.Grid.Height(), func(x, y int) int {
			if x == xx && y == yy {
				return ts.Turn
			}
			return ts.Grid.MustGet(x, y)
		})

		// Check for win
		rows := allKInARowsAt(nextState.Grid, xx, yy, TTT_K)
		if len(rows) > 0 {
			var posns []grid.Pos
			for _, r := range rows {
				posns = append(posns, r...)
			}

			nextState.WinningMove = grid.Pos{
				X: m.X,
				Y: m.Y,
			}
			nextState.WinningTiles = posns
			if ts.Turn == 1 {
				nextState.Status = P1Win
			} else {
				nextState.Status = P2Win
			}

			return nextState, nil
		}

		// Would check for tie, but ties are impossible in this variant of tic tac toe

	case Expand:
		nextState.Grid = expand(ts.Grid, ExpandDirection(m))
	}

	return nextState, nil
}

func (g *TTTState) GameStateToStrings() []string {
	var res []string
	for y := range g.Grid.Height() {
		var cur strings.Builder
		for x := range g.Grid.Width() {
			v := g.Grid.MustGet(x, y)
			switch v {
			case 0:
				cur.WriteByte('.')
			case 1:
				cur.WriteByte('X')
			case 2:
				cur.WriteByte('O')
			}
		}
		res = append(res, cur.String())
	}

	return res
}
