// Tcp handshake
package main

import (
	"fmt"
	"time"
)

//channel1 := make(chan string)

func main() {

	channel1 := make(chan int32)
	go Client(channel1)
	go Server(channel1)

	time.Sleep(5 * time.Second)

}

func Client(channel1 chan int32) {
	var SYN = int32(2)
	channel1 <- SYN

	NewSYN := <-channel1
	ACK := <-channel1

	if NewSYN == SYN+1 {
		ACK = ACK + 1
		channel1 <- ACK
		channel1 <- NewSYN
	}
}

func Server(channel1 chan int32) {
	var SYN = int32(12)

	ACK := <-channel1
	ACK = ACK + 1
	channel1 <- ACK
	channel1 <- SYN

	NewSyn := <-channel1
	check := <-channel1
	if NewSyn == SYN+1 && check == ACK {
		fmt.Println("Connection Established")
	}
}

// 2 threads. 1 Channel.
