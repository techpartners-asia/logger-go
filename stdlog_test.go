package logger

import (
	"log"
	"testing"
)

func TestStdLogBridge(t *testing.T) {
	l, buf := newJSON(t)
	std := l.Std(LevelWarn)
	std.Printf("line one\nline two")
	ls := lines(t, buf)
	if len(ls) != 2 || ls[0]["msg"] != "line one" || ls[1]["level"] != "WARN" {
		t.Errorf("%v", ls)
	}
	buf.Reset()
	restore := RedirectStdLog(l, LevelInfo)
	log.Println("global")
	restore()
	if lines(t, buf)[0]["msg"] != "global" {
		t.Error("redirect")
	}
}
