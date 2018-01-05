package gossipernode

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
)

func (gossiper *Gossiper) LaunchWebServer() {
	r := mux.NewRouter()

	// POST
	r.HandleFunc("/message", gossiper.NewMessageHandler).Methods("POST")
	r.HandleFunc("/private", gossiper.NewPrivateMessageHandler).Methods("POST")
	r.HandleFunc("/name", gossiper.NewNameHandler).Methods("POST")
	r.HandleFunc("/peer", gossiper.NewPeerHandler).Methods("POST")
	r.HandleFunc("/share-file", gossiper.NewFilerHandler).Methods("POST")
	r.HandleFunc("/search", gossiper.NewSearchHandler).Methods("POST")
	r.HandleFunc("/download", gossiper.NewDownloadHandler).Methods("POST")
	r.HandleFunc("/tx", gossiper.NewTxHandler).Methods("POST")

	// GET
	r.HandleFunc("/chat", gossiper.ChatHandler).Methods("GET")
	r.HandleFunc("/peers", gossiper.PeersHandler).Methods("GET")
	r.HandleFunc("/name", gossiper.NameHandler).Methods("GET")
	r.HandleFunc("/destinations", gossiper.DestinationsHandler).Methods("GET")
	r.HandleFunc("/private-chat", gossiper.PrivateChatHandler).Methods("GET")
	r.HandleFunc("/matches", gossiper.MatchesHandler).Methods("GET")
	r.HandleFunc("/blockchain", gossiper.GetBlockchainHandler).Methods("GET")

	// Serve static files
	r.PathPrefix("/").Handler(http.FileServer(http.Dir("./gossipernode/static/")))

	http.Handle("/", r)
	http.ListenAndServe(":"+gossiper.guiPort, nil)

	fmt.Println("Listening...")
}

// -------------------------------- Outputs --------------------------------- //
func (gossiper *Gossiper) MatchesHandler(w http.ResponseWriter, r *http.Request) {
	var filenames []string
	gossiper.matchesMutex.Lock()
	for _, state := range gossiper.matches {
		filenames = append(filenames, state.FileName)
	}
	gossiper.matchesMutex.Unlock()

	m := map[string]interface{}{"matches": filenames}

	payload, err := json.Marshal(m)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.Write(payload)
}

func (gossiper *Gossiper) ChatHandler(w http.ResponseWriter, r *http.Request) {
	gossiper.globalChatMutex.Lock()
	m := map[string]interface{}{"chat": gossiper.globalChat}
	gossiper.globalChatMutex.Unlock()

	payload, err := json.Marshal(m)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.Write(payload)
}

func (gossiper *Gossiper) PeersHandler(w http.ResponseWriter, r *http.Request) {
	gossiper.peersMutex.Lock()
	peers := make([]string, 0, len(gossiper.peers))
	for peer := range gossiper.peers {
		peers = append(peers, peer)
	}
	gossiper.peersMutex.Unlock()

	m := map[string]interface{}{"peers": peers}
	payload, err := json.Marshal(m)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.Write(payload)
}

func (gossiper *Gossiper) NameHandler(w http.ResponseWriter, r *http.Request) {
	gossiper.nameMutex.Lock()
	m := map[string]interface{}{"name": gossiper.name}
	gossiper.nameMutex.Unlock()

	payload, err := json.Marshal(m)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.Write(payload)
}

func (gossiper *Gossiper) DestinationsHandler(w http.ResponseWriter, r *http.Request) {
	gossiper.routingMutex.Lock()
	destinations := make([]string, 0, len(gossiper.routing))
	for dest := range gossiper.routing {
		destinations = append(destinations, dest)
	}
	gossiper.routingMutex.Unlock()

	m := map[string]interface{}{"destinations": destinations}
	payload, err := json.Marshal(m)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.Write(payload)
}

func (gossiper *Gossiper) PrivateChatHandler(w http.ResponseWriter, r *http.Request) {
	dest := r.URL.Query()["dest"]
	if dest == nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	gossiper.messagesMutex.Lock()
	history := gossiper.getHistory(dest[0])
	m := map[string]interface{}{"private-chat": history.privateChat}
	gossiper.messagesMutex.Unlock()

	payload, err := json.Marshal(m)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.Write(payload)
}

func (gossiper *Gossiper) GetBlockchainHandler(w http.ResponseWriter, r *http.Request) {
	blocks := gossiper.getBlockchain()
	m := map[string]interface{}{"blocks": blocks}

	payload, err := json.Marshal(m)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.Write(payload)
}

// --------------------------------- Inputs --------------------------------- //
func (gossiper *Gossiper) NewMessageHandler(w http.ResponseWriter, r *http.Request) {
	// Get input
	message := r.FormValue("message")
	if message == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// Handle new rumor message
	gossiper.sendNewMessage(message)
	w.WriteHeader(http.StatusOK)
}

func (gossiper *Gossiper) NewPrivateMessageHandler(w http.ResponseWriter, r *http.Request) {
	// Get input
	message := r.FormValue("message")
	dest := r.FormValue("dest")
	if message == "" || dest == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// Handle new private message
	gossiper.sendNewPrivateMessage(message, dest)
	w.WriteHeader(http.StatusOK)
}

func (gossiper *Gossiper) NewNameHandler(w http.ResponseWriter, r *http.Request) {
	// Get input
	name := r.FormValue("name")
	if name == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// Handle new name
	gossiper.changeName(name)
	w.WriteHeader(http.StatusOK)
}

func (gossiper *Gossiper) NewPeerHandler(w http.ResponseWriter, r *http.Request) {
	// Get input
	peer := r.FormValue("peer")
	if peer == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// Handle new peer
	gossiper.addNewPeer(peer)
	w.WriteHeader(http.StatusOK)
}

func (gossiper *Gossiper) NewFilerHandler(w http.ResponseWriter, r *http.Request) {
	// Get input
	filename := r.FormValue("filename")
	if filename == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// Handle new peer
	err := gossiper.shareFile(filename)
	if err == nil {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func (gossiper *Gossiper) NewSearchHandler(w http.ResponseWriter, r *http.Request) {
	// Get input
	keywords := r.FormValue("keywords")
	if keywords == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// Split keywords
	keywordArray := strings.Split(keywords, ",")

	// Handle new search
	gossiper.searchFile(keywordArray, DefaultBudget)
	w.WriteHeader(http.StatusOK)
}

func (gossiper *Gossiper) NewDownloadHandler(w http.ResponseWriter, r *http.Request) {
	// Get input
	filename := r.FormValue("filename")
	if filename == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	gossiper.downloadFile(filename)
	w.WriteHeader(http.StatusOK)
}

func (gossiper *Gossiper) NewTxHandler(w http.ResponseWriter, r *http.Request) {
	// Get input
	value := r.FormValue("value")
	gossiper.errLogger.Println(value)
	valueInt, err := strconv.Atoi(value)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	to := r.FormValue("to")
	if to == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	err = gossiper.createTransaction(valueInt, to)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
	} else {
		w.WriteHeader(http.StatusOK)
	}
}
