package networkinfo

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/harmony-one/harmony/internal/utils"
	"github.com/harmony-one/harmony/p2p"
	"github.com/prometheus/common/log"

	peerstore "github.com/libp2p/go-libp2p-peerstore"

	libp2pdis "github.com/libp2p/go-libp2p-discovery"
	libp2pdht "github.com/libp2p/go-libp2p-kad-dht"
)

// Service is the role conversion service.
type Service struct {
	Host        p2p.Host
	Rendezvous  string
	dht         *libp2pdht.IpfsDHT
	ctx         context.Context
	cancel      context.CancelFunc
	stopChan    chan struct{}
	stoppedChan chan struct{}
	peerChan    chan p2p.Peer
	peerInfo    <-chan peerstore.PeerInfo
	discovery   *libp2pdis.RoutingDiscovery
}

// NewService returns role conversion service.
func NewService(h p2p.Host, rendezvous string, peerChan chan p2p.Peer) *Service {
	timeout := 30 * time.Minute
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	dht, err := libp2pdht.New(ctx, h.GetP2PHost())
	if err != nil {
		panic(err)
	}

	return &Service{
		Host:        h,
		dht:         dht,
		Rendezvous:  rendezvous,
		ctx:         ctx,
		cancel:      cancel,
		stopChan:    make(chan struct{}),
		stoppedChan: make(chan struct{}),
		peerChan:    peerChan,
		peerInfo:    make(<-chan peerstore.PeerInfo),
	}
}

// StartService starts role conversion service.
func (s *Service) StartService() {
	s.Init()
	s.Run()
}

// Init initializes role conversion service.
func (s *Service) Init() error {
	log.Info("Init networkinfo service")

	// Bootstrap the DHT. In the default configuration, this spawns a Background
	// thread that will refresh the peer table every five minutes.
	log.Debug("Bootstrapping the DHT")
	if err := s.dht.Bootstrap(s.ctx); err != nil {
		return fmt.Errorf("error bootstrap dht")
	}

	var wg sync.WaitGroup
	for _, peerAddr := range utils.BootNodes {
		peerinfo, _ := peerstore.InfoFromP2pAddr(peerAddr)
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.Host.GetP2PHost().Connect(s.ctx, *peerinfo); err != nil {
				log.Warn("can't connect to bootnode", "error", err)
			} else {
				log.Info("connected to bootnode", "node", *peerinfo)
			}
		}()
	}
	wg.Wait()

	// We use a rendezvous point "shardID" to announce our location.
	log.Info("Announcing ourselves...")
	s.discovery = libp2pdis.NewRoutingDiscovery(s.dht)
	libp2pdis.Advertise(s.ctx, s.discovery, s.Rendezvous)
	log.Info("Successfully announced!")

	go s.DoService()

	return nil
}

// Run runs role conversion.
func (s *Service) Run() {
	defer close(s.stoppedChan)
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()

	// Do FindPeers call every 3 minutes
	for {
		select {
		case <-tick.C:
			log.Info("tick")
         var err error
			s.peerInfo, err = s.discovery.FindPeers(s.ctx, s.Rendezvous)
			if err != nil {
				log.Error("FindPeers", "error", err)
				return
			}
		case <-s.ctx.Done():
		case <-s.stopChan:
			return
		}
	}

}

// DoService does role conversion.
func (s *Service) DoService() {
	for {
		select {
		case peer, ok := <-s.peerInfo:
         log.Info("peer", "found", peer)
			if !ok {
				log.Debug("no more peer info", "peer", peer.ID)
				return
			}
			if peer.ID != s.Host.GetP2PHost().ID() && len(peer.ID) > 0 {
				log.Debug("Found Peer", "peer", peer.ID, "addr", peer.Addrs)
				for i, addr := range peer.Addrs {
					log.Debug("address", "no", i, "addr", addr)
				}
				p := p2p.Peer{IP: "127.0.0.1", Port: "9999", PeerID: peer.ID, Addrs: peer.Addrs}
				s.peerChan <- p
			}
      case <-s.ctx.Done():
         return
		}
	}

}

// StopService stops role conversion service.
func (s *Service) StopService() {
	defer s.cancel()

	utils.GetLogInstance().Info("Stopping role conversion service.")
	s.stopChan <- struct{}{}
	<-s.stoppedChan
	utils.GetLogInstance().Info("Role conversion stopped.")
}
