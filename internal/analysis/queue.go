package analysis

import "context"

// Queue is shared by OCR and image workers: at most one protected child runs.
type Queue struct { slot chan struct{} }

func NewQueue() *Queue { return &Queue{slot:make(chan struct{},1)} }

func (q *Queue) Acquire(ctx context.Context) (func(),error) {
	select { case q.slot<-struct{}{}: return func(){<-q.slot},nil; case <-ctx.Done(): return nil,ctx.Err() }
}
