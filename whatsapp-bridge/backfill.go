package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/types"
)

// maxBackfillCount bounds what a caller can ask the phone for in one request.
const maxBackfillCount = 100

// unwrapHistoryMessage strips the same wrappers events.Message.UnwrapRaw strips,
// so history-synced messages are read the same way live ones are.
func unwrapHistoryMessage(m *waProto.Message) *waProto.Message {
	for m != nil {
		switch {
		case m.GetDeviceSentMessage().GetMessage() != nil:
			m = m.GetDeviceSentMessage().GetMessage()
		case m.GetEphemeralMessage().GetMessage() != nil:
			m = m.GetEphemeralMessage().GetMessage()
		case m.GetViewOnceMessage().GetMessage() != nil:
			m = m.GetViewOnceMessage().GetMessage()
		case m.GetViewOnceMessageV2().GetMessage() != nil:
			m = m.GetViewOnceMessageV2().GetMessage()
		case m.GetDocumentWithCaptionMessage().GetMessage() != nil:
			m = m.GetDocumentWithCaptionMessage().GetMessage()
		case m.GetEditedMessage().GetMessage() != nil:
			m = m.GetEditedMessage().GetMessage()
		default:
			return m
		}
	}
	return m
}

type backfillRequest struct {
	ChatJID string `json:"chat_jid"`
	Count   int    `json:"count"`
	// Optional anchor; the phone returns messages BEFORE it. Without one the
	// anchor is a synthetic key stamped "now", to reach messages newer than
	// anything in messages.db.
	BeforeID     string `json:"before_id,omitempty"`
	BeforeFromMe bool   `json:"before_from_me,omitempty"`
}

// requestOnDemandBackfill asks the user's own phone (a peer message to our own
// account, never visible to any contact) for recent history of one chat. The
// answer arrives as an ON_DEMAND *events.HistorySync and goes through
// handleHistorySync like any other sync.
func requestOnDemandBackfill(client *whatsmeow.Client, req backfillRequest) error {
	if !client.IsConnected() {
		return fmt.Errorf("not connected")
	}
	chat, err := types.ParseJID(req.ChatJID)
	if err != nil {
		return fmt.Errorf("bad chat_jid: %w", err)
	}
	if req.Count <= 0 || req.Count > maxBackfillCount {
		return fmt.Errorf("count must be 1..%d", maxBackfillCount)
	}
	anchor := &types.MessageInfo{
		MessageSource: types.MessageSource{Chat: chat, IsFromMe: req.BeforeFromMe},
		ID:            req.BeforeID,
		Timestamp:     time.Now(),
	}
	if anchor.ID == "" {
		anchor.ID = client.GenerateMessageID()
	}
	_, err = client.SendPeerMessage(context.Background(), client.BuildHistorySyncRequest(anchor, req.Count))
	return err
}

func registerBackfillHandler(client *whatsmeow.Client) {
	http.HandleFunc("/api/backfill", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req backfillRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
			http.Error(w, "Invalid request format", http.StatusBadRequest)
			return
		}
		if err := requestOnDemandBackfill(client, req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		fmt.Printf("Requested on-demand backfill of %d messages in %s\n", req.Count, req.ChatJID)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"success": true})
	})
}
