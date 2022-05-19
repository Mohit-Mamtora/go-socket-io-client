package engineIOCodes

import "strconv"

const (
	OPEN    = 0
	CLOSE   = 1
	PING    = 2
	PONG    = 3
	MESSAGE = 4
	UPGRADE = 5
	NOOP    = 6
)

var PONG_BINARY = []byte(strconv.Itoa(3))
