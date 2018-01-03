package gossipernode

import (
	"fmt"
	"net"
	"time"

	"github.com/dedis/protobuf"
)

type NextHop struct {
	Hop    *net.UDPAddr
	Direct bool
}

// -------------------------------- Routines -------------------------------- //

func (gossiper *Gossiper) RouteRumoringRoutine() {
	ticker := time.NewTicker(*gossiper.rtimer)

	for {
		gossiper.nameMutex.Lock()
		rumor := &RumorMessage{Origin: gossiper.name, ID: gossiper.getID()}
		gossiper.nameMutex.Unlock()

		gossiper.storeRumor(rumor)

		gossiper.peersMutex.Lock()

		for _, peer := range gossiper.peers {
			gossiper.logRouteMongering(peer)
			gossiper.sendRumorNoTimeout(rumor, peer)
		}

		gossiper.peersMutex.Unlock()
		<-ticker.C
	}
}

// --------------------------------- Helpers -------------------------------- //

func (gossiper *Gossiper) updateRoutingTable(rumor *RumorMessage, relay *net.UDPAddr) {
	gossiper.messagesMutex.Lock()
	defer gossiper.messagesMutex.Unlock()

	// Get history
	history := gossiper.getHistory(rumor.Origin)

	// Is the route direct ?
	isDirect := false //(rumor.LastIP == nil && rumor.LastPort == nil)

	//gossiper.routingMutex.Lock()
	//prevHop, in := gossiper.routing[rumor.Origin]
	//gossiper.routingMutex.Unlock()

	// Update routing table if greater sequence number
	if rumor.ID > history.maxReceivedID /*|| !in || (rumor.ID == history.maxReceivedID && !prevHop.Direct)*/ {
		history.maxReceivedID = rumor.ID

		gossiper.routingMutex.Lock()
		gossiper.routing[rumor.Origin] = &NextHop{Hop: relay, Direct: isDirect}
		gossiper.routingMutex.Unlock()

		if isDirect {
			gossiper.logDirectRoute(rumor.Origin, relay)
		}

		gossiper.logDSDV(rumor.Origin, relay)
	}
}

func (gossiper *Gossiper) getNextHop(peer string) (*net.UDPAddr, bool) {
	gossiper.routingMutex.Lock()
	defer gossiper.routingMutex.Unlock()

	nextHop, in := gossiper.routing[peer]
	if !in {
		return nil, false
	}

	return nextHop.Hop, true
}

func (gossiper *Gossiper) routePrivatePacket(privatePacket *PrivatePacket) (bool, error) {
	// Decrease hop limit
	privatePacket.HopLimit = privatePacket.HopLimit - 1

	// Process private message if dest is current gossiper
	if privatePacket.Destination == gossiper.name {
		return true, nil
	}

	// Drop if next hop is 0
	if privatePacket.HopLimit == 0 {
		return false, nil
	}

	if gossiper.noforward {
		gossiper.stdLogger.Printf("Not forwarding private message from %s to %s", privatePacket.Origin, privatePacket.Destination)
		return false, nil
	}

	err := gossiper.sendPrivateNoTimeout(privatePacket, privatePacket.Destination)
	gossiper.errLogger.Printf("Forwarding private message %v", privatePacket)
	if err != nil {
		return false, err
	}

	return false, nil
}

func (gossiper *Gossiper) sendPrivateNoTimeout(privatePacket *PrivatePacket, peer string) error {
	packet := gossiper.unpackPrivate(privatePacket)

	// Get next hop
	hop, in := gossiper.getNextHop(peer)
	if !in {
		gossiper.errLogger.Printf("No next for hop %s", peer)
		return fmt.Errorf("No next hop for %s", peer)
	}

	// Mashall message
	buffer, err := protobuf.Encode(packet)
	if err != nil {
		return err
	}

	_, err = gossiper.gossipConn.WriteToUDP(buffer, hop)
	return err
}

func (gossiper *Gossiper) isRoutingRumor(rumor *RumorMessage) bool {
	return rumor.Text == ""
}

func (gossiper *Gossiper) sendRumorNoTimeout(rumor *RumorMessage, peer *Peer) error {
	// Mashall message
	packet := GossipPacket{Rumor: rumor}
	buffer, err := protobuf.Encode(&packet)
	if err != nil {
		return err
	}

	_, err = gossiper.gossipConn.WriteToUDP(buffer, peer.addr)
	return err
}

func (gossiper *Gossiper) packPrivateMessage(msg *PrivateMessage) *PrivatePacket {
	return &PrivatePacket{
		Origin:         msg.Origin,
		Destination:    msg.Destination,
		HopLimit:       msg.HopLimit,
		PrivateMessage: msg,
	}
}

func (gossiper *Gossiper) packPrivateRequest(request *DataRequest) *PrivatePacket {
	return &PrivatePacket{
		Origin:      request.Origin,
		Destination: request.Destination,
		HopLimit:    request.HopLimit,
		DataRequest: request,
	}
}

func (gossiper *Gossiper) packPrivateReply(reply *DataReply) *PrivatePacket {
	return &PrivatePacket{
		Origin:      reply.Origin,
		Destination: reply.Destination,
		HopLimit:    reply.HopLimit,
		DataReply:   reply,
	}
}

func (gossiper *Gossiper) packPrivateSearchReply(reply *SearchReply) *PrivatePacket {
	return &PrivatePacket{
		Origin:      reply.Origin,
		Destination: reply.Destination,
		HopLimit:    reply.HopLimit,
		SearchReply: reply,
	}
}

func (gossiper *Gossiper) unpackPrivate(privatePacket *PrivatePacket) *GossipPacket {
	var packet GossipPacket
	if privatePacket.PrivateMessage != nil {
		privatePacket.PrivateMessage.HopLimit = privatePacket.HopLimit
		packet = GossipPacket{Private: privatePacket.PrivateMessage}
	} else if privatePacket.DataRequest != nil {
		privatePacket.DataRequest.HopLimit = privatePacket.HopLimit
		packet = GossipPacket{DataRequest: privatePacket.DataRequest}
	} else if privatePacket.DataReply != nil {
		privatePacket.DataReply.HopLimit = privatePacket.HopLimit
		packet = GossipPacket{DataReply: privatePacket.DataReply}
	} else {
		privatePacket.SearchReply.HopLimit = privatePacket.HopLimit
		packet = GossipPacket{SearchReply: privatePacket.SearchReply}
	}

	return &packet
}
