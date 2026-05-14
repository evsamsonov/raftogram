// Package messenger implements the deterministic Raft FSM that holds messenger
// state: channels and their ordered message histories.
package messenger

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"sort"
	"sync"

	"github.com/hashicorp/raft"
	"google.golang.org/protobuf/proto"

	"github.com/evsamsonov/raftogram/internal/gen/raftogrampb"
)

const (
	// DefaultMaxMessageSizeBytes is the default maximum message payload accepted
	// by the FSM if no explicit limit is passed to NewFSM.
	DefaultMaxMessageSizeBytes = 64 * 1024
)

var (
	ErrUnknownCommand        = errors.New("unknown raft command")
	ErrChannelNotFound       = errors.New("channel not found")
	ErrPayloadTooLarge       = errors.New("payload exceeds max size")
	ErrMissingChannelID      = errors.New("channel_id is required")
	ErrInvalidMaxMessageSize = errors.New("max message size must be positive")
)

// FSM is the Raft finite-state machine for the messenger service.
type FSM struct {
	mu sync.RWMutex

	maxMessageSize int
	channels       map[string]*channelState
	dedupByChannel map[string]map[string]uint64
}

type channelState struct {
	id       string
	name     string
	messages []storedMessage
}

type storedMessage struct {
	sequence        uint64
	authorID        string
	payload         []byte
	clientMessageID string
}

// CreateChannelResult is returned from Apply for create-channel commands.
type CreateChannelResult struct {
	ChannelID string
	Created   bool
}

// SendMessageResult is returned from Apply for send-message commands.
type SendMessageResult struct {
	Sequence     uint64
	Deduplicated bool
}

var _ raft.FSM = (*FSM)(nil)

// NewFSM returns a deterministic messenger FSM with an in-memory state model.
func NewFSM(maxMessageSize int) (*FSM, error) {
	if maxMessageSize <= 0 {
		return nil, ErrInvalidMaxMessageSize
	}

	return &FSM{
		maxMessageSize: maxMessageSize,
		channels:       make(map[string]*channelState),
		dedupByChannel: make(map[string]map[string]uint64),
	}, nil
}

// NewDefaultFSM returns a messenger FSM configured with project defaults.
func NewDefaultFSM() *FSM {
	fsm, _ := NewFSM(DefaultMaxMessageSizeBytes)
	return fsm
}

// Apply is called by the Raft library once a log entry is committed. It applies
// the command to the in-memory state and returns an application-specific result.
func (f *FSM) Apply(logEntry *raft.Log) interface{} {
	decoded, err := DecodeRaftLogEntry(logEntry.Data)
	if err != nil {
		return err
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	switch command := decoded.GetCommand().(type) {
	case *raftogrampb.RaftLogEntry_CreateChannel:
		return f.applyCreateChannel(logEntry.Index, command.CreateChannel)
	case *raftogrampb.RaftLogEntry_SendMessage:
		return f.applySendMessage(command.SendMessage)
	default:
		return ErrUnknownCommand
	}
}

func (f *FSM) applyCreateChannel(logIndex uint64, command *raftogrampb.CreateChannelCommand) CreateChannelResult {
	channelID := command.GetChannelId()
	if channelID == "" {
		channelID = fmt.Sprintf("channel-%d", logIndex)
	}

	if _, ok := f.channels[channelID]; ok {
		return CreateChannelResult{
			ChannelID: channelID,
			Created:   false,
		}
	}

	f.channels[channelID] = &channelState{
		id:       channelID,
		name:     command.GetName(),
		messages: make([]storedMessage, 0, 1),
	}

	return CreateChannelResult{
		ChannelID: channelID,
		Created:   true,
	}
}

func (f *FSM) applySendMessage(command *raftogrampb.SendMessageCommand) interface{} {
	channelID := command.GetChannelId()
	if channelID == "" {
		return ErrMissingChannelID
	}

	channel, ok := f.channels[channelID]
	if !ok {
		return fmt.Errorf("%w: %s", ErrChannelNotFound, channelID)
	}

	payload := command.GetPayload()
	if len(payload) > f.maxMessageSize {
		return fmt.Errorf("%w: %d > %d", ErrPayloadTooLarge, len(payload), f.maxMessageSize)
	}

	clientMessageID := command.GetClientMessageId()
	if clientMessageID != "" {
		if dedupedSequence, ok := f.dedupByChannel[channelID][clientMessageID]; ok {
			return SendMessageResult{
				Sequence:     dedupedSequence,
				Deduplicated: true,
			}
		}
	}

	sequence := uint64(len(channel.messages) + 1)
	channel.messages = append(channel.messages, storedMessage{
		sequence:        sequence,
		authorID:        command.GetAuthorId(),
		payload:         slices.Clone(payload),
		clientMessageID: clientMessageID,
	})

	if clientMessageID != "" {
		if _, ok := f.dedupByChannel[channelID]; !ok {
			f.dedupByChannel[channelID] = make(map[string]uint64)
		}
		f.dedupByChannel[channelID][clientMessageID] = sequence
	}

	return SendMessageResult{
		Sequence:     sequence,
		Deduplicated: false,
	}
}

// Snapshot returns a point-in-time snapshot of the current FSM state.
func (f *FSM) Snapshot() (raft.FSMSnapshot, error) {
	f.mu.RLock()
	snapshot, err := f.snapshotStateLocked()
	f.mu.RUnlock()
	if err != nil {
		return nil, err
	}

	return &fsmSnapshot{data: snapshot}, nil
}

func (f *FSM) snapshotStateLocked() ([]byte, error) {
	channelIDs := make([]string, 0, len(f.channels))
	for channelID := range f.channels {
		channelIDs = append(channelIDs, channelID)
	}
	sort.Strings(channelIDs)

	state := &raftogrampb.SnapshotState{
		Channels: make([]*raftogrampb.ChannelSnapshot, 0, len(channelIDs)),
	}

	for _, channelID := range channelIDs {
		channel := f.channels[channelID]
		channelSnapshot := &raftogrampb.ChannelSnapshot{
			ChannelId: channel.id,
			Name:      channel.name,
			Messages:  make([]*raftogrampb.StoredMessage, 0, len(channel.messages)),
		}

		for _, msg := range channel.messages {
			channelSnapshot.Messages = append(channelSnapshot.Messages, &raftogrampb.StoredMessage{
				Sequence:        msg.sequence,
				AuthorId:        msg.authorID,
				Payload:         slices.Clone(msg.payload),
				ClientMessageId: msg.clientMessageID,
			})
		}

		state.Channels = append(state.Channels, channelSnapshot)
	}

	encoded, err := proto.Marshal(state)
	if err != nil {
		return nil, fmt.Errorf("marshal snapshot state: %w", err)
	}

	return encoded, nil
}

// Restore replaces the current FSM state with the contents of the snapshot.
func (f *FSM) Restore(rc io.ReadCloser) (err error) {
	defer func() {
		if closeErr := rc.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close snapshot reader: %w", closeErr))
		}
	}()

	payload, err := io.ReadAll(rc)
	if err != nil {
		return fmt.Errorf("read snapshot payload: %w", err)
	}

	state := &raftogrampb.SnapshotState{}
	if err := proto.Unmarshal(payload, state); err != nil {
		return fmt.Errorf("unmarshal snapshot state: %w", err)
	}

	restoredChannels := make(map[string]*channelState, len(state.GetChannels()))
	restoredDedup := make(map[string]map[string]uint64, len(state.GetChannels()))

	for _, channel := range state.GetChannels() {
		channelID := channel.GetChannelId()
		if channelID == "" {
			continue
		}

		entry := &channelState{
			id:       channelID,
			name:     channel.GetName(),
			messages: make([]storedMessage, 0, len(channel.GetMessages())),
		}

		for _, msg := range channel.GetMessages() {
			stored := storedMessage{
				sequence:        msg.GetSequence(),
				authorID:        msg.GetAuthorId(),
				payload:         slices.Clone(msg.GetPayload()),
				clientMessageID: msg.GetClientMessageId(),
			}
			entry.messages = append(entry.messages, stored)

			if stored.clientMessageID != "" {
				if _, ok := restoredDedup[channelID]; !ok {
					restoredDedup[channelID] = make(map[string]uint64)
				}
				restoredDedup[channelID][stored.clientMessageID] = stored.sequence
			}
		}

		restoredChannels[channelID] = entry
	}

	f.mu.Lock()
	f.channels = restoredChannels
	f.dedupByChannel = restoredDedup
	f.mu.Unlock()

	return nil
}

type fsmSnapshot struct {
	data []byte
}

func (s *fsmSnapshot) Persist(sink raft.SnapshotSink) error {
	if _, err := sink.Write(s.data); err != nil {
		if cancelErr := sink.Cancel(); cancelErr != nil {
			return errors.Join(
				fmt.Errorf("write snapshot: %w", err),
				fmt.Errorf("cancel snapshot sink: %w", cancelErr),
			)
		}

		return fmt.Errorf("write snapshot: %w", err)
	}

	if err := sink.Close(); err != nil {
		return fmt.Errorf("close snapshot sink: %w", err)
	}

	return nil
}

func (s *fsmSnapshot) Release() {}
