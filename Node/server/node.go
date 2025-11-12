package main

import (
	"context"
	"log"
	"net"
	"os"
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
	id    string
}

type Node struct {
	id             string
	clock          *LamportClock
	timestamp      int64
	otherNodes     map[string]proto.NodeServiceClient
	requestCS      bool
	replyCount     int
	delayedReplies []string
	pending        map[string]chan struct{}
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
		requestCS:      false,
		replyCount:     0,
		delayedReplies: make([]string, 0),
		pending:        make(map[string]chan struct{}),
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
		for retries := 0; retries < 5; retries++ {
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
	n.requestCS = true
	n.replyCount = 0
	clock.Tick()
	n.timestamp = clock.GetTime()
	log.Printf("[L=%d] Node %s is requesting access to critical section", n.timestamp, n.id)
	n.mu.Unlock()

	req := &proto.RequestMessage{
		NodeId:    n.id,
		Timestamp: n.timestamp,
	}

	needed := len(n.otherNodes)
	if needed == 0 {
		log.Printf("[L=%d] Node %s entering critical section (no peers)", n.clock.GetTime(), n.id)
		n.EnterCriticalSection()
		return
	}

	var wg sync.WaitGroup
	var mu sync.Mutex

	for peerId, client := range n.otherNodes {
		wg.Add(1)
		go func(pid string, c proto.NodeServiceClient) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			resp, err := c.RequestAccess(ctx, req)
			if err != nil {
				log.Printf("[L=%d] Error requesting access from peer %s: %v", n.clock.GetTime(), pid, err)
				return
			}
			if resp != nil && resp.AccessGranted {
				mu.Lock()
				n.replyCount++
				clock.Tick()
				log.Printf("[L=%d] Node %s received reply from %s (%d/%d)", n.clock.GetTime(), n.id, pid, n.replyCount, needed)
				mu.Unlock()
			}
		}(peerId, client)
	}

	wg.Wait()

	n.mu.Lock()
	if n.replyCount >= needed {
		log.Printf("[L=%d] Node %s entering critical section (got %d/%d replies)", n.clock.GetTime(), n.id, n.replyCount, needed)
		n.mu.Unlock()
		n.EnterCriticalSection()
		return
	}
	n.mu.Unlock()

	log.Printf("[L=%d] Node %s did not receive enough replies (%d/%d), aborting request", n.clock.GetTime(), n.id, n.replyCount, needed)
	n.mu.Lock()
	n.requestCS = false
	n.mu.Unlock()
}

func (n *Node) EnterCriticalSection() {
	log.Printf("[L=%d] Node %s is in the critical section", n.clock.GetTime(), n.id)
	time.Sleep(2 * time.Second)
	log.Printf("[L=%d] Node %s is leaving the critical section", n.clock.GetTime(), n.id)

	n.mu.Lock()
	n.requestCS = false
	for pid, ch := range n.pending {
		select {
		case <-ch:
		default:
			close(ch)
		}
		delete(n.pending, pid)
		log.Printf("[L=%d] Node %s unblocked deferred request from %s", n.clock.GetTime(), n.id, pid)
	}
	n.delayedReplies = make([]string, 0)
	n.mu.Unlock()
}

func (s *server) RequestAccess(ctx context.Context, req *proto.RequestMessage) (*proto.RequestResponse, error) {
	s.clock.UpdateClock(req.Timestamp)

	globalNodesMutex.Lock()
	local := globalNodes[s.id]
	globalNodesMutex.Unlock()

	if local == nil {
		return &proto.RequestResponse{NodeId: req.NodeId, AccessGranted: false}, nil
	}

	local.mu.Lock()

	deferReply := false

	if local.requestCS {
		if local.timestamp < req.Timestamp || (local.timestamp == req.Timestamp && local.id < req.NodeId) {
			deferReply = true
		}
	}

	if deferReply {
		ch := make(chan struct{})
		local.pending[req.NodeId] = ch
		local.delayedReplies = append(local.delayedReplies, req.NodeId)
		log.Printf("[L=%d] Node %s deferring reply to %s (local ts=%d, req ts=%d)", s.clock.GetTime(), local.id, req.NodeId, local.timestamp, req.Timestamp)
		local.mu.Unlock()

		<-ch

		local.clock.Tick()
		return &proto.RequestResponse{NodeId: local.id, AccessGranted: true}, nil
	}

	local.clock.Tick()
	log.Printf("[L=%d] Node %s replying immediately to %s", s.clock.GetTime(), local.id, req.NodeId)
	local.mu.Unlock()
	return &proto.RequestResponse{NodeId: local.id, AccessGranted: true}, nil
}

func (s *server) ReplyAccess(ctx context.Context, reply *proto.ReplyMessage) (*proto.ReplyResponse, error) {
	s.clock.UpdateClock(reply.Timestamp)
	return &proto.ReplyResponse{NodeId: reply.NodeId}, nil
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

	delays := map[string]int{"A": 2, "B": 1, "C": 5}

	for id, node := range globalNodes {
		delay := delays[id]
		go func(n *Node, d int) {
			time.Sleep(time.Duration(d) * time.Second)
			n.RequestCriticalSection(n.clock)
		}(node, delay)
	}
}

func main() {
	logFile, err := os.OpenFile("node.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatalf("Failed to open log file: %v", err)
	}
	defer logFile.Close()

	log.SetOutput(logFile)
	log.SetFlags(0)

	go startNodeServer("A", ":5000")
	time.Sleep(2 * time.Second)
	go startNodeServer("B", ":5001")
	time.Sleep(1 * time.Second)
	go startNodeServer("C", ":5002")
	time.Sleep(3 * time.Second)

	connectAllNodes()

	startRequestToCriticalSection()

	select {}
}

func startNodeServer(nodeID string, port string) {
	server := &server{node: make(map[string]*Node), clock: &LamportClock{Timestamp: 0}, id: nodeID}

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

	log.Printf("[L=%d] Started server for node %s on %s", server.clock.GetTime(), nodeID, port)
	err = grpcServer.Serve(listener)
	if err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
