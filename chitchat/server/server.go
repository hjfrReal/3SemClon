package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"sync"

	proto "github.com/3SemClon/chitchat/grpc"
	"github.com/google/uuid"
	"google.golang.org/grpc"
)

type LamportClock struct {
	id        int64
	Timestamp int64
	mu        sync.Mutex
}

type user struct {
	id     string
	name   string
	stream proto.ChitChatService_ChatServer
}

type server struct {
	proto.UnimplementedChitChatServiceServer
	users map[string]*user
	clock *LamportClock
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

func (s *server) JoinChat(ctx context.Context, req *proto.JoinRequest) (*proto.JoinResponse, error) {
	s.clock.Tick()
	userID := uuid.New().String()

	newUser := &user{
		id:   userID,
		name: req.Name,
	}

	s.users[userID] = newUser
	for _, user := range s.users {
		if user.id != userID && user.stream != nil {
			user.stream.Send(&proto.ChatMessage{
				SenderId:       "System",
				MessageContent: req.Name + " has joined the chat.",
			})
		}
	}
	return &proto.JoinResponse{Id: userID}, nil

}

func (s *server) LeaveChat(ctx context.Context, req *proto.LeaveRequest) (*proto.LeaveResponse, error) {
	s.clock.Tick()
	userID := req.Id

	user, exists := s.users[userID]
	if !exists {
		return &proto.LeaveResponse{Success: false}, nil
	}

	for _, otherUser := range s.users {
		if otherUser.stream != nil {
			otherUser.stream.Send(&proto.ChatMessage{
				SenderId:       "System",
				MessageContent: user.name + " has left the chat.",
			})
		}
	}

	delete(s.users, userID)
	return &proto.LeaveResponse{Success: true}, nil
}

func (s *server) Chat(stream proto.ChitChatService_ChatServer) error {
	initMessage, err := stream.Recv()
	if err != nil {
		return err
	}

	userID := initMessage.SenderId
	user, exists := s.users[userID]
	if !exists {
		return fmt.Errorf("user not found")
	}

	user.stream = stream

	defer func() {
		for _, otherUser := range s.users {
			if otherUser.id != userID && otherUser.stream != nil {
				otherUser.stream.Send(&proto.ChatMessage{
					SenderId:       "System",
					MessageContent: user.name + " has left the chat.",
				})
			}
		}
		delete(s.users, userID)
	}()

	for {
		msg, err := stream.Recv()
		if err != nil {
			return err
		}
		for _, user := range s.users {
			if user.id != userID && user.stream != nil {
				user.stream.Send(msg)
			}
		}
	}
}

func GetActiveUsers(s *server) []string {
	activeUsers := []string{}
	for _, user := range s.users {
		activeUsers = append(activeUsers, user.name)
	}
	return activeUsers
}

func main() {
	server := &server{users: make(map[string]*user), clock: &LamportClock{Timestamp: 0}}

	server.start_server()
}

func (s *server) start_server() {
	grpcServer := grpc.NewServer()
	listener, err := net.Listen("tcp", ":5050")
	if err != nil {
		log.Fatalf("Did not work")
	}

	proto.RegisterChitChatServiceServer(grpcServer, s)

	err = grpcServer.Serve(listener)

	if err != nil {
		log.Fatalf("Did not work")
	}
}
