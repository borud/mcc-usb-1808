package stream

import (
	"bytes"
	"errors"
	"sync"
	"testing"
)

func TestSingleSubscriber(t *testing.T) {
	src := make(chan []byte, 3)
	f := NewFanout(src)
	sub := f.Subscribe(WithDepth(8))

	src <- []byte{1, 2, 3}
	src <- []byte{4, 5, 6}
	src <- []byte{7, 8, 9}
	close(src)

	go f.Run()

	var got [][]byte
	for chunk := range sub.Chunks() {
		cp := make([]byte, len(chunk))
		copy(cp, chunk)
		got = append(got, cp)
		sub.Release(chunk)
	}

	if len(got) != 3 {
		t.Fatalf("received %d chunks, want 3", len(got))
	}
	if !bytes.Equal(got[0], []byte{1, 2, 3}) {
		t.Errorf("chunk[0] = %v, want [1 2 3]", got[0])
	}
	if !bytes.Equal(got[2], []byte{7, 8, 9}) {
		t.Errorf("chunk[2] = %v, want [7 8 9]", got[2])
	}
	if sub.Err() != nil {
		t.Errorf("err = %v, want nil", sub.Err())
	}
}

func TestTwoSubscribers(t *testing.T) {
	src := make(chan []byte, 3)
	f := NewFanout(src)
	sub1 := f.Subscribe(WithDepth(8))
	sub2 := f.Subscribe(WithDepth(8))

	src <- []byte{10, 20}
	src <- []byte{30, 40}
	close(src)

	go f.Run()

	var got1, got2 [][]byte
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for chunk := range sub1.Chunks() {
			cp := make([]byte, len(chunk))
			copy(cp, chunk)
			got1 = append(got1, cp)
			sub1.Release(chunk)
		}
	}()
	go func() {
		defer wg.Done()
		for chunk := range sub2.Chunks() {
			cp := make([]byte, len(chunk))
			copy(cp, chunk)
			got2 = append(got2, cp)
			sub2.Release(chunk)
		}
	}()
	wg.Wait()

	if len(got1) != 2 || len(got2) != 2 {
		t.Fatalf("sub1=%d sub2=%d, want 2 each", len(got1), len(got2))
	}
	// Each subscriber gets independent copies.
	if !bytes.Equal(got1[0], got2[0]) {
		t.Errorf("sub1[0]=%v != sub2[0]=%v", got1[0], got2[0])
	}
}

func TestSlowSubscriberDrops(t *testing.T) {
	src := make(chan []byte)
	f := NewFanout(src)
	// Depth=1 means only 1 chunk can be buffered.
	sub := f.Subscribe(WithDepth(1))

	go f.Run()

	// Send enough to fill the buffer and cause drops.
	for i := range 10 {
		src <- []byte{byte(i)}
	}
	close(src)

	// Drain.
	for range sub.Chunks() {
	}

	if sub.Dropped() == 0 {
		t.Error("expected some drops with depth=1 and 10 fast sends")
	}
}

func TestErrPropagation(t *testing.T) {
	src := make(chan []byte, 1)
	f := NewFanout(src)
	sub := f.Subscribe(WithDepth(4))

	testErr := errors.New("upstream failure")
	f.SetErr(testErr)

	src <- []byte{1}
	close(src)
	f.Run()

	// Drain.
	for range sub.Chunks() {
	}

	if !errors.Is(sub.Err(), testErr) {
		t.Errorf("err = %v, want %v", sub.Err(), testErr)
	}
}

func TestSourceClosePropagatesToSubscribers(t *testing.T) {
	src := make(chan []byte)
	f := NewFanout(src)
	sub1 := f.Subscribe(WithDepth(4))
	sub2 := f.Subscribe(WithDepth(4))

	go f.Run()
	close(src)

	// Both subscribers' channels should close.
	_, ok1 := <-sub1.Chunks()
	_, ok2 := <-sub2.Chunks()
	if ok1 || ok2 {
		t.Error("expected both subscription channels to be closed")
	}
}
