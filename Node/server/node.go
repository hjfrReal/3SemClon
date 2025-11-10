package main

import (
	"log"
	"net"
	"sync"
	"time"

	proto "github.com/3SemClon/Node/grpc"
	"google.golang.org/grpc"
)

type LamportClock struct {
	id        int64
	Timestamp int64
	mu        sync.Mutex
}

type server struct {
	proto.UnimplementedNodeServiceServer
	node  map[string]*Node
	clock *LamportClock
}

type Node struct {
	id           string
	timestamp    int64
	otherNodes   []proto.NodeServiceClient
	replyChannel chan string
}

func (lc *LamportClock) Tick() {
	lc.mu.Lock()
	lc.Timestamp++
	lc.mu.Unlock()
}

func (lc *LamportClock) UpdateClock(receivedTimestamp int64) {
	lc.mu.Lock()
	if receivedTimestamp > lc.Timestamp {
		lc.Timestamp = receivedTimestamp
	}
	lc.Timestamp++
	lc.mu.Unlock()
}
func (lc *LamportClock) GetTime() int64 {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	return lc.Timestamp
}

func NewNode(id string) *Node {
	return &Node{
		id:           id,
		timestamp:    time.Now().Unix(),
		otherNodes:   make([]proto.NodeServiceClient, 0),
		replyChannel: make(chan string),
	}
}

func main() {
	server := &server{node: make(map[string]*Node), clock: &LamportClock{Timestamp: 0}}

	server.start_server()
}
func (s *server) start_server() {
	grpcServer := grpc.NewServer()
	listener, err := net.Listen("tcp", ":5050")
	if err != nil {
		log.Fatalf("Did not work")
	}

	proto.RegisterNodeServiceServer(grpcServer, s)

	err = grpcServer.Serve(listener)

	if err != nil {
		log.Fatalf("Did not work")
	}
}
