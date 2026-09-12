package telebot

import (
	"encoding/json"
	"testing"
)

func TestMessageRawPreservation(t *testing.T) {
	in := []byte(`{"message_id":1,"text":"hello","extra":true}`)
	var msg Message
	if err := json.Unmarshal(in, &msg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if string(msg.Raw) != string(in) {
		t.Fatalf("raw = %q", msg.Raw)
	}
	out, err := json.Marshal(&msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(out) != string(in) {
		t.Fatalf("out = %s", out)
	}
}

func TestMessageBuilderBuildText(t *testing.T) {
	b := &MessageBuilder{}
	b.Write(T("hello", Args{NoNewline: true}))
	msg := b.Build(42)
	if msg == nil || msg.Text != "hello" {
		t.Fatalf("msg = %#v", msg)
	}
	if msg.Chat == nil || msg.Chat.ID != 42 {
		t.Fatalf("chat = %#v", msg.Chat)
	}
}

func TestMessageBuilderAddFile(t *testing.T) {
	b := &MessageBuilder{}
	b.AddFile("photo-id", "static/photo.png", "image/png")
	msg := b.Build(1)
	if msg.Photo == nil || msg.Photo.File.FileID != "photo-id" {
		t.Fatalf("photo = %#v", msg.Photo)
	}
}
