package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

// WalletServer manages wallet operations and serves the web interface
type WalletServer struct {
	rpcClient *KernelcoinRPCClient
	mu        sync.RWMutex
	wallets   map[string]*WalletSession
}

// WalletSession stores information about a wallet session
type WalletSession struct {
	ID        string
	Wallet    *Wallet
	CreatedAt time.Time
	LastUsed  time.Time
}

// API Response structures
type BalanceResponse struct {
	Total       float64 `json:"total"`
	Confirmed   float64 `json:"confirmed"`
	Unconfirmed float64 `json:"unconfirmed"`
	Immature    float64 `json:"immature"`
}

type TransactionResponse struct {
	Account       string  `json:"account"`
	Address       string  `json:"address"`
	Category      string  `json:"category"`
	Amount        float64 `json:"amount"`
	Confirmations int     `json:"confirmations"`
	Txid          string  `json:"txid"`
	Time          int64   `json:"time"`
	TimeReceived  int64   `json:"timereceived"`
	Comment       string  `json:"comment,omitempty"`
}

type SendTransactionRequest struct {
	ToAddress string  `json:"to_address"`
	Amount    float64 `json:"amount"`
}

type SendTransactionResponse struct {
	Success bool   `json:"success"`
	Txid    string `json:"txid,omitempty"`
	Error   string `json:"error,omitempty"`
}

type ImportKeyRequest struct {
	WIF string `json:"wif"`
}

type ImportKeyResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

type MnemonicToWIFRequest struct {
	Mnemonic string `json:"mnemonic"`
}

type MnemonicToWIFResponse struct {
	Success bool   `json:"success"`
	WIF     string `json:"wif,omitempty"`
	Error   string `json:"error,omitempty"`
}

type NewAddressResponse struct {
	Success       bool   `json:"success"`
	LegacyAddress string `json:"legacy_address,omitempty"`
	SegWitAddress string `json:"segwit_address,omitempty"`
	Error         string `json:"error,omitempty"`
}

type AddressesResponse struct {
	Success   bool          `json:"success"`
	Addresses []AddressInfo `json:"addresses,omitempty"`
	Error     string        `json:"error,omitempty"`
}

type AddressInfo struct {
	Address string `json:"address"`
	Type    string `json:"type"`
}

type GetNewAddressRequest struct {
	AddressType string `json:"address_type"`
}

type GetNewAddressResponse struct {
	Success bool   `json:"success"`
	Address string `json:"address,omitempty"`
	Error   string `json:"error,omitempty"`
}

type GenerateAddressRequest struct {
	Type string `json:"type"`
}

type GenerateAddressResponse struct {
	Success bool   `json:"success"`
	Address string `json:"address,omitempty"`
	Error   string `json:"error,omitempty"`
}

type ValidateAddressRequest struct {
	Address string `json:"address"`
}

type ValidateAddressResponse struct {
	Isvalid bool   `json:"isvalid"`
	Error   string `json:"error,omitempty"`
}

type NewWalletResponse struct {
	Success       bool   `json:"success"`
	Mnemonic      string `json:"mnemonic,omitempty"`
	PrivateKeyWIF string `json:"private_key_wif,omitempty"`
	LegacyAddress string `json:"legacy_address,omitempty"`
	SegWitAddress string `json:"segwit_address,omitempty"`
	Error         string `json:"error,omitempty"`
}

type TransactionsListResponse struct {
	Success      bool                  `json:"success"`
	Transactions []TransactionResponse `json:"transactions,omitempty"`
	Error        string                `json:"error,omitempty"`
}

// NewWalletServer creates a new wallet server instance
func NewWalletServer(rpcURL, rpcUser, rpcPass string) *WalletServer {
	rpcClient := NewKernelcoinRPCClient(rpcURL, rpcUser, rpcPass)
	return &WalletServer{
		rpcClient: rpcClient,
		wallets:   make(map[string]*WalletSession),
	}
}

// HTTP Handler Functions

// HandleIndex serves the main HTML page
func (ws *WalletServer) HandleIndex(w http.ResponseWriter, r *http.Request) {
	log.Printf("[HTTP] Serving index page to %s", r.RemoteAddr)
	http.ServeFile(w, r, "index.html")
}

// HandleBalance returns the current balance
func (ws *WalletServer) HandleBalance(w http.ResponseWriter, r *http.Request) {
	log.Printf("[API] Balance request from %s", r.RemoteAddr)

	// Get all addresses in the wallet and sum their balances
	balanceInfo, err := ws.rpcClient.GetBalanceInfo("")
	if err != nil {
		log.Printf("[API] Balance ERROR: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Failed to get balance"})
		return
	}

	response := BalanceResponse{
		Total:       balanceInfo.Total,
		Confirmed:   balanceInfo.Confirmed,
		Unconfirmed: balanceInfo.Unconfirmed,
		Immature:    balanceInfo.Total - balanceInfo.Confirmed - balanceInfo.Unconfirmed,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
	log.Printf("[API] Balance response: %+v", response)
}

// HandleSendTransaction handles sending coins
func (ws *WalletServer) HandleSendTransaction(w http.ResponseWriter, r *http.Request) {
	log.Printf("[API] SendTransaction request from %s", r.RemoteAddr)

	if r.Method != http.MethodPost {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "POST only"})
		return
	}

	var req SendTransactionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("[API] SendTransaction ERROR: Invalid request - %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(SendTransactionResponse{
			Success: false,
			Error:   "Invalid request format",
		})
		return
	}

	log.Printf("[API] SendTransaction: %f KCN to %s", req.Amount, req.ToAddress)

	// Validate address
	valid, err := ws.rpcClient.ValidateAddress(req.ToAddress)
	if err != nil || !valid {
		log.Printf("[API] SendTransaction ERROR: Invalid address - %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(SendTransactionResponse{
			Success: false,
			Error:   "Invalid recipient address",
		})
		return
	}

	// Send transaction using the loaded wallet
	txid, err := ws.rpcClient.SendToAddress(req.ToAddress, req.Amount)
	if err != nil {
		log.Printf("[API] SendTransaction ERROR: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(SendTransactionResponse{
			Success: false,
			Error:   fmt.Sprintf("Failed to send transaction: %v", err),
		})
		return
	}

	log.Printf("[API] SendTransaction SUCCESS: txid=%s", txid)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(SendTransactionResponse{
		Success: true,
		Txid:    txid,
	})
}

// HandleImportKey imports a private key
func (ws *WalletServer) HandleImportKey(w http.ResponseWriter, r *http.Request) {
	log.Printf("[API] ImportKey request from %s", r.RemoteAddr)

	if r.Method != http.MethodPost {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "POST only"})
		return
	}

	var req ImportKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("[API] ImportKey ERROR: Invalid request - %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ImportKeyResponse{
			Success: false,
			Error:   "Invalid request format",
		})
		return
	}

	_, err := ws.rpcClient.ImportPrivateKey(req.WIF)
	if err != nil {
		log.Printf("[API] ImportKey ERROR: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ImportKeyResponse{
			Success: false,
			Error:   fmt.Sprintf("Failed to import key: %v", err),
		})
		return
	}

	log.Printf("[API] ImportKey SUCCESS")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ImportKeyResponse{
		Success: true,
	})
}

// HandleNewWallet generates a new wallet
func (ws *WalletServer) HandleNewWallet(w http.ResponseWriter, r *http.Request) {
	log.Printf("[API] NewWallet request from %s", r.RemoteAddr)

	if r.Method != http.MethodPost {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "POST only"})
		return
	}

	wallet, err := GenerateNewWallet()
	if err != nil {
		log.Printf("[API] NewWallet ERROR: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(NewWalletResponse{
			Success: false,
			Error:   fmt.Sprintf("Failed to generate wallet: %v", err),
		})
		return
	}

	log.Printf("[API] NewWallet SUCCESS: %s", wallet.LegacyAddress)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(NewWalletResponse{
		Success:       true,
		Mnemonic:      wallet.Mnemonic,
		PrivateKeyWIF: wallet.PrivateKeyWIF,
		LegacyAddress: wallet.LegacyAddress,
		SegWitAddress: wallet.SegWitAddress,
	})
}

// HandleNewAddress generates a new address from an existing wallet
func (ws *WalletServer) HandleNewAddress(w http.ResponseWriter, r *http.Request) {
	log.Printf("[API] NewAddress request from %s", r.RemoteAddr)

	if r.Method != http.MethodPost {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "POST only"})
		return
	}

	var req map[string]string
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("[API] NewAddress ERROR: Invalid request - %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(NewAddressResponse{
			Success: false,
			Error:   "Invalid request format",
		})
		return
	}

	mnemonic, ok := req["mnemonic"]
	if !ok || mnemonic == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(NewAddressResponse{
			Success: false,
			Error:   "Mnemonic is required",
		})
		return
	}

	wallet, err := GenerateWalletFromMnemonic(mnemonic)
	if err != nil {
		log.Printf("[API] NewAddress ERROR: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(NewAddressResponse{
			Success: false,
			Error:   fmt.Sprintf("Failed to generate address: %v", err),
		})
		return
	}

	log.Printf("[API] NewAddress SUCCESS: %s", wallet.LegacyAddress)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(NewAddressResponse{
		Success:       true,
		LegacyAddress: wallet.LegacyAddress,
		SegWitAddress: wallet.SegWitAddress,
	})
}

// HandleListTransactions lists transactions
func (ws *WalletServer) HandleListTransactions(w http.ResponseWriter, r *http.Request) {
	log.Printf("[API] ListTransactions request from %s", r.RemoteAddr)

	// Get count from query parameters, default to 50
	countStr := r.URL.Query().Get("count")
	count := 50
	if countStr != "" {
		if c, err := strconv.Atoi(countStr); err == nil {
			count = c
		}
	}

	txs, err := ws.rpcClient.ListTransactions("", count)
	if err != nil {
		log.Printf("[API] ListTransactions ERROR: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(TransactionsListResponse{
			Success: false,
			Error:   "Failed to list transactions",
		})
		return
	}

	// Convert interface{} slice to TransactionResponse structs
	transactions := []TransactionResponse{}
	for _, tx := range txs {
		if txMap, ok := tx.(map[string]interface{}); ok {
			txResp := TransactionResponse{
				Account:       getString(txMap, "account"),
				Address:       getString(txMap, "address"),
				Category:      getString(txMap, "category"),
				Amount:        getFloat64(txMap, "amount"),
				Confirmations: getInt(txMap, "confirmations"),
				Txid:          getString(txMap, "txid"),
				Time:          getInt64(txMap, "time"),
				TimeReceived:  getInt64(txMap, "timereceived"),
				Comment:       getString(txMap, "comment"),
			}
			transactions = append(transactions, txResp)
		}
	}

	log.Printf("[API] ListTransactions SUCCESS: Retrieved %d transactions", len(transactions))
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(TransactionsListResponse{
		Success:      true,
		Transactions: transactions,
	})
}

// HandleGetAddresses lists all addresses with label ""
func (ws *WalletServer) HandleGetAddresses(w http.ResponseWriter, r *http.Request) {
	log.Printf("[API] GetAddresses request from %s", r.RemoteAddr)

	addrs, err := ws.rpcClient.GetAddressesByLabel("")
	if err != nil {
		log.Printf("[API] GetAddresses ERROR: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(AddressesResponse{
			Success: false,
			Error:   fmt.Sprintf("Failed to get addresses: %v", err),
		})
		return
	}

	log.Printf("[API] GetAddresses: Retrieved %d addresses from RPC", len(addrs))

	addresses := []AddressInfo{}
	for _, addr := range addrs {
		addresses = append(addresses, AddressInfo{
			Address: addr,
			Type:    "Address",
		})
	}

	log.Printf("[API] GetAddresses SUCCESS: Returning %d addresses", len(addresses))
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(AddressesResponse{
		Success:   true,
		Addresses: addresses,
	})
}

// HandleGetNewAddress generates a new address
func (ws *WalletServer) HandleGetNewAddress(w http.ResponseWriter, r *http.Request) {
	log.Printf("[API] GetNewAddress request from %s", r.RemoteAddr)

	if r.Method != http.MethodPost {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "POST only"})
		return
	}

	var req GetNewAddressRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("[API] GetNewAddress ERROR: Invalid request - %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(GetNewAddressResponse{
			Success: false,
			Error:   "Invalid request format",
		})
		return
	}

	addr, err := ws.rpcClient.GetNewAddress("", req.AddressType)
	if err != nil {
		log.Printf("[API] GetNewAddress ERROR: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(GetNewAddressResponse{
			Success: false,
			Error:   fmt.Sprintf("Failed to generate address: %v", err),
		})
		return
	}

	log.Printf("[API] GetNewAddress SUCCESS: %s", addr)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(GetNewAddressResponse{
		Success: true,
		Address: addr,
	})
}

// HandleGenerateAddress generates a new address (frontend-compatible endpoint)
func (ws *WalletServer) HandleGenerateAddress(w http.ResponseWriter, r *http.Request) {
	log.Printf("[API] GenerateAddress request from %s", r.RemoteAddr)

	if r.Method != http.MethodPost {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "POST only"})
		return
	}

	var req GenerateAddressRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("[API] GenerateAddress ERROR: Invalid request - %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(GenerateAddressResponse{
			Success: false,
			Error:   "Invalid request format",
		})
		return
	}

	addr, err := ws.rpcClient.GetNewAddress("", req.Type)
	if err != nil {
		log.Printf("[API] GenerateAddress ERROR: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(GenerateAddressResponse{
			Success: false,
			Error:   fmt.Sprintf("Failed to generate address: %v", err),
		})
		return
	}

	log.Printf("[API] GenerateAddress SUCCESS: %s", addr)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(GenerateAddressResponse{
		Success: true,
		Address: addr,
	})
}

// HandleValidateAddress validates an address format
func (ws *WalletServer) HandleValidateAddress(w http.ResponseWriter, r *http.Request) {
	log.Printf("[API] ValidateAddress request from %s", r.RemoteAddr)

	if r.Method != http.MethodPost {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "POST only"})
		return
	}

	var req ValidateAddressRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("[API] ValidateAddress ERROR: Invalid request - %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ValidateAddressResponse{
			Isvalid: false,
			Error:   "Invalid request format",
		})
		return
	}

	valid, err := ws.rpcClient.ValidateAddress(req.Address)
	if err != nil {
		log.Printf("[API] ValidateAddress ERROR: %v", err)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ValidateAddressResponse{
			Isvalid: false,
			Error:   fmt.Sprintf("Validation error: %v", err),
		})
		return
	}

	log.Printf("[API] ValidateAddress: %s is valid=%v", req.Address, valid)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ValidateAddressResponse{
		Isvalid: valid,
	})
}

// HandleCheckWallet checks if a wallet is loaded
func (ws *WalletServer) HandleCheckWallet(w http.ResponseWriter, r *http.Request) {
	log.Printf("[API] CheckWallet request from %s", r.RemoteAddr)

	addrs, err := ws.rpcClient.GetAddressesByLabel("")
	if err != nil {
		log.Printf("[API] CheckWallet ERROR: %v", err)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"loaded": false,
		})
		return
	}

	loaded := len(addrs) > 0
	log.Printf("[API] CheckWallet: Wallet loaded=%v (addresses: %d)", loaded, len(addrs))
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"loaded": loaded,
	})
}

// HandleMnemonicToWIF converts a mnemonic phrase to WIF format
func (ws *WalletServer) HandleMnemonicToWIF(w http.ResponseWriter, r *http.Request) {
	log.Printf("[API] MnemonicToWIF request from %s", r.RemoteAddr)

	if r.Method != http.MethodPost {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "POST only"})
		return
	}

	var req MnemonicToWIFRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("[API] MnemonicToWIF ERROR: Invalid request - %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(MnemonicToWIFResponse{
			Success: false,
			Error:   "Invalid request format",
		})
		return
	}

	if req.Mnemonic == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(MnemonicToWIFResponse{
			Success: false,
			Error:   "Mnemonic phrase is required",
		})
		return
	}

	// Generate wallet from mnemonic
	wallet, err := GenerateWalletFromMnemonic(req.Mnemonic)
	if err != nil {
		log.Printf("[API] MnemonicToWIF ERROR: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(MnemonicToWIFResponse{
			Success: false,
			Error:   fmt.Sprintf("Failed to convert mnemonic: %v", err),
		})
		return
	}

	log.Printf("[API] MnemonicToWIF SUCCESS: Converted mnemonic to WIF")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(MnemonicToWIFResponse{
		Success: true,
		WIF:     wallet.PrivateKeyWIF,
	})
}

// HandleNetworkInfo returns network information
func (ws *WalletServer) HandleNetworkInfo(w http.ResponseWriter, r *http.Request) {
	log.Printf("[API] NetworkInfo request from %s", r.RemoteAddr)

	info, err := ws.rpcClient.GetNetworkInfo()
	if err != nil {
		log.Printf("[API] NetworkInfo ERROR: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Failed to get network info"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(info)
	log.Printf("[API] NetworkInfo response sent")
}

// HandleBlockchainInfo returns blockchain information
func (ws *WalletServer) HandleBlockchainInfo(w http.ResponseWriter, r *http.Request) {
	log.Printf("[API] BlockchainInfo request from %s", r.RemoteAddr)

	info, err := ws.rpcClient.GetBlockchainInfo()
	if err != nil {
		log.Printf("[API] BlockchainInfo ERROR: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Failed to get blockchain info"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(info)
	log.Printf("[API] BlockchainInfo response sent")
}

// Helper functions to safely extract values from interface{} maps
func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func getFloat64(m map[string]interface{}, key string) float64 {
	if v, ok := m[key].(float64); ok {
		return v
	}
	return 0
}

func getInt(m map[string]interface{}, key string) int {
	if v, ok := m[key].(float64); ok {
		return int(v)
	}
	return 0
}

func getInt64(m map[string]interface{}, key string) int64 {
	if v, ok := m[key].(float64); ok {
		return int64(v)
	}
	return 0
}

// StartServer starts the HTTP server
func (ws *WalletServer) StartServer(listenAddr string) error {
	// Create a custom mux to control route priority
	mux := http.NewServeMux()

	// API routes (must be registered before static files)
	mux.HandleFunc("/api/balance", ws.HandleBalance)
	mux.HandleFunc("/api/send", ws.HandleSendTransaction)
	mux.HandleFunc("/api/import", ws.HandleImportKey)
	mux.HandleFunc("/api/import-mnemonic", ws.HandleMnemonicToWIF)
	mux.HandleFunc("/api/new-wallet", ws.HandleNewWallet)
	mux.HandleFunc("/api/new-address", ws.HandleNewAddress)
	mux.HandleFunc("/api/transactions", ws.HandleListTransactions)
	mux.HandleFunc("/api/addresses", ws.HandleGetAddresses)
	mux.HandleFunc("/api/getnewaddress", ws.HandleGetNewAddress)
	mux.HandleFunc("/api/generate-address", ws.HandleGenerateAddress)
	mux.HandleFunc("/api/validateaddress", ws.HandleValidateAddress)
	mux.HandleFunc("/api/check-wallet", ws.HandleCheckWallet)
	mux.HandleFunc("/api/network-info", ws.HandleNetworkInfo)
	mux.HandleFunc("/api/blockchain-info", ws.HandleBlockchainInfo)

	// Index route
	mux.HandleFunc("/", ws.HandleIndex)

	// Static files (catch-all, must be last)
	fs := http.FileServer(http.Dir("."))
	mux.Handle("/static/", fs)

	log.Printf("[SERVER] Starting wallet server on %s", listenAddr)
	return http.ListenAndServe(listenAddr, mux)
}

// InitializeWalletFromEnv loads and imports a wallet from the WALLET_WIF environment variable
func (ws *WalletServer) InitializeWalletFromEnv() error {
	walletWIF := os.Getenv("WALLET_WIF")
	if walletWIF == "" {
		log.Printf("[INIT] No WALLET_WIF environment variable set - wallet will need to be imported manually")
		return nil
	}

	log.Printf("[INIT] Loading wallet from WALLET_WIF environment variable...")
	_, err := ws.rpcClient.ImportPrivateKey(walletWIF)
	if err != nil {
		log.Printf("[INIT] WARNING: Failed to import wallet from WALLET_WIF: %v", err)
		return err
	}

	log.Printf("[INIT] Wallet successfully imported from WALLET_WIF")
	return nil
}

func main() {
	// Configuration from environment variables or defaults
	rpcURL := os.Getenv("RPC_URL")
	if rpcURL == "" {
		rpcURL = "http://127.0.0.1:9332"
	}

	rpcUser := os.Getenv("RPC_USER")
	if rpcUser == "" {
		rpcUser = "kernelcoinrpc"
	}

	rpcPass := os.Getenv("RPC_PASS")
	if rpcPass == "" {
		rpcPass = "kernelcoinpass"
	}

	listenAddr := os.Getenv("LISTEN_ADDR")
	if listenAddr == "" {
		listenAddr = "127.0.0.1:8080"
	}

	// Change to the directory where the executable is
	exePath, err := os.Executable()
	if err == nil {
		exeDir := filepath.Dir(exePath)
		os.Chdir(exeDir)
	}

	log.Printf("[INIT] Kernelcoin Web Wallet")
	log.Printf("[INIT] RPC URL: %s", rpcURL)
	log.Printf("[INIT] RPC User: %s", rpcUser)
	log.Printf("[INIT] Listen Address: %s", listenAddr)

	// Create wallet server
	server := NewWalletServer(rpcURL, rpcUser, rpcPass)

	// Initialize wallet from environment variable if provided
	if err := server.InitializeWalletFromEnv(); err != nil {
		log.Printf("[INIT] WARNING: Could not initialize wallet from environment: %v", err)
	}

	// Start server
	if err := server.StartServer(listenAddr); err != nil {
		log.Fatalf("[ERROR] Failed to start server: %v", err)
	}
}
