package main

import (
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"

	"github.com/dedis/protobuf"
	"github.com/paulnicolet/tibcoin/gossipernode"
)

const DEFAULT_UI_PORT = "10001"
const DEFAULT_BUDGET = 2

func main() {
	// Create new logger
	logger := log.New(os.Stderr, "[Client] ", log.Ltime|log.Lshortfile)

	// Parse arguments
	port, msg, dest, file, keywords, budget, request, address, tibcoin, err := parseInput(os.Args)
	if err != nil {
		logger.Fatal(err)
	}

	// Resolve local gossiper address
	gossiperAddr, err := net.ResolveUDPAddr("udp", gossipernode.LocalIP(port))
	if err != nil {
		logger.Fatal(err)
	}

	// Create local UDP connection
	conn, err := net.DialUDP("udp", nil, gossiperAddr)
	if err != nil {
		logger.Fatal(err)
	}

	defer conn.Close()

	// Marshall and send message
	var packet gossipernode.ClientPacket
	if dest != "" && file == "" {
		packet = gossipernode.ClientPacket{NewPrivateMessage: &gossipernode.NewPrivateMessage{Dest: dest, Message: msg}}
	} else if file != "" && request == nil {
		packet = gossipernode.ClientPacket{NewFile: &gossipernode.NewFile{FileName: file}}
	} else if len(keywords) != 0 {
		packet = gossipernode.ClientPacket{NewSearchRequest: &gossipernode.NewSearchRequest{Keywords: keywords, Budget: budget}}
	} else if file != "" && request != nil {
		packet = gossipernode.ClientPacket{DownloadRequest: &gossipernode.DownloadRequest{
			FileName: file,
			MetaHash: request,
		}}
	} else if address != "" && tibcoin != 0 {
		packet = gossipernode.ClientPacket{NewTx: &gossipernode.NewTx{
			To:    address,
			Value: int(tibcoin),
		}}
	} else {
		packet = gossipernode.ClientPacket{NewMessage: &gossipernode.NewMessage{Message: msg}}
	}
	buffer, err := protobuf.Encode(&packet)
	if err != nil {
		logger.Fatal(err)
	}

	if _, err := conn.Write(buffer); err != nil {
		logger.Fatal(err)
	}
}

func parseInput(args []string) (string, string, string, string, []string, uint64, []byte, string, uint64, error) {
	uiPort := flag.String("UIPort", "", "Port on which node is listening for message from client.")
	msg := flag.String("msg", "", "Message to gossip.")
	dest := flag.String("Dest", "", "Name of the destination if for a private message.")
	file := flag.String("file", "", "Name of the file to share: file should be in the \"/files\" directory")
	keywords := flag.String("keywords", "", "Keywords of words to look for, separated by commas")
	budget := flag.Int64("budget", DEFAULT_BUDGET, "Budget of the file search request")
	request := flag.String("request", "", "Metahash of file to download")

	address := flag.String("address", "", "Tibcoin public address")
	tibcoin := flag.Int64("tibcoin", 0, "Amount of money to send")

	flag.Parse()

	if *msg == "" && *file == "" && *keywords == "" && *address == "" {
		return "", "", "", "", nil, 0, nil, "", 0, fmt.Errorf("Missing command line arguments, please see -help for format specifications.")
	}

	if *uiPort == "" {
		fmt.Printf("Using default UI Port: %s", DEFAULT_UI_PORT)
		*uiPort = DEFAULT_UI_PORT
	}

	// Check local UDP peer port
	if _, err := strconv.Atoi(*uiPort); err != nil {
		return "", "", "", "", nil, 0, nil, "", 0, fmt.Errorf("Invalid port number: %s", *uiPort)
	}

	// Split keywords
	var keywordsArray []string
	if *keywords != "" {
		keywordsArray = strings.Split(*keywords, ",")
	}

	// Encoding request
	var metahash []byte
	if *request != "" {
		hash, err := hex.DecodeString(*request)
		metahash = hash
		if err != nil {
			return "", "", "", "", nil, 0, nil, "", 0, fmt.Errorf("Invalid metahash")
		}
	}

	return *uiPort, *msg, *dest, *file, keywordsArray, uint64(*budget), metahash, *address, uint64(*tibcoin), nil
}
