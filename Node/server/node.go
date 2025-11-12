package main

import (
	"context"
	"log"
	"net"
	"sync"
	"time"

	proto "node/grpc"

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
	clock          *LamportClock
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

var globalNodes = make(map[string]*Node)
var globalNodesMutex = sync.Mutex{}

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

func NewNode(id string, clock *LamportClock) *Node {
	return &Node{
		id:             id,
		clock:          clock,
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
		var conn *grpc.ClientConn
		var err error
		for retries := 0; retries < 5; retries++ { // retry 5 times
			conn, err = grpc.Dial(address, grpc.WithInsecure())
			if err == nil {
				break
			}
			time.Sleep(200 * time.Millisecond)
		}
		if err != nil {
			return err
		}
		n.otherNodes[id] = proto.NewNodeServiceClient(conn)
	}
	return nil
}

func (n *Node) RequestCriticalSection(clock *LamportClock) {
	n.mu.Lock()
	log.Printf("Node %s is requesting access to critical section", n.id)
	n.requestCS = true
	n.replyCount = 0
	clock.Tick()
	n.timestamp = clock.GetTime()
	n.mu.Unlock()

	req := &proto.RequestMessage{
		NodeId:    n.id,
		Timestamp: n.timestamp,
	}
	for peerId, client := range n.otherNodes {
		go func(pid string, c proto.NodeServiceClient) {
			clock.Tick()
			_, err := c.RequestAccess(context.Background(), req)
			if err != nil {
				log.Printf("Error requesting access from peer: %v", err)
			}
		}(peerId, client)
	}
	<-n.replyChannel
	log.Printf("Node %s entering critical section", n.id)
	n.EnterCriticalSection()
}

func (n *Node) EnterCriticalSection() {
	log.Printf("Node %s is in the critical section", n.id)
	time.Sleep(2 * time.Second)
	log.Printf("Node %s is leaving the critical section", n.id)
	n.mu.Lock()
	n.requestCS = false

	for _, deferredNodeId := range n.delayedReplies {
		client, ok := n.otherNodes[deferredNodeId]
		if ok {
			go func(c proto.NodeServiceClient, nodeId string) {
				n.clock.Tick()
				_, err := c.ReplyAccess(context.Background(), &proto.ReplyMessage{
					NodeId:        n.id,
					AccessGranted: true,
					Timestamp:     n.clock.GetTime(),
				})
				if err != nil {
					log.Printf("Error sending deferred reply to node %s: %v", nodeId, err)
				} else {
					log.Printf("Sent deferred reply to node %s", nodeId)
				}
			}(client, deferredNodeId)
		}
	}
	n.delayedReplies = make([]string, 0)
	n.mu.Unlock()
}

func (s *server) RequestAccess(ctx context.Context, req *proto.RequestMessage) (*proto.RequestResponse, error) {
	s.clock.UpdateClock(req.Timestamp)

	globalNodesMutex.Lock()
	node := globalNodes[req.NodeId]
	globalNodesMutex.Unlock()

	if node == nil {
		return &proto.RequestResponse{NodeId: req.NodeId, AccessGranted: false}, nil
	}

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
				s.clock.Tick()
				_, err := c.ReplyAccess(ctx, &proto.ReplyMessage{
					NodeId:        node.id,
					AccessGranted: true,
					Timestamp:     s.clock.GetTime(),
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

func (s *server) ReplyAccess(ctx context.Context, reply *proto.ReplyMessage) (*proto.ReplyResponse, error) {
	s.clock.UpdateClock(reply.Timestamp)

	globalNodesMutex.Lock()
	node := globalNodes[reply.NodeId]
	globalNodesMutex.Unlock()

	if node == nil {
		return &proto.ReplyResponse{NodeId: reply.NodeId}, nil
	}

	node.mu.Lock()
	defer node.mu.Unlock()

	node.replyCount++
	if node.replyCount == len(node.otherNodes) {
		node.replyChannel <- true
	}

	return &proto.ReplyResponse{NodeId: node.id}, nil
}

func connectAllNodes() {
	globalNodesMutex.Lock()
	defer globalNodesMutex.Unlock()

	for _, node := range globalNodes {
		for peerID, addr := range nodeAddresses {
			if peerID == node.id {
				continue
			}
			conn, err := grpc.Dial(addr, grpc.WithInsecure())
			if err != nil {
				log.Fatalf("Node %s failed to connect to %s: %v", node.id, peerID, err)
			}
			node.otherNodes[peerID] = proto.NewNodeServiceClient(conn)
		}
	}
}

func startRequestToCriticalSection() {
	globalNodesMutex.Lock()
	defer globalNodesMutex.Unlock()

	delays := map[string]int{"A": 5, "B": 10, "C": 15}

	for id, node := range globalNodes {
		delay := delays[id]
		go func(n *Node, d int) {
			time.Sleep(time.Duration(d) * time.Second)
			n.RequestCriticalSection(n.clock)
		}(node, delay)
	}
}

func main() {
	// Start node A on port 5000
	go startNodeServer("A", ":5000")
	time.Sleep(1 * time.Second)
	// Start node B on port 5001
	go startNodeServer("B", ":5001")
	time.Sleep(1 * time.Second)
	// Start node C on port 5002
	go startNodeServer("C", ":5002")
	time.Sleep(2 * time.Second)

	connectAllNodes()

	startRequestToCriticalSection()

	// Keep main alive
	select {}
}

func startNodeServer(nodeID string, port string) {
	server := &server{node: make(map[string]*Node), clock: &LamportClock{Timestamp: 0}}

	grpcServer := grpc.NewServer()
	listener, err := net.Listen("tcp", port)
	if err != nil {
		log.Fatalf("Failed to listen on %s: %v", port, err)
	}

	proto.RegisterNodeServiceServer(grpcServer, server)

	node := NewNode(nodeID, server.clock)

	globalNodesMutex.Lock()
	globalNodes[nodeID] = node
	globalNodesMutex.Unlock()

	err = grpcServer.Serve(listener)
	if err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
