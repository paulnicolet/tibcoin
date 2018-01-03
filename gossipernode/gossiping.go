package gossipernode

import (
	"math/rand"
	"net"
	"sync"
	"time"

	"github.com/dedis/protobuf"
)

// -------------------------------- Routines -------------------------------- //

func (gossiper *Gossiper) AntiEntropyRoutine() {
	ticker := time.NewTicker(30 * time.Second)

	for {
		<-ticker.C

		peer := gossiper.pickNextPeer(nil)

		if peer != nil {
			// Send status
			packet := GossipPacket{Status: gossiper.getStatus()}

			buffer, err := protobuf.Encode(&packet)
			if err != nil {
				continue
			}

			gossiper.gossipConn.WriteToUDP(buffer, peer.addr)
		}
	}
}

func (gossiper *Gossiper) GossiperRoutine(
	gossipChannel <-chan *Packet,
	rumorChannel chan<- *GossiperPacketSender,
	statusChannel chan<- *GossiperPacketSender,
	privateChannel chan<- *GossiperPacketSender,
	dataRequestChannel chan<- *GossiperPacketSender,
	dataReplyChannel chan<- *GossiperPacketSender,
	searchRequestChannel chan<- *GossiperPacketSender,
	searchReplyChannel chan<- *GossiperPacketSender,
	blockRequestChannel chan<- *GossiperPacketSender,
	blockReplyChannel chan<- *GossiperPacketSender) {
	for {
		packet := <-gossipChannel

		// Decode message
		gossipPacket := GossipPacket{}
		err := protobuf.Decode(packet.payload, &gossipPacket)
		if err != nil {
			// gossiper.errLogger.Printf("Error decoding message: %v\n", err)
			// continue
		}

		// Send arriving packet to the right routine
		if gossipPacket.Rumor != nil && gossipPacket.Status == nil && gossipPacket.Private == nil {
			rumorChannel <- &GossiperPacketSender{from: packet.from, packet: &gossipPacket}
		} else if gossipPacket.Rumor == nil && gossipPacket.Status != nil && gossipPacket.Private == nil {
			statusChannel <- &GossiperPacketSender{from: packet.from, packet: &gossipPacket}
		} else if gossipPacket.Rumor == nil && gossipPacket.Status == nil && gossipPacket.Private != nil {
			privateChannel <- &GossiperPacketSender{from: packet.from, packet: &gossipPacket}
		} else if gossipPacket.DataRequest != nil {
			dataRequestChannel <- &GossiperPacketSender{from: packet.from, packet: &gossipPacket}
		} else if gossipPacket.DataReply != nil {
			dataReplyChannel <- &GossiperPacketSender{from: packet.from, packet: &gossipPacket}
		} else if gossipPacket.SearchRequest != nil {
			searchRequestChannel <- &GossiperPacketSender{from: packet.from, packet: &gossipPacket}
		} else if gossipPacket.SearchReply != nil {
			searchReplyChannel <- &GossiperPacketSender{from: packet.from, packet: &gossipPacket}
		} else if gossipPacket.BlockRequest != nil {
			blockRequestChannel <- &GossiperPacketSender{from: packet.from, packet: &gossipPacket}
		} else if gossipPacket.BlockReply != nil {
			blockReplyChannel <- &GossiperPacketSender{from: packet.from, packet: &gossipPacket}
		} else {
			gossiper.errLogger.Println(packet.from)
			gossiper.errLogger.Println("Invalid packet")
		}
	}
}

func (gossiper *Gossiper) RumorRoutine(channel <-chan *GossiperPacketSender) {
	for {
		packet := <-channel
		err := gossiper.handleRumor(packet)
		if err != nil {
			gossiper.errLogger.Printf("Error processing the rumor: %v", err)
		}
	}
}

func (gossiper *Gossiper) StatusRoutine(channel <-chan *GossiperPacketSender) {
	for {
		packet := <-channel
		err := gossiper.handleStatus(packet)
		if err != nil {
			gossiper.errLogger.Printf("Error processing the status: %v", err)
		}
	}
}

// ---------------------------- Message handlers ---------------------------- //

func (gossiper *Gossiper) handleRumor(rumorPacket *GossiperPacketSender) error {
	relay := rumorPacket.from
	rumor := rumorPacket.packet.Rumor

	gossiper.logRumor(rumor, relay)
	gossiper.logPeers()

	// Add peer
	gossiper.addPeer(relay)

	// Add previous relay (to punch hole)
	if rumor.LastIP != nil && rumor.LastPort != nil {
		//previousRelay := net.UDPAddr{IP: *rumor.LastIP, Port: *rumor.LastPort}
		//gossiper.addPeer(&previousRelay)
	}

	// Store rumor if new
	isNew := gossiper.storeRumor(rumor)

	// Update routing table
	gossiper.updateRoutingTable(rumor, relay)

	// Ack with status
	if !gossiper.isRoutingRumor(rumor) {
		packet := GossipPacket{Status: gossiper.getStatus()}
		buffer, err := protobuf.Encode(&packet)
		if err != nil {
			return err
		}

		_, err = gossiper.gossipConn.WriteToUDP(buffer, relay)
		if err != nil {
			return err
		}
	}

	// Update LastIP and LastPort
	rumor.LastIP = &relay.IP
	rumor.LastPort = &relay.Port

	// Send rumor if new
	if isNew {
		if gossiper.canForward(rumor) {
			if gossiper.isRoutingRumor(rumor) {
				gossiper.peersMutex.Lock()

				for _, peer := range gossiper.peers {
					gossiper.logRouteMongering(peer)
					gossiper.sendRumorNoTimeout(rumor, peer)
				}

				gossiper.peersMutex.Unlock()
			} else {
				peer := gossiper.pickNextPeer([]*net.UDPAddr{relay})
				if peer != nil {
					gossiper.logTextMongering(peer)
					gossiper.sendRumorWithTimeout(rumor, peer)
				}
			}
		} else {
			gossiper.stdLogger.Printf("Not forwarding rumor from %s ID %d", rumor.Origin, rumor.ID)
		}
	}

	return nil
}

func (gossiper *Gossiper) handleStatus(statusPacket *GossiperPacketSender) error {
	relay := statusPacket.from
	status := statusPacket.packet.Status

	gossiper.logStatus(status, relay)
	gossiper.logPeers()

	// Add peer
	gossiper.addPeer(relay)

	// Get peer
	gossiper.peersMutex.Lock()
	peer := gossiper.peers[relay.String()]
	gossiper.peersMutex.Unlock()

	// Is it an ack ?
	rumor := gossiper.processAck(peer, status)
	ack := rumor != nil

	// Check if sender miss some message
	missing := gossiper.getPeerMissingRumor(status)
	if missing != nil && gossiper.canForward(missing) {
		if ack {
			// It's an ack, keep mongering
			newPeer := gossiper.pickNextPeer([]*net.UDPAddr{relay})
			if newPeer != nil {
				gossiper.logRumorMongering(rumor, newPeer)
				gossiper.sendRumorWithTimeout(rumor, newPeer)
			}
		}

		return gossiper.sendRumorWithTimeout(missing, peer)
	}

	// If not, if we miss message, send a status message
	if gossiper.rumorIsMissing(status) {

		if ack && gossiper.canForward(rumor) {
			// It's an ack, keep mongering
			newPeer := gossiper.pickNextPeer([]*net.UDPAddr{relay})
			if newPeer != nil {
				gossiper.logRumorMongering(rumor, newPeer)
				gossiper.sendRumorWithTimeout(rumor, newPeer)
			}
		}

		buffer, err := protobuf.Encode(&GossipPacket{Status: gossiper.getStatus()})
		if err != nil {
			return err
		}

		_, err = gossiper.gossipConn.WriteToUDP(buffer, relay)
		if err != nil {
			return err
		}

		return nil
	}

	gossiper.logSynced(peer)

	if ack && gossiper.canForward(rumor) && rand.Int()%2 == 0 {
		newPeer := gossiper.pickNextPeer([]*net.UDPAddr{relay})

		if newPeer != nil {
			gossiper.logCoinFlip(newPeer)

			gossiper.sendRumorWithTimeout(rumor, newPeer)
		}
	}

	return nil
}

func (gossiper *Gossiper) handleTimeout(peer *Peer, timer *time.Timer) {
	// Make sure the ack did not arrive in the meantime
	peer.mutex.Lock()
	var rumor *RumorMessage
	for i, timerContainer := range peer.timers {
		if timerContainer.timer == timer {
			timer.Stop()
			rumor = timerContainer.rumor
			peer.timers = append(peer.timers[:i], peer.timers[i+1:]...)
			break
		}
	}
	peer.mutex.Unlock()

	if rumor == nil {
		return
	}

	// Send rumor to someone else maybe
	if rand.Int()%2 == 0 {
		newPeer := gossiper.pickNextPeer([]*net.UDPAddr{peer.addr})

		if newPeer != nil {
			gossiper.logCoinFlip(newPeer)
			gossiper.sendRumorWithTimeout(rumor, newPeer)
		}
	}
}

// --------------------------------- Helpers -------------------------------- //

func (gossiper *Gossiper) addPeer(peerAddr *net.UDPAddr) {
	if peerAddr.String() == gossiper.gossipConn.LocalAddr().String() {
		return
	}

	gossiper.peersMutex.Lock()
	defer gossiper.peersMutex.Unlock()

	// Add peer if not known
	relay := peerAddr.String()
	if _, in := gossiper.peers[relay]; !in {
		gossiper.peers[relay] = &Peer{addr: peerAddr, mutex: &sync.Mutex{}}
	}
}

func (gossiper *Gossiper) pickNextPeer(blackList []*net.UDPAddr) *Peer {
	gossiper.peersMutex.Lock()
	defer gossiper.peersMutex.Unlock()

	if len(gossiper.peers) == 0 {
		// No peer
		return nil
	}

	maxTrials := 10
	trials := 0
	for trials < maxTrials {
		// Pick peer
		idx := rand.Intn(len(gossiper.peers))
		i := 0

		for _, peer := range gossiper.peers {
			peer.mutex.Lock()
			if i == idx && !ContainsAddress(peer.addr, blackList) {
				peer.mutex.Unlock()
				return peer
			}
			peer.mutex.Unlock()
			i++
		}

		trials++
	}

	return nil
}

func (gossiper *Gossiper) sendRumorWithTimeout(rumor *RumorMessage, peer *Peer) error {
	// Mashall message
	packet := GossipPacket{Rumor: rumor}
	buffer, err := protobuf.Encode(&packet)
	if err != nil {
		return err
	}

	// Set current rumor to be able to send it again in case of timeout or sync
	peer.mutex.Lock()

	// Send
	_, err = gossiper.gossipConn.WriteToUDP(buffer, peer.addr)
	if err != nil {
		peer.mutex.Unlock()
		return err
	}

	// Launch timer
	timer := time.NewTimer(time.Second)

	timerContainer := TimerContainer{ack: make(chan bool), timer: timer, rumor: rumor}
	peer.timers = append(peer.timers, &timerContainer)

	go func(peer *Peer, timer *time.Timer, ack <-chan (bool)) {
		select {
		case <-ack:
			return
		case <-timer.C:
			gossiper.handleTimeout(peer, timer)
		}
	}(peer, timer, timerContainer.ack)

	peer.mutex.Unlock()

	return nil
}

func (gossiper *Gossiper) storeRumor(rumor *RumorMessage) bool {
	// Lock the messages map
	gossiper.messagesMutex.Lock()
	defer gossiper.messagesMutex.Unlock()

	// Get peer history
	history := gossiper.getHistory(rumor.Origin)

	// If not new, can return
	if _, in := history.messages[rumor.ID]; in {
		return false
	}

	// Store rumor
	history.messages[rumor.ID] = rumor

	if rumor.Text != "" {
		gossiper.globalChatMutex.Lock()
		gossiper.globalChat = append(gossiper.globalChat, rumor)
		gossiper.globalChatMutex.Unlock()
	}

	// Update last ID received
	_, in := history.messages[history.lastConsecutiveID+1]
	for in {
		history.lastConsecutiveID += 1
		_, in = history.messages[history.lastConsecutiveID+1]
	}

	// Store updated history
	gossiper.messages[rumor.Origin] = history

	return true
}

func (gossiper *Gossiper) rumorIsMissing(status *StatusPacket) bool {
	gossiper.messagesMutex.Lock()
	defer gossiper.messagesMutex.Unlock()

	for _, peerStatus := range status.Want {
		history, in := gossiper.messages[peerStatus.Identifier]

		if peerStatus.NextID != 1 && (!in || history.lastConsecutiveID < peerStatus.NextID-1) {
			return true
		}
	}

	return false
}

func (gossiper *Gossiper) getPeerMissingRumor(status *StatusPacket) *RumorMessage {
	// Build a map out of the status
	statusesMap := make(map[string]uint32)
	for _, peerStatus := range status.Want {
		statusesMap[peerStatus.Identifier] = peerStatus.NextID
	}

	gossiper.messagesMutex.Lock()
	defer gossiper.messagesMutex.Unlock()

	// Find a missing rumor
	for peer, history := range gossiper.messages {
		nextID, in := statusesMap[peer]

		// Peer does not have any message from the other peer
		if !in {
			return history.messages[1]
		}

		// Return next if we have it
		if nextID <= history.lastConsecutiveID {
			return history.messages[nextID]
		}
	}

	return nil
}

func (gossiper *Gossiper) getStatus() *StatusPacket {
	gossiper.messagesMutex.Lock()
	defer gossiper.messagesMutex.Unlock()

	var status []PeerStatus
	for name, history := range gossiper.messages {
		status = append(status, PeerStatus{Identifier: name, NextID: history.lastConsecutiveID + 1})
	}

	return &StatusPacket{Want: status}
}

func (gossiper *Gossiper) processAck(peer *Peer, status *StatusPacket) *RumorMessage {
	peer.mutex.Lock()
	defer peer.mutex.Unlock()

	for _, peerStatus := range status.Want {
		for i := len(peer.timers) - 1; i >= 0; i-- {
			timer := peer.timers[i]
			if timer.rumor.ID+1 == peerStatus.NextID {
				// Stop timer
				if timer.timer.Stop() {
					timer.ack <- true
				}
				peer.timers = append(peer.timers[:i], peer.timers[i+1:]...)
				return timer.rumor
			}
		}
	}

	return nil
}

func (gossiper *Gossiper) canForward(rumor *RumorMessage) bool {
	return !gossiper.noforward || (gossiper.noforward && gossiper.isRoutingRumor(rumor))
}
