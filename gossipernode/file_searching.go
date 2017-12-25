package gossipernode

import (
	"net"
	"time"

	"github.com/dedis/protobuf"
	"github.com/paulnicolet/tibcoin/common"
)

type FileSearchState struct {
	FileName         string
	MetaHash         []byte
	MetaFile         []byte
	Chunks           map[uint64][]string
	AvailableChunks  uint64
	MetaFileRequests map[string]*Timeout
}

type FileSearchRequest struct {
	Keywords      []string
	CurrentBudget uint64
	Files         map[string]*FileSearchState
	Timeout       *Timeout
}

type ReceivedSearchRequest struct {
	Timestamp time.Time
	Request   *common.SearchRequest
}

func (gossiper *Gossiper) SearchReplyRoutine(channel <-chan *GossiperPacketSender) {
	for {
		packet := <-channel
		err := gossiper.handleSearchReply(packet)
		if err != nil {
			gossiper.errLogger.Printf("Error processing the data request: %v", err)
		}
	}
}

func (gossiper *Gossiper) handleSearchReply(packet *GossiperPacketSender) error {
	reply := packet.packet.SearchReply

	// Pack in private packet for simulate inheritance
	privatePacket := gossiper.packPrivateSearchReply(reply)

	// Route packet
	arrived, err := gossiper.routePrivatePacket(privatePacket)
	reply.HopLimit = privatePacket.HopLimit
	if err != nil {
		return err
	}

	// The packet is for the current gossiper
	if arrived {
		return gossiper.processSearchReply(reply)
	}
	return nil
}

func (gossiper *Gossiper) processSearchReply(reply *common.SearchReply) error {
	gossiper.errLogger.Printf("New search reply %v", reply.Origin)

	gossiper.currentFileSearchMutex.Lock()
	gossiper.logSearchReply(reply, gossiper.currentFileSearch.CurrentBudget)
	gossiper.currentFileSearchMutex.Unlock()

	for _, result := range reply.Results {
		// Get state for file
		state := gossiper.getFileSearchState(result.FileName, result.MetafileHash)

		// Update state
		gossiper.updateSearchState(result, reply.Origin, state)

		if state.MetaFile == nil {
			// Request metafile
			request := &common.DataRequest{
				Origin:      gossiper.name,
				Destination: reply.Origin,
				HopLimit:    common.HopLimit,
				FileName:    result.FileName,
				HashValue:   result.MetafileHash,
			}

			// New ticker to repeat request
			timeout := &Timeout{
				Ticker: time.NewTicker(common.DataRequestRepeatDelay * time.Second),
				Killer: make(chan bool),
			}
			state.MetaFileRequests[reply.Origin] = timeout

			// Send private packet
			gossiper.stdLogger.Printf("DOWNLOADING metafile of %s from %s", result.FileName, reply.Origin)
			privatePacket := gossiper.packPrivateRequest(request)
			gossiper.sendPrivateNoTimeout(privatePacket, reply.Origin)

			// Launch reminder
			go gossiper.requestTicker(timeout, privatePacket, reply.Origin)
		} else {
			// Do we have all the chunks available ?
			nChunk := len(state.MetaFile) / common.HashSize

			if state.AvailableChunks == uint64(nChunk) {
				// It's a match
				gossiper.errLogger.Println("New match")

				// Add new match
				nMatches := gossiper.newMatch(state)

				// Stop request if necessary
				gossiper.currentFileSearchMutex.Lock()
				if nMatches >= common.MatchThreshold {
					KillTimeout(gossiper.currentFileSearch.Timeout)
				}

				// Remove file state from search
				delete(gossiper.currentFileSearch.Files, result.FileName)
				gossiper.currentFileSearchMutex.Unlock()
			}
		}

	}
	return nil
}

func (gossiper *Gossiper) broadcastRequest(request *common.SearchRequest, relay *net.UDPAddr) {
	gossiper.errLogger.Println("Broadcasting search request")
	gossiper.peersMutex.Lock()
	defer gossiper.peersMutex.Unlock()

	budget := request.Budget
	nNeighbors := uint64(len(gossiper.peers))
	if relay != nil && nNeighbors > 0 {
		nNeighbors--
		if nNeighbors == 0 {
			return
		}
	}
	perNeighbor := budget / nNeighbors
	oneMoreN := budget % nNeighbors

	peerIdx := 0
	for ip, peer := range gossiper.peers {
		if relay != nil && ip == relay.String() {
			continue
		}

		finalBudget := perNeighbor
		if uint64(peerIdx) < oneMoreN {
			finalBudget++
		}

		request.Budget = finalBudget

		// Send message
		packet := &common.GossipPacket{SearchRequest: request}
		buffer, err := protobuf.Encode(packet)

		_, err = gossiper.gossipConn.WriteToUDP(buffer, peer.addr)
		if err != nil {
			gossiper.errLogger.Printf("Could not send search request: %v", err)
		}
	}
}

func (gossiper *Gossiper) repeatSearchRequest(ticker *time.Ticker, tickerKiller chan bool) {
	for {
		select {
		case <-ticker.C:
			// Build the new request
			gossiper.currentFileSearchMutex.Lock()
			gossiper.currentFileSearch.CurrentBudget *= 2
			currentBudget := gossiper.currentFileSearch.CurrentBudget
			gossiper.currentFileSearchMutex.Unlock()

			gossiper.errLogger.Printf("Sending again request with budget %d", currentBudget)

			request := &common.SearchRequest{
				Origin:   gossiper.name,
				Budget:   currentBudget,
				Keywords: gossiper.currentFileSearch.Keywords,
			}

			gossiper.broadcastRequest(request, nil)

			if currentBudget >= common.MaxBudget {
				gossiper.stdLogger.Println("SEARCH FINISHED")
				ticker.Stop()
				return
			}

		case <-tickerKiller:
			gossiper.stdLogger.Println("SEARCH FINISHED")
			return
		}
	}
}

func (gossiper *Gossiper) updateSearchState(result *common.SearchResult, from string, state *FileSearchState) {
	for chunkIdx := range result.ChunkMap {
		_, in := state.Chunks[uint64(chunkIdx)]
		if !in {
			state.AvailableChunks++
		}
		state.Chunks[uint64(chunkIdx)] = append(state.Chunks[uint64(chunkIdx)], from)
	}
}

func (gossiper *Gossiper) newMatch(state *FileSearchState) int {
	gossiper.matchesMutex.Lock()
	defer gossiper.matchesMutex.Unlock()

	if !gossiper.alreadyMatched(state.FileName, gossiper.matches) {
		gossiper.matches = append(gossiper.matches, state)
	}
	return len(gossiper.matches)
}

func (gossiper *Gossiper) getFileSearchState(filename string, metahash []byte) *FileSearchState {
	gossiper.currentFileSearchMutex.Lock()
	state, in := gossiper.currentFileSearch.Files[filename]
	gossiper.currentFileSearchMutex.Unlock()

	if !in {
		state = &FileSearchState{
			FileName:         filename,
			MetaHash:         metahash,
			Chunks:           make(map[uint64][]string),
			MetaFileRequests: make(map[string]*Timeout),
		}

		gossiper.currentFileSearchMutex.Lock()
		gossiper.currentFileSearch.Files[filename] = state
		gossiper.currentFileSearchMutex.Unlock()
	}

	return state
}
