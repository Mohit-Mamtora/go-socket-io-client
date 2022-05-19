package socketio_client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/Mohit-Mamtora/go-socket-io-client/engineIOCodes"
	"github.com/Mohit-Mamtora/go-socket-io-client/l"
	"github.com/Mohit-Mamtora/go-socket-io-client/socketIOCodes"
)

type Socket struct {
	Sid          string `json:"sid"`
	PingInterval int    `json:"pinginterval"`
	PingTimeout  int    `json:"pingtimeout"`
}

type OnEventMessage map[string]interface{}
type OnEvents map[string]func(OnEventMessage)

const (
	ON_CONNECTED    = "connected"
	ON_DISCONNECTED = "disconnected"
)

type WebSocketClient struct {
	configStr   string
	sendBuf     chan []byte
	responseBuf chan []byte

	ctx       context.Context
	ctxCancel context.CancelFunc

	wsContext       context.Context
	wsContextCancel context.CancelFunc

	mu     sync.RWMutex
	wsconn *websocket.Conn

	socket          Socket
	onEvent         OnEvents
	websocketClosed chan bool
}

// NewWebSocketClient create new websocket connection
func Initiate(schema, host, channel string, rawQuery string, websocketClosed chan bool) *WebSocketClient {

	u := url.URL{Scheme: schema, Host: host, Path: channel, RawQuery: rawQuery}
	// fmt.Println("Created URL", u.String())

	conn := WebSocketClient{
		sendBuf:         make(chan []byte, 10),
		responseBuf:     make(chan []byte, 100),
		configStr:       u.String(),
		websocketClosed: websocketClosed,
	}

	conn.onEvent = make(OnEvents, 4)
	conn.onEvent["defaultEvent"] = defaultEvent
	conn.onEvent[ON_CONNECTED] = connected
	conn.onEvent[ON_DISCONNECTED] = disconnected
	return &conn
}

func (conn *WebSocketClient) StartSocket() error {
	if conn.ctxCancel != nil {
		conn.ctxCancel()
	}
	conn.ctx, conn.ctxCancel = context.WithCancel(context.Background())
	conn.wsContext, conn.wsContextCancel = context.WithCancel(context.Background())

	// try connecting
	_, err := conn.GetSocket()
	if err != nil {
		conn.websocketClosed <- true
		return err
	}
	go conn.listenServerMessages()
	return nil
}

func (conn *WebSocketClient) GetSocket() (*websocket.Conn, error) {
	conn.mu.Lock()
	defer conn.mu.Unlock()

	if conn.wsconn != nil {
		return conn.wsconn, nil
	}

	ticker := time.NewTicker(time.Second * 5)
	defer ticker.Stop()

	for ; ; <-ticker.C {

		select {

		case <-conn.ctx.Done():
			return nil, errors.New("socket expired")

		default:
			ws, _, err := websocket.DefaultDialer.Dial(conn.configStr, nil)
			if err != nil {
				return nil, err
			}
			conn.wsconn = ws
			l.P(fmt.Sprintf("connected to websocket to %s", conn.configStr) + "connect")
			go conn.writeBuffer()
			return conn.wsconn, nil
		}

	}
}

// ---------------------- GO ROUTINE FUNCTIONS ------------------
//TODO
// if no socket found then listenServerMessages() will try to re connect with socket
func (conn *WebSocketClient) listenServerMessages() {

	timer := time.NewTicker(time.Second)
	for {

		select {
		case <-conn.ctx.Done():
			time.Sleep(time.Second * 5)
			continue
		case <-timer.C:
			if conn.wsconn == nil {
				time.Sleep(time.Second * 5)
				continue
			}
			_, bytMsg, err := conn.wsconn.ReadMessage()

			if err != nil {
				l.E(err.Error() + "listenServerMessages error")
				conn.closeWs()
				time.Sleep(time.Second * 5)
				continue
			}
			l.P(string(bytMsg) + "listenServerMessages")
			conn.responseHandler(bytMsg)
		}
	}

}

func (conn *WebSocketClient) writeBuffer() {
	for data := range conn.sendBuf {
		wsconn := conn.wsconn
		if wsconn == nil {
			return
		}

		err := conn.wsconn.WriteMessage(websocket.TextMessage, data)
		if err != nil {
			conn.closeWs()
			l.E("WebSocket Write Error" + err.Error() + "writeBuffer")
			return
		} else {
			l.P(fmt.Sprintf("send: %s", data) + "writeBuffer")
		}
	}
}

func (conn *WebSocketClient) ping() {
	ticker := time.NewTicker(time.Second * time.Duration(conn.socket.PingInterval/1000))
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if conn.wsconn == nil {
				l.E("No websocket connection" + " ping")
				return
			}

			err := conn.wsconn.WriteMessage(websocket.TextMessage, engineIOCodes.PONG_BINARY)

			if err != nil {
				conn.closeWs()
				l.E("WebSocket Write Error" + err.Error() + "- ping")
				return
			}
		case <-conn.wsContext.Done():
			return
		}
	}
}

// --------------------- ^ -----------------------

// Write data to the websocket server
func (conn *WebSocketClient) Write(payload interface{}) error {

	if conn.wsconn == nil {
		return errors.New("socket expired; try to re-initiate websocket again")
	}

	var data []byte
	switch payload := payload.(type) {
	case int:
		data = []byte(strconv.Itoa(payload))
	case string:
		data = []byte(payload)
	case []byte:
		data = payload
	default:
		data, _ = json.Marshal(payload)
	}
	ticker := time.NewTicker(time.Second * 20)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			return errors.New("buffer timeout")
		case conn.sendBuf <- data:
			return nil
		case <-conn.ctx.Done():
			return errors.New("socket expired; try to re-initiate websocket again")
		}
	}
}

// Close will send close message and shutdown websocket connection
func (conn *WebSocketClient) Shutdown() {
	if conn.ctx.Err() == nil {
		conn.ctxCancel() // closing context
		conn.closeWs()
	}
}

func (conn *WebSocketClient) IsClosed() bool {
	return conn.wsconn == nil
}

// Close will send close message and shutdown websocket connection
func (conn *WebSocketClient) closeWs() {
	conn.mu.Lock()
	if conn.wsconn != nil {
		conn.wsconn.Close()
		conn.wsconn = nil
		conn.wsContextCancel()
		conn.executeEvent(ON_DISCONNECTED, nil)
		conn.websocketClosed <- true
	}
	conn.mu.Unlock()
}

// ====================  socket io message writer ===========================

// handling first time handshake ping, event
func (conn *WebSocketClient) responseHandler(message []byte) {
	if len(message) == 0 {
		return
	}
	engineIOcode, _ := strconv.Atoi(string(message[0]))

	switch engineIOcode {

	case engineIOCodes.OPEN:
		err := json.Unmarshal(message[1:], &conn.socket)
		if err != nil {
			l.E(err)
			return
		}
		err = conn.doAuthentication(fmt.Sprintf("%d%d", engineIOCodes.MESSAGE, socketIOCodes.CONNECT))
		if err != nil {
			l.E(err)
		} else {
			go conn.ping()
		}

	case engineIOCodes.MESSAGE:

		socketIOCode, _ := strconv.Atoi(string(message[1]))
		switch socketIOCode {

		case socketIOCodes.CONNECT:
			err := json.Unmarshal(message[2:], &conn.socket)
			if err != nil {
				l.E(err)
			}
			conn.executeEvent(ON_CONNECTED, nil)
			l.P("--------session established ----------------")

		case socketIOCodes.EVENT:
			conn.executeChannelEvent(message[2:])

		case socketIOCodes.ERROR:
			// conn.closeWs()
		}

	case engineIOCodes.PING:
		l.P(string(message))

	default:
		l.P(string(message))
	}
}

func (conn *WebSocketClient) doAuthentication(message string) error {
	code, err := strconv.Atoi(message)
	if err != nil {
		l.E(err)
	}
	err = conn.Write(code)
	if err != nil {
		l.E(err)
	}
	return err
}

func (conn *WebSocketClient) executeChannelEvent(messageRaw []byte) {
	var payload []interface{}
	l.P(string(messageRaw))
	err := json.Unmarshal(messageRaw, &payload)
	if err != nil {
		l.E(err.Error() + " - executeChannelEvent")
	}
	if payload[1] == nil {
		conn.executeEvent(payload[0].(string), OnEventMessage{})
	} else {
		conn.executeEvent(payload[0].(string), payload[1].(map[string]interface{}))
	}
}

// ============== socket io interfaces =========================

func (conn *WebSocketClient) Emit(eventName, message string) error {

	return conn.Write(
		fmt.Sprintf("%d%d[\"%s\",%s]", engineIOCodes.MESSAGE, socketIOCodes.EVENT,
			eventName, message))
}

func (conn *WebSocketClient) Join(channelname string) {}

func (conn *WebSocketClient) executeEvent(eventName string, message OnEventMessage) {

	if fun, exists := (conn.onEvent)[eventName]; exists {
		go fun(message)
	} else {
		message["eventName"] = eventName
		go (conn.onEvent)["defaultEvent"](message)
	}
}

func (conn *WebSocketClient) On(eventName string, event func(OnEventMessage)) {
	(conn.onEvent)[eventName] = event
}

func connected(message OnEventMessage)    {}
func disconnected(message OnEventMessage) {}

func defaultEvent(message OnEventMessage) {
	fmt.Printf("Default event running %v \n", message)
}
