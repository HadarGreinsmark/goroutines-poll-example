package main

import (
	"log"
	"net"
	"bytes"
	"golang.org/x/sys/unix"
	"os"
)

func main() {
	addr, err := net.ResolveTCPAddr("tcp", ":7777")
	listener, err := net.ListenTCP("tcp", addr)
	if err != nil {
		log.Panic(err)
	}

	replierPoll(listener)
}

func replierGoroutine(listener net.Listener) {
	for {
		conn, _ := listener.Accept()
		go func() {
			buf := make([]byte, 16)
			conn.Read(buf)
			log.Printf("received: %s", buf)
			conn.Write(bytes.ToUpper(buf))
			conn.Close()
		}()
	}
}

type ConnState struct {
	connFile *os.File
	buffer   []byte
}

func replierPoll(listener *net.TCPListener) {
	epollFd, err := unix.EpollCreate(8)
	if err != nil {
		log.Panic(err)
	}

	// UNIX represents a TCP listener socket as a file
	listenerFile, err := listener.File()
	if err != nil {
		log.Panic(err)
	}

	// Add the TCP listener to the set of file descriptors being polled
	listenerPoll := unix.EpollEvent{
		Fd:     int32(listenerFile.Fd()),
		Events: unix.POLLIN, // POLLIN triggers on accept()
		Pad:    0,           // Arbitary data
	}
	err = unix.EpollCtl(epollFd, unix.EPOLL_CTL_ADD, int(listenerPoll.Fd), &listenerPoll)
	if err != nil {
		log.Panic(err)
	}

	// Map EpollEvent.Pad to the connection state
	states := map[int]*ConnState{}

	for {
		// Wait infinitely until at least one new event is happening
		var eventsBuf [10]unix.EpollEvent
		_, err := unix.EpollWait(epollFd, eventsBuf[:], -1)
		if err != nil {
			log.Panic(err)
		}

		// Go though every event occured; most often len(eventsBuf) == 1
		for _, event := range eventsBuf {
			if event.Fd == listenerPoll.Fd {
				// AcceptTCP() will now return immediately
				conn, err := listener.AcceptTCP()
				if err != nil {
					log.Panic(err)
				}
				newState := pollNewClient(epollFd, conn)
				fd := int(newState.connFile.Fd())
				states[fd] = newState
				continue
			}

			fd := int(event.Pad)
			state := states[fd]

			if event.Events == unix.POLLIN {
				state.connFile.Read(state.buffer)
				log.Printf("received: %s", state.buffer)

				// Equal to switching away the goroutine
				newPoll := event
				newPoll.Events = unix.POLLOUT
				unix.EpollCtl(epollFd, unix.EPOLL_CTL_MOD, fd, &newPoll)

			} else if event.Events == unix.POLLOUT {
				state.connFile.Write(bytes.ToUpper(state.buffer))
				state.connFile.Close()

				// Equal to stopping the goroutine
				unix.EpollCtl(epollFd, unix.EPOLL_CTL_DEL, fd, nil)
				delete(states, fd)
			}
		}
	}
}

func pollNewClient(epollFd int, conn *net.TCPConn) *ConnState {
	connFile, err := conn.File()
	if err != nil {
		log.Panic(err)
	}
	conn.Close() // Close this an use the connFile copy instead

	// Equal to starting the goroutine
	newState := ConnState{
		connFile: connFile,
		buffer:   make([]byte, 16),
	}
	fd := int(connFile.Fd())

	connPoll := unix.EpollEvent{
		Fd:     int32(fd),
		Events: unix.POLLIN, // POLLIN triggers on accept()
		Pad:    int32(fd),   // So we can find states[fd] when triggered
	}
	err = unix.EpollCtl(epollFd, unix.EPOLL_CTL_ADD, fd, &connPoll)
	if err != nil {
		log.Panic(err)
	}

	return &newState
}
