package main

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

const evalWaitingInterval = 30 * time.Second

type evalProgressTicker interface {
	Chan() <-chan time.Time
	Stop()
}

type realEvalProgressTicker struct{ *time.Ticker }

func (t realEvalProgressTicker) Chan() <-chan time.Time { return t.C }

type evalProgress struct {
	out       io.Writer
	now       func() time.Time
	newTicker func(time.Duration) evalProgressTicker
	waiter    func(evalWait) func()
}

func (p evalProgress) line(format string, args ...any) {
	if p.out != nil {
		fmt.Fprintf(p.out, format+"\n", args...)
	}
}

type evalWait struct {
	trial, phase string
	ceiling      time.Duration
}

func (p evalProgress) wait(trial, phase string, ceiling time.Duration) func() {
	wait := evalWait{trial: trial, phase: boundedPhase(phase), ceiling: ceiling}
	if p.waiter != nil {
		return p.waiter(wait)
	}
	if p.out == nil {
		return func() {}
	}
	now := p.now
	if now == nil {
		now = time.Now
	}
	newTicker := p.newTicker
	if newTicker == nil {
		newTicker = func(interval time.Duration) evalProgressTicker {
			return realEvalProgressTicker{time.NewTicker(interval)}
		}
	}
	started := now()
	ticker := newTicker(evalWaitingInterval)
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case tick := <-ticker.Chan():
				select {
				case <-stop:
					return
				default:
				}
				line := fmt.Sprintf("Trial %s waiting: phase=%s elapsed=%s", wait.trial, wait.phase, tick.Sub(started).Round(time.Second))
				if wait.ceiling > 0 {
					line += " ceiling=" + wait.ceiling.String()
				}
				p.line("%s", line)
			}
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() {
			close(stop)
			<-done
		})
	}
}

func boundedPhase(phase string) string {
	phase = strings.Join(strings.Fields(phase), " ")
	const max = 120
	if len(phase) <= max {
		return phase
	}
	return phase[:max-3] + "..."
}

func trialLabel(caseName, arm string, repeat int) string {
	return fmt.Sprintf("Case=%s Arm=%s repeat=%d", caseName, arm, repeat)
}

func gradeSummary(record trialRecord) string {
	if record.Grade == nil {
		return "grade=ungraded acceptance=n/a quality=n/a"
	}
	summary := fmt.Sprintf("grade=%s acceptance=n/a quality=n/a", record.Grade.Status)
	if record.Grade.Scores != nil {
		met := 0
		for _, item := range record.Grade.Scores.Items {
			if item.Met {
				met++
			}
		}
		if len(record.Grade.Scores.Items) > 0 {
			summary = fmt.Sprintf("grade=%s acceptance=%d/%d quality=%d/5", record.Grade.Status, met, len(record.Grade.Scores.Items), record.Grade.Scores.Quality)
		} else {
			summary = fmt.Sprintf("grade=%s acceptance=0 quality=%d/5", record.Grade.Status, record.Grade.Scores.Quality)
		}
	}
	return summary
}
