package reconfig

import (
	"sync"
	"sync/atomic"
	"time"
)

//------------------------------------------------------------------------------------------------------------------------------------
type CDataQueue struct {
	head    []interface{}
	headPos int
	tail    []interface{}
	muQueue sync.Mutex
}

// len returns the number of items in the queue.
func (q *CDataQueue) len() int {
	return len(q.head) - q.headPos + len(q.tail)
}

// pushBack adds w to the back of the queue.
func (q *CDataQueue) pushBack(w interface{}) {
	q.muQueue.Lock()
	defer q.muQueue.Unlock()
	q.tail = append(q.tail, w)
}

// popFront removes and returns the wantConn at the front of the queue.
func (q *CDataQueue) popFront() interface{} {
	q.muQueue.Lock()
	defer q.muQueue.Unlock()
	if q.headPos >= len(q.head) {
		if len(q.tail) == 0 {
			return nil
		}
		// Pick up tail as new head, clear tail.
		q.head, q.headPos, q.tail = q.tail, 0, q.head[:0]
	}
	w := q.head[q.headPos]
	q.head[q.headPos] = nil
	q.headPos++
	return w
}

// peekFront returns the wantConn at the front of the queue without removing it.
func (q *CDataQueue) peekFront() interface{} {
	if q.headPos < len(q.head) {
		return q.head[q.headPos]
	}
	if len(q.tail) > 0 {
		return q.tail[0]
	}
	return nil
}

// cleanFront pops any wantConns that are no longer waiting from the head of the
// queue, reporting whether any were popped.
func (q *CDataQueue) cleanFront() (cleaned bool) {
	for {
		w := q.peekFront()
		if w == nil {
			return cleaned
		}
		q.popFront()
		cleaned = true
	}
}

//------------------------------------------------------------------------------------------------------------------------------------

const MaxTime = time.Hour * 24 * 365 * 100

// Job function
//type Job func()

type JobFunc func(data ...interface{})

type Job struct {
	runfunc JobFunc
	runargs []interface{}
}

func (runner *Job) Run() {
	runner.runfunc(runner.runargs...)
}

//fcurTask    chan defTaskRunner

type worker struct {
	workerPool chan *worker
	jobQueue   *CDataQueue
	stop       chan struct{}
	idleTime   time.Duration

	dis    *dispatcher
	isStop bool
}
type GoPool struct {
	dispatcher       *dispatcher
	wg               sync.WaitGroup
	enableWaitForAll bool
}
type dispatcher struct {
	workerPool  chan *worker
	jobQueue    *CDataQueue
	stop        chan struct{}
	idleTime    time.Duration
	workerCount int32

	locker sync.Mutex
	pool   *GoPool
}

func newWorker(workerPool chan *worker, idleTime time.Duration, dis *dispatcher) *worker {

	return &worker{
		workerPool: workerPool,
		jobQueue:   new(CDataQueue),
		stop:       make(chan struct{}, 1),
		idleTime:   idleTime,
		dis:        dis,
	}
}

// One worker start to work
func (w *worker) start() {

	var idTime = w.idleTime
	if idTime <= 0 {
		idTime = MaxTime
	}
	tc := time.NewTimer(idTime)

	for {
		w.workerPool <- w
		select {
		case <-tc.C:

			w.dis.locker.Lock()
			if !w.isStop {
				w.isStop = true
				w.dis.workerCount--
			} else {
				<-w.stop
			}
			w.dis.locker.Unlock()
			return
		case <-w.stop:
			return
		default:
			j := w.jobQueue.popFront()
			if j != nil {
				job := j.(Job)
				p := w.dis.pool
				if p != nil && p.enableWaitForAll {
					p.wg.Add(1)
				}
				job.Run()
				if p != nil && p.enableWaitForAll {
					p.wg.Done()
				}
				if idTime != MaxTime {
					tc = time.NewTimer(idTime)
				}
			}
		}
	}
}

func newDispatcher(workerPool chan *worker) *dispatcher {
	return &dispatcher{workerPool: workerPool, jobQueue: new(CDataQueue), stop: make(chan struct{})}
}

func (dis *dispatcher) startWorker() {

	dis.locker.Lock()
	defer dis.locker.Unlock()

	if dis.workerCount < int32(cap(dis.workerPool)) {
		dis.workerCount++
		worker := newWorker(dis.workerPool, dis.idleTime, dis)
		go worker.start()
	}
}

// Dispatch job to free worker
func (dis *dispatcher) dispatch() {
	for {
		worker := <-dis.workerPool

		select {
		case <-dis.stop:

			func() {
				dis.locker.Lock()
				defer dis.locker.Unlock()

				wl := len(dis.workerPool)
				for i := 0; i < wl; i++ {
					worker := <-dis.workerPool
					if !worker.isStop {
						worker.isStop = true
						worker.stop <- struct{}{}
					}

				}
			}()
			time.Sleep(100 * time.Millisecond)
			dis.stop <- struct{}{}
			return
		default:
			if !worker.isStop {
				j := dis.jobQueue.popFront()
				worker.jobQueue.pushBack(j)
			} else {
				dis.startWorker()
			}
		}
	}
}

// WorkerNum is worker number of worker pool,on worker have one goroutine
// JobNum is job number of job pool
func NewGoPool(workerNum int, enableWaitForAll bool) *GoPool {
	workers := make(chan *worker, workerNum)
	pool := &GoPool{
		dispatcher:       newDispatcher(workers),
		enableWaitForAll: enableWaitForAll,
	}
	pool.dispatcher.pool = pool
	return pool

}
func (p *GoPool) Start() *GoPool {
	go p.dispatcher.dispatch()
	return p
}

// After idleTime stop a worker circulate, default 100 year
func (p *GoPool) SetIdleDuration(idleTime time.Duration) *GoPool {
	if p.dispatcher != nil {
		p.dispatcher.idleTime = idleTime
	}
	return p
}
func (p *GoPool) WorkerLength() int {
	if p.dispatcher != nil {
		return int(atomic.LoadInt32(&p.dispatcher.workerCount))
	}
	return 0
}

func (p *GoPool) AddJob(job Job) {

	p.dispatcher.startWorker()
	p.dispatcher.jobQueue.pushBack(job)
}

func (p *GoPool) AddFunc(routineFunc JobFunc, params ...interface{}) {
	job := Job{runfunc: routineFunc, runargs: params}
	p.AddJob(job)
}

// Wait all job finish
func (p *GoPool) WaitForAll() {
	if p.enableWaitForAll {
		p.wg.Wait()
	}
}

// Stop all worker
func (p *GoPool) StopAll() {
	p.dispatcher.stop <- struct{}{}
	<-p.dispatcher.stop
}
