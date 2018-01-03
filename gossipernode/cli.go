package gossipernode

import (
	"github.com/dedis/protobuf"
)

func (gossiper *Gossiper) CLIRoutine(channel <-chan *Packet) {
	for {
		packet := <-channel
		// Decode message
		clientPacket := ClientPacket{}
		err := protobuf.Decode(packet.payload, &clientPacket)
		if err != nil {
			// gossiper.errLogger.Printf("Error decoding message: %v\n", err)
			// continue
		}

		gossiper.handleCLI(&clientPacket)
	}
}

func (gossiper *Gossiper) handleCLI(clientPacket *ClientPacket) {
	if clientPacket.NewMessage != nil {
		gossiper.sendNewMessage(clientPacket.NewMessage.Message)
	} else if clientPacket.NewName != nil {
		gossiper.changeName(clientPacket.NewName.Name)
	} else if clientPacket.NewPeer != nil {
		gossiper.addNewPeer(clientPacket.NewPeer.Address)
	} else if clientPacket.NewPrivateMessage != nil {
		private := clientPacket.NewPrivateMessage
		gossiper.sendNewPrivateMessage(private.Message, private.Dest)
	} else if clientPacket.NewFile != nil {
		gossiper.shareFile(clientPacket.NewFile.FileName)
	} else if clientPacket.NewSearchRequest != nil {
		request := clientPacket.NewSearchRequest
		gossiper.searchFile(request.Keywords, request.Budget)
	} else if clientPacket.DownloadRequest != nil {
		gossiper.downloadFile(clientPacket.DownloadRequest.FileName)
	} else {
		gossiper.errLogger.Println("Empty client packet")
	}
}
