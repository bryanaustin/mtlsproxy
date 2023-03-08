package main

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
)

type Instance struct {
	ident string
	p     *Profile
	// l net.Listener // Interface
	newCon  chan newConnection
	newDest chan *socketInfo
	newList chan *socketInfo
	fin     chan struct{}
	change  sync.Mutex
	closed  bool
}

type newConnection struct {
	ident string
	conn  net.Conn // Interface
}

type socketInfo struct {
	tlsconf   *tls.Config
	net, addr string
}

type conConculsion struct {
	ident string
	err   error
	xfer  int64
}

func NewInstance(p *Profile) (inst *Instance, err error) {
	inst = &Instance{
		p:       p,
		ident:   p.Name,
		newCon:  make(chan newConnection),
		newDest: make(chan *socketInfo),
		newList: make(chan *socketInfo),
		fin:     make(chan struct{}),
	}
	go inst.run()
	err = inst.changeEverything(p) // locking not needed
	return
}

func (inst *Instance) AdaptTo(p *Profile) error {
	inst.change.Lock()
	defer inst.change.Unlock()

	if inst.closed {
		return nil
	}

	lc := inst.p.ListenChanged(p)
	dc := inst.p.DestinationChanged(p)

	var err error
	if lc && dc {
		err = inst.changeEverything(p)
	} else if lc {
		err = inst.changeListener(p)
	} else if dc {
		err = inst.changeDesination(p)
	}

	if err != nil {
		return err
	}

	inst.p = p
	return nil
}

func (inst *Instance) Stop() {
	inst.change.Lock()
	defer inst.change.Unlock()

	if inst.closed {
		return
	}

	inst.newDest <- nil
	inst.newList <- nil
	inst.closed = true
	close(inst.fin)
}

func (inst *Instance) changeListener(p *Profile) error {
	proto := p.Protocol
	if len(proto) < 1 {
		proto = "tcp"
	}

	if len(p.ListenAuthorityRaw) < 1 && len(p.ListenCertRaw) < 1 {
		inst.newList <- &socketInfo{tlsconf: nil, net: proto, addr: p.Listen}
		return nil
	}

	tlsconf := new(tls.Config)

	if len(p.ListenAuthorityRaw) > 0 {
		capool := x509.NewCertPool()
		if ok := capool.AppendCertsFromPEM([]byte(p.ListenAuthorityRaw)); !ok {
			return errors.New("no certs found for the listen authority")
		}
		tlsconf.ClientCAs = capool
		tlsconf.ClientAuth = tls.RequireAndVerifyClientCert
	}

	if len(p.ListenCertRaw) > 0 {
		cert, err := tls.X509KeyPair([]byte(p.ListenCertRaw), []byte(p.ListenPrivateRaw))
		if err != nil {
			return errors.New("loading cert/key pair: " + err.Error())
		}
		tlsconf.Certificates = []tls.Certificate{cert}
	}

	inst.newList <- &socketInfo{tlsconf: tlsconf, net: proto, addr: p.Listen}
	return nil
}

func (inst *Instance) changeDesination(p *Profile) error {
	proto := p.Protocol
	if len(proto) < 1 {
		proto = "tcp"
	}

	if len(p.SendAuthorityRaw) < 1 && len(p.SendCertRaw) < 1 {
		inst.newDest <- &socketInfo{tlsconf: nil, net: proto, addr: p.Proxy}
		return nil
	}

	tlsconf := new(tls.Config)

	if len(p.SendAuthorityRaw) > 0 {
		capool := x509.NewCertPool()
		if ok := capool.AppendCertsFromPEM([]byte(p.SendAuthorityRaw)); !ok {
			return errors.New("no certs found for the listen authority")
		}
		tlsconf.RootCAs = capool
	}

	if len(p.SendCertRaw) > 0 {
		cert, err := tls.X509KeyPair([]byte(p.SendCertRaw), []byte(p.SendPrivateRaw))
		if err != nil {
			return errors.New("loading cert/key pair: " + err.Error())
		}
		tlsconf.Certificates = []tls.Certificate{cert}
	}

	inst.newDest <- &socketInfo{tlsconf: tlsconf, net: proto, addr: p.Proxy}
	return nil
}

func (inst *Instance) changeEverything(p *Profile) error {
	err := inst.changeDesination(p)
	if err != nil {
		return err
	}

	return inst.changeListener(p)
}

func (inst *Instance) run() {
	var listener net.Listener
	var conCloser chan struct{}
	var dest *socketInfo
	var count uint64
	var rev uint64

	for {
		select {
		case con := <-inst.newCon:
			if dest != nil {
				newident := fmt.Sprintf("%s$%d#%d", inst.ident, rev, count)
				count++
				go inst.connection(newident, con.conn, *dest, conCloser)
			} else {
				con.conn.Close()
			}
		case x := <-inst.newDest:
			rev++
			if conCloser != nil {
				close(conCloser)
			}

			dest = x
			if x != nil {
				conCloser = make(chan struct{})
			}
		case x := <-inst.newList:
			//TODO: if new and old don't have the same address, change the order to open, close for high availability
			if listener != nil {
				if err := listener.Close(); err != nil {
					ident := fmt.Sprintf("%s$%d", inst.ident, rev)
					log.Println(fmt.Sprintf("%s: error closing old listener: %s", ident, err.Error()))
				}
			}
			ident := fmt.Sprintf("%s$%d", inst.ident, rev)
			rev++
			if x == nil {
				listener = nil
				continue
			}
			l, err := x.listen()
			if err != nil {
				log.Println(fmt.Sprintf("%s: error opening new listener: %s", ident, err.Error()))
			} else {
				// list = &x
				listener = l
				go inst.acceptance(ident, l)
			}
		case <-inst.fin:
			return
		}
	}
}

// acceptance runs in it's own Go routine for handling new connection
func (inst *Instance) acceptance(ident string, l net.Listener) {
	var count uint64
	for {
		c, err := l.Accept()
		if err != nil {
			log.Println(fmt.Sprintf("%s: error accepting new connections: %s", ident, err.Error()))
			// Are there any errors here that are recoverable?
			return
		}
		inst.newCon <- newConnection{ident: fmt.Sprintf("%s#%d", ident, count), conn: c}
		// verbose logging of the new connection
		count++
	}
}

// connection runs in it's own Go routine and manages the connection to dest as well as the read/write go routines.
func (inst *Instance) connection(ident string, l net.Conn, config socketInfo, done <-chan struct{}) {
	defer l.Close()
	c, err := config.connect()
	if err != nil {
		log.Println(fmt.Sprintf("%s: error connecting to destination: %s", ident, err.Error()))
		//TODO: consider upstream effects
		//TODO: close parent socket?
		return
	}
	defer c.Close()
	ec := make(chan conConculsion)
	defer close(ec)
	go inst.transfer(ident+":ltd", l, c, ec)
	go inst.transfer(ident+":dtl", c, l, ec)
	var result conConculsion
	open := 2

	select {
	case result = <-ec:
		open--
		if result.err != nil {
			log.Println(fmt.Sprintf("%s: socket error after xfer:%d: %s", ident, result.xfer, result.err.Error()))
		} else if Debug {
			log.Println(fmt.Sprintf("%s: closed after xfer:%d", ident, result.xfer))
		}
	case <-done:
		//TODO: Add grace period
	}

	// Close never needs checked form the connection level right? io channels will always indicate?

	// drain both channels
	for ; open > 0; open-- {
		result = <-ec
		if Debug {
			log.Println(fmt.Sprintf("%s: closed after xfer:%d", ident, result.xfer))
		}
	}
}

func (inst *Instance) transfer(ident string, r io.Reader, w io.Writer, e chan<- conConculsion) {
	count, err := io.Copy(w, r)
	if err != nil {
		werr := fmt.Errorf("%s: error after transferring %d bytes: %w", ident, count, err)
		e <- conConculsion{ident: ident, err: werr, xfer: count}
	} else {
		e <- conConculsion{ident: ident, xfer: count}
	}
}

func (info socketInfo) connect() (net.Conn, error) {
	if info.tlsconf == nil {
		return net.Dial(info.net, info.addr)
		//TODO: implement DialTimeout
	}
	return tls.Dial(info.net, info.addr, info.tlsconf)
}

func (info socketInfo) listen() (net.Listener, error) {
	if info.tlsconf == nil {
		return net.Listen(info.net, info.addr)
	}
	return tls.Listen(info.net, info.addr, info.tlsconf)
}
