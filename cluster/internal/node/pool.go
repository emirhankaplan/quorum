package node

import "sync"

// WorkerPool is a fixed set of worker goroutines draining a bounded job queue.
// It is the Go analogue of the thread pool built in C++ Concurrency in Action
// (Ch. 9): a thread-safe queue of waitable tasks serviced by a fixed number of
// workers, where submitting a task hands back a future you can wait on. Here the
// queue is a buffered channel (Go's threadsafe_queue), the workers are
// goroutines, and Submit returns a Future backed by a one-shot channel
// (Go's std::future/std::promise).
type WorkerPool struct {
	jobs      chan func()
	wg        sync.WaitGroup
	mu        sync.Mutex
	accepting bool
}

// NewWorkerPool starts `workers` goroutines draining a queue of `queue` slots.
func NewWorkerPool(workers, queue int) *WorkerPool {
	if workers < 1 {
		workers = 1
	}
	if queue < 1 {
		queue = 1
	}
	p := &WorkerPool{jobs: make(chan func(), queue), accepting: true}
	p.wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer p.wg.Done()
			for job := range p.jobs {
				job()
			}
		}()
	}
	return p
}

// submit enqueues a job. It returns false once the pool is draining. The send
// happens under the same mutex that Shutdown uses to close the channel, so a
// send-on-closed-channel panic is impossible.
func (p *WorkerPool) submit(job func()) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.accepting {
		return false
	}
	p.jobs <- job
	return true
}

// Shutdown stops accepting work and blocks until every queued and in-flight job
// has finished — a clean cooperative drain rather than an abrupt teardown.
func (p *WorkerPool) Shutdown() {
	p.mu.Lock()
	if !p.accepting {
		p.mu.Unlock()
		p.wg.Wait()
		return
	}
	p.accepting = false
	close(p.jobs)
	p.mu.Unlock()
	p.wg.Wait()
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
