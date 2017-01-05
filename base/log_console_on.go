//+build !consoleoff

package base

import (
	"log"
)

func consoleLog(format string, v ...interface{}) {
	log.Printf(format, v...)
}
