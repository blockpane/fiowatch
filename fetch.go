package fiowatch

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/fioprotocol/fio-go"
	"github.com/fioprotocol/fio-go/eos"
	"github.com/fioprotocol/fio-go/eos/p2p"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
	"log"
	"runtime"
	"sync"
	"time"
)

type ActionRow struct {
	Contract string
	Action   string
	Actor    string
	Info     string
	TxDetail []byte
	Time     time.Time
	TxId     string
	BlockNum uint32
	Used     string
}

func (t ActionRow) String() string {
	return string(t.TxDetail)
}

var (
	knownAddresses *addressCache
	Abis           *abiCache
)

func WatchBlocks(summary chan *BlockSummary, details chan *ActionRow, quit chan bool, head chan int, lib chan int, diedChan chan bool, heartBeat chan time.Time, slow chan bool, url string, p2pnode string) {
	var stopRequested bool
	quitting := make(chan bool, 1)
	notifyQuitting := func() {
		log.Println("activity monitor: attempting to stop data collection threads")
		if stopRequested {
			return
		}
		stopRequested = true
		close(quitting)
		diedChan <- true
	}
	defer func() {
		if !stopRequested {
			notifyQuitting()
		}
		log.Println("activity monitor: collection thread exiting")
	}()
	go func() {
		for {
			select {
			case <-quitting:
				return
			case <-quit:
				go notifyQuitting()
				return
			}
		}
	}()
	api, opts, err := fio.NewConnection(nil, url)
	if err != nil {
		log.Println(err)
		return
	}
	Abis, err = newAbiCache(api)
	if err != nil {
		log.Println(err)
		return
	}

	api.Header.Set("User-Agent", "fio-watch")
	knownAddresses, err = newAddressCache(api)
	if err != nil {
		log.Println(err)
		return
	}
	workers := runtime.NumCPU()/2 + 1
	if workers > 8 {
		workers = 8
	}
	tickTime := 500
	if p2pnode != "" {
		tickTime = 6000
	}
	fetchTick := time.NewTicker(time.Duration(tickTime) * time.Millisecond)
	fetchQueue := make(chan uint32)
	blockResult := make(chan *eos.BlockResp)
	//var pending bool

	var highestFetched uint32
	highestChan := make(chan uint32)
	go func() {
		for {
			select {
			case h := <-highestChan:
				if h > highestFetched {
					highestFetched = h
				}
			}
		}
	}()

	// processedBlocks is a map that ensures we don't double process anything,
	// it includes a mutex and the map needs to be occassionally truncated to prevent infinite growth
	seen := processedBlocks{
		done: make(map[uint32]bool),
		sent: make(map[uint32]bool),
	}
	go func() {
		for {
			time.Sleep(2 * time.Minute)
			if stopRequested {
				return
			}
			if seen.working {
				return
			}
			if len(seen.done) > 240 && !seen.working {
				seen.working = true
				seen.mux.RLock()
				// pause collection of highest block
				//pending = true
				newDone := make(map[uint32]bool)
				for v := range seen.done {
					if v > highestFetched-240 {
						newDone[v] = true
					}
				}
				seen.done = newDone
				newSent := make(map[uint32]bool)
				for v := range seen.sent {
					if v > highestFetched-240 {
						newSent[v] = true
					}
				}
				seen.mux.RUnlock()
				seen.mux.Lock()
				seen.sent = newSent
				seen.mux.Unlock()
				seen.working = false
			}
			if stopRequested {
				return
			}
		}
	}()

	// workers to fetch blocks, expect to need ability for multiple simultaneously
	wg := &sync.WaitGroup{}
	switch p2pnode {
	// impolite mode: hammer the API, no p2p info provided.
	case "":
		wg.Add(workers)
		for i := 0; i < workers; i++ {
			go getBlock(fetchQueue, quitting, blockResult, &seen, wg, api.BaseURL)
		}

		go func() {
			// try to recover if our workers die ....
			for {
				wg.Wait()
				time.Sleep(time.Second)
				if stopRequested {
					return
				}
				wg.Add(workers)
				for i := 0; i < workers; i++ {
					go getBlock(fetchQueue, quitting, blockResult, &seen, wg, api.BaseURL)
				}
			}
		}()
	// allow p2p node to send blocks:
	default:
		go func() {
			client := p2p.NewClient(
				p2p.NewOutgoingPeer(p2pnode, "fiowatch", &p2p.HandshakeInfo{
					ChainID:      opts.ChainID,
					HeadBlockNum: 1,
				}),
				false,
			)
			blockHandler := p2p.HandlerFunc(func(envelope *p2p.Envelope) {
				name, _ := envelope.Packet.Type.Name()
				switch name {
				case "SignedBlock":
					block := &eos.SignedBlock{}
					err := eos.UnmarshalBinary(envelope.Packet.Payload, block)
					if err != nil {
						fmt.Println(string(envelope.Packet.Payload))
						log.Println(err)
						return
					}
					id, _ := block.BlockID()
					_, prefix, _ := fio.GetRefBlockFor(block.BlockNumber(), id.String())
					head <- int(block.BlockNumber())
					blockResult <- &eos.BlockResp{
						SignedBlock:    *block,
						ID:             id,
						BlockNum:       block.BlockNumber(),
						RefBlockPrefix: prefix,
					}
					block = nil
				}
			})
			client.RegisterHandler(blockHandler)
			e := client.Start()
			if e != nil {
				log.Println(e)
				notifyQuitting()
				return
			}
		}()
	}

	resultAlive := time.Now()
	go func() {
		for {
			select {
			case <-quitting:
				return
			case incoming := <-blockResult:
				if stopRequested {
					return
				}
				seen.sentMux.RLock()
				dup := seen.sent[incoming.BlockNum]
				seen.sentMux.RUnlock()
				if dup {
					continue
				}
				seen.sentMux.Lock()
				seen.sent[incoming.BlockNum] = true
				seen.sentMux.Unlock()
				go func() {
					res, actions := blockToSummary(incoming, api)
					summary <- res
					if highestFetched-incoming.BlockNum > 30 && p2pnode == "" {
						slow <- true
						log.Println("activity monitor: more than 15s behind processing head block, cannot keep up.")
					} else {
						for _, a := range actions {
							if stopRequested {
								return
							}
							ref := &a
							cp := *ref
							details <- cp
							ref, a = nil, nil
						}
					}
					resultAlive = time.Now()
				}()
			}
		}
	}()
	go func() {
		for {
			time.Sleep(15 * time.Second)
			if stopRequested {
				return
			}
			if resultAlive.Before(time.Now().Add(-15 * time.Second)) {
				go notifyQuitting()
			}
		}
	}()

	if err != nil {
		go notifyQuitting()
		return
	}
	lastHeartBeat := time.Now()
	tickApi, _, _ := fio.NewConnection(nil, api.BaseURL)
	//tickApi.HttpClient.Timeout = time.Second
	tickApi.Header.Set("User-Agent", "fio-watch")
	for {
		select {
		case <-quitting:
			return
		case hb := <-heartBeat:
			if stopRequested {
				return
			}
			lastHeartBeat = hb
		case <-fetchTick.C:
			if stopRequested {
				return
			}
			if lastHeartBeat.Before(time.Now().Add(-90 * time.Second)) {
				log.Println("activity monitor: detected main window inactive, stopping workers")
				stopRequested = true
				go notifyQuitting()
				return
			}
			// don't block if can't do a get info, parent thread will attempt restart if stalled too long.
			innerWg := sync.WaitGroup{}
			waitCh := make(chan struct{})
			innerWg.Add(1)
			go func() {
				go func() {
					defer innerWg.Done()
					if stopRequested {
						return
					}
					if h, l, ok := getInfo(tickApi); ok {
						if h <= highestFetched {
							//pending = false
							return
						}
						head <- int(h)
						lib <- int(l)
						switch true {
						case h-highestFetched > 120 && p2pnode == "":
							// we just started, don't try to fetch more than needed
							highestFetched = h - 1
							fetchQueue <- h
						case h-highestFetched > 0 && p2pnode == "":
							for i := highestFetched; i <= h; i++ {
								if stopRequested {
									return
								}
								fetchQueue <- i
							}
						}
						highestChan <- h
					}
				}()
				innerWg.Wait()
				close(waitCh)
			}()
			select {
			case <-waitCh:
				if stopRequested {
					return
				}
				continue
			case <-time.After(5 * time.Second):
				if stopRequested {
					return
				}
				continue
			}
		}
	}
}

func getInfo(api *fio.API) (head uint32, lib uint32, ok bool) {
	info, err := api.GetInfo()
	if err != nil {
		log.Println(err)
		return 0, 0, false
	}
	return info.HeadBlockNum, info.LastIrreversibleBlockNum, true
}

//func getBlock(fetchQueue chan uint32, blockResult chan *eos.BlockResp, highest *uint32, wg sync.WaitGroup, api *fio.API) {
func getBlock(fetchQueue chan uint32, quit chan bool, blockResult chan *eos.BlockResp, seen *processedBlocks, wg *sync.WaitGroup, url string) {
	defer wg.Done()
	api, _, err := fio.NewConnection(nil, url)
	if err != nil {
		return
	}
	api.Header.Set("User-Agent", "fio-watch")
	//api.HttpClient.Timeout = time.Duration(4) * time.Second
	var stopRequested bool
	done := make(chan bool, 1)
	defer close(done)
	go func() {
		for {
			select {
			case <-done:
				return
			case <-quit:
				stopRequested = true
				return
			}
		}
	}()
	for {
		select {
		case block := <-fetchQueue:
			if stopRequested {
				return
			}
			seen.mux.RLock()
			if seen.done[block] {
				seen.mux.RUnlock()
				continue
			}
			seen.mux.RUnlock()
			seen.mux.Lock()
			seen.done[block] = true
			seen.mux.Unlock()
			if stopRequested {
				return
			}
			resp, err := api.GetBlockByNum(block)
			if err != nil {
				log.Println(err)
				time.Sleep(500 * time.Millisecond)
				if stopRequested {
					return
				}
				fetchQueue <- block
			}
			if resp != nil {
				if stopRequested {
					return
				}
				blockResult <- resp
			}
		}
	}
}

func blockToSummary(block *eos.BlockResp, api *fio.API) (*BlockSummary, []*ActionRow) {
	p := message.NewPrinter(language.AmericanEnglish)
	summary := BlockSummary{
		Actions: make(map[string]int),
		T:       block.Timestamp.Time,
		Y:       float64(len(block.Transactions)),
		Block:   int(block.BlockNum),
	}

	rows := make([]*ActionRow, 0)
	ar := make(chan *ActionRow)
	actionWg := sync.WaitGroup{}
	complete := make(chan bool, 1)
	sums := make(chan string)

	appendWg := sync.WaitGroup{}
	appendWg.Add(1)

	go func() {
		defer appendWg.Done()
		timeout := make(chan bool, 1)
		for {
			select {
			case s := <-sums:
				summary.Mux.Lock()
				summary.Actions[s] = summary.Actions[s] + 1
				summary.Mux.Unlock()
			case a := <-ar:
				rows = append(rows, a)
			case <-complete:
				go func() {
					time.Sleep(250 * time.Millisecond)
					close(timeout)
				}()
			case <-timeout:
				return
			}
		}
	}()

	for i := range block.Transactions {
		actionWg.Add(1)
		go func(i int) {
			defer actionWg.Done()
			tx := block.Transactions[i]
			if tx.Transaction.Packed == nil {
				summary.Actions["Unpack Error"] = summary.Actions["Unpack Error"] + 1
				return
			}
			utx, err := tx.Transaction.Packed.Unpack()
			if err != nil {
				summary.Actions["Unpack Error"] = summary.Actions["Unpack Error"] + 1
				return
			}

			for ai, action := range utx.Actions {
				txDetail := bytes.NewBuffer(nil)
				txid, _ := tx.Transaction.Packed.ID()
				txDetail.Write([]byte(fmt.Sprintf("Details for Action # %d of TXID %s in block %d\n\n", ai, hex.EncodeToString(txid), block.BlockNum)))
				sums <- string(action.Name)
				m, err := Abis.Decode(action.Account, action.Name, utx.Actions[ai].HexData)
				if err != nil {
					log.Println(err)
					continue
				}
				if m["content"] != nil {
					m["content"] = "... hidden ..."
				}
				j, err := json.MarshalIndent(m, "", "    ")
				if err == nil {
					txDetail.Write([]byte(fmt.Sprintf("Decoded Action Data For %s::%s\n\n", action.Account, action.Name)))
					txDetail.Write(j)
				}

				j, err = json.MarshalIndent(utx, "", "    ")
				if err == nil {
					txDetail.Write([]byte("\n\nUnpacked Transaction\n\n"))
					txDetail.Write(j)
				}
				var usedPrefix string
				if tx.TransactionReceiptHeader.CPUUsageMicroSeconds > 5000 || len(action.HexData) > 500 {
					usedPrefix = " ... "
				}

				fioName := knownAddresses.Get(string(action.Authorization[0].Actor))
				ar <- &ActionRow{
					Contract: string(action.Account),
					Action:   string(action.Name),
					Actor:    string(action.Authorization[0].Actor),
					Info:     fioName,
					TxDetail: txDetail.Bytes(),
					Time:     block.Timestamp.Time,
					TxId:     hex.EncodeToString(txid),
					BlockNum: block.BlockNum,
					Used:     p.Sprintf("%s%d bytes, %d µs%s", usedPrefix, len(action.HexData), tx.TransactionReceiptHeader.CPUUsageMicroSeconds, usedPrefix),
				}
			}
			utx = nil
		}(i)
	}
	actionWg.Wait()
	complete <- true
	appendWg.Wait()
	return &summary, rows
}

type processedBlocks struct {
	working bool
	mux     sync.RWMutex
	sentMux sync.RWMutex
	done    map[uint32]bool
	sent    map[uint32]bool
}

type addressCacheRow struct {
	address string
	time    time.Time
	exists  bool
}

type addressCache struct {
	sync.RWMutex
	cache map[string]addressCacheRow
	api   *fio.API
}

func newAddressCache(api *fio.API) (*addressCache, error) {
	return &addressCache{
		RWMutex: sync.RWMutex{},
		cache:   make(map[string]addressCacheRow),
		api:     api,
	}, nil

}

func (a *addressCache) Get(actor string) (result string) {
	a.RLock()
	if a.cache[actor].exists && a.cache[actor].time.Before(time.Now().Add(-2*time.Minute)) {
		result = a.cache[actor].address
	}
	a.RUnlock()
	if result != "" {
		return
	}
	a.Lock()
	defer a.Unlock()
	a.cache[actor] = addressCacheRow{
		time:   time.Now(),
		exists: true,
	}
	addrs, ok, _ := a.api.GetFioNamesForActor(actor)
	if !ok {
		return
	}
	if len(addrs.FioAddresses) > 0 {
		result = addrs.FioAddresses[0].FioAddress
	}
	return
}

type abiCache struct {
	sync.RWMutex
	abis map[eos.AccountName]*eos.ABI
	api  *fio.API
	Ready bool
}

func newAbiCache(api *fio.API) (*abiCache, error) {
	var err error
	a := &abiCache{}
	a.api = api
	a.abis, err = api.AllABIs()
	if err != nil {
		return nil, err
	}
	a.Ready = true
	return a, nil
}

func (a *abiCache) Decode(account eos.AccountName, action eos.ActionName, b []byte) (msi map[string]interface{}, err error) {
	if a.abis[account] == nil {
		gabi, err := a.api.GetABI(account)
		if err != nil {
			return nil, err
		}
		a.Lock()
		a.abis[gabi.AccountName] = &gabi.ABI
		a.Unlock()
	}
	j, err := a.abis[account].DecodeAction(b, action)
	if err != nil {
		return nil, err
	}
	msi = make(map[string]interface{})
	err = json.Unmarshal(j, &msi)
	if err != nil {
		return nil, err
	}
	if len(msi) == 0 {
		return nil, errors.New("abi decode resulted in empty struct")
	}
	return msi, nil
}
