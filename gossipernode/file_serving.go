package gossipernode

import (
	"strings"
	"time"
)

func (gossiper *Gossiper) SearchRequestRoutine(channel <-chan *GossiperPacketSender) {
	for {
		packet := <-channel
		err := gossiper.handleSearchRequest(packet)
		if err != nil {
			gossiper.errLogger.Printf("Error processing the search request: %v", err)
		}
	}
}

func (gossiper *Gossiper) handleSearchRequest(packet *GossiperPacketSender) error {
	request := packet.packet.SearchRequest

	// Decrease budget and drop if necessary
	request.Budget--
	if request.Budget == 0 {
		return nil
	}

	// Look for duplicate
	duplicate := gossiper.isDuplicate(request)
	if duplicate {
		return nil
	}

	// Store new
	gossiper.archiveRecentRequest(request)

	gossiper.errLogger.Printf("New search request %v, budget %d", request.Origin, request.Budget)

	// Broadcast
	gossiper.broadcastRequest(request, packet.from)

	// Process request
	results := gossiper.buildSearchResults(request.Keywords)
	if results == nil {
		return nil
	}

	// Reply to request
	reply := &SearchReply{
		Origin:      gossiper.name,
		Destination: request.Origin,
		HopLimit:    HopLimit,
		Results:     results,
	}

	privatePacket := gossiper.packPrivateSearchReply(reply)
	gossiper.errLogger.Printf("Answering request to %s", request.Origin)
	gossiper.sendPrivateNoTimeout(privatePacket, request.Origin)

	return nil
}

func (gossiper *Gossiper) buildSearchResults(keywords []string) []*SearchResult {
	gossiper.filesMutex.Lock()
	defer gossiper.filesMutex.Unlock()

	var results []*SearchResult
	for name, file := range gossiper.files {
		for _, keyword := range keywords {
			if strings.Contains(name, keyword) {
				result := &SearchResult{
					FileName:     name,
					MetafileHash: file.MetaHash,
				}

				for chunkIdx, _ := range file.Content {
					result.ChunkMap = append(result.ChunkMap, chunkIdx)
				}

				results = append(results, result)
				break
			}
		}
	}

	return results
}

func (gossiper *Gossiper) isDuplicate(request *SearchRequest) bool {
	gossiper.recentRequestsMutex.Lock()
	defer gossiper.recentRequestsMutex.Unlock()
	now := time.Now()

	cutIdx := 0
	for i := len(gossiper.recentReceivedRequests) - 1; i >= 0; i-- {
		recent := gossiper.recentReceivedRequests[i]

		if now.Sub(recent.Timestamp) < RecentRequestAgeMs*time.Millisecond {
			if recent.Request.Origin == request.Origin {
				for j, k := range recent.Request.Keywords {
					if k != request.Keywords[j] {
						break
					}

					// Same request
					gossiper.errLogger.Println("Same request in recents: %s, %v", request.Origin, request.Keywords)
					return true
				}
			}
		} else {
			cutIdx = i
			break
		}
	}

	// Remove old recent request
	gossiper.recentReceivedRequests = gossiper.recentReceivedRequests[cutIdx:]
	return false
}

func (gossiper *Gossiper) archiveRecentRequest(request *SearchRequest) {
	gossiper.recentRequestsMutex.Lock()
	defer gossiper.recentRequestsMutex.Unlock()
	newRecent := &ReceivedSearchRequest{
		Timestamp: time.Now(),
		Request:   request,
	}
	gossiper.recentReceivedRequests = append(gossiper.recentReceivedRequests, newRecent)
}
