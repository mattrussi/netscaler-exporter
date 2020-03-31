package main

import (
	"encoding/json"
	"math"
	"sync"
	"time"

	"github.com/jbvmio/netscaler"
	"go.uber.org/zap"
)

// RawLBVServerStats is the payload as returned by the Nitro API.
type RawLBVServerStats []byte

// Len returns the size of the underlying []byte.
func (r RawLBVServerStats) Len() int {
	return len(r)
}

// LBVServerStats represents the data returned from the /stat/service Nitro API endpoint
type LBVServerStats struct {
	Name                        string           `json:"name"`
	AvgTimeClientTTLB           string           `json:"avgcltttlb"`
	State                       CurState         `json:"state"`
	VSLBHealth                  string           `json:"vslbhealth"`
	TotalRequests               string           `json:"totalrequests"`
	TotalResponses              string           `json:"totalresponses"`
	TotalRequestBytes           string           `json:"totalrequestbytes"`
	TotalResponseBytes          string           `json:"totalresponsebytes"`
	TotalClientTTLBTransactions string           `json:"totcltttlbtransactions"`
	ActiveServices              string           `json:"actsvcs"`
	TotalHits                   string           `json:"tothits"`
	TotalPktsReceived           string           `json:"totalpktsrecvd"`
	TotalPktsSent               string           `json:"totalpktssent"`
	SurgeCount                  string           `json:"surgecount"`
	SvcSurgeCount               string           `json:"svcsurgecount"`
	VSvrSurgeCount              string           `json:"vsvrsurgecount"`
	LBService                   []LBServiceStats `json:"service"`
}

// LBServiceStats represents the data returned from the /stat/service Nitro API endpoint
type LBServiceStats struct {
	Name string `json:"name"`
	//ServiceName                  string   `json:"servicename"`
	Throughput                   string   `json:"throughput"`
	AvgTimeToFirstByte           string   `json:"avgsvrttfb"`
	State                        CurState `json:"state"`
	TotalRequests                string   `json:"totalrequests"`
	TotalResponses               string   `json:"totalresponses"`
	TotalRequestBytes            string   `json:"totalrequestbytes"`
	TotalResponseBytes           string   `json:"totalresponsebytes"`
	CurrentClientConnections     string   `json:"curclntconnections"`
	SurgeCount                   string   `json:"surgecount"`
	CurrentServerConnections     string   `json:"cursrvrconnections"`
	ServerEstablishedConnections string   `json:"svrestablishedconn"`
	CurrentReusePool             string   `json:"curreusepool"`
	MaxClients                   string   `json:"maxclients"`
	CurrentLoad                  string   `json:"curload"`
	ServiceHits                  string   `json:"vsvrservicehits"`
	ActiveTransactions           string   `json:"activetransactions"`
}

// NitroType implements the NitroData interface.
func (s LBVServerStats) NitroType() string {
	return lbvserverSubsystem
}

// NitroType implements the NitroData interface.
func (s LBServiceStats) NitroType() string {
	return lbvserverSvcSubsystem
}

func processLBVServerStats(P *Pool, wg *sync.WaitGroup) {
	if wg != nil {
		defer wg.Done()
	}
	thisSS := lbvserverSubsystem
	switch {
	case P.metricFlipBit[thisSS].good():
		defer P.metricFlipBit[thisSS].flip()
		switch {
		case P.stopped:
			P.logger.Info("Skipping sybSystem stat collection, process is stopping", zap.String("subSystem", thisSS))
		default:
			P.logger.Debug("Processing subSystem Stats", zap.String("subSystem", thisSS))
			lbvServers, err := GetLBServerServiceStats(P)
			switch {
			case err != nil:
				P.logger.Error("error retrieving data for subSystem stat collection", zap.String("subSystem", thisSS))
				P.insertBackoff(thisSS)
			default:
				P.logger.Debug("processing lbservice stats", zap.String("subSystem", thisSS), zap.Int("number of lbvservers", len(lbvServers)))
				for _, svr := range lbvServers {
					req := newNitroDataReq(svr)
					success := P.submit(req)
					if !success {
						exporterProcessingFailures.WithLabelValues(P.nsInstance, thisSS).Inc()
					}
				}
				go TK.set(P.nsInstance, thisSS, float64(time.Now().UnixNano()))
				P.logger.Debug("subSystem stat collection Complete", zap.String("subSystem", thisSS))
			}
		}
	default:
		P.logger.Info("subSystem stat collection already in progress", zap.String("subSystem", thisSS))
	}
}

// GetLBServerServiceStats retrieves stats for both GSLBServers and GSLBServices.
func GetLBServerServiceStats(P *Pool) ([]LBVServerStats, error) {
	var lbVServers []LBVServerStats
	servers, err := getLBVServerStats(P.client)
	if err != nil {
		exporterAPICollectFailures.WithLabelValues(P.nsInstance, lbvserverSubsystem).Inc()
		return lbVServers, err
	}
	svcChan := make(chan []LBVServerStats, len(servers)+1)
	errChan := make(chan bool, len(servers)+1)
	var controlSize float64 = 40
	control := int(math.Round((float64(len(servers)) / controlSize) + 0.6))
	if control <= 1 {
		control = len(servers)
	}
	var count int
	for count < len(servers) {
		begin := count
		end := count + control
		if end > len(servers) {
			end = len(servers)
		}
		svrGroups := servers[begin:end]
		count = end
		go func(groups []LBVServerStats) {
			for _, grp := range groups {
				var retries int
				s, err := getLBVServerStats(P.client, grp.Name)
			retryLoop:
				for err != nil {
					exporterAPICollectFailures.WithLabelValues(P.nsInstance, lbvserverSvcSubsystem).Inc()
					if retries >= 3 {
						break retryLoop
					}
					time.Sleep(time.Second * time.Duration(retries+1))
					s, err = getLBVServerStats(P.client, grp.Name)
					retries++
				}
				switch {
				case err == nil:
					svcChan <- s
				default:
					errChan <- false
				}

			}
		}(svrGroups)
	}
	for i := 0; i < len(servers); i++ {
		select {
		case <-errChan:
			exporterProcessingFailures.WithLabelValues(P.nsInstance, lbvserverSvcSubsystem).Inc()
		case s := <-svcChan:
			lbVServers = append(lbVServers, s...)
		}
	}
	return lbVServers, nil
}

// GetLBServerServiceStatsOrig retrieves stats for both GSLBServers and GSLBServices.
func GetLBServerServiceStatsOrig(P *Pool) ([]LBVServerStats, error) {
	var lbVServers []LBVServerStats
	servers, err := getLBVServerStats(P.client)
	if err != nil {
		exporterAPICollectFailures.WithLabelValues(P.nsInstance, lbvserverSubsystem).Inc()
		return lbVServers, err
	}
	for _, svr := range servers {
		var retries int
		s, err := getLBVServerStats(P.client, svr.Name)
	retryLoop:
		for err != nil {
			exporterAPICollectFailures.WithLabelValues(P.nsInstance, lbvserverSvcSubsystem).Inc()
			if retries >= 3 {
				break retryLoop
			}
			time.Sleep(time.Millisecond * 100)
			s, err = getLBVServerStats(P.client, svr.Name)
			retries++
		}
		if err == nil {
			lbVServers = append(lbVServers, s...)
		}
	}
	return lbVServers, nil
}

func getLBVServerStats(client *netscaler.NitroClient, target ...string) ([]LBVServerStats, error) {
	var lbVServers []LBVServerStats
	var b []byte
	var err error
	switch len(target) {
	case 0:
		b, err = client.GetAll(netscaler.StatsTypeLBVServer)
	default:
		svr := target[0]
		b, err = client.Get(netscaler.StatsTypeLBVServer, svr+`?statbindings=yes`)
	}
	if err != nil {
		return lbVServers, err
	}
	tmp := struct {
		Target *[]LBVServerStats `json:"lbvserver"`
	}{Target: &lbVServers}
	err = json.Unmarshal(b, &tmp)
	if err != nil {
		return lbVServers, err
	}
	return lbVServers, nil
}
