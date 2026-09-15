package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/joho/godotenv"
)

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	http.HandleFunc("/auth", func(w http.ResponseWriter, r *http.Request) {
		authCode := r.URL.Query().Get("code")

		if authCode == "" {
			fmt.Fprintf(w, "error: authCode was empty")
			return
		}

		type OAuthToken struct {
			ClientId     string `json:"client_id"`
			ClientSecret string `json:"client_secret"`
			RedirectUri  string `json:"redirect_uri"`
			Code         string `json:"code"`
			GrantType    string `json:"grant_type"`
		}

		payload := OAuthToken{
			ClientId:     os.Getenv("CLIENT_ID"),
			ClientSecret: os.Getenv("CLIENT_SECRET"),
			RedirectUri:  os.Getenv("REDIRECT_URL"),
			Code:         authCode,
			GrantType:    "authorization_code"}

		payloadStr, err := json.Marshal(payload)
		if err != nil {
			fmt.Fprintf(w, "error: can't marshal oauth token packet")
			return
		}

		oauthResponse, err := http.Post("https://auth.hackclub.com/oauth/token", "application/json", bytes.NewBuffer(payloadStr))
		if err != nil {
			fmt.Fprintf(w, "error: %q", err.Error())
			return
		}

		oauthResponseBody, err := io.ReadAll(oauthResponse.Body)
		if err != nil {
			fmt.Fprintf(w, "error: %q", err.Error())
			return
		}

		var decodedOauthResponse map[string]any

		err = json.Unmarshal(oauthResponseBody, &decodedOauthResponse)

		if err != nil {
			fmt.Fprintf(w, "error: %q", err.Error())
			return
		}

		errorResponse, hasError := decodedOauthResponse["error_description"]

		if hasError {
			fmt.Fprintf(w, "error: %q", errorResponse)
			return
		}

		fmt.Fprintf(w, "Hello, %q %q", oauthResponseBody, payloadStr)
	})

	s := &http.Server{
		Addr:           ":8080",
		Handler:        http.DefaultServeMux,
		ReadTimeout:    10 * time.Second,
		WriteTimeout:   10 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}
	log.Fatal(s.ListenAndServe())
}
