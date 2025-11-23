package main

import (
	"context"
	"sync"
	"time"

	proto "github.com/3SemClon/AuctionService/grpc"
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
	state     AuctionState
	isPrimary bool
	nodeID    string
	peers     []string
	mu        sync.Mutex
}

type server struct {
	proto.UnimplementedServiceServer
	node  map[string]*AuctionServiceNode
	clock *LamportClock
	id    string
}

func NewAuctionServiceNode(id, address string, otherNodes map[string]string) *AuctionServiceNode {
	// Initialize node

}

func (node *AuctionServiceNode) bidHandler(ctx context.Context, req *proto.BidRequest) (*proto.BidResponse, error) {
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

	if _, ok := node.state.BidsByClient[req.ClientId]; !ok {
		node.state.BidsByClient[req.ClientId] = 0
	}

	node.state.BidsByClient[req.ClientId] += int(req.Amount)
	node.state.HighestBid = int(req.Amount)
	node.state.HighestBidder = req.ClientId
	return &proto.BidResponse{
		Success: true,
		Message: "Bid accepted.",
	}, nil

}

func (node *AuctionServiceNode) result(ctx context.Context, req *proto.ResultRequest) (*proto.ResultResponse, error) {
	node.mu.Lock()
	defer node.mu.Unlock()

	closed := !node.state.AuctionOpen
	highestBid := int32(node.state.HighestBid)
	highestBidder := node.state.HighestBidder

	return &proto.ResultResponse{
		WinnerId:   highestBid,
		WinningBid: highestBidder,
	}, nil
}

func main() {
	// Implementation of main function to initialize and run the auction service node
}
