package gossipernode

import (
	"net"
	"time"

	"github.com/paulnicolet/tibcoin/common"
)

func (gossiper *Gossiper) sendNewPrivateMessage(message string, dest string) {
	gossiper.stdLogger.Printf("CLIENT PRIVATE %s %s\n", message, dest)

	private := &common.PrivateMessage{
		Origin:      gossiper.name,
		Destination: dest,
		HopLimit:    common.HopLimit,
		ID:          0,
		Text:        message,
	}

	// Store private message for destination
	gossiper.messagesMutex.Lock()
	history := gossiper.getHistory(private.Destination)
	history.privateChat = append(history.privateChat, private)
	gossiper.messagesMutex.Unlock()

	// Send private packet
	privatePacket := gossiper.packPrivateMessage(private)
	gossiper.sendPrivateNoTimeout(privatePacket, dest)
}

func (gossiper *Gossiper) sendNewMessage(message string) {
	rumor := &common.RumorMessage{Origin: gossiper.name, ID: gossiper.getID(), Text: message}

	gossiper.stdLogger.Printf("CLIENT %s %s\n", rumor.Text, gossiper.name)
	gossiper.logPeers()

	// Store rumor
	gossiper.storeRumor(rumor)

	// Pick peer
	peer := gossiper.pickNextPeer(nil)

	// Send
	if peer != nil {
		gossiper.stdLogger.Printf("MONGERING with %s\n", peer.addr.String())
		err := gossiper.sendRumorWithTimeout(rumor, peer)
		if err != nil {
			gossiper.errLogger.Printf("Could not monger with %s: %v \n", peer.addr.String(), err)
		}
	}
}

func (gossiper *Gossiper) changeName(name string) {
	gossiper.nameMutex.Lock()
	defer gossiper.nameMutex.Unlock()

	// Change name
	gossiper.name = name
}

func (gossiper *Gossiper) addNewPeer(peerAddr string) {
	gossiper.errLogger.Printf("Peer request: %s\n", peerAddr)
	addr, err := net.ResolveUDPAddr("udp", peerAddr)
	if err == nil {
		gossiper.addPeer(addr)
	} else {
		gossiper.errLogger.Printf("Could not add new peer %v: %v\n", addr, err)
	}
}

func (gossiper *Gossiper) downloadFile(filename string) {
	gossiper.errLogger.Printf("Download request %s", filename)

	gossiper.matchesMutex.Lock()
	for _, match := range gossiper.matches {
		if match.FileName == filename {
			gossiper.matchesMutex.Unlock()
			gossiper.launchDownload(match)
			return
		}
	}
	gossiper.matchesMutex.Unlock()
}

func (gossiper *Gossiper) searchFile(keywords []string, budget uint64) {
	gossiper.errLogger.Printf("Search file request %v, %d", keywords, budget)

	// Cancel previous request
	gossiper.currentFileSearchMutex.Lock()
	if gossiper.currentFileSearch != nil {
		KillTimeout(gossiper.currentFileSearch.Timeout)
	}
	gossiper.currentFileSearchMutex.Unlock()

	// Empty matches
	gossiper.matchesMutex.Lock()
	gossiper.matches = make([]*FileSearchState, 0)
	gossiper.matchesMutex.Unlock()

	// Create the new request to propagate
	request := &common.SearchRequest{
		Origin:   gossiper.name,
		Budget:   budget,
		Keywords: keywords,
	}

	// Store as recent request
	gossiper.recentRequestsMutex.Lock()
	receivedRequest := &ReceivedSearchRequest{
		Timestamp: time.Now(),
		Request:   request,
	}
	gossiper.recentReceivedRequests = append(gossiper.recentReceivedRequests, receivedRequest)
	gossiper.recentRequestsMutex.Unlock()

	ticker := time.NewTicker(common.FileSearchRepeatDelay * time.Second)
	tickerKiller := make(chan bool)
	timeout := &Timeout{
		Ticker: ticker,
		Killer: tickerKiller,
	}

	// Create the new file search request state
	gossiper.currentFileSearchMutex.Lock()
	gossiper.currentFileSearch = &FileSearchRequest{
		CurrentBudget: budget,
		Keywords:      keywords,
		Files:         make(map[string]*FileSearchState),
		Timeout:       timeout,
	}
	gossiper.currentFileSearchMutex.Unlock()

	// Broadcast the request
	gossiper.broadcastRequest(request, nil)

	// Launch ticker to repeat the query
	go gossiper.repeatSearchRequest(ticker, tickerKiller)
}

func (gossiper *Gossiper) shareFile(filename string) error {
	gossiper.errLogger.Printf("Got new filename: %v", filename)

	// Build file
	file, err := gossiper.buildFile(filename)
	if err != nil {
		return err
	}

	// Store file
	gossiper.filesMutex.Lock()
	gossiper.files[file.Name] = file
	gossiper.filesMutex.Unlock()

	return nil
}
