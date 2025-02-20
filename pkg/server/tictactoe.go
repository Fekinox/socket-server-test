package server

import "github.com/Fekinox/socket-server-test/pkg/grid"

type TTTStatus int

const (
	NotFinished TTTStatus = iota
	P1Win
	P2Win
	Tie
)

type TicTacToeState struct {
	Grid   grid.TicTacToeGrid
	Turn   int
	Status TTTStatus
}
