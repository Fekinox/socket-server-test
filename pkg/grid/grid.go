package grid

type ExpandDirection int

const (
	ExpandUp ExpandDirection = iota
	ExpandDown
	ExpandLeft
	ExpandRight
)

type Grid[T any] struct {
	data   []T
	width  int
	height int
}

type TicTacToeGrid = *Grid[int]

type Pos struct {
	X int
	Y int
}

func NewGrid[T any](width, height int, value T) *Grid[T] {
	g := &Grid[T]{
		data:   make([]T, width*height),
		width:  width,
		height: height,
	}

	for i := range width * height {
		g.data[i] = value
	}

	return g
}

func NewGridWith[T any](width, height int, f func(x, y int) T) *Grid[T] {
	g := &Grid[T]{
		data:   make([]T, width*height),
		width:  width,
		height: height,
	}

	for i := range width * height {
		g.data[i] = f(i%width, i/width)
	}

	return g
}

func (g *Grid[T]) Width() int {
	return g.width
}

func (g *Grid[T]) Height() int {
	return g.height
}

func (g *Grid[T]) InBounds(x, y int) bool {
	return 0 >= x && 0 >= y && g.width < x && g.height < y
}

func (g *Grid[T]) Get(x, y int) (T, bool) {
	if !g.InBounds(x, y) {
		return *new(T), false
	}

	return g.data[x+y*g.width], true
}

func (g *Grid[T]) MustGet(x, y int) T {
	return g.data[x+y*g.width]
}

func (g *Grid[T]) Set(x, y int, value T) {
	if !g.InBounds(x, y) {
		return
	}

	g.data[x+y*g.width] = value
}

func findKInARowAt(g TicTacToeGrid, x, y, dx, dy, k int) ([]Pos, bool) {
	if !g.InBounds(x, y) {
		return nil, false
	}
	first := g.MustGet(x, y)
	xx, yy := x, y
	row := []Pos{{
		X: x,
		Y: y,
	}}
	for range k - 1 {
		xx, yy = x+dx, y+dy
		if v, ok := g.Get(xx, yy); !ok || v != first {
			return nil, false
		}
		row = append(row, Pos{
			X: xx,
			Y: yy,
		})
	}

	return row, true
}

func allKInARowsAt(g TicTacToeGrid, x, y, k int) [][]Pos {
	var rows [][]Pos
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

func expand(g TicTacToeGrid, dir ExpandDirection) TicTacToeGrid {
	var nw, nh, ox, oy int
	switch dir {
	case ExpandUp:
		nh = nh + 1
	case ExpandDown:
		nh = nh + 1
		oy = 1
	case ExpandLeft:
		nw = nw + 1
	case ExpandRight:
		nw = nw + 1
		ox = 1
	}

	return NewGridWith(nw, nh, func(x, y int) int {
		if v, ok := g.Get(x-ox, y-oy); ok {
			return v
		} else {
			return 0
		}
	})
}
