package gossipernode

import (
	"github.com/paulnicolet/Peerster/part2/common"
)

// -------------------------------- Routines -------------------------------- //

func (gossiper *Gossiper) PrivateMessageRoutine(channel <-chan *GossiperPacketSender) {
	for {
		packet := <-channel
		err := gossiper.handlePrivateMessage(packet)
		if err != nil {
			gossiper.errLogger.Printf("Error processing the private message: %v", err)
		}
	}
}

// -------------------------------- Handlers -------------------------------- //

func (gossiper *Gossiper) handlePrivateMessage(packet *GossiperPacketSender) error {
	privateMessage := packet.packet.Private

	// Pack in private packet for simulate inheritance
	privatePacket := &common.PrivatePacket{
		Origin:         privateMessage.Origin,
		Destination:    privateMessage.Destination,
		HopLimit:       privateMessage.HopLimit,
		PrivateMessage: privateMessage,
	}

	// Route packet
	arrived, err := gossiper.routePrivatePacket(privatePacket)
	privateMessage.HopLimit = privatePacket.HopLimit
	if err != nil {
		return err
	}

	// The packet is for the current gossiper
	if arrived {
		gossiper.messagesMutex.Lock()
		history := gossiper.getHistory(privateMessage.Origin)
		history.privateChat = append(history.privateChat, privateMessage)
		gossiper.messagesMutex.Unlock()

		gossiper.logPrivateMessage(privateMessage)
	}

	return nil
}
