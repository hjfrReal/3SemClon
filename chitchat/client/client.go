package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"

	proto "github.com/3SemClon/chitchat/grpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
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
func (lc *LamportClock) GetTime() int64 {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	return lc.Timestamp
}

func logToChatFile(logFile *os.File, line string) {
	logFile.WriteString(line + "\n")
}

func main() {
	// Connect to server
	conn, err := grpc.Dial("localhost:5050", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()
	client := proto.NewChitChatServiceClient(conn)

	// Open log file
	logFile, err := os.OpenFile("chat.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatalf("Failed to open log file: %v", err)
	}
	defer logFile.Close()

	// Prompt for username
	fmt.Print("Enter your name: ")
	reader := bufio.NewReader(os.Stdin)
	name, _ := reader.ReadString('\n')
	name = strings.TrimSpace(name)
	lc := &LamportClock{id: 1, Timestamp: 0}
	// Join chat
	lc.Tick()
	joinResp, err := client.JoinChat(context.Background(), &proto.JoinRequest{Name: name})
	if err != nil {
		log.Fatalf("JoinChat failed: %v", err)
	}
	userID := joinResp.Id
	fmt.Printf("Joined as %s (ID: %s)\n", name, userID)
	logToChatFile(logFile, fmt.Sprintf("[%d] [Client] [JOIN] UserID=%s %s joined the chat", lc.GetTime(), userID, name))

	defer func() {
		lc.Tick()
		if _, err := client.LeaveChat(context.Background(), &proto.LeaveRequest{Id: userID}); err != nil {
			log.Printf("LeaveChat failed: %v", err)
		}
		logToChatFile(logFile, fmt.Sprintf("[%d] [Client] [LEAVE] UserID=%s %s left the chat", lc.GetTime(), userID, name))
	}()

	// Start chat stream
	stream, err := client.Chat(context.Background())
	if err != nil {
		log.Fatalf("Chat stream failed: %v", err)
	}

	// Send initial message with userID (required by server)
	initMsg := &proto.ChatMessage{SenderId: userID}
	if err := stream.Send(initMsg); err != nil {
		log.Fatalf("Failed to send init message: %v", err)
	}

	// Goroutine to receive messages
	go func() {
		for {
			msg, err := stream.Recv()
			if err != nil {
				log.Printf("Receive error: %v", err)
				return
			}
			lc.UpdateClock(msg.Timestamp)
			logLine := fmt.Sprintf("[%d] [Client] [RECEIVE_MESSAGE] UserID=%s Message: %q", lc.GetTime(), msg.SenderId, msg.MessageContent)
			logFile.WriteString(logLine + "\n")

			// Display to user
			fmt.Printf("[%d] %s: %s\n", lc.GetTime(), msg.SenderName, msg.MessageContent)
		}
	}()

	// Main loop: read user input and send messages
	for {
		fmt.Print("> ")
		text, _ := reader.ReadString('\n')
		text = strings.TrimSpace(text)
		if len(text) == 0 {
			continue
		}
		if len(text) > 128 {
			fmt.Println("Message too long (max 128 chars).")
			continue
		}
		lc.Tick()
		chatMsg := &proto.ChatMessage{
			SenderId:       userID,
			SenderName:     name,
			MessageContent: text,
			Timestamp:      int64(lc.GetTime()),
		}
		if err := stream.Send(chatMsg); err != nil {
			log.Printf("Send error: %v", err)
			break
		}
	}

}
