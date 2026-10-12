package kcp_test

import (
	"testing"

	"github.com/liangpengcheng/qcontinuum/kcp"
	"github.com/liangpengcheng/qcontinuum/network"
)

func TestPublicPeerLiteralCompatibility(t *testing.T) {
	peer := &kcp.KCPPeer{ID: 12345, Flag: 7}
	client := peer.ToClientPeer()
	if client.ID != peer.ID || client.Flag != peer.Flag {
		t.Fatal("literal fields are not shared with the business view")
	}
	client.ID = 67890
	if peer.ID != 67890 {
		t.Fatal("literal peer lost shared identity")
	}
	peer.Close()
	peer.Close()
	if peer.GetState() != kcp.KCPPeerStateClosed || client.GetState() != network.PeerStateClosed {
		t.Fatal("literal peer could not be closed safely")
	}
}

func TestPeerWithoutConnectionCanClose(t *testing.T) {
	peers := []*kcp.KCPPeer{{}, kcp.NewKCPPeer(nil, network.NewProcessor())}
	for _, peer := range peers {
		peer.Close()
		if peer.GetState() != kcp.KCPPeerStateClosed || peer.RemoteAddr() != nil {
			t.Fatal("peer without a connection did not close safely")
		}
	}
}
