package gossipernode

import (
	"net"
)

const (
	Localhost              = "127.0.0.1"
	FilePath               = "./files/"
	DownloadPath           = "./hw3/_Downloads/"
	ChunkSize              = 8 * 1024
	HashSize               = 32
	HopLimit               = 10
	FileSearchRepeatDelay  = 2
	DataRequestRepeatDelay = 5
	MaxBudget              = 32
	MatchThreshold         = 2
	RecentRequestAgeMs     = 500
	DefaultBudget          = 4
)

type GossipPacket struct {
	Rumor         *RumorMessage
	Status        *StatusPacket
	Private       *PrivateMessage
	DataRequest   *DataRequest
	DataReply     *DataReply
	SearchRequest *SearchRequest
	SearchReply   *SearchReply
	BlockRequest  *BlockRequest
	BlockReply    *BlockReply
	Tx            *SerializableTx
}

type File struct {
	Name     string
	MetaFile []byte
	MetaHash []byte
	Content  map[uint64][]byte
}

type DataRequest struct {
	Origin      string
	Destination string
	HopLimit    uint32
	FileName    string
	HashValue   []byte
}

type DataReply struct {
	Origin      string
	Destination string
	HopLimit    uint32
	FileName    string
	HashValue   []byte
	Data        []byte
}

type SearchRequest struct {
	Origin   string
	Budget   uint64
	Keywords []string
}

type SearchReply struct {
	Origin      string
	Destination string
	HopLimit    uint32
	Results     []*SearchResult
}

type SearchResult struct {
	FileName     string
	MetafileHash []byte
	ChunkMap     []uint64
}

/* Block chain struct routine */

type BlockRequest struct {
	Origin        string
	BlockHash     [32]byte
	WaitingInv    bool
	CurrentHeight uint32
}

type BlockReply struct {
	Origin     string
	Hash       [32]byte
	Block      *SerializableBlock
	BlocksHash [][32]byte
}

/* END */

type RumorMessage struct {
	Origin   string
	ID       uint32
	Text     string
	LastIP   *net.IP
	LastPort *int
}

type PeerStatus struct {
	Identifier string
	NextID     uint32
}

type StatusPacket struct {
	Want []PeerStatus
}

type PrivateMessage struct {
	Origin      string
	Destination string
	HopLimit    uint32
	ID          uint32
	Text        string
}

type PrivatePacket struct {
	Origin         string
	Destination    string
	HopLimit       uint32
	PrivateMessage *PrivateMessage
	DataRequest    *DataRequest
	DataReply      *DataReply
	SearchReply    *SearchReply
	BlockRequest   *BlockRequest
	BlockReply     *BlockReply
}

// Client structs
type ClientPacket struct {
	NewMessage        *NewMessage
	NewName           *NewNameMessage
	NewPeer           *NewPeerMessage
	NewPrivateMessage *NewPrivateMessage
	NewFile           *NewFile
	DownloadRequest   *DownloadRequest
	NewSearchRequest  *NewSearchRequest
	NewTx             *NewTx
}

type NewSearchRequest struct {
	Keywords []string
	Budget   uint64
}

type ClientUpdatePacket struct {
	ID                string
	NewRumor          *RumorMessage
	NewPeer           string
	NewPrivateMessage *PrivateMessage
	NewDestination    string
}

type NewMessage struct {
	Message string
}

type NewFile struct {
	FileName string
}

type NewPrivateMessage struct {
	Message string
	Dest    string
}

type NewNameMessage struct {
	Name string
}

type NewPeerMessage struct {
	Address string
}

type DownloadRequest struct {
	FileName string
	MetaHash []byte
}

type NewTx struct {
	To    string
	Value int
}

func LocalIP(port string) string {
	return Localhost + ":" + port
}
