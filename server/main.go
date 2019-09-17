package main

import (
	"log"
	"net"
	"flag"
	"time"
	"container/list"
	"strings"
	"bytes"
)

type Connection struct {
	net.Conn
	Name	string
}

const (
	msgTimeout	= 10 * time.Millisecond
	fedTimeout	= 2 * time.Millisecond
	fedBufSize	= 20
)

var (
	port		string
	maxConns	uint64
	maxMsgSize	uint
	maxUsrSize	uint
	nConns		uint64	= 0
)

// Manage writing message to clients
func federator(fedChan chan Connection, msgChan chan string) {
	conns := list.New()
	
	outer:
	for {
		select {
		case c := <- fedChan:
			for e := conns.Front(); e != nil; e = e.Next() {
				conn := e.Value.(Connection)
				
				// This is kind of bad and should be part of the interactive bit ☺
				if conn.Name == c.Name {
					c.Write([]byte("Username is already taken, bye.\n"))
					c.Close()
					continue outer
				}
			}
		
			// Add a connection to our list
			conns.PushBack(c)
			
			go func() {
				msgChan <- "→ " + c.Name
			}()
			
			nConns++

		case s := <- msgChan:
			s = s + "\n"
			
			log.Print("msg: ", s)
		
			// Iterate through all connections we know about
			for e := conns.Front(); e != nil; e = e.Next() {
				conn := e.Value.(Connection)
				
				// Apparently this is thread-safe
				_, err := conn.Write([]byte(s))
				if err != nil {
					log.Println("warn: client write failure, dc - ", err)
					
					conns.Remove(e)
					nConns--
				}
			}

		default:
			time.Sleep(fedTimeout)
		}
	}
}

// Handle incoming connection
func handler(conn net.Conn, fedChan chan Connection, msgChan chan string) {
	raddr := conn.RemoteAddr().String()
	log.Println("info: Accepted connection from ", raddr)
	
	strip := func(s string) string {
				s = strings.ReplaceAll(s, "\n", "")
				s = strings.ReplaceAll(s, "\r", "")
				return s
			}
	
	getuser:
	userBuf := make([]byte, maxUsrSize)

	_, err := conn.Write([]byte("What is your username?: "))
	if err != nil {
		log.Println("warn: failed to query for username from ", raddr, " - ", err)
	}
	
	_, err = conn.Read(userBuf)
	if err != nil {
		log.Println("warn: failed to read username from ", raddr, " - ", err)
	}
	
	// Trim null bytes which pad the unused space
	userBuf = bytes.Trim(userBuf, "\x00")
	
	name := strip(string(userBuf))
	
	if len(name) < 1 {
		conn.Write([]byte("Invalid username, try again.\n"))
		goto getuser
	}
	
	log.Println("info: ", raddr, " → ", name)
	
	c := Connection {
			conn, 
			name,
		}
	
	defer c.Close()

	fedChan <- c
	
	quitMsg := func() {
					msgChan <- "← " + c.Name
				}

	// Hack to get around federator closing the conn for us if a username is taken
	once := false

	loop:
	for {
		buf := make([]byte, maxMsgSize)
		//conn.SetReadDeadline(time.Now().Add(msgTimeout))
	
		_, err := conn.Read(buf)
		if err != nil {
			// We can have this happen whenever and close ourselves
			if once {
				quitMsg()
			}
			break
		}
		
		once = true
		
		buf = bytes.Trim(buf, "\x00")
		
		msg := string(buf)
		msg = strip(msg)
		
		if msg == "" {
			continue
		}
		
		switch(msg){
		case "!quit":
			quitMsg()
			break loop
		
		default:
			msgChan <- c.Name + " → " + msg
		}
	}
	
	log.Println("info: routine for ", raddr, " ended")
}

// Host a chat server compatible with SocketH
func main() {
	flag.StringVar(&port, "p", ":9090", "Port to listen on")
	flag.Uint64Var(&maxConns, "m", 100, "Maximum chat connections")
	flag.UintVar(&maxMsgSize, "s", 256, "Max size of a message read out")
	flag.UintVar(&maxUsrSize, "u", 25, "Max size of username")
	flag.Parse()
	
	fedChan := make(chan Connection, fedBufSize)
	msgChan := make(chan string, maxConns)
	
	go federator(fedChan, msgChan)
	
	log.Println("Listening on localhost" + port + " ...")
	
	// Start listening
	ln, err := net.Listen("tcp", port)
	if err != nil {
		log.Fatal("err: could not listen - ", err)
	}
	
	// Accept connections
	for {
		conn, err := ln.Accept()
		if err != nil || nConns >= maxConns {
			log.Println("warn: could not accept conn #", nConns, " - ", err)
			continue
		}
		
		nConns++
		go handler(conn, fedChan, msgChan)
	}
}
