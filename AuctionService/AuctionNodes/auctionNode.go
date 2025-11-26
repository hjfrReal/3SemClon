package main

import (
	"context"
	"flag"
	"log"
	"net"
	"sync"
	"time"

	proto "github.com/3SemClon/AuctionService/grpc"
	"google.golang.org/grpc"
)

type LamportClock struct {
	id        int64
	Timestamp int64
	mu        sync.Mutex
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

type AuctionState struct {
	AuctionOpen   bool
	StartTime     time.Time
	HighestBid    int
	HighestBidder string
	BidsByClient  map[string]int
}

type AuctionServiceNode struct {
	proto.UnimplementedAuctionServiceServer
	state             AuctionState
	isPrimary         bool
	nodeID            string
	peers             map[string]proto.AuctionServiceClient
	allNodes          map[string]string
	primaryID         string
	lastHeartbeat     time.Time
	lastElection      time.Time
	electionInProcess bool
	mu                sync.Mutex
}

func NewAuctionServiceNode(id string, allNodes map[string]string) *AuctionServiceNode {
	peers := make(map[string]proto.AuctionServiceClient)

	for peerID, addr := range allNodes {
		if peerID == id {
			continue
		}
		conn, err := grpc.Dial(addr, grpc.WithInsecure())
		if err != nil {
			log.Printf("[WARN] Failed to connect to peer %s at %s: %v", peerID, addr, err)
			continue
		}
		peers[peerID] = proto.NewAuctionServiceClient(conn)
	}

	primary := id
	for n := range allNodes {
		if n > primary {
			primary = n
		}
	}

	return &AuctionServiceNode{
		state: AuctionState{
			AuctionOpen:   true,
			StartTime:     time.Now(),
			HighestBid:    0,
			HighestBidder: "",
			BidsByClient:  make(map[string]int),
		},
		isPrimary:     id == primary,
		nodeID:        id,
		peers:         peers,
		primaryID:     primary,
		lastHeartbeat: time.Now(),
		lastElection:  time.Now(),
	}
}

func (node *AuctionServiceNode) Heartbeat(ctx context.Context, req *proto.HeartbeatRequest) (*proto.HeartbeatResponse, error) {
	node.mu.Lock()
	node.lastHeartbeat = time.Now()
	node.primaryID = req.NodeId
	node.mu.Unlock()
	return &proto.HeartbeatResponse{Ok: true}, nil
}

func (node *AuctionServiceNode) startHeartbeatSender() {
	if !node.isPrimary {
		return
	}

	ticker := time.NewTicker(1 * time.Second)
	for range ticker.C {
		node.mu.Lock()
		for id, peer := range node.peers {
			go func(pid string, c proto.AuctionServiceClient) {
				ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
				defer cancel()
				_, err := c.Heartbeat(ctx, &proto.HeartbeatRequest{NodeId: node.nodeID})
				if err != nil {
					log.Printf("[PRIMARY %s] cannot reach %s: %v", node.nodeID, pid, err)
				}
			}(id, peer)
		}
		node.mu.Unlock()
	}
}

func (node *AuctionServiceNode) monitorPrimary() {
	if node.isPrimary {
		return
	}

	ticker := time.NewTicker(1 * time.Second)
	for range ticker.C {

		node.mu.Lock()
		expired := time.Since(node.lastHeartbeat) > 3*time.Second
		node.mu.Unlock()

		if expired {
			node.startElection()
		}
	}
}

func (node *AuctionServiceNode) startElection() {
	node.mu.Lock()
	defer node.mu.Unlock()
	if node.electionInProcess || time.Since(node.lastElection) < 2*time.Second {
		return
	}
	node.lastElection = time.Now()
	node.electionInProcess = true
	defer func() { node.electionInProcess = false }()

	// simple rule: highest nodeID among all nodes
	candidate := node.nodeID
	for n := range node.allNodes {
		if n > candidate {
			candidate = n
		}
	}

	if candidate == node.nodeID {
		// promote self
		node.isPrimary = true
		node.primaryID = node.nodeID
		node.lastHeartbeat = time.Now()
		log.Printf("[%s] ELECTED as NEW PRIMARY", node.nodeID)

		// start heartbeat sender in background
		go node.startHeartbeatSender()
	} else {
		// another node should be primary
		node.isPrimary = false
		node.primaryID = candidate
	}
}

func (node *AuctionServiceNode) Bid(ctx context.Context, req *proto.BidRequest) (*proto.BidResponse, error) {

	node.mu.Lock()
	isPrimary := node.isPrimary
	currentPrimary := node.primaryID
	node.mu.Unlock()

	// forward if not primary
	if !isPrimary {
		primaryClient, ok := node.peers[currentPrimary]
		if !ok {
			return &proto.BidResponse{Success: false, Message: "Primary unavailable"}, nil
		}
		return primaryClient.Bid(ctx, req)
	}

	// PRIMARY HANDLES BID
	node.mu.Lock()
	if !node.state.AuctionOpen {
		node.mu.Unlock()
		return &proto.BidResponse{Success: false, Message: "Auction closed"}, nil
	}
	if int(req.Amount) <= node.state.HighestBid {
		node.mu.Unlock()
		return &proto.BidResponse{Success: false, Message: "Bid too low"}, nil
	}
	stagedBidder := req.BidderId
	stagedAmount := int(req.Amount)
	node.mu.Unlock()

	// replicate
	ok := node.replicateStateSync(stagedAmount, stagedBidder)
	if !ok {
		return &proto.BidResponse{Success: false, Message: "Replication failed"}, nil
	}

	node.mu.Lock()
	node.state.BidsByClient[stagedBidder] = stagedAmount
	node.state.HighestBid = stagedAmount
	node.state.HighestBidder = stagedBidder
	node.mu.Unlock()

	return &proto.BidResponse{Success: true, Message: "Bid accepted"}, nil
}

func (node *AuctionServiceNode) replicateStateSync(amount int, bidder string) bool {
	req := &proto.UpdateStateRequest{
		HighestBid:    int32(amount),
		HighestBidder: bidder,
	}

	var wg sync.WaitGroup
	ackCh := make(chan bool, len(node.peers)+1)
	ackCh <- true // self ack

	node.mu.Lock()
	peersCopy := make(map[string]proto.AuctionServiceClient)
	for k, v := range node.peers {
		peersCopy[k] = v
	}
	node.mu.Unlock()

	for peerID, client := range node.peers {
		wg.Add(1)
		go func(pid string, c proto.AuctionServiceClient) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			resp, err := c.UpdateState(ctx, req)
			if err != nil {
				log.Printf("[%s] replicate error to %s: %v", node.nodeID, pid, err)
				ackCh <- false
				return
			}
			ackCh <- resp.Success
		}(peerID, client)
	}

	wg.Wait()
	close(ackCh)

	positives := 0
	for ok := range ackCh {
		if ok {
			positives++
		}
	}

	total := len(peersCopy) + 1
	quorum := total/2 + 1
	return positives >= quorum
}

func (node *AuctionServiceNode) GetResult(ctx context.Context, req *proto.ResultRequest) (*proto.ResultResponse, error) {
	node.mu.Lock()
	defer node.mu.Unlock()

	return &proto.ResultResponse{
		WinnerId:   node.state.HighestBidder,
		WinningBid: int32(node.state.HighestBid),
	}, nil
}

func (node *AuctionServiceNode) UpdateState(ctx context.Context, req *proto.UpdateStateRequest) (*proto.UpdateStateResponse, error) {
	node.mu.Lock()
	defer node.mu.Unlock()

	node.state.HighestBid = int(req.HighestBid)
	node.state.HighestBidder = req.HighestBidder
	return &proto.UpdateStateResponse{
		Success: true,
		Message: "State updated successfully.",
	}, nil

}

func main() {
	nodeID := flag.String("id", "node1", "Unique node ID")
	port := flag.String("port", ":5001", "Port to listen on")
	flag.Parse()

	allNodes := map[string]string{
		"node1": "localhost:5001",
		"node2": "localhost:5002",
		"node3": "localhost:5003",
	}

	node := NewAuctionServiceNode(*nodeID, allNodes)

	go func() {
		time.Sleep(100 * time.Second)
		node.mu.Lock()
		node.state.AuctionOpen = false
		winner := node.state.HighestBidder
		amount := node.state.HighestBid
		node.mu.Unlock()

		log.Printf("AUCTION CLOSED → %s wins with %d", winner, amount)
	}()

	// start heartbeat processes
	go node.startHeartbeatSender()
	go node.monitorPrimary()

	// Start gRPC server
	lis, err := net.Listen("tcp", *port)
	if err != nil {
		log.Fatalf("[%s] Failed to listen: %v", *nodeID, err)
	}

	grpcServer := grpc.NewServer()
	proto.RegisterAuctionServiceServer(grpcServer, node)

	log.Printf("[%s] Node running on %s (primary=%v)\n", *nodeID, *port, node.isPrimary)
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("[%s] gRPC server failed: %v", *nodeID, err)
	}
}
