package gossipernode

import (
	"errors"

	"github.com/dedis/protobuf"
)

func (gossiper *Gossiper) TxRoutine(channel <-chan *GossiperPacketSender) {
	for {
		packet := <-channel
		err := gossiper.handleTx(packet)
		if err != nil {
			gossiper.errLogger.Printf("Error processing the transaction: %v", err)
		}
	}
}

func (gossiper *Gossiper) handleTx(packet *GossiperPacketSender) error {
	tx, err := packet.packet.Tx.toNormal()
	if err != nil {
		return err
	}

	gossiper.errLogger.Printf("Received new tx %x from network: %s", tx.hash(), packet.from.String())

	// Verify transaction
	validationResult := gossiper.VerifyTx(tx)
	if validationResult == InvalidTx {
		gossiper.errLogger.Printf("Invalid tx: reject %x", tx.hash())
		return errors.New("Error during transaction verification")
	} else if validationResult == DuplicateTx {
		gossiper.errLogger.Printf("Tx already in pool, ignoring: %x", tx.hash())
		return nil
	} else if validationResult == OrphanTx {
		gossiper.addToOrphanPool(tx)
		return nil
	}

	// Add to transaction pool
	gossiper.addToPool(tx)

	// Try to validate some orphans
	gossiper.updateOrphansTx(tx)

	// Broadcast transaction
	return gossiper.broadcastTx(tx)
}

func (gossiper *Gossiper) broadcastTx(tx *Tx) error {
	// We must convert to SerializableTx because big.Int is not serializable by protobuf
	serTx, err := tx.toSerializable()
	if err != nil {
		return err
	}

	// Mashall message
	packet := GossipPacket{Tx: serTx}
	buffer, err := protobuf.Encode(&packet)
	if err != nil {
		return err
	}

	gossiper.errLogger.Printf("Broadcasting tx to all peers: %x", tx.hash())
	gossiper.peersMutex.Lock()
	for _, peer := range gossiper.peers {
		gossiper.gossipConn.WriteToUDP(buffer, peer.addr)
	}
	gossiper.peersMutex.Unlock()

	return nil
}
