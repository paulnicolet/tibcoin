package gossipernode

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"net"
	"time"

	"github.com/dedis/protobuf"
)

const INVENTORY_SIZE = 10
const REQUEST_INVENTORY_WAIT = 15
const REQUEST_BLOCK_WAIT = 5
const DIFF_TO_DELETE_ORPHAN = 10

func (gossiper *Gossiper) getInventory() {

	// every predefined time, you request to all your neighboor their top block to
	// see if you are behind
	for range time.NewTicker(time.Second * REQUEST_INVENTORY_WAIT).C {

		packet := &GossipPacket{
			BlockRequest: &BlockRequest{
				Origin:     gossiper.name,
				BlockHash:  gossiper.topBlock,
				WaitingInv: true,
			},
		}

		buffer, err := protobuf.Encode(&packet)
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

func hashInList(l [][32]byte, e [32]byte) bool {
	for _, hash := range l {

		if bytes.Equal(hash[:], e[:]) {
			return true
		}
	}
	return false
}

func (gossiper *Gossiper) requestBlocksFromInventory(inventory [][32]byte, from *net.UDPAddr) {

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

	buffer, err := protobuf.Encode(&packet)
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
			gossiper.peerNumRequest[currentRequestedPeer]--
			gossiper.peerNumRequestMutex.Unlock()

			currentRequestedPeer = nil
		}

		// check if we have received the block while we were waiting
		gossiper.blocksMutex.Lock()
		_, containsBlock := gossiper.blocks[blockHash]
		gossiper.blocksMutex.Unlock()

		// if yes, we destroy ourselves
		if containsBlock {

			delete(gossiper.blockInRequest, blockHash)

			break

			// if no, time to request again randomly among possible peer
		} else {

			gossiper.blockInRequestMutex.Lock()
			l := gossiper.blockInRequest[blockHash]

			currentRequestedPeer = l[0] // TODO optim: choose randomly
			gossiper.blockInRequestMutex.Unlock()

			// if any peer is available, send the request
			if currentRequestedPeer != nil {
				gossiper.peerNumRequestMutex.Lock()
				gossiper.peerNumRequest[currentRequestedPeer]++
				gossiper.peerNumRequestMutex.Unlock()

				_, err = gossiper.gossipConn.WriteToUDP(buffer, currentRequestedPeer)
				if err != nil {
					gossiper.errLogger.Printf("Error in getBlock: %v", err)
				}
			}
		}
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

	// if it is an inventory
	if request.WaitingInv {

		// first check that we will be able to answer to the requester
		to, containsOrigin := gossiper.peers[request.Origin]

		// if yes
		if containsOrigin {

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
				nilHash := BytesToHash(make([]byte, 32))
				for !bytes.Equal(nilHash[:], currentBlockHash.PrevHash[:]) && !bytes.Equal(currentBlockHash.PrevHash[:], request.BlockHash[:]) {

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

				// we are ready to send the inventory
				packet := &GossipPacket{
					BlockReply: &BlockReply{
						Origin:     gossiper.name,
						Hash:       sha256.Sum256(concatHash), // don't forget to verify the slice
						BlocksHash: blocksHash,
					},
				}

				buffer, err := protobuf.Encode(&packet)
				if err != nil {
					return err
				}

				_, err = gossiper.gossipConn.WriteToUDP(buffer, to.addr)
				return err

			} else {
				// same top block, nothing to send
				return nil
			}

		} else {
			return errors.New("Unknown origin")
		}

		// else it's a unique block
	} else {

		// check first that we have the requested block
		gossiper.blocksMutex.Lock()
		block, containsBlock := gossiper.blocks[request.BlockHash]
		gossiper.blocksMutex.Unlock()

		// if yes
		if containsBlock {

			// then check if we know the origin
			to, containsOrigin := gossiper.peers[request.Origin]
			if containsOrigin {
				return gossiper.sendBlockTo(block, to.addr)
			} else {
				return errors.New("Unknown origin")
			}

			// error if we don't have the requested block,
			// a peer should know who has what
		} else {
			return errors.New("Unknown requested block")
		}
	}
}

func (gossiper *Gossiper) sendBlockTo(block *Block, to *net.UDPAddr) error {
	packet := &GossipPacket{
		BlockReply: &BlockReply{
			Origin: gossiper.name,
			Hash:   block.hash(),
			Block:  block,
		},
	}

	buffer, err := protobuf.Encode(&packet)
	if err != nil {
		return err
	}

	_, err = gossiper.gossipConn.WriteToUDP(buffer, to)
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

	if reply.Block != nil {

		tmp := reply.Block.hash()
		corrupted := !bytes.Equal(tmp[:], reply.Hash[:])

		if !corrupted {
			gossiper.blocksMutex.Lock()
			_, ok := gossiper.blocks[reply.Hash]
			gossiper.blocksMutex.Unlock()

			if !ok {

				// TODO verfiy block
				verify := true
				if verify {

					// TODO check if we were expecting this block, if no => forward
					// TODO otpim : check for finer grain lock
					gossiper.blocksMutex.Lock()
					gossiper.forksMutex.Lock()
					gossiper.blockOrphanPoolMutex.Lock()

					gossiper.blocks[reply.Hash] = reply.Block

					// see if we can add this block to one of the top fork
					found := false
					var toRemove [32]byte
					for hashTopFork, _ := range gossiper.forks {
						if bytes.Equal(reply.Block.PrevHash[:], hashTopFork[:]) {
							found = true
							toRemove = hashTopFork // need to remove this one from fork
						}
					}

					if found {

						// add the new height
						reply.Block.Height = gossiper.blocks[toRemove].Height + 1

						// replace fork top
						gossiper.forks[reply.Hash] = true
						// but remove it from top only if its not the genesis (genesis always one of the top fork)
						tmp := GenesisBlock.hash()
						if !bytes.Equal(toRemove[:], tmp[:]) {
							delete(gossiper.forks, toRemove)
						}

						// fixed point needed to move on we had the possibility to put the new block at the top of the updated fork
						currentTopForkHash := reply.Hash
						currentTopForkBlock := reply.Block
						done := false
						for !done {

							// we assume we won't need a new pass at the beginning of each pass
							done = true

							// we pass over each orphan
							for hash, prevHash := range gossiper.blockOrphanPool {

								if bytes.Equal(prevHash[:], currentTopForkHash[:]) {

									newBlockTop := gossiper.blocks[hash]

									// add the new height
									newBlockTop.Height = currentTopForkBlock.Height + 1
									currentTopForkBlock = newBlockTop

									// replace fork top
									gossiper.forks[hash] = true
									delete(gossiper.forks, currentTopForkHash)
									currentTopForkHash = hash

									done = false // found a new top, need to repeat the process again
								}
							}
						}

						// now that the new top fork is at its max, need to compare with current top
						gossiper.topBlockMutex.Lock()
						currentTopBlock := gossiper.blocks[gossiper.topBlock]
						if currentTopBlock.Height < currentTopForkBlock.Height {
							gossiper.topBlock = currentTopForkHash
							// TODO warn Valentin

							// TODO optim : new top means that we may remove some orphan
						}
						gossiper.topBlockMutex.Unlock()

					} else {

						// can't find a place to put it, it's an orphan
						gossiper.blockOrphanPool[reply.Hash] = reply.Block.PrevHash
					}

					gossiper.blockOrphanPoolMutex.Unlock()
					gossiper.forksMutex.Unlock()
					gossiper.blocksMutex.Unlock()

					return nil
				}
			}

			return nil
		} else {
			return errors.New("Block corrupted")
		}
	} else {

		concatHash := make([]byte, 0)
		for i := 0; i < len(reply.BlocksHash); i++ {
			concatHash = append(concatHash, reply.BlocksHash[i][:]...)
		}

		tmp := sha256.Sum256(concatHash)
		corrupted := !bytes.Equal(reply.Hash[:], tmp[:])

		if !corrupted {
			go gossiper.requestBlocksFromInventory(reply.BlocksHash, from)
			return nil
		} else {
			return errors.New("Inventory corrupted")
		}
	}
}
