package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"

	"os/exec"
	"runtime"

	"github.com/alecthomas/kong"
	"github.com/spf13/viper"
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

func (r *UploadCmd) Run(ctx *Context) error {
	fileDescriptor, err := os.Open(r.Path)

	if err != nil {
		return err
	}

	reader := bufio.NewReader(fileDescriptor)

	req, err := http.NewRequest(http.MethodPost, "http://0.0.0.0:8080/upload_bitstream", reader)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)

	if err != nil {
		return err
	}

	println(string(respBody))

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

	return nil
}

var CLI struct {
	Upload UploadCmd `cmd:"" help:"Upload bitstream to be run"`
	Auth   HCACmd    `cmd:"" help:"Connect with Hack Club Auth"`
	SetKey SetKeyCmd `cmd:"" help:"Store user key"`
}

func main() {
	viper.SetConfigName("config")

	viper.AddConfigPath("/etc/appname/")
	viper.AddConfigPath("$HOME/.appname")
	viper.AddConfigPath(".")

	err := viper.ReadInConfig()

	ctx := kong.Parse(&CLI)
	err = ctx.Run(&Context{})
	ctx.FatalIfErrorf(err)
}
