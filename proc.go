package proc

import (
	"context"
	"sync"

	"github.com/zeebo/errs"
)

// Proc represents something that can be started and stopped and possibly
// has some dependencies on other Procs.
type Proc interface {
	Start(ctx context.Context) error
	Wait() error
}

//
//
//

// Node is a node in the DAG of Procs. It has a single Proc and a set of
// child Nodes that it manages.
type Node struct {
	mu   sync.Mutex
	once *sync.Once
	err  error

	proc     Proc
	children []*Node
}

// New wraps the Proc in a Node and ensures that when the returned
// Node is Run, all of the children are Run as well. The children
// are guaranteed to start after the parent, and exit before the parent.
func New(proc Proc, children ...*Node) *Node {
	return &Node{proc: proc, children: children}
}

// lockAll recursively locks this node and all of the children of this node.
func (n *Node) lockAll(locks map[*Node]struct{}) {
	if _, ok := locks[n]; !ok {
		n.mu.Lock()
		locks[n] = struct{}{}
	}
	for _, child := range n.children {
		child.lockAll(locks)
	}
}

// unlockAll recursively unlocks this node and all of the children of this node.
func (n *Node) unlockAll(locks map[*Node]struct{}) {
	if _, ok := locks[n]; ok {
		n.mu.Unlock()
		delete(locks, n)
	}
	for _, child := range n.children {
		child.unlockAll(locks)
	}
}

// resetAll recursively resets this node and all of the children of this node.
func (n *Node) resetAll() {
	n.once = nil
	n.err = nil
	for _, child := range n.children {
		child.resetAll()
	}
}

// Run starts the associated Proc, recursively starts its children, waits for its
// children to exit, then cancels and waits for its associated Proc. There can
// only be one call to Run executing at a time.
func (n *Node) Run(ctx context.Context) (err error) {
	// Ensure that only one Run is happening at a time.
	n.mu.Lock()
	if n.once != nil {
		n.mu.Unlock()
		return errs.New("cannot call run concurrently")
	}
	n.once = new(sync.Once)
	n.mu.Unlock()

	// Ok, now start the run logic.
	n.once.Do(func() { n.err = n.doRun(ctx) })
	err = n.err

	// Now recursively reset the children.
	locks := make(map[*Node]struct{})
	n.lockAll(locks)
	n.resetAll()
	n.unlockAll(locks)

	return err
}

// startRun executes doRun but ensures that it only runs once.
func (n *Node) startRun(ctx context.Context) (err error) {
	n.mu.Lock()
	if n.once == nil {
		n.once = new(sync.Once)
	}
	n.mu.Unlock()

	n.once.Do(func() { n.err = n.doRun(ctx) })
	return n.err
}

// doRun does the work of running: it starts the proc, starts all of the
// child procs, waits for all of the child procs, then cancels and waits
// for the proc.
func (n *Node) doRun(ctx context.Context) error {
	// start the proc with a context where we control when it closes.
	doneCtx, done := newDoneContext(ctx)
	if err := n.proc.Start(doneCtx); err != nil {
		return err
	}

	// launch all the children using the provided context.
	errCh := make(chan error, len(n.children))
	for _, child := range n.children {
		child := child
		go func() { errCh <- child.startRun(ctx) }()
	}

	// wait for all of the children to exit and then wait for
	// the provided context to be done.
	var eg errs.Group
	for range n.children {
		eg.Add(<-errCh)
	}
	<-ctx.Done()

	// close the done channel for the proc we're responsible for
	// and wait for it.
	close(done)
	eg.Add(n.proc.Wait())
	return eg.Err()
}

//
//
//

// doneContext is a helper to override the done channel of some context.
type doneContext struct {
	context.Context
	done chan struct{}
}

// newDoneContext returns a context and done channel from some parent context.
// The done channel is not closed when the parent's done channel is closed.
func newDoneContext(ctx context.Context) (context.Context, chan struct{}) {
	done := make(chan struct{})
	return &doneContext{
		Context: ctx,
		done:    done,
	}, done
}

// Done returns the done channel associated with the context.
func (d *doneContext) Done() <-chan struct{} {
	return d.done
}
