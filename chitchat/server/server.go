package main

import (
	"context"

	proto "github.com/3SemClon/chitchat/grpc"
	"github.com/google/uuid"
)

type user struct {
	id     string
	name   string
	stream proto.ChitChatService_ChatServer
}

type server struct {
	proto.UnimplementedChitChatServiceServer
	users map[string]*user
}

func (s *server) JoinChat(ctx context.Context, req *proto.JoinRequest) (*proto.JoinResponse, error) {
	userID := uuid.New().String()

	newUser := &user{
		id:   userID,
		name: req.Name,
	}

	s.users[userID] = newUser
	return &proto.JoinResponse{Id: userID}, nil
}

func (s *server) LeaveChat(ctx context.Context, req *proto.LeaveRequest) (*proto.LeaveResponse, error) {
	userID := req.Id

	if _, ok := s.users[userID]; ok {
		delete(s.users, userID)
		return &proto.LeaveResponse{Success: true}, nil

	}
	return &proto.LeaveResponse{Success: false}, nil
}
