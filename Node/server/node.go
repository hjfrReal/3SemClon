using System.Xml.Serialization;

package main

import (

    proto "github.com/3SemClon/Node/grpc"  
    "google.golang.org/grpc"



)

type Node struct {
    id string
    timestamp int64
    otherNodes []proto.NodeServiceClient

    replyChannel chan string
}