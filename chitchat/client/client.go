package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	proto "github.com/3SemClon/chitchat/grpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var logicalTimestamp int64 = 0

func main() {
	// Connect to server
	conn, err := grpc.NewClient("localhost:5050", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()
	client := proto.NewChitChatServiceClient(conn)

	// Prompt for username
	fmt.Print("Enter your name: ")
	reader := bufio.NewReader(os.Stdin)
	name, _ := reader.ReadString('\n')
	name = strings.TrimSpace(name)

	// Join chat
	joinResp, err := client.JoinChat(context.Background(), &proto.JoinRequest{Name: name})
	if err != nil {
		log.Fatalf("JoinChat failed: %v", err)
	}
	userID := joinResp.Id
	fmt.Printf("Joined as %s (ID: %s)\n", name, userID)

	// Open log file
	logFile, err := os.OpenFile("chat.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatalf("Failed to open log file: %v", err)
	}
	defer logFile.Close()

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
			display := fmt.Sprintf("[%d] %s: %s", msg.Timestamp, msg.SenderName, msg.MessageContent)
			fmt.Println(display)
			logFile.WriteString(display + "\n")
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
		chatMsg := &proto.ChatMessage{
			SenderId:       userID,
			SenderName:     name,
			MessageContent: text,
			Timestamp:      logicalTimestamp,
		}
		if err := stream.Send(chatMsg); err != nil {
			log.Printf("Send error: %v", err)
			break
		}
		logicalTimestamp = max(logicalTimestamp, chatMsg.Timestamp) + 1
	}

	// Leave chat on exit
	finally {
	_, err = client.LeaveChat(context.Background(), &proto.LeaveRequest{Id: userID})
	if err != nil {
		log.Printf("LeaveChat failed: %v", err)
	}
}
}
