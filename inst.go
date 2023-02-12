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
	newDest chan socketInfo
	newList chan socketInfo
	change  sync.Mutex
}

type newConnection struct {
	ident string
	conn  net.Conn // Interface
}

type socketInfo struct {
	tlsconf   *tls.Config
	net, addr string
}

func NewInstance(p *Profile) (inst *Instance, err error) {
	inst = &Instance{
		p:       p,
		ident:   p.Name,
		newCon:  make(chan newConnection),
		newDest: make(chan socketInfo),
		newList: make(chan socketInfo),
	}
	go inst.run()
	err = inst.changeEverything(p) // locking not needed
	return
}

// Cases:
// 1) New Profile
//   - Destination info - Set
//   - Connection handler - Created/Started
//   - Listener - Created/Started
//
// 2) Change destination certs or address
//   - Destination info - Swap with old profile, new connections get this
//   - Connection handler - End all current connections immediately
//   - Listener - Unchanged
//     (Dest info is swapped before connections are altered)
//
// 3) Change listen certs or address
//   - Destination info - No change
//   - Connection handler - No change
//   - Listener - Unchanged
//     (Start new listener then gracefully close old listener with timeout)
func (inst *Instance) AdaptTo(p *Profile) error {
	inst.change.Lock()
	defer inst.change.Unlock()
	lc := p.ListenChanged(p)
	dc := p.DestinationChanged(p)

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

func (inst *Instance) changeListener(p *Profile) error {
	proto := p.Protocol
	if len(proto) < 1 {
		proto = "tcp"
	}

	if len(p.ListenAuthorityRaw) < 1 {
		inst.newList <- socketInfo{tlsconf: nil, net: proto, addr: p.Listen}
		return nil
	}

	capool := x509.NewCertPool()
	if ok := capool.AppendCertsFromPEM([]byte(p.ListenAuthorityRaw)); !ok {
		return errors.New("no certs found for the listen authority")
	}

	cert, err := tls.X509KeyPair([]byte(p.CertRaw), []byte(p.PrivateRaw))
	if err != nil {
		return errors.New("error loading cert/key pair: " + err.Error())
	}

	tlsconf := &tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientCAs:    capool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
	}

	inst.newList <- socketInfo{tlsconf: tlsconf, net: proto, addr: p.Listen}
	return nil
}

func (inst *Instance) changeDesination(p *Profile) error {
	proto := p.Protocol
	if len(proto) < 1 {
		proto = "tcp"
	}

	if len(p.SendAuthorityRaw) < 1 {
		inst.newList <- socketInfo{tlsconf: nil, net: proto, addr: p.Proxy}
		return nil
	}

	capool := x509.NewCertPool()
	if ok := capool.AppendCertsFromPEM([]byte(p.SendAuthorityRaw)); !ok {
		return errors.New("no certs found for the listen authority")
	}

	cert, err := tls.X509KeyPair([]byte(p.CertRaw), []byte(p.PrivateRaw))
	if err != nil {
		return errors.New("error loading cert/key pair: " + err.Error())
	}

	tlsconf := &tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientCAs:    capool,
	}

	inst.newDest <- socketInfo{tlsconf: tlsconf, net: proto, addr: p.Proxy}
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
			dest = &x
			conCloser = make(chan struct{})
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
			l, err := x.listen()
			if err != nil {
				log.Println(fmt.Sprintf("%s: error opening new listener: %s", ident, err.Error()))
			} else {
				// list = &x
				listener = l
				go inst.acceptance(ident, l)
			}
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
	ec := make(chan error)
	go inst.transfer(ident+":ltd", l, c, ec)
	go inst.transfer(ident+":dtl", c, l, ec)
	open := 2

	select {
	case err = <-ec:
		open--
		// errors.Is()
		// verbose?
		log.Println(err)
		// io.ErrClosed?
	case <-done:
		//TODO: Add grace period
	}

	// Close never needs checked form the connection level right? io channels will always indicate?

	// drain both channels
	for ; open > 0; open-- {
		err = <-ec
		// verbose?
		log.Println(err)
	}
}

func (inst *Instance) transfer(ident string, r io.Reader, w io.Writer, e chan<- error) {
	defer close(e)
	var count uint64
	for {
		n, err := io.Copy(w, r)
		count += uint64(n)
		if err != nil {
			e <- fmt.Errorf("%s: error after transferring %d bytes: %w", ident, count, err)
			return
		}
		// Add a delay?
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
