package metric

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/buger/jsonparser"
	"github.com/coocood/freecache"
	"github.com/negbie/heplify-server"
	"github.com/negbie/heplify-server/config"
	"github.com/negbie/heplify-server/logp"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Prometheus struct {
	TargetIP          []string
	TargetName        []string
	TargetEmpty       bool
	GaugeMetrics      map[string]prometheus.Gauge
	GaugeVecMetrics   map[string]*prometheus.GaugeVec
	CounterVecMetrics map[string]*prometheus.CounterVec
	Cache             *freecache.Cache
	horaclifixPaths   [][]string
	rtpPaths          [][]string
	rtcpPaths         [][]string
}

func (p *Prometheus) setup() (err error) {
	promTargetIP := cutSpace(config.Setting.PromTargetIP)
	promTargetName := cutSpace(config.Setting.PromTargetName)

	p.TargetIP = strings.Split(promTargetIP, ",")
	p.TargetName = strings.Split(promTargetName, ",")

	dedupIP := make(map[string]bool)
	dedupName := make(map[string]bool)

	uniqueIP := []string{}
	for _, ti := range p.TargetIP {
		if _, ok := dedupIP[ti]; !ok {
			dedupIP[ti] = true
			uniqueIP = append(uniqueIP, ti)
		}
	}

	uniqueNames := []string{}
	for _, tn := range p.TargetName {
		if _, ok := dedupName[tn]; !ok {
			dedupName[tn] = true
			uniqueNames = append(uniqueNames, tn)
		}
	}

	p.TargetIP = uniqueIP
	p.TargetName = uniqueNames

	if len(p.TargetIP) != len(p.TargetName) {
		return fmt.Errorf("please give every prometheus Target a unique IP address and a unique name")
	}

	if p.TargetIP[0] == "" && p.TargetName[0] == "" {
		logp.Info("Start prometheus with no targets")
		p.TargetEmpty = true
		p.Cache = freecache.NewCache(60 * 1024 * 1024)
	}

	p.GaugeMetrics = map[string]prometheus.Gauge{}
	p.GaugeVecMetrics = map[string]*prometheus.GaugeVec{}
	p.CounterVecMetrics = map[string]*prometheus.CounterVec{}

	p.CounterVecMetrics["heplify_method_response"] = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "heplify_method_response", Help: "SIP method and response counter"}, []string{"target_name", "response", "method"})
	p.CounterVecMetrics["heplify_packets_total"] = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "heplify_packets_total", Help: "Total packets by HEP type"}, []string{"type"})
	p.GaugeVecMetrics["heplify_packets_size"] = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "heplify_packets_size", Help: "Packet size by HEP type"}, []string{"type"})
	p.GaugeVecMetrics["heplify_xrtp_cs"] = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "heplify_xrtp_cs", Help: "XRTP call setup time"}, []string{"target_name"})
	p.GaugeVecMetrics["heplify_xrtp_jir"] = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "heplify_xrtp_jir", Help: "XRTP received jitter"}, []string{"target_name"})
	p.GaugeVecMetrics["heplify_xrtp_jis"] = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "heplify_xrtp_jis", Help: "XRTP sent jitter"}, []string{"target_name"})
	p.GaugeVecMetrics["heplify_xrtp_plr"] = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "heplify_xrtp_plr", Help: "XRTP received packets lost"}, []string{"target_name"})
	p.GaugeVecMetrics["heplify_xrtp_pls"] = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "heplify_xrtp_pls", Help: "XRTP sent packets lost"}, []string{"target_name"})
	p.GaugeVecMetrics["heplify_xrtp_dle"] = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "heplify_xrtp_dle", Help: "XRTP mean rtt"}, []string{"target_name"})
	p.GaugeVecMetrics["heplify_xrtp_mos"] = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "heplify_xrtp_mos", Help: "XRTP mos"}, []string{"target_name"})

	p.GaugeMetrics["heplify_rtcp_fraction_lost"] = prometheus.NewGauge(prometheus.GaugeOpts{Name: "heplify_rtcp_fraction_lost", Help: "RTCP fraction lost"})
	p.GaugeMetrics["heplify_rtcp_packets_lost"] = prometheus.NewGauge(prometheus.GaugeOpts{Name: "heplify_rtcp_packets_lost", Help: "RTCP packets lost"})
	p.GaugeMetrics["heplify_rtcp_jitter"] = prometheus.NewGauge(prometheus.GaugeOpts{Name: "heplify_rtcp_jitter", Help: "RTCP jitter"})
	p.GaugeMetrics["heplify_rtcp_dlsr"] = prometheus.NewGauge(prometheus.GaugeOpts{Name: "heplify_rtcp_dlsr", Help: "RTCP dlsr"})

	p.GaugeMetrics["heplify_rtcpxr_fraction_lost"] = prometheus.NewGauge(prometheus.GaugeOpts{Name: "heplify_rtcpxr_fraction_lost", Help: "RTCPXR fraction lost"})
	p.GaugeMetrics["heplify_rtcpxr_fraction_discard"] = prometheus.NewGauge(prometheus.GaugeOpts{Name: "heplify_rtcpxr_fraction_discard", Help: "RTCPXR fraction discard"})
	p.GaugeMetrics["heplify_rtcpxr_burst_density"] = prometheus.NewGauge(prometheus.GaugeOpts{Name: "heplify_rtcpxr_burst_density", Help: "RTCPXR burst density"})
	p.GaugeMetrics["heplify_rtcpxr_gap_density"] = prometheus.NewGauge(prometheus.GaugeOpts{Name: "heplify_rtcpxr_gap_density", Help: "RTCPXR gap density"})
	p.GaugeMetrics["heplify_rtcpxr_burst_duration"] = prometheus.NewGauge(prometheus.GaugeOpts{Name: "heplify_rtcpxr_burst_duration", Help: "RTCPXR burst duration"})
	p.GaugeMetrics["heplify_rtcpxr_gap_duration"] = prometheus.NewGauge(prometheus.GaugeOpts{Name: "heplify_rtcpxr_gap_duration", Help: "RTCPXR gap duration"})
	p.GaugeMetrics["heplify_rtcpxr_round_trip_delay"] = prometheus.NewGauge(prometheus.GaugeOpts{Name: "heplify_rtcpxr_round_trip_delay", Help: "RTCPXR round trip delay"})
	p.GaugeMetrics["heplify_rtcpxr_end_system_delay"] = prometheus.NewGauge(prometheus.GaugeOpts{Name: "heplify_rtcpxr_end_system_delay", Help: "RTCPXR end system delay"})

	if config.Setting.RTPAgentStats {
		p.GaugeMetrics["heplify_rtpagent_delta"] = prometheus.NewGauge(prometheus.GaugeOpts{Name: "heplify_rtpagent_delta", Help: "RTPAgent delta"})
		p.GaugeMetrics["heplify_rtpagent_jitter"] = prometheus.NewGauge(prometheus.GaugeOpts{Name: "heplify_rtpagent_jitter", Help: "RTPAgent jitter"})
		p.GaugeMetrics["heplify_rtpagent_mos"] = prometheus.NewGauge(prometheus.GaugeOpts{Name: "heplify_rtpagent_mos", Help: "RTPAgent mos"})
		p.GaugeMetrics["heplify_rtpagent_packets_lost"] = prometheus.NewGauge(prometheus.GaugeOpts{Name: "heplify_rtpagent_packets_lost", Help: "RTPAgent packets lost"})
		p.GaugeMetrics["heplify_rtpagent_rfactor"] = prometheus.NewGauge(prometheus.GaugeOpts{Name: "heplify_rtpagent_rfactor", Help: "RTPAgent rfactor"})
		p.GaugeMetrics["heplify_rtpagent_skew"] = prometheus.NewGauge(prometheus.GaugeOpts{Name: "heplify_rtpagent_skew", Help: "RTPAgent skew"})

		p.rtpPaths = [][]string{
			[]string{"DELTA"},
			[]string{"JITTER"},
			[]string{"MOS"},
			[]string{"PACKET_LOSS"},
			[]string{"RFACTOR"},
			[]string{"SKEW"},
		}
	}

	if config.Setting.HoraclifixStats {
		p.GaugeVecMetrics["horaclifix_inc_rtp_mos"] = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "horaclifix_inc_rtp_mos", Help: "Incoming RTP MOS"}, []string{"sbc_name", "inc_realm", "out_realm"})
		p.GaugeVecMetrics["horaclifix_inc_rtp_rval"] = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "horaclifix_inc_rtp_rval", Help: "Incoming RTP rVal"}, []string{"sbc_name", "inc_realm", "out_realm"})
		p.GaugeVecMetrics["horaclifix_inc_rtp_packets"] = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "horaclifix_inc_rtp_packets", Help: "Incoming RTP packets"}, []string{"sbc_name", "inc_realm", "out_realm"})
		p.GaugeVecMetrics["horaclifix_inc_rtp_lost_packets"] = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "horaclifix_inc_rtp_lost_packets", Help: "Incoming RTP lostPackets"}, []string{"sbc_name", "inc_realm", "out_realm"})
		p.GaugeVecMetrics["horaclifix_inc_rtp_avg_jitter"] = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "horaclifix_inc_rtp_avg_jitter", Help: "Incoming RTP avgJitter"}, []string{"sbc_name", "inc_realm", "out_realm"})
		p.GaugeVecMetrics["horaclifix_inc_rtp_max_jitter"] = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "horaclifix_inc_rtp_max_jitter", Help: "Incoming RTP maxJitter"}, []string{"sbc_name", "inc_realm", "out_realm"})
		p.GaugeVecMetrics["horaclifix_inc_rtcp_packets"] = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "horaclifix_inc_rtcp_packets", Help: "Incoming RTCP packets"}, []string{"sbc_name", "inc_realm", "out_realm"})
		p.GaugeVecMetrics["horaclifix_inc_rtcp_lost_packets"] = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "horaclifix_inc_rtcp_lost_packets", Help: "Incoming RTCP lostPackets"}, []string{"sbc_name", "inc_realm", "out_realm"})
		p.GaugeVecMetrics["horaclifix_inc_rtcp_avg_jitter"] = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "horaclifix_inc_rtcp_avg_jitter", Help: "Incoming RTCP avgJitter"}, []string{"sbc_name", "inc_realm", "out_realm"})
		p.GaugeVecMetrics["horaclifix_inc_rtcp_max_jitter"] = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "horaclifix_inc_rtcp_max_jitter", Help: "Incoming RTCP maxJitter"}, []string{"sbc_name", "inc_realm", "out_realm"})
		p.GaugeVecMetrics["horaclifix_inc_rtcp_avg_lat"] = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "horaclifix_inc_rtcp_avg_lat", Help: "Incoming RTCP avgLat"}, []string{"sbc_name", "inc_realm", "out_realm"})
		p.GaugeVecMetrics["horaclifix_inc_rtcp_max_lat"] = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "horaclifix_inc_rtcp_max_lat", Help: "Incoming RTCP maxLat"}, []string{"sbc_name", "inc_realm", "out_realm"})
		p.GaugeVecMetrics["horaclifix_out_rtp_mos"] = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "horaclifix_out_rtp_mos", Help: "Outgoing RTP MOS"}, []string{"sbc_name", "inc_realm", "out_realm"})
		p.GaugeVecMetrics["horaclifix_out_rtp_rval"] = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "horaclifix_out_rtp_rval", Help: "Outgoing RTP rVal"}, []string{"sbc_name", "inc_realm", "out_realm"})
		p.GaugeVecMetrics["horaclifix_out_rtp_packets"] = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "horaclifix_out_rtp_packets", Help: "Outgoing RTP packets"}, []string{"sbc_name", "inc_realm", "out_realm"})
		p.GaugeVecMetrics["horaclifix_out_rtp_lost_packets"] = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "horaclifix_out_rtp_lost_packets", Help: "Outgoing RTP lostPackets"}, []string{"sbc_name", "inc_realm", "out_realm"})
		p.GaugeVecMetrics["horaclifix_out_rtp_avg_jitter"] = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "horaclifix_out_rtp_avg_jitter", Help: "Outgoing RTP avgJitter"}, []string{"sbc_name", "inc_realm", "out_realm"})
		p.GaugeVecMetrics["horaclifix_out_rtp_max_jitter"] = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "horaclifix_out_rtp_max_jitter", Help: "Outgoing RTP maxJitter"}, []string{"sbc_name", "inc_realm", "out_realm"})
		p.GaugeVecMetrics["horaclifix_out_rtcp_packets"] = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "horaclifix_out_rtcp_packets", Help: "Outgoing RTCP packets"}, []string{"sbc_name", "inc_realm", "out_realm"})
		p.GaugeVecMetrics["horaclifix_out_rtcp_lost_packets"] = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "horaclifix_out_rtcp_lost_packets", Help: "Outgoing RTCP lostPackets"}, []string{"sbc_name", "inc_realm", "out_realm"})
		p.GaugeVecMetrics["horaclifix_out_rtcp_avg_jitter"] = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "horaclifix_out_rtcp_avg_jitter", Help: "Outgoing RTCP avgJitter"}, []string{"sbc_name", "inc_realm", "out_realm"})
		p.GaugeVecMetrics["horaclifix_out_rtcp_max_jitter"] = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "horaclifix_out_rtcp_max_jitter", Help: "Outgoing RTCP maxJitter"}, []string{"sbc_name", "inc_realm", "out_realm"})
		p.GaugeVecMetrics["horaclifix_out_rtcp_avg_lat"] = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "horaclifix_out_rtcp_avg_lat", Help: "Outgoing RTCP avgLat"}, []string{"sbc_name", "inc_realm", "out_realm"})
		p.GaugeVecMetrics["horaclifix_out_rtcp_max_lat"] = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "horaclifix_out_rtcp_max_lat", Help: "Outgoing RTCP maxLat"}, []string{"sbc_name", "inc_realm", "out_realm"})

		p.horaclifixPaths = [][]string{
			[]string{"NAME"},
			[]string{"INC_REALM"},
			[]string{"OUT_REALM"},
			//[]string{"INC_ID"},
			//[]string{"OUT_ID"},
			[]string{"INC_MOS"},
			[]string{"INC_RVAL"},
			//[]string{"INC_RTP_BYTE"},
			[]string{"INC_RTP_PK"},
			[]string{"INC_RTP_PK_LOSS"},
			[]string{"INC_RTP_AVG_JITTER"},
			[]string{"INC_RTP_MAX_JITTER"},
			//[]string{"INC_RTCP_BYTE"},
			[]string{"INC_RTCP_PK"},
			[]string{"INC_RTCP_PK_LOSS"},
			[]string{"INC_RTCP_AVG_JITTER"},
			[]string{"INC_RTCP_MAX_JITTER"},
			[]string{"INC_RTCP_AVG_LAT"},
			[]string{"INC_RTCP_MAX_LAT"},
			//[]string{"CALLER_VLAN"},
			//[]string{"CALLER_SRC_IP"},
			//[]string{"CALLER_SRC_PORT"},
			//[]string{"CALLER_DST_IP"},
			//[]string{"CALLER_DST_PORT"},
			[]string{"OUT_MOS"},
			[]string{"OUT_RVAL"},
			//[]string{"OUT_RTP_BYTE"},
			[]string{"OUT_RTP_PK"},
			[]string{"OUT_RTP_PK_LOSS"},
			[]string{"OUT_RTP_AVG_JITTER"},
			[]string{"OUT_RTP_MAX_JITTER"},
			//[]string{"OUT_RTCP_BYTE"},
			[]string{"OUT_RTCP_PK"},
			[]string{"OUT_RTCP_PK_LOSS"},
			[]string{"OUT_RTCP_AVG_JITTER"},
			[]string{"OUT_RTCP_MAX_JITTER"},
			[]string{"OUT_RTCP_AVG_LAT"},
			[]string{"OUT_RTCP_MAX_LAT"},
			//[]string{"CALLEE_VLAN"},
			//[]string{"CALLEE_SRC_IP"},
			//[]string{"CALLEE_SRC_PORT"},
			//[]string{"CALLEE_DST_IP"},
			//[]string{"CALLEE_DST_PORT"},
			//[]string{"MEDIA_TYPE"},
		}
	}

	p.rtcpPaths = [][]string{
		[]string{"report_blocks", "[0]", "fraction_lost"},
		[]string{"report_blocks", "[0]", "packets_lost"},
		[]string{"report_blocks", "[0]", "ia_jitter"},
		[]string{"report_blocks", "[0]", "dlsr"},
		[]string{"report_blocks_xr", "fraction_lost"},
		[]string{"report_blocks_xr", "fraction_discard"},
		[]string{"report_blocks_xr", "burst_density"},
		[]string{"report_blocks_xr", "gap_density"},
		[]string{"report_blocks_xr", "burst_duration"},
		[]string{"report_blocks_xr", "gap_duration"},
		[]string{"report_blocks_xr", "round_trip_delay"},
		[]string{"report_blocks_xr", "end_system_delay"},
	}

	for k := range p.GaugeMetrics {
		prometheus.MustRegister(p.GaugeMetrics[k])
	}
	for k := range p.GaugeVecMetrics {
		prometheus.MustRegister(p.GaugeVecMetrics[k])
	}
	for k := range p.CounterVecMetrics {
		prometheus.MustRegister(p.CounterVecMetrics[k])
	}

	go func() {
		http.Handle("/metrics", promhttp.Handler())
		err = http.ListenAndServe(config.Setting.PromAddr, nil)
		if err != nil {
			logp.Err("%v", err)
		}
	}()
	return err
}

func (p *Prometheus) collect(hCh chan *decoder.HEP) {
	var (
		pkt *decoder.HEP
		ok  bool
	)

	for {
		pkt, ok = <-hCh
		if !ok {
			break
		}

		if pkt.ProtoType == 1 {
			p.CounterVecMetrics["heplify_packets_total"].WithLabelValues("sip").Inc()
			p.GaugeVecMetrics["heplify_packets_size"].WithLabelValues("sip").Set(float64(len(pkt.Payload)))
		} else if pkt.ProtoType == 5 {
			p.CounterVecMetrics["heplify_packets_total"].WithLabelValues("rtcp").Inc()
			p.GaugeVecMetrics["heplify_packets_size"].WithLabelValues("rtcp").Set(float64(len(pkt.Payload)))
		} else if pkt.ProtoType == 38 {
			p.CounterVecMetrics["heplify_packets_total"].WithLabelValues("horaclifix").Inc()
			p.GaugeVecMetrics["heplify_packets_size"].WithLabelValues("horaclifix").Set(float64(len(pkt.Payload)))
		} else if pkt.ProtoType == 100 {
			p.CounterVecMetrics["heplify_packets_total"].WithLabelValues("log").Inc()
			p.GaugeVecMetrics["heplify_packets_size"].WithLabelValues("log").Set(float64(len(pkt.Payload)))
		}

		if pkt.SIP != nil && pkt.ProtoType == 1 {
			if !p.TargetEmpty {
				for k, tn := range p.TargetName {
					if strings.Contains(pkt.SrcIP, p.TargetIP[k]) || strings.Contains(pkt.DstIP, p.TargetIP[k]) {
						p.CounterVecMetrics["heplify_method_response"].WithLabelValues(tn, pkt.SIP.StartLine.Method, pkt.SIP.CseqMethod).Inc()

						if pkt.SIP.RTPStatVal != "" {
							p.dissectXRTPStats(tn, pkt.SIP.RTPStatVal)
						}
					}
				}
			} else {
				_, err := p.Cache.Get([]byte(pkt.SIP.CallID + pkt.SIP.StartLine.Method + pkt.SIP.CseqMethod))
				if err == nil {
					continue
				}
				err = p.Cache.Set([]byte(pkt.SIP.CallID+pkt.SIP.StartLine.Method+pkt.SIP.CseqMethod), nil, 600)
				if err != nil {
					logp.Warn("%v", err)
				}

				p.CounterVecMetrics["heplify_method_response"].WithLabelValues("", pkt.SIP.StartLine.Method, pkt.SIP.CseqMethod).Inc()

				if pkt.SIP.RTPStatVal != "" {
					p.dissectXRTPStats("", pkt.SIP.RTPStatVal)
				}
			}
		} else if pkt.ProtoType == 5 {
			p.dissectRTCPStats([]byte(pkt.Payload))
		} else if pkt.ProtoType == 34 && config.Setting.RTPAgentStats {
			p.dissectRTPStats([]byte(pkt.Payload))
		} else if pkt.ProtoType == 38 && config.Setting.HoraclifixStats {
			p.dissectHoraclifixStats([]byte(pkt.Payload))
		}
	}
}

func (p *Prometheus) dissectXRTPStats(tn, stats string) {
	var err error
	cs, pr, ps, plr, pls, jir, jis, dle, r, mos := 0, 0, 0, 0, 0, 0, 0, 0, 0.0, 0.0
	m := make(map[string]string)
	sr := strings.Split(stats, ";")

	for _, pair := range sr {
		ss := strings.Split(pair, "=")
		if len(ss) == 2 {
			m[ss[0]] = ss[1]
		}
	}

	if v, ok := m["CS"]; ok {
		if len(v) >= 1 {
			cs, err = strconv.Atoi(v)
			if err == nil {
				p.GaugeVecMetrics["heplify_xrtp_cs"].WithLabelValues(tn).Set(float64(cs / 1000))
			} else {
				logp.Err("%v", err)
			}
		}
	}
	if v, ok := m["PR"]; ok {
		if len(v) >= 1 {
			pr, err = strconv.Atoi(v)
			if err == nil {
			} else {
				logp.Err("%v", err)
			}
		}
	}
	if v, ok := m["PS"]; ok {
		if len(v) >= 1 {
			ps, err = strconv.Atoi(v)
			if err == nil {
			} else {
				logp.Err("%v", err)
			}
		}
	}
	if v, ok := m["PL"]; ok {
		if len(v) >= 1 {
			pl := strings.Split(v, ",")
			if len(pl) == 2 {
				plr, err = strconv.Atoi(pl[0])
				if err == nil {
					p.GaugeVecMetrics["heplify_xrtp_plr"].WithLabelValues(tn).Set(float64(plr))
				} else {
					logp.Err("%v", err)
				}
				pls, err = strconv.Atoi(pl[1])
				if err == nil {
					p.GaugeVecMetrics["heplify_xrtp_pls"].WithLabelValues(tn).Set(float64(pls))
				} else {
					logp.Err("%v", err)
				}
			}
		}
	}
	if v, ok := m["JI"]; ok {
		if len(v) >= 1 {
			ji := strings.Split(v, ",")
			if len(ji) == 2 {
				jir, err = strconv.Atoi(ji[0])
				if err == nil {
					p.GaugeVecMetrics["heplify_xrtp_jir"].WithLabelValues(tn).Set(float64(jir))
				} else {
					logp.Err("%v", err)
				}
				jis, err = strconv.Atoi(ji[1])
				if err == nil {
					p.GaugeVecMetrics["heplify_xrtp_jis"].WithLabelValues(tn).Set(float64(jis))
				} else {
					logp.Err("%v", err)
				}
			}
		}
	}
	if v, ok := m["DL"]; ok {
		if len(v) >= 1 {
			dl := strings.Split(v, ",")
			if len(dl) == 3 {
				dle, err = strconv.Atoi(dl[0])
				if err == nil {
					p.GaugeVecMetrics["heplify_xrtp_dle"].WithLabelValues(tn).Set(float64(dle))
				} else {
					logp.Err("%v", err)
				}
			}
		}
	}

	if pr == 0 && ps == 0 {
		pr, ps = 1, 1
	}

	loss := ((plr + pls) * 100) / (pr + ps)
	el := (jir * 2) + (dle + 10)

	if el < 160 {
		r = 93.2 - (float64(el) / 40)
	} else {
		r = 93.2 - (float64(el-120) / 10)
	}
	r = r - (float64(loss) * 2.5)

	mos = 1 + (0.035)*r + (0.000007)*r*(r-60)*(100-r)
	p.GaugeVecMetrics["heplify_xrtp_mos"].WithLabelValues(tn).Set(mos)

}

func (p *Prometheus) dissectRTCPStats(data []byte) {
	jsonparser.EachKey(data, func(idx int, value []byte, vt jsonparser.ValueType, err error) {
		switch idx {
		case 0:
			if fractionLost, err := jsonparser.ParseFloat(value); err == nil {
				p.GaugeMetrics["heplify_rtcp_fraction_lost"].Set(normMax(fractionLost))
			}
		case 1:
			if packetsLost, err := jsonparser.ParseFloat(value); err == nil {
				p.GaugeMetrics["heplify_rtcp_packets_lost"].Set(normMax(packetsLost))
			}
		case 2:
			if iaJitter, err := jsonparser.ParseFloat(value); err == nil {
				p.GaugeMetrics["heplify_rtcp_jitter"].Set(normMax(iaJitter))
			}
		case 3:
			if dlsr, err := jsonparser.ParseFloat(value); err == nil {
				p.GaugeMetrics["heplify_rtcp_dlsr"].Set(normMax(dlsr))
			}
		case 4:
			if fractionLost, err := jsonparser.ParseFloat(value); err == nil {
				p.GaugeMetrics["heplify_rtcpxr_fraction_lost"].Set(fractionLost)
			}
		case 5:
			if fractionDiscard, err := jsonparser.ParseFloat(value); err == nil {
				p.GaugeMetrics["heplify_rtcpxr_fraction_discard"].Set(fractionDiscard)
			}
		case 6:
			if burstDensity, err := jsonparser.ParseFloat(value); err == nil {
				p.GaugeMetrics["heplify_rtcpxr_burst_density"].Set(burstDensity)
			}
		case 7:
			if gapDensity, err := jsonparser.ParseFloat(value); err == nil {
				p.GaugeMetrics["heplify_rtcpxr_gap_density"].Set(gapDensity)
			}
		case 8:
			if burstDuration, err := jsonparser.ParseFloat(value); err == nil {
				p.GaugeMetrics["heplify_rtcpxr_burst_duration"].Set(burstDuration)
			}
		case 9:
			if gapDuration, err := jsonparser.ParseFloat(value); err == nil {
				p.GaugeMetrics["heplify_rtcpxr_gap_duration"].Set(gapDuration)
			}
		case 10:
			if roundTripDelay, err := jsonparser.ParseFloat(value); err == nil {
				p.GaugeMetrics["heplify_rtcpxr_round_trip_delay"].Set(roundTripDelay)
			}
		case 11:
			if endSystemDelay, err := jsonparser.ParseFloat(value); err == nil {
				p.GaugeMetrics["heplify_rtcpxr_end_system_delay"].Set(endSystemDelay)
			}
		}
	}, p.rtcpPaths...)
}

func (p *Prometheus) dissectRTPStats(data []byte) {
	jsonparser.EachKey(data, func(idx int, value []byte, vt jsonparser.ValueType, err error) {
		switch idx {
		case 0:
			if delta, err := jsonparser.ParseFloat(value); err == nil {
				p.GaugeMetrics["heplify_rtpagent_delta"].Set(delta)
			}
		case 1:
			if iaJitter, err := jsonparser.ParseFloat(value); err == nil {
				p.GaugeMetrics["heplify_rtpagent_jitter"].Set(iaJitter)
			}
		case 2:
			if mos, err := jsonparser.ParseFloat(value); err == nil {
				p.GaugeMetrics["heplify_rtpagent_mos"].Set(mos)
			}
		case 3:
			if packetsLost, err := jsonparser.ParseFloat(value); err == nil {
				p.GaugeMetrics["heplify_rtpagent_packets_lost"].Set(packetsLost)
			}
		case 4:
			if rfactor, err := jsonparser.ParseFloat(value); err == nil {
				p.GaugeMetrics["heplify_rtpagent_rfactor"].Set(rfactor)
			}
		case 5:
			if skew, err := jsonparser.ParseFloat(value); err == nil {
				p.GaugeMetrics["heplify_rtpagent_skew"].Set(skew)
			}
		}
	}, p.rtpPaths...)
}

func (p *Prometheus) dissectHoraclifixStats(data []byte) {
	var sbcName, incRealm, outRealm string

	jsonparser.EachKey(data, func(idx int, value []byte, vt jsonparser.ValueType, err error) {
		switch idx {
		case 0:
			if sbcName, err = jsonparser.ParseString(value); err != nil {
				logp.Warn("could not decode sbcName %s from horaclifix report", string(sbcName))
				return
			}
		case 1:
			if incRealm, err = jsonparser.ParseString(value); err != nil {
				logp.Warn("could not decode incRealm %s from horaclifix report", string(incRealm))
				return
			}
		case 2:
			if outRealm, err = jsonparser.ParseString(value); err != nil {
				logp.Warn("could not decode outRealm %s from horaclifix report", string(outRealm))
				return
			}
		case 3:
			if incMos, err := jsonparser.ParseFloat(value); err == nil {
				p.GaugeVecMetrics["horaclifix_inc_rtp_mos"].WithLabelValues(sbcName, incRealm, outRealm).Set(normMax(incMos / 100))
			}
		case 4:
			if incRval, err := jsonparser.ParseFloat(value); err == nil {
				p.GaugeVecMetrics["horaclifix_inc_rtp_rval"].WithLabelValues(sbcName, incRealm, outRealm).Set(normMax(incRval / 100))
			}
		case 5:
			if incRtpPackets, err := jsonparser.ParseFloat(value); err == nil {
				p.GaugeVecMetrics["horaclifix_inc_rtp_packets"].WithLabelValues(sbcName, incRealm, outRealm).Set(normMax(incRtpPackets))
			}
		case 6:
			if incRtpLostPackets, err := jsonparser.ParseFloat(value); err == nil {
				p.GaugeVecMetrics["horaclifix_inc_rtp_lost_packets"].WithLabelValues(sbcName, incRealm, outRealm).Set(normMax(incRtpLostPackets))
			}
		case 7:
			if incRtpAvgJitter, err := jsonparser.ParseFloat(value); err == nil {
				p.GaugeVecMetrics["horaclifix_inc_rtp_avg_jitter"].WithLabelValues(sbcName, incRealm, outRealm).Set(normMax(incRtpAvgJitter))
			}
		case 8:
			if incRtpMaxJitter, err := jsonparser.ParseFloat(value); err == nil {
				p.GaugeVecMetrics["horaclifix_inc_rtp_max_jitter"].WithLabelValues(sbcName, incRealm, outRealm).Set(normMax(incRtpMaxJitter))
			}
		case 9:
			if incRtcpPackets, err := jsonparser.ParseFloat(value); err == nil {
				p.GaugeVecMetrics["horaclifix_inc_rtcp_packets"].WithLabelValues(sbcName, incRealm, outRealm).Set(normMax(incRtcpPackets))
			}
		case 10:
			if incRtcpLostPackets, err := jsonparser.ParseFloat(value); err == nil {
				p.GaugeVecMetrics["horaclifix_inc_rtcp_lost_packets"].WithLabelValues(sbcName, incRealm, outRealm).Set(normMax(incRtcpLostPackets))
			}
		case 11:
			if incRtcpAvgJitter, err := jsonparser.ParseFloat(value); err == nil {
				p.GaugeVecMetrics["horaclifix_inc_rtcp_avg_jitter"].WithLabelValues(sbcName, incRealm, outRealm).Set(normMax(incRtcpAvgJitter))
			}
		case 12:
			if incRtcpMaxJitter, err := jsonparser.ParseFloat(value); err == nil {
				p.GaugeVecMetrics["horaclifix_inc_rtcp_max_jitter"].WithLabelValues(sbcName, incRealm, outRealm).Set(normMax(incRtcpMaxJitter))
			}
		case 13:
			if incRtcpAvgLat, err := jsonparser.ParseFloat(value); err == nil {
				p.GaugeVecMetrics["horaclifix_inc_rtcp_avg_lat"].WithLabelValues(sbcName, incRealm, outRealm).Set(normMax(incRtcpAvgLat))
			}
		case 14:
			if incRtcpMaxLat, err := jsonparser.ParseFloat(value); err == nil {
				p.GaugeVecMetrics["horaclifix_inc_rtcp_max_lat"].WithLabelValues(sbcName, incRealm, outRealm).Set(normMax(incRtcpMaxLat))
			}
		case 15:
			if outMos, err := jsonparser.ParseFloat(value); err == nil {
				p.GaugeVecMetrics["horaclifix_out_rtp_mos"].WithLabelValues(sbcName, incRealm, outRealm).Set(normMax(outMos / 100))
			}
		case 16:
			if outRval, err := jsonparser.ParseFloat(value); err == nil {
				p.GaugeVecMetrics["horaclifix_out_rtp_rval"].WithLabelValues(sbcName, incRealm, outRealm).Set(normMax(outRval / 100))
			}
		case 17:
			if outRtpPackets, err := jsonparser.ParseFloat(value); err == nil {
				p.GaugeVecMetrics["horaclifix_out_rtp_packets"].WithLabelValues(sbcName, incRealm, outRealm).Set(normMax(outRtpPackets))
			}
		case 18:
			if outRtpLostPackets, err := jsonparser.ParseFloat(value); err == nil {
				p.GaugeVecMetrics["horaclifix_out_rtp_lost_packets"].WithLabelValues(sbcName, incRealm, outRealm).Set(normMax(outRtpLostPackets))
			}
		case 19:
			if outRtpAvgJitter, err := jsonparser.ParseFloat(value); err == nil {
				p.GaugeVecMetrics["horaclifix_out_rtp_avg_jitter"].WithLabelValues(sbcName, incRealm, outRealm).Set(normMax(outRtpAvgJitter))
			}
		case 20:
			if outRtpMaxJitter, err := jsonparser.ParseFloat(value); err == nil {
				p.GaugeVecMetrics["horaclifix_out_rtp_max_jitter"].WithLabelValues(sbcName, incRealm, outRealm).Set(normMax(outRtpMaxJitter))
			}
		case 21:
			if outRtcpPackets, err := jsonparser.ParseFloat(value); err == nil {
				p.GaugeVecMetrics["horaclifix_out_rtcp_packets"].WithLabelValues(sbcName, incRealm, outRealm).Set(normMax(outRtcpPackets))
			}
		case 22:
			if outRtcpLostPackets, err := jsonparser.ParseFloat(value); err == nil {
				p.GaugeVecMetrics["horaclifix_out_rtcp_lost_packets"].WithLabelValues(sbcName, incRealm, outRealm).Set(normMax(outRtcpLostPackets))
			}
		case 23:
			if outRtcpAvgJitter, err := jsonparser.ParseFloat(value); err == nil {
				p.GaugeVecMetrics["horaclifix_out_rtcp_avg_jitter"].WithLabelValues(sbcName, incRealm, outRealm).Set(normMax(outRtcpAvgJitter))
			}
		case 24:
			if outRtcpMaxJitter, err := jsonparser.ParseFloat(value); err == nil {
				p.GaugeVecMetrics["horaclifix_out_rtcp_max_jitter"].WithLabelValues(sbcName, incRealm, outRealm).Set(normMax(outRtcpMaxJitter))
			}
		case 25:
			if outRtcpAvgLat, err := jsonparser.ParseFloat(value); err == nil {
				p.GaugeVecMetrics["horaclifix_out_rtcp_avg_lat"].WithLabelValues(sbcName, incRealm, outRealm).Set(normMax(outRtcpAvgLat))
			}
		case 26:
			if outRtcpMaxLat, err := jsonparser.ParseFloat(value); err == nil {
				p.GaugeVecMetrics["horaclifix_out_rtcp_max_lat"].WithLabelValues(sbcName, incRealm, outRealm).Set(normMax(outRtcpMaxLat))
			}
		}
	}, p.horaclifixPaths...)
}
