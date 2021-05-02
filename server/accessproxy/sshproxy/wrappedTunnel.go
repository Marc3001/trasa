package sshproxy

import (
	"fmt"
	"github.com/gorilla/websocket"
	"github.com/seknox/trasa/server/utils"
	"github.com/sirupsen/logrus"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"time"
)

const STANDBY_TIMEOUT = time.Minute * 15

//WrappedTunnel wraps upstream(backend) ssh connection and writes data to session file,guests
//It also writes data coming from guests to upstream(backend) ssh connection
type WrappedTunnel struct {
	backend       io.ReadWriteCloser
	frontend      io.ReadWriteCloser
	sessionRecord bool
	tempLogFile   *os.File
	guests        []*websocket.Conn
	timer         *time.Timer
	closed        bool
}

func NewWrappedTunnel(sessionID string, sessionRecord bool, backend io.ReadWriteCloser, frontend io.ReadWriteCloser, guestChan chan GuestClient) (*WrappedTunnel, error) {

	err := os.MkdirAll(filepath.Join(utils.GetTmpDir(), "trasa", "accessproxy", "ssh"), 0644)
	if err != nil {
		logrus.Error(err)
		return nil, err
	}

	tunn := &WrappedTunnel{
		backend:       backend,
		frontend:      frontend,
		sessionRecord: sessionRecord,
		guests:        nil,
		closed:        false,
	}

	tunn.timer = time.AfterFunc(STANDBY_TIMEOUT, func() {
		logrus.Debug("Timeout after no interaction")
		tunn.closed = true
		tunn.Close()
	})

	if sessionRecord {
		tunn.tempLogFile, err = os.OpenFile(filepath.Join(utils.GetTmpDir(), "trasa", "accessproxy", "ssh", sessionID+".session"), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0644)
		if err != nil {
			logrus.Error(err)
			return nil, err
		}

	}

	go tunn.ListenToNewGuests(guestChan)
	return tunn, nil
}

func (lr *WrappedTunnel) ListenToNewGuests(guestChan chan GuestClient) {
	defer func() {
		if r := recover(); r != nil {
			logrus.Error(r, string(debug.Stack()))
		}
	}()
	for v := range guestChan {
		//Append wesocket connection to list of viewers
		lr.guests = append(lr.guests, v.Conn)

		//TODO
		//Append guest email to  log
		//lr.proxyMeta.Log.Guests = append(lr.proxyMeta.Log.Guests, v.Email)
		go lr.readFromGuests(v.Conn)
		logrus.Debug("New viewer joined")
	}
}

func (lr *WrappedTunnel) readFromGuests(guest *websocket.Conn) {
	defer func() {
		r := recover()
		if r != nil {
			logrus.Error(r, string(debug.Stack()))
		}
	}()
	for {
		_, data, err := guest.ReadMessage()
		if err != nil {
			logrus.Debugf(`Could not read from viewer, disconnected: %v`, err)
			return
		}
		lr.backend.Write(data)
	}

}

func (lr *WrappedTunnel) Read(p []byte) (n int, err error) {
	n, err = lr.backend.Read(p)

	//Write to file
	if lr.sessionRecord {
		lr.tempLogFile.WriteString(fmt.Sprintf("%s", string(p[:n])))
	}

	///Write to guest
	for _, guest := range lr.guests {
		if guest != nil {
			guest.WriteMessage(1, p[:n])
		}
	}

	if n > 0 {
		lr.timer.Reset(time.Minute)
	}

	return n, err
}

func (lr *WrappedTunnel) pipe() {
	go func() {
		for !lr.closed {
			buff := make([]byte, 100)
			n, err := lr.backend.Read(buff)
			if err != nil {
				logrus.Debug(err)
				return
			}
			if n > 0 {
				lr.timer.Reset(STANDBY_TIMEOUT)
				_, err := lr.frontend.Write(buff[:n])
				if err != nil {
					logrus.Debug(err)
					return
				}
			}
		}
	}()

	func() {
		for !lr.closed {
			buff := make([]byte, 100)
			n, err := lr.frontend.Read(buff)
			if err != nil {
				logrus.Debug(err)
				return
			}
			if n > 0 {
				lr.timer.Reset(STANDBY_TIMEOUT)
				_, err := lr.backend.Write(buff[:n])
				if err != nil {
					logrus.Debug(err)
					return
				}
			}
		}
	}()

	logrus.Debug("Session Ended")
}

func (lr *WrappedTunnel) Close() (err error) {
	logrus.Debug("closing tunnel")
	lr.tempLogFile.Close()

	for _, v := range lr.guests {
		if v != nil {
			v.Close()
		}
	}
	lr.timer.Stop()

	e := lr.backend.Close()
	if e != nil {
		logrus.Error(e)
		err = e
	}

	e = lr.frontend.Close()
	if e != nil {
		logrus.Error(e)
		err = e
	}
	return err

}
