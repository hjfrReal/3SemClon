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
	state     AuctionState
	isPrimary bool
	nodeID    string
	peers     map[string]proto.AuctionServiceClient
	mu        sync.Mutex
}

func NewAuctionServiceNode(id string, isPrimary bool, otherNodes map[string]string) *AuctionServiceNode {

	peers := make(map[string]proto.AuctionServiceClient)

	for peerID, addr := range otherNodes {
		conn, err := grpc.Dial(addr, grpc.WithInsecure())
		if err != nil {
			log.Fatalf("Failed to connect to peer %s at %s: %v", peerID, addr, err)
			panic(err)
		}
		peers[peerID] = proto.NewAuctionServiceClient(conn)
	}

	return &AuctionServiceNode{
		state: AuctionState{
			AuctionOpen:   true,
			StartTime:     time.Now(),
			HighestBid:    0,
			HighestBidder: "",
			BidsByClient:  make(map[string]int),
		},
		isPrimary: isPrimary,
		nodeID:    id,
		peers:     peers,
	}
}

func (node *AuctionServiceNode) Bid(ctx context.Context, req *proto.BidRequest) (*proto.BidResponse, error) {
	node.mu.Lock()
	defer node.mu.Unlock()

	if node.state.AuctionOpen == false {
		return &proto.BidResponse{
			Success: false,
			Message: "Auction is closed.",
		}, nil
	}

	if int(req.Amount) <= node.state.HighestBid {
		return &proto.BidResponse{
			Success: false,
			Message: "Bid is too low."}, nil
	}

	if _, ok := node.state.BidsByClient[req.BidderId]; !ok {
		node.state.BidsByClient[req.BidderId] = 0
	}

	node.state.BidsByClient[req.BidderId] += int(req.Amount)
	node.state.HighestBid = int(req.Amount)
	node.state.HighestBidder = req.BidderId
	if node.isPrimary {
		go node.replicateState()
	}
	return &proto.BidResponse{
		Success: true,
		Message: "Bid accepted.",
	}, nil

}

func (node *AuctionServiceNode) GetResult(ctx context.Context, req *proto.ResultRequest) (*proto.ResultResponse, error) {
	node.mu.Lock()
	defer node.mu.Unlock()

	highestBid := int32(node.state.HighestBid)
	highestBidder := node.state.HighestBidder

	return &proto.ResultResponse{
		WinnerId:   highestBidder,
		WinningBid: highestBid,
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

func (node *AuctionServiceNode) replicateState() {
	node.mu.Lock()
	req := &proto.UpdateStateRequest{
		HighestBid:    int32(node.state.HighestBid),
		HighestBidder: node.state.HighestBidder,
	}
	node.mu.Unlock()

	ctx := context.Background()

	for _, client := range node.peers {
		_, err := client.UpdateState(ctx, req)
		if err != nil {
			// not fatal — one node can fail
			println("Replication error:", err.Error())
		}
	}

}

func main() {
	// To run main see readme.md file.
	// Flags to configure the node
	nodeID := flag.String("id", "node1", "Unique node ID")
	port := flag.String("port", ":5001", "Port to listen on")
	isPrimary := flag.Bool("primary", false, "Is this node primary?")
	flag.Parse()

	allNodes := map[string]string{
		"node1": "localhost:5001",
		"node2": "localhost:5002",
		"node3": "localhost:5003",
	}
	// Remove self from peers
	delete(allNodes, *nodeID)

	node := NewAuctionServiceNode(*nodeID, *isPrimary, allNodes)

	go func() {
		time.Sleep(100 * time.Second)
		node.mu.Lock()
		node.state.AuctionOpen = false
		node.mu.Unlock()
		log.Printf("[%s] Auction closed\n", *nodeID)
	}()

	// Start gRPC server
	lis, err := net.Listen("tcp", *port)
	if err != nil {
		log.Fatalf("[%s] Failed to listen: %v", *nodeID, err)
	}

	grpcServer := grpc.NewServer()
	proto.RegisterAuctionServiceServer(grpcServer, node)

	log.Printf("[%s] Node running on %s (primary=%v)\n", *nodeID, *port, *isPrimary)
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("[%s] gRPC server failed: %v", *nodeID, err)
	}
}
