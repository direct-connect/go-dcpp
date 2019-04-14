package hub

import (
	"time"

	"github.com/direct-connect/go-dc/nmdc"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

func measure(m prometheus.Observer) func() {
	start := time.Now()
	return func() {
		dt := time.Since(start)
		m.Observe(dt.Seconds())
	}
}

const cmdUnknown = "other"

func measureM(m map[string]prometheus.Observer, k string) func() {
	o, ok := m[k]
	if !ok {
		o = m[cmdUnknown]
	}
	if o == nil {
		return func() {}
	}
	start := time.Now()
	return func() {
		dt := time.Since(start)
		o.Observe(dt.Seconds())
	}
}

func countM(m map[string]prometheus.Counter, k string, n int) {
	cnt, ok := m[k]
	if !ok {
		cnt = m[cmdUnknown]
	}
	if cnt != nil {
		cnt.Add(float64(n))
	}
}

var (
	cntConnAccepted = promauto.NewCounter(prometheus.CounterOpts{
		Name: "dc_conn_accepted",
		Help: "The total number of accepted connections",
	})
	cntConnBlocked = promauto.NewCounter(prometheus.CounterOpts{
		Name: "dc_conn_blocked",
		Help: "The total number of blocked connections",
	})
	cntConnError = promauto.NewCounter(prometheus.CounterOpts{
		Name: "dc_conn_error",
		Help: "The total number of connections failed with an error",
	})
	cntConnOpen = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "dc_conn_open",
		Help: "The number of open connections",
	})

	durConnPeek = promauto.NewHistogram(prometheus.HistogramOpts{
		Name: "dc_conn_peek",
		Help: "The time to peek protocol magic from the connection",
	})
	durConnPeekTLS = promauto.NewHistogram(prometheus.HistogramOpts{
		Name: "dc_conn_tls_peek",
		Help: "The time to peek protocol magic from the TLS connection",
	})

	cntConnAuto = promauto.NewCounter(prometheus.CounterOpts{
		Name: "dc_conn_auto",
		Help: "The number of open connections with protocol auto-detection",
	})
	cntConnNMDC = promauto.NewCounter(prometheus.CounterOpts{
		Name: "dc_conn_nmdc",
		Help: "The number of open NMDC connections",
	})
	cntConnADC = promauto.NewCounter(prometheus.CounterOpts{
		Name: "dc_conn_adc",
		Help: "The number of open ADC connections",
	})
	cntConnIRC = promauto.NewCounter(prometheus.CounterOpts{
		Name: "dc_conn_irc",
		Help: "The number of open IRC connections",
	})
	cntConnHTTP1 = promauto.NewCounter(prometheus.CounterOpts{
		Name: "dc_conn_http1",
		Help: "The number of open HTTP1 connections",
	})
	cntConnHTTP2 = promauto.NewCounter(prometheus.CounterOpts{
		Name: "dc_conn_http2",
		Help: "The number of open HTTP2 connections",
	})

	cntConnNMDCOpen = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "dc_conn_nmdc_open",
		Help: "The number of open NMDC connections",
	})
	cntConnADCOpen = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "dc_conn_adc_open",
		Help: "The number of open ADC connections",
	})
	cntConnIRCOpen = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "dc_conn_irc_open",
		Help: "The number of open IRC connections",
	})
	cntConnHTTPOpen = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "dc_conn_http_open",
		Help: "The number of open HTTP connections",
	})

	cntConnTLS = promauto.NewCounter(prometheus.CounterOpts{
		Name: "dc_conn_tls",
		Help: "The total number of accepted TLS connections",
	})
	cntConnNMDCS = promauto.NewCounter(prometheus.CounterOpts{
		Name: "dc_conn_tls_nmdc",
		Help: "The total number of secure NMDC connections",
	})
	cntConnADCS = promauto.NewCounter(prometheus.CounterOpts{
		Name: "dc_conn_tls_adc",
		Help: "The total number of secure ADC connections",
	})
	cntConnIRCS = promauto.NewCounter(prometheus.CounterOpts{
		Name: "dc_conn_tls_irc",
		Help: "The total number of secure IRC connections",
	})
	cntConnHTTPS = promauto.NewCounter(prometheus.CounterOpts{
		Name: "dc_conn_tls_http",
		Help: "The total number of secure HTTP connections",
	})

	cntConnALPN = promauto.NewCounter(prometheus.CounterOpts{
		Name: "dc_conn_alpn",
		Help: "The total number of TLS connections that support ALPN",
	})
	cntConnAlpnNMDC = promauto.NewCounter(prometheus.CounterOpts{
		Name: "dc_conn_alpn_nmdc",
		Help: "The total number of accepted NMDC connections that support ALPN",
	})
	cntConnAlpnADC = promauto.NewCounter(prometheus.CounterOpts{
		Name: "dc_conn_alpn_adc",
		Help: "The total number of accepted ADC connections that support ALPN",
	})
	cntConnAlpnHTTP = promauto.NewCounter(prometheus.CounterOpts{
		Name: "dc_conn_alpn_http",
		Help: "The total number of accepted HTTP connections that support ALPN",
	})

	cntPings = promauto.NewCounter(prometheus.CounterOpts{
		Name: "dc_pings",
		Help: "The total number of pings",
	})
	cntPingsNMDC = promauto.NewCounter(prometheus.CounterOpts{
		Name: "dc_pings_nmdc",
		Help: "The total number of NMDC pings",
	})
	cntPingsADC = promauto.NewCounter(prometheus.CounterOpts{
		Name: "dc_pings_adc", // TODO
		Help: "The total number of ADC pings",
	})
	cntPingsHTTP = promauto.NewCounter(prometheus.CounterOpts{
		Name: "dc_pings_http",
		Help: "The total number of HTTP pings",
	})

	cntClients = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "dc_clients",
		Help: "The total number of clients with a specific app version",
	}, []string{"app", "vers"})

	cntChatRooms = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "dc_chat_rooms",
		Help: "The number of active chat rooms",
	})
	cntChatMsg = promauto.NewCounter(prometheus.CounterOpts{
		Name: "dc_chat_msg",
		Help: "The total number of chat messages sent",
	})
	cntChatMsgDropped = promauto.NewCounter(prometheus.CounterOpts{
		Name: "dc_chat_msg_dropped",
		Help: "The total number of chat messages dropped",
	})
	cntChatMsgPM = promauto.NewCounter(prometheus.CounterOpts{
		Name: "dc_chat_msg_pm",
		Help: "The total number of private messages sent",
	})

	cntSearch = promauto.NewCounter(prometheus.CounterOpts{
		Name: "dc_search",
		Help: "The total number of search requests processed",
	})
	durSearch = promauto.NewHistogram(prometheus.HistogramOpts{
		Name: "dc_search_dur",
		Help: "The time to send the search request",
	})

	sizeNMDCLinesR = promauto.NewHistogram(prometheus.HistogramOpts{
		Name: "dc_nmdc_lines_read",
		Help: "The number of bytes of NMDC protocol received",
	})
	sizeNMDCLinesW = promauto.NewHistogram(prometheus.HistogramOpts{
		Name: "dc_nmdc_lines_write",
		Help: "The number of bytes of NMDC protocol sent",
	})
	durNMDCHandshake = promauto.NewHistogram(prometheus.HistogramOpts{
		Name: "dc_nmdc_handshake_dur",
		Help: "The time to perform NMDC handshake",
	})
	cntNMDCExtensions = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "dc_nmdc_extension",
		Help: "The total number of NMDC connections with a given extension",
	}, []string{"ext"})

	sizeADCLinesR = promauto.NewHistogram(prometheus.HistogramOpts{
		Name: "dc_adc_lines_read",
		Help: "The number of bytes of ADC protocol received",
	})
	sizeADCLinesW = promauto.NewHistogram(prometheus.HistogramOpts{
		Name: "dc_adc_lines_write",
		Help: "The number of bytes of ADC protocol sent",
	})
	durADCHandshake = promauto.NewHistogram(prometheus.HistogramOpts{
		Name: "dc_adc_handshake_dur",
		Help: "The time to perform ADC handshake",
	})
	cntADCPackets = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "dc_adc_packets",
		Help: "The total number of specific ADC packets",
	}, []string{"kind"})
	cntADCCommands = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "dc_adc_commands",
		Help: "The total number of specific ADC commands",
	}, []string{"kind"})
	cntADCExtensions = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "dc_adc_extension",
		Help: "The total number of ADC connections with a given extension",
	}, []string{"ext"})
	durADCHandlePacket = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name: "dc_adc_packet_handle",
		Help: "The time to to handle a specific packet kind",
	}, []string{"kind"})
	durADCHandleCommand = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name: "dc_adc_command_handle",
		Help: "The time to to handle a specific command",
	}, []string{"cmd"})

	cntPeers = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "dc_peers",
		Help: "The number of active peers",
	})
	cntShare = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "dc_share",
		Help: "Total share size",
	})
)

var (
	cntNMDCCommandsR    = make(map[string]prometheus.Counter)
	cntNMDCCommandsW    = make(map[string]prometheus.Counter)
	cntNMDCCommandsDrop = make(map[string]prometheus.Counter)
	durNMDCHandle       = make(map[string]prometheus.Observer)
	sizeNMDCCommandR    = make(map[string]prometheus.Observer)
)

func init() {
	nmdcCommandsR := promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "dc_nmdc_commands_read",
		Help: "The total number of NMDC commands received",
	}, []string{"cmd"})
	nmdcCommandsDrop := promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "dc_nmdc_commands_dropped",
		Help: "The total number of dropped NMDC commands",
	}, []string{"cmd"})
	nmdcCommandsW := promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "dc_nmdc_commands_write",
		Help: "The total number of NMDC commands sent",
	}, []string{"cmd"})
	nmdcCommandsRSize := promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name: "dc_nmdc_commands_read_bytes",
		Help: "The total number of NMDC command bytes received",
	}, []string{"cmd"})
	nmdcHandleDur := promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name: "dc_nmdc_handle",
		Help: "The time to to handle a specific command",
	}, []string{"cmd"})
	reg := func(cmd string) {
		cntNMDCCommandsR[cmd] = nmdcCommandsR.WithLabelValues(cmd)
		cntNMDCCommandsDrop[cmd] = nmdcCommandsDrop.WithLabelValues(cmd)
		cntNMDCCommandsW[cmd] = nmdcCommandsW.WithLabelValues(cmd)
		sizeNMDCCommandR[cmd] = nmdcCommandsRSize.WithLabelValues(cmd)
		durNMDCHandle[cmd] = nmdcHandleDur.WithLabelValues(cmd)
	}
	for _, cmd := range nmdc.RegisteredTypes() {
		reg(cmd)
	}
	reg(cmdUnknown)
}
