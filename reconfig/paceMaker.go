package reconfig

import (
	"runtime"
	//	"runtime"
	"sync"
	"time"

	"github.com/cypherium/cypherBFT/core"
	"github.com/cypherium/cypherBFT/core/types"
	"github.com/cypherium/cypherBFT/log"
	"github.com/cypherium/cypherBFT/params"
	"github.com/cypherium/cypherBFT/reconfig/bftview"
)

var maxPaceMakerTime time.Time

type paceMakerTimer struct {
	sync.Mutex
	startTime     time.Time
	waitTime      time.Duration
	beStop        bool
	beClose       bool
	service       serviceI
	txPool        *core.TxPool
	candidatepool *core.CandidatePool
	retryNumber   int
	config        *params.ChainConfig
	kbc           *core.KeyBlockChain
}

func newPaceMakerTimer(config *params.ChainConfig, s serviceI, cph Backend) (vTimer *paceMakerTimer) {
	maxPaceMakerTime = time.Now().AddDate(100, 0, 0) //100 years
	vt := &paceMakerTimer{
		service:       s,
		txPool:        cph.TxPool(),
		candidatepool: cph.CandidatePool(),
		startTime:     maxPaceMakerTime,
		waitTime:      params.PaceMakerTimeout,
		beStop:        true,
		beClose:       false,
		config:        config,
	}

	return vt

}

// Start for time counting of pacemake
func (t *paceMakerTimer) start() error {
	t.Lock()
	defer t.Unlock()
	if t.beStop { //first
		if t.txPool.PendingCount() > 0 {
			t.startTime = time.Now()
		}
	} else {
		t.startTime = time.Now()
	}
	//log.Info("paceMakerTimer.start", "startTime", t.startTime )

	t.beStop = false

	return nil
}

// Stop for time counting of pacemake
func (t *paceMakerTimer) stop() error {
	t.Lock()
	defer t.Unlock()
	t.beStop = true
	t.retryNumber = 0
	t.startTime = maxPaceMakerTime
	return nil
}

// Close pacemake loop
func (t *paceMakerTimer) close() {
	t.Lock()
	defer t.Unlock()
	t.beClose = true
}

func (t *paceMakerTimer) get() (time.Time, bool, bool, int) {
	t.Lock()
	defer t.Unlock()
	return t.startTime, t.beStop, t.beClose, t.retryNumber

}

// Loop for status action
func (t *paceMakerTimer) loopTimer() {
	lastHeartBeatTm := time.Now()
	for {
		time.Sleep(50 * time.Millisecond)
		startTime, beStop, beClose, retryNumber := t.get()
		if beClose {
			return
		}

		diff := time.Now().Sub(lastHeartBeatTm)
		if diff > params.PaceMakerHeatTimeout {
			t.service.sendHeartBeatMsg()
			lastHeartBeatTm = time.Now()
		}

		if beStop {
			continue
		}

		diff = time.Now().Sub(startTime)
		if diff > t.waitTime /**time.Duration(retryNumber+1)*/ && bftview.IamMember() >= 0 { //timeout
			log.Warn("Viewchange Event is coming", "retryNumber", retryNumber)
			switchLen := bftview.GetServerCommitteeLen()/2 + 1
			if t.retryNumber > switchLen && t.retryNumber%switchLen == 0 {
				log.Warn("Viewchange Event is coming", "double wait, retryNumber", retryNumber, "committee len", bftview.GetServerCommitteeLen())
				t.start()
				continue
			}
			curView := t.service.getCurrentView()
			if curView.ReconfigType == types.PowReconfig || curView.ReconfigType == types.PacePowReconfig {
				t.service.setNextLeader(types.PacePowReconfig)
			} else {
				if t.service.getBestCandidate(false) != nil || len(t.candidatepool.Content()) > 0 {
					t.service.setNextLeader(types.PacePowReconfig)
				} else {
					t.service.setNextLeader(types.PaceReconfig)
				}
			}
			t.service.sendNewViewMsg(curView.TxNumber)
			t.start()
			t.retryNumber++
		}
	}
}

var m_totalTxs int
var m_tps10StartTm time.Time

// Event for new block done
func (t *paceMakerTimer) procBlockDone(curBlock *types.Block, curKeyBlock *types.KeyBlock) {
	if curBlock != nil {
		if t.config.EnabledTPS {
			txs := len(curBlock.Transactions())
			m_totalTxs += txs
			if txs > 0 {
				now := time.Now()
				if m_tps10StartTm.Equal(time.Time{}) {
					m_tps10StartTm = now
				} else if now.Sub(m_tps10StartTm).Seconds() > 10 {
					tps := float64(m_totalTxs) / now.Sub(m_tps10StartTm).Seconds()
					log.Debug("@TPS10", "txs/s", tps)
					m_totalTxs = 0
					m_tps10StartTm = now
				}
				tps := float64(txs) / now.Sub(t.startTime).Seconds()
				log.Debug("@TPS", "txs/s", tps)
			}
		}

		n := (curBlock.NumberU64() - curKeyBlock.T_Number() + 1)
		if n > 0 {
			if n%params.KeyblockPerTxBlocks == 0 {
				t.service.setNextLeader(types.PowReconfig)
			} else if n%params.GapTxBlocks == 0 {
				t.service.setNextLeader(types.TimeReconfig)
			}
		}

		if curBlock.NumberU64()%20 == 0 {
			//log.Info("Goroutine", "num", runtime.NumGoroutine())
			runtime.GC() //force gc
		}

	}

	t.stop()
	if bftview.IamMember() >= 0 {
		t.start()
	}

}

// Event for New TXS coming
func (t *paceMakerTimer) onNewTx() {
	t.Lock()
	defer t.Unlock()
	if t.beStop || t.beClose {
		return
	}
	if t.startTime == maxPaceMakerTime {
		t.startTime = time.Now()
	}
}
