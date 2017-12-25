package gossipernode

import (
	"encoding/hex"
	"fmt"
	"net"
	"strings"

	"github.com/paulnicolet/tibcoin/common"
)

func (gossiper *Gossiper) logSearchReply(reply *common.SearchReply, budget uint64) {
	for _, result := range reply.Results {
		log := fmt.Sprintf("FOUND match %s at %s budget=%d metafile=%s chunks=", result.FileName, reply.Origin, budget, hex.EncodeToString(result.MetafileHash))
		var chunks []string
		for chunk := range result.ChunkMap {
			chunks = append(chunks, fmt.Sprintf("%d", chunk))
		}

		log += strings.Join(chunks, ",")

		gossiper.stdLogger.Println(log)
	}
}

func (gossiper *Gossiper) logStatus(status *common.StatusPacket, relay *net.UDPAddr) {
	result := fmt.Sprintf("STATUS from %s", relay.String())
	for _, peerStatus := range status.Want {
		result += fmt.Sprintf(" origin %s nextID %d", peerStatus.Identifier, peerStatus.NextID)
	}
	result += "\n"
	gossiper.stdLogger.Printf(result)
}

func (gossiper *Gossiper) logPeers() {
	gossiper.peersMutex.Lock()
	defer gossiper.peersMutex.Unlock()

	last := len(gossiper.peers) - 1
	i := 0
	result := ""
	for _, peer := range gossiper.peers {
		result += peer.addr.String()
		if i < last {
			result += ","
		} else {
			result += "\n"
		}
		i++
	}

	gossiper.stdLogger.Printf(result)
}

func (gossiper *Gossiper) logDSDV(dest string, hop *net.UDPAddr) {
	gossiper.stdLogger.Printf("DSDV %s:%s", dest, hop)
}

func (gossiper *Gossiper) logDirectRoute(dest string, hop *net.UDPAddr) {
	gossiper.stdLogger.Printf("DIRECT-ROUTE FOR %s: %v", dest, hop)
}

func (gossiper *Gossiper) logCoinFlip(peer *Peer) {
	gossiper.stdLogger.Printf("FLIPPED COIN sending rumor to %s", peer.addr.String())
}

func (gossiper *Gossiper) logSynced(peer *Peer) {
	gossiper.stdLogger.Printf("IN SYNC WITH %s", peer.addr)
}

func (gossiper *Gossiper) logRumor(rumor *common.RumorMessage, relay *net.UDPAddr) {
	if gossiper.isRoutingRumor(rumor) {
		gossiper.stdLogger.Printf("ROUTING RUMOR origin %s from %s ID %d", rumor.Origin, relay.String(), rumor.ID)
	} else {
		gossiper.stdLogger.Printf("RUMOR origin %s from %s ID %d contents %s\n", rumor.Origin, relay.String(), rumor.ID, rumor.Text)
	}
}

func (gossiper *Gossiper) logRumorMongering(rumor *common.RumorMessage, peer *Peer) {
	if gossiper.isRoutingRumor(rumor) {
		gossiper.logRouteMongering(peer)
	} else {
		gossiper.logTextMongering(peer)
	}
}

func (gossiper *Gossiper) logTextMongering(peer *Peer) {
	gossiper.stdLogger.Printf("MONGERING TEXT with %s", peer.addr.String())
}

func (gossiper *Gossiper) logRouteMongering(peer *Peer) {
	gossiper.stdLogger.Printf("MONGERING ROUTE with %s", peer.addr.String())
}

func (gossiper *Gossiper) logPrivateMessage(privateMessage *common.PrivateMessage) {
	gossiper.stdLogger.Printf("PRIVATE: %s:%d:%s", privateMessage.Origin, privateMessage.HopLimit, privateMessage.Text)
}
