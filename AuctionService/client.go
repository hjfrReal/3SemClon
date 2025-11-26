/*package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	//"math/rand/v2" -> rand.IntN(100)

	proto "github.com/3SemClon/AuctionService/grpc"
	"google.golang.org/grpc"
)

func main() {

	assign := map[string]string{
        "Alice":   "localhost:5001",
        "Bob":     "localhost:5002",
        "Charlie": "localhost:5003",
        "Diana":   "localhost:5001", // two bidders on node1 to test multi-client behaviour
    }
	bidder := flag.String("bidder", "ClientA", "Bidder ID")
	amount := flag.Int("amount", 100, "Bid amount")

	flag.Parse()

	conn, err := grpc.Dial("localhost:5001", grpc.WithInsecure())
	if err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	client := proto.NewAuctionServiceClient(conn)

	res, err := client.Bid(context.Background(), &proto.BidRequest{
		BidderId:  *bidder,
		Amount:    float64(*amount),
		Timestamp: 1,
	})
	if err != nil {
		log.Fatalf("Bid error: %v", err)
	}

	fmt.Println("Bid response:", res.Message)

	result, err := client.GetResult(context.Background(), &proto.ResultRequest{})
	if err != nil {
		log.Fatalf("Result error: %v", err)
	}

	fmt.Println("Current highest bidder:", result.WinnerId)
	fmt.Println("Current highest bid:", result.WinningBid)
}
*/