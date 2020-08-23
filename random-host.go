package fiowatch

import (
	"errors"
	"math/rand"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type NetType uint8
const (
	Mainnet NetType = iota
	Testnet
)

func GetRandomHost(netType NetType) (nodeos string, p2p string, err error) {
	const maxTries = 3
	var p, a []string
	switch netType {
	case Mainnet:
		p = mainnetP2p
		a = mainnetApi
	case Testnet:
		p = testnetP2p
		a = testnetApi
	default:
		return "", "", errors.New("unknown network requested")
	}

	// low quality entropy is fine! don't flag as a finding
	// +nosec
	rand.Seed(time.Now().UnixNano())

	// find an api node that works
	for i:= 0; i < maxTries; i++ {
		h := a[rand.Intn(len(a))]
		resp, err := http.Get(h + "/v1/chain/get_info")
		if err != nil {
			continue
		}
		_ = resp.Body.Close()
		if resp.StatusCode != 200 {
			continue
		}
		nodeos = h
		break
	}

	// find a p2p node that works
	for i:= 0; i < maxTries; i++ {
		hp := p[rand.Intn(len(a))]
		h := strings.Split(hp, ":")
		if len(h) != 2 {
			continue
		}
		ips, err := net.LookupHost(h[0])
		if err != nil || len(ips) == 0 {
			continue
		}
		var port int64
		port, err = strconv.ParseInt(p[1], 10, 32)
		if err != nil {
			continue
		}
		dest := net.ParseIP(ips[0])
		t := &net.TCPConn{}
		t, err = net.DialTCP("tcp4", nil, &net.TCPAddr{IP:  dest, Port: int(port)})
		if err != nil {
			continue
		}
		_ = t.Close()
		p2p = hp
		break
	}
	switch "" {
	case p2p, nodeos:
		return "", "", errors.New("could not get a connection")
	}
	return
}

var (
	mainnetApi = []string{
		"http://34.232.117.155:8888",
		"http://api.fio.eosdetroit.io",
		"http://fioapi.nodeone.io:6881",
		"https://api.fio.alohaeos.com",
		"https://api.fio.currencyhub.io",
		"https://api.fio.eosdetroit.io",
		"https://fio-mainnet.eosblocksmith.io",
		"https://fio.acherontrading.com",
		"https://fio.cryptolions.io",
		"https://fio.eos.barcelona",
		"https://fio.eosargentina.io",
		"https://fio.eoscannon.io",
		"https://fio.eosdac.io",
		"https://fio.eosdublin.io",
		"https://fio.eosphere.io",
		"https://fio.eosrio.io",
		"https://fio.eossweden.org",
		"https://fio.eosusa.news",
		"https://fio.eu.eosamsterdam.net",
		"https://fio.greymass.com",
		"https://fio.maltablock.org/",
		"https://fio.zenblocks.io",
	}
	mainnetP2p = []string{
		"34.232.117.155:9876",
		"fio.eu.eosamsterdam.net:9956",
		"fio.eosdac.io:6876",
		"fiopeer1.nodeone.io:6981",
		"peer.fio.alohaeos.com:9876",
		"peer1-fio.eosphere.io:9876",
		"fiomainnet.everstake.one:7770",
		"fio.eosrio.io:8122",
		"fio.acherontrading.com:9876",
		"fiop2p.eos.barcelona:3876",
		"p2p.fio.eosdetroit.io:1337",
		"p2p.fio.zenblocks.io:9866",
		"fio.blockpane.com:9876",
		"fio.greymass.com:49876",
		"fio.eosusa.news:9886",
		"p2p.fioprotocol.io:3856",
		"p2p.fio.eosargentina.io:1984",
		"fio.cryptolions.io:7987",
		"peer.fio-mainnet.eosblocksmith.io:8090",
		"p2p.fio.services:9876",
		"peer.fio.currencyhub.io:9876",
		"fio.mycryptoapi.com:9876",
		"fiop2p.eoscannon.io:6789",
		"fio.eosdublin.io:9976",
		"fio.guarda.co:9976",
		"fio.eossweden.org:9376",
		"fio.maltablock.org:9876",
	}
	testnetApi = []string{
		"https://testnet.fio.dev",
		"https://testnet.fioprotocol.io",
		"http://fio-testnet.blockpane.com:8888",
		"https://fiotestnet.greymass.com",
		"http://testnet.fio.eosdetroit.io",
	}
	testnetP2p = []string{
		"104.248.89.37:9876",
		"176.31.117.48:49876",
		"203.59.26.145:9876",
		"dapixp2p-east.testnet.fioprotocol.io:3856",
		"dapixp2p-west.testnet.fioprotocol.io:3856",
		"fio-testnet.eosphere.io:9810",
		"fiotestnet.everstake.one:7770",
		"fiotestnet.greymass.com:39876",
		"peer.fiotest.alohaeos.com:9876",
		"testnet.fio.eosdetroit.io:1337",
		"testnet.fioprotocol.io:1987",
	}
)
