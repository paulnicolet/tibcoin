package gossipernode

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
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
	privateKey             *ecdsa.PrivateKey
	publicKey              *PublicKey
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

	blockInRequest      map[[32]byte][]*net.UDPAddr
	blockInRequestMutex *sync.Mutex
	peerNumRequest      map[*net.UDPAddr]int
	peerNumRequestMutex *sync.Mutex

	orphanTxPool      []*Tx
	orphanTxPoolMutex *sync.Mutex
	target            [32]byte    // TODO: remove and check in last block for target
	targetMutex       *sync.Mutex // TODO: remove

	miningChannel chan bool
}

func NewGossiper(name string, uiPort string, guiPort string, gossipAddr *net.UDPAddr, peersAddr []*net.UDPAddr, rtimer *time.Duration, noforward bool) (*Gossiper, error) {
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
	for _, addr := range peersAddr {
		if addr.String() != gossipAddr.String() {
			peers[addr.String()] = &Peer{addr: addr, mutex: &sync.Mutex{}}
		}
	}

	// Generate public/private key pair
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	publicKey := &PublicKey{
		X: privateKey.PublicKey.X,
		Y: privateKey.PublicKey.Y,
	}

	// Init first block
	blocks := make(map[[32]byte]*Block)
	genesisHash := GenesisBlock.hash()
	blocks[genesisHash] = GenesisBlock

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
		privateKey:             privateKey,
		publicKey:              publicKey,
		topBlock:               genesisHash,
		topBlockMutex:          &sync.Mutex{},
		blocks:                 blocks,
		blocksMutex:            &sync.Mutex{},
		forks:                  make(map[[32]byte]bool),
		forksMutex:             &sync.Mutex{},
		blockOrphanPool:        make(map[[32]byte][32]byte),
		blockOrphanPoolMutex:   &sync.Mutex{},
		txPool:                 make([]*Tx, 0),
		txPoolMutex:            &sync.Mutex{},
		orphanTxPool:           make([]*Tx, 0),
		orphanTxPoolMutex:      &sync.Mutex{},
		target:                 BytesToHash(InitialTarget), // TODO: remove and check in last block for target
		targetMutex:            &sync.Mutex{},              // TODO: remove and check in last block for target
		miningChannel:          make(chan bool),
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
	blockRequestChannel := make(chan *GossiperPacketSender)
	blockReplyChannel := make(chan *GossiperPacketSender)
	transactionChannel := make(chan *GossiperPacketSender)

	// Launch webserver
	go gossiper.LaunchWebServer()

	// Listen for UI and gossips
	go gossiper.Listen(gossiper.uiConn, clientChannel)
	go gossiper.Listen(gossiper.gossipConn, gossipChannel)

	// Spawn handler
	go gossiper.GossiperRoutine(gossipChannel, rumorChannel, statusChannel, privateChannel, dataRequestChannel, dataReplyChannel, searchRequestChannel, searchReplyChannel, blockRequestChannel, blockReplyChannel, transactionChannel)
	go gossiper.CLIRoutine(clientChannel)
	go gossiper.RumorRoutine(rumorChannel)
	go gossiper.StatusRoutine(statusChannel)
	go gossiper.PrivateMessageRoutine(privateChannel)
	go gossiper.DataRequestRoutine(dataRequestChannel)
	go gossiper.DataReplyRoutine(dataReplyChannel)
	go gossiper.SearchRequestRoutine(searchRequestChannel)
	go gossiper.SearchReplyRoutine(searchReplyChannel)
	go gossiper.blockRequestRoutine(blockRequestChannel)
	go gossiper.blockReplyRoutine(blockReplyChannel)
	go gossiper.TxRoutine(transactionChannel)

	// Spawn anti-antropy
	go gossiper.AntiEntropyRoutine()

	// Spawn route rumoring routine
	go gossiper.RouteRumoringRoutine()

	// Miner
	go gossiper.Mine()

	add := PublicKeyToAddress(gossiper.publicKey)
	gossiper.errLogger.Printf("Tibcoin address %s\n", add)

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
				Timestamp: time.Date(2018, 1, 3, 11, 00, 00, 00, time.UTC).Unix(),
				Height:    0,
				Nonce:     nonce,
				Target:    BytesToHash(InitialTarget),
				PrevHash:  NilHash,
				Txs:       make([]*Transaction, 0),
			}

		}
	*/

	// TODO: Do this to start mining. Should we only start when we are up to date and have all the blocks?
	gossiper.miningChannel <- true

	select {}
}

func (gossiper *Gossiper) Listen(conn *net.UDPConn, channel chan<- *Packet) {
	for {
		// Read from connection
		buffer := make([]byte, 2*ChunkSize)
		_, sender, err := conn.ReadFromUDP(buffer)
		if err != nil {
			gossiper.errLogger.Println(err.Error())
			continue
		}
		channel <- &Packet{from: sender, payload: buffer}
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
