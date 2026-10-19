package main

import (
	"bytes"
	"crypto"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/joho/godotenv"
	_ "github.com/mattn/go-sqlite3"
	"go.bug.st/serial"
)

func getTTY(mode *serial.Mode) (serial.Port, error) {
	for i := 0; i < 10; i++ {
		nameTTY := fmt.Sprintf("/dev/ttyUSB%v", i)

		file, err := serial.Open(nameTTY, mode)
		if err != nil {
			continue
		}
		return file, nil
	}
	return nil, errors.New("cannot open FPGA uart")
}

func GetAccessToken(authCode string) (string, error) {
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

	var upgrader = websocket.Upgrader{}

	type QueuedInfo struct {
		Id        string
		Value     []byte
		TimeAdded time.Time
	}

	type WebsocketState string
	const (
		WebsocketConnect    WebsocketState = "CONNECT"
		WebsocketDisconnect WebsocketState = "DISCONNECT"
	)

	type WebsocketMessage struct {
		Socket *websocket.Conn
		State  WebsocketState
	}

	readyForQueue := atomic.Bool{}
	readyForQueue.Store(true)

	if err != nil {
		fmt.Println("error: ", err.Error())
	}

	var activeConnection *websocket.Conn
	activeConnectionMutex := sync.Mutex{}

	mode := &serial.Mode{
		BaudRate: 115200,
	}

	serialStream := make(chan serial.Port)

	go func() {
		for {

			file := <-serialStream

			buffer := make([]byte, 1024)
			for {
				bufferLen, err := file.Read(buffer)

				if err != nil {
					errorMessage := UpdateType{
						Type:        "warn",
						Description: "cannot read fpga serial",
					}

					errorMarshalled, err := json.Marshal(errorMessage)
					if err != nil {
						fmt.Println("error:", err.Error())
						continue
					}

					if activeConnection != nil {
						activeConnectionMutex.Lock()
						err = activeConnection.WriteJSON(string(errorMarshalled))
						activeConnectionMutex.Unlock()
					}

					break
				}

				returnArray := make([]int, bufferLen)
				for item := 0; item < bufferLen; item++ {
					returnArray[item] = int(buffer[item])
				}

				if activeConnection != nil {
					transferMessage := TransferType{
						Type: "egress",
						Data: returnArray,
					}

					transferMarshalled, err := json.Marshal(transferMessage)
					if err != nil {
						fmt.Println("error: ", err.Error())
						continue
					}

					activeConnectionMutex.Lock()
					err = activeConnection.WriteJSON(string(transferMarshalled))
					activeConnectionMutex.Unlock()
				}

			}
		}
	}()

	activeChannel := make(chan WebsocketMessage, 0)
	websocketChannel := make(chan WebsocketMessage, 10)
	finishActiveConnection := make(chan bool, 0)

	fileDB := map[*websocket.Conn][]byte{}
	fileDBMutex := sync.Mutex{}

	// active connection handler
	go func() {
		for {
			println("waiting again")
			activeMsg := <-activeChannel

			activeConnectionMutex.Lock()
			activeConnection = activeMsg.Socket
			activeConnectionMutex.Unlock()

			fileDBMutex.Lock()
			bitstream := fileDB[activeMsg.Socket]
			fileDBMutex.Unlock()

			if bitstream == nil {
				payload := UpdateType{
					Type:        "error",
					Description: "no bitstream file"}

				payloadStr, err := json.Marshal(payload)
				if err != nil {
					activeMsg.Socket.WriteJSON(string(payloadStr))
				}
			} else {
				{
					payload := UpdateType{
						Type:        "status",
						Description: "flashing"}
					payloadStr, err := json.Marshal(payload)
					if err == nil {
						activeMsg.Socket.WriteJSON(string(payloadStr))
					}
				}

				bitstreamFd, err := os.OpenFile("file.bit", os.O_CREATE|os.O_RDWR, os.ModePerm)
				bitstreamFd.Write(bitstream)
				bitstreamFd.Close()

				cmd := exec.Command("openFPGALoader", "-b", "icepi-zero", "file.bit")

				outMsg, err := cmd.CombinedOutput()

				if err != nil {
					payload := UpdateType{
						Type:        "error",
						Description: string(outMsg)}

					payloadStr, err := json.Marshal(payload)
					if err == nil {
						activeMsg.Socket.WriteJSON(string(payloadStr))
					}

					finishActiveConnection <- true
				}

				{
					payload := UpdateType{
						Type:        "status",
						Description: "flashSuccess"}
					payloadStr, err := json.Marshal(payload)
					if err == nil {
						activeMsg.Socket.WriteJSON(string(payloadStr))
					}
				}

				file, err := getTTY(mode)
				defer file.Close()
				if err != nil {
					payload := UpdateType{
						Type:        "error",
						Description: string("no serial found for uart")}

					payloadStr, err := json.Marshal(payload)
					if err == nil {
						activeMsg.Socket.WriteJSON(string(payloadStr))
					}

					finishActiveConnection <- true
					continue
				}

				serialStream <- file

				for {
					readStructureStr := ""

					println("reading")
					err = activeMsg.Socket.ReadJSON(&readStructureStr)
					if err != nil {
						println("error reading")
						break
					}

					readStructure := map[string]any{}

					err = json.Unmarshal([]byte(readStructureStr), &readStructure)
					if err != nil {
						payload := UpdateType{
							Type:        "error",
							Description: "unmarshal error on servers fault"}

						payloadStr, err := json.Marshal(payload)
						if err != nil {
							activeMsg.Socket.WriteJSON(string(payloadStr))
						}
						break
					}

					data, hasData := readStructure["data"]
					if !hasData {
						fmt.Println("error: error parsing data, with unknown error")
						break
					}

					dataBytes := make([]byte, len(data.([]any)))
					for item := 0; item < len(data.([]any)); item++ {
						dataBytes[item] = byte(data.([]any)[item].(float64))
					}

					_, err = file.Write(dataBytes)
					if err != nil {
						println(err.Error())
					}
				}
			}

			println("ended")
			finishActiveConnection <- true
		}
	}()

	timerExpired := atomic.Bool{}
	timerExpired.Store(false)

	// queue handler
	go func() {
		websocketQueue := []*websocket.Conn{}
		websocketStatuses := map[*websocket.Conn]bool{}

		lockoutTimer := time.NewTimer(time.Second * 60 * 5)
		lockoutTimer.Stop()

		var connectedCount uint64 = 0
		for {

			println("waiting again connection")
			select {
			case _ = <-finishActiveConnection:
				println("removing connection")
				lockoutTimer.Stop()

				readyForQueue.Store(true)

				println("waiting on lock")
				activeConnectionMutex.Lock()

				if activeConnection != nil {
					activeConnection.Close()
				}

				websocketChannel <- WebsocketMessage{Socket: activeConnection, State: WebsocketDisconnect}

				activeConnection = nil
				activeConnectionMutex.Unlock()
				println("unlocked unlocked")
			case _ = <-lockoutTimer.C:
				println("debug: timer expired!")

				payload := UpdateType{
					Type:        "status",
					Description: "timerExpired"}
				payloadStr, err := json.Marshal(payload)
				if err == nil {
					activeConnectionMutex.Lock()
					activeConnection.WriteJSON(string(payloadStr))
					activeConnectionMutex.Unlock()
				}

				if readyForQueue.Load() {
					continue
				}

				if len(websocketQueue) <= 1 {
					finishActiveConnection <- true
					continue
				}

				// we need to kill it if somone new joins
				timerExpired.Store(true)
			case websocketUpdate := <-websocketChannel:
				println("new update msg")
				switch websocketUpdate.State {
				case WebsocketConnect:
					websocketStatuses[websocketUpdate.Socket] = true
					websocketQueue = append(websocketQueue, websocketUpdate.Socket)

					connectedCount++
				case WebsocketDisconnect:
					connectedCount--

					removeIndex := slices.IndexFunc(websocketQueue, func(conn *websocket.Conn) bool { return websocketUpdate.Socket == conn })
					if removeIndex != -1 {
						websocketQueue = append(websocketQueue[:removeIndex], websocketQueue[removeIndex+1:]...)
						delete(websocketStatuses, websocketUpdate.Socket)
					}
				}

				var first *websocket.Conn = nil
				if len(websocketQueue) != 0 {
					first = websocketQueue[0]
				}

				for connection := range websocketStatuses {
					broadcastMessage := QueueBroadcastType{
						Type:             "queue_broadcast",
						ConnectedClients: connectedCount,
						IsProccessing:    connection == first,
					}

					broadcastMarshalled, err := json.Marshal(broadcastMessage)
					if err != nil {
						fmt.Println("error: ", err.Error())
						continue
					}
					err = connection.WriteJSON(string(broadcastMarshalled))

					if err != nil {
						connection.Close()

						websocketChannel <- WebsocketMessage{Socket: connection, State: WebsocketDisconnect}
						continue
					}
				}

				if timerExpired.Load() && len(websocketQueue) > 1 && !readyForQueue.Load() {
					timerExpired.Store(false)
					finishActiveConnection <- true
				}

				if readyForQueue.Load() && len(websocketStatuses) != 0 {
					activeChannel <- WebsocketMessage{Socket: first, State: WebsocketConnect}
					readyForQueue.Store(false)

					lockoutTimer.Reset(time.Second * 60 * 5)
					println("debug: started timer")
				}
			}
		}

		for connection := range websocketStatuses {
			connection.Close()
		}
	}()

	http.HandleFunc("/queue", func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Print("upgrade:", err)
			return
		}

		authKey, hasAuthKey := r.Header["Authorization"]
		if !hasAuthKey {
			payload := UpdateType{
				Type:        "error",
				Description: "must have Authorization header set"}

			payloadStr, err := json.Marshal(payload)
			if err != nil {
				c.WriteJSON(string(payloadStr))
			}

			c.Close()
			return
		}

		tx, err := db.Begin()
		if err != nil {
			payload := UpdateType{
				Type:        "error",
				Description: err.Error()}

			payloadStr, err := json.Marshal(payload)
			if err != nil {
				c.WriteJSON(string(payloadStr))
			}

			c.Close()
			return
		}

		stmt, err := tx.Prepare(`SELECT access_key, auth_type, name, time_created FROM "access_tokens" WHERE access_key = ?`)
		if err != nil {
			payload := UpdateType{
				Type:        "error",
				Description: err.Error()}

			payloadStr, err := json.Marshal(payload)
			if err != nil {
				c.WriteJSON(string(payloadStr))
			}

			c.Close()
			return
		}

		defer stmt.Close()

		row := stmt.QueryRow(authKey[0])

		var returnInfo InfoType

		err = row.Scan(&returnInfo.AccessKey, &returnInfo.AuthType, &returnInfo.Name, &returnInfo.TimeCreated)

		if err != nil {
			payload := UpdateType{
				Type:        "error",
				Description: err.Error()}

			payloadStr, err := json.Marshal(payload)
			if err != nil {
				c.WriteJSON(string(payloadStr))
			}

			c.Close()
			return
		}

		err = c.WriteJSON(`{"type": "connected"}`)
		if err != nil {
			return
		}

		readStructureStr := ""
		err = c.ReadJSON(&readStructureStr)
		if err != nil {
			return
		}

		readStructure := map[string]any{}

		err = json.Unmarshal([]byte(readStructureStr), &readStructure)
		if err != nil {
			payload := UpdateType{
				Type:        "error",
				Description: "unmarshal error on servers fault"}

			payloadStr, err := json.Marshal(payload)
			if err != nil {
				c.WriteJSON(string(payloadStr))
			}

			c.Close()
			return
		}

		messageType, hasType := readStructure["type"]
		if !hasType {
			payload := UpdateType{
				Type:        "error",
				Description: "type parsing error, server sent invalid packet"}

			c.WriteJSON(payload)
			c.Close()
			return
		}

		if messageType != "upload" {
			payload := UpdateType{
				Type:        "error",
				Description: "first packet must be error"}

			payloadStr, err := json.Marshal(payload)
			if err != nil {
				c.WriteJSON(string(payloadStr))
			}

			c.Close()
			return
		}

		messageFile, hasFile := readStructure["file"]
		if !hasFile {
			payload := UpdateType{
				Type:        "error",
				Description: "you must upload a file"}

			payloadStr, err := json.Marshal(payload)
			if err != nil {
				c.WriteJSON(string(payloadStr))
			}

			c.Close()
			return
		}

		if len(messageFile.(string)) > 1000000 /* 1mb */ {
			payload := UpdateType{
				Type:        "error",
				Description: "file content must be smaller than 1mb"}

			payloadStr, err := json.Marshal(payload)
			if err != nil {
				c.WriteJSON(string(payloadStr))
			}

			c.Close()
			return
		}

		messageFileReader := strings.NewReader(messageFile.(string))

		fileParsedReader := base64.NewDecoder(base64.RawStdEncoding, messageFileReader)
		fileParsed, err := io.ReadAll(fileParsedReader)

		fileDBMutex.Lock()

		fileDB[c] = fileParsed

		fileDBMutex.Unlock()

		// ownership gets passed to the queue
		websocketChannel <- WebsocketMessage{Socket: c, State: WebsocketConnect}
		//
		// websocketChanel <- WebsocketMessage{Socket: c, State: WebsocketDisconnect}
	})

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

		fmt.Fprintf(w, "Run `gated-community set-key %q`", userIdSum)
	})

	http.HandleFunc("/api/v1/get_info", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Content-Type", "application/json")

		if r.Method != "GET" {
			payload := UpdateType{
				Type:        "error",
				Description: "must be a GET"}
			payloadStr, _ := json.Marshal(payload)

			w.Write(payloadStr)
			return
		}

		authKey, hasAuthKey := r.Header["Authorization"]
		if !hasAuthKey {
			payload := UpdateType{
				Type:        "error",
				Description: "must have Authorization header set"}
			payloadStr, _ := json.Marshal(payload)

			w.Write(payloadStr)
			return
		}

		tx, err := db.Begin()
		if err != nil {
			payload := UpdateType{
				Type:        "error",
				Description: err.Error()}
			payloadStr, _ := json.Marshal(payload)

			w.Write(payloadStr)
			return
		}

		stmt, err := tx.Prepare(`SELECT access_key, auth_type, name, time_created FROM "access_tokens" WHERE access_key = ?`)
		if err != nil {
			payload := UpdateType{
				Type:        "error",
				Description: err.Error()}
			payloadStr, _ := json.Marshal(payload)

			w.Write(payloadStr)
			return
		}

		defer stmt.Close()

		row := stmt.QueryRow(authKey[0])

		var returnInfo InfoType

		err = row.Scan(&returnInfo.AccessKey, &returnInfo.AuthType, &returnInfo.Name, &returnInfo.TimeCreated)

		if err != nil {
			payload := UpdateType{
				Type:        "error",
				Description: err.Error()}
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
