package node

import "sync"

// WorkerPool is a fixed set of worker goroutines draining a bounded job queue.
// It is the Go analogue of the thread pool built in C++ Concurrency in Action
// (Ch. 9): a thread-safe queue of waitable tasks serviced by a fixed number of
// workers, where submitting a task hands back a future you can wait on. Here the
// queue is a buffered channel (Go's threadsafe_queue), the workers are
// goroutines, and Submit returns a Future backed by a one-shot channel
// (Go's std::future/std::promise).
//
// Shutdown is signalled through a `done` channel rather than by closing `jobs`,
// for two reasons: the job channel is never closed (so a concurrent submit can
// never panic with send-on-closed), and submit never sends while holding a lock
// (so it can never deadlock with Shutdown).
type WorkerPool struct {
	jobs    chan func()
	done    chan struct{}
	wg      sync.WaitGroup
	mu      sync.Mutex
	stopped bool
}

// NewWorkerPool starts `workers` goroutines draining a queue of `queue` slots.
func NewWorkerPool(workers, queue int) *WorkerPool {
	if workers < 1 {
		workers = 1
	}
	if queue < 1 {
		queue = 1
	}
	p := &WorkerPool{jobs: make(chan func(), queue), done: make(chan struct{})}
	p.wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer p.wg.Done()
			for {
				select {
				case job := <-p.jobs:
					job()
				case <-p.done:
					return
				}
			}
		}()
	}
	return p
}

// submit enqueues a job, returning false once the pool is shutting down. The
// send is performed under no lock and `jobs` is never closed, so submit can
// neither deadlock with Shutdown nor panic.
func (p *WorkerPool) submit(job func()) bool {
	select {
	case <-p.done:
		return false
	default:
	}
	select {
	case p.jobs <- job:
		return true
	case <-p.done:
		return false
	}
}

// Shutdown stops accepting work, lets the workers exit, then runs any jobs still
// sitting in the queue so their Futures resolve — a clean cooperative drain with
// no leaked waiters and no send-on-closed panic. Idempotent.
func (p *WorkerPool) Shutdown() {
	p.mu.Lock()
	if p.stopped {
		p.mu.Unlock()
		p.wg.Wait()
		return
	}
	p.stopped = true
	close(p.done)
	p.mu.Unlock()

	p.wg.Wait()
	for {
		select {
		case job := <-p.jobs:
			job()
		default:
			return
		}
	}
}

// Future is the result handle returned by Submit — the std::future analogue.
type Future[T any] struct{ ch chan T }

// Get blocks until the task completes and returns its value.
func (f Future[T]) Get() T { return <-f.ch }

// Submit runs fn on the pool and returns a Future for its result. If the pool is
// draining, fn runs inline so the future always resolves (no lost work).
func Submit[T any](p *WorkerPool, fn func() T) Future[T] {
	f := Future[T]{ch: make(chan T, 1)}
	if !p.submit(func() { f.ch <- fn() }) {
		f.ch <- fn()
	}
	return f
}
