package main

import (
	"time"

	"github.com/gorilla/websocket"
)

type OAuthToken struct {
	ClientId     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	RedirectUri  string `json:"redirect_uri"`
	Code         string `json:"code"`
	GrantType    string `json:"grant_type"`
}

type InfoType struct {
	AccessKey   string `json:"access_key"`
	AuthType    string `json:"auth_type"`
	Name        string `json:"name"`
	TimeCreated int    `json:"time_created"`
}

type UpdateType struct {
	Type        string `json:"type"`
	Description string `json:"description"`
}

type QueueBroadcastType struct {
	Type             string `json:"type"`
	ConnectedClients uint64 `json:"connected_clients"`
	CurrentPlace     uint64 `json:"current_place"`
	IsProccessing    bool   `json:"is_proccessing"`
}

type TransferType struct {
	Type string `json:"type"`
	Data []int  `json:"data"`
}

type FileCacheMessage struct {
	Delete     bool
	Connection *websocket.Conn
	File       []byte
}

const timerLength = time.Minute * 10
