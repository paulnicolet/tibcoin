package gossipernode

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"time"

	"github.com/dedis/protobuf"
)

const INVENTORY_SIZE = 10
const REQUEST_INVENTORY_WAIT = 15
const REQUEST_BLOCK_WAIT = 5
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

	gossiper.errLogger.Printf("[bc_rout]: requested block(s) from inventory of %s", from.String())

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
			gossiper.peerNumRequestMutex.Lock()
			l := gossiper.blockInRequest[blockHash]

			randomIdx := rand.Intn(len(l))
			currentIdx := randomIdx

			var currentRequestedPeer *net.UDPAddr
			currentRequestedPeer = nil

			for currentRequestedPeer != nil && (currentIdx+1)%len(l) != randomIdx {
				currentRequestedPeer = l[currentIdx%len(l)]
				if gossiper.peerNumRequest[currentRequestedPeer] < REQUEST_BLOCK_WAIT {
					currentRequestedPeer = nil
				}
				currentIdx++
			}
			gossiper.peerNumRequestMutex.Unlock()
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

		requestedName := "_nobody_"
		if currentRequestedPeer != nil {
			requestedName = currentRequestedPeer.String()
		}
		gossiper.errLogger.Printf("[bc_rout]: requester request block %x to %s", blockHash[:], requestedName)
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

				gossiper.errLogger.Printf("[bc_rout]: valid requested inventory from %s, size send = %d", from.String(), counter)

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
				gossiper.errLogger.Printf("[bc_rout]: valid requested block %x, from %s", request.BlockHash[:], from.String())

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

	tmp := block.hash()
	gossiper.errLogger.Printf("[bc_rout]: block %x sent to %s", tmp[:], to.String())

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

		// check that the block wasn't corrupted by UDP
		block, err := reply.Block.toNormal()
		if err != nil {
			return err
		}

		tmp := block.hash()
		corrupted := !bytes.Equal(tmp[:], reply.Hash[:])

		if !corrupted {

			gossiper.blocksMutex.Lock()
			_, alreadyHaveBlock := gossiper.blocks[reply.Hash]
			gossiper.blocksMutex.Unlock()

			// check if we don't already have the block
			if !alreadyHaveBlock {

				// verfiy block
				verify := gossiper.VerifyBlock(block)
				if verify {
					gossiper.errLogger.Printf("[bc_route]: valid block %x received and prevhash %x.", reply.Hash[:], reply.Block.PrevHash[:])

					// check if we are expecting this block
					gossiper.blockInRequestMutex.Lock()
					_, isRequestedBlock := gossiper.blockInRequest[reply.Hash]
					gossiper.blockInRequestMutex.Unlock()

					// it's a totally unexpected block =>
					// it's a recently mined block, needs to forward to our neighboor
					if !isRequestedBlock {
						gossiper.peersMutex.Lock()
						for _, peer := range gossiper.peers {
							gossiper.sendBlockTo(block, peer.addr)
						}
						gossiper.peersMutex.Unlock()

						gossiper.errLogger.Printf("It's a recently mined block")
					} else {
						gossiper.errLogger.Printf("It's a requested block")
					}

					// modifying the blockchain, if needed
					gossiper.blocksMutex.Lock()
					gossiper.forksMutex.Lock()

					// add the block to the big block map
					gossiper.blocks[reply.Hash] = block

					// see if we can add this block to one of the top fork
					found := false
					mainBranch := false
					foundNewTop := false
					var toRemove [32]byte
					for hashTopFork, _ := range gossiper.forks {

						// check if we found a top
						if bytes.Equal(reply.Block.PrevHash[:], hashTopFork[:]) {
							found = true
							foundNewTop = true
							toRemove = hashTopFork // need to remove this one from fork

							// Checking if we found on the main branch or not
							gossiper.topBlockMutex.Lock()
							mainBranch = bytes.Equal(hashTopFork[:], gossiper.topBlock[:])
							gossiper.topBlockMutex.Unlock()
						}

						// TODO optim break
						// need to go over all fork chain
						currentBlock := gossiper.blocks[hashTopFork]
						currentHash := hashTopFork
						for !found && !bytes.Equal(currentHash[:], NilHash[:]) {

							// if found => new fork
							if bytes.Equal(currentHash[:], reply.Block.PrevHash[:]) {
								found = true
							}

							// if not found, pass to the next block in the chain
							currentHash = currentBlock.PrevHash
							currentBlock = gossiper.blocks[currentBlock.PrevHash]
						}

						// if found either a new top or a new fork, we are done
						if found {
							break
						}
					}

					if found {

						gossiper.blockOrphanPoolMutex.Lock()
						gossiper.errLogger.Printf("It's put at the top of %x", toRemove[:])

						// replace fork top
						gossiper.forks[reply.Hash] = true
						// but remove it from top only if the prev was a top
						if foundNewTop {
							delete(gossiper.forks, toRemove)
						}

						// Slice to store all blocks that will need their txs to be removed from the txPool;
						// it will happen when the current block is the prev of a orphan block: we should
						// not forget to remove txs of this block from txPool
						var blocksToRemoveTxs []*Block
						blocksToRemoveTxs = append(blocksToRemoveTxs, block)

						// fixed point needed to move on we had the possibility to put the new block at the top of the updated fork
						currentTopForkHash := reply.Hash
						currentTopForkBlock := block
						done := false
						for !done {

							// we assume we won't need a new pass at the beginning of each pass
							done = true

							// we pass over each orphan
							for hash, prevHash := range gossiper.blockOrphanPool {

								if bytes.Equal(prevHash[:], currentTopForkHash[:]) {

									newBlockTop := gossiper.blocks[hash]

									currentTopForkBlock = newBlockTop

									// Append orphan to slice containing all blocks from which
									// we'll remove txs from txPool
									blocksToRemoveTxs = append(blocksToRemoveTxs, newBlockTop)

									// replace fork top
									gossiper.forks[hash] = true
									delete(gossiper.forks, currentTopForkHash)
									currentTopForkHash = hash

									done = false // found a new top, need to repeat the process again
								}
							}

							// remove from orphan if needed
							if !done {
								delete(gossiper.blockOrphanPool, currentTopForkHash)

								gossiper.errLogger.Printf("Orphan %x is now top of the fork", currentTopForkHash[:])
							}

						}

						gossiper.forksMutex.Unlock()

						// now that the new top fork is at its max, need to compare with current top
						gossiper.topBlockMutex.Lock()
						currentTopBlock := gossiper.blocks[gossiper.topBlock]
						gossiper.topBlockMutex.Unlock()
						if currentTopBlock.Height < currentTopForkBlock.Height {
							gossiper.errLogger.Printf("It's the new longest blockchain")

							// Clean the txPool from the txs in the new blocks
							for _, blockToRemoveTxs := range blocksToRemoveTxs {
								gossiper.removeBlockTxsFromPool(blockToRemoveTxs)
							}

							// If a secondary branch became the main branch, we need to find where is the fork
							// happening from the main branch and we need to call 'removeBlockTxsFromPool' for all
							// blocks between the first new block of the fork until the block we've just received (exclusive)
							if !mainBranch {
								gossiper.errLogger.Printf("It was a secondary branch that became the main one")

								// Find the hash of the block that is in the main branch AND in the fork
								forkHash, forkErr := gossiper.findForkBlockHash(block)
								if forkErr != nil {
									return forkErr
								}

								// Remove txs from txPool for all blocks between the block we've just received
								// (exclusive) and the the first new block of the fork
								currentHash := block.PrevHash
								for !bytes.Equal(currentHash[:], forkHash[:]) {
									currentBlock, blockExists := gossiper.blocks[currentHash]
									if !blockExists {
										return errors.New(fmt.Sprintf("Couldn't find block in 'gossiper.blocks' (hash = %x).", currentHash[:]))
									}

									// Remove this block's txs from txPool
									gossiper.removeBlockTxsFromPool(currentBlock)

									currentHash = currentBlock.PrevHash
								}
							}

							// Update topBlock
							gossiper.topBlockMutex.Lock()
							gossiper.topBlock = currentTopForkHash
							gossiper.topBlockMutex.Unlock()

							// Warn Miner that he lost the round
							gossiper.miningChannel <- true

							// new top means that we may remove some orphan
							for hash, _ := range gossiper.blockOrphanPool {
								orphanBlock := gossiper.blocks[hash]
								if currentTopBlock.Height-orphanBlock.Height >= DIFF_TO_DELETE_ORPHAN {
									delete(gossiper.blocks, hash)
									delete(gossiper.blockOrphanPool, hash)
								}
							}
						}

						gossiper.blockOrphanPoolMutex.Unlock()
						gossiper.blocksMutex.Unlock()

					} else {

						gossiper.forksMutex.Unlock()
						gossiper.blocksMutex.Unlock()

						// can't find a place to put it, it's an orphan
						gossiper.blockOrphanPoolMutex.Lock()
						gossiper.blockOrphanPool[reply.Hash] = reply.Block.PrevHash
						gossiper.blockOrphanPoolMutex.Unlock()

						gossiper.errLogger.Printf("It's put in the orphan pool")
					}

					return nil
				} else {
					return errors.New("Block wrong at verification step")
				}
			} else {
				// we already have the block, no operation
				return nil
			}
		} else {
			return errors.New("Block corrupted")
		}
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
