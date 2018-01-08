package gossipernode

import (
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strings"
	"time"
)

func (gossiper *Gossiper) sendNewPrivateMessage(message string, dest string) {
	gossiper.stdLogger.Printf("CLIENT PRIVATE %s %s\n", message, dest)

	private := &PrivateMessage{
		Origin:      gossiper.name,
		Destination: dest,
		HopLimit:    HopLimit,
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
	rumor := &RumorMessage{Origin: gossiper.name, ID: gossiper.getID(), Text: message}

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
	request := &SearchRequest{
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

	ticker := time.NewTicker(FileSearchRepeatDelay * time.Second)
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

// Return address
func (gossiper *Gossiper) createTibcoinNode(addrPrefix string, miner bool) error {
	if gossiper.isTibcoinNode() {
		return errors.New(fmt.Sprintf("You already have a key pair and an address; address = %s", PublicKeyToAddress(gossiper.publicKey)))
	}

	if len(addrPrefix) > MaxLengthPrefixAddress {
		return errors.New(fmt.Sprintf("The prefix requested is too long, it will take too much time to find an address. Max. size is: %d", MaxLengthPrefixAddress))
	}

	r, _ := regexp.Compile("^[a-zA-Z0-9]+$")

	if len(addrPrefix) > 0 && (!r.MatchString(addrPrefix) || strings.Index(addrPrefix, "0") != -1 || strings.Index(addrPrefix, "O") != -1 || strings.Index(addrPrefix, "I") != -1 || strings.Index(addrPrefix, "l") != -1) {
		return errors.New(fmt.Sprintf("The prefix should contain only alpha-numeric characters and cannot contain any '0', 'O', 'I' nor 'l'; prefix = %s", addrPrefix))
	}

	// Update miner status
	gossiper.isMinerMutex.Lock()
	if miner {
		gossiper.isMiner = true
	} else {
		gossiper.isMiner = false
	}
	gossiper.isMinerMutex.Unlock()

	go func() {
		gossiper.createNodeMutex.Lock()

		if !gossiper.isTibcoinNode() {

			privateKey, publicKey, err := GeneratePrivateAndPublicKeys(addrPrefix)
			if err != nil {
				panic(err)
			}

			gossiper.keysMutex.Lock()
			gossiper.privateKey = privateKey
			gossiper.publicKey = publicKey
			gossiper.keysMutex.Unlock()

			// Start the routines
			gossiper.StartTibcoinRoutines(miner)
		}

		gossiper.createNodeMutex.Unlock()
	}()

	return nil
}

func (gossiper *Gossiper) createTx(value int, to string) error {
	gossiper.errLogger.Printf("\nClient tx request: %d tibcoins for %s", value, to)
	// Generate transaction
	tx, err := gossiper.NewTx(to, value)
	if err != nil {
		return err
	}

	gossiper.errLogger.Println(tx)

	// Add to transaction pool
	gossiper.addToPool(tx)

	// Broadcast transaction
	return gossiper.broadcastTx(tx)
}

func (gossiper *Gossiper) switchMiningStatus() {
	gossiper.isMinerMutex.Lock()
	if gossiper.isMiner {
		gossiper.isMiner = false
	} else {
		gossiper.isMiner = true
		gossiper.resetBlockMutex.Lock()
		gossiper.resetBlock = true
		gossiper.resetBlockMutex.Unlock()
		go gossiper.Mine()
	}
	gossiper.isMinerMutex.Unlock()
}

func (gossiper *Gossiper) isMinerNodeAPI() bool {
	return gossiper.isMinerNode()
}

type BlockWithHash struct {
	Hash             string
	Timestamp        string
	Height           uint32
	Nonce            uint32
	PrevHash         string
	TransactionsHash string
	Target           string
	Txs              []*TxWithHash
}

type TxWithHash struct {
	Inputs  []*InputWithHash
	Outputs []*TxOutput
	Hash    string
	Address string
}

type InputWithHash struct {
	OutputTxHash string
	OutputIdx    int
}

func (gossiper *Gossiper) getBlockchain() []*BlockWithHash {
	gossiper.topBlockMutex.Lock()
	topBlockHash := gossiper.topBlock
	gossiper.topBlockMutex.Unlock()

	var blocks []*BlockWithHash

	gossiper.blocksMutex.Lock()
	currentBlock, hasNextBlock := gossiper.blocks[topBlockHash]
	for hasNextBlock {
		// Transform txs
		var txs []*TxWithHash
		for _, tx := range currentBlock.Txs {
			txHash := tx.hash()

			var inputs []*InputWithHash
			for _, input := range tx.Inputs {
				inputs = append(inputs, &InputWithHash{
					OutputTxHash: hex.EncodeToString(input.OutputTxHash[:]),
					OutputIdx:    input.OutputIdx,
				})
			}

			txs = append(txs, &TxWithHash{
				Inputs:  inputs,
				Outputs: tx.Outputs,
				Hash:    hex.EncodeToString(txHash[:]),
				Address: PublicKeyToAddress(tx.PublicKey),
			})
		}

		currentHash := currentBlock.hash()
		blocks = append(blocks, &BlockWithHash{
			Timestamp:        time.Unix(currentBlock.Timestamp, 0).String(),
			Hash:             hex.EncodeToString(currentHash[:]),
			Height:           currentBlock.Height,
			Nonce:            currentBlock.Nonce,
			PrevHash:         hex.EncodeToString(currentBlock.PrevHash[:]),
			TransactionsHash: hex.EncodeToString(currentBlock.TransactionsHash[:]),
			Target:           hex.EncodeToString(currentBlock.Target[:]),
			Txs:              txs,
		})
		currentBlock, hasNextBlock = gossiper.blocks[currentBlock.PrevHash]
	}
	gossiper.blocksMutex.Unlock()

	return blocks
}
