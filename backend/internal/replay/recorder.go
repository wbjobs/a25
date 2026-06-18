package replay

import (
	"context"
	"sync"
	"time"

	pb "github.com/ecscard/game/internal/proto"
	"github.com/google/uuid"
)

const (
	DefaultSpectatorDelayMs = 5 * 60 * 1000
	MaxActionsInMemory      = 100000
	FlushIntervalMs         = 10000
)

type ReplayRecorder struct {
	mu               sync.RWMutex
	gameID           string
	matchID          string
	player1ID        string
	player1Name      string
	player2ID        string
	player2Name      string
	startTime        int64
	endTime          int64
	initialSnapshot  *pb.GameSnapshot
	latestSnapshot   *pb.GameSnapshot
	actions          []*pb.ReplayAction
	eventBuffer      []*pb.GameEvent
	spectatorCount   int32
	isLive           bool
	finished         bool
	winnerID         string
	totalTurns       int32
	lastFlushIndex   int
	spectatorDelayMs int32
	store            *ReplayStore
	eventSubs        map[string]chan *pb.ReplayAction
}

func NewReplayRecorder(gameID, matchID, p1ID, p1Name, p2ID, p2Name string, store *ReplayStore) *ReplayRecorder {
	return &ReplayRecorder{
		gameID:           gameID,
		matchID:          matchID,
		player1ID:        p1ID,
		player1Name:      p1Name,
		player2ID:        p2ID,
		player2Name:      p2Name,
		startTime:        time.Now().UnixNano() / 1e6,
		actions:          make([]*pb.ReplayAction, 0),
		eventBuffer:      make([]*pb.GameEvent, 0),
		isLive:           true,
		spectatorDelayMs: DefaultSpectatorDelayMs,
		store:            store,
		eventSubs:        make(map[string]chan *pb.ReplayAction),
	}
}

func (r *ReplayRecorder) SetInitialSnapshot(snap *pb.GameSnapshot) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.initialSnapshot = snap
	r.latestSnapshot = snap
}

func (r *ReplayRecorder) RecordAction(action *pb.Action, stateAfter *pb.GameStatus, events []*pb.GameEvent, frameNum uint64) {
	r.mu.Lock()
	currentMs := time.Now().UnixNano() / 1e6
	relativeMs := currentMs - r.startTime

	replayAction := &pb.ReplayAction{
		RelativeTimeMs: relativeMs,
		FrameNumber:    frameNum,
		Action:         action,
		StateAfter:     stateAfter,
		Events:         events,
	}

	r.actions = append(r.actions, replayAction)

	subs := make([]chan *pb.ReplayAction, 0, len(r.eventSubs))
	for _, ch := range r.eventSubs {
		subs = append(subs, ch)
	}

	actionCount := len(r.actions)
	r.mu.Unlock()

	for _, ch := range subs {
		select {
		case ch <- replayAction:
		default:
		}
	}

	if actionCount >= MaxActionsInMemory {
		_ = r.FlushToStore()
	}
}

func (r *ReplayRecorder) RecordEvent(event *pb.GameEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.eventBuffer = append(r.eventBuffer, event)
}

func (r *ReplayRecorder) RecordLatestSnapshot(snap *pb.GameSnapshot) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.latestSnapshot = snap
}

func (r *ReplayRecorder) MarkFinished(winnerID string, totalTurns int32) {
	r.mu.Lock()
	r.finished = true
	r.isLive = false
	r.endTime = time.Now().UnixNano() / 1e6
	r.winnerID = winnerID
	r.totalTurns = totalTurns
	r.mu.Unlock()

	_ = r.FlushToStore()
}

func (r *ReplayRecorder) GetMeta() *pb.ReplayMeta {
	r.mu.RLock()
	defer r.mu.RUnlock()

	phase := pb.ReplayPhase_REPLAY_PHASE_WAITING
	if r.finished {
		phase = pb.ReplayPhase_REPLAY_PHASE_FINISHED
	} else if r.isLive {
		phase = pb.ReplayPhase_REPLAY_PHASE_LIVE
	}

	duration := r.endTime - r.startTime
	if !r.finished {
		duration = time.Now().UnixNano()/1e6 - r.startTime
	}

	return &pb.ReplayMeta{
		GameId:           r.gameID,
		MatchId:          r.matchID,
		Player1Id:        r.player1ID,
		Player1Name:      r.player1Name,
		Player2Id:        r.player2ID,
		Player2Name:      r.player2Name,
		StartTimeUnixMs:  r.startTime,
		EndTimeUnixMs:    r.endTime,
		TotalTurns:       r.totalTurns,
		WinnerId:         r.winnerID,
		DurationMs:       duration,
		SpectatorDelayMs: r.spectatorDelayMs,
		IsLive:           r.isLive,
		Phase:            phase,
		InitialSnapshot:  r.initialSnapshot,
		TotalActions:     int32(len(r.actions)),
	}
}

func (r *ReplayRecorder) GetSegment(fromTimeMs, toTimeMs int64, fromIndex, toIndex int32) *pb.ReplaySegment {
	r.mu.RLock()
	defer r.mu.RUnlock()

	total := len(r.actions)
	startIdx := int(fromIndex)
	endIdx := int(toIndex)

	if startIdx < 0 {
		startIdx = 0
	}
	if endIdx <= 0 || endIdx > total {
		endIdx = total
	}

	if fromTimeMs >= 0 || toTimeMs > 0 {
		for i, act := range r.actions {
			if fromTimeMs >= 0 && act.RelativeTimeMs >= fromTimeMs && startIdx == 0 {
				startIdx = i
			}
			if toTimeMs > 0 && act.RelativeTimeMs > toTimeMs && endIdx == total {
				endIdx = i
				break
			}
		}
	}

	if startIdx > endIdx {
		startIdx = endIdx
	}

	segmentActions := make([]*pb.ReplayAction, 0)
	if startIdx < endIdx && startIdx < total {
		if endIdx > total {
			endIdx = total
		}
		segmentActions = append(segmentActions, r.actions[startIdx:endIdx]...)
	}

	hasMore := endIdx < total
	actualStartMs := int64(0)
	actualEndMs := int64(0)
	if len(segmentActions) > 0 {
		actualStartMs = segmentActions[0].RelativeTimeMs
		actualEndMs = segmentActions[len(segmentActions)-1].RelativeTimeMs
	}

	return &pb.ReplaySegment{
		GameId:      r.gameID,
		StartTimeMs: actualStartMs,
		EndTimeMs:   actualEndMs,
		StartIndex:  int32(startIdx),
		EndIndex:    int32(endIdx),
		Actions:     segmentActions,
		HasMore:     hasMore,
	}
}

func (r *ReplayRecorder) GetActionsCount() int32 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return int32(len(r.actions))
}

func (r *ReplayRecorder) AddSpectator() int32 {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.spectatorCount++
	return r.spectatorCount
}

func (r *ReplayRecorder) RemoveSpectator() int32 {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.spectatorCount > 0 {
		r.spectatorCount--
	}
	return r.spectatorCount
}

func (r *ReplayRecorder) GetSpectatorCount() int32 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.spectatorCount
}

func (r *ReplayRecorder) GetCurrentStreamTimeMs() int64 {
	r.mu.RLock()
	defer r.mu.RUnlock()

	currentMs := time.Now().UnixNano() / 1e6
	elapsed := currentMs - r.startTime

	if r.isLive {
		delayed := elapsed - int64(r.spectatorDelayMs)
		if delayed < 0 {
			delayed = 0
		}
		if elapsed < delayed {
			return elapsed
		}
		return delayed
	}

	return elapsed
}

func (r *ReplayRecorder) Subscribe() chan *pb.ReplayAction {
	r.mu.Lock()
	defer r.mu.Unlock()

	ch := make(chan *pb.ReplayAction, 256)
	r.eventSubs[uuid.New().String()] = ch
	return ch
}

func (r *ReplayRecorder) Unsubscribe(ch chan *pb.ReplayAction) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for key, subCh := range r.eventSubs {
		if subCh == ch {
			close(ch)
			delete(r.eventSubs, key)
			break
		}
	}
}

func (r *ReplayRecorder) FlushToStore() error {
	r.mu.Lock()
	if r.store == nil {
		r.mu.Unlock()
		return nil
	}

	flushCount := len(r.actions) - r.lastFlushIndex
	if flushCount <= 0 {
		r.mu.Unlock()
		return nil
	}

	actionsToFlush := make([]*ReplayActionDocument, 0, flushCount)
	for i := r.lastFlushIndex; i < len(r.actions); i++ {
		act := r.actions[i]
		actionData, _ := act.Action.Marshal()
		stateData, _ := act.StateAfter.Marshal()
		eventsData := make([][]byte, 0, len(act.Events))
		for _, ev := range act.Events {
			evData, _ := ev.Marshal()
			eventsData = append(eventsData, evData)
		}
		actionsToFlush = append(actionsToFlush, &ReplayActionDocument{
			GameID:      r.gameID,
			RelativeMs:  act.RelativeTimeMs,
			FrameNumber: act.FrameNumber,
			ActionData:  actionData,
			StateAfter:  stateData,
			EventsData:  eventsData,
			ActionIndex: int32(i),
		})
	}

	meta := &ReplayDocument{
		GameID:           r.gameID,
		MatchID:          r.matchID,
		Player1ID:        r.player1ID,
		Player1Name:      r.player1Name,
		Player2ID:        r.player2ID,
		Player2Name:      r.player2Name,
		StartTimeMs:      r.startTime,
		EndTimeMs:        r.endTime,
		DurationMs:       r.endTime - r.startTime,
		WinnerID:         r.winnerID,
		TotalTurns:       r.totalTurns,
		TotalActions:     int32(len(r.actions)),
		IsLive:           r.isLive,
		SpectatorDelayMs: r.spectatorDelayMs,
	}

	if r.initialSnapshot != nil {
		meta.InitialSnapshot, _ = r.initialSnapshot.Marshal()
	}

	newLastFlush := len(r.actions)
	r.mu.Unlock()

	ctx, cancel := contextWithTimeout()
	defer cancel()

	if err := r.store.SaveReplayMeta(ctx, meta); err != nil {
		return err
	}
	if err := r.store.SaveReplayActions(ctx, r.gameID, actionsToFlush); err != nil {
		return err
	}

	r.mu.Lock()
	r.lastFlushIndex = newLastFlush
	r.mu.Unlock()

	return nil
}

func (r *ReplayRecorder) GetSpectatorDelayMs() int32 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.spectatorDelayMs
}

func (r *ReplayRecorder) SeekByTime(targetMs int64) (*pb.GameSnapshot, int32, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	targetIdx := int32(0)
	for i, act := range r.actions {
		if act.RelativeTimeMs <= targetMs {
			targetIdx = int32(i + 1)
		} else {
			break
		}
	}

	snap := r.initialSnapshot
	if targetIdx > 0 && int(targetIdx) <= len(r.actions) {
		act := r.actions[targetIdx-1]
		if act.StateAfter != nil && r.latestSnapshot != nil {
			snap = &pb.GameSnapshot{
				FrameNumber: act.FrameNumber,
				Status:      act.StateAfter,
			}
		}
	}

	return snap, targetIdx, nil
}

func (r *ReplayRecorder) IsDelayedStreamAvailable() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if !r.isLive {
		return true
	}

	elapsed := time.Now().UnixNano()/1e6 - r.startTime
	return elapsed >= int64(r.spectatorDelayMs)
}

func contextWithTimeout() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 10*time.Second)
}
