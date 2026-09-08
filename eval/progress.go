package main

import (
	"fmt"
	"io"
)

type evalProgress struct {
	out io.Writer
}

func (p evalProgress) line(format string, args ...any) {
	if p.out != nil {
		fmt.Fprintf(p.out, format+"\n", args...)
	}
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
