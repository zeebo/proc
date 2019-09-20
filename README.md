# proc

    import "github.com/zeebo/proc"

## Usage

#### type Proc

```go
type Proc interface {
	Start(ctx context.Context) error
	Wait() error
}
```

Proc represents something that can be started and stopped and possibly has some dependencies on other Procs.

#### type Node

```go
type Node struct {
}
```

Node is a node in the DAG of Procs. It has a single Proc and a set of child Nodes that it manages.

#### func  New

```go
func New(proc Proc, children ...*Node) *Node
```

New wraps the Proc in a Node and ensures that when the returned Node is Run, all of the children are Run as well. The children are guaranteed to start after the parent, and exit before the parent.

#### func (*Node) Run

```go
func (n *Node) Run(ctx context.Context) (err error)
```

Run starts the associated Proc, recursively starts its children, waits for its children to exit, then cancels and waits for its associated Proc. There can only be one call to Run executing at a time.
