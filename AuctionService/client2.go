package main

import (
	"context"
	"log"
	"sync"
	"time"

	proto "github.com/3SemClon/AuctionService/grpc"
	"google.golang.org/grpc"
)

type LamportClock struct {
	Timestamp int64
	mu        sync.Mutex
}

func (lc *LamportClock) Tick() {
	lc.mu.Lock()
	lc.Timestamp++
	lc.mu.Unlock()
}
func (lc *LamportClock) UpdateClock(received int64) {
	lc.mu.Lock()
	if received > lc.Timestamp {
		lc.Timestamp = received
	}
	lc.Timestamp++
	lc.mu.Unlock()
}
func (lc *LamportClock) Get() int64 {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	return lc.Timestamp
}

type Bidder struct {
	Id     string
	Node   string
	Client proto.AuctionServiceClient
	Lc     *LamportClock
}

func NewBidder(id, addr string) (*Bidder, error) {
	conn, err := grpc.Dial(addr, grpc.WithInsecure())
	if err != nil {
		return nil, err
	}
	client := proto.NewAuctionServiceClient(conn)
	return &Bidder{
		Id:     id,
		Node:   addr,
		Client: client,
		Lc:     &LamportClock{Timestamp: 0},
	}, nil
}

func (b *Bidder) Bid(amount int) (*proto.BidResponse, error) {
	b.Lc.Tick()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req := &proto.BidRequest{
		BidderId:  b.Id,
		Amount:    float64(amount),
		Timestamp: b.Lc.Get(),
	}
	resp, err := b.Client.Bid(ctx, req)
	return resp, err
}

func (b *Bidder) GetResult() (*proto.ResultResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return b.Client.GetResult(ctx, &proto.ResultRequest{})
}

func runSequence(b *Bidder, seq []int, delays []time.Duration, wg *sync.WaitGroup) {
	defer wg.Done()
	for i, amount := range seq {
		time.Sleep(delays[i])
		resp, err := b.Bid(amount)
		if err != nil {
			log.Printf("[%s -> %s] Bid %d error: %v", b.Id, b.Node, amount, err)
			continue
		}
		log.Printf("[%s -> %s] Bid %d -> OK=%v msg=%s (L=%d)", b.Id, b.Node, amount, resp.Success, resp.Message, b.Lc.Get())

		// Get result after each bid
		r, err := b.GetResult()
		if err != nil {
			log.Printf("[%s -> %s] GetResult error: %v", b.Id, b.Node, err)
			continue
		}
		log.Printf("[%s -> %s] reported current highest: %s %v", b.Id, b.Node, r.WinnerId, r.WinningBid)
	}
}

func main() {
	//  connect clients to nodes
	assign := map[string]string{
		"Alice":   "localhost:5001",
		"Bob":     "localhost:5002",
		"Charlie": "localhost:5003",
		"Diana":   "localhost:5001",
	}

	// Client hardcoded bids sequences and delays
	sequences := map[string][]int{
		"Alice":   {100, 200},
		"Bob":     {150, 250},
		"Charlie": {120, 300},
		"Diana":   {220},
	}
	delays := map[string][]time.Duration{
		"Alice":   {0 * time.Second, 1 * time.Second},
		"Bob":     {200 * time.Millisecond, 2 * time.Second},
		"Charlie": {500 * time.Millisecond, 3 * time.Second},
		"Diana":   {1500 * time.Millisecond},
	}

	bidders := make(map[string]*Bidder)
	for id, addr := range assign {
		b, err := NewBidder(id, addr)
		if err != nil {
			log.Fatalf("Failed to create bidder %s -> %s: %v", id, addr, err)
		}
		bidders[id] = b
	}

	var wg sync.WaitGroup
	for id, b := range bidders {
		seq := sequences[id]
		ds := delays[id]
		wg.Add(1)
		go runSequence(b, seq, ds, &wg)
	}
	wg.Wait()
	/*
		// retrieve final results from all nodes
		fmt.Println("Final results from each node:")
		for _, addr := range []string{"localhost:5001", "localhost:5002", "localhost:5003"} {
			conn, err := grpc.Dial(addr, grpc.WithInsecure())
			if err != nil {
				log.Printf("Failed to connect to %s: %v", addr, err)
				continue
			}
			client := proto.NewAuctionServiceClient(conn)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			res, err := client.GetResult(ctx, &proto.ResultRequest{})
			cancel()
			conn.Close()
			if err != nil {
				log.Printf("GetResult from %s error: %v", addr, err)
				continue
			}
			fmt.Printf("%s -> Winner: %s bid=%v\n", addr, res.WinnerId, res.WinningBid)
		}*/
}
