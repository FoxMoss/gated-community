package main

import (
	"bytes"
	"crypto"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/joho/godotenv"
	_ "github.com/mattn/go-sqlite3"
)

func GetAccessToken(authCode string) (string, error) {
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
		return "", errors.New("can't marshal oauth token packet")
	}

	oauthResponse, err := http.Post("https://auth.hackclub.com/oauth/token", "application/json", bytes.NewBuffer(payloadStr))
	if err != nil {
		return "", err
	}

	oauthResponseBody, err := io.ReadAll(oauthResponse.Body)
	if err != nil {
		return "", err
	}

	var decodedOauthResponse map[string]any

	err = json.Unmarshal(oauthResponseBody, &decodedOauthResponse)

	if err != nil {
		return "", err
	}

	errorResponse, hasError := decodedOauthResponse["error_description"]

	if hasError {
		return "", errors.New(errorResponse.(string))
	}

	accessToken, ok := decodedOauthResponse["access_token"].(string)

	if !ok {
		return "", err
	}

	return accessToken, nil
}

func GetMe(accessToken string) (map[string]any, error) {
	client := &http.Client{}

	request, err := http.NewRequest(http.MethodGet, "https://auth.hackclub.com/api/v1/me", nil)
	if err != nil {
		return nil, err
	}

	request.Header.Add("Authorization", fmt.Sprintf("Bearer %v", accessToken))

	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}

	meResponseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}

	var decodedMeResponse map[string]any

	err = json.Unmarshal(meResponseBody, &decodedMeResponse)

	if err != nil {
		return nil, err
	}

	errorResponse, hasError := decodedMeResponse["error_description"]

	if hasError {
		return nil, errors.New(errorResponse.(string))
	}

	return decodedMeResponse, nil
}

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("error: cant load .env file")
	}

	db, err := sql.Open("sqlite3", "cache.db")
	if err != nil {
		log.Fatal("error: cant load cache.db file")
	}

	defer db.Close()

	// TODO: pop a name & oid in here
	db.Exec(`
CREATE TABLE "access_tokens" (
	"access_key"	TEXT NOT NULL UNIQUE,
	"auth_type"	TEXT NOT NULL,
	"access_token"	TEXT,
	"ip"	TEXT,
	"name"	TEXT NOT NULL,
	"time_created"	INTEGER NOT NULL,
	PRIMARY KEY("access_key")
);`)

	http.HandleFunc("/auth", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			fmt.Fprintf(w, "error: must be GET")
			return
		}

		authCode := r.URL.Query().Get("code")

		if authCode == "" {
			fmt.Fprintf(w, "error: authCode was empty")
			return
		}

		accessToken, err := GetAccessToken(authCode)
		if err != nil {
			fmt.Fprintf(w, "error: %q", err.Error())
			return
		}

		meData, err := GetMe(accessToken)

		if err != nil {
			fmt.Fprintf(w, "error: %q", err.Error())
			return
		}

		firstName := meData["identity"].(map[string]any)["first_name"].(string)

		userIdHash := crypto.SHA256.New()
		userIdHash.Write([]byte(accessToken))
		userIdSum := hex.EncodeToString(userIdHash.Sum(nil))

		tx, err := db.Begin()
		if err != nil {
			fmt.Fprintf(w, "error: %q", err.Error())
			return
		}

		stmt, err := tx.Prepare(`INSERT OR REPLACE INTO "access_tokens" 
																(access_key, auth_type, access_token, name, time_created) 
																VALUES (?, "HCA", ?, ?, unixepoch())`)
		if err != nil {
			log.Fatal(err)
		}

		defer stmt.Close()

		_, err = stmt.Exec(userIdSum, accessToken, firstName)

		if err != nil {
			fmt.Fprintf(w, "error: %q", err.Error())
			return
		}

		err = tx.Commit()

		if err != nil {
			fmt.Fprintf(w, "error: %q", err.Error())
			return
		}

		fmt.Fprintf(w, "Run `gated-community setkey %q`", userIdSum)
	})

	http.HandleFunc("/api/v1/get_info", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Content-Type", "application/json")

		type ErrorType struct {
			ErrorDescription string `json:"error_description"`
		}

		if r.Method != "GET" {
			payload := ErrorType{
				ErrorDescription: "must be a GET"}
			payloadStr, _ := json.Marshal(payload)

			w.Write(payloadStr)
			return
		}

		authKey, hasAuthKey := r.Header["Authorization"]
		if !hasAuthKey {
			payload := ErrorType{
				ErrorDescription: "must have Authorization header set"}
			payloadStr, _ := json.Marshal(payload)

			w.Write(payloadStr)
			return
		}

		tx, err := db.Begin()
		if err != nil {
			payload := ErrorType{
				ErrorDescription: err.Error()}
			payloadStr, _ := json.Marshal(payload)

			w.Write(payloadStr)
			return
		}

		stmt, err := tx.Prepare(`SELECT access_key, auth_type, name, time_created FROM "access_tokens" WHERE access_key = ?`)
		if err != nil {
			payload := ErrorType{
				ErrorDescription: err.Error()}
			payloadStr, _ := json.Marshal(payload)

			w.Write(payloadStr)
			return
		}

		defer stmt.Close()

		row := stmt.QueryRow(authKey[0])

		type InfoType struct {
			AccessKey   string `json:"access_key"`
			AuthType    string `json:"auth_type"`
			Name        string `json:"name"`
			TimeCreated int    `json:"time_created"`
		}

		var returnInfo InfoType

		err = row.Scan(&returnInfo.AccessKey, &returnInfo.AuthType, &returnInfo.Name, &returnInfo.TimeCreated)

		if err != nil {
			payload := ErrorType{
				ErrorDescription: err.Error()}
			payloadStr, _ := json.Marshal(payload)

			w.Write(payloadStr)
			return
		}

		payloadStr, _ := json.Marshal(returnInfo)

		w.Write(payloadStr)

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
