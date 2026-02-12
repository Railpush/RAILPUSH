package services

import (
	"bufio"
	"bytes"
	"fmt"
	"os/exec"
	"sync"

	"github.com/railpush/api/config"
)

type Logger struct {
	Config *config.Config
	mu     sync.RWMutex
	subs   map[string][]chan string
}

func NewLogger(cfg *config.Config) *Logger {
	return &Logger{Config: cfg, subs: make(map[string][]chan string)}
}

func (l *Logger) Subscribe(cid string) chan string {
	ch := make(chan string, 100)
	l.mu.Lock()
	l.subs[cid] = append(l.subs[cid], ch)
	l.mu.Unlock()
	return ch
}

func (l *Logger) Unsubscribe(cid string, ch chan string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	s := l.subs[cid]
	for i, c := range s {
		if c == ch {
			l.subs[cid] = append(s[:i], s[i+1:]...)
			close(ch)
			return
		}
	}
}

func (l *Logger) TailContainer(cid string) {
	cmd := exec.Command("docker", "logs", "-f", cid)
	out, _ := cmd.StdoutPipe()
	cmd.Start()
	s := bufio.NewScanner(out)
	for s.Scan() {
		l.bcast(cid, s.Text())
	}
	cmd.Wait()
}

func (l *Logger) bcast(cid, line string) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	for _, ch := range l.subs[cid] {
		select {
		case ch <- line:
		default:
		}
	}
}

func (l *Logger) GetLogs(cid string, n int) ([]string, error) {
	o, e := exec.Command("docker", "logs", "--tail", fmt.Sprintf("%d", n), cid).Output()
	if e != nil {
		return nil, e
	}
	var r []string
	s := bufio.NewScanner(bytes.NewReader(o))
	for s.Scan() {
		r = append(r, s.Text())
	}
	return r, nil
}
