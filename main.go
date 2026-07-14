package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
)

var (
	tokenMutex  sync.Mutex
	cachedToken string
)

func main() {
	// 1. Load environment variables from .env
	loadEnv(".env")

	port := os.Getenv("PORT")
	if port == "" {
		port = "8002"
	}

	// 2. Set up HTTP handlers
	http.HandleFunc("/", serveIndex)
	http.HandleFunc("/api/v1/customer/maverick/init-payment", handleInitPayment)
	http.HandleFunc("/api/v1/customer/maverick/save-card", handleSaveCard)
	http.HandleFunc("/api/v1/customer/maverick/charge", handleChargeCard)

	log.Printf("Starting Maverick POC standalone server on http://localhost:%s\n", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("Server failed to start: %v\n", err)
	}
}

// serveIndex serves the index.html page, styles.css, or app.js
func serveIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/styles.css" {
		http.ServeFile(w, r, "styles.css")
		return
	}
	if r.URL.Path == "/app.js" {
		http.ServeFile(w, r, "app.js")
		return
	}
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, "index.html")
}

// handleInitPayment proxies OAuth authentication and Maverick token generation
func handleInitPayment(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	clientID := os.Getenv("INAVATE_CLIENT_ID")
	clientSecret := os.Getenv("INAVATE_CLIENT_SECRET")
	apiURL := os.Getenv("INAVATE_API_URL")

	if clientID == "" || clientSecret == "" || apiURL == "" {
		respondWithError(w, http.StatusInternalServerError, "Server misconfigured: missing environment variables")
		return
	}

	log.Printf("[InitPayment] Step 1: Requesting OAuth token from Inavate for client ID: %s\n", clientID)

	// Call OAuth server to get Access Token
	oauthURL := apiURL + "/oauth/token"
	formBody := fmt.Sprintf("grant_type=client_credentials&client_id=%s&client_secret=%s", clientID, clientSecret)

	resp, err := http.Post(oauthURL, "application/x-www-form-urlencoded", strings.NewReader(formBody))
	if err != nil {
		log.Printf("[InitPayment] OAuth error: %v\n", err)
		respondWithError(w, http.StatusBadGateway, "Failed to reach Inavate OAuth server")
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		log.Printf("[InitPayment] OAuth failed with status %d: %s\n", resp.StatusCode, string(respBody))
		respondWithError(w, resp.StatusCode, "OAuth authentication failed")
		return
	}

	var oauthData struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(respBody, &oauthData); err != nil || oauthData.AccessToken == "" {
		log.Printf("[InitPayment] Failed to parse OAuth response: %v\n", err)
		respondWithError(w, http.StatusBadGateway, "Invalid OAuth token response")
		return
	}

	// Determine domain for Maverick SDK validation
	requestDomain := "http://localhost:8002"
	if origin := r.Header.Get("Origin"); origin != "" {
		requestDomain = origin
	} else if referer := r.Header.Get("Referer"); referer != "" {
		if parts := strings.Split(referer, "/"); len(parts) >= 3 {
			requestDomain = parts[0] + "//" + parts[2]
		}
	} else {
		requestDomain = "http://" + r.Host
	}

	log.Printf("[InitPayment] Step 2: Requesting Maverick token for domain: %s\n", requestDomain)

	maverickURL := apiURL + "/api/v1/card/maverick/get-token"
	req, err := http.NewRequest("POST", maverickURL, nil)
	if err != nil {
		log.Printf("[InitPayment] Failed to build request: %v\n", err)
		respondWithError(w, http.StatusInternalServerError, "Internal request construction error")
		return
	}
	req.Header.Set("Authorization", "Bearer "+oauthData.AccessToken)

	mavResp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("[InitPayment] API error: %v\n", err)
		respondWithError(w, http.StatusBadGateway, "Failed to reach Inavate API server")
		return
	}
	defer mavResp.Body.Close()

	mavRespBody, _ := io.ReadAll(mavResp.Body)
	if mavResp.StatusCode != http.StatusOK {
		log.Printf("[InitPayment] Token generation failed with status %d: %s\n", mavResp.StatusCode, string(mavRespBody))
		respondWithError(w, mavResp.StatusCode, "Maverick token generation failed")
		return
	}

	var mavData struct {
		MaverickToken string `json:"maverick_token"`
		ApiToken      string `json:"api_token"`
	}
	if err := json.Unmarshal(mavRespBody, &mavData); err != nil || mavData.MaverickToken == "" {
		log.Printf("[InitPayment] Parse error: %v\n", err)
		respondWithError(w, http.StatusBadGateway, "Failed to parse Maverick token payload")
		return
	}

	// Cache api_token in memory for subsequent save-card/charge operations
	tokenMutex.Lock()
	cachedToken = mavData.ApiToken
	tokenMutex.Unlock()

	log.Printf("[InitPayment] Success. Maverick Token: %s...\n", mavData.MaverickToken[:15])

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"maverick_token": mavData.MaverickToken,
	})
}

// handleSaveCard exchanges temporary maverick token for persistent card token
func handleSaveCard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tokenMutex.Lock()
	apiToken := cachedToken
	tokenMutex.Unlock()

	if apiToken == "" {
		respondWithError(w, http.StatusUnauthorized, "Not initialized. Call init-payment first.")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	log.Println("[SaveCard] Requesting card token exchange from Inavate API")

	apiURL := os.Getenv("INAVATE_API_URL")
	req, err := http.NewRequest("POST", apiURL+"/api/v1/card/maverick/save-card", bytes.NewReader(body))
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Internal request construction error")
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("[SaveCard] API error: %v\n", err)
		respondWithError(w, http.StatusBadGateway, "Failed to reach Inavate API server")
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	w.Write(respBody)
}

// handleChargeCard processes the payment via Maverick
func handleChargeCard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tokenMutex.Lock()
	apiToken := cachedToken
	tokenMutex.Unlock()

	if apiToken == "" {
		respondWithError(w, http.StatusUnauthorized, "Not initialized. Call init-payment first.")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	log.Println("[ChargeCard] Directing sale transaction request to Inavate API")

	apiURL := os.Getenv("INAVATE_API_URL")
	req, err := http.NewRequest("POST", apiURL+"/api/v1/card/maverick/charge", bytes.NewReader(body))
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Internal request construction error")
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("[ChargeCard] API error: %v\n", err)
		respondWithError(w, http.StatusBadGateway, "Failed to reach Inavate API server")
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	w.Write(respBody)
}

// loadEnv reads key=value pairs from env file and sets them
func loadEnv(filepath string) {
	file, err := os.Open(filepath)
	if err != nil {
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			value = strings.Trim(value, `"'`)
			os.Setenv(key, value)
		}
	}
}

// respondWithError helper to return JSON-formatted errors
func respondWithError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{
		"error": msg,
	})
}
