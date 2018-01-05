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

	gossiper.errLogger.Printf("Received new tx from network: %s", packet.from.String())
	gossiper.errLogger.Println(tx)

	// Verify transaction
	valid, orphan := gossiper.VerifyTx(tx)
	if !valid {
		return errors.New("Error during transaction verification")
	}

	if orphan {
		gossiper.orphanTxPoolMutex.Lock()
		gossiper.orphanTxPool = append(gossiper.orphanTxPool, tx)
		gossiper.orphanTxPoolMutex.Unlock()
		return nil
	}

	// Add to transaction pool
	gossiper.txPoolMutex.Lock()
	gossiper.txPool = append(gossiper.txPool, tx)
	gossiper.txPoolMutex.Unlock()

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

	gossiper.peersMutex.Lock()
	for _, peer := range gossiper.peers {
		gossiper.gossipConn.WriteToUDP(buffer, peer.addr)
	}
	gossiper.peersMutex.Unlock()

	return nil
}
