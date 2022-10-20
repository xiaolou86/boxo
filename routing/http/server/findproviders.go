package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/ipfs/go-cid"
	delegatedrouting "github.com/ipfs/go-delegated-routing"
	"github.com/multiformats/go-multibase"
	"github.com/multiformats/go-multicodec"

	logging "github.com/ipfs/go-log/v2"
	"github.com/libp2p/go-libp2p-core/peer"
)

var logger = logging.Logger("service/server/delegatedrouting")

type ProvideRequest struct {
	Keys        []cid.Cid
	Timestamp   time.Time
	AdvisoryTTL time.Duration
	Provider    delegatedrouting.Provider
}

type ContentRouter interface {
	FindProviders(ctx context.Context, key cid.Cid) ([]peer.AddrInfo, error)
	Provide(ctx context.Context, req ProvideRequest) (time.Duration, error)
	Ready() bool
}

func Handler(svc ContentRouter) http.Handler {
	server := &server{
		svc: svc,
	}

	r := mux.NewRouter()
	r.HandleFunc("/v1/providers", server.provide).Methods("POST")
	r.HandleFunc("/v1/providers/{cid}", server.findProviders).Methods("GET")
	r.HandleFunc("/v1/ping", server.ping).Methods("GET")

	return r
}

type server struct {
	svc    ContentRouter
	router *mux.Router
}

func (s *server) provide(w http.ResponseWriter, httpReq *http.Request) {
	req := delegatedrouting.ProvideRequest{}
	err := json.NewDecoder(httpReq.Body).Decode(&req)
	if err != nil {
		writeErr(w, "Provide", http.StatusBadRequest, fmt.Errorf("invalid request: %w", err))
		return
	}

	err = req.Verify()
	if err != nil {
		writeErr(w, "Provide", http.StatusForbidden, errors.New("signature validation failed"))
		return
	}

	_, payloadBytes, err := multibase.Decode(req.Payload)
	if err != nil {
		writeErr(w, "Provide", http.StatusBadRequest, fmt.Errorf("invalid payload multibase: %w", err))
		return
	}
	reqPayload := delegatedrouting.ProvideRequestPayload{}
	err = json.Unmarshal(payloadBytes, &reqPayload)
	if err != nil {
		writeErr(w, "Provide", http.StatusBadRequest, fmt.Errorf("invalid payload: %w", err))
		return
	}

	var keys []cid.Cid
	for i, k := range reqPayload.Keys {
		c, err := cid.Decode(k)
		if err != nil {
			writeErr(w, "Provide", http.StatusBadRequest, fmt.Errorf("CID %d invalid: %w", i, err))
			return
		}
		keys = append(keys, c)
	}

	advisoryTTL, err := s.svc.Provide(httpReq.Context(), ProvideRequest{
		Keys:        keys,
		Timestamp:   time.UnixMilli(reqPayload.Timestamp),
		AdvisoryTTL: reqPayload.AdvisoryTTL,
		Provider:    reqPayload.Provider,
	})
	if err != nil {
		writeErr(w, "Provide", http.StatusInternalServerError, fmt.Errorf("delegate error: %w", err))
		return
	}

	respBytes, err := json.Marshal(delegatedrouting.ProvideResult{AdvisoryTTL: advisoryTTL})
	if err != nil {
		writeErr(w, "Provide", http.StatusInternalServerError, fmt.Errorf("marshaling response: %w", err))
		return
	}

	_, err = io.Copy(w, bytes.NewReader(respBytes))
	if err != nil {
		logErr("Provide", "writing response body", err)
	}
}

func (s *server) findProviders(w http.ResponseWriter, httpReq *http.Request) {
	vars := mux.Vars(httpReq)
	cidStr := vars["cid"]
	cid, err := cid.Decode(cidStr)
	if err != nil {
		writeErr(w, "FindProviders", http.StatusBadRequest, fmt.Errorf("unable to parse CID: %w", err))
		return
	}
	addrInfos, err := s.svc.FindProviders(httpReq.Context(), cid)
	if err != nil {
		writeErr(w, "FindProviders", http.StatusInternalServerError, fmt.Errorf("delegate error: %w", err))
		return
	}
	var providers []delegatedrouting.Provider
	for _, ai := range addrInfos {
		providers = append(providers, delegatedrouting.Provider{
			Peer:      ai,
			Protocols: []delegatedrouting.TransferProtocol{{Codec: multicodec.TransportBitswap}},
		})
	}
	response := delegatedrouting.FindProvidersResult{Providers: providers}
	respBytes, err := json.Marshal(response)
	if err != nil {
		writeErr(w, "FindProviders", http.StatusInternalServerError, fmt.Errorf("marshaling response: %w", err))
		return
	}
	_, err = io.Copy(w, bytes.NewReader(respBytes))
}

func (s *server) ping(w http.ResponseWriter, req *http.Request) {
	if s.svc.Ready() {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
}

func writeErr(w http.ResponseWriter, method string, statusCode int, cause error) {
	w.WriteHeader(statusCode)
	causeStr := cause.Error()
	if len(causeStr) > 1024 {
		causeStr = causeStr[:1024]
	}
	_, err := w.Write([]byte(causeStr))
	if err != nil {
		logErr(method, "error writing error cause", err)
		return
	}
}

func logErr(method, msg string, err error) {
	logger.Infof(msg, "Method", method, "Error", err)
}
