package main

import (
	"context"
	"log"
	"net"
	proto "node/grpc"
	"sync"

	//proto "github.com/3SemClon/Node/grpc"
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
	id             string
	timestamp      int64
	otherNodes     map[string]proto.NodeServiceClient
	replyChannel   chan bool
	requestCS      bool
	replyCount     int
	delayedReplies []string
	mu             sync.Mutex
}

var nodeAddresses = map[string]string{
	"A": "localhost:5000",
	"B": "localhost:5001",
	"C": "localhost:5002",
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
		id:             id,
		timestamp:      clock.GetTime(),
		otherNodes:     make(map[string]proto.NodeServiceClient),
		replyChannel:   make(chan bool),
		requestCS:      false,
		replyCount:     0,
		delayedReplies: make([]string, 0),
		mu:             sync.Mutex{},
	}
}

func (n *Node) ConnectToPeers() error {
	for id, address := range nodeAddresses {
		if id == n.id {
			continue
		}
		conn, err := grpc.Dial(address, grpc.WithInsecure())
		if err != nil {
			return err
		}
		n.otherNodes[id] = proto.NewNodeServiceClient(conn)
	}
	return nil
}

func (n *Node) RequestCriticalSection(clock *LamportClock) {
	n.mu.Lock()
	n.requestCS = true
	n.replyCount = 0
	n.timestamp = clock.GetTime()
	n.mu.Unlock()

	req := &proto.RequestMessage{
		NodeId:    n.id,
		Timestamp: n.timestamp,
	}
	for peerId, client := range n.otherNodes {
		go func(pid string, c proto.NodeServiceClient) {
			_, err := c.RequestAccess(context.Background(), req)
			if err != nil {
				log.Printf("Error requesting access from peer: %v", err)
			}
		}(peerId, client)
	}
	<-n.replyChannel
	log.Printf("Node %s entering critical section", n.id)
}

func (s *server) RequestAccess(ctx context.Context, req *proto.RequestMessage) (*proto.RequestResponse, error) {
	s.clock.UpdateClock(req.Timestamp)

	node := s.node[req.NodeId]
	node.mu.Lock()
	defer node.mu.Unlock()

	deferReply := false

	if node.requestCS {
		if req.Timestamp < node.timestamp {
			deferReply = true
		} else if req.Timestamp == node.timestamp && req.NodeId < node.id {
			deferReply = true
		}
	}
	if deferReply {
		node.delayedReplies = append(node.delayedReplies, req.NodeId)
	} else {
		client, ok := node.otherNodes[req.NodeId]
		if ok {
			go func(c proto.NodeServiceClient) {
				_, err := c.ReplyAccess(ctx, &proto.ReplyMessage{
					NodeId:        node.id,
					AccessGranted: true,
				})
				if err != nil {
					log.Printf("Error sending reply to node %s: %v", req.NodeId, err)
				} else {
					log.Printf("Replied to node %s immediately", req.NodeId)
				}
			}(client)
		}
	}
	return &proto.RequestResponse{NodeId: node.id, AccessGranted: true}, nil
}

func (s *server) ReplyAccess(ctx context.Context, reply *proto.ReplyMessage) (*proto.RequestResponse, error) {
	s.clock.UpdateClock(s.clock.GetTime())
	node := s.node[reply.NodeId]
	node.mu.Lock()
	defer node.mu.Unlock()

	node.replyCount++
	if node.replyCount == len(node.otherNodes) {
		node.replyChannel <- true
	}

	return &proto.RequestResponse{NodeId: node.id}, nil
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
