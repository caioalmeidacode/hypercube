package main

import (
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

var errNotFound = errors.New("não encontrado")

type Event struct {
	Type string `json:"type"`
	Data any    `json:"data"`
}

type Hub struct {
	mu       sync.Mutex
	clients  map[chan []byte]struct{}
	ever     bool
	lastSeen time.Time
}

var hub = &Hub{clients: map[chan []byte]struct{}{}}

func (h *Hub) Subscribe() chan []byte {
	ch := make(chan []byte, 256)
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	h.ever = true
	h.mu.Unlock()
	return ch
}

func (h *Hub) Unsubscribe(ch chan []byte) {
	h.mu.Lock()
	delete(h.clients, ch)
	h.lastSeen = time.Now()
	h.mu.Unlock()
}

func (h *Hub) Count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.clients)
}

func (h *Hub) Emit(typ string, data any) {
	b, err := json.Marshal(Event{Type: typ, Data: data})
	if err != nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.clients {
		select {
		case ch <- b:
		default:
		}
	}
}

type Task struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Detail   string `json:"detail"`
	Instance string `json:"instance,omitempty"`
	Done     int64  `json:"done"`
	Total    int64  `json:"total"`
	Status   string `json:"status"`
	Error    string `json:"error,omitempty"`

	mu       sync.Mutex
	lastEmit time.Time
	done     atomic.Int64
	total    atomic.Int64
}

var (
	tasksMu sync.Mutex
	tasks   = map[string]*Task{}
)

func NewTask(title, instance string) *Task {
	t := &Task{ID: newID(), Title: title, Instance: instance, Status: "running"}
	tasksMu.Lock()
	tasks[t.ID] = t
	tasksMu.Unlock()
	t.emit(true)
	return t
}

func RunningTasks() int {
	tasksMu.Lock()
	defer tasksMu.Unlock()
	n := 0
	for _, t := range tasks {
		if t.Status == "running" {
			n++
		}
	}
	return n
}

func TaskSnapshot() []map[string]any {
	tasksMu.Lock()
	defer tasksMu.Unlock()
	out := []map[string]any{}
	for _, t := range tasks {
		if t.Status == "running" {
			out = append(out, t.snapshot())
		}
	}
	return out
}

func (t *Task) snapshot() map[string]any {
	t.mu.Lock()
	defer t.mu.Unlock()
	return map[string]any{
		"id": t.ID, "title": t.Title, "detail": t.Detail, "instance": t.Instance,
		"done": t.done.Load(), "total": t.total.Load(), "status": t.Status, "error": t.Error,
	}
}

func (t *Task) emit(force bool) {
	t.mu.Lock()
	if !force && time.Since(t.lastEmit) < 120*time.Millisecond {
		t.mu.Unlock()
		return
	}
	t.lastEmit = time.Now()
	t.mu.Unlock()
	hub.Emit("task", t.snapshot())
}

func (t *Task) Stage(detail string, total int64) {
	t.mu.Lock()
	t.Detail = detail
	t.mu.Unlock()
	t.done.Store(0)
	t.total.Store(total)
	t.emit(true)
}

func (t *Task) SetDetail(detail string) {
	t.mu.Lock()
	t.Detail = detail
	t.mu.Unlock()
	t.emit(false)
}

func (t *Task) AddTotal(n int64) { t.total.Add(n); t.emit(false) }
func (t *Task) Add(n int64)      { t.done.Add(n); t.emit(false) }

func (t *Task) Finish(err error) {
	t.mu.Lock()
	if err != nil {
		t.Status = "error"
		t.Error = err.Error()
	} else {
		t.Status = "done"
	}
	t.mu.Unlock()
	t.emit(true)
	go func() {
		time.Sleep(30 * time.Second)
		tasksMu.Lock()
		delete(tasks, t.ID)
		tasksMu.Unlock()
	}()
}
