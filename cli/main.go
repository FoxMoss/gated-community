package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"

	"os/exec"
	"runtime"

	"github.com/alecthomas/kong"
	"github.com/containerd/console"
	"github.com/gorilla/websocket"
	"github.com/spf13/viper"

	tea "charm.land/bubbletea/v2"
)

// https://gist.github.com/sevkin/9798d67b2cb9d07cb05f89f14ba682f8
func OpenInBrowser(url string) error {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "windows":
		cmd = "cmd"
		args = []string{"/c", "start"}
	case "darwin":
		cmd = "open"
	default: // "linux", "freebsd", "openbsd", "netbsd"
		cmd = "xdg-open"
	}
	args = append(args, url)
	return exec.Command(cmd, args...).Start()
}

func GetInfo(accessKey string) (map[string]any, error) {
	client := &http.Client{}

	request, err := http.NewRequest(http.MethodGet, "http://localhost:8080/api/v1/get_info", nil)
	if err != nil {
		return nil, err
	}

	request.Header.Add("Authorization", fmt.Sprintf("%v", accessKey))

	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}

	infoResponseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}

	var decodedInfoResponse map[string]any

	err = json.Unmarshal(infoResponseBody, &decodedInfoResponse)

	if err != nil {
		return nil, err
	}

	errorResponse, hasError := decodedInfoResponse["error_description"]

	if hasError {
		return nil, errors.New(errorResponse.(string))
	}

	return decodedInfoResponse, nil
}

type Context struct {
}

type UploadCmd struct {
	Path string `arg:"" name:"path" help:"Paths to remove." type:"path"`
}

type UIState struct {
	status           string
	errorMessage     string
	ptyName          string
	currentPlace     uint64
	connectedClients uint64
}

func (m model) QueueConnection(r *UploadCmd, authKey string) error {
	fileBuffer, err := os.ReadFile(r.Path)

	if err != nil {
		m.stateChan <- UIState{status: "error", errorMessage: err.Error()}
		return err
	}

	url := url.URL{Scheme: "ws", Host: "127.0.0.1:8080", Path: "/queue"}

	requestHeader := http.Header{}
	requestHeader.Add("Authorization", fmt.Sprintf("%v", authKey))

	c, _, err := websocket.DefaultDialer.Dial(url.String(), requestHeader)
	if err != nil {
		m.stateChan <- UIState{status: "error", errorMessage: err.Error()}
		return err
	}

	defer func() {
		type CloseType struct {
			Type        string `json:"type"`
			Description string `json:"description"`
		}

		payload := CloseType{
			Type:        "close",
			Description: "client closing gracefully"}

		payloadStr, err := json.Marshal(payload)
		if err != nil {
			c.WriteJSON(string(payloadStr))
		}

		c.Close()
	}()

	type UploadType struct {
		Type string `json:"type"`
		File []byte `json:"file"`
	}

	fileUpload := UploadType{
		Type: "upload",
		File: fileBuffer,
	}

	uploadBuffer, err := json.Marshal(fileUpload)

	if err != nil {
		m.stateChan <- UIState{status: "error", errorMessage: err.Error()}
		return err
	}

	err = c.WriteJSON(string(uploadBuffer))

	if err != nil {
		m.stateChan <- UIState{status: "error", errorMessage: err.Error()}
		return err
	}

	consoleInterface, slave, err := console.NewPty()
	if err != nil {
		m.stateChan <- UIState{status: "error", errorMessage: err.Error()}
		return err
	}

	defer consoleInterface.Close()

	go func() {
		buffer := make([]byte, 1024)
		for {
			bufferLen, err := consoleInterface.Read(buffer)

			if err != nil {
				m.stateChan <- UIState{status: "warning", errorMessage: "error reading pty\n" + err.Error()}
				continue
			}

			returnArray := make([]int, bufferLen)
			for item := 0; item < bufferLen; item++ {
				returnArray[item] = int(buffer[item])
			}

			type TransferType struct {
				Type string `json:"type"`
				Data []int  `json:"data"`
			}

			transferMessage := TransferType{
				Type: "ingress",
				Data: returnArray,
			}

			transferMarshalled, err := json.Marshal(transferMessage)

			if err != nil {
				m.stateChan <- UIState{status: "error", errorMessage: err.Error()}
				continue
			}

			err = c.WriteJSON(string(transferMarshalled))

			if err != nil {
				break
			}

		}
	}()

	for {
		readStructureStr := ""

		err = c.ReadJSON(&readStructureStr)

		if err != nil {
			m.stateChan <- UIState{status: "error", errorMessage: err.Error()}
			break
		}

		readStructure := map[string]any{}

		err = json.Unmarshal([]byte(readStructureStr), &readStructure)
		if err != nil {
			m.stateChan <- UIState{status: "error", errorMessage: err.Error()}
			break
		}

		messageType, hasType := readStructure["type"]
		if !hasType {
			m.stateChan <- UIState{status: "error", errorMessage: "type parsing error, server sent invalid packet"}
			break
		}

		switch messageType {
		case "error":
			errorDescription, hasDescription := readStructure["description"]
			if hasDescription {
				m.stateChan <- UIState{status: "error", errorMessage: errorDescription.(string)}
				break
			} else {
				m.stateChan <- UIState{status: "error", errorMessage: "undeclared error from server"}
				break
			}
		case "status":
			description, hasDescription := readStructure["description"]
			if hasDescription {
				m.stateChan <- UIState{status: description.(string), ptyName: slave}
				break
			}
		case "queue_broadcast":
			connectedClients, hasConnectedClients := readStructure["connected_clients"]
			currentPlace, hasCurrentPlace := readStructure["current_place"]

			if hasConnectedClients && hasCurrentPlace {
				m.stateChan <- UIState{status: "waiting", connectedClients: uint64(connectedClients.(float64)), currentPlace: uint64(currentPlace.(float64))}
				break
			}
		case "egress":
			data, hasData := readStructure["data"]
			if !hasData {
				m.stateChan <- UIState{status: "error", errorMessage: "parsing webscoket serial data failed"}
				break
			}

			dataBytes := make([]byte, len(data.([]any)))
			for item := 0; item < len(data.([]any)); item++ {
				dataBytes[item] = byte(data.([]any)[item].(float64))
			}

			_, err = consoleInterface.Write(dataBytes)
			if err != nil {
				m.stateChan <- UIState{status: "warn", errorMessage: err.Error()}
			}

		default:
		}
	}

	return nil
}

func (r *UploadCmd) Run(ctx *Context) error {
	authKey := viper.GetString("AuthKey")

	if authKey == "" {
		fmt.Println("No auth key, run:\ngated-community auth")
		return nil
	}

	m := model{}

	m.stateChan = make(chan UIState)

	p := tea.NewProgram(m)

	go m.QueueConnection(r, authKey)

	if _, err := p.Run(); err != nil {
		fmt.Printf("error: %v", err)
		os.Exit(1)
	}

	return nil
}

type HCACmd struct {
}

func (r *HCACmd) Run(ctx *Context) error {
	print("Opening auth link in browser, hit enter to continue...")

	stdinBuffer := []byte{0}
	os.Stdin.Read(stdinBuffer)

	OpenInBrowser("https://auth.hackclub.com/oauth/authorize?client_id=55e72dce07e7645a7d66e11a8b18a88d&redirect_uri=http%3A%2F%2Flocalhost%3A8080%2Fauth&response_type=code&scope=openid+email+name+profile+birthdate+verification_status+slack_id+basic_info")

	println("Opened!")

	return nil
}

type SetKeyCmd struct {
	Key string `arg:"" name:"key" help:"User key to be stored for identification"`
}

func (r *SetKeyCmd) Run(ctx *Context) error {
	info, err := GetInfo(r.Key)

	if err != nil {
		return err
	}

	fmt.Printf("Hello %s! You have been authed with %s.\n", info["name"].(string), info["auth_type"].(string))

	viper.Set("AuthKey", r.Key)
	err = viper.WriteConfig()
	if err != nil {
		return err
	}

	return nil
}

type StatusCmd struct {
}

func (r *StatusCmd) Run(ctx *Context) error {
	authKey := viper.GetString("AuthKey")

	if authKey == "" {
		fmt.Println("No auth key, run:\ngated-community auth")
		return nil
	}

	info, err := GetInfo(authKey)

	if err != nil {
		return err
	}

	fmt.Printf("Name: %s\nAuth: %s\n", info["name"].(string), info["auth_type"].(string))

	return nil
}

var CLI struct {
	Upload UploadCmd `cmd:"" help:"Upload bitstream to be run"`
	Auth   HCACmd    `cmd:"" help:"Connect with Hack Club Auth"`
	SetKey SetKeyCmd `cmd:"" help:"Store user key"`
	Status StatusCmd `cmd:"" help:"Check auth details"`
}

func main() {
	viper.SetConfigName("config")
	viper.SetConfigType("json")

	home, err := os.UserHomeDir()

	if err != nil {
		log.Fatal(err.Error())
	}

	gatedPath := fmt.Sprintf("%v/.gated-community", home)
	gatedConfigPath := fmt.Sprintf("%v/.gated-community/config", home)

	viper.AddConfigPath(gatedPath)

	os.MkdirAll(gatedPath, fs.ModeDir|fs.ModePerm)

	err = viper.ReadInConfig()

	if err != nil {
		// couldn't read file so we need to make file
		_, err = os.Create(gatedConfigPath)

		if err != nil {
			log.Fatal(err.Error())
		}
	}

	ctx := kong.Parse(&CLI)
	err = ctx.Run(&Context{})
	ctx.FatalIfErrorf(err)
}
