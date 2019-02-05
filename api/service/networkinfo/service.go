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
	dht         *libp2pdht.IpfsDHT
	Rendezvous  string
	ctx         context.Context
	cancel      context.CancelFunc
	stopChan    chan struct{}
	stoppedChan chan struct{}
	peerChan    <-chan peerstore.PeerInfo
}

// NewService returns role conversion service.
func NewService(h p2p.Host, rendezvous string, peerChan <-chan peerstore.PeerInfo) *Service {
	timeout := 3 * time.Minute
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
	return nil
}

// Run runs role conversion.
func (s *Service) Run() {
	go func() {
		defer close(s.stoppedChan)
		for {
			select {
			default:
				utils.GetLogInstance().Info("Running role conversion")
				// TODO: Write some logic here.
				s.DoService()
			case <-s.stopChan:
				return
			}
		}
	}()
}

// DoService does role conversion.
func (s *Service) DoService() {
	// We use a rendezvous point "shardID" to announce our location.
	log.Info("Announcing ourselves...")
	routingDiscovery := libp2pdis.NewRoutingDiscovery(s.dht)
	libp2pdis.Advertise(s.ctx, routingDiscovery, s.Rendezvous)
	log.Debug("Successfully announced!")

	log.Debug("Searching for other peers...")
	var err error
	s.peerChan, err = routingDiscovery.FindPeers(s.ctx, s.Rendezvous)
	if err != nil {
		log.Error("FindPeers", "error", err)
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
