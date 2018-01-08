package gossipernode

import (
	"crypto/ecdsa"
	"fmt"
	"log"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

type Packet struct {
	from    *net.UDPAddr
	payload []byte
}

type GossiperPacketSender struct {
	from   *net.UDPAddr
	packet *GossipPacket
}

type PeerHistory struct {
	peerID            string
	lastConsecutiveID uint32
	maxReceivedID     uint32
	messages          map[uint32]*RumorMessage
	privateChat       []*PrivateMessage
}

type Peer struct {
	addr   *net.UDPAddr
	mutex  *sync.Mutex
	timers []*TimerContainer
}

type TimerContainer struct {
	ack   chan (bool)
	timer *time.Timer
	rumor *RumorMessage
}

// TODO use timeouts everywhere
type Timeout struct {
	Ticker *time.Ticker
	Killer chan bool
}

type Gossiper struct {
	name                   string
	nameMutex              *sync.Mutex
	uiConn                 *net.UDPConn
	gossipConn             *net.UDPConn
	guiPort                string
	nextID                 uint32
	messages               map[string]*PeerHistory
	messagesMutex          *sync.Mutex
	globalChat             []*RumorMessage
	globalChatMutex        *sync.Mutex
	peers                  map[string]*Peer
	peersMutex             *sync.Mutex
	stdLogger              *log.Logger
	errLogger              *log.Logger
	routing                map[string]*NextHop
	routingMutex           *sync.Mutex
	rtimer                 *time.Duration
	noforward              bool
	filesMutex             *sync.Mutex
	files                  map[string]*File
	downloadsMutex         *sync.Mutex
	downloads              map[string]*DownloadState
	currentFileSearchMutex *sync.Mutex
	currentFileSearch      *FileSearchRequest
	matchesMutex           *sync.Mutex
	matches                []*FileSearchState
	recentRequestsMutex    *sync.Mutex
	recentReceivedRequests []*ReceivedSearchRequest
	tibcoin 			   bool
	privateKey             *ecdsa.PrivateKey
	publicKey              *PublicKey
	keysMutex			   *sync.Mutex
	topBlock               [32]byte
	topBlockMutex          *sync.Mutex
	blocks                 map[[32]byte]*Block
	blocksMutex            *sync.Mutex
	forks                  map[[32]byte]bool
	forksMutex             *sync.Mutex
	blockOrphanPool        map[[32]byte][32]byte
	blockOrphanPoolMutex   *sync.Mutex
	txPool                 []*Tx
	txPoolMutex            *sync.Mutex
	blockInRequest         map[[32]byte][]*net.UDPAddr
	blockInRequestMutex    *sync.Mutex
	peerNumRequest         map[string]int
	peerNumRequestMutex    *sync.Mutex
	orphanTxPool           []*Tx
	orphanTxPoolMutex      *sync.Mutex
	resetBlock             bool
	resetBlockMutex        *sync.Mutex
	createNodeMutex		   *sync.Mutex
	isMiner				   bool
	isMinerMutex		   *sync.Mutex
	blockRequestChannel	   chan *GossiperPacketSender
	blockReplyChannel	   chan *GossiperPacketSender
	transactionChannel	   chan *GossiperPacketSender
}

func NewGossiper(name string, uiPort string, guiPort string, gossipAddr *net.UDPAddr, peersAddr []*net.UDPAddr, rtimer *time.Duration, noforward bool, tibcoin bool, miner bool) (*Gossiper, error) {
	stdLogger := log.New(os.Stdout, "", 0)
	errLogger := log.New(os.Stderr, fmt.Sprintf("[Gossiper: %s] ", name), log.Ltime|log.Lshortfile)

	uiAddr, err := net.ResolveUDPAddr("udp", LocalIP(uiPort))
	uiConn, err := net.ListenUDP("udp", uiAddr)
	if err != nil {
		return nil, err
	}

	gossipConn, err := net.ListenUDP("udp", gossipAddr)
	if err != nil {
		return nil, err
	}

	peers := make(map[string]*Peer, 0)
	peerNumRequest := make(map[string]int)
	for _, addr := range peersAddr {
		if addr.String() != gossipAddr.String() {
			peers[addr.String()] = &Peer{addr: addr, mutex: &sync.Mutex{}}
			peerNumRequest[addr.String()] = 0
		}
	}

	// Generate public/private key pair
	var privateKey *ecdsa.PrivateKey = nil
	var publicKey *PublicKey = nil
	if tibcoin {
		// By default, don't care about a prefix for address
		privateKey, publicKey, err = GeneratePrivateAndPublicKeys("")
		if err != nil {
			return nil, err
		}
	}

	// Init block chain
	blocks := make(map[[32]byte]*Block)
	genesisHash := GenesisBlock.hash()
	blocks[genesisHash] = GenesisBlock

	forks := make(map[[32]byte]bool)
	forks[genesisHash] = true

	return &Gossiper{
		name:                   name,
		nameMutex:              &sync.Mutex{},
		uiConn:                 uiConn,
		gossipConn:             gossipConn,
		guiPort:                guiPort,
		nextID:                 1,
		messages:               make(map[string]*PeerHistory, 0),
		messagesMutex:          &sync.Mutex{},
		globalChat:             make([]*RumorMessage, 0, 0),
		globalChatMutex:        &sync.Mutex{},
		peers:                  peers,
		peersMutex:             &sync.Mutex{},
		stdLogger:              stdLogger,
		errLogger:              errLogger,
		routing:                make(map[string]*NextHop),
		routingMutex:           &sync.Mutex{},
		rtimer:                 rtimer,
		noforward:              noforward,
		filesMutex:             &sync.Mutex{},
		files:                  make(map[string]*File),
		downloadsMutex:         &sync.Mutex{},
		downloads:              make(map[string]*DownloadState),
		currentFileSearchMutex: &sync.Mutex{},
		recentRequestsMutex:    &sync.Mutex{},
		recentReceivedRequests: make([]*ReceivedSearchRequest, 0, 0),
		matchesMutex:           &sync.Mutex{},
		tibcoin:				tibcoin,
		privateKey:             privateKey,
		publicKey:              publicKey,
		keysMutex:				&sync.Mutex{},
		topBlock:               genesisHash,
		topBlockMutex:          &sync.Mutex{},
		blocks:                 blocks,
		blocksMutex:            &sync.Mutex{},
		forks:                  forks,
		forksMutex:             &sync.Mutex{},
		blockOrphanPool:        make(map[[32]byte][32]byte),
		blockOrphanPoolMutex:   &sync.Mutex{},
		txPool:                 make([]*Tx, 0),
		txPoolMutex:            &sync.Mutex{},
		orphanTxPool:           make([]*Tx, 0),
		orphanTxPoolMutex:      &sync.Mutex{},
		blockInRequest:         make(map[[32]byte][]*net.UDPAddr),
		blockInRequestMutex:    &sync.Mutex{},
		peerNumRequest:         peerNumRequest,
		peerNumRequestMutex:    &sync.Mutex{},
		resetBlock:             true, // Start mining directly
		resetBlockMutex:        &sync.Mutex{},
		createNodeMutex:		&sync.Mutex{},
		isMiner:				miner,
		isMinerMutex:			&sync.Mutex{},
		blockRequestChannel:	make(chan *GossiperPacketSender),
		blockReplyChannel:		make(chan *GossiperPacketSender),
		transactionChannel:		make(chan *GossiperPacketSender),
	}, nil
}

func (gossiper *Gossiper) Start() error {
	clientChannel := make(chan *Packet)
	gossipChannel := make(chan *Packet)
	rumorChannel := make(chan *GossiperPacketSender)
	statusChannel := make(chan *GossiperPacketSender)
	privateChannel := make(chan *GossiperPacketSender)
	dataRequestChannel := make(chan *GossiperPacketSender)
	dataReplyChannel := make(chan *GossiperPacketSender)
	searchRequestChannel := make(chan *GossiperPacketSender)
	searchReplyChannel := make(chan *GossiperPacketSender)

	// Launch webserver
	go gossiper.LaunchWebServer()

	// Listen for UI and gossips
	go gossiper.Listen(gossiper.uiConn, clientChannel)
	go gossiper.Listen(gossiper.gossipConn, gossipChannel)

	// Spawn handler
	go gossiper.GossiperRoutine(gossipChannel, rumorChannel, statusChannel, privateChannel, dataRequestChannel, dataReplyChannel, searchRequestChannel, searchReplyChannel, gossiper.blockRequestChannel, gossiper.blockReplyChannel, gossiper.transactionChannel)
	go gossiper.CLIRoutine(clientChannel)
	go gossiper.RumorRoutine(rumorChannel)
	go gossiper.StatusRoutine(statusChannel)
	go gossiper.PrivateMessageRoutine(privateChannel)
	go gossiper.DataRequestRoutine(dataRequestChannel)
	go gossiper.DataReplyRoutine(dataReplyChannel)
	go gossiper.SearchRequestRoutine(searchRequestChannel)
	go gossiper.SearchReplyRoutine(searchReplyChannel)

	// Spawn anti-antropy
	go gossiper.AntiEntropyRoutine()

	// Spawn route rumoring routine
	go gossiper.RouteRumoringRoutine()

	// Launch tibcoin-related routines if wanted
	if gossiper.tibcoin {
		gossiper.StartTibcoinRoutines(true)
	}

	// TODO: remove
	/*
	// Create new block + hash
	block := GenesisBlock
	target := block.Target
	var nonce uint32 = 0

	for {
		blockHash := block.hash()

		// See if found new valid block
		if bytes.Compare(blockHash[:], target[:]) < 0 {
			// Found block!
			fmt.Printf("[GENESIS] Found new block: %x with nonce = %d.\n", blockHash[:], nonce)
			break
		}

		nonce++

		block = &Block{
			Timestamp: 			GenesisTime,
			Height:    			0,
			Nonce:     			nonce,
			Target:    			BytesToHash(InitialTarget),
			TransactionsHash: 	NilHash,
			PrevHash:  			NilHash,
			Txs:       			make([]*Tx, 0),
		}
	}
	*/

	select {}
}

func (gossiper *Gossiper) Listen(conn *net.UDPConn, channel chan<- *Packet) {
	for {
		// Read from connection
		buffer := make([]byte, 2*ChunkSize)
		bytesReceived, sender, err := conn.ReadFromUDP(buffer)
		if err != nil {
			gossiper.errLogger.Println(err.Error())
			continue
		}
		gossiper.errLogger.Printf("Payload received (%d bytes)", bytesReceived)

		channel <- &Packet{from: sender, payload: buffer}
	}
}

func (gossiper *Gossiper) StartTibcoinRoutines(miner bool) {
	go gossiper.blockRequestRoutine(gossiper.blockRequestChannel)
	go gossiper.blockReplyRoutine(gossiper.blockReplyChannel)
	go gossiper.TxRoutine(gossiper.transactionChannel)
	go gossiper.getInventoryRoutine()

	if miner {
		go gossiper.Mine()
	}
}

// --------------------------------- Helpers -------------------------------- //

func (gossiper *Gossiper) getID() uint32 {
	atomic.AddUint32(&gossiper.nextID, 1)
	return atomic.LoadUint32(&gossiper.nextID) - 1
}

func (gossiper *Gossiper) getHistory(peerID string) *PeerHistory {
	history, in := gossiper.messages[peerID]
	if !in {
		history = &PeerHistory{peerID: peerID, lastConsecutiveID: 0, maxReceivedID: 0, messages: make(map[uint32]*RumorMessage), privateChat: make([]*PrivateMessage, 0)}
		gossiper.messages[peerID] = history
	}

	return history
}

func (gossiper *Gossiper) isTibcoinNode() bool {
	gossiper.keysMutex.Lock()
	isTibcoinNode := gossiper.privateKey != nil && gossiper.publicKey != nil
	gossiper.keysMutex.Unlock()
	return isTibcoinNode
}

func (gossiper *Gossiper) isMinerNode() bool {
	gossiper.isMinerMutex.Lock()
	isMinerNode := gossiper.isMiner
	gossiper.isMinerMutex.Unlock()
	return isMinerNode
}

func KillTimeout(timeout *Timeout) {
	timeout.Ticker.Stop()
}

func ContainsAddress(addr *net.UDPAddr, list []*net.UDPAddr) bool {
	for _, elem := range list {
		if elem.String() == addr.String() {
			return true
		}
	}

	return false
}
