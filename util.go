package main

import (
	"crypto/sha256"
	"fmt"
	"io"
	"log"
	"os"
)

func getEnv(key string, def string) string {
	v := os.Getenv(key)
	if len(v) == 0 {
		return def
	}
	return v
}

func calcPassHash(password, hash string) string {
	h := sha256.New()
	io.WriteString(h, password)
	io.WriteString(h, ":")
	io.WriteString(h, hash)
	return fmt.Sprintf("%x", h.Sum(nil))
}

func must(err error) {
	if err != nil {
		log.Panic(err)
	}
}
