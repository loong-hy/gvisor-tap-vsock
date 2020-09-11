package virtualnetwork

import (
	"context"
	"fmt"
	"net"
	"net/http"

	"github.com/code-ready/gvisor-tap-vsock/pkg/services/dns"
	"github.com/code-ready/gvisor-tap-vsock/pkg/services/forwarder"
	"github.com/code-ready/gvisor-tap-vsock/pkg/types"
	"github.com/google/tcpproxy"
	log "github.com/sirupsen/logrus"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/udp"
)

func addServices(configuration *types.Configuration, s *stack.Stack) error {
	tcpForwarder := forwarder.TCP(s)
	s.SetTransportProtocolHandler(tcp.ProtocolNumber, tcpForwarder.HandlePacket)
	udpForwarder := forwarder.UDP(s)
	s.SetTransportProtocolHandler(udp.ProtocolNumber, udpForwarder.HandlePacket)

	go func() {
		if err := dnsServer(configuration, s); err != nil {
			log.Error(err)
		}
	}()
	go func() {
		if err := forwardHostVM(configuration, s); err != nil {
			log.Error(err)
		}
	}()
	return sampleHTTPServer(configuration, s)
}

func dnsServer(configuration *types.Configuration, s *stack.Stack) error {
	udpConn, err := gonet.DialUDP(s, &tcpip.FullAddress{
		NIC:  1,
		Addr: tcpip.Address(net.ParseIP(configuration.GatewayIP).To4()),
		Port: uint16(53),
	}, nil, ipv4.ProtocolNumber)
	if err != nil {
		return err
	}

	return dns.Serve(udpConn)
}

func forwardHostVM(configuration *types.Configuration, s *stack.Stack) error {
	var p tcpproxy.Proxy
	p.AddRoute(":2222", &tcpproxy.DialProxy{
		Addr: fmt.Sprintf("%s:22", configuration.VMIP),
		DialContext: func(ctx context.Context, network, addr string) (conn net.Conn, e error) {
			return gonet.DialTCP(s, tcpip.FullAddress{
				NIC:  1,
				Addr: tcpip.Address(net.ParseIP(configuration.VMIP).To4()),
				Port: uint16(22),
			}, ipv4.ProtocolNumber)
		},
	})
	return p.Run()
}

func sampleHTTPServer(configuration *types.Configuration, s *stack.Stack) error {
	ln, err := gonet.ListenTCP(s, tcpip.FullAddress{
		NIC:  1,
		Addr: tcpip.Address(net.ParseIP(configuration.GatewayIP).To4()),
		Port: uint16(80),
	}, ipv4.ProtocolNumber)
	if err != nil {
		return err
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if _, err := w.Write([]byte(`Hello world`)); err != nil {
			log.Error(err)
		}
	})
	go func() {
		if err := http.Serve(ln, mux); err != nil {
			log.Error(err)
		}
	}()
	return nil
}