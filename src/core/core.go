package core

import (
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"io/ioutil"
	"time"

	iw "github.com/Arceliar/ironwood/encrypted"
	"github.com/Arceliar/phony"
	"github.com/gologme/log"

	"github.com/yggdrasil-network/yggdrasil-go/src/config"
	//"github.com/yggdrasil-network/yggdrasil-go/src/crypto"
	"github.com/yggdrasil-network/yggdrasil-go/src/version"
)

// The Core object represents the Yggdrasil node. You should create a Core
// object for each Yggdrasil node you plan to run.
type Core struct {
	// This is the main data structure that holds everything else for a node
	// We're going to keep our own copy of the provided config - that way we can
	// guarantee that it will be covered by the mutex
	phony.Inbox
	*iw.PacketConn
	config       config.NodeState // Config
	secret       ed25519.PrivateKey
	public       ed25519.PublicKey
	links        links
	log          *log.Logger
	addPeerTimer *time.Timer
}

func (c *Core) _init() error {
	// TODO separate init and start functions
	//  Init sets up structs
	//  Start launches goroutines that depend on structs being set up
	// This is pretty much required to completely avoid race conditions
	if c.log == nil {
		c.log = log.New(ioutil.Discard, "", 0)
	}

	current := c.config.GetCurrent()

	sigPriv, err := hex.DecodeString(current.PrivateKey)
	if err != nil {
		return err
	}
	if len(sigPriv) < ed25519.PrivateKeySize {
		return errors.New("PrivateKey is incorrect length")
	}

	c.secret = ed25519.PrivateKey(sigPriv)
	c.public = c.secret.Public().(ed25519.PublicKey)
	// TODO check public against current.PublicKey, error if they don't match

	c.PacketConn, err = iw.NewPacketConn(c.secret)
	return err
}

// If any static peers were provided in the configuration above then we should
// configure them. The loop ensures that disconnected peers will eventually
// be reconnected with.
func (c *Core) _addPeerLoop() {
	// Get the peers from the config - these could change!
	current := c.config.GetCurrent()

	// Add peers from the Peers section
	for _, peer := range current.Peers {
		go func(peer string, intf string) {
			if err := c.CallPeer(peer, intf); err != nil {
				c.log.Errorln("Failed to add peer:", err)
			}
		}(peer, "") // TODO: this should be acted and not in a goroutine?
	}

	// Add peers from the InterfacePeers section
	for intf, intfpeers := range current.InterfacePeers {
		for _, peer := range intfpeers {
			go func(peer string, intf string) {
				if err := c.CallPeer(peer, intf); err != nil {
					c.log.Errorln("Failed to add peer:", err)
				}
			}(peer, intf) // TODO: this should be acted and not in a goroutine?
		}
	}

	c.addPeerTimer = time.AfterFunc(time.Minute, func() {
		c.Act(nil, c._addPeerLoop)
	})
}

// Start starts up Yggdrasil using the provided config.NodeConfig, and outputs
// debug logging through the provided log.Logger. The started stack will include
// TCP and UDP sockets, a multicast discovery socket, an admin socket, router,
// switch and DHT node. A config.NodeState is returned which contains both the
// current and previous configurations (from reconfigures).
func (c *Core) Start(nc *config.NodeConfig, log *log.Logger) (conf *config.NodeState, err error) {
	phony.Block(c, func() {
		conf, err = c._start(nc, log)
	})
	return
}

// This function is unsafe and should only be ran by the core actor.
func (c *Core) _start(nc *config.NodeConfig, log *log.Logger) (*config.NodeState, error) {
	c.log = log

	c.config = config.NodeState{
		Current:  *nc,
		Previous: *nc,
	}

	if name := version.BuildName(); name != "unknown" {
		c.log.Infoln("Build name:", name)
	}
	if version := version.BuildVersion(); version != "unknown" {
		c.log.Infoln("Build version:", version)
	}

	c.log.Infoln("Starting up...")
	if err := c._init(); err != nil {
		c.log.Errorln("Failed to initialize core")
		return nil, err
	}

	if err := c.links.init(c); err != nil {
		c.log.Errorln("Failed to start link interfaces")
		return nil, err
	}

	//if err := c.switchTable.start(); err != nil {
	//	c.log.Errorln("Failed to start switch")
	//	return nil, err
	//}

	//if err := c.router.start(); err != nil {
	//	c.log.Errorln("Failed to start router")
	//	return nil, err
	//}

	c.Act(c, c._addPeerLoop)

	c.log.Infoln("Startup complete")
	return &c.config, nil
}

// Stop shuts down the Yggdrasil node.
func (c *Core) Stop() {
	phony.Block(c, c._stop)
}

// This function is unsafe and should only be ran by the core actor.
func (c *Core) _stop() {
	c.PacketConn.Close()
	c.log.Infoln("Stopping...")
	if c.addPeerTimer != nil {
		c.addPeerTimer.Stop()
	}
	c.links.stop()
	/* FIXME this deadlocks, need a waitgroup or something to coordinate shutdown
	for _, peer := range c.GetPeers() {
		c.DisconnectPeer(peer.Port)
	}
	*/
	c.log.Infoln("Stopped")
}
