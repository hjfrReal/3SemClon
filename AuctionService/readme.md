1. go to AuctionNodes folder.
2. run the commands in this order to activate the 3 auction processes/nodes in different terminals:
    go run auctionNode.go --id=node3 --port=:5003
    go run auctionNode.go --id=node2 --port=:5002
    go run auctionNode.go --id=node1 --port=:5001
3. Make another terminal and navigate to client folder. 
