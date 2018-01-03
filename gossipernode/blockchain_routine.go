package gossipernode

import (
	"bytes"
	"crypto/sha256"
	"errors"

	"github.com/dedis/protobuf"
)

const INVENTORY_SIZE = 10

func (gossiper *Gossiper) getInventory(topBlockHash [32]byte) {

	// ticker each 10 seconds
	// send hash block request WaitingInv = true

}

func (gossiper *Gossiper) requestBlocksFromInventory(inventory [][32]byte) {

	// create new thread for each hash in inventory and launch requestBlock
	// if map blabla
}

func (gossiper *Gossiper) requestBlock(blockHash [][32]byte) {

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

	if request.WaitingInv {

		to, containsOrigin := gossiper.peers[request.Origin]
		if containsOrigin {

			gossiper.topBlockMutex.Lock()
			currentTopBlockHash := gossiper.topBlock
			gossiper.topBlockMutex.Unlock()

			if !bytes.Equal(currentTopBlockHash[:], request.BlockHash[:]) {
				blocksHashChan := make(chan [32]byte, INVENTORY_SIZE)
				blocksHashChan <- currentTopBlockHash
				counter := 1

				gossiper.blocksMutex.Lock()
				currentBlockHash := gossiper.blocks[currentTopBlockHash]
				gossiper.blocksMutex.Unlock()

				for !bytes.Equal(currentBlockHash.PrevHash[:], request.BlockHash[:]) {

					if counter == INVENTORY_SIZE {
						counter--
						<-blocksHashChan
					}

					blocksHashChan <- currentBlockHash.PrevHash
					counter++

					gossiper.blocksMutex.Lock()
					currentBlockHash = gossiper.blocks[currentBlockHash.PrevHash]
					gossiper.blocksMutex.Unlock()

				}

				blocksHash := make([][32]byte, counter)
				concatHash := make([]byte, 0)
				for i := 0; i < counter; i++ {
					blocksHash[i] = <-blocksHashChan
					concatHash = append(concatHash, blocksHash[i][:]...)
				}

				packet := &GossipPacket{
					BlockReply: &BlockReply{
						Origin:     gossiper.name,
						Hash:       sha256.Sum256(concatHash),
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
				return nil
			}

		} else {
			return errors.New("Unknown origin")
		}

	} else {

		gossiper.blocksMutex.Lock()
		block, containsBlock := gossiper.blocks[request.BlockHash]
		gossiper.blocksMutex.Unlock()

		if containsBlock {

			to, containsOrigin := gossiper.peers[request.Origin]
			if containsOrigin {

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

				_, err = gossiper.gossipConn.WriteToUDP(buffer, to.addr)
				return err

			} else {
				return errors.New("Unknown origin")
			}
		} else {
			return errors.New("Unknown requested block")
		}
	}
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
					gossiper.blocksMutex.Lock()
					gossiper.blocks[reply.Hash] = reply.Block
					gossiper.blocksMutex.Unlock()

					// TODO check if orphan can be removed
					// TODO check if new forks appears
					// TODO check if new top block

					// TODO remove from the map of waiting block

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
			go gossiper.requestBlocksFromInventory(reply.BlocksHash)
			return nil
		} else {
			return errors.New("Inventory corrupted")
		}
	}
}
