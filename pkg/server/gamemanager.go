package server

import (
	"fmt"
	"log"
	"math/rand"
	"slices"
	"strings"
	"sync"
)

const (
	LOBBY_GEN_MAX_ATTEMPTS = 10
)

func newLobbyName() string {
	var sb strings.Builder

	for range 4 {
		sb.WriteByte(byte(rand.Intn(26)) + 'A')
	}

	return sb.String()
}

type GameManager struct {
	mu sync.Mutex

	Lobbies     map[string]*Lobby
	UserLobbies map[string]string
}

func NewGameManager() *GameManager {
	return &GameManager{
		Lobbies:     make(map[string]*Lobby),
		UserLobbies: make(map[string]string),
	}
}

type Lobby struct {
	Name  string
	Host  string
	Users []string
}

func (g *GameManager) NewLobby(ws *SocketServer, host string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	var name string
	var attempts int
	for {
		if attempts >= LOBBY_GEN_MAX_ATTEMPTS {
			ws.BroadcastText("Cannot generate lobby at this time", host)
			return
		}
		name = newLobbyName()
		if _, ok := g.Lobbies[name]; !ok {
			break
		}
		attempts++
	}

	lobby := &Lobby{
		Name:  name,
		Host:  host,
		Users: []string{host},
	}

	g.Lobbies[name] = lobby
	g.UserLobbies[host] = name

	ws.BroadcastText(fmt.Sprintf("You have created a lobby named %s", name), host)
}

func (g *GameManager) removeFromLobby(ws *SocketServer, user string) {
	lbName, ok := g.UserLobbies[user]
	if !ok {
		ws.BroadcastText("You are not in a lobby", user)
		return
	}

	lb, ok := g.Lobbies[lbName]
	if !ok {
		return
	}
	lb.Users = slices.DeleteFunc(lb.Users, func(u string) bool {
		return u == user
	})

	ws.BroadcastText(fmt.Sprintf("%s has left the lobby", user), lb.Users...)

	if lb.Host == user && len(lb.Users) > 0 {
		lb.Host = lb.Users[0]

		ws.BroadcastText("You are now the host", lb.Host)
	}

	if len(lb.Users) == 0 {
		delete(g.Lobbies, lbName)
	}

	delete(g.UserLobbies, user)

	log.Println("remove starting")
	ws.BroadcastText("You have left the lobby", user)
	log.Println("remove done")
}

func (g *GameManager) RemoveFromLobby(ws *SocketServer, user string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.removeFromLobby(ws, user)
}

func (g *GameManager) JoinLobby(ws *SocketServer, user, lobby string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if _, ok := g.UserLobbies[user]; ok {
		g.removeFromLobby(ws, user)
	}

	lb, ok := g.Lobbies[lobby]
	if !ok {
		ws.BroadcastText(fmt.Sprintf("Lobby %s does not exist", lobby), user)
		return
	}

	if len(lb.Users) >= 2 {
		ws.BroadcastText(fmt.Sprintf("Lobby %s is full", lobby), user)
		return
	}

	if slices.Contains(lb.Users, user) {
		ws.BroadcastText(fmt.Sprintf("Already in lobby %s", lobby), user)
		return
	}

	ws.BroadcastText(fmt.Sprintf("%s is joining the lobby", user), lb.Users...)

	lb.Users = append(lb.Users, user)
	g.UserLobbies[user] = lobby

	ws.BroadcastText(fmt.Sprintf("You have joined lobby %s", lobby), user)
	g.lobbyInfo(ws, user)

}

func (g *GameManager) SayInLobby(ws *SocketServer, user, message string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	lbName, ok := g.UserLobbies[user]
	if !ok {
		ws.BroadcastText("You are not in a lobby", user)
		return
	}

	lb, ok := g.Lobbies[lbName]
	if !ok {
		return
	}

	ws.BroadcastText(fmt.Sprintf("%s: %s", user, message), lb.Users...)
}

func (g *GameManager) lobbyInfo(ws *SocketServer, user string) {
	lbName, ok := g.UserLobbies[user]
	if !ok {
		ws.BroadcastText("You are not in a lobby", user)
		return
	}

	lb, ok := g.Lobbies[lbName]
	if !ok {
		ws.BroadcastText("You are not in a lobby", user)
		return
	}

	ws.BroadcastText(fmt.Sprintf("Lobby: %s", lbName), user)
	ws.BroadcastText("Users:", user)
	for _, u := range lb.Users {
		if u == lb.Host {
			ws.BroadcastText(fmt.Sprintf("%s (host)", u), user)
		} else {
			ws.BroadcastText(u, user)
		}
	}
}

func (g *GameManager) LobbyInfo(ws *SocketServer, user string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.lobbyInfo(ws, user)
}
