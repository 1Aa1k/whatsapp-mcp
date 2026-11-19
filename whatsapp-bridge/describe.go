package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"

	waProto "go.mau.fi/whatsmeow/binary/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// describeNonText turns message types the bridge used to drop silently into a
// readable placeholder, so every incoming message leaves a row in messages.db.
//
// Why: handleMessage only kept conversation/extendedText/media. Anything else
// (reaction, sticker, edit, location, contact, poll, video note, captions on
// media) was skipped with no log line. On 2026-11-19 a reply in chat
// 10904955572421@lid at 17:50:29 hit that path: the chats table got the
// timestamp, the messages table got nothing, and nobody could tell why.
//
// Returns ("", false) only for protocol housekeeping that is not a chat
// message (history-sync notices, key shares, ephemeral settings, ...).
func describeNonText(msg *waProto.Message) (string, bool) {
	if msg == nil {
		return "", false
	}
	if r := msg.GetReactionMessage(); r != nil {
		if r.GetText() == "" {
			return fmt.Sprintf("[reaction removed from %s]", r.GetKey().GetID()), true
		}
		return fmt.Sprintf("[reaction %s to %s]", r.GetText(), r.GetKey().GetID()), true
	}
	if p := msg.GetProtocolMessage(); p != nil {
		return describeProtocol(p)
	}
	if msg.GetStickerMessage() != nil {
		return "[sticker]", true
	}
	if l := msg.GetLocationMessage(); l != nil {
		return fmt.Sprintf("[location %.6f,%.6f %s %s]", l.GetDegreesLatitude(), l.GetDegreesLongitude(), l.GetName(), l.GetAddress()), true
	}
	if l := msg.GetLiveLocationMessage(); l != nil {
		return fmt.Sprintf("[live location %.6f,%.6f]", l.GetDegreesLatitude(), l.GetDegreesLongitude()), true
	}
	if c := msg.GetContactMessage(); c != nil {
		return fmt.Sprintf("[contact %s]\n%s", c.GetDisplayName(), c.GetVcard()), true
	}
	if c := msg.GetContactsArrayMessage(); c != nil {
		names := make([]string, 0, len(c.GetContacts()))
		for _, ct := range c.GetContacts() {
			names = append(names, ct.GetDisplayName())
		}
		return "[contacts " + strings.Join(names, ", ") + "]", true
	}
	for _, poll := range []*waProto.PollCreationMessage{msg.GetPollCreationMessage(), msg.GetPollCreationMessageV2(), msg.GetPollCreationMessageV3()} {
		if poll == nil {
			continue
		}
		opts := make([]string, 0, len(poll.GetOptions()))
		for _, o := range poll.GetOptions() {
			opts = append(opts, o.GetOptionName())
		}
		return fmt.Sprintf("[poll] %s: %s", poll.GetName(), strings.Join(opts, " | ")), true
	}
	if msg.GetPollUpdateMessage() != nil {
		return "[poll vote]", true
	}
	return "[unsupported message: " + setFieldNames(msg) + "]", true
}

func describeProtocol(p *waProto.ProtocolMessage) (string, bool) {
	switch p.GetType() {
	case waProto.ProtocolMessage_MESSAGE_EDIT:
		edited := p.GetEditedMessage()
		text := extractTextContent(edited)
		if text == "" {
			text = mediaCaption(edited)
		}
		return fmt.Sprintf("[edited %s] %s", p.GetKey().GetID(), text), true
	case waProto.ProtocolMessage_REVOKE:
		return fmt.Sprintf("[deleted %s]", p.GetKey().GetID()), true
	default:
		return "", false
	}
}

// mediaCaption returns the caption on an image/video/document, which
// extractTextContent never looked at.
func mediaCaption(msg *waProto.Message) string {
	if msg == nil {
		return ""
	}
	if m := msg.GetImageMessage(); m != nil {
		return m.GetCaption()
	}
	if m := msg.GetVideoMessage(); m != nil {
		return m.GetCaption()
	}
	if m := msg.GetPtvMessage(); m != nil {
		return m.GetCaption()
	}
	if m := msg.GetDocumentMessage(); m != nil {
		return m.GetCaption()
	}
	return ""
}

// setFieldNames lists which top-level fields are populated, for the log line
// and the placeholder of a type this file does not know yet.
func setFieldNames(msg *waProto.Message) string {
	var names []string
	msg.ProtoReflect().Range(func(fd protoreflect.FieldDescriptor, _ protoreflect.Value) bool {
		if fd.Name() != "messageContextInfo" {
			names = append(names, string(fd.Name()))
		}
		return true
	})
	if len(names) == 0 {
		return "empty"
	}
	return strings.Join(names, ",")
}

// storeSentMessage records a message this bridge sent through the REST API.
func storeSentMessage(client *whatsmeow.Client, store *MessageStore, chat types.JID, resp whatsmeow.SendResponse, text, mediaPath string) {
	content := text
	if content == "" && mediaPath != "" {
		content = "[sent file " + filepath.Base(mediaPath) + "]"
	}
	sender := ""
	if lid := client.Store.GetLID(); !lid.IsEmpty() {
		sender = lid.User
	} else if client.Store.ID != nil {
		sender = client.Store.ID.User
	}
	chatJID := chat.String()
	// Keep an existing chat name (StoreChat is INSERT OR REPLACE).
	name := chat.User
	var existing string
	if store.db.QueryRow("SELECT name FROM chats WHERE jid = ?", chatJID).Scan(&existing) == nil && existing != "" {
		name = existing
	}
	if err := store.StoreChat(chatJID, name, resp.Timestamp); err != nil {
		fmt.Printf("Failed to store chat for sent message: %v\n", err)
	}
	if err := store.StoreMessage(resp.ID, chatJID, sender, content, resp.Timestamp, true, "", "", "", nil, nil, nil, 0); err != nil {
		fmt.Printf("Failed to store sent message: %v\n", err)
	}
}
