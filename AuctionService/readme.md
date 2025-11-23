1. go to AuctionNodes folder.
2. run the commands in this order to activate the 3 auction processes/nodes in different terminals:
    go run node.go --id=node1 --port=:5001 --primary=true
    go run node.go --id=node2 --port=:5002 --primary=false
    go run node.go --id=node3 --port=:5003 --primary=false
3. Make another terminal and navigate to client folder. 
