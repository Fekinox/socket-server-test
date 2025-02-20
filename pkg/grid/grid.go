package grid

type Grid[T any] struct {
	data   []T
	width  int
	height int
}

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
	return 0 <= x && 0 <= y && g.width > x && g.height > y
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
