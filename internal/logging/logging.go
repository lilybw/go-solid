package logging

import (
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"log"

	. "github.com/lilybw/go-solid/shared/logging"
)

// Mirrors shared/logging.DEFAULT_LEVEL, so anything logged before the first
// SetLevel obeys the same quiet default.
var level = DEFAULT_LEVEL

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

	mashalled, err := json.Marshal(object, jsontext.WithIndentPrefix(" "), jsontext.WithIndent("   "))
	if err != nil {
		log.Println("Failed to marshal logged object with message: " + msg + "\n\t" + err.Error())
		return
	}
	log.Println(msg + "\n" + string(mashalled))
}
