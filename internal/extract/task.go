package extract

type Completed[T any] struct {
	Value T
	Err   error
}

var completed = func() <-chan struct{} {
	done := make(chan struct{})
	close(done)
	return done
}()

func (t Completed[T]) Done() <-chan struct{} { return completed }
func (t Completed[T]) Wait() (T, error)      { return t.Value, t.Err }
