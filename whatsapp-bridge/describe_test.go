package main

import (
	"strings"
	"testing"

	waProto "go.mau.fi/whatsmeow/binary/proto"
	"google.golang.org/protobuf/proto"
)

func TestDescribeNonTextKeepsChatMessages(t *testing.T) {
	key := &waProto.MessageKey{ID: proto.String("ABC")}
	cases := map[string]struct {
		msg  *waProto.Message
		want string
	}{
		"reaction": {&waProto.Message{ReactionMessage: &waProto.ReactionMessage{Key: key, Text: proto.String("👍")}}, "[reaction 👍 to ABC]"},
		"sticker":  {&waProto.Message{StickerMessage: &waProto.StickerMessage{}}, "[sticker]"},
		"edit": {&waProto.Message{ProtocolMessage: &waProto.ProtocolMessage{
			Type: waProto.ProtocolMessage_MESSAGE_EDIT.Enum(), Key: key,
			EditedMessage: &waProto.Message{Conversation: proto.String("new text")},
		}}, "[edited ABC] new text"},
		"unknown": {&waProto.Message{EventMessage: &waProto.EventMessage{}}, "[unsupported message: eventMessage]"},
	}
	for name, tc := range cases {
		got, keep := describeNonText(tc.msg)
		if !keep || got != tc.want {
			t.Errorf("%s: got (%q,%v), want %q", name, got, keep, tc.want)
		}
	}
}

func TestDescribeNonTextSkipsProtocolHousekeeping(t *testing.T) {
	msg := &waProto.Message{ProtocolMessage: &waProto.ProtocolMessage{Type: waProto.ProtocolMessage_HISTORY_SYNC_NOTIFICATION.Enum()}}
	if got, keep := describeNonText(msg); keep {
		t.Fatalf("history sync notice kept as %q", got)
	}
	if !strings.Contains(setFieldNames(msg), "protocolMessage") {
		t.Fatal("field names missing protocolMessage")
	}
}
