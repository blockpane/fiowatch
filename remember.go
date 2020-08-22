package fiowatch

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"time"
)

type savedHost struct {
	Api string `json:"api"`
	P2p string `json:"p2p"`
}

const settingsDir = "org.frameloss.fiowatch"

// SaveHost tries to save the connection details for next start, best effort only
func SaveHost(apiUrl string, p2p string) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		log.Println(err)
		return
	}
	dirName := fmt.Sprintf("%s%c%s", configDir, os.PathSeparator, settingsDir)
	_, err = os.Stat(dirName)
	if err != nil {
		// try to create dir:
		if err = os.Mkdir(dirName, os.FileMode(0700)); err != nil {
			log.Println(err)
			return
		}
	}
	fileBytes, _ := json.Marshal(savedHost{
		Api: apiUrl,
		P2p: p2p,
	})
	fileName := fmt.Sprintf("%s%c%s%c%s", configDir, os.PathSeparator, settingsDir, os.PathSeparator, "host.json")
	f, err := os.OpenFile(fileName, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		log.Println(err)
		return
	}
	defer f.Close()
	_ = f.SetWriteDeadline(time.Now().Add(time.Second))
	n, err := f.Write(fileBytes)
	if err != nil {
		log.Println(err)
		return
	}
	log.Println("wrote", n, "bytes to host.json")
}

func GetHost() (apiUrl string, p2p string) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		log.Println(err)
		return
	}
	fileName := fmt.Sprintf("%s%c%s%c%s", configDir, os.PathSeparator, settingsDir, os.PathSeparator, "host.json")
	f, err := os.Open(fileName)
	if err != nil {
		log.Println(err)
		return
	}
	defer f.Close()
	b, err := ioutil.ReadAll(f)
	if err != nil {
		log.Println(err)
		return
	}
	host := &savedHost{}
	err = json.Unmarshal(b, host)
	if err != nil {
		log.Println(err)
	}
	return host.Api, host.P2p
}
