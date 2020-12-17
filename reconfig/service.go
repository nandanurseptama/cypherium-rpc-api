package reconfig

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cypherium/cypherBFT/common"
	"github.com/cypherium/cypherBFT/core"
	"github.com/cypherium/cypherBFT/core/types"
	"github.com/cypherium/cypherBFT/crypto/bls"
	"github.com/cypherium/cypherBFT/event"
	"github.com/cypherium/cypherBFT/log"
	"github.com/cypherium/cypherBFT/onet"
	"github.com/cypherium/cypherBFT/onet/network"
	"github.com/cypherium/cypherBFT/params"
	"github.com/cypherium/cypherBFT/reconfig/bftview"
	"github.com/cypherium/cypherBFT/reconfig/hotstuff"
)

type committeeInfo struct {
	Committee *bftview.Committee
	KeyHash   common.Hash
	KeyNumber uint64
}
type bestCandidateInfo struct {
	Node      *common.Cnode
	KeyHash   common.Hash
	KeyNumber uint64
}
type cachedCommitteeInfo struct {
	keyHash   common.Hash
	keyNumber uint64
	committee *bftview.Committee
	node      *common.Cnode
}
type committeeMsg struct {
	sid   *network.ServerIdentity
	cinfo *committeeInfo
	best  *bestCandidateInfo
}
type hotstuffMsg struct {
	sid   *network.ServerIdentity
	lastN uint64
	hMsg  *hotstuff.HotstuffMessage
}
type networkMsg struct {
	ID   int64
	Hmsg *hotstuff.HotstuffMessage
	Cmsg *committeeInfo
	Bmsg *bestCandidateInfo
}
type networkMsgAck struct {
	ID int64
}
type lastAckMsg struct {
	si    *network.ServerIdentity
	ackTm time.Time
	msg   *networkMsg
}

//Service work for protcol
type Service struct {
	*onet.ServiceProcessor // We need to embed the ServiceProcessor, so that incoming messages are correctly handled.
	server                 *onet.Server
	serverID               string
	serverAddress          string

	bc         *core.BlockChain
	txService  *txService
	kbc        *core.KeyBlockChain
	keyService *keyService

	protocolMng *hotstuff.HotstuffProtocolManager

	lastCmInfoMap   map[common.Hash]*cachedCommitteeInfo
	muCommitteeInfo sync.Mutex
	currentView     bftview.View
	waittingView    bftview.View
	lastReqCmNumber uint64
	muCurrentView   sync.Mutex

	replicaView     *bftview.View
	runningState    int32
	lastProposeTime time.Time
	pacetMakerTimer *paceMakerTimer

	netErrMap    map[string]time.Time
	muNetErr     sync.Mutex
	netLastMsg   map[string]*lastAckMsg
	muNetLastMsg sync.Mutex

	hotstuffMsgs CDataQueue
	/*
		feed   event.Feed
		msgCh  chan hotstuffMsg
		msgSub event.Subscription // Subscription for msg event
	*/
	feed1   event.Feed
	msgCh1  chan committeeMsg
	msgSub1 event.Subscription // Subscription for msg event
	/*
		blockCh     chan *types.Block
		muInserting sync.Mutex
		isInserting bool
	*/
	goPool *GoPool
}

func newService(sName string, conf *Reconfig) *Service {
	onet.RegisterNewService(sName, registerService)
	server := onet.NewKcpServer(conf.cph.ExtIP().String() + ":" + conf.config.OnetPort)

	s := server.Service(sName).(*Service)
	s.txService = newTxService(s, conf.cph, conf.config)
	s.keyService = newKeyService(s, conf.cph, conf.config)

	s.server = server
	s.bc = conf.cph.BlockChain()
	s.kbc = conf.cph.KeyBlockChain()
	s.lastCmInfoMap = make(map[common.Hash]*cachedCommitteeInfo)
	s.netErrMap = make(map[string]time.Time)
	s.netLastMsg = make(map[string]*lastAckMsg)

	//	s.msgCh = make(chan hotstuffMsg, 1024)
	//	s.msgSub = s.feed.Subscribe(s.msgCh)
	s.msgCh1 = make(chan committeeMsg, 10)
	s.msgSub1 = s.feed1.Subscribe(s.msgCh1)

	//	s.blockCh = make(chan *types.Block, 5)
	s.goPool = NewGoPool(params.MaxGoRoutines, false).Start()
	s.protocolMng = hotstuff.NewHotstuffProtocolManager(s, nil, nil, params.PaceMakerTimeout*2)

	go s.handleHotStuffMsg()
	go s.handleCommitteeMsg()
	go s.ackStatusMonitor()
	return s
}

// register service for networkMsg and networkMsgAck
func registerService(c *onet.Context) (onet.Service, error) {
	s := &Service{ServiceProcessor: onet.NewServiceProcessor(c)}
	s.RegisterProcessorFunc(network.RegisterMessage(&networkMsg{}), s.handleNetworkMsgReq)
	s.RegisterProcessorFunc(network.RegisterMessage(&networkMsgAck{}), s.handleNetworkMsgAckReq)

	//s.RegisterProcessorFunc(network.RegisterMessage(&hotstuff.HotstuffMessage{}), s.handleHotStuffReq)
	//s.RegisterProcessorFunc(network.RegisterMessage(&committeeInfo{}), s.handleCommitteeInfoReq)
	//s.RegisterProcessorFunc(network.RegisterMessage(&bestCandidateInfo{}), s.handleBestCandidateInfoReq)
	return s, nil
}

// When the 2F + 1 vote is collected, the onnewview event is generated
func (s *Service) OnNewView(data []byte, extraes [][]byte) error { //buf is snapshot, //verify repla' block before newview
	view := bftview.DecodeToView(data)
	log.Info("OnNewView..", "txNumber", view.TxNumber, "keyNumber", view.KeyNumber)

	s.muCurrentView.Lock()
	s.replicaView = view
	if view.EqualNoIndex(&s.currentView) {
		s.currentView.LeaderIndex = view.LeaderIndex
		s.currentView.ReconfigType = view.ReconfigType
	}
	s.muCurrentView.Unlock()

	var bestCandidates []*types.Candidate
	for _, extraD := range extraes {
		if extraD == nil {
			continue
		}
		cand := types.DecodeToCandidate(extraD)
		if cand == nil {
			continue
		}
		bestCandidates = append(bestCandidates, cand)
	}
	s.keyService.setBestCandidateAndBadAddress(bestCandidates, nil)

	return nil
}

// CurrentState call by hotstuff
func (s *Service) CurrentState() ([]byte, string) { //recv by onnewview
	curView := s.getCurrentView()
	leaderID := ""
	mb := bftview.GetCurrentMember()
	if mb != nil {
		leader := mb.List[curView.LeaderIndex]
		//leader := mb.List[0]
		log.Info("CurrentState.NextLeader", "index", curView.LeaderIndex, "ip", leader.Address)
		leaderID = bftview.GetNodeID(leader.Address, leader.Public)
	} else {
		log.Error("CurrentState.NextLeader: can't get current committee!")
		s.Committee_Request(curView.KeyNumber, curView.KeyHash)
	}

	log.Info("CurrentState", "TxNumber", curView.TxNumber, "KeyNumber", curView.KeyNumber, "LeaderIndex", curView.LeaderIndex, "ReconfigType", curView.ReconfigType)

	return curView.EncodeToBytes(), leaderID
}

// GetExtra data of best candidate call by hotstuff
func (s *Service) GetExtra() []byte {
	best := s.keyService.getBestCandidate(true)
	if best == nil {
		return nil
	}
	return best.EncodeToBytes()
}

// GetPublicKey of all current member, call by hotstuff
func (s *Service) GetPublicKey() []*bls.PublicKey {
	keyblock := s.kbc.CurrentBlock()
	keyNumber := keyblock.NumberU64()
	c := bftview.LoadMember(keyNumber, keyblock.Hash(), false)
	if c == nil {
		return nil
	}
	return c.ToBlsPublicKeys(keyblock.Hash())
}

// Self, call by hotstuff
func (s *Service) Self() string {
	return s.serverID
}

// Check for old data, call by hotstuff
func (s *Service) CheckView(data []byte) error {
	if !s.isRunning() {
		return types.ErrNotRunning
	}
	view := bftview.DecodeToView(data)
	knumber := s.kbc.CurrentBlock().NumberU64()
	txnumber := s.bc.CurrentBlock().NumberU64()
	log.Debug("CheckView..", "txNumber", view.TxNumber, "keyNumber", view.KeyNumber, "local key number", knumber, "tx number", txnumber)
	if view.KeyNumber < knumber {
		return hotstuff.ErrOldState
	} else if view.KeyNumber > knumber {
		return hotstuff.ErrFutureState
	}
	if view.TxNumber < txnumber {
		return hotstuff.ErrOldState
	} else if view.TxNumber > txnumber {
		return hotstuff.ErrFutureState
	}

	return nil
}

// Propose verify, call by hotstuff
func (s *Service) OnPropose(kState []byte, tState []byte, extra []byte) error { //verify new block
	log.Debug("OnPropose..")
	if !s.isRunning() {
		return types.ErrNotRunning
	}

	var err error
	var block *types.Block
	var kblock *types.KeyBlock
	if kState != nil {
		kblock = types.DecodeToKeyBlock(kState)
		log.Info("OnPropose", "keyNumber", kblock.NumberU64())
	}
	if tState != nil {
		block = types.DecodeToBlock(tState)
		log.Info("OnPropose", "txNumber", block.NumberU64())
	}
	if kblock != nil {
		err = s.keyService.verifyKeyBlock(kblock, types.DecodeToCandidate(extra), nil)
		if err != nil {
			log.Error("verify keyblock", "number", kblock.NumberU64(), "err", err)
			return err
		}
	}
	if block != nil {
		err = s.txService.verifyTxBlock(block)
		if err != nil {
			log.Error("verify txblock", "number", block.NumberU64(), "err", err)
			return err
		}
	}
	s.pacetMakerTimer.start()
	return nil
}

// Propose block including txs, call by hotstuff
func (s *Service) Propose() (e error, kState []byte, tState []byte, extra []byte) { //buf recv by onpropose, onviewdown
	log.Debug("Propose..")

	proposeOK := false
	defer func() {
		if !proposeOK {
			go func() {
				time.Sleep(2 * time.Second)
				curView := s.getCurrentView()
				if bftview.IamLeader(curView.LeaderIndex) {
					s.addHotstuffMsg(&hotstuffMsg{sid: nil, lastN: s.bc.CurrentBlock().NumberU64(), hMsg: s.protocolMng.TryProposeMessage()})
					//s.feed.Send(hotstuffMsg{sid: nil, lastN: s.bc.CurrentBlock().NumberU64(), hMsg: s.protocolMng.TryProposeMessage()})
				}
			}()
		} else {
			s.lastProposeTime = time.Now()
		}
	}()

	if !s.isRunning() {
		err := fmt.Errorf("not running for propose")
		return err, nil, nil, nil
	}

	s.muCurrentView.Lock()
	leaderIndex := s.currentView.LeaderIndex
	reconfigType := s.currentView.ReconfigType
	if !s.replicaView.EqualAll(&s.currentView) {
		log.Error("Propose", "replica view not equal to local current view txNumber", s.currentView.TxNumber, "keyNumber", s.currentView.KeyNumber, "LeaderIndex", leaderIndex, "ReconfigType",
			reconfigType, "replica txNumber", s.replicaView.TxNumber, "keyNumber", s.replicaView.KeyNumber, "LeaderIndex", s.replicaView.LeaderIndex, "ReconfigType", s.replicaView.ReconfigType)
		s.muCurrentView.Unlock()
		return fmt.Errorf("replica view not equal to local current view"), nil, nil, nil
	}
	if !bftview.IamLeader(leaderIndex) {
		proposeOK = true
		err := fmt.Errorf("not leader for propose")
		log.Error("Propose", "error", err)
		s.muCurrentView.Unlock()
		return err, nil, nil, nil
	}
	s.muCurrentView.Unlock()

	if reconfigType > 0 {
		keyblock, mb, bestCandi, _, err := s.keyService.tryProposalChangeCommittee(s.bc.CurrentBlock(), reconfigType, leaderIndex)
		if err == nil && keyblock != nil && mb != nil {
			kbuf := keyblock.EncodeToBytes()
			if bestCandi != nil {
				extra = bestCandi.EncodeToBytes()
			}

			tbuf, err := s.txService.tryProposalNewBlock(types.IsKeyBlockType)
			if err != nil {
				log.Error("Propose keyblock+txblock", "error", err)
			} else {
				proposeOK = true
				return nil, kbuf, tbuf, extra
			}
		}
		log.Warn("tryProposalChangeCommittee error and tryProposalNewBlock", "error", err)
		data, err := s.txService.tryProposalNewBlock(types.IsKeyBlockSkipType)
		if err != nil {
			log.Error("tryProposalNewBlock.1", "error", err)
			return err, nil, nil, nil
		}
		proposeOK = true
		return nil, nil, data, nil
	}
	data, err := s.txService.tryProposalNewBlock(types.IsTxBlockType)
	if err != nil {
		log.Error("tryProposalNewBlock", "error", err)
		return err, nil, nil, nil
	}
	proposeOK = true
	return nil, nil, data, nil
}

// Protocol done,call by hotstuff
func (s *Service) OnViewDone(e error, phase uint64, kSign *hotstuff.SignedState, tSign *hotstuff.SignedState) error {
	log.Info("OnViewDone", "phase", phase)
	if !s.isRunning() {
		return types.ErrNotRunning
	}
	if e != nil {
		log.Info("OnViewDone", "error", e)
		return e
	}
	if tSign != nil {
		block := types.DecodeToBlock(tSign.State)
		err := s.txService.decideNewBlock(block, tSign.Sign, tSign.Mask)
		if err != nil {
			return err
		}
	}
	if kSign != nil {
		block := types.DecodeToKeyBlock(kSign.State)
		err := s.keyService.decideNewKeyBlock(block, kSign.Sign, kSign.Mask)
		if err != nil {
			return err
		}
	}

	return nil
}

// Send data, call by hotstuff------------------------------------------------------------------------------------------------
func (s *Service) Write(id string, data *hotstuff.HotstuffMessage) error {
	if data.Code != hotstuff.MsgCollectTimeoutView {
		log.Info("Write", "to id", id, "code", hotstuff.ReadableMsgType(data.Code), "ViewId", data.ViewId)
	}

	if id == s.Self() {
		s.addHotstuffMsg(&hotstuffMsg{sid: nil, hMsg: data})
		//s.feed.Send(hotstuffMsg{sid: nil, hMsg: data})
		return nil
	}

	mb := bftview.GetCurrentMember()
	if mb == nil {
		return fmt.Errorf("can't find current committee,id %s", id)
	}
	node, _ := mb.Get(id, bftview.ID)
	if node == nil || len(node.Address) < 7 { //1.1.1.1
		err := fmt.Errorf("can't find id %s in current committee", id)
		log.Error("Couldn't send", "err", err)
		return err
	}

	s.goPool.AddFunc(s.SendRawData1, network.NodeToSid(node.Address, node.Public), &networkMsg{Hmsg: data}, true)
	//go s.SendRawData(network.NodeToSid(node.Address, node.Public), &networkMsg{Hmsg: data}, true)
	return nil
}

// Broadcast data, call by hotstuff
func (s *Service) Broadcast(data *hotstuff.HotstuffMessage) []error {
	log.Debug("Broadcast", "code", hotstuff.ReadableMsgType(data.Code), "ViewId", data.ViewId)
	//var arr []error
	mb := bftview.GetCurrentMember()
	if mb == nil {
		return []error{fmt.Errorf("can't find current committee")}
	}
	for _, node := range mb.List {
		if node.Address == s.serverAddress {
			s.addHotstuffMsg(&hotstuffMsg{sid: nil, hMsg: data})
			//s.feed.Send(hotstuffMsg{sid: nil, hMsg: data})
			continue
		}
		//go s.SendRawData(network.NodeToSid(node.Address, node.Public), &networkMsg{Hmsg: data}, true)
		s.goPool.AddFunc(s.SendRawData1, network.NodeToSid(node.Address, node.Public), &networkMsg{Hmsg: data}, true)

		//arr = append(arr, err)
	}
	return nil //return arr
}

func (s *Service) addHotstuffMsg(msg *hotstuffMsg) {
	s.hotstuffMsgs.pushBack(msg)
}

// networkMsg recived call back
func (s *Service) handleNetworkMsgReq(env *network.Envelope) {
	msg, ok := env.Msg.(*networkMsg)
	if !ok {
		log.Error("handleNetworkMsgReq failed to cast to ")
		return
	}
	si := env.ServerIdentity
	address := si.Address.String()
	//	log.Info("handleNetworkMsgReq Recv", "from address", address )
	s.muNetLastMsg.Lock()
	r := s.netLastMsg[address]
	if r != nil {
		r.ackTm = time.Now()
	}
	s.muNetLastMsg.Unlock()

	if msg.Hmsg != nil {
		s.addHotstuffMsg(&hotstuffMsg{sid: si, hMsg: msg.Hmsg})
		return
	}
	s.feed1.Send(committeeMsg{sid: si, cinfo: msg.Cmsg, best: msg.Bmsg})
	s.goPool.AddFunc(s.SendRaw1, env.ServerIdentity, &networkMsgAck{ID: msg.ID}, false)
	//go s.SendRaw(env.ServerIdentity, &networkMsgAck{ID: msg.ID}, false)

}

// Heartbeat in current member
func (s *Service) sendHeartBeatMsg() {
	if bftview.IamMember() < 0 {
		return
	}
	mb := bftview.GetCurrentMember()
	for _, node := range mb.List {
		if node.Address != s.serverAddress {
			s.muNetLastMsg.Lock()
			r := s.netLastMsg[node.Address]
			s.muNetLastMsg.Unlock()
			if r != nil {
				if time.Now().Sub(r.ackTm) < params.PaceMakerHeatTimeout {
					continue
				}
			}
			log.Debug("sendHeartBeatMsg", "tm", time.Now())
			s.goPool.AddFunc(s.SendRaw1, network.NodeToSid(node.Address, node.Public), &networkMsgAck{ID: 0}, false)
			//go s.SendRaw(network.NodeToSid(node.Address, node.Public), &networkMsgAck{ID: 0}, false)
		}
	}
}

// networkMsgAck recived call back
func (s *Service) handleNetworkMsgAckReq(env *network.Envelope) {
	_, ok := env.Msg.(*networkMsgAck)
	if !ok {
		log.Error("handleNetworkMsgAckReq failed to cast to ")
		return
	}
	si := env.ServerIdentity
	address := si.Address.String()
	log.Debug("handleNetworkMsgAckReq Recv", "from address", address)
	s.muNetLastMsg.Lock()
	r := s.netLastMsg[address]
	if r != nil {
		r.ackTm = time.Now()
	}
	s.muNetLastMsg.Unlock()
}

// handle all hostuff message in queue of hotstuffMsgs
func (s *Service) handleHotStuffMsg() {
	for {
		data := s.hotstuffMsgs.popFront()
		if data == nil {
			time.Sleep(5 * time.Millisecond)
			continue
		}
		msg := data.(*hotstuffMsg)
		msgCode := msg.hMsg.Code
		if msgCode != hotstuff.MsgCollectTimeoutView {
			log.Debug("handleHotStuffMsg", "id", msg.hMsg.Id, "code", hotstuff.ReadableMsgType(msgCode), "ViewId", msg.hMsg.ViewId)
		}
		var curN uint64
		if msgCode == hotstuff.MsgTryPropose || msgCode == hotstuff.MsgStartNewView {
			curN = s.bc.CurrentBlock().NumberU64()
			if msg.lastN < curN {
				log.Debug("handleHotStuffMsg", "code", hotstuff.ReadableMsgType(msgCode), "lastN", msg.lastN, "curN", curN)
				continue
			}
		} else if msgCode == hotstuff.MsgPrepare {
			curBlock := s.kbc.CurrentBlock()
			keyNumber := curBlock.NumberU64()
			keyHash := curBlock.Hash()
			if bftview.LoadMember(keyNumber, keyHash, true) == nil && msg.sid.Address.String() != s.serverAddress {
				log.Debug("request committee", "keynumber", keyNumber, "send to address", msg.sid.Address)
				s.goPool.AddFunc(s.SendRawData1, msg.sid, &networkMsg{Cmsg: &committeeInfo{Committee: nil, KeyHash: keyHash, KeyNumber: keyNumber}}, true)
				//go s.SendRawData(msg.sid, &networkMsg{Cmsg: &committeeInfo{Committee: nil, KeyHash: keyHash, KeyNumber: keyNumber}}, true)
			}
		}

		err := s.protocolMng.HandleMessage(msg.hMsg)
		if err != nil && msgCode == hotstuff.MsgStartNewView {
			go func(curN uint64) {
				time.Sleep(2 * time.Second)
				s.sendNewViewMsg(curN)
			}(curN)
		}
	}
}

// sync committee in current member
func (s *Service) syncCommittee(mb *bftview.Committee, keyblock *types.KeyBlock) {
	if !keyblock.HasNewNode() {
		return
	}

	in := mb.In()
	s.goPool.AddFunc(s.SendRawData1, network.NodeToSid(in.Address, in.Public), &networkMsg{Cmsg: &committeeInfo{Committee: mb, KeyHash: keyblock.Hash(), KeyNumber: keyblock.NumberU64()}}, true)
	//go s.SendRawData(network.NodeToSid(in.Address, in.Public), &networkMsg{Cmsg: &committeeInfo{Committee: mb, KeyHash: keyblock.Hash(), KeyNumber: keyblock.NumberU64()}}, true)

	msg := &bestCandidateInfo{Node: in, KeyHash: keyblock.Hash(), KeyNumber: keyblock.NumberU64()}
	for i, r := range mb.List {
		if i == 0 {
			continue
		}
		if r.Address == s.serverAddress {
			continue
		}
		log.Debug("syncBestCandidate", "send to", r.Address)
		s.goPool.AddFunc(s.SendRawData1, network.NodeToSid(r.Address, r.Public), &networkMsg{Bmsg: msg}, true)
		//go s.SendRawData(network.NodeToSid(r.Address, r.Public), &networkMsg{Bmsg: msg}, true)
	}
}

// Store committee in cache
func (s *Service) storeCommitteeInCache(cmInfo *committeeInfo, best *bestCandidateInfo) {
	s.muCommitteeInfo.Lock()
	defer s.muCommitteeInfo.Unlock()
	var (
		keyHash   common.Hash
		keyNumber uint64
		committee *bftview.Committee
		node      *common.Cnode
	)
	if cmInfo != nil {
		keyHash = cmInfo.KeyHash
		keyNumber = cmInfo.KeyNumber
		committee = cmInfo.Committee
	} else if best != nil {
		keyHash = best.KeyHash
		keyNumber = best.KeyNumber
		node = best.Node
	}

	ac, ok := s.lastCmInfoMap[keyHash]
	if ok {
		if cmInfo != nil {
			ac.committee = cmInfo.Committee
		}
		if best != nil {
			ac.node = best.Node
		}
		return
	}
	//clear prev map
	maxNumber := s.kbc.CurrentBlock().NumberU64()
	for hash, ac := range s.lastCmInfoMap {
		if ac.keyNumber < maxNumber-9 {
			delete(s.lastCmInfoMap, hash)
		}
	}
	log.Info("@@storeCommitteeInCache", "key number", keyNumber)

	s.lastCmInfoMap[keyHash] = &cachedCommitteeInfo{keyHash: keyHash, keyNumber: keyNumber, committee: committee, node: node}
}

// handle committee sync message
func (s *Service) handleCommitteeMsg() {
	for {
		select {
		case msg := <-s.msgCh1:
			if msg.best != nil {
				if bftview.LoadMember(msg.best.KeyNumber, msg.best.KeyHash, true) != nil {
					continue
				}
				log.Info("bestCandidate", "best KeyNumber", msg.best.KeyNumber)
				s.storeCommitteeInCache(nil, msg.best)
				continue
			}
			cInfo := msg.cinfo
			if cInfo == nil {
				continue
			}
			if cInfo.Committee == nil {
				mb := bftview.LoadMember(cInfo.KeyNumber, cInfo.KeyHash, true)
				if mb == nil {
					continue
				}
				log.Debug("committeeInfo answer", "number", cInfo.KeyNumber, "adddress", msg.sid.Address)
				r, _ := mb.Get(msg.sid.Address.String(), bftview.Address)
				if r != nil {
					log.Debug("committeeInfo answer..ok", "number", cInfo.KeyNumber)
					s.goPool.AddFunc(s.SendRawData1, msg.sid, &networkMsg{Cmsg: &committeeInfo{Committee: mb, KeyHash: cInfo.KeyHash, KeyNumber: cInfo.KeyNumber}}, true)
					//go s.SendRawData(msg.sid, &networkMsg{Cmsg: &committeeInfo{Committee: mb, KeyHash: cInfo.KeyHash, KeyNumber: cInfo.KeyNumber}}, true)
				}
				continue
			}

			if bftview.LoadMember(cInfo.KeyNumber, cInfo.KeyHash, true) != nil {
				continue
			}
			log.Debug("committeeInfo", "number", cInfo.KeyNumber, "adddress", msg.sid.Address)
			keyblock := s.kbc.GetBlock(cInfo.KeyHash, cInfo.KeyNumber)
			if keyblock != nil {
				cInfo.Committee.Store(keyblock)
			} else {
				s.storeCommitteeInCache(cInfo, nil)
			}

		case <-s.msgSub1.Err():
			log.Error("handleHotStuffMsg Feed error")
			return
		}
	}
}

// Update committee by keyblock
func (s *Service) updateCommittee(keyBlock *types.KeyBlock) bool {
	bStore := false
	curKeyBlock := keyBlock
	if curKeyBlock == nil {
		curKeyBlock = s.kbc.CurrentBlock()
	}
	mb := bftview.LoadMember(curKeyBlock.NumberU64(), curKeyBlock.Hash(), true)
	if mb != nil {
		return bStore
	}

	s.muCommitteeInfo.Lock()
	ac, ok := s.lastCmInfoMap[curKeyBlock.Hash()]
	if ok {
		if ac.committee != nil {
			mb = ac.committee
		} else if ac.node != nil {
			mb, _ = bftview.GetCommittee(ac.node, curKeyBlock, true)
		}
	}
	s.muCommitteeInfo.Unlock()

	if mb == nil && !curKeyBlock.HasNewNode() {
		mb, _ = bftview.GetCommittee(nil, curKeyBlock, true)
	}

	if mb != nil {
		bStore = mb.Store(curKeyBlock)
	} else {
		log.Info("updateCommittee can't found committee", "txNumber", s.bc.CurrentBlock().NumberU64(), "keyNumber", curKeyBlock.NumberU64())
	}
	return bStore
}

func (s *Service) Committee_OnStored(keyblock *types.KeyBlock, mb *bftview.Committee) {
	log.Debug("store committee", "keyNumber", keyblock.NumberU64(), "ip0", mb.List[0].Address, "ipn", mb.List[len(mb.List)-1].Address)
	//if keyblock.HasNewNode() && keyblock.NumberU64() == s.kbc.CurrentBlock().NumberU64() {
	//	s.server.AdjustConnect( mb.List )
	//}
}

// Request committee for keyblock
func (s *Service) Committee_Request(kNumber uint64, hash common.Hash) {
	if kNumber <= s.lastReqCmNumber || !bftview.IamMemberByNumber(kNumber, hash) {
		return
	}

	log.Debug("Committee_Request", "keynumber", kNumber)

	var parentMb *bftview.Committee
	for i := 1; i < 10; i++ {
		keyblock := s.kbc.GetBlockByNumber(kNumber - uint64(i))
		if keyblock == nil {
			return
		}
		mb := bftview.LoadMember(keyblock.NumberU64(), keyblock.Hash(), true)
		if mb != nil {
			parentMb = mb
			break
		}
	}
	if parentMb == nil {
		return
	}
	for _, node := range parentMb.List {
		if node.Address != s.serverAddress {
			s.goPool.AddFunc(s.SendRawData1, network.NodeToSid(node.Address, node.Public), &networkMsg{Cmsg: &committeeInfo{Committee: nil, KeyHash: hash, KeyNumber: kNumber}}, false)
			//go s.SendRawData(network.NodeToSid(node.Address, node.Public), &networkMsg{Cmsg: &committeeInfo{Committee: nil, KeyHash: hash, KeyNumber: kNumber}}, false)
		}
	}
	s.lastReqCmNumber = kNumber
}

// Update current view data
func (s *Service) updateCurrentView(fromKeyBlock bool) { //call by keyblock done
	s.muCurrentView.Lock()
	defer s.muCurrentView.Unlock()

	curBlock := s.bc.CurrentBlock()
	curKeyBlock := s.kbc.CurrentBlock()

	s.currentView.TxNumber = curBlock.NumberU64()
	s.currentView.TxHash = curBlock.Hash()
	s.currentView.KeyNumber = curKeyBlock.NumberU64()
	s.currentView.KeyHash = curKeyBlock.Hash()
	s.currentView.CommitteeHash = curKeyBlock.CommitteeHash()

	if fromKeyBlock || curBlock.NumberU64() >= curKeyBlock.T_Number() {
		s.currentView.LeaderIndex = 0
		s.currentView.ReconfigType = 0
	}
	log.Trace("updateCurrentView", "TxNumber", s.currentView.TxNumber, "KeyNumber", s.currentView.KeyNumber, "LeaderIndex", s.currentView.LeaderIndex, "ReconfigType", s.currentView.ReconfigType)
	if (s.currentView.TxNumber >= s.waittingView.TxNumber && s.currentView.KeyNumber >= s.waittingView.KeyNumber) || curBlock.BlockType() == types.IsKeyBlockSkipType {
		s.sendNewViewMsg(s.currentView.TxNumber)
		s.waittingView.KeyNumber = s.currentView.KeyNumber
		s.waittingView.TxNumber = s.currentView.TxNumber
	}
}

func (s *Service) getCurrentView() *bftview.View {
	s.muCurrentView.Lock()
	defer s.muCurrentView.Unlock()
	v := &s.currentView
	return v
}

func (s *Service) getBestCandidate(refresh bool) *types.Candidate {
	return s.keyService.getBestCandidate(refresh)
}

// Send new view when new block done
func (s *Service) sendNewViewMsg(curN uint64) {
	if bftview.IamMember() >= 0 && curN >= s.kbc.CurrentBlock().T_Number() {
		s.addHotstuffMsg(&hotstuffMsg{sid: nil, lastN: curN, hMsg: s.protocolMng.NewViewMessage()})
		//s.feed.Send(hotstuffMsg{sid: nil, lastN: curN, hMsg: s.protocolMng.NewViewMessage()})
	}
}

// Set next leader by prescribed rules
func (s *Service) setNextLeader(reconfigType uint8) {
	s.muCurrentView.Lock()
	defer s.muCurrentView.Unlock()

	if reconfigType == types.PowReconfig {
		s.currentView.LeaderIndex = s.kbc.GetNextLeaderIndex(0, nil)
	} else {
		s.currentView.LeaderIndex = s.kbc.GetNextLeaderIndex(s.currentView.LeaderIndex, nil)
	}
	s.currentView.ReconfigType = reconfigType
	log.Info("setNextLeader", "type", reconfigType, "index", s.currentView.LeaderIndex)

	s.waittingView.TxNumber = s.currentView.TxNumber + 1
	s.waittingView.KeyNumber = s.currentView.KeyNumber + 1
}

// Ack status monitor for the node's active status
func (s *Service) ackStatusMonitor() {
	emptyTm := time.Time{}
	timeOut := network.ReadTimeout
	for {
		time.Sleep(100 * time.Millisecond)
		tmNow := time.Now()
		s.muNetLastMsg.Lock()
		for id, r := range s.netLastMsg {
			if !r.ackTm.Equal(emptyTm) && tmNow.Sub(r.ackTm) > timeOut {
				if r.msg != nil {
					log.Warn("ackStatusMonitor force connect", "address", id)
					s.goPool.AddFunc(s.SendRaw1, r.si, r.msg, true)
					//go s.SendRaw(r.si, r.msg, true)
				}
				delete(s.netLastMsg, id)
			}
		}
		s.muNetLastMsg.Unlock()
	}
}

// Send data
func (s *Service) SendRawData1(ps ...interface{}) {
	si := ps[0].(*network.ServerIdentity)
	id := si.Address.String()
	msg := ps[1].(*networkMsg)
	//shouldReSend := ps[2].(bool)
	log.Debug("SendRawData1", "address", id)

	s.muNetErr.Lock()
	tm, ok := s.netErrMap[id]
	s.muNetErr.Unlock()

	if ok {
		if time.Now().Sub(tm) < params.SendErrReTryTime {
			log.Debug("SendRawData1 SendErrReTryTime err", "address", id)
			return
		}
		s.muNetErr.Lock()
		delete(s.netErrMap, id)
		s.muNetErr.Unlock()
	}
	msg.ID = time.Now().UnixNano()

	s.muNetLastMsg.Lock()
	r, ok := s.netLastMsg[id]
	if ok {
		r.msg = msg
		r.si = si
	} else {
		s.netLastMsg[id] = &lastAckMsg{si: si, msg: msg}
	}
	s.muNetLastMsg.Unlock()

	err := s.SendRaw(si, msg, false)
	if err != nil {
		log.Warn("SendRawData", "couldn't send to", si.Address, "msg ID", msg.ID, "error", err)
		s.muNetErr.Lock()
		s.netErrMap[si.String()] = time.Now()
		s.muNetErr.Unlock()
	}
}

// New block done
func (s *Service) procBlockDone(txBlock *types.Block, keyblock *types.KeyBlock) {
	if keyblock == nil {
		keyblock = s.kbc.CurrentBlock()
	}
	s.pacetMakerTimer.procBlockDone(txBlock, keyblock)
}

// call by miner.start
func (s *Service) start(config *common.NodeConfig) {
	if !s.isRunning() {
		s.serverAddress = config.Ip + ":" + config.Port
		s.serverID = bftview.GetNodeID(s.serverAddress, config.Public)
		s.protocolMng.UpdateKeyPair(bftview.StrToBlsPrivKey(config.Private))
		bftview.SetServerInfo(s.serverAddress, config.Public)
		s.server.Start()
		if bftview.IamMember() >= 0 {
			s.updateCommittee(nil)
			s.pacetMakerTimer.start()
		}
		s.updateCurrentView(false)
		s.setRunState(1)
	}
}

// call by miner stop
func (s *Service) stop() {
	if s.isRunning() {
		//s.server.Close()
		s.pacetMakerTimer.stop()
		s.setRunState(0)
		s.netErrMap = make(map[string]time.Time) //clear map
	}
}

func (s *Service) isRunning() bool {
	return atomic.LoadInt32(&s.runningState) == 1
}
func (s *Service) setRunState(state int32) {
	atomic.StoreInt32(&s.runningState, state)
}
