package server

type Move interface {
	move()
}

type Mark struct {
	X int
	Y int
}

type Expand ExpandDirection

func (m Mark) move()   {}
func (e Expand) move() {}
