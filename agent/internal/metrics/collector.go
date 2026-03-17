// Package metrics provides a Prometheus collector for WireGuard peer metrics.
package metrics

import (
	"log"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/aleks-dolotin/wg-sockd/agent/internal/storage"
	"github.com/aleks-dolotin/wg-sockd/agent/internal/wireguard"
)

// Collector implements prometheus.Collector, reading live wgctrl data
// merged with DB metadata on each scrape.
type Collector struct {
	wgClient  wireguard.WireGuardClient
	store     *storage.DB
	ifaceName string

	// Per-peer descriptors.
	peerRxDesc        *prometheus.Desc
	peerTxDesc        *prometheus.Desc
	peerHandshakeDesc *prometheus.Desc
	peerOnlineDesc    *prometheus.Desc
	peerEnabledDesc   *prometheus.Desc

	// Aggregate descriptors.
	peersTotal    *prometheus.Desc
	peersOnline   *prometheus.Desc
	transferRx    *prometheus.Desc
	transferTx    *prometheus.Desc
}

var peerLabels = []string{"peer_name", "public_key", "profile"}

// New creates a Collector for the given WireGuard interface.
func New(wgClient wireguard.WireGuardClient, store *storage.DB, ifaceName string) *Collector {
	return &Collector{
		wgClient:  wgClient,
		store:     store,
		ifaceName: ifaceName,

		peerRxDesc: prometheus.NewDesc(
			"wireguard_peer_receive_bytes_total",
			"Total bytes received from this peer.",
			peerLabels, nil,
		),
		peerTxDesc: prometheus.NewDesc(
			"wireguard_peer_transmit_bytes_total",
			"Total bytes transmitted to this peer.",
			peerLabels, nil,
		),
		peerHandshakeDesc: prometheus.NewDesc(
			"wireguard_peer_last_handshake_seconds",
			"Unix timestamp of last handshake with this peer.",
			peerLabels, nil,
		),
		peerOnlineDesc: prometheus.NewDesc(
			"wireguard_peer_is_online",
			"Whether the peer is currently online (handshake < 3 min ago).",
			peerLabels, nil,
		),
		peerEnabledDesc: prometheus.NewDesc(
			"wireguard_peer_enabled",
			"Whether the peer is enabled in the database.",
			peerLabels, nil,
		),

		peersTotal: prometheus.NewDesc(
			"wireguard_peers_total",
			"Total number of peers.",
			nil, nil,
		),
		peersOnline: prometheus.NewDesc(
			"wireguard_peers_online",
			"Number of currently online peers.",
			nil, nil,
		),
		transferRx: prometheus.NewDesc(
			"wireguard_transfer_receive_bytes_total",
			"Total bytes received across all peers.",
			nil, nil,
		),
		transferTx: prometheus.NewDesc(
			"wireguard_transfer_transmit_bytes_total",
			"Total bytes transmitted across all peers.",
			nil, nil,
		),
	}
}

// Describe sends descriptor metadata to the channel.
func (c *Collector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.peerRxDesc
	ch <- c.peerTxDesc
	ch <- c.peerHandshakeDesc
	ch <- c.peerOnlineDesc
	ch <- c.peerEnabledDesc
	ch <- c.peersTotal
	ch <- c.peersOnline
	ch <- c.transferRx
	ch <- c.transferTx
}

const onlineThreshold = 3 * time.Minute

// Collect reads live wgctrl + DB data and emits metrics.
func (c *Collector) Collect(ch chan<- prometheus.Metric) {
	dbPeers, err := c.store.ListPeers()
	if err != nil {
		log.Printf("metrics: ListPeers error: %v", err)
		return
	}

	dev, err := c.wgClient.GetDevice(c.ifaceName)
	if err != nil {
		log.Printf("metrics: GetDevice error: %v", err)
		// Still emit DB-only metrics with zero transfer values.
		c.emitDBOnly(ch, dbPeers)
		return
	}

	// Build wg peer map by public key.
	wgMap := make(map[string]wireguard.Peer, len(dev.Peers))
	for _, p := range dev.Peers {
		wgMap[p.PublicKey.String()] = p
	}

	now := time.Now()
	var totalRx, totalTx int64
	var onlineCount int

	for _, dbp := range dbPeers {
		profile := ""
		if dbp.Profile != nil {
			profile = *dbp.Profile
		}
		labels := []string{dbp.FriendlyName, dbp.PublicKey, profile}

		var rx, tx int64
		var handshakeUnix float64
		var online float64

		if wgp, ok := wgMap[dbp.PublicKey]; ok {
			rx = wgp.ReceiveBytes
			tx = wgp.TransmitBytes
			if !wgp.LastHandshake.IsZero() {
				handshakeUnix = float64(wgp.LastHandshake.Unix())
				if now.Sub(wgp.LastHandshake) < onlineThreshold {
					online = 1
					onlineCount++
				}
			}
		}

		totalRx += rx
		totalTx += tx

		enabled := float64(0)
		if dbp.Enabled {
			enabled = 1
		}

		ch <- prometheus.MustNewConstMetric(c.peerRxDesc, prometheus.CounterValue, float64(rx), labels...)
		ch <- prometheus.MustNewConstMetric(c.peerTxDesc, prometheus.CounterValue, float64(tx), labels...)
		ch <- prometheus.MustNewConstMetric(c.peerHandshakeDesc, prometheus.GaugeValue, handshakeUnix, labels...)
		ch <- prometheus.MustNewConstMetric(c.peerOnlineDesc, prometheus.GaugeValue, online, labels...)
		ch <- prometheus.MustNewConstMetric(c.peerEnabledDesc, prometheus.GaugeValue, enabled, labels...)
	}

	ch <- prometheus.MustNewConstMetric(c.peersTotal, prometheus.GaugeValue, float64(len(dbPeers)))
	ch <- prometheus.MustNewConstMetric(c.peersOnline, prometheus.GaugeValue, float64(onlineCount))
	ch <- prometheus.MustNewConstMetric(c.transferRx, prometheus.CounterValue, float64(totalRx))
	ch <- prometheus.MustNewConstMetric(c.transferTx, prometheus.CounterValue, float64(totalTx))
}

// emitDBOnly emits metrics when wgctrl is unavailable (degraded mode).
func (c *Collector) emitDBOnly(ch chan<- prometheus.Metric, dbPeers []storage.Peer) {
	for _, dbp := range dbPeers {
		profile := ""
		if dbp.Profile != nil {
			profile = *dbp.Profile
		}
		labels := []string{dbp.FriendlyName, dbp.PublicKey, profile}

		enabled := float64(0)
		if dbp.Enabled {
			enabled = 1
		}

		ch <- prometheus.MustNewConstMetric(c.peerRxDesc, prometheus.CounterValue, 0, labels...)
		ch <- prometheus.MustNewConstMetric(c.peerTxDesc, prometheus.CounterValue, 0, labels...)
		ch <- prometheus.MustNewConstMetric(c.peerHandshakeDesc, prometheus.GaugeValue, 0, labels...)
		ch <- prometheus.MustNewConstMetric(c.peerOnlineDesc, prometheus.GaugeValue, 0, labels...)
		ch <- prometheus.MustNewConstMetric(c.peerEnabledDesc, prometheus.GaugeValue, enabled, labels...)
	}

	ch <- prometheus.MustNewConstMetric(c.peersTotal, prometheus.GaugeValue, float64(len(dbPeers)))
	ch <- prometheus.MustNewConstMetric(c.peersOnline, prometheus.GaugeValue, 0)
	ch <- prometheus.MustNewConstMetric(c.transferRx, prometheus.CounterValue, 0)
	ch <- prometheus.MustNewConstMetric(c.transferTx, prometheus.CounterValue, 0)
}
