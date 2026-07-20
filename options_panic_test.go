package telebot

import "testing"

func TestExtractOptionsStringPanics(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Logf("panic: %v", r)
		} else {
			t.Fatal("expected panic")
		}
	}()
	b := &Bot{}
	b.extractOptions([]interface{}{&SendOptions{}, "caption string"})
}
