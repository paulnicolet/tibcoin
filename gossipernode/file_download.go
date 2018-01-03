package gossipernode

import (
	"bytes"
	"fmt"
	"math/rand"
	"time"
)

type DownloadState struct {
	SearchState *FileSearchState
	Tickers     map[uint64]*Timeout
}

func (gossiper *Gossiper) DataReplyRoutine(channel <-chan *GossiperPacketSender) {
	for {
		packet := <-channel
		err := gossiper.handleDataReply(packet)
		if err != nil {
			gossiper.errLogger.Printf("Error processing the data reply: %v", err)
		}
	}
}

func (gossiper *Gossiper) handleDataReply(packet *GossiperPacketSender) error {
	dataReply := packet.packet.DataReply

	// Route packet
	privatePacket := gossiper.packPrivateReply(dataReply)
	gossiper.errLogger.Printf("REPLY FROM %s TO %s", dataReply.Origin, dataReply.Destination)
	arrived, err := gossiper.routePrivatePacket(privatePacket)
	dataReply.HopLimit = privatePacket.HopLimit
	if err != nil {
		return err
	}

	// The packet is for the current gossiper
	if arrived {
		return gossiper.processDataReply(dataReply)
	}

	return nil
}

func (gossiper *Gossiper) processDataReply(reply *DataReply) error {
	// Process metafile if it is one
	if gossiper.processMetaFile(reply) {
		gossiper.errLogger.Println("METAFILE")
		return nil
	}

	gossiper.errLogger.Println("NOT METAFILE")

	// It is not a metafile, process chunk then
	// Get the download state
	gossiper.downloadsMutex.Lock()
	download, in := gossiper.downloads[reply.FileName]
	gossiper.downloadsMutex.Unlock()
	if !in {
		return fmt.Errorf("No current download for file %s", reply.FileName)
	}

	// Get file
	file, in := gossiper.getFile(reply.FileName)
	if !in {
		return fmt.Errorf("File structure not found for name %s", reply.FileName)
	}

	// Store chunk or drop
	err := gossiper.storeChunk(reply, file, download)
	if err != nil {
		return err
	}

	return nil
}

func (gossiper *Gossiper) processMetaFile(reply *DataReply) bool {
	gossiper.currentFileSearchMutex.Lock()
	for filename, state := range gossiper.currentFileSearch.Files {
		// Is it a metafile ?
		if filename == reply.FileName && bytes.Equal(reply.HashValue, state.MetaHash) {
			// Do we have a timer for it ?
			timeout, in := state.MetaFileRequests[reply.Origin]
			if !in {
				// No timer, just ignore
				gossiper.currentFileSearchMutex.Unlock()
				return true
			}

			// Cancel timeout
			KillTimeout(timeout)

			// Store metafile
			state.MetaFile = reply.Data

			// Remove metafile request
			delete(state.MetaFileRequests, reply.Origin)

			gossiper.errLogger.Printf("Got metafile for %s from %s", reply.FileName, reply.Origin)
			gossiper.currentFileSearchMutex.Unlock()

			// Do we already have all the chunks ?
			nChunk := len(state.MetaFile) / HashSize
			if state.AvailableChunks == uint64(nChunk) {
				// It's a match
				gossiper.errLogger.Println("New match")

				// Update matches
				nMatches := gossiper.newMatch(state)

				// Stop request if necessary
				gossiper.currentFileSearchMutex.Lock()
				if nMatches >= MatchThreshold {
					KillTimeout(gossiper.currentFileSearch.Timeout)
				}

				// Remove state
				delete(gossiper.currentFileSearch.Files, reply.FileName)
				gossiper.currentFileSearchMutex.Unlock()
				return true
			}
			return true
		}
	}
	gossiper.currentFileSearchMutex.Unlock()
	return false
}

func (gossiper *Gossiper) launchDownload(state *FileSearchState) {
	// Create file
	file := &File{
		Name:     state.FileName,
		MetaFile: state.MetaFile,
		MetaHash: state.MetaHash,
		Content:  make(map[uint64][]byte),
	}
	gossiper.filesMutex.Lock()
	gossiper.files[state.FileName] = file
	gossiper.filesMutex.Unlock()

	// Create download
	download := &DownloadState{
		SearchState: state,
		Tickers:     make(map[uint64]*Timeout),
	}

	gossiper.downloadsMutex.Lock()
	gossiper.downloads[state.FileName] = download
	gossiper.downloadsMutex.Unlock()

	for chunkIdx, _ := range state.Chunks {
		timeout := &Timeout{
			Ticker: time.NewTicker(DataRequestRepeatDelay * time.Second),
			Killer: make(chan bool),
		}

		download.Tickers[chunkIdx] = timeout

		go gossiper.downloadChunk(timeout, download, chunkIdx)
	}

}

func (gossiper *Gossiper) downloadChunk(timeout *Timeout, download *DownloadState, chunkIdx uint64) {
	peers := download.SearchState.Chunks[chunkIdx]
	peer := peers[rand.Intn(len(peers))]

	lowIdx := chunkIdx * HashSize
	highIdx := (chunkIdx + 1) * HashSize
	hash := download.SearchState.MetaFile[lowIdx:highIdx]

	request := &DataRequest{
		Origin:      gossiper.name,
		Destination: peer,
		HopLimit:    HopLimit,
		FileName:    download.SearchState.FileName,
		HashValue:   hash,
	}

	gossiper.stdLogger.Printf("DOWNLOADING %s chunk %d from %s", download.SearchState.FileName, chunkIdx, peer)
	privatePacket := gossiper.packPrivateRequest(request)
	gossiper.sendPrivateNoTimeout(privatePacket, peer)

	for {
		select {
		case <-timeout.Ticker.C:
			peer = peers[rand.Intn(len(peers))]

			gossiper.stdLogger.Printf("DOWNLOADING %s chunk %d from %s", download.SearchState.FileName, chunkIdx, peer)
			privatePacket = gossiper.packPrivateRequest(request)
			gossiper.sendPrivateNoTimeout(privatePacket, peer)

		case <-timeout.Killer:
			return
		}
	}
}

func (gossiper *Gossiper) storeChunk(reply *DataReply, file *File, state *DownloadState) error {
	// Get chunk index
	chunkIdx, err := gossiper.getChunkIdx(file, reply.HashValue)
	if err != nil {
		return err
	}

	// Check if we don't already received this chunk
	_, in := file.Content[chunkIdx]
	if in {
		return nil
	}

	// Store it and cancel timer
	KillTimeout(state.Tickers[chunkIdx])

	gossiper.errLogger.Printf("Storing chunk idx %d of file %s", chunkIdx, reply.FileName)
	file.Content[chunkIdx] = reply.Data

	nChunk := len(file.MetaFile) / HashSize
	if len(file.Content) >= nChunk {
		// Archive file
		gossiper.archiveFile(file)

		// Remove download state
		gossiper.downloadsMutex.Lock()
		delete(gossiper.downloads, reply.FileName)
		gossiper.downloadsMutex.Unlock()
		return nil
	}

	return nil
}
