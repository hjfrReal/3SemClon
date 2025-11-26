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
	primaryID string
	mu        sync.Mutex
}

func NewAuctionServiceNode(id string, isPrimary bool, otherNodes map[string]string, primaryID string) *AuctionServiceNode {

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
		primaryID: primaryID,
	}
}

func (node *AuctionServiceNode) Bid(ctx context.Context, req *proto.BidRequest) (*proto.BidResponse, error) {
    // if we're not primary, forward the bid to the primary
    if !node.isPrimary {
        // If primary is unknown, fail fast
        if node.primaryID == "" {
            return &proto.BidResponse{Success: false, Message: "Primary unknown"}, nil
        }
        primaryClient, ok := node.peers[node.primaryID]
        if !ok {
            return &proto.BidResponse{Success: false, Message: "Cannot reach primary"}, nil
        }
        // forward (preserve client's context/timeout)
        return primaryClient.Bid(ctx, req)
    }

    // Primary: validate and replicate before committing (see step 3)
    node.mu.Lock()
    if node.state.AuctionOpen == false {
        node.mu.Unlock()
        return &proto.BidResponse{Success: false, Message: "Auction is closed."}, nil
    }
    if int(req.Amount) <= node.state.HighestBid {
        node.mu.Unlock()
        return &proto.BidResponse{Success: false, Message: "Bid is too low."}, nil
    }

    // Tentatively update local staged state — but don't commit until replication succeeds
    stagedBidder := req.BidderId
    stagedAmount := int(req.Amount)
    node.mu.Unlock()

    // Replicate (synchronous): ensure backups apply
    ok := node.replicateStateSync(stagedAmount, stagedBidder)
    if !ok {
        return &proto.BidResponse{Success: false, Message: "Replication failed"}, nil
    }

    // Commit locally after replication
    node.mu.Lock()
    if _, present := node.state.BidsByClient[stagedBidder]; !present {
        node.state.BidsByClient[stagedBidder] = 0
    }
    node.state.BidsByClient[stagedBidder] += stagedAmount
    node.state.HighestBid = stagedAmount
    node.state.HighestBidder = stagedBidder
    node.mu.Unlock()

    return &proto.BidResponse{Success: true, Message: "Bid accepted."}, nil
}

func (node *AuctionServiceNode) GetResult(ctx context.Context, req *proto.ResultRequest) (*proto.ResultResponse, error) {
    if !node.isPrimary {
        if primaryClient, ok := node.peers[node.primaryID]; ok {
            return primaryClient.GetResult(ctx, req)
        }
        // fallback to local state if primary unreachable
    }
    // existing local state return
    node.mu.Lock()
    defer node.mu.Unlock()
    return &proto.ResultResponse{WinnerId: node.state.HighestBidder, WinningBid: int32(node.state.HighestBid)}, nil
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

func (node *AuctionServiceNode) replicateStateSync(amount int, bidder string) bool {
    // build request and include a sequence or lamport if you use them
    req := &proto.UpdateStateRequest{
        HighestBid:    int32(amount),
        HighestBidder: bidder,
    }

    var wg sync.WaitGroup
    ackCh := make(chan bool, len(node.peers))
    for peerID, client := range node.peers {
        // count self as immediate ack
        if peerID == node.nodeID {
            ackCh <- true
            continue
        }
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
    total := len(node.peers)
    quorum := total/2 + 1
    return positives >= quorum
}

func main() {
    nodeID := flag.String("id", "node1", "Unique node ID")
    port := flag.String("port", ":5001", "Port to listen on")
    isPrimary := flag.Bool("primary", false, "Is this node primary?")
    primaryID := flag.String("primary-id", "node1", "Current primary node ID")
    flag.Parse()

    allNodes := map[string]string{
		"node1": "localhost:5001",
		"node2": "localhost:5002",
		"node3": "localhost:5003",
	}

	// Remove self from peers
    delete(allNodes, *nodeID)

    node := NewAuctionServiceNode(*nodeID, *isPrimary, allNodes, *primaryID)

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
