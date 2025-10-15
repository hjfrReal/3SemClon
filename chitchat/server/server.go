package main

import (
	proto "ChitChat/grpc"
	"context"
	"fmt"

	proto "github.com/3SemClon/chitchit/grpc"
	"github.com/3SemClon/chitchit/grpc;proto"
	"google.golang.org/grpc"
)

type user struct {
	id     string
	name   string
	stream grpc.ChitChatService_ChatServer
}

type server struct {
	proto.UnimplementedChitChatServiceServer
	users map[string]*user
}

func (s *server) SendMessage(ctx context.Context, msg *proto.ChatMessage) (*proto.ChatMessage, error) {
	fmt.Printf("[%s]: %s\n", msg.SenderId, msg.MessageContent)
	return &proto.ChatMessage{}, nil
}
