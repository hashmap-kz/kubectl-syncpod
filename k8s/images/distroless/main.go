package main

import (
	"log"
	"os"
	"path/filepath"
	"time"
)

func main() {
	for {
		log.Println(time.Now().Format(time.DateTime))
		time.Sleep(5 * time.Second)
		err := os.WriteFile(filepath.Join("/tmp", time.Now().Format("2006-01-02-150405")), []byte{}, 0o600)
		if err != nil {
			log.Printf("ERROR create file: %v", err)
		}
	}
}
