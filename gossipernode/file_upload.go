package gossipernode

import (
	"bytes"
	"fmt"

	"github.com/paulnicolet/tibcoin/common"
)

func (gossiper *Gossiper) DataRequestRoutine(channel <-chan *GossiperPacketSender) {
	for {
		packet := <-channel
		err := gossiper.handleDataRequest(packet)
		if err != nil {
			gossiper.errLogger.Printf("Error processing the data request: %v", err)
		}
	}
}

func (gossiper *Gossiper) handleDataRequest(packet *GossiperPacketSender) error {
	dataRequest := packet.packet.DataRequest

	// Pack in private packet for simulate inheritance
	privatePacket := gossiper.packPrivateRequest(dataRequest)

	// Route packet
	arrived, err := gossiper.routePrivatePacket(privatePacket)
	dataRequest.HopLimit = privatePacket.HopLimit
	if err != nil {
		return err
	}

	// The packet is for the current gossiper
	if arrived {
		return gossiper.processDataRequest(dataRequest)
	}

	return nil
}

func (gossiper *Gossiper) processDataRequest(request *common.DataRequest) error {
	// Get corresponding file
	gossiper.filesMutex.Lock()
	file, in := gossiper.files[request.FileName]
	gossiper.filesMutex.Unlock()
	if !in {
		return fmt.Errorf("File %s does not exist", request.FileName)
	}

	// Send metafile or chunk
	if bytes.Equal(request.HashValue, file.MetaHash) {
		// Send metafile
		reply := &common.DataReply{
			Origin:      gossiper.name,
			Destination: request.Origin,
			FileName:    request.FileName,
			HopLimit:    common.HopLimit,
			HashValue:   file.MetaHash,
			Data:        file.MetaFile,
		}

		// Encapsulate in private packet and send
		privatePacket := gossiper.packPrivateReply(reply)
		gossiper.sendPrivateNoTimeout(privatePacket, request.Origin)
	} else {
		return gossiper.sendChunk(request)
	}

	return nil
}

func (gossiper *Gossiper) sendChunk(request *common.DataRequest) error {
	// Get file
	file, in := gossiper.getFile(request.FileName)
	if !in {
		return fmt.Errorf("File structure not found for name %s", request.FileName)
	}

	// Look for requested chunk
	chunkIdx, err := gossiper.getChunkIdx(file, request.HashValue)
	if err != nil {
		return err
	}

	// Send data
	data := gossiper.getChunk(file, uint64(chunkIdx))
	reply := &common.DataReply{
		Origin:      gossiper.name,
		Destination: request.Origin,
		FileName:    request.FileName,
		HopLimit:    common.HopLimit,
		HashValue:   request.HashValue,
		Data:        data,
	}

	// Encapsulate in private packet and send
	gossiper.errLogger.Printf("Sending chunk idx %d", chunkIdx)
	privatePacket := gossiper.packPrivateReply(reply)
	gossiper.sendPrivateNoTimeout(privatePacket, request.Origin)

	return nil
}
