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

	"github.com/paulnicolet/tibcoin/blockchain"
	"github.com/paulnicolet/tibcoin/common"
)

type Packet struct {
	from    *net.UDPAddr
	payload []byte
}

type GossiperPacketSender struct {
	from   *net.UDPAddr
	packet *common.GossipPacket
}

type PeerHistory struct {
	peerID            string
	lastConsecutiveID uint32
	maxReceivedID     uint32
	messages          map[uint32]*common.RumorMessage
	privateChat       []*common.PrivateMessage
}

type Peer struct {
	addr   *net.UDPAddr
	mutex  *sync.Mutex
	timers []*TimerContainer
}

type TimerContainer struct {
	ack   chan (bool)
	timer *time.Timer
	rumor *common.RumorMessage
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
	globalChat             []*common.RumorMessage
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
	files                  map[string]*common.File
	downloadsMutex         *sync.Mutex
	downloads              map[string]*DownloadState
	currentFileSearchMutex *sync.Mutex
	currentFileSearch      *FileSearchRequest
	matchesMutex           *sync.Mutex
	matches                []*FileSearchState
	recentRequestsMutex    *sync.Mutex
	recentReceivedRequests []*ReceivedSearchRequest
	privateKey             *ecdsa.PrivateKey

	topBlock             []byte
	topBlockMutex        *sync.Mutex
	blocks               map[[]byte]*Block
	blocksMutex          *sync.Mutex
	forks                [][]byte
	forksMutex           *sync.Mutex
	blockOrphanPool      [][]byte
	blockOrphanPoolMutex *sync.Mutex
	txPool               []*Transaction
	txPoolMutex          *sync.Mutex
}

func NewGossiper(name string, uiPort string, guiPort string, gossipAddr *net.UDPAddr, peersAddr []*net.UDPAddr, rtimer *time.Duration, noforward bool) (*Gossiper, error) {
	stdLogger := log.New(os.Stdout, "", 0)
	errLogger := log.New(os.Stderr, fmt.Sprintf("[Gossiper: %s] ", name), log.Ltime|log.Lshortfile)

	uiAddr, err := net.ResolveUDPAddr("udp", common.LocalIP(uiPort))
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
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}

	return &Gossiper{
		name:                   name,
		nameMutex:              &sync.Mutex{},
		uiConn:                 uiConn,
		gossipConn:             gossipConn,
		guiPort:                guiPort,
		nextID:                 1,
		messages:               make(map[string]*PeerHistory, 0),
		messagesMutex:          &sync.Mutex{},
		globalChat:             make([]*common.RumorMessage, 0, 0),
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
		files:                  make(map[string]*common.File),
		downloadsMutex:         &sync.Mutex{},
		downloads:              make(map[string]*DownloadState),
		currentFileSearchMutex: &sync.Mutex{},
		recentRequestsMutex:    &sync.Mutex{},
		recentReceivedRequests: make([]*ReceivedSearchRequest, 0, 0),
		matchesMutex:           &sync.Mutex{},
		privateKey:             key,
		topBlock:               blockChain.HashGenesis, // TODO compute it before
		topBlockMutex:          &sync.Mutex{},
		blocks:                 make(map[[]byte]*Block), // TODO init with Genesis inside
		blocksMutex:            &sync.Mutex{},
		forks:                  make([][]byte, 0),
		forksMutex:             &sync.Mutex{},
		blockOrphanPool:        make([][]byte, 0),
		blockOrphanPoolMutex:   &sync.Mutex{},
		txPool:                 make([]*Transaction, 0),
		txPoolMutex:            &sync.Mutex{},
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

	// Launch webserver
	go gossiper.LaunchWebServer()

	// Listen for UI and gossips
	go gossiper.Listen(gossiper.uiConn, clientChannel)
	go gossiper.Listen(gossiper.gossipConn, gossipChannel)

	// Spawn handler
	go gossiper.GossiperRoutine(gossipChannel, rumorChannel, statusChannel, privateChannel, dataRequestChannel, dataReplyChannel, searchRequestChannel, searchReplyChannel, blockRequestChannel, blockReplyChannel)
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

	select {}
}

func (gossiper *Gossiper) Listen(conn *net.UDPConn, channel chan<- *Packet) {
	for {
		// Read from connection
		buffer := make([]byte, 2*common.ChunkSize)
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
		history = &PeerHistory{peerID: peerID, lastConsecutiveID: 0, maxReceivedID: 0, messages: make(map[uint32]*common.RumorMessage), privateChat: make([]*common.PrivateMessage, 0)}
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
