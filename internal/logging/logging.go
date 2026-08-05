package logging

import (
	"encoding/json"
	"log"

	. "github.com/lilybw/go-solid/shared/logging"
)

var level = LEVEL_DEBUG

func SetLevel(l LogLevel) {
	level = l
}

func Log(l LogLevel, msg string) {
	if l < level {
		return
	}
	log.Println(msg)
}

func LogJSON(l LogLevel, msg string, object any) {
	if l < level {
		return
	}

	mashalled, err := json.MarshalIndent(object, " ", "   ")
	if err != nil {
		log.Println("Failed to marshal logged object with message: " + msg + "\n\t" + err.Error())
		return
	}
	log.Println(string(mashalled))
}
