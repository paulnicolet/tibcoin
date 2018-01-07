package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/paulnicolet/tibcoin/gossipernode"
)

const DEFAULT_RTIMER = "60"
const DEFAULT_GUI_PORT = "8080"

func main() {
	logger := log.New(os.Stderr, "[Gossiper] ", log.Ltime|log.Lshortfile)

	name, uiPort, guiPort, gossipAddr, peers, rtimer, noforward, tibcoin, err := parseInput(os.Args)
	if err != nil {
		logger.Fatal(err)
	}

	gossiper, err := gossipernode.NewGossiper(name, uiPort, guiPort, gossipAddr, peers, rtimer, noforward, tibcoin)
	if err != nil {
		logger.Fatal(err)
	}

	err = gossiper.Start()
}

func parseInput(args []string) (string, string, string, *net.UDPAddr, []*net.UDPAddr, *time.Duration, bool, bool, error) {
	name := flag.String("name", "", "Gossiper's name")
	uiPort := flag.String("UIPort", "", "Port listening for message from CLI client.")
	guiPort := flag.String("GUIPort", "", "Port used by webserver for GUI.")
	gossipPort := flag.String("gossipAddr", "", "Port or address listening for message from other gossipers.")
	peers := flag.String("peers", "", "List of connected peers of the form <ip>:<port>, separated by commas.")
	rtimer := flag.String("rtimer", DEFAULT_RTIMER, "Interval between two routing rumor messages in second.")
	noforward := flag.Bool("noforward", false, "Indicate if the gossiper should forward rumors.")
	tibcoin := flag.Bool("tibcoin", false, "Indicate if we should generate a new private/public key pair for joining the tibcoin network.")

	flag.Parse()

	// Check missing flags
	if *name == "" || *uiPort == "" || *gossipPort == "" {
		return "", "", "", nil, nil, nil, false, false, fmt.Errorf("Missing command line arguments, please see -help for format specifications.")
	}

	// Check listening port format
	if _, err := strconv.Atoi(*uiPort); err != nil {
		return "", "", "", nil, nil, nil, false, false, fmt.Errorf("Invalid UI port number: %s", *uiPort)
	}

	if *guiPort == "" {
		*guiPort = DEFAULT_GUI_PORT
	} else if _, err := strconv.Atoi(*guiPort); err != nil {
		return "", "", "", nil, nil, nil, false, false, fmt.Errorf("Invalid gui port number: %s", *guiPort)
	}

	// Convert gossipPort to address if necessary
	if len(strings.Split(*gossipPort, ":")) == 1 {
		*gossipPort = gossipernode.LocalIP(*gossipPort)
	}

	gossipAddr, err := net.ResolveUDPAddr("udp", *gossipPort)
	if err != nil {
		return "", "", "", nil, nil, nil, false, false, err
	}

	// Convert to duration
	timerDuration, err := time.ParseDuration(*rtimer + "s")
	if err != nil {
		return "", "", "", nil, nil, nil, false, false, fmt.Errorf("Invalid duration (in second): %s", *rtimer)
	}

	// Create peers map
	var addresses []*net.UDPAddr
	if *peers != "" {
		for _, node := range strings.Split(*peers, ",") {
			addr, err := net.ResolveUDPAddr("udp", node)
			if err != nil {
				return "", "", "", nil, nil, nil, false, false, err
			}

			addresses = append(addresses, addr)
		}
	}

	return *name, *uiPort, *guiPort, gossipAddr, addresses, &timerDuration, *noforward, *tibcoin, nil
}
