package main

import (
	"log"
	"os"
	"path/filepath"
	"time"
)

func main() {
	for {
		now := time.Now().Format("2006-01-02-150405")
		path := filepath.Join("/tmp", now)
		log.Printf("writing file: %s\n", path)
		err := os.WriteFile(path, []byte{}, 0o600)
		if err != nil {
			log.Printf("ERROR create file: %v", err)
		}
		time.Sleep(5 * time.Second)
	}
}
