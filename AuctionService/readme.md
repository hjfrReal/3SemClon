1. go to AuctionNodes folder.
2. prepare the commands to activate the 3 auction processes/nodes in different terminals
Do remember that there is time on the auction and the moment you run the nodes the timer starts. (It lasts 180s)
    go run auctionNode.go --id=node3 --port=:5003
    go run auctionNode.go --id=node2 --port=:5002
    go run auctionNode.go --id=node1 --port=:5001
3. Make another terminal and navigate to AuctionService.
4. After you have startet the nodes you can write here: 
go run client.go
5. Now the clients will bid and you will see it in the terminal.
6. While the client is running you can close a terminal with a node to see if the program still runs. (ctrl + c).

