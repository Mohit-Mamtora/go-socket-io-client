package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	socketIO "github.com/Mohit-Mamtora/go-socket-io-client"
	"github.com/Mohit-Mamtora/go-socket-io-client/l"
)

var WebSocketClient *socketIO.WebSocketClient

type _WebsocketMonitor struct {
	autoConnectContext context.Context
	autoConnectCancel  context.CancelFunc

	Schema   string
	Host     string
	Path     string
	RawQuery string

	websocketClosed chan bool
}

var WebsocketMonitor _WebsocketMonitor

func InitiateWebsocket(scheme, host, path, rawQuery string, autoReconnect bool) error {

	WebsocketMonitor.websocketClosed = make(chan bool, 2)
	WebSocketClient = socketIO.Initiate(scheme, host, path, rawQuery, WebsocketMonitor.websocketClosed)
	WebSocketClient.On(socketIO.ON_CONNECTED, connected)
	WebSocketClient.On(socketIO.ON_DISCONNECTED, disconnected)
	WebSocketClient.On("start_capture", startCapture)
	WebSocketClient.On("refresh_profile", refreshProfile)
	WebSocketClient.On("start-call", startMeeting)

	if autoReconnect {
		WebsocketMonitor.autoConnectContext, WebsocketMonitor.autoConnectCancel = context.WithCancel(context.Background())
		WebsocketMonitor.Schema = scheme
		WebsocketMonitor.Host = host
		WebsocketMonitor.Path = path
		WebsocketMonitor.RawQuery = rawQuery

		go WebsocketMonitor.autoReconnect()
		err := WebSocketClient.StartSocket()
		if err != nil {
			return err
		}

	} else {
		err := WebSocketClient.StartSocket()
		if err != nil {
			return err
		}
	}

	return nil
}

func (websocketMonitor *_WebsocketMonitor) autoReconnect() {
	for {
		select {
		case <-WebsocketMonitor.autoConnectContext.Done():
			return
		case <-websocketMonitor.websocketClosed:
			l.P("in auto connect")
			time.Sleep(time.Second * 5)
			err := WebSocketClient.StartSocket()
			if err != nil {
				l.E(err)
			}
			time.Sleep(time.Second * 10)
		}
	}
}

func IsOnline() bool {
	return !WebSocketClient.IsClosed()
}

func StopWebsocket() {
	if WebsocketMonitor.autoConnectCancel != nil {
		WebsocketMonitor.autoConnectCancel()
		WebSocketClient.Shutdown()
	}
}

func AddWSListener(eventName string, event func(socketIO.OnEventMessage)) {
	WebSocketClient.On(eventName, event)
}

func WSEmit(eventName string, data string) {
	WebSocketClient.Emit(eventName, data)
}

// ---------- pub sub listeners ----------------

func connected(message socketIO.OnEventMessage) {
	l.P("connected")
}

func disconnected(message socketIO.OnEventMessage) {
	l.P("disconnected")
}

func startCapture(message socketIO.OnEventMessage) {

	data, err := json.Marshal(message)
	if err != nil {
		l.E(err)
		return
	}
	WebSocketClient.Emit("ss_complete", string(data))
}

func refreshProfile(message socketIO.OnEventMessage) {
}

func startMeeting(message socketIO.OnEventMessage) {
	fmt.Println(message)
}
