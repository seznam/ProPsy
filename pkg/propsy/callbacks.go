package propsy

import (
	"context"
	"errors"
	api "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/sirupsen/logrus"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"sync"
)

type EnvoyCertificateValidator struct {
	VerifyCN       bool
	streamContexts map[int64]context.Context
	mu             sync.Mutex
}

func (P *EnvoyCertificateValidator) Add(ctx context.Context, streamid int64) {
	P.mu.Lock()
	defer P.mu.Unlock()

	if P.streamContexts == nil {
		P.streamContexts = map[int64]context.Context{}
	}
	P.streamContexts[streamid] = ctx
}

func (P *EnvoyCertificateValidator) VerifyStream(streamid int64, dr *api.DiscoveryRequest) error {
	if !P.VerifyCN {
		return nil
	}

	P.mu.Lock()
	defer P.mu.Unlock()

	if ctx, ok := P.streamContexts[streamid]; !ok {
		logrus.Warn("there was no context to this streamid")
		return errors.New("unknown stream id")
	} else {
		return P.VerifyCtx(ctx, dr)
	}
}

func (P *EnvoyCertificateValidator) VerifyCtx(ctx context.Context, dr *api.DiscoveryRequest) error {
	if !P.VerifyCN {
		return nil
	}

	nodeId := dr.Node.Id
	if peer, ok := peer.FromContext(ctx); !ok {
		logrus.Warn("unknown context")
		return errors.New("unknown context")
	} else {
		tlsInfo := peer.AuthInfo.(credentials.TLSInfo)
		for i := range tlsInfo.State.PeerCertificates {
			if tlsInfo.State.PeerCertificates[i].Subject.CommonName == nodeId {
				logrus.Debugf("successfully verified CN %s", nodeId)
				return nil
			}
		}
	}

	logrus.Debugf("couldn't verify node %s", nodeId)
	return errors.New("couldn't verify the node")
}

type PropsyCallbacks struct {
	cache *EnvoyCertificateValidator
}

func (P PropsyCallbacks) OnStreamOpen(ctx context.Context, streamid int64, typeurl string) error {
	logrus.Debug("OnStreamOpen")
	P.cache.Add(ctx, streamid)
	return nil
}

func (PropsyCallbacks) OnStreamClosed(int64) {
	logrus.Debug("OnStreamClosed")
}

func (P PropsyCallbacks) OnStreamRequest(streamid int64, dr *api.DiscoveryRequest) error {
	logrus.Debug("OnStreamRequest")
	return P.cache.VerifyStream(streamid, dr)
}

func (PropsyCallbacks) OnStreamResponse(int64, *api.DiscoveryRequest, *api.DiscoveryResponse) {
	logrus.Debug("OnStreamResponse")
}

func (P PropsyCallbacks) OnFetchRequest(ctx context.Context, dr *api.DiscoveryRequest) error {
	logrus.Debug("OnFetchRequest")
	return P.cache.VerifyCtx(ctx, dr)
}

func (PropsyCallbacks) OnFetchResponse(*api.DiscoveryRequest, *api.DiscoveryResponse) {
	logrus.Debug("OnFetchResponse")
}
