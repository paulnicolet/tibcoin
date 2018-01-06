package gossipernode

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"math/rand"
	"net"
	"time"

	"github.com/dedis/protobuf"
)

const INVENTORY_SIZE = 10
const REQUEST_INVENTORY_WAIT = 15
const REQUEST_BLOCK_WAIT = 5
const MAX_REQUEST = 3
const DIFF_TO_DELETE_ORPHAN = 10

func (gossiper *Gossiper) getInventoryRoutine() {

	// every predefined time, you request to all your neighboor their top block to
	// see if you are behind
	for range time.NewTicker(time.Second * REQUEST_INVENTORY_WAIT).C {

		gossiper.topBlockMutex.Lock()
		currentTopBlock := gossiper.topBlock
		gossiper.topBlockMutex.Unlock()

		packet := &GossipPacket{
			BlockRequest: &BlockRequest{
				Origin:     gossiper.name,
				BlockHash:  currentTopBlock,
				WaitingInv: true,
			},
		}

		buffer, err := protobuf.Encode(packet)
		if err != nil {
			gossiper.errLogger.Printf("Error in getInventory: %v", err)
		}

		// request the neighboor
		for _, peer := range gossiper.peers {
			_, err = gossiper.gossipConn.WriteToUDP(buffer, peer.addr)
			if err != nil {
				gossiper.errLogger.Printf("Error in getInventory: %v", err)
			}
		}

		gossiper.errLogger.Printf("[bc_rout]: request inventory from neighboor(s)")
	}
}

func addrInList(l []*net.UDPAddr, e *net.UDPAddr) bool {
	for _, addr := range l {

		if addr.String() == e.String() {
			return true
		}
	}
	return false
}

func (gossiper *Gossiper) requestBlocksFromInventory(inventory [][32]byte, from *net.UDPAddr) {

	gossiper.errLogger.Printf("[bc_rout]: requesting block(s) from inventory of %s", from.String())

	for _, hash := range inventory {

		//check if I already have the block
		gossiper.blocksMutex.Lock()
		_, containsBlock := gossiper.blocks[hash]
		gossiper.blocksMutex.Unlock()

		// if it is not the case, time to work for this block
		if !containsBlock {

			// check if we have already an ongoing request for this block
			gossiper.blockInRequestMutex.Lock()
			l, containsOngoingRequest := gossiper.blockInRequest[hash]

			// if yes, add the peer as a possible guy to request (if not already present)
			if containsOngoingRequest {

				// check if it already in the list
				if !addrInList(l, from) {
					gossiper.blockInRequest[hash] = append(l, from)
				}

				// if no, time to create a requester
			} else {

				// second check to avoid race condition
				gossiper.blocksMutex.Lock()
				_, containsBlock := gossiper.blocks[hash]
				gossiper.blocksMutex.Unlock()

				// after second check, create the list of possible peer to request
				if !containsBlock {
					tmp := make([]*net.UDPAddr, 1)
					tmp[0] = from
					gossiper.blockInRequest[hash] = tmp
				}
			}
			gossiper.blockInRequestMutex.Unlock()

			// we create the requester if needed
			if !containsOngoingRequest {
				go gossiper.requestBlock(hash)

				gossiper.errLogger.Printf("[bc_rout]: created requester for block %x", hash[:])
			}
		}
	}
}

func (gossiper *Gossiper) requestBlock(blockHash [32]byte) {

	// the block to request
	packet := &GossipPacket{
		BlockRequest: &BlockRequest{
			Origin:     gossiper.name,
			BlockHash:  blockHash,
			WaitingInv: false,
		},
	}

	buffer, err := protobuf.Encode(packet)
	if err != nil {
		gossiper.errLogger.Printf("Error in getBlock: %v", err)
	}

	// only one guy requested at a time to avoid DDOS behavior
	var currentRequestedPeer *net.UDPAddr
	currentRequestedPeer = nil
	for range time.NewTicker(time.Second * REQUEST_BLOCK_WAIT).C {

		// decreased current if not nil
		if currentRequestedPeer != nil {

			gossiper.peerNumRequestMutex.Lock()
			gossiper.peerNumRequest[currentRequestedPeer.String()]--
			gossiper.peerNumRequestMutex.Unlock()

			currentRequestedPeer = nil
		}

		// check if we have received the block while we were waiting
		gossiper.blocksMutex.Lock()
		_, containsBlock := gossiper.blocks[blockHash]
		gossiper.blocksMutex.Unlock()

		// if yes, we destroy ourselves
		if containsBlock {

			gossiper.blockInRequestMutex.Lock()
			delete(gossiper.blockInRequest, blockHash)
			gossiper.blockInRequestMutex.Unlock()

			break

			// if no, time to request again randomly among possible peer
		} else {

			gossiper.blockInRequestMutex.Lock()
			gossiper.peerNumRequestMutex.Lock()
			l := gossiper.blockInRequest[blockHash]

			randomIdx := rand.Intn(len(l))
			currentIdx := randomIdx

			for currentRequestedPeer == nil {

				currentRequestedPeer = l[currentIdx%len(l)]

				if gossiper.peerNumRequest[currentRequestedPeer.String()] >= MAX_REQUEST {
					currentRequestedPeer = nil
				}
				currentIdx++

				if currentIdx%len(l) == randomIdx {
					break
				}
			}
			gossiper.peerNumRequestMutex.Unlock()
			gossiper.blockInRequestMutex.Unlock()

			// if any peer is available, send the request
			if currentRequestedPeer != nil {
				gossiper.peerNumRequestMutex.Lock()
				gossiper.peerNumRequest[currentRequestedPeer.String()]++
				gossiper.peerNumRequestMutex.Unlock()

				_, err = gossiper.gossipConn.WriteToUDP(buffer, currentRequestedPeer)
				if err != nil {
					gossiper.errLogger.Printf("Error in getBlock: %v", err)
				}
			}
		}

		requestedName := "_nobody_"
		if currentRequestedPeer != nil {
			requestedName = currentRequestedPeer.String()
		}
		gossiper.errLogger.Printf("[bc_rout]: requesting block %x to %s", blockHash[:], requestedName)
	}
}

func (gossiper *Gossiper) blockRequestRoutine(channel <-chan *GossiperPacketSender) {
	for {
		packet := <-channel
		err := gossiper.handleBlockRequest(packet)
		if err != nil {
			gossiper.errLogger.Printf("Error processing a block request: %v", err)
		}
	}
}

func (gossiper *Gossiper) handleBlockRequest(blockRequestPacket *GossiperPacketSender) error {

	request := blockRequestPacket.packet.BlockRequest
	from := blockRequestPacket.from

	// if it is an inventory
	if request.WaitingInv {

		gossiper.topBlockMutex.Lock()
		currentTopBlockHash := gossiper.topBlock
		gossiper.topBlockMutex.Unlock()

		// check that we have different top block
		if !bytes.Equal(currentTopBlockHash[:], request.BlockHash[:]) {

			// the fifo queue that we will send
			blocksHashChan := make(chan [32]byte, INVENTORY_SIZE)
			blocksHashChan <- currentTopBlockHash
			counter := 1

			gossiper.blocksMutex.Lock()
			currentBlockHash := gossiper.blocks[currentTopBlockHash]
			gossiper.blocksMutex.Unlock()

			// loop until we find the requested block, or we reach the genesis block
			for !bytes.Equal(NilHash[:], currentBlockHash.PrevHash[:]) && !bytes.Equal(currentBlockHash.PrevHash[:], request.BlockHash[:]) {

				// if fifo full, remove the first entered one
				if counter == INVENTORY_SIZE {
					counter--
					<-blocksHashChan
				}

				// add the new element to the queue
				blocksHashChan <- currentBlockHash.PrevHash
				counter++

				// go the to the next block
				gossiper.blocksMutex.Lock()
				currentBlockHash = gossiper.blocks[currentBlockHash.PrevHash]
				gossiper.blocksMutex.Unlock()

			}

			// a channel isn't a slice, thus we need to transform it
			blocksHash := make([][32]byte, counter)
			concatHash := make([]byte, 0)
			for i := 0; i < counter; i++ {
				blocksHash[i] = <-blocksHashChan
				concatHash = append(concatHash, blocksHash[i][:]...)
			}

			gossiper.errLogger.Printf("[bc_rout]: inventory requested, sending to %s, size send = %d", from.String(), counter)

			// we are ready to send the inventory
			packet := &GossipPacket{
				BlockReply: &BlockReply{
					Origin:     gossiper.name,
					Hash:       sha256.Sum256(concatHash), // don't forget to verify the slice
					BlocksHash: blocksHash,
				},
			}

			buffer, err := protobuf.Encode(packet)
			if err != nil {
				return err
			}

			_, err = gossiper.gossipConn.WriteToUDP(buffer, from)
			return err

		} else {
			// same top block, nothing to send
			return nil
		}

		// else it's a unique block
	} else {

		// check first that we have the requested block
		gossiper.blocksMutex.Lock()
		block, containsBlock := gossiper.blocks[request.BlockHash]
		gossiper.blocksMutex.Unlock()

		// if yes
		if containsBlock {

			gossiper.errLogger.Printf("[bc_rout]: block requested, sending to %x, from %s", request.BlockHash[:], from.String())

			return gossiper.sendBlockTo(block, from)

			// error if we don't have the requested block,
			// a peer should know who has what
		} else {
			return errors.New("Unknown requested block")
		}
	}
}

func (gossiper *Gossiper) sendBlockTo(block *Block, to *net.UDPAddr) error {
	serBlock, err := block.toSerializable()
	if err != nil {
		return err
	}
	packet := &GossipPacket{
		BlockReply: &BlockReply{
			Origin: gossiper.name,
			Hash:   block.hash(),
			Block:  serBlock,
		},
	}

	buffer, err := protobuf.Encode(packet)
	if err != nil {
		gossiper.errLogger.Println(err)
		return err
	}

	_, err = gossiper.gossipConn.WriteToUDP(buffer, to)

	//tmp := block.hash()
	//gossiper.errLogger.Printf("[bc_rout]: block %x sent to %s", tmp[:], to.String())

	return err
}

func (gossiper *Gossiper) blockReplyRoutine(channel <-chan *GossiperPacketSender) {
	for {
		packet := <-channel
		err := gossiper.handleBlockReply(packet)
		if err != nil {
			gossiper.errLogger.Printf("Error processing a block reply: %v", err)
		}
	}
}

func (gossiper *Gossiper) handleBlockReply(blockReplyPacket *GossiperPacketSender) error {

	reply := blockReplyPacket.packet.BlockReply
	from := blockReplyPacket.from

	gossiper.errLogger.Printf("Received new block from %s", from.String())

	// check if we got a block or inventory
	// first the block
	if reply.Block != nil {

		// Check that the block wasn't corrupted by UDP
		block, err := reply.Block.toNormal()
		if err != nil {
			return err
		}

		if gossiper.VerifyBlock(block, reply.Hash) {
			gossiper.errLogger.Printf("Block valid: %x", reply.Hash[:])
		} else {
			gossiper.errLogger.Printf("Block invalid: %x", reply.Hash[:])
		}

		return nil
	} else {
		// we got an inventory

		// first check not corrupted by UDP
		concatHash := make([]byte, 0)
		for i := 0; i < len(reply.BlocksHash); i++ {
			concatHash = append(concatHash, reply.BlocksHash[i][:]...)
		}

		tmp := sha256.Sum256(concatHash)
		corrupted := !bytes.Equal(reply.Hash[:], tmp[:])

		// if not corrupted request for all elements in inventory
		if !corrupted {
			go gossiper.requestBlocksFromInventory(reply.BlocksHash, from)

			gossiper.errLogger.Printf("[bc_rout]: valid inventory in reply from %s", from.String())
			return nil
		} else {
			return errors.New("Inventory corrupted")
		}
	}
}
