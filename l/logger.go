package l

import (
	"log"
	"os"
)

func SetOutput(logFile *os.File) {
	log.SetOutput(logFile)
}
func P(message interface{}) {
	log.Println(message)
}

func E(message interface{}) {
	log.Println(message)
}
